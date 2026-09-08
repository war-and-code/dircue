//go:build linux || darwin

package scanner

import (
	"context"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestFIFOSkippedAndOpenIsNonblocking(t *testing.T) {
	dir := t.TempDir()
	if err := syscall.Mkfifo(filepath.Join(dir, "pipe.go"), 0600); err != nil {
		t.Fatal(err)
	}
	report, err := Scan(context.Background(), dir, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if report.Summary.SkippedFiles != 1 || report.Summary.AnalyzedFiles != 0 {
		t.Fatalf("FIFO not skipped: %+v", report.Summary)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	done := make(chan error, 1)
	go func() {
		file, err := openRegular(root, "pipe.go")
		if file != nil {
			file.Close()
		}
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("opening FIFO blocked")
	}
}
