package structure

import (
	"bytes"
	"context"
	"crypto/sha256"
	"dircue/internal/jsontext"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/bits"
	"os/exec"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const HotspotLimit = 10
const HotspotPathMaxBytes = 1024
const HotspotHistogramBuckets = 65

// HotspotEntry identifies one BCA Function-kind space. Nested spaces can overlap.
type HotspotEntry struct {
	Index      uint64 `json:"index"`
	Name       string `json:"name,omitempty"`
	NameStatus string `json:"name_status"`
	StartLine  uint64 `json:"start_line"`
	EndLine    uint64 `json:"end_line"`
	Value      uint64 `json:"value"`
}

type HotspotMetric struct {
	Metric    string         `json:"metric"`
	Count     uint64         `json:"count"`
	Min       *uint64        `json:"min"`
	Max       *uint64        `json:"max"`
	Histogram []uint64       `json:"histogram"`
	Top       []HotspotEntry `json:"top"`
}

// FileHotspots describes all valid-span function spaces, before evidence retention.
type FileHotspots struct {
	Provider          string          `json:"provider"`
	Rule              string          `json:"rule"`
	RuleVersion       string          `json:"rule_version"`
	SyntaxErrors      bool            `json:"syntax_errors"`
	TotalSpaces       uint64          `json:"total_spaces"`
	InvalidSpanSpaces uint64          `json:"invalid_span_spaces"`
	Metrics           []HotspotMetric `json:"metrics"`
}

type HotspotEvidence struct {
	Path         string `json:"path,omitempty"`
	PathStatus   string `json:"path_status"`
	PathSHA256   string `json:"path_sha256,omitempty"`
	sortPath     string
	SourceSHA256 string `json:"source_sha256"`
	HotspotEntry
}

type HotspotDistribution struct {
	Metric      string            `json:"metric"`
	Definition  string            `json:"definition"`
	Unit        string            `json:"unit"`
	MetricScope string            `json:"metric_scope"`
	Count       uint64            `json:"count"`
	Min         *uint64           `json:"min"`
	Max         *uint64           `json:"max"`
	Histogram   []uint64          `json:"histogram"`
	Top         []HotspotEvidence `json:"top"`
}

type HotspotGroup struct {
	Language          string                `json:"language"`
	Grammar           string                `json:"grammar"`
	SyntaxCohort      string                `json:"syntax_cohort"`
	AnalyzedFiles     uint64                `json:"analyzed_files"`
	TotalSpaces       uint64                `json:"total_spaces"`
	InvalidSpanSpaces uint64                `json:"invalid_span_spaces"`
	Metrics           []HotspotDistribution `json:"metrics"`
}

// HotspotReport ranks only the measured population. Omissions describe selected
// files whose function population is unknown; counts do not imply zero functions.
type HotspotReport struct {
	Provider           string           `json:"provider"`
	Rule               string           `json:"rule"`
	RuleVersion        string           `json:"rule_version"`
	Scope              string           `json:"scope"`
	Status             string           `json:"status"`
	FileCoverageStatus string           `json:"file_coverage_status"`
	Population         string           `json:"population"`
	HistogramRule      string           `json:"histogram_rule"`
	Order              string           `json:"order"`
	Limit              int              `json:"limit"`
	PathMaxBytes       int              `json:"path_max_bytes"`
	NameMaxBytes       int              `json:"name_max_bytes"`
	AnalyzedFiles      uint64           `json:"analyzed_files"`
	RecoveredFiles     uint64           `json:"recovered_files"`
	TotalSpaces        uint64           `json:"total_spaces"`
	InvalidSpanSpaces  uint64           `json:"invalid_span_spaces"`
	Omissions          map[string]int64 `json:"omissions"`
	Groups             []HotspotGroup   `json:"groups"`
}

func NewHotspotReport() *HotspotReport {
	return &HotspotReport{Provider: "big-code-analysis@2.2.0", Rule: "function-population", RuleVersion: "1.0.0", Scope: "selected-source-files", Status: "complete", FileCoverageStatus: "complete", Population: "all_valid_span_function_spaces_before_retention", HistogramRule: "bucket_0_is_zero;bucket_i_is_[2^(i-1),2^i-1]_for_i_1_to_64", Order: "value_desc_then_path_then_provider_index", Limit: HotspotLimit, NameMaxBytes: FunctionNameMaxBytes, PathMaxBytes: HotspotPathMaxBytes, Omissions: map[string]int64{}, Groups: []HotspotGroup{}}
}

func hotspotMetricDefinition(name string) (definition, unit, scope string) {
	if name == "cyclomatic_sum" {
		return "BCA standard cyclomatic sum for a Function-kind space and its nested spaces; overlapping spaces are not additive", "cyclomatic_count", "includes_nested_spaces"
	}
	return "Inclusive physical source-line span of a Function-kind space; includes blank lines, comments and nested spaces", "physical_lines", "includes_nested_spans"
}

// Add accepts files returned by Client.Analyze. It merges their exact
// histograms and each file's top ten. A file's eleventh-ranked
// space cannot outrank the repository's tenth-ranked space under the same order.
func (r *HotspotReport) Add(file File) error {
	f := file.Hotspots
	if f == nil {
		return nil
	}
	if file.Provenance == nil || !Supports(file.Language) || f.Provider != "big-code-analysis@2.2.0" || f.Rule != "function-population" || f.RuleVersion != "1.0.0" || f.InvalidSpanSpaces > f.TotalSpaces || len(f.Metrics) != 2 {
		return errors.New("structure hotspot aggregate requires a client-validated file")
	}
	for i, metric := range f.Metrics {
		if metric.Metric != []string{"cyclomatic_sum", "span_lines"}[i] || metric.Count != f.TotalSpaces-f.InvalidSpanSpaces || len(metric.Histogram) != HotspotHistogramBuckets || len(metric.Top) > HotspotLimit {
			return errors.New("structure hotspot aggregate received malformed metrics")
		}
		var count uint64
		for _, n := range metric.Histogram {
			if n > metric.Count-count {
				return errors.New("structure hotspot aggregate received malformed histogram")
			}
			count += n
		}
		if count != metric.Count {
			return errors.New("structure hotspot aggregate received incomplete histogram")
		}
	}
	if f.TotalSpaces > math.MaxUint64-r.TotalSpaces || r.AnalyzedFiles == math.MaxUint64 {
		return errors.New("structure hotspot counter overflow")
	}
	cohort := "clean"
	if f.SyntaxErrors {
		cohort = "recovered"
	}
	index := slices.IndexFunc(r.Groups, func(g HotspotGroup) bool { return g.Language == file.Language && g.SyntaxCohort == cohort })
	if index < 0 {
		group := HotspotGroup{Language: file.Language, Grammar: file.Provenance.Grammar, SyntaxCohort: cohort, Metrics: []HotspotDistribution{}}
		for _, m := range f.Metrics {
			definition, unit, scope := hotspotMetricDefinition(m.Metric)
			group.Metrics = append(group.Metrics, HotspotDistribution{Metric: m.Metric, Definition: definition, Unit: unit, MetricScope: scope, Histogram: make([]uint64, HotspotHistogramBuckets), Top: []HotspotEvidence{}})
		}
		r.Groups = append(r.Groups, group)
		index = len(r.Groups) - 1
	}
	g := &r.Groups[index]
	r.AnalyzedFiles++
	g.AnalyzedFiles++
	r.TotalSpaces += f.TotalSpaces
	g.TotalSpaces += f.TotalSpaces
	r.InvalidSpanSpaces += f.InvalidSpanSpaces
	g.InvalidSpanSpaces += f.InvalidSpanSpaces
	if f.SyntaxErrors {
		r.RecoveredFiles++
	}
	for i, m := range f.Metrics {
		aggregate := &g.Metrics[i]
		aggregate.Count += m.Count
		if m.Min != nil && (aggregate.Min == nil || *m.Min < *aggregate.Min) {
			n := *m.Min
			aggregate.Min = &n
		}
		if m.Max != nil && (aggregate.Max == nil || *m.Max > *aggregate.Max) {
			n := *m.Max
			aggregate.Max = &n
		}
		for bucket, count := range m.Histogram {
			aggregate.Histogram[bucket] += count
		}
		for _, entry := range m.Top {
			value := HotspotEvidence{Path: file.Path, PathStatus: "present", PathSHA256: fmt.Sprintf("%x", sha256.Sum256([]byte(file.Path))), sortPath: file.Path, SourceSHA256: file.SourceSHA256, HotspotEntry: entry}
			if file.Path == "" || len(file.Path) > HotspotPathMaxBytes || strings.ContainsFunc(file.Path, unicode.IsControl) {
				value.Path = ""
				value.PathStatus = "omitted"
			}
			insertion, _ := slices.BinarySearchFunc(aggregate.Top, value, compareHotspot)
			if insertion >= HotspotLimit {
				continue
			}
			aggregate.Top = slices.Insert(aggregate.Top, insertion, value)
			if len(aggregate.Top) > HotspotLimit {
				aggregate.Top = aggregate.Top[:HotspotLimit]
			}
		}
	}
	return nil
}

func compareHotspot(a, b HotspotEvidence) int {
	if a.Value > b.Value {
		return -1
	}
	if a.Value < b.Value {
		return 1
	}
	if n := strings.Compare(a.sortPath, b.sortPath); n != 0 {
		return n
	}
	if a.Index < b.Index {
		return -1
	}
	if a.Index > b.Index {
		return 1
	}
	return 0
}

func (r *HotspotReport) Finish(fileCoverageStatus string, omissions map[string]int64) {
	r.FileCoverageStatus = fileCoverageStatus
	r.Status = fileCoverageStatus
	for reason, count := range omissions {
		r.Omissions[reason] = count
	}
	if r.Status != "skipped" && (r.RecoveredFiles > 0 || r.InvalidSpanSpaces > 0) {
		r.Status = "partial"
	}
	slices.SortFunc(r.Groups, func(a, b HotspotGroup) int {
		if n := strings.Compare(a.Language, b.Language); n != 0 {
			return n
		}
		return strings.Compare(a.SyntaxCohort, b.SyntaxCohort)
	})
}

func decodeHotspots(data, content []byte, syntaxErrors bool, syntaxNodes uint64) (*FileHotspots, bool) {
	if !jsontext.ValidUnicode(data) {
		return nil, false
	}
	var raw map[string]json.RawMessage
	if json.Unmarshal(data, &raw) != nil || !exactKeys(raw, "provider", "rule", "rule_version", "syntax_errors", "total_spaces", "invalid_span_spaces", "metrics") {
		return nil, false
	}
	if !boundedHotspotArray(raw["metrics"], 2) {
		return nil, false
	}
	var rawMetrics []map[string]json.RawMessage
	if json.Unmarshal(raw["metrics"], &rawMetrics) != nil || len(rawMetrics) != 2 {
		return nil, false
	}
	for _, metric := range rawMetrics {
		if !boundedHotspotArray(metric["top"], HotspotLimit) || !boundedHotspotArray(metric["histogram"], HotspotHistogramBuckets) {
			return nil, false
		}
		var buckets []json.RawMessage
		if json.Unmarshal(metric["histogram"], &buckets) != nil {
			return nil, false
		}
		for _, bucket := range buckets {
			if bytes.Equal(bytes.TrimSpace(bucket), []byte("null")) {
				return nil, false
			}
		}
	}
	var f FileHotspots
	if json.Unmarshal(data, &f) != nil || f.Provider != "big-code-analysis@2.2.0" || f.Rule != "function-population" || f.RuleVersion != "1.0.0" || f.SyntaxErrors != syntaxErrors || f.TotalSpaces > syntaxNodes || f.InvalidSpanSpaces > f.TotalSpaces || len(f.Metrics) != 2 {
		return nil, false
	}
	lines := uint64(bytes.Count(content, []byte{'\n'}))
	if len(content) > 0 && content[len(content)-1] != '\n' {
		lines++
	}
	for i, name := range []string{"cyclomatic_sum", "span_lines"} {
		m := &f.Metrics[i]
		rm := rawMetrics[i]
		if !exactKeys(rm, "metric", "count", "min", "max", "histogram", "top") || m.Metric != name || m.Count != f.TotalSpaces-f.InvalidSpanSpaces || len(m.Histogram) != HotspotHistogramBuckets || m.Top == nil || len(m.Top) != int(min(m.Count, uint64(HotspotLimit))) {
			return nil, false
		}
		if m.Count == 0 {
			if m.Min != nil || m.Max != nil {
				return nil, false
			}
		} else if m.Min == nil || m.Max == nil || *m.Min > *m.Max || (name == "span_lines" && (*m.Min == 0 || *m.Max > lines)) {
			return nil, false
		}
		var count uint64
		for bucket, n := range m.Histogram {
			if n > m.Count-count {
				return nil, false
			}
			count += n
			if n > 0 && (bucket < bits.Len64(*m.Min) || bucket > bits.Len64(*m.Max)) {
				return nil, false
			}
		}
		if count != m.Count {
			return nil, false
		}
		if m.Count > 0 && (m.Histogram[bits.Len64(*m.Min)] == 0 || m.Histogram[bits.Len64(*m.Max)] == 0 || m.Top[0].Value != *m.Max) {
			return nil, false
		}
		var rawEntries []map[string]json.RawMessage
		if json.Unmarshal(rm["top"], &rawEntries) != nil {
			return nil, false
		}
		seen := map[uint64]bool{}
		topBuckets := make([]uint64, HotspotHistogramBuckets)
		for j, e := range m.Top {
			re := rawEntries[j]
			if !functionKeys(re, []string{"index", "name_status", "start_line", "end_line", "value"}, "name") || e.Index == 0 || e.Index > f.TotalSpaces || seen[e.Index] || e.StartLine < 1 || e.EndLine < e.StartLine || e.EndLine > lines || e.Value < *m.Min || e.Value > *m.Max {
				return nil, false
			}
			seen[e.Index] = true
			if name == "span_lines" && e.Value != e.EndLine-e.StartLine+1 {
				return nil, false
			}
			if j > 0 && (m.Top[j-1].Value < e.Value || (m.Top[j-1].Value == e.Value && m.Top[j-1].Index >= e.Index)) {
				return nil, false
			}
			_, named := re["name"]
			switch e.NameStatus {
			case "present":
				if !named || e.Name == "" || len(e.Name) > FunctionNameMaxBytes || !utf8.ValidString(e.Name) || strings.ContainsFunc(e.Name, unicode.IsControl) {
					return nil, false
				}
			case "unavailable", "omitted":
				if named {
					return nil, false
				}
			default:
				return nil, false
			}
			bucket := bits.Len64(e.Value)
			topBuckets[bucket]++
			if topBuckets[bucket] > m.Histogram[bucket] {
				return nil, false
			}
		}
		if m.Count > 0 && *m.Min == *m.Max {
			for position, entry := range m.Top {
				if entry.Index-uint64(position+1) > f.InvalidSpanSpaces {
					return nil, false
				}
			}
		}
		if len(m.Top) > 0 {
			cutoffBucket := bits.Len64(m.Top[len(m.Top)-1].Value)
			for bucket := cutoffBucket + 1; bucket < HotspotHistogramBuckets; bucket++ {
				if topBuckets[bucket] != m.Histogram[bucket] {
					return nil, false
				}
			}
		}
		if m.Count <= HotspotLimit {
			for b, n := range topBuckets {
				if n != m.Histogram[b] {
					return nil, false
				}
			}
			if m.Count > 0 && m.Top[len(m.Top)-1].Value != *m.Min {
				return nil, false
			}
		}
	}
	identities := map[uint64]HotspotEntry{}
	for _, entry := range f.Metrics[0].Top {
		identities[entry.Index] = entry
	}
	for _, entry := range f.Metrics[1].Top {
		if prior, exists := identities[entry.Index]; exists && (prior.Name != entry.Name || prior.NameStatus != entry.NameStatus || prior.StartLine != entry.StartLine || prior.EndLine != entry.EndLine) {
			return nil, false
		}
	}
	return &f, true
}

func exactKeys(raw map[string]json.RawMessage, fields ...string) bool {
	if len(raw) != len(fields) {
		return false
	}
	for _, field := range fields {
		value, exists := raw[field]
		if !exists {
			return false
		}
		if field != "min" && field != "max" && bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return false
		}
	}
	return true
}

func unsupportedHotspotRequest(data []byte) bool {
	var failure struct {
		Status     string
		ParseCount *int `json:"parse_count"`
		Error      struct{ Code, Message string }
	}
	return json.Unmarshal(data, &failure) == nil && failure.Status == "error" && failure.ParseCount != nil && *failure.ParseCount == 0 && failure.Error.Code == "invalid_request" && strings.HasPrefix(failure.Error.Message, "unknown field `hotspots`")
}

func hotspotFailure() error {
	return fmt.Errorf("structure worker hotspot response violates its population, metrics, bounds, or coverage contract")
}

func boundedHotspotArray(data []byte, limit int) bool {
	decoder := json.NewDecoder(bytes.NewReader(data))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('[') {
		return false
	}
	count := 0
	for decoder.More() {
		if count == limit {
			return false
		}
		var item json.RawMessage
		if decoder.Decode(&item) != nil {
			return false
		}
		count++
	}
	token, err = decoder.Token()
	return err == nil && token == json.Delim(']')
}

// CheckCapabilities negotiates opt-in features. Legacy analysis
// never starts a probe. Call once per scan, including scans with no source files.
func (c *Client) CheckCapabilities(ctx context.Context) error {
	if !c.options.Hotspots && !c.options.Functions {
		return nil
	}
	options := c.options
	ctx, cancel := context.WithTimeout(ctx, options.Timeout)
	defer cancel()
	stdout := &cappedBuffer{limit: 4096, cancel: cancel}
	stderr := &cappedBuffer{limit: 4096, cancel: cancel}
	cmd := exec.CommandContext(ctx, options.Worker, "--capabilities")
	configureWorkerProcess(cmd)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	cmd.WaitDelay = time.Second
	err := cmd.Run()
	if stdout.overflow || stderr.overflow {
		return errors.New("structure worker hotspot capability response exceeds limit")
	}
	if ctx.Err() != nil {
		return fmt.Errorf("structure worker hotspot capability check: %w", ctx.Err())
	}
	if err != nil || !validCapabilities(stdout.buf.Bytes(), c.options.Functions, c.options.Hotspots) {
		return errors.New("structure worker does not advertise requested function or hotspot support; use an updated worker")
	}
	return nil
}

func validHotspotCapabilities(data []byte) bool {
	return validCapabilities(data, false, true)
}

func validCapabilities(data []byte, functions, hotspots bool) bool {
	if !jsontext.ValidUnicode(data) {
		return false
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if !functionJSONValue(decoder, 0) {
		return false
	}
	var raw map[string]json.RawMessage
	if json.Unmarshal(data, &raw) != nil || !exactKeys(raw, "protocol", "version", "parse_count", "features") {
		return false
	}
	var response struct {
		Protocol   string
		Version    int
		ParseCount int `json:"parse_count"`
		Features   []string
	}
	if json.Unmarshal(data, &response) != nil || response.Protocol != "dircue-structural-worker" || response.Version != 1 || response.ParseCount != 0 || len(response.Features) > 32 {
		return false
	}
	seen := map[string]bool{}
	for _, feature := range response.Features {
		if feature == "" || len(feature) > 64 || seen[feature] {
			return false
		}
		seen[feature] = true
	}
	return (!functions || seen["functions"]) && (!hotspots || seen["hotspots"])
}
