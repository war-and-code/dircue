package scanner

import (
	"os"
	"path/filepath"
	"testing"
)

// A file observed as regular by the directory walk may be replaced before its
// worker opens it. Every platform must still reject a replacement symlink,
// including a symlink whose target is another regular file inside the root.
func TestReadBoundedSizeRejectsSymlinkToRegularFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "target.txt"), []byte("secret"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("target.txt", filepath.Join(dir, "candidate.txt")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()

	if _, _, _, err := readBoundedSize(root, "candidate.txt", 1<<10); err == nil {
		t.Fatal("replacement symlink to regular file was read")
	} else if !isRecoverableFileError(err) {
		t.Fatalf("expected recoverable per-file error, got %T: %v", err, err)
	}
}
