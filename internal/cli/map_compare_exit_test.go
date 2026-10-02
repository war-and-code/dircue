package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/war-and-code/dircue/pkg/mapdiff"
	"github.com/war-and-code/dircue/pkg/mapdoc"
)

func TestMapCompareOptInOutcomes(t *testing.T) {
	baseDoc := mapComparisonFixture("before", "aaa", true)
	base := writeComparisonMapFixture(t, "base.json", baseDoc)
	changed := writeComparisonMapFixture(t, "changed.json", mapComparisonFixture("after", "bbb", true))
	partialDoc := mapComparisonFixture("before", "bbb", false)
	partialDoc.Nodes = nil
	partial := writeComparisonMapFixture(t, "partial.json", partialDoc)
	unknownDoc := baseDoc
	unknownDoc.Source = mapdoc.Source{Mode: "directory"}
	unknownDoc.Coverage = append([]mapdoc.QuestionCoverage(nil), baseDoc.Coverage...)
	unknownDoc.Coverage[1].Coverage = mapdoc.Coverage{Status: mapdoc.CoverageUnknown, Reasons: []string{"source_digest_disabled"}}
	unknown := writeComparisonMapFixture(t, "unknown.json", unknownDoc)
	downgradedDoc := baseDoc
	downgradedDoc.Coverage = append([]mapdoc.QuestionCoverage(nil), baseDoc.Coverage...)
	for i := range downgradedDoc.Coverage {
		if downgradedDoc.Coverage[i].Question == "components" {
			downgradedDoc.Coverage[i].Coverage = mapdoc.Coverage{Status: mapdoc.CoveragePartial, Reasons: []string{"bounded_declaration_catalog"}}
		}
	}
	downgraded := writeComparisonMapFixture(t, "downgraded.json", downgradedDoc)
	partialBindingDoc := unknownDoc
	partialBindingDoc.Source.Digest = &mapdoc.Digest{Algorithm: "git-sha1", Scope: "gitignore_filtered", Normalization: "git_normalized", Value: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}
	partialBindingDoc.Coverage = append([]mapdoc.QuestionCoverage(nil), unknownDoc.Coverage...)
	for i := range partialBindingDoc.Coverage {
		if partialBindingDoc.Coverage[i].Question == "source_binding" {
			partialBindingDoc.Coverage[i].Coverage = mapdoc.Coverage{Status: mapdoc.CoveragePartial, Reasons: []string{"windows_checkout_semantics"}}
		}
	}
	partialBinding := writeComparisonMapFixture(t, "partial-binding.json", partialBindingDoc)
	for _, tt := range []struct {
		name, head string
		flags      []string
		code       int
	}{
		{"default changed", changed, nil, 0},
		{"default partial", partial, nil, 0},
		{"unchanged", base, []string{"--exit-code"}, 0},
		{"changed", changed, []string{"--exit-code"}, 1},
		{"partial", partial, []string{"--exit-code"}, 2},
		{"partial allowed", partial, []string{"--exit-code", "--on-uncertain=allow"}, 0},
		{"unknown binding", unknown, []string{"--exit-code"}, 2},
		{"coverage downgrade", downgraded, []string{"--exit-code"}, 2},
		{"coverage downgrade allowed", downgraded, []string{"--exit-code", "--on-uncertain=allow"}, 0},
		{"coverage downgrade default", downgraded, nil, 0},
		{"partial binding with digest", partialBinding, []string{"--exit-code"}, 2},
	} {
		t.Run(tt.name, func(t *testing.T) {
			for _, alias := range []string{"dircue", "dirq"} {
				args := append([]string{"map", "compare", "--json"}, tt.flags...)
				args = append(args, base, tt.head)
				var out, stderr bytes.Buffer
				err := ExecuteAs(context.Background(), alias, args, &out, &stderr)
				var outcome *comparisonExit
				code := 0
				if err != nil {
					if !errors.As(err, &outcome) {
						t.Fatalf("unexpected diagnostic: %v", err)
					}
					code = outcome.code
				}
				if code != tt.code || stderr.Len() != 0 {
					t.Fatalf("code=%d stderr=%q", code, stderr.String())
				}
				var report mapdiff.Report
				if err := json.Unmarshal(out.Bytes(), &report); err != nil {
					t.Fatalf("outcome lacks valid report: %v", err)
				}
				if report.Kind != "map_comparison" {
					t.Fatalf("wrong report: %+v", report)
				}
			}
		})
	}
	for _, args := range [][]string{
		{"--on-uncertain=allow"}, {"--exit-code", "--on-uncertain=bogus"},
	} {
		args = append(append([]string{"map", "compare", "--json"}, args...), base, changed)
		out, _, err := invoke(args...)
		var outcome *comparisonExit
		if err == nil || errors.As(err, &outcome) || out != "" {
			t.Fatalf("invalid policy accepted: %q %v", out, err)
		}
	}
}

func TestEqualPartialBindingsRemainUncertainDespiteEqualDigests(t *testing.T) {
	doc := mapComparisonFixture("before", "aaa", true)
	doc.Source = mapdoc.Source{Mode: "directory", Digest: &mapdoc.Digest{Algorithm: "git-sha1", Scope: "gitignore_filtered", Normalization: "git_normalized", Value: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}}
	for i := range doc.Coverage {
		if doc.Coverage[i].Question == "source_binding" {
			doc.Coverage[i].Coverage = mapdoc.Coverage{Status: mapdoc.CoveragePartial, Reasons: []string{"windows_checkout_semantics"}}
		}
	}
	file := writeComparisonMapFixture(t, "partial.json", doc)
	defaultJSON, _, err := invoke("map", "compare", "--json", file, file)
	if err != nil {
		t.Fatal(err)
	}
	flagJSON, _, err := invoke("map", "compare", "--json", "--exit-code", file, file)
	var outcome *comparisonExit
	if !errors.As(err, &outcome) || outcome.code != 2 || flagJSON != defaultJSON {
		t.Fatalf("partial binding accepted or report changed: %v", err)
	}
}

type failedComparisonWriter struct{}

func (failedComparisonWriter) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }

func TestMapCompareOutputErrorPrecedesOutcome(t *testing.T) {
	base := writeComparisonMapFixture(t, "base.json", mapComparisonFixture("before", "aaa", true))
	head := writeComparisonMapFixture(t, "head.json", mapComparisonFixture("after", "bbb", true))
	for _, format := range []string{"text", "markdown", "json"} {
		args := []string{"map", "compare", "--exit-code"}
		if format == "json" {
			args = append(args, "--json")
		} else {
			args = append(args, "--format", format)
		}
		args = append(args, base, head)
		err := ExecuteAs(context.Background(), "dircue", args, failedComparisonWriter{}, io.Discard)
		var outcome *comparisonExit
		if !errors.Is(err, io.ErrClosedPipe) || errors.As(err, &outcome) {
			t.Fatalf("write error masked for %s: %v", format, err)
		}
	}
}

func TestMapCompareOptInErrorsPreserveCausesForBothAliases(t *testing.T) {
	base := writeComparisonMapFixture(t, "base.json", mapComparisonFixture("before", "aaa", true))
	head := writeComparisonMapFixture(t, "head.json", mapComparisonFixture("after", "bbb", true))
	for _, alias := range []string{"dircue", "dirq"} {
		err := ExecuteAs(context.Background(), alias, []string{"map", "compare", "--exit-code", "--json", base, head}, failedComparisonWriter{}, io.Discard)
		var handled *comparisonErrorExit
		if !errors.As(err, &handled) || !errors.Is(err, io.ErrClosedPipe) || handled.Unwrap() == nil {
			t.Fatalf("%s did not preserve output failure through error exit: %v", alias, err)
		}
	}
}

func TestMapCompareUncertaintyPrecedesMaterialChange(t *testing.T) {
	for _, report := range []mapdiff.Report{
		{Status: "changed", Counts: mapdiff.Counts{IndeterminateRemoval: 1}},
		{Status: "unchanged", ObserverCompatibility: "different"},
	} {
		var outcome *comparisonExit
		if !errors.As(comparisonOutcome(report, "fail"), &outcome) || outcome.code != 2 {
			t.Fatalf("uncertainty accepted: %+v", report)
		}
		allowed := comparisonOutcome(report, "allow")
		if report.Status == "changed" {
			if !errors.As(allowed, &outcome) || outcome.code != 1 {
				t.Fatal("allow hid material change")
			}
		} else if allowed != nil {
			t.Fatalf("explicit uncertainty allow failed: %v", allowed)
		}
	}
}

func TestBuiltMapComparisonExits(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "dircue")
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}
	if out, err := exec.Command("go", "build", "-o", binary, "../..").CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	base := writeComparisonMapFixture(t, "base.json", mapComparisonFixture("before", "aaa", true))
	changed := writeComparisonMapFixture(t, "head.json", mapComparisonFixture("after", "bbb", true))
	partialDoc := mapComparisonFixture("before", "bbb", false)
	partialDoc.Nodes = nil
	partial := writeComparisonMapFixture(t, "partial.json", partialDoc)
	for head, want := range map[string]int{base: 0, changed: 1, partial: 2} {
		cmd := exec.Command(binary, "map", "compare", "--exit-code", "--json", base, head)
		var stdout, stderr bytes.Buffer
		cmd.Stdout, cmd.Stderr = &stdout, &stderr
		err := cmd.Run()
		code := 0
		if err != nil {
			var e *exec.ExitError
			if !errors.As(err, &e) {
				t.Fatal(err)
			}
			code = e.ExitCode()
		}
		if code != want || stderr.Len() != 0 || !json.Valid(stdout.Bytes()) {
			t.Fatalf("code=%d want=%d out=%q stderr=%q", code, want, stdout.String(), stderr.String())
		}
	}
	invalid := filepath.Join(t.TempDir(), "invalid.json")
	if err := os.WriteFile(invalid, []byte("{"), 0600); err != nil {
		t.Fatal(err)
	}
	missing := filepath.Join(t.TempDir(), "missing.json")
	aliases := []string{binary}
	if runtime.GOOS != "windows" {
		alias := filepath.Join(t.TempDir(), "dirq")
		if err := os.Symlink(binary, alias); err != nil {
			t.Fatal(err)
		}
		aliases = append(aliases, alias)
	}
	for _, alias := range aliases {
		for _, tc := range []struct {
			name string
			args []string
			code int
		}{
			{"missing baseline", []string{"map", "compare", "--exit-code", "--json", missing, changed}, 3},
			{"invalid JSON", []string{"map", "compare", "--exit-code", "--json", invalid, changed}, 3},
			{"bad option", []string{"map", "compare", "--exit-code", "--not-a-flag", base, changed}, 3},
			{"bad option before opt in", []string{"map", "compare", "--not-a-flag", "--exit-code", missing, missing}, 3},
			{"bad exit-code value", []string{"map", "compare", "--exit-code=invalid", base, changed}, 3},
			{"invalid repeated value still selects error contract", []string{"map", "compare", "--exit-code=false", "--exit-code=invalid", base, changed}, 3},
			{"bad arguments", []string{"map", "compare", "--exit-code", base}, 3},
			{"last repeated flag disables errors contract", []string{"map", "compare", "--exit-code", "--exit-code=false", "--not-a-flag", base, changed}, 1},
			{"last repeated flag enables errors contract", []string{"map", "compare", "--exit-code=false", "--exit-code", "--not-a-flag", base, changed}, 3},
			{"parent persistent flag before command", []string{"map", "--json", "compare", "--exit-code", "--not-a-flag", base, changed}, 3},
			{"command words inside flag value do not opt in", []string{"map", "settings", "--set", "map compare", "--exit-code"}, 1},
			{"legacy error code", []string{"map", "compare", "--json", missing, changed}, 1},
		} {
			t.Run(filepath.Base(alias)+"/"+tc.name, func(t *testing.T) {
				cmd := exec.Command(alias, tc.args...)
				var stdout, stderr bytes.Buffer
				cmd.Stdout, cmd.Stderr = &stdout, &stderr
				err := cmd.Run()
				code := 0
				if err != nil {
					var e *exec.ExitError
					if !errors.As(err, &e) {
						t.Fatal(err)
					}
					code = e.ExitCode()
				}
				if code != tc.code || stderr.Len() == 0 || stdout.Len() != 0 {
					t.Fatalf("code=%d want=%d stdout=%q stderr=%q", code, tc.code, stdout.String(), stderr.String())
				}
			})
		}
	}
	if _, err := os.Stat("/dev/full"); err == nil {
		full, err := os.OpenFile("/dev/full", os.O_WRONLY, 0)
		if err != nil {
			t.Fatal(err)
		}
		cmd := exec.Command(binary, "map", "compare", "--exit-code", "--json", base, changed)
		cmd.Stdout = full
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		err = cmd.Run()
		_ = full.Close()
		var exit *exec.ExitError
		if !errors.As(err, &exit) || exit.ExitCode() != 3 || stderr.Len() == 0 {
			t.Fatalf("output error should exit 3 with diagnostic: err=%v stderr=%q", err, stderr.String())
		}
	}
}
