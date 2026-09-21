package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"dircue/pkg/capabilities"
	"dircue/pkg/planning"
	"dircue/pkg/profile"
)

func savedPlanningProfile(t *testing.T) string {
	t.Helper()
	p := profile.Report{SchemaVersion: "1.0.0", Root: "/untrusted/report/root", Languages: []profile.Language{}, Ecosystems: []profile.Finding{}, Frameworks: []profile.Finding{}, Layouts: []profile.Finding{}, Warnings: []profile.Warning{}}
	b, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	name := filepath.Join(t.TempDir(), "report.json")
	if err = os.WriteFile(name, b, 0600); err != nil {
		t.Fatal(err)
	}
	return name
}

func TestCapabilitiesJSONCommand(t *testing.T) {
	var out, errOut bytes.Buffer
	if err := Execute(t.Context(), []string{"capabilities", "--json"}, &out, &errOut); err != nil {
		t.Fatal(err)
	}
	var d capabilities.Descriptor
	if err := json.Unmarshal(out.Bytes(), &d); err != nil {
		t.Fatal(err)
	}
	if err := d.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestPlanJSONIsInertAndRejectsAnalysisFlags(t *testing.T) {
	name := savedPlanningProfile(t)
	var out, errOut bytes.Buffer
	if err := Execute(t.Context(), []string{"plan", name, "--module", "metrics", "--json"}, &out, &errOut); err != nil {
		t.Fatal(err)
	}
	var r planning.Report
	if err := json.Unmarshal(out.Bytes(), &r); err != nil {
		t.Fatal(err)
	}
	if len(r.Steps) != 1 || r.Steps[0].Command.Executable || strings.Contains(strings.Join(r.Steps[0].Command.Argv, "\x00"), "/untrusted/report/root") {
		t.Fatalf("unsafe plan: %+v", r)
	}
	out.Reset()
	if err := Execute(t.Context(), []string{"plan", name, "--module", "metrics", "--source", "directory"}, &out, &errOut); err == nil || !strings.Contains(err.Error(), "does not apply") {
		t.Fatalf("analysis flag accepted: %v", err)
	}
}

func TestPlanProjectFlagPreservesCommaAndBoundsPrimary(t *testing.T) {
	name := savedPlanningProfile(t)
	var out, errOut bytes.Buffer
	err := Execute(t.Context(), []string{"plan", name, "--module", "focus", "--project", "a,b/project.csproj", "--json"}, &out, &errOut)
	if err != nil {
		t.Fatal(err)
	}
	var r planning.Report
	if err = json.Unmarshal(out.Bytes(), &r); err != nil {
		t.Fatal(err)
	}
	if len(r.Steps) != 1 || !slicesContainsCLI(r.Steps[0].Command.Argv, "a,b/project.csproj") {
		t.Fatalf("comma selector changed: %+v", r.Steps)
	}
	if err = Execute(t.Context(), []string{"plan", name, "--module", "focus", "--project", "a.csproj", "--project", "b.csproj"}, &out, &errOut); err != planning.ErrLimit {
		t.Fatalf("multiple primary selectors: %v", err)
	}
}

func TestPlanRejectsOptionsUnusedBySelectedModule(t *testing.T) {
	name := savedPlanningProfile(t)
	for _, args := range [][]string{{"plan", name, "--module", "metrics", "--project", "a.csproj"}, {"plan", name, "--module", "metrics", "--input", "structural-worker"}, {"plan", name, "--module", "structure", "--input", "structural-wroker"}} {
		var out, errOut bytes.Buffer
		if err := Execute(t.Context(), args, &out, &errOut); err != planning.ErrInvalid {
			t.Fatalf("accepted unused option %v: %v", args, err)
		}
	}
}

func slicesContainsCLI(values []string, want string) bool {
	for _, v := range values {
		if v == want {
			return true
		}
	}
	return false
}
