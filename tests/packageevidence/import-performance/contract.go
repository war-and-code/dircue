//go:build ignore

// Profiling helper invoked explicitly with go build contract.go. It imports an
// existing report without running a scanner or traversing a repository.
package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"runtime"
	"sync"
	"time"

	"dircue/pkg/packageevidence"
)

type checkedContext struct {
	context.Context
	limit, checks int
	done          chan struct{}
	once          sync.Once
}

func (c *checkedContext) Done() <-chan struct{} { return c.done }
func (c *checkedContext) Err() error {
	c.checks++
	if c.limit >= 0 && c.checks > c.limit {
		c.once.Do(func() { close(c.done) })
		return context.Canceled
	}
	return nil
}

type failingReader struct {
	reader    *bytes.Reader
	remaining int64
}

func (r *failingReader) Read(p []byte) (int, error) {
	if r.remaining <= 0 {
		return 0, io.ErrUnexpectedEOF
	}
	if int64(len(p)) > r.remaining {
		p = p[:r.remaining]
	}
	n, err := r.reader.Read(p)
	r.remaining -= int64(n)
	return n, err
}
func fail(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func hash(raw []byte) string { d := sha256.Sum256(raw); return hex.EncodeToString(d[:]) }
func main() {
	path := flag.String("report", "", "Existing report path")
	limitsJSON := flag.String("limits", "{}", "Import Limits JSON")
	cancelChecks := flag.Int("cancel-checks", -1, "Cancel after this many context checks; -1 disables")
	readBytes := flag.Int64("reader-error-after", -1, "Reader failure after this many bytes; -1 disables")
	save := flag.String("save-output", "", "Optional exact successful report JSON path")
	flag.Parse()
	var limits packageevidence.Limits
	fail(json.Unmarshal([]byte(*limitsJSON), &limits))
	data, err := os.ReadFile(*path)
	fail(err)
	var reader io.Reader = bytes.NewReader(data)
	if *readBytes >= 0 {
		reader = &failingReader{bytes.NewReader(data), *readBytes}
	}
	ctx := &checkedContext{Context: context.Background(), limit: *cancelChecks, done: make(chan struct{})}
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	start := time.Now()
	report, err := packageevidence.Import(ctx, reader, packageevidence.Options{Limits: limits})
	elapsed := time.Since(start)
	runtime.ReadMemStats(&after)
	identity, text := "", ""
	if err != nil {
		text = err.Error()
		switch {
		case errors.Is(err, packageevidence.ErrInvalid):
			identity = "invalid"
		case errors.Is(err, packageevidence.ErrLimit):
			identity = "limit"
		case errors.Is(err, packageevidence.ErrUnsupported):
			identity = "unsupported"
		case errors.Is(err, context.Canceled):
			identity = "canceled"
		case errors.Is(err, io.ErrUnexpectedEOF):
			identity = "reader_unexpected_eof"
		default:
			identity = "other"
		}
	}
	encoded, encodeErr := json.Marshal(report)
	fail(encodeErr)
	if *save != "" {
		fail(os.WriteFile(*save, encoded, 0600))
	}
	fail(json.NewEncoder(os.Stdout).Encode(map[string]any{"input_sha256": hash(data), "input_bytes": len(data), "output_sha256": hash(encoded), "output_bytes": len(encoded), "report_nil": report == nil, "error_identity": identity, "error_text": text, "context_checks": ctx.checks, "duration_ns": elapsed.Nanoseconds(), "allocated_bytes": after.TotalAlloc - before.TotalAlloc, "allocations": after.Mallocs - before.Mallocs, "heap_alloc_at_import_end": after.HeapAlloc, "gomaxprocs": runtime.GOMAXPROCS(0)}))
	runtime.KeepAlive(report)
	runtime.KeepAlive(data)
}
