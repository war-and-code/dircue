// Package structure runs the optional, explicitly selected native structural worker.
// The worker parses each supported file once for observations and BCA metrics.
package structure

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

const MaxSourceBytes int64 = 8 << 20
const DefaultTimeout = 10 * time.Second

type Options struct {
	Worker       string
	Functions    bool
	Hotspots     bool
	MaxFileBytes int64
	Timeout      time.Duration
}

type Provenance struct {
	BCA        string `json:"bca"`
	TreeSitter string `json:"tree_sitter"`
	Grammar    string `json:"grammar"`
}

// File contains deterministic observations, not a claim of semantic resolution.
// Partial means the grammar recovered from missing or unrecognized syntax.
type File struct {
	Path         string            `json:"path"`
	Hotspots     *FileHotspots     `json:"hotspots,omitempty"`
	Functions    *FunctionSpaces   `json:"functions,omitempty"`
	SourceSHA256 string            `json:"source_sha256,omitempty"`
	Language     string            `json:"language"`
	Status       string            `json:"status"`
	Reason       string            `json:"reason,omitempty"`
	SourceBytes  int64             `json:"source_bytes"`
	ParseCount   int               `json:"parse_count"`
	SyntaxErrors bool              `json:"syntax_errors"`
	Observations map[string]uint64 `json:"observations,omitempty"`
	Metrics      json.RawMessage   `json:"metrics,omitempty"`
	Provenance   *Provenance       `json:"provenance,omitempty"`
}

// Client permits one live worker at a time. Each process releases its parser
// allocations on exit; MaxFileBytes limits input size, not process RSS.
type Client struct {
	options   Options
	admission chan struct{}
}

func New(options Options) (*Client, error) {
	if options.Worker == "" {
		return nil, errors.New("structure requires an explicit worker path")
	}
	if options.MaxFileBytes == 0 {
		options.MaxFileBytes = MaxSourceBytes
	}
	if options.MaxFileBytes < 1 || options.MaxFileBytes > MaxSourceBytes {
		return nil, fmt.Errorf("structure max file bytes must be in 1..%d", MaxSourceBytes)
	}
	if options.Timeout == 0 {
		options.Timeout = DefaultTimeout
	}
	if options.Timeout < 0 {
		return nil, errors.New("structure timeout must be positive")
	}
	absolute, err := filepath.Abs(options.Worker)
	if err != nil {
		return nil, fmt.Errorf("structure worker path: %w", err)
	}
	info, err := os.Stat(absolute)
	if err != nil {
		return nil, fmt.Errorf("structure worker: %w", err)
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("structure worker must be a regular executable file")
	}
	resolved, err := exec.LookPath(absolute)
	if err != nil {
		return nil, fmt.Errorf("structure worker is not executable: %w", err)
	}
	options.Worker = resolved
	return &Client{options: options, admission: make(chan struct{}, 1)}, nil
}

func (c *Client) HotspotsEnabled() bool { return c.options.Hotspots }

func (c *Client) FunctionMetricsEnabled() bool { return c.options.Functions }

func (c *Client) MaxFileBytes() int64 { return c.options.MaxFileBytes }
func (c *Client) WorkerPath() string  { return c.options.Worker }

func (c *Client) Analyze(ctx context.Context, path, language string, content []byte) (File, error) {
	result := File{Path: path, Language: language, SourceBytes: int64(len(content)), Status: "skipped"}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	switch {
	case !Supports(language):
		result.Reason = "unsupported_language"
	case len(path) > 16<<10 || !utf8.ValidString(path) || strings.ContainsRune(path, 0):
		result.Reason = "invalid_path"
	case int64(len(content)) > c.options.MaxFileBytes:
		result.Reason = "file_too_large"
	case !utf8.Valid(content):
		result.Reason = "invalid_utf8"
	case bytes.IndexByte(content, 0) >= 0:
		result.Reason = "binary_source"
	}
	if result.Reason != "" {
		return result, nil
	}
	select {
	case c.admission <- struct{}{}:
		defer func() { <-c.admission }()
	case <-ctx.Done():
		return result, ctx.Err()
	}
	// Admission precedes JSON encoding, so queued callers cannot multiply the
	// escaped source buffer and worker allocations.
	if err := ctx.Err(); err != nil {
		return result, err
	}
	request := struct {
		Path      string `json:"path"`
		Language  string `json:"language"`
		Source    string `json:"source"`
		Mode      string `json:"mode"`
		Functions bool   `json:"functions,omitempty"`
		Hotspots  bool   `json:"hotspots,omitempty"`
	}{path, language, string(content), "combined", c.options.Functions, c.options.Hotspots}
	body, err := json.Marshal(request)
	if err != nil {
		return result, err
	}
	child, cancel := context.WithTimeout(ctx, c.options.Timeout)
	defer cancel()
	stdout := &cappedBuffer{limit: 16 << 20, cancel: cancel}
	stderr := &cappedBuffer{limit: 64 << 10, cancel: cancel}
	cmd := exec.CommandContext(child, c.options.Worker)
	cmd.Stdin = bytes.NewReader(body)
	cmd.Stdout, cmd.Stderr = stdout, stderr
	cmd.WaitDelay = time.Second
	err = cmd.Run()
	if stdout.overflow || stderr.overflow {
		return result, errors.New("structure worker output limit exceeded")
	}
	if child.Err() != nil {
		return result, fmt.Errorf("structure worker: %w", child.Err())
	}
	if err != nil {
		if c.options.Hotspots && unsupportedHotspotRequest(stdout.buf.Bytes()) {
			return result, fmt.Errorf("structure worker does not support hotspots; use an updated worker: %w", err)
		}
		if c.options.Functions && unsupportedFunctionRequest(stdout.buf.Bytes()) {
			return result, fmt.Errorf("structure worker does not support function-space metrics; use an updated worker: %w", err)
		}
		return result, fmt.Errorf("structure worker: %w; stderr: %s", err, strings.TrimSpace(stderr.buf.String()))
	}
	return decodeEnrichedResponse(stdout.buf.Bytes(), result, content, c.options.Functions, c.options.Hotspots)
}

func decode(data []byte, submitted File) (File, error) {
	return decodeResponse(data, submitted, nil, false)
}

func decodeResponse(data []byte, submitted File, content []byte, functions bool) (File, error) {
	return decodeEnrichedResponse(data, submitted, content, functions, false)
}

func decodeEnrichedResponse(data []byte, submitted File, content []byte, functions, hotspots bool) (File, error) {
	if (functions || hotspots) && !strictFunctionResponse(data) {
		return submitted, errors.New("structure worker function response contains invalid JSON, duplicate keys, or field aliases")
	}
	var response struct {
		File
		Functions    json.RawMessage `json:"functions"`
		Hotspots     json.RawMessage `json:"hotspots"`
		SourceBytes  *int64          `json:"source_bytes"`
		ParseCount   *int            `json:"parse_count"`
		SyntaxErrors *bool           `json:"syntax_errors"`
	}
	if err := json.Unmarshal(data, &response); err != nil {
		return submitted, fmt.Errorf("structure worker invalid JSON: %w", err)
	}
	fail := func() (File, error) {
		return submitted, errors.New("structure worker response violates identity, provenance, or single-parse contract")
	}
	if response.Path != submitted.Path || response.Language != submitted.Language || response.SourceBytes == nil || *response.SourceBytes != submitted.SourceBytes || response.ParseCount == nil || *response.ParseCount != 1 || response.SyntaxErrors == nil {
		return fail()
	}
	if response.Status != "complete" && response.Status != "partial" {
		return fail()
	}
	if (*response.SyntaxErrors) != (response.Status == "partial") {
		return fail()
	}
	capability, supported := capabilities[submitted.Language]
	if !supported {
		return fail()
	}
	p := response.Provenance
	if p == nil || p.BCA != "big-code-analysis@2.2.0" || p.TreeSitter != "0.26.12" || p.Grammar != capability.Grammar {
		return fail()
	}
	fields := capability.Observations
	if len(response.Observations) != len(fields) {
		return fail()
	}
	for _, name := range fields {
		if _, ok := response.Observations[name]; !ok {
			return fail()
		}
	}
	if response.Observations["syntax_nodes"] == 0 {
		return fail()
	}
	// Some grammars mark the root as recovered without exposing a traversable
	// ERROR or missing node. Positive counters still require syntax_errors.
	if !*response.SyntaxErrors && (response.Observations["error_nodes"] > 0 || response.Observations["missing_nodes"] > 0) {
		return fail()
	}
	metrics, ok := decodeMetrics(response.Metrics, capability.MetricGroups)
	if !ok {
		return fail()
	}
	response.File.SourceBytes = *response.SourceBytes
	response.File.ParseCount = *response.ParseCount
	response.File.SyntaxErrors = *response.SyntaxErrors
	response.File.Reason = ""
	if *response.SyntaxErrors {
		response.File.Reason = "syntax_errors"
	}
	response.File.Hotspots = nil
	response.File.Functions = nil
	response.File.SourceSHA256 = ""
	if functions {
		if len(response.Functions) == 0 {
			return submitted, errors.New("structure worker did not return requested function-space metrics; use a worker with function support")
		}
		// strictFunctionResponse already checked every nested function metric.
		decoded, ok := decodeValidatedFunctions(response.Functions, content, *response.SyntaxErrors, response.Observations["syntax_nodes"])
		if !ok {
			return submitted, errors.New("structure worker function-space response violates its metrics, bounds, or coverage contract")
		}
		response.File.Functions = decoded
		response.File.SourceSHA256 = fmt.Sprintf("%x", sha256.Sum256(content))
	} else if len(response.Functions) != 0 {
		return fail()
	}
	if hotspots {
		if len(response.Hotspots) == 0 {
			return submitted, errors.New("structure worker did not return requested hotspots; use a worker with hotspot support")
		}
		decoded, ok := decodeHotspots(response.Hotspots, content, *response.SyntaxErrors, response.Observations["syntax_nodes"])
		if !ok {
			return submitted, hotspotFailure()
		}
		response.File.Hotspots = decoded
		response.File.SourceSHA256 = fmt.Sprintf("%x", sha256.Sum256(content))
	} else if len(response.Hotspots) != 0 {
		return fail()
	}
	// Marshal again to remove whitespace from the native wire representation.
	response.File.Metrics, _ = json.Marshal(metrics)
	return response.File, nil
}

// BCA 2.2.0 exposes these groups for languages with class and member metrics.
// Keep the boundary synchronized with the pinned native worker's output.
var metricGroups = []string{"abc", "cognitive", "cyclomatic", "halstead", "loc", "mi", "nargs", "nexits", "nom", "npa", "npm", "tokens", "wmc"}

func decodeMetrics(data []byte, groups []string) (map[string]any, bool) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var metrics map[string]any
	if decoder.Decode(&metrics) != nil || len(metrics) != len(groups) {
		return nil, false
	}
	for _, name := range groups {
		group, ok := metrics[name].(map[string]any)
		if !ok || len(group) == 0 || !numericMetric(group, 0) {
			return nil, false
		}
	}
	return metrics, true
}

func numericMetric(value any, depth int) bool {
	if depth > 32 {
		return false
	}
	switch v := value.(type) {
	case nil:
		return true // Upstream undefined floating-point values serialize as null.
	case json.Number:
		_, err := strconv.ParseFloat(string(v), 64)
		return err == nil
	case map[string]any:
		if len(v) == 0 {
			return false
		}
		for _, child := range v {
			if !numericMetric(child, depth+1) {
				return false
			}
		}
		return true
	default:
		return false
	}
}

type cappedBuffer struct {
	buf      bytes.Buffer
	limit    int
	overflow bool
	cancel   context.CancelFunc
}

func (b *cappedBuffer) Write(p []byte) (int, error) {
	if len(p) > b.limit-b.buf.Len() {
		b.buf.Write(p[:b.limit-b.buf.Len()])
		b.overflow = true
		b.cancel()
		return len(p), nil
	}
	return b.buf.Write(p)
}
