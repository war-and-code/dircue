package planning_test

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/war-and-code/dircue/pkg/capabilities"
	"github.com/war-and-code/dircue/pkg/declarations"
	"github.com/war-and-code/dircue/pkg/discovery"
	"github.com/war-and-code/dircue/pkg/focus"
	"github.com/war-and-code/dircue/pkg/planning"
	"github.com/war-and-code/dircue/pkg/profile"
	"github.com/war-and-code/dircue/pkg/projects"
)

const digest = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

func baseReport() *profile.Report {
	c := discovery.New("directory", "", 100000)
	c.Add(discovery.File{Path: "huge/events.csv", Size: 1 << 30})
	c.Add(discovery.File{Path: "tiny/package.json", Size: 42})
	return &profile.Report{SchemaVersion: "1.3.0", Root: "/untrusted/$(touch pwned)", Discovery: c.Finish(), Languages: []profile.Language{}, Ecosystems: []profile.Finding{}, Frameworks: []profile.Finding{}, Layouts: []profile.Finding{}, Warnings: []profile.Warning{}}
}

func build(t *testing.T, p *profile.Report, s planning.Selection) *planning.Report {
	t.Helper()
	r, err := planning.Build(context.Background(), planning.Input{Profile: p, ReportSHA256: digest, Capabilities: capabilities.Dircue("test"), Selection: s})
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestTinyProjectSignalSurvivesLargeDataAndRootIsNeverArgv(t *testing.T) {
	r := build(t, baseReport(), planning.Selection{Modules: []string{"declarations"}})
	if len(r.Steps) != 1 || r.Steps[0].Scope.CandidateFiles != 1 || r.Steps[0].Scope.CandidateBytes != 42 || !slicesContains(r.Steps[0].Scope.CandidatePaths, "tiny/package.json") {
		t.Fatalf("tiny candidate buried: %+v", r.Steps)
	}
	encoded, _ := json.Marshal(r.Steps[0].Command.Argv)
	if strings.Contains(string(encoded), "/untrusted") || r.Steps[0].Command.Executable || !reflect.DeepEqual(r.Steps[0].Command.Argv[len(r.Steps[0].Command.Argv)-2:], []string{"--", "{source}"}) {
		t.Fatalf("unsafe argv: %+v", r.Steps[0].Command)
	}
}

func TestProjectsSourcePinsTreeAndProjectTraversalIsRejected(t *testing.T) {
	p := baseReport()
	p.Discovery = nil
	p.Projects = &projects.Report{Status: "complete", Source: "git", Tree: "0123456789012345678901234567890123456789", Projects: []projects.Project{}, Configurations: []projects.Configuration{}, Composition: []projects.Role{}, Diagnostics: []projects.Diagnostic{}}
	r := build(t, p, planning.Selection{Modules: []string{"metrics"}})
	if r.Identity.Source.Status != "consistent" || !slicesContains(r.Steps[0].Command.Argv, "--tree") || slicesContains(r.Steps[0].Command.Argv, "--rev") {
		t.Fatalf("projects source not retained: %+v", r)
	}
	for _, project := range []string{"..", "../a.csproj", "a/../b.csproj", "-dash.csproj"} {
		_, err := planning.Build(t.Context(), planning.Input{Profile: baseReport(), ReportSHA256: digest, Capabilities: capabilities.Dircue("test"), Selection: planning.Selection{Modules: []string{"focus"}, Projects: []string{project}}})
		if err != planning.ErrInvalid {
			t.Fatalf("accepted project %q: %v", project, err)
		}
	}
}

func TestStructureNeverProbesWorkerAvailability(t *testing.T) {
	r := build(t, baseReport(), planning.Selection{Modules: []string{"structure"}})
	if r.Decisions[0].Applicability != "blocked" || !slicesContains(r.Decisions[0].UnresolvedInputs, "structural-worker") {
		t.Fatalf("worker auto-assumed: %+v", r.Decisions[0])
	}
	r = build(t, baseReport(), planning.Selection{Modules: []string{"structure"}, Inputs: []string{"structural-worker"}})
	if r.Decisions[0].Applicability != "unknown" || slicesContains(r.Decisions[0].UnresolvedInputs, "structural-worker") {
		t.Fatalf("caller input ignored: %+v", r.Decisions[0])
	}
}

func TestMissingOrPartialInventoryNeverBecomesSafeSkip(t *testing.T) {
	p := baseReport()
	p.Discovery = nil
	r := build(t, p, planning.Selection{Modules: []string{"formats"}})
	if r.Decisions[0].Applicability != "unknown" || r.Steps[0].Scope.EvidenceStatus != "unavailable" {
		t.Fatalf("absence overclaimed: %+v", r)
	}
	p = baseReport()
	p.Discovery.OmittedCandidates["manifest"] = 2
	p.Discovery.Status = "partial"
	r = build(t, p, planning.Selection{Modules: []string{"declarations"}})
	if r.Steps[0].Scope.EvidenceStatus != "partial" {
		t.Fatalf("partial scope hidden: %+v", r.Steps[0].Scope)
	}
}

func TestDataOnlyInventoryMakesFormatsApplicableWithoutInventedPaths(t *testing.T) {
	r := build(t, baseReport(), planning.Selection{Modules: []string{"formats"}})
	if r.Decisions[0].Applicability != "proposed" || r.Steps[0].Scope.CandidateFiles != 1 || r.Steps[0].Scope.CandidateBytes != 1<<30 {
		t.Fatalf("data evidence ignored: %+v", r)
	}
	if len(r.Steps[0].Scope.CandidatePaths) != 0 {
		t.Fatalf("invented data paths: %+v", r.Steps[0].Scope.CandidatePaths)
	}
}

func TestDeterministicDedupedRequestsAndCancellation(t *testing.T) {
	s := planning.Selection{Modules: []string{"metrics", "declarations", "metrics"}, Questions: []string{"project-declarations"}}
	a, b := build(t, baseReport(), s), build(t, baseReport(), s)
	if !reflect.DeepEqual(a, b) || len(a.Decisions) != 2 || a.Decisions[0].Module != "declarations" {
		t.Fatalf("nondeterministic plan: %+v", a.Decisions)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := planning.Build(ctx, planning.Input{Profile: baseReport(), ReportSHA256: digest, Capabilities: capabilities.Dircue("test"), Selection: s}); err != context.Canceled {
		t.Fatalf("cancellation: %v", err)
	}
}

func TestInvalidCapabilityAndRequestBounds(t *testing.T) {
	d := capabilities.Dircue("test")
	d.Modules = nil
	if _, err := planning.Build(context.Background(), planning.Input{Profile: baseReport(), ReportSHA256: digest, Capabilities: d, Selection: planning.Selection{Modules: []string{"metrics"}}}); err == nil {
		t.Fatal("accepted missing registry")
	}
	values := make([]string, planning.MaxRequests+1)
	for i := range values {
		values[i] = "metrics"
	}
	if _, err := planning.Build(context.Background(), planning.Input{Profile: baseReport(), ReportSHA256: digest, Capabilities: capabilities.Dircue("test"), Selection: planning.Selection{Modules: values}}); err != planning.ErrLimit {
		t.Fatalf("request cap: %v", err)
	}
}

func TestRejectsIgnoredScopeAndInputs(t *testing.T) {
	for _, selection := range []planning.Selection{{Modules: []string{"metrics"}, Projects: []string{"a.csproj"}}, {Modules: []string{"metrics"}, Inputs: []string{"structural-worker"}}, {Modules: []string{"structure"}, Inputs: []string{"structural-wroker"}}} {
		if _, err := planning.Build(context.Background(), planning.Input{Profile: baseReport(), ReportSHA256: digest, Capabilities: capabilities.Dircue("test"), Selection: selection}); err != planning.ErrInvalid {
			t.Fatalf("accepted ignored input %+v: %v", selection, err)
		}
	}
}

func TestUnavailableSourceQualifiesRetainedReuse(t *testing.T) {
	p := &profile.Report{SchemaVersion: "1.3.0", Metrics: &profile.MetricsReport{Status: "complete", Languages: []profile.LanguageMetrics{}, Directories: []profile.DirectoryMetrics{}, Skipped: []profile.MetricSkip{}}}
	r := build(t, p, planning.Selection{Modules: []string{"metrics"}})
	if r.Decisions[0].Applicability != "already_present" || !strings.Contains(strings.Join(r.Decisions[0].Reasons, " "), "provenance is unavailable") {
		t.Fatalf("unqualified reuse: %+v", r.Decisions[0])
	}
}

func TestPartialRetainedModuleAndDifferentFocusScopeAreNotDeduped(t *testing.T) {
	p := baseReport()
	p.Discovery.Status = "partial"
	r := build(t, p, planning.Selection{Modules: []string{"discovery"}})
	if r.Decisions[0].Applicability != "retained_partial" || len(r.Steps) != 1 {
		t.Fatalf("partial evidence suppressed: %+v", r)
	}
	p = baseReport()
	p.Declarations = &declarations.Report{Status: "complete", Projects: []declarations.Project{{ID: "a.csproj", Kind: "dotnet"}, {ID: "b.csproj", Kind: "dotnet"}}}
	p.Focus = &focus.Report{Status: "complete", PrimaryProject: &focus.Project{ID: "a.csproj"}, Related: []focus.RelatedPopulation{}}
	r = build(t, p, planning.Selection{Modules: []string{"focus"}, Projects: []string{"a.csproj"}})
	if r.Decisions[0].Applicability != "already_present" || len(r.Steps) != 0 {
		t.Fatalf("exact focus not reused: %+v", r)
	}
	r = build(t, p, planning.Selection{Modules: []string{"focus"}, Projects: []string{"b.csproj"}})
	if r.Decisions[0].Applicability != "retained_partial" || len(r.Steps) != 1 {
		t.Fatalf("different focus wrongly reused: %+v", r)
	}
}

func TestConflictingSourceIdentityBlocksAndCostOverflowFails(t *testing.T) {
	p := baseReport()
	p.Metrics = &profile.MetricsReport{Status: "complete", Source: "git", Tree: "abc", Languages: []profile.LanguageMetrics{}, Directories: []profile.DirectoryMetrics{}, Skipped: []profile.MetricSkip{}}
	r := build(t, p, planning.Selection{Modules: []string{"availability"}})
	if r.Identity.Source.Status != "conflict" || r.Decisions[0].Applicability != "blocked" {
		t.Fatalf("source conflict hidden: %+v", r)
	}
	p = baseReport()
	p.Languages = []profile.Language{{Name: "Go", Bytes: 1 << 62, FileCount: 1}, {Name: "Python", Bytes: 1 << 62, FileCount: 1}}
	if _, err := planning.Build(context.Background(), planning.Input{Profile: p, ReportSHA256: digest, Capabilities: capabilities.Dircue("test"), Selection: planning.Selection{Modules: []string{"metrics"}}}); err != planning.ErrLimit {
		t.Fatalf("overflow accepted: %v", err)
	}
}

func slicesContains(v []string, want string) bool {
	for _, x := range v {
		if x == want {
			return true
		}
	}
	return false
}

func TestLongDeclaredRootIsEvidenceNotRequestKey(t *testing.T) {
	p := baseReport()
	p.Root = "/" + strings.Repeat("nested/", 100)
	r := build(t, p, planning.Selection{Modules: []string{"declarations"}})
	if r.Identity.DeclaredRoot != p.Root {
		t.Fatal("declared root changed")
	}
	p.Root = strings.Repeat("x", planning.MaxReportedRootBytes+1)
	if _, err := planning.Build(t.Context(), planning.Input{Profile: p, ReportSHA256: digest, Capabilities: capabilities.Dircue("test"), Selection: planning.Selection{Modules: []string{"declarations"}}}); err == nil {
		t.Fatal("unbounded root accepted")
	}
}

func TestFocusCandidateEvidenceExcludesUnselectedProjects(t *testing.T) {
	p := baseReport()
	r := build(t, p, planning.Selection{Modules: []string{"focus"}, Projects: []string{"other/pyproject.toml"}})
	if len(r.Steps) != 1 || len(r.Steps[0].Scope.CandidatePaths) != 0 || r.Steps[0].Scope.CandidateBytes != 0 {
		t.Fatal("unselected manifest was attributed to selected project scope")
	}
}
