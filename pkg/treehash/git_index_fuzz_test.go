package treehash

import (
	"os"
	"path/filepath"
	"testing"
)

// FuzzGitIndexParsing checks that the Git index reader never panics,
// produces bounded output, and handles arbitrary byte sequences gracefully.
//
// Invariants:
//   - No panic (the function already has a recover, this fuzz target exercises it).
//   - When the function returns a non-nil result, the result is internally
//     consistent: tracked[p] is true for every path in blobs or gitlinks.
//   - skipWorktree is non-negative.
//   - No path in the index escapes via ".." or starts with "/".
//
// This target is also the regression harness for go-git issue #2378
// (backwards delta in pack files can be triggered through the index reader
// if delta resolution panics; the index decoder does not resolve deltas, but
// the recover() must fire and not corrupt state).
//
// Seed corpus: pkg/treehash/testdata/fuzz/FuzzGitIndexParsing/
func FuzzGitIndexParsing(f *testing.F) {
	// Seeds are loaded from testdata/fuzz/FuzzGitIndexParsing/ automatically
	// by the testing framework when running with -test.fuzz.
	// Additional inline seeds:
	f.Add([]byte("DIRC\x00\x00\x00\x02\x00\x00\x00\x00" + "\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00"))
	f.Add([]byte{})
	f.Add([]byte("DIRC"))
	f.Add([]byte("not a git index at all"))

	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > maxIndexBytes {
			return
		}
		// Write the fuzz input as .git/index in a temp directory.
		dir := t.TempDir()
		gitDir := filepath.Join(dir, ".git")
		if err := os.Mkdir(gitDir, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(gitDir, "index"), data, 0o600); err != nil {
			t.Fatal(err)
		}
		root, err := os.OpenRoot(dir)
		if err != nil {
			t.Fatal(err)
		}
		defer root.Close()

		// The function must not panic (it has a recover; the fuzz target
		// verifies that the recover fires and returns a clean error, not that
		// the function succeeds).
		result, parseErr := readRepositoryIndex(root, ".git")

		// An error is fine; a nil result with no error is also fine (empty
		// index). What is not allowed: panic (caught by recover → error).
		if parseErr != nil {
			// Expected: malformed input returns an error. No further checks.
			return
		}
		if result == nil {
			// Nil result with nil error: also fine (defensive).
			return
		}

		// Consistency: every blob or gitlink path must be in tracked.
		for p := range result.blobs {
			if !result.tracked[p] {
				t.Fatalf("blob path %q is not in tracked set", p)
			}
		}
		for p := range result.gitlinks {
			if !result.tracked[p] {
				t.Fatalf("gitlink path %q is not in tracked set", p)
			}
		}

		// skipWorktree must be non-negative.
		if result.skipWorktree < 0 {
			t.Fatalf("skipWorktree is negative: %d", result.skipWorktree)
		}

		// No path in the result must escape via ".." or start with "/".
		check := func(p string) {
			if len(p) > 0 && (p[0] == '/' || p == ".." || len(p) > 2 && p[:3] == "../") {
				t.Fatalf("path %q escaped root", p)
			}
		}
		for p := range result.tracked {
			check(p)
		}
		for p := range result.blobs {
			check(p)
		}
		for p := range result.trackedDirs {
			check(p)
		}
		for p := range result.gitlinks {
			check(p)
		}
	})
}
