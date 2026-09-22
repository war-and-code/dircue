//go:build linux || darwin

package scanner

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// TestReadBoundedSizeAssumeRegularRejectsNonRegular protects the TOCTOU defence
// for the per-file worker read path. readBoundedSizeAssumeRegular skips the
// pre-open Lstat that readBoundedSize performs, so it must still refuse a
// non-regular final component via openRegular's O_NOFOLLOW/O_NONBLOCK flags and
// its post-open fstat.
func TestReadBoundedSizeAssumeRegularRejectsNonRegular(t *testing.T) {
	dir := t.TempDir()
	if err := syscall.Mkfifo(filepath.Join(dir, "pipe.txt"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("pipe.txt", filepath.Join(dir, "link.txt")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if err := os.Mkdir(filepath.Join(dir, "subdir"), 0700); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()

	for _, name := range []string{"pipe.txt", "link.txt", "subdir"} {
		name := name
		t.Run(name, func(t *testing.T) {
			done := make(chan struct{})
			var readErr error
			go func() {
				defer close(done)
				_, _, _, readErr = readBoundedSizeAssumeRegular(root, name, 1<<10)
			}()
			select {
			case <-done:
			case <-time.After(2 * time.Second):
				t.Fatal("readBoundedSizeAssumeRegular blocked; O_NONBLOCK defence lost")
			}
			if readErr == nil {
				t.Fatalf("expected non-regular refusal for %s, got nil", name)
			}
			if !isRecoverableFileError(readErr) {
				t.Fatalf("expected recoverable per-file error, got %T: %v", readErr, readErr)
			}
		})
	}
}
