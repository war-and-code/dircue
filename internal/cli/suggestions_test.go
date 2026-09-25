package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestNearbyNameEditDistance covers the widened matcher: names shorter than six
// characters still require a single edit, while longer names accept a second
// edit whenever one candidate is strictly closer than every other. The rule
// exists to catch double transpositions like `declraetions` while keeping
// short misspellings such as `plann` -> `plan` and `hlp` -> `help` behind the
// single-edit gate. Hyphen-prefixed tokens (`-h`, `--v`) never reach
// nearbyName because isTypoCandidate rejects them at the caller, and
// nearbyName itself refuses ties and inputs over 64 bytes.
func TestNearbyNameEditDistance(t *testing.T) {
	subcommands := []string{"analyze", "capabilities", "compare", "help", "map", "plan"}
	analyzers := []string{"availability", "declarations", "discovery", "environments", "focus", "formats", "metrics", "structure"}
	enumSource := []string{"auto", "git", "directory"}
	enumOnError := []string{"fail", "continue"}
	for _, tc := range []struct {
		name       string
		input      string
		candidates []string
		want       string
	}{
		{"exact", "plan", subcommands, "plan"},
		{"one_edit_subcommand", "plann", subcommands, "plan"},
		{"one_edit_map_subcommand", "maps", subcommands, "map"},
		{"one_edit_source", "sourc", enumSource, ""},
		{"typo_source_delete", "gt", enumSource, "git"},
		{"typo_source_substitution", "atuo", enumSource, "auto"},
		{"typo_source_directory_transposition", "diretcory", enumSource, "directory"},
		{"typo_on_error_transposition", "contineu", enumOnError, "continue"},
		{"typo_on_error_missing_letter", "cntinue", enumOnError, "continue"},
		{"two_edit_analyzer_double_transposition", "declraetions", analyzers, "declarations"},
		{"two_edit_analyzer_missing_letter", "enviroments", analyzers, "environments"},
		{"two_edit_short_input_refused", "helpo", subcommands, "help"},
		// A 3-char input against a 4-char candidate is still a single-edit
		// insertion; the widened matcher does not artificially raise the
		// minimum input length beyond nonempty + <=64 bytes. If callers need
		// to protect a meaning boundary they gate the token upstream (see
		// isTypoCandidate for the hyphen-prefix filter).
		{"one_edit_very_short_accepted", "hlp", subcommands, "help"},
		{"never_maps_across_meaning", "-h", subcommands, ""},
		{"never_maps_hyphen", "--v", subcommands, ""},
		{"empty_input", "", subcommands, ""},
		{"oversize_input_refused", strings.Repeat("z", 100), subcommands, ""},
		{"ambiguous_tie_refused", "foo", []string{"fob", "fot", "for"}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := nearbyName(tc.input, tc.candidates); got != tc.want {
				t.Fatalf("nearbyName(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

// TestEnumValueErrorsIncludeNearestChoiceHint verifies that finite-choice
// flags append a "did you mean X?" hint when the user's value is close to a
// valid choice. Non-close inputs and empty inputs receive the plain error.
func TestEnumValueErrorsIncludeNearestChoiceHint(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"--source", "drc", root}, "--source must be auto, git, or directory"},
		{[]string{"--source", "atuo", root}, "--source must be auto, git, or directory; did you mean auto?"},
		{[]string{"--on-error", "contineu", root}, "--on-error must be fail or continue; did you mean continue?"},
		{[]string{"analyze", "metrics", "--metrics-scope", "sourc", "--json", root}, "--metrics-scope must be source or text; did you mean source?"},
	} {
		out, stderr, err := invoke(tc.args...)
		if err == nil || err.Error() != tc.want || out != "" || stderr != "" {
			t.Fatalf("%v: stdout=%q stderr=%q err=%v want=%q", tc.args, out, stderr, err, tc.want)
		}
	}
}

// TestUnknownFirstTokenSuggestsSubcommand covers the two-argument entry point:
// a mistyped subcommand plus a source path used to be swallowed by the
// pathArgs "expected at most one directory path" error with no suggestion.
// Extending pathArgs preserves the original message and exit code while adding
// a one-line hint whenever the first positional resembles a known subcommand.
func TestUnknownFirstTokenSuggestsSubcommand(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)
	// Single-argument case: the stat-failure hint already handled 1-edit typos
	// before this change; the widened distance-2 matcher covers longer names.
	for _, tc := range []struct {
		token string
		want  string
	}{
		{"plann", "if you intended the command, use: dircue plan --help"},
		{"capabilties", "if you intended the command, use: dircue capabilities --help"},
		{"maps", "if you intended the command, use: dircue map --help"},
	} {
		_, _, err := invoke(tc.token)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("single-arg %q: %v", tc.token, err)
		}
	}
	// Two-argument case: pathArgs appends a suggestion when the first token
	// does not exist as a path and is close to a subcommand name.
	_, _, err := invoke("plann", ".")
	if err == nil || !strings.Contains(err.Error(), "expected at most one directory path, received 2; did you mean `dircue plan`?") {
		t.Fatalf("two-arg plann .: %v", err)
	}
	// A first token that already resolves to an existing path is treated as a
	// path and gets no subcommand hint. The pathArgs "received 2" message
	// stays byte-identical for that case.
	if err := os.Mkdir(filepath.Join(root, "otherdir"), 0700); err != nil {
		t.Fatal(err)
	}
	if _, _, err := invoke("otherdir", "."); err == nil || strings.Contains(err.Error(), "did you mean") || !strings.Contains(err.Error(), "received 2") {
		t.Fatalf("existing-path two-arg: %v", err)
	}
}

func TestMapNestedCommandTyposTeachWithoutGuessingOrBreakingPaths(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"map", "routee", "missing.json"}, "did you mean `dircue map route`?"},
		{[]string{"map", "locat", "map.json", "results.sarif"}, "did you mean `dircue map locate`?"},
	} {
		out, stderr, err := invoke(tc.args...)
		if err == nil || out != "" || stderr != "" || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("%v: stdout=%q stderr=%q err=%v", tc.args, out, stderr, err)
		}
	}
	if err := os.Mkdir(filepath.Join(root, "routee"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "routee", "main.go"), []byte("package main\n"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"map", "--source", "directory", "--json", "--", "routee"},
		{"map", "--source", "directory", "--json", "./routee"},
	} {
		out, stderr, err := invoke(args...)
		if err != nil || stderr != "" || !strings.Contains(out, `"Go"`) || strings.Contains(out, "did you mean") {
			t.Fatalf("explicit path %v: stdout=%q stderr=%q err=%v", args, out, stderr, err)
		}
	}
}

func TestMapSavedInputArgumentErrorsPointToExactHelp(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"map", "route"}, "map route requires one saved map file; see: dircue map route --help"},
		{[]string{"map", "locate"}, "map locate requires a saved map and SARIF report; see: dircue map locate --help"},
		{[]string{"map", "compare"}, "map compare requires base and head map files; see: dircue map compare --help"},
	} {
		out, stderr, err := invoke(tc.args...)
		if err == nil || out != "" || stderr != "" || err.Error() != tc.want {
			t.Fatalf("%v: stdout=%q stderr=%q err=%v", tc.args, out, stderr, err)
		}
	}
}
