//go:build windows

package treehash

import (
	"context"
	"os"
	"slices"
	"testing"
)

// On Windows a Git-scoped digest can identify the content but cannot be
// shown to equal a POSIX checkout's tree, so it must not claim complete.
func TestWindowsGitDigestIsPartial(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(dir+`\a.txt`, []byte("a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	res, err := Compute(context.Background(), root, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != StatusPartial || !slices.Contains(res.Reasons, ReasonWindowsSemantics) {
		t.Fatalf("status %s reasons %v, want partial with %s", res.Status, res.Reasons, ReasonWindowsSemantics)
	}
}
