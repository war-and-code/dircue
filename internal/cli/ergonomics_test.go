package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/war-and-code/dircue/pkg/planning"
)

func TestErgonomicErrorsTeachWithoutExecutingGuesses(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"--jsno"}, "did you mean --json?"},
		{[]string{"--jason"}, "did you mean --json?"},
		{[]string{"analyze", "structure", "--structural-wroker", "must-not-execute"}, "did you mean --structural-worker?"},
		{[]string{"analyze", "declaration"}, "dircue analyze declarations --json"},
		{[]string{"analyze"}, "dircue analyze discovery --json"},
		{[]string{"plan"}, "dircue plan report.json --module declarations --json"},
		{[]string{"compare"}, "dircue compare base.json head.json --json"},
		{[]string{"analyze", "structure"}, "--structural-worker"},
		{[]string{"analyze", "structure", "--structural-max-file-bytes", "0"}, "--structural-max-file-bytes must be between"},
		{[]string{"analyze", "structure", "--structural-timeout", "0s"}, "--structural-timeout must be positive"},
	} {
		out, stderr, err := invoke(tc.args...)
		if err == nil || !strings.Contains(err.Error(), tc.want) || out != "" || stderr != "" {
			t.Fatalf("%v: stdout=%q stderr=%q err=%v", tc.args, out, stderr, err)
		}
	}
}

func TestFlagDiagnosticsDoNotReflectValuesOrTerminalControls(t *testing.T) {
	for _, args := range [][]string{
		{"--jsno=private-value"}, {"--workers", "private-value"}, {"--=private-value"},
		{"--odd\x1b[31m\u202e"}, {"--" + strings.Repeat("z", 10000)},
	} {
		out, _, err := invoke(args...)
		if err == nil || out != "" {
			t.Fatalf("%v: %q %v", args, out, err)
		}
		message := err.Error()
		if strings.Contains(message, "private-value") || strings.ContainsAny(message, "\x1b\u202e\n\r") || len(message) > 1200 {
			t.Fatalf("unsafe diagnostic: %q", message)
		}
	}
	out, _, err := invoke("--totally-unrelated")
	if err == nil || out != "" || strings.Contains(err.Error(), "did you mean") {
		t.Fatalf("unrelated guess: %s %v", out, err)
	}
	if nearbyName("fot", []string{"foo", "fob"}) != "" {
		t.Fatal("ambiguous suggestion")
	}
}

func TestCommandHintsPreserveExplicitAndExistingPaths(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	for _, token := range []string{"languages", "analyse"} {
		_, _, err := invoke(token)
		if err == nil || !strings.Contains(err.Error(), "if you intended") {
			t.Fatalf("missing %s: %v", token, err)
		}
		for _, args := range [][]string{{"./" + token}, {"--", token}, {filepath.Join(root, token)}} {
			_, _, err = invoke(args...)
			if err == nil || strings.Contains(err.Error(), "if you intended") {
				t.Fatalf("explicit %v: %v", args, err)
			}
		}
	}
	for _, token := range []string{"languages", "analyse", "plan", "capabilities", "compare", "map", "-odd"} {
		if err := os.Mkdir(token, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(token, "main.go"), []byte("package main\n"), 0600); err != nil {
			t.Fatal(err)
		}
		for _, args := range [][]string{{"--json", "--", token}, {"./" + token, "--json"}} {
			out, stderr, err := invoke(args...)
			if err != nil || stderr != "" || !strings.Contains(out, `"Go"`) {
				t.Fatalf("%v: %q %q %v", args, out, stderr, err)
			}
		}
	}
	out, _, err := invoke("languages", "--json")
	if err != nil || !strings.Contains(out, `"Go"`) {
		t.Fatalf("existing bare path: %q %v", out, err)
	}
}

func TestSavedReportHelpHasApplicableFlagsAndWorkflowExamples(t *testing.T) {
	for _, cmd := range []string{"plan", "compare", "capabilities"} {
		out, stderr, err := invoke(cmd, "--help")
		if err != nil || stderr != "" || !strings.Contains(out, "--json") || !strings.Contains(out, "Examples:") {
			t.Fatalf("%s: %s %s %v", cmd, out, stderr, err)
		}
		if strings.Contains(out, "--workers") || strings.Contains(out, "--source string") {
			t.Fatalf("inapplicable flags advertised: %s", out)
		}
	}
	out, _, err := invoke("--help")
	if err != nil || !strings.Contains(out, "--workers") || !strings.Contains(out, "handled errors exit 1") {
		t.Fatalf("root help: %s %v", out, err)
	}
	out, _, err = invoke("plan", "--help")
	if err != nil || !strings.Contains(out, "source-structure") || !strings.Contains(out, "--input structural-worker") {
		t.Fatalf("plan vocabulary: %s %v", out, err)
	}
}

func TestPlanningErrorsRetainSentinelsAndNameInvalidSelection(t *testing.T) {
	name := savedPlanningProfile(t)
	for _, tc := range []struct {
		args     []string
		want     string
		sentinel error
	}{
		{[]string{"--module", "declaration"}, "--module declarations", planning.ErrInvalid},
		{[]string{"--question", "source-structur"}, "--question source-structure", planning.ErrInvalid},
		{[]string{"--module", "structure", "--input", "structural-wroker"}, "--input structural-worker", planning.ErrInvalid},
		{[]string{"--module", "metrics", "--project", "app.csproj"}, "--project requires", planning.ErrInvalid},
		{[]string{"--module", "focus", "--project", "a.csproj", "--project", "b.csproj"}, "one primary project", planning.ErrLimit},
	} {
		out, _, err := invoke(append([]string{"plan", name}, tc.args...)...)
		if !errors.Is(err, tc.sentinel) || out != "" || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("%v: %q %v", tc.args, out, err)
		}
	}
}

type ergonomicFailWriter struct{}

func (ergonomicFailWriter) Write([]byte) (int, error) {
	return 0, errors.New("deliberate writer failure")
}

func TestPlainPlanShowsInertJSONArgvAndPropagatesWriterFailure(t *testing.T) {
	name := savedPlanningProfile(t)
	out, _, err := invoke("plan", name, "--module", "metrics")
	if err != nil || !strings.Contains(out, "Inert argv (not executable): [") || !strings.Contains(out, "Revalidation required:") || strings.Contains(out, "/untrusted/report/root") {
		t.Fatalf("%s %v", out, err)
	}
	var stderr bytes.Buffer
	if err := Execute(t.Context(), []string{"plan", name, "--module", "metrics"}, ergonomicFailWriter{}, &stderr); err == nil {
		t.Fatal("writer error lost")
	}
	argument := "app\u0085\u009b\u202e\U000e0001.csproj"
	plan := &planning.Report{Steps: []planning.Step{{Command: planning.Command{Argv: []string{"dircue", argument}}}}}
	var rendered bytes.Buffer
	if err := writePlan(&rendered, plan); err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(rendered.String(), "\n") {
		if strings.Contains(line, "Inert argv (not executable): ") {
			encoded := strings.SplitN(line, ": ", 2)[1]
			var argv []string
			if strings.ContainsAny(encoded, "\u0085\u009b\u202e\U000e0001") || json.Unmarshal([]byte(encoded), &argv) != nil || argv[1] != argument {
				t.Fatalf("unsafe/non-JSON argv: %s", encoded)
			}
		}
	}
}
