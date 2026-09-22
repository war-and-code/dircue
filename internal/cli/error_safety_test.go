package cli

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"unicode"

	git "github.com/go-git/go-git/v5"
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
