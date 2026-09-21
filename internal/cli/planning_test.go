package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"dircue/pkg/capabilities"
	"dircue/pkg/planning"
	"dircue/pkg/profile"
	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
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

func TestCapabilityRegistryMatchesAnalyzeCommandSurfaceWithoutProbingTools(t *testing.T) {
	for _, module := range capabilities.Dircue("test").Modules {
		var out, errOut bytes.Buffer
		if err := Execute(t.Context(), []string{"analyze", module.ID, "--help"}, &out, &errOut); err != nil {
			t.Fatalf("registered module %q is not an analyze command: %v", module.ID, err)
		}
		help := out.String()
		for _, flag := range []string{"--source", "--tree"} {
			if !strings.Contains(help, flag) {
				t.Errorf("registered module %q lacks planner flag %s", module.ID, flag)
			}
		}
		if module.ID == "focus" && !strings.Contains(help, "--project") {
			t.Errorf("registered focus module lacks --project")
		}
		if module.ID == "structure" && !strings.Contains(help, "--structural-worker") {
			t.Errorf("registered structure module lacks --structural-worker")
		}
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

func TestGitProjectsReportPlanExecutesAgainstPinnedTree(t *testing.T) {
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
	if _, err = wt.Add("main.go"); err != nil {
		t.Fatal(err)
	}
	if _, err = wt.Commit("fixture", &git.CommitOptions{Author: &object.Signature{Name: "Fixture", Email: "fixture@example.invalid", When: time.Unix(1700000000, 0)}}); err != nil {
		t.Fatal(err)
	}

	var first, errOut bytes.Buffer
	if err = Execute(t.Context(), []string{"analyze", "all", "--projects", "--source", "git", "--json", root}, &first, &errOut); err != nil {
		t.Fatal(err)
	}
	var initial profile.Report
	if err = json.Unmarshal(first.Bytes(), &initial); err != nil {
		t.Fatal(err)
	}
	if initial.Projects == nil || initial.Projects.Tree == "" {
		t.Fatalf("projects source missing: %+v", initial.Projects)
	}
	reportPath := filepath.Join(t.TempDir(), "report.json")
	if err = os.WriteFile(reportPath, first.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}

	var planned bytes.Buffer
	if err = Execute(t.Context(), []string{"plan", reportPath, "--module", "metrics", "--json"}, &planned, &errOut); err != nil {
		t.Fatal(err)
	}
	var plan planning.Report
	if err = json.Unmarshal(planned.Bytes(), &plan); err != nil {
		t.Fatal(err)
	}
	if len(plan.Steps) != 1 {
		t.Fatalf("plan: %+v", plan)
	}
	argv := append([]string(nil), plan.Steps[0].Command.Argv[1:]...)
	for i := range argv {
		if argv[i] == "{source}" {
			argv[i] = root
		}
	}
	var actual bytes.Buffer
	if err = Execute(t.Context(), argv, &actual, &errOut); err != nil {
		t.Fatalf("emitted argv failed: %v argv=%v stderr=%s", err, argv, errOut.String())
	}
	var rerun profile.Report
	if err = json.Unmarshal(actual.Bytes(), &rerun); err != nil {
		t.Fatal(err)
	}
	if rerun.Metrics == nil || rerun.Metrics.Tree != initial.Projects.Tree {
		t.Fatalf("tree changed: projects=%s metrics=%+v", initial.Projects.Tree, rerun.Metrics)
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
	if err = Execute(t.Context(), []string{"plan", name, "--module", "focus", "--project", "a.csproj", "--project", "b.csproj"}, &out, &errOut); !errors.Is(err, planning.ErrLimit) {
		t.Fatalf("multiple primary selectors: %v", err)
	}
}

func TestPlanRejectsOptionsUnusedBySelectedModule(t *testing.T) {
	name := savedPlanningProfile(t)
	for _, args := range [][]string{{"plan", name, "--module", "metrics", "--project", "a.csproj"}, {"plan", name, "--module", "metrics", "--input", "structural-worker"}, {"plan", name, "--module", "structure", "--input", "structural-wroker"}} {
		var out, errOut bytes.Buffer
		if err := Execute(t.Context(), args, &out, &errOut); !errors.Is(err, planning.ErrInvalid) {
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
