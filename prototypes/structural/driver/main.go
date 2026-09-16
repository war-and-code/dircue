// This experimental driver is separate from dircue's production CLI.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"
)

const maxSourceBytes int64 = 8 << 20

type request struct {
	Path     string `json:"path"`
	Language string `json:"language"`
	Source   string `json:"source"`
	Mode     string `json:"mode"`
}

type fileRecord struct {
	Type     string          `json:"type"`
	Path     string          `json:"path"`
	Language string          `json:"language,omitempty"`
	Status   string          `json:"status"`
	Reason   string          `json:"reason,omitempty"`
	Result   json.RawMessage `json:"result,omitempty"`
}

type summary struct {
	Type                string `json:"type"`
	Scope               string `json:"scope"`
	Mode                string `json:"mode"`
	Status              string `json:"status"`
	Observed            int    `json:"observed"`
	Skipped             int    `json:"skipped"`
	Errors              int    `json:"errors"`
	PartialFiles        int    `json:"partial_files"`
	ExcludedDirectories int    `json:"excluded_directories"`
	IgnoredFiles        int    `json:"ignored_files"`
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	os.Exit(run(ctx, os.Args[1:], os.Stdout, os.Stderr))
}

func run(ctx context.Context, args []string, out, errOut io.Writer) int {
	flags := flag.NewFlagSet("dircue-structural-prototype", flag.ContinueOnError)
	flags.SetOutput(errOut)
	worker := flags.String("worker", "", "path to the prebuilt worker (required)")
	mode := flags.String("mode", "combined", "combined, structure, or metrics")
	limit := flags.Int64("max-file-bytes", maxSourceBytes, "per-file source limit, at most 8388608 bytes; not an RSS limit")
	timeout := flags.Duration("timeout", 10*time.Second, "worker time limit per file")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if flags.NArg() > 1 || *worker == "" || *limit < 1 || *limit > maxSourceBytes || *timeout <= 0 || (*mode != "combined" && *mode != "structure" && *mode != "metrics") {
		fmt.Fprintln(errOut, "require --worker, at most one directory, a positive --timeout, --max-file-bytes in 1..8388608, and --mode combined|structure|metrics; place flags before the directory")
		return 2
	}
	workerPath, err := filepath.Abs(*worker)
	if err != nil {
		fmt.Fprintln(errOut, err)
		return 2
	}
	workerInfo, err := os.Stat(workerPath)
	if err != nil || !workerInfo.Mode().IsRegular() {
		fmt.Fprintln(errOut, "worker must be an existing executable file:", workerPath)
		return 2
	}
	if _, err := exec.LookPath(workerPath); err != nil {
		fmt.Fprintln(errOut, "worker is not executable:", err)
		return 2
	}
	root := "."
	if flags.NArg() == 1 {
		root = flags.Arg(0)
	}
	root, err = filepath.Abs(root)
	if err != nil {
		fmt.Fprintln(errOut, err)
		return 2
	}
	rootInfo, err := os.Lstat(root)
	if err != nil || !rootInfo.IsDir() {
		fmt.Fprintln(errOut, "target must be an existing directory, not a symlink:", root)
		return 2
	}
	enc := json.NewEncoder(out)
	s := summary{Type: "summary", Scope: "working-directory", Mode: *mode, Status: "complete"}
	walkErr := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if walkErr != nil {
			s.Errors++
			return enc.Encode(fileRecord{Type: "file", Path: rel, Status: "error", Reason: "walk: " + walkErr.Error()})
		}
		if entry.IsDir() {
			if rel != "." && excludedDirectory(entry.Name()) {
				s.ExcludedDirectories++
				if err := enc.Encode(fileRecord{Type: "directory", Path: rel, Status: "skipped", Reason: "excluded-directory"}); err != nil {
					return err
				}
				return filepath.SkipDir
			}
			return nil
		}
		lang := language(entry.Name())
		if lang == "" {
			s.IgnoredFiles++
			return nil
		}
		r := fileRecord{Type: "file", Path: rel, Language: lang, Status: "observed"}
		content, reason, err := readSource(path, entry, *limit)
		if err != nil {
			r.Status, r.Reason = "error", "read: "+err.Error()
			s.Errors++
		} else if reason != "" {
			r.Status, r.Reason = "skipped", reason
			s.Skipped++
		} else {
			result, partial, err := invoke(ctx, workerPath, request{Path: rel, Language: lang, Source: string(content), Mode: *mode}, *timeout)
			if err != nil {
				r.Status, r.Reason = "error", err.Error()
				s.Errors++
			} else {
				r.Result = result
				s.Observed++
				if partial {
					s.PartialFiles++
				}
			}
		}
		return enc.Encode(r)
	})
	if walkErr != nil {
		fmt.Fprintln(errOut, walkErr)
		s.Errors++
	}
	if s.Errors > 0 || s.Skipped > 0 || s.PartialFiles > 0 {
		s.Status = "partial"
	}
	if err := enc.Encode(s); err != nil {
		fmt.Fprintln(errOut, err)
		return 1
	}
	if s.Errors > 0 {
		return 1
	}
	return 0
}

func excludedDirectory(name string) bool {
	if strings.HasPrefix(name, ".") {
		return true
	}
	switch name {
	case "node_modules", "vendor", "bin", "obj", "target":
		return true
	}
	return false
}

func language(name string) string {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".java":
		return "Java"
	case ".cs":
		return "C#"
	}
	return ""
}

func readSource(path string, entry fs.DirEntry, limit int64) ([]byte, string, error) {
	info, err := entry.Info()
	if err != nil {
		return nil, "", err
	}
	if !info.Mode().IsRegular() {
		return nil, "non-regular-file", nil
	}
	if info.Size() > limit {
		return nil, "file-too-large", nil
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, "", err
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil {
		return nil, "", err
	}
	if !opened.Mode().IsRegular() || !os.SameFile(info, opened) {
		return nil, "", errors.New("file changed while opening")
	}
	content, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, "", err
	}
	if int64(len(content)) > limit {
		return nil, "file-too-large", nil
	}
	if !utf8.Valid(content) {
		return nil, "invalid-utf8", nil
	}
	if bytes.IndexByte(content, 0) >= 0 {
		return nil, "binary-source", nil
	}
	return content, "", nil
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

func invoke(parent context.Context, worker string, req request, timeout time.Duration) (json.RawMessage, bool, error) {
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	body, err := json.Marshal(req)
	if err != nil {
		return nil, false, err
	}
	stdout := &cappedBuffer{limit: 16 << 20, cancel: cancel}
	stderr := &cappedBuffer{limit: 64 << 10, cancel: cancel}
	cmd := exec.CommandContext(ctx, worker)
	cmd.Stdin = bytes.NewReader(body)
	cmd.Stdout, cmd.Stderr = stdout, stderr
	// Bound pipe cleanup if a worker unexpectedly leaves inherited descriptors open.
	cmd.WaitDelay = time.Second
	err = cmd.Run()
	if stdout.overflow || stderr.overflow {
		return nil, false, errors.New("worker-output-limit")
	}
	if ctx.Err() != nil {
		return nil, false, fmt.Errorf("worker: %w", ctx.Err())
	}
	if err != nil {
		var failure struct {
			Status string `json:"status"`
			Error  struct {
				Code    string `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
		}
		if json.Unmarshal(stdout.buf.Bytes(), &failure) == nil && failure.Status == "error" && failure.Error.Code != "" {
			return nil, false, fmt.Errorf("worker: %s: %s", boundedMessage(failure.Error.Code, 128), boundedMessage(failure.Error.Message, 2048))
		}
		return nil, false, fmt.Errorf("worker: %w; stderr: %s", err, strings.TrimSpace(stderr.buf.String()))
	}
	result := bytes.TrimSpace(stdout.buf.Bytes())
	var envelope struct {
		Status       string          `json:"status"`
		Path         string          `json:"path"`
		Language     string          `json:"language"`
		SourceBytes  *int            `json:"source_bytes"`
		ParseCount   *int            `json:"parse_count"`
		Observations json.RawMessage `json:"observations"`
		Metrics      json.RawMessage `json:"metrics"`
		Provenance   struct {
			BCA string `json:"bca"`
		} `json:"provenance"`
	}
	if len(result) == 0 || result[0] != '{' || json.Unmarshal(result, &envelope) != nil || (envelope.Status != "complete" && envelope.Status != "partial") {
		return nil, false, errors.New("worker returned an invalid result object")
	}
	if envelope.Path != req.Path || envelope.Language != req.Language || envelope.SourceBytes == nil || *envelope.SourceBytes != len(req.Source) || envelope.ParseCount == nil || *envelope.ParseCount != 1 {
		return nil, false, errors.New("worker result does not match the submitted file or single-parse contract")
	}
	validObject := func(v json.RawMessage) bool { v = bytes.TrimSpace(v); return len(v) > 0 && v[0] == '{' }
	if strings.TrimSpace(envelope.Provenance.BCA) == "" || (req.Mode != "metrics" && !validObject(envelope.Observations)) || (req.Mode == "metrics" && len(envelope.Observations) != 0) || (req.Mode != "structure" && !validObject(envelope.Metrics)) || (req.Mode == "structure" && len(envelope.Metrics) != 0) {
		return nil, false, errors.New("worker result does not match requested analysis mode or provenance contract")
	}
	return json.RawMessage(result), envelope.Status == "partial", nil
}

func boundedMessage(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	return strings.ToValidUTF8(s[:limit], "") + "…"
}
