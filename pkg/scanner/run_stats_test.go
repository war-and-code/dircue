package scanner

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// TestRunCountersInvariantAcrossWorkerCounts verifies that the deterministic
// cost counters produce identical values regardless of worker count, for the
// same input and settings.
func TestRunCountersInvariantAcrossWorkerCounts(t *testing.T) {
	// Create a small, deterministic fixture directory.
	root := t.TempDir()
	for name, content := range map[string]string{
		"main.go":          "package main\nfunc main() {}\n",
		"lib.go":           "package main\nfunc lib() {}\n",
		"README.md":        "# Test\n",
		"data.json":        `{"key":"value"}`,
		"sub/helper.go":    "package main\nfunc helper() {}\n",
		"sub/types.go":     "package main\ntype T struct{}\n",
		"vendor/third.go":  "package third\nfunc third() {}\n",
	} {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}

	scan := func(workers int) RunCounters {
		var c RunCounters
		_, err := Scan(context.Background(), root, Options{
			Source:      "directory",
			Workers:     workers,
			RunCounters: &c,
		})
		if err != nil {
			t.Fatalf("Scan(workers=%d): %v", workers, err)
		}
		return c
	}

	baseline := scan(1)
	for _, workers := range []int{2, 4, 8} {
		got := scan(workers)
		if got.FilesContentRead.Load() != baseline.FilesContentRead.Load() {
			t.Errorf("workers=%d: FilesContentRead=%d, want %d",
				workers, got.FilesContentRead.Load(), baseline.FilesContentRead.Load())
		}
		if got.BytesRequested.Load() != baseline.BytesRequested.Load() {
			t.Errorf("workers=%d: BytesRequested=%d, want %d",
				workers, got.BytesRequested.Load(), baseline.BytesRequested.Load())
		}
	}
}

// TestRunCountersNilSafe verifies that Scan with RunCounters=nil does not panic.
func TestRunCountersNilSafe(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.go"), []byte("package a\n"), 0600); err != nil {
		t.Fatal(err)
	}
	_, err := Scan(context.Background(), root, Options{
		Source:      "directory",
		Workers:     1,
		RunCounters: nil, // must not panic
	})
	if err != nil {
		t.Fatal(err)
	}
}

// TestRunCountersPositive verifies that counters are positive for a non-empty
// directory with readable files.
func TestRunCountersPositive(t *testing.T) {
	root := t.TempDir()
	for name, content := range map[string]string{
		"a.go": "package a\n",
		"b.go": "package b\n",
	} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	var c RunCounters
	_, err := Scan(context.Background(), root, Options{
		Source:      "directory",
		Workers:     1,
		RunCounters: &c,
	})
	if err != nil {
		t.Fatal(err)
	}
	if c.FilesContentRead.Load() <= 0 {
		t.Errorf("FilesContentRead=%d; want > 0", c.FilesContentRead.Load())
	}
	if c.BytesRequested.Load() <= 0 {
		t.Errorf("BytesRequested=%d; want > 0", c.BytesRequested.Load())
	}
}
