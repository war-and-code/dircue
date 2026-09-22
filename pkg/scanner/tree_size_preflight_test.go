//go:build linux || darwin

package scanner

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"dircue/pkg/profile"
)

// TestTreeSizeLimitOverridesUnreadableFile pins the contract that the
// tree-size-limit skeleton wins before content observation begins, even under
// the default fail policy with an unreadable file in the tree.
func TestTreeSizeLimitOverridesUnreadableFile(t *testing.T) {
	files := map[string]string{
		"a.go":     goSource,
		"b.py":     "print('hi')\n",
		"forbid.c": "int main(){return 0;}\n",
		"d.rs":     "fn main() {}\n",
	}
	root := fixtures(t, files)
	// Deny read on one file so any worker that tries to open it fails.
	unreadable := filepath.Join(root, "forbid.c")
	if err := os.Chmod(unreadable, 0); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(unreadable, 0644) })

	for _, workers := range []int{1, 4, 16} {
		workers := workers
		t.Run(fmt.Sprintf("workers=%d", workers), func(t *testing.T) {
			report, err := Scan(context.Background(), root, Options{
				Source:      "directory",
				Workers:     workers,
				MaxTreeSize: 2, // 4 non-dir entries > 2 → limit fires
			})
			if err != nil {
				t.Fatalf("Scan returned error despite tree-size crossing: %v", err)
			}
			if len(report.Languages) != 0 || report.Summary != (profile.Summary{}) {
				t.Fatalf("expected empty skeleton; got languages=%v summary=%+v", report.Languages, report.Summary)
			}
			if len(report.Warnings) != 1 {
				t.Fatalf("expected exactly one warning; got %d: %+v", len(report.Warnings), report.Warnings)
			}
			w := report.Warnings[0]
			if w.Code != "tree_size_limit" || w.Path != "." {
				t.Fatalf("expected tree_size_limit warning at '.', got %+v", w)
			}
			// The read error must not leak into the skeleton.
			for _, w := range report.Warnings {
				if strings.Contains(w.Message, "permission") || w.Code == "file_read_error" {
					t.Fatalf("worker read error leaked into skeleton response: %+v", w)
				}
			}
		})
	}
}

// TestReadErrorSurfacesBelowTreeSizeLimit locks in the complementary case:
// when the tree stays below MaxTreeSize, a per-file worker error under the
// default fail policy still surfaces to the caller.
func TestReadErrorSurfacesBelowTreeSizeLimit(t *testing.T) {
	files := map[string]string{
		"a.go":     goSource,
		"forbid.c": "int main(){return 0;}\n",
	}
	root := fixtures(t, files)
	unreadable := filepath.Join(root, "forbid.c")
	if err := os.Chmod(unreadable, 0); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(unreadable, 0644) })
	for _, workers := range []int{1, 4} {
		workers := workers
		t.Run(fmt.Sprintf("workers=%d", workers), func(t *testing.T) {
			_, err := Scan(context.Background(), root, Options{
				Source:      "directory",
				Workers:     workers,
				MaxTreeSize: 10, // tree well under limit
			})
			if err == nil {
				t.Fatalf("expected read error to surface below tree-size limit; got nil")
			}
			// Report the error is per-file (recoverable classification) so
			// the caller knows what kind of failure this was.
			if !isRecoverableFileError(err) && !errors.Is(err, fs.ErrPermission) && !strings.Contains(err.Error(), "permission") {
				t.Fatalf("expected recoverable/permission error; got %T: %v", err, err)
			}
		})
	}
}

// TestOnErrorContinueBelowAndAtTreeSizeLimit checks that --on-error continue
// still produces the recoverable per-file warning below the limit and still
// yields the byte-identical skeleton once the limit fires.
func TestOnErrorContinueBelowAndAtTreeSizeLimit(t *testing.T) {
	files := map[string]string{
		"a.go":     goSource,
		"forbid.c": "int main(){return 0;}\n",
		"b.py":     "print('hi')\n",
	}
	root := fixtures(t, files)
	unreadable := filepath.Join(root, "forbid.c")
	if err := os.Chmod(unreadable, 0); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(unreadable, 0644) })

	t.Run("under_limit_reports_file_read_error", func(t *testing.T) {
		report, err := Scan(context.Background(), root, Options{
			Source:      "directory",
			Workers:     4,
			MaxTreeSize: 10,
			ErrorPolicy: ErrorPolicyContinue,
		})
		if err != nil {
			t.Fatalf("continue policy should not surface read error: %v", err)
		}
		var found bool
		for _, w := range report.Warnings {
			if w.Code == "file_read_error" && w.Path == "forbid.c" {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("expected file_read_error warning; got warnings=%+v", report.Warnings)
		}
	})

	t.Run("over_limit_returns_skeleton", func(t *testing.T) {
		report, err := Scan(context.Background(), root, Options{
			Source:      "directory",
			Workers:     4,
			MaxTreeSize: 2,
			ErrorPolicy: ErrorPolicyContinue,
		})
		if err != nil {
			t.Fatalf("Scan returned error: %v", err)
		}
		if len(report.Languages) != 0 || report.Summary != (profile.Summary{}) {
			t.Fatalf("expected empty skeleton; got %+v", report)
		}
		if len(report.Warnings) != 1 || report.Warnings[0].Code != "tree_size_limit" {
			t.Fatalf("expected only tree_size_limit warning; got %+v", report.Warnings)
		}
	})
}

// TestTreeSizeLimitOverridesAttributeReadError protects the same preflight
// ordering for scanner-owned attribute reads.
func TestTreeSizeLimitOverridesAttributeReadError(t *testing.T) {
	root := fixtures(t, map[string]string{
		".gitattributes": "*.go linguist-language=Go\n",
		"a.go":           goSource,
		"b.py":           "print('hi')\n",
	})
	attributes := filepath.Join(root, ".gitattributes")
	if err := os.Chmod(attributes, 0); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(attributes, 0644) })

	report, err := Scan(context.Background(), root, Options{
		Source:      "directory",
		Workers:     4,
		MaxTreeSize: 2,
	})
	if err != nil {
		t.Fatalf("tree-size crossing must override attribute read error: %v", err)
	}
	if len(report.Warnings) != 1 || report.Warnings[0].Code != "tree_size_limit" {
		t.Fatalf("expected only tree_size_limit warning; got %+v", report.Warnings)
	}
}

func TestTreeSizePreflightDoesNotInvokeDetectors(t *testing.T) {
	root := fixtures(t, map[string]string{
		"a.go": goSource,
		"b.go": goSource,
		"c.go": goSource,
	})
	called := false
	detector := detectorFunc(func(context.Context, profile.File) ([]profile.Finding, error) {
		called = true
		return nil, nil
	})

	report, err := Scan(context.Background(), root, Options{
		Source:      "directory",
		Workers:     1,
		MaxTreeSize: 3,
		Detectors:   []profile.Detector{detector},
	})
	if err != nil {
		t.Fatalf("Scan returned error: %v", err)
	}
	if called {
		t.Fatal("detector ran before the tree-size limit was established")
	}
	if len(report.Warnings) != 1 || report.Warnings[0].Code != "tree_size_limit" {
		t.Fatalf("expected only tree_size_limit warning; got %+v", report.Warnings)
	}
}
