package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode"

	git "github.com/war-and-code/dircue/third_party/go-git"
	"github.com/war-and-code/dircue/third_party/go-git/plumbing/object"
)

func assertSafeCLIError(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("expected an error")
	}
	for _, r := range err.Error() {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			t.Fatalf("unsafe terminal character %U in %q", r, err)
		}
	}
}

func TestExecuteEscapesTerminalControlsInSourceErrors(t *testing.T) {
	unsafe := "missing\x1b[31m\n\u0085\u202e"
	missing := t.TempDir() + string(os.PathSeparator) + unsafe
	for _, args := range [][]string{
		{missing},
		{"analyze", "discovery", missing},
		{"--source", "git", missing},
	} {
		out, stderr, err := invoke(args...)
		if out != "" || stderr != "" {
			t.Fatalf("%v printed before main handled the error: stdout=%q stderr=%q", args, out, stderr)
		}
		assertSafeCLIError(t, err)
		for _, want := range []string{"missing", `\x1b`, `\n`, `\u0085`, `\u202e`} {
			if !strings.Contains(err.Error(), want) {
				t.Fatalf("%v: escaped error %q does not retain %q", args, err, want)
			}
		}
	}
}

func TestExecuteEscapesControlsFromBrokenGitRevision(t *testing.T) {
	root := t.TempDir()
	if _, err := git.PlainInit(root, false); err != nil {
		t.Fatal(err)
	}
	out, stderr, err := invoke("--source", "git", "--rev", "bad\x1b[31m\u0085\u202e", root)
	if out != "" || stderr != "" {
		t.Fatalf("printed before main handled the error: stdout=%q stderr=%q", out, stderr)
	}
	assertSafeCLIError(t, err)
	if !strings.Contains(err.Error(), "resolve Git revision") || !strings.Contains(err.Error(), `\u202e`) {
		t.Fatalf("Git context was lost: %q", err)
	}
}

func TestSafeCLIErrorPreservesCauseAndOrdinaryErrors(t *testing.T) {
	sentinel := errors.New("sentinel")
	pathError := &os.PathError{Op: "stat", Path: "bad\x1b[31m\u0085\u202e", Err: sentinel}
	original := fmt.Errorf("inspect source: %w", pathError)
	safe := safeCLIError(original)
	assertSafeCLIError(t, safe)
	if safe == original || !errors.Is(safe, sentinel) {
		t.Fatalf("unsafe error was not wrapped with its cause: %v", safe)
	}
	var gotPath *os.PathError
	if !errors.As(safe, &gotPath) || gotPath != pathError {
		t.Fatalf("typed cause was not preserved: %v", safe)
	}

	ordinary := errors.New("ordinary error: café and \\x1b are literal text")
	if got := safeCLIError(ordinary); got != ordinary || got.Error() != ordinary.Error() {
		t.Fatalf("ordinary error changed: got %q, want %q", got, ordinary)
	}
	if safeCLIError(nil) != nil {
		t.Fatal("nil error changed")
	}
}

func TestSafeCLIErrorDoesNotReintroducePrivateFlagValues(t *testing.T) {
	for _, args := range [][]string{{"--jsno=private-value"}, {"--workers=private-value"}, {"-zprivate-value"}} {
		out, stderr, err := invoke(args...)
		if err == nil || out != "" || stderr != "" || strings.Contains(err.Error(), "private-value") {
			t.Fatalf("%v: stdout=%q stderr=%q err=%v", args, out, stderr, err)
		}
		assertSafeCLIError(t, err)
	}
}

func TestExplainSemanticFlagErrorsMatchPeerWithoutReflectingValues(t *testing.T) {
	root := t.TempDir()
	private := "private-value-" + strings.Repeat("x", 5000)
	for _, tc := range []struct {
		flag  string
		value string
		want  string
	}{
		{"--on-error", private, "--on-error must be fail or continue"},
		{"--source", private, "--source must be auto, git, or directory"},
		{"--tree", "", "--tree requires a full Git tree object ID"},
	} {
		peerOut, peerStderr, peerErr := invoke("analyze", "discovery", tc.flag, tc.value, root)
		explainOut, explainStderr, explainErr := invoke("analyze", "explain", tc.flag, tc.value, "--file", "main.go", root)
		if peerErr == nil || explainErr == nil {
			t.Fatalf("%s accepted an invalid value: peer=%v explain=%v", tc.flag, peerErr, explainErr)
		}
		if peerOut != "" || peerStderr != "" || explainOut != "" || explainStderr != "" {
			t.Fatalf("%s wrote output before the caller handled the error", tc.flag)
		}
		if peerErr.Error() != tc.want || explainErr.Error() != tc.want {
			t.Fatalf("%s diagnostic mismatch: peer=%q explain=%q", tc.flag, peerErr, explainErr)
		}
		if strings.Contains(peerErr.Error(), "private-value") || strings.Contains(explainErr.Error(), "private-value") {
			t.Fatalf("%s reflected the assigned value", tc.flag)
		}
	}
}

func TestExplainAllowsOmittedTreeWithDefaultErrorPolicy(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(root+string(os.PathSeparator)+"main.go", []byte("package main\n"), 0600); err != nil {
		t.Fatal(err)
	}
	out, stderr, err := invoke("analyze", "explain", "--source", "directory", "--file", "main.go", "--json", root)
	if err != nil || stderr != "" || !strings.Contains(out, `"explanation"`) {
		t.Fatalf("valid explain default: stdout=%q stderr=%q err=%v", out, stderr, err)
	}
}

// TestTreeFlagReportsObjectKindWhenNotATree verifies that a caller who
// copy-pastes a commit or blob SHA into --tree receives a specific "object is
// a commit/blob, not a tree" diagnostic instead of the misleading
// "object not found" wording. Stderr stays empty and the exit path is the
// ordinary handled-error path.
func TestTreeFlagReportsObjectKindWhenNotATree(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n"), 0600); err != nil {
		t.Fatal(err)
	}
	repo, err := git.PlainInit(root, false)
	if err != nil {
		t.Fatal(err)
	}
	wt, err := repo.Worktree()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := wt.Add("main.go"); err != nil {
		t.Fatal(err)
	}
	commitHash, err := wt.Commit("fixture", &git.CommitOptions{Author: &object.Signature{Name: "Fixture", Email: "fixture@example.invalid", When: time.Unix(1700000000, 0)}})
	if err != nil {
		t.Fatal(err)
	}
	commit, err := repo.CommitObject(commitHash)
	if err != nil {
		t.Fatal(err)
	}
	tree, err := commit.Tree()
	if err != nil {
		t.Fatal(err)
	}
	if len(tree.Entries) == 0 {
		t.Fatal("empty tree")
	}
	blobHash := tree.Entries[0].Hash.String()
	for _, tc := range []struct {
		name string
		hash string
		want string
	}{
		{"commit_hash", commitHash.String(), "object is a commit, not a tree"},
		{"blob_hash", blobHash, "object is a blob, not a tree"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, stderr, err := invoke("--json", "--tree", tc.hash, root)
			if err == nil {
				t.Fatalf("accepted non-tree --tree %s: stdout=%q", tc.hash, out)
			}
			if out != "" || stderr != "" {
				t.Fatalf("wrote before caller handled the error: stdout=%q stderr=%q", out, stderr)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("diagnostic %q missing want %q", err.Error(), tc.want)
			}
			assertSafeCLIError(t, err)
		})
	}
}

func TestSavedExplainStillRejectsScanFlagsBeforeOpeningReport(t *testing.T) {
	private := "private-value-" + strings.Repeat("x", 5000)
	for _, tc := range []struct {
		flag  string
		value string
	}{
		{"--on-error", private},
		{"--tree", ""},
	} {
		out, stderr, err := invoke("analyze", "explain", "--report", "missing.json", "--file", "main.go", tc.flag, tc.value)
		want := tc.flag + " does not apply to saved-report explanations"
		if err == nil || err.Error() != want || out != "" || stderr != "" {
			t.Fatalf("%s saved-report ordering: stdout=%q stderr=%q err=%v", tc.flag, out, stderr, err)
		}
	}
}
