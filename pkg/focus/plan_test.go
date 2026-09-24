package focus

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"testing"

	"github.com/war-and-code/dircue/pkg/declarations"
)

func project(id, kind, name string, parsed bool, refs ...declarations.Reference) ProjectRecord {
	return ProjectRecord{Parsed: parsed, Project: declarations.Project{ID: id, Root: pathDir(id), Kind: kind, Name: name, Requirements: []declarations.Requirement{}, References: refs, Interfaces: []declarations.Interface{}}}
}

func pathDir(name string) string {
	for i := len(name) - 1; i >= 0; i-- {
		if name[i] == '/' {
			return name[:i]
		}
	}
	return "."
}

func completeInput(files []File, projects ...ProjectRecord) Input {
	return Input{Source: "directory", Inventory: files, InventoryComplete: true, DeclarationsComplete: true, Projects: projects}
}

func mustBuild(t *testing.T, in Input, request Request, limits Limits) *Result {
	t.Helper()
	result, err := Build(context.Background(), in, request, limits)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func TestNestedInvalidAndUnsupportedManifestsRemainOwnershipBarriers(t *testing.T) {
	input := completeInput([]File{
		{Path: "root/root.csproj", Size: 10},
		{Path: "root/Program.cs", Size: 20},
		{Path: "root/bad/bad.csproj", Size: 5},
		{Path: "root/bad/Hidden.cs", Size: 30},
		{Path: "root/web/package.json", Size: 5},
		{Path: "root/web/index.ts", Size: 40},
	},
		project("root/root.csproj", "dotnet", "", true),
		project("root/bad/bad.csproj", "dotnet-configuration", "", false),
		project("root/web/package.json", "npm", "web", true),
	)
	result := mustBuild(t, input, Request{Project: "root/root.csproj"}, Limits{})
	if got := result.PrimaryPathList(); !slices.Equal(got, []string{"root/Program.cs", "root/root.csproj"}) {
		t.Fatalf("primary paths = %#v", got)
	}
	if result.Report.Status != "partial" || result.Report.Coverage.UnresolvedFiles != 4 {
		t.Fatalf("barrier coverage = %#v", result.Report.Coverage)
	}
	reasons := map[string]bool{}
	for _, b := range result.Report.Boundaries {
		reasons[b.Reason] = true
	}
	if !reasons["ineligible-project-boundary"] {
		t.Fatalf("boundaries = %#v", result.Report.Boundaries)
	}
}

func TestNonregularProjectManifestCandidateBlocksAncestor(t *testing.T) {
	input := completeInput([]File{{Path: "app/app.csproj", Size: 1}, {Path: "app/Own.cs", Size: 1}, {Path: "app/nested/Hidden.cs", Size: 1}}, project("app/app.csproj", "dotnet", "", true))
	input.OwnershipBarriers = []string{"app/nested/pyproject.toml"}
	result := mustBuild(t, input, Request{Project: "app/app.csproj"}, Limits{})
	if _, found := result.PrimaryPaths["app/nested/Hidden.cs"]; found {
		t.Fatal("nonregular nested manifest did not block ancestor selection")
	}
	if result.Report.Coverage.OwnershipBarriers != 1 || result.SelectionComplete {
		t.Fatalf("barrier coverage = %#v", result.Report.Coverage)
	}
}

func TestSameRootProjectsAreAmbiguous(t *testing.T) {
	input := completeInput([]File{
		{Path: "app/app.csproj", Size: 1},
		{Path: "app/pyproject.toml", Size: 1},
		{Path: "app/main.cs", Size: 1},
	},
		project("app/app.csproj", "dotnet", "", true),
		project("app/pyproject.toml", "python", "app", true),
	)
	result := mustBuild(t, input, Request{Project: "app/app.csproj"}, Limits{})
	if len(result.PrimaryPaths) != 0 || result.Report.Coverage.AmbiguousFiles != 3 {
		t.Fatalf("same-root ownership was not ambiguous: %#v", result.Report)
	}
}

func TestContainmentDoesNotClaimLinkedCompilerInput(t *testing.T) {
	input := completeInput([]File{
		{Path: "apps/a/a.csproj", Size: 1},
		{Path: "apps/a/Own.cs", Size: 1},
		{Path: "shared/Linked.cs", Size: 1},
	}, project("apps/a/a.csproj", "dotnet", "", true, declarations.Reference{
		Kind: "compile", Value: "../../shared/Linked.cs", Target: "shared/Linked.cs", State: "resolved", TargetStatus: "present", Evidence: "apps/a/a.csproj",
	}))
	result := mustBuild(t, input, Request{Project: "apps/a/a.csproj"}, Limits{})
	if _, found := result.PrimaryPaths["shared/Linked.cs"]; found {
		t.Fatal("linked file was treated as containment-owned source")
	}
	if len(result.Report.Relations) != 0 {
		t.Fatalf("unsupported compiler input relation was promoted: %#v", result.Report.Relations)
	}
}

func TestDotnetContextSeparatesCandidatesAndConditionalImports(t *testing.T) {
	input := completeInput([]File{
		{Path: "Directory.Build.props", Size: 1},
		{Path: "packages.config", Size: 1},
		{Path: "src/Directory.Build.targets", Size: 1},
		{Path: "src/app/app.csproj", Size: 1},
		{Path: "src/app/packages.config", Size: 1},
		{Path: "src/app/extra.props", Size: 1},
	},
		project("Directory.Build.props", "dotnet-configuration", "", true),
		project("packages.config", "dotnet-configuration", "", true),
		project("src/Directory.Build.targets", "dotnet-configuration", "", false),
		project("src/app/packages.config", "dotnet-configuration", "", true),
		project("src/app/app.csproj", "dotnet", "", true, declarations.Reference{
			Kind: "import", Value: "extra.props", Target: "src/app/extra.props", State: "conditional", TargetStatus: "present", Evidence: "src/app/app.csproj", Condition: "condition-present; expression-withheld",
		}),
	)
	result := mustBuild(t, input, Request{Project: "src/app/app.csproj"}, Limits{})
	contexts := map[string]Context{}
	for _, c := range result.Report.Context {
		contexts[c.Path] = c
	}
	if c := contexts["Directory.Build.props"]; c.Applicability != "candidate" || !c.Parsed || c.State != "parsed" {
		t.Fatalf("parsed ancestor candidate = %#v", c)
	}
	if c := contexts["src/Directory.Build.targets"]; c.Applicability != "candidate" || c.Parsed || c.State != "unparsed" {
		t.Fatalf("unparsed ancestor candidate = %#v", c)
	}
	if c := contexts["src/app/extra.props"]; c.Applicability != "conditional" || c.Basis != "import" || c.Parsed {
		t.Fatalf("declared import = %#v", c)
	}
	if _, found := contexts["packages.config"]; found {
		t.Fatal("ancestor packages.config was treated as inherited context")
	}
	if c := contexts["src/app/packages.config"]; c.Basis != "supported-project-local-configuration-name" {
		t.Fatalf("project-local packages.config = %#v", c)
	}
}

func TestPythonMemberGetsQualifiedWorkspaceAndLockContext(t *testing.T) {
	memberRef := declarations.Reference{Kind: "uv-workspace-member", Value: "declared-member", Target: "packages/api/pyproject.toml", State: "resolved", TargetStatus: "present", Evidence: "pyproject.toml"}
	lockRef := declarations.Reference{Kind: "uv-lockfile", Value: "presence-only", Target: "uv.lock", State: "declared", TargetStatus: "present", Evidence: "packages/api/pyproject.toml"}
	input := completeInput([]File{
		{Path: "pyproject.toml", Size: 1}, {Path: "uv.lock", Size: 1},
		{Path: "packages/api/pyproject.toml", Size: 1}, {Path: "packages/api/api.py", Size: 1},
		{Path: "packages/other/pyproject.toml", Size: 1}, {Path: "packages/other/other.py", Size: 1},
	},
		project("pyproject.toml", "python-workspace", "", true, memberRef),
		project("packages/api/pyproject.toml", "python", "api", true, lockRef),
		project("packages/other/pyproject.toml", "python", "other", true),
	)
	result := mustBuild(t, input, Request{Project: "packages/api/pyproject.toml"}, Limits{})
	if _, found := result.PrimaryPaths["packages/other/other.py"]; found {
		t.Fatal("workspace sibling source entered member population")
	}
	contexts := map[string]Context{}
	for _, c := range result.Report.Context {
		contexts[c.Path] = c
	}
	if c := contexts["pyproject.toml"]; c.Kind != "workspace-declaration" || c.Basis != "uv-workspace-member" || !c.Parsed {
		t.Fatalf("workspace parent context = %#v", c)
	}
	if c := contexts["uv.lock"]; c.Kind != "declared-configuration" || c.Basis != "uv-lockfile" {
		t.Fatalf("uv lock context = %#v", c)
	}
}

func TestIncompleteDeclarationCoverageAndUnretainedManifestBarrier(t *testing.T) {
	input := completeInput([]File{
		{Path: "root/root.csproj", Size: 1},
		{Path: "root/Own.cs", Size: 1},
		{Path: "root/crate/Cargo.toml", Size: 1},
		{Path: "root/crate/src/lib.rs", Size: 1},
	}, project("root/root.csproj", "dotnet", "", true))
	input.DeclarationsComplete = false
	input.OmittedProjects = 1
	result := mustBuild(t, input, Request{Project: "root/root.csproj"}, Limits{})
	if result.Report.Status != "partial" || result.Report.Coverage.OmittedProjects != 1 {
		t.Fatalf("declaration coverage = %#v", result.Report.Coverage)
	}
	if _, found := result.PrimaryPaths["root/crate/src/lib.rs"]; found {
		t.Fatal("unretained Cargo manifest failed to block ancestor ownership")
	}
}

func TestRelationsAreDirectAndRelatedPopulationRequiresExplicitRequest(t *testing.T) {
	aToB := declarations.Reference{Kind: "project-reference", Value: "../b/b.csproj", Target: "b/b.csproj", State: "resolved", TargetStatus: "present", Evidence: "a/a.csproj"}
	bToA := declarations.Reference{Kind: "project-reference", Value: "../a/a.csproj", Target: "a/a.csproj", State: "resolved", TargetStatus: "present", Evidence: "b/b.csproj"}
	input := completeInput([]File{
		{Path: "a/a.csproj", Size: 1}, {Path: "a/A.cs", Size: 1},
		{Path: "b/b.csproj", Size: 1}, {Path: "b/B.cs", Size: 1},
	}, project("a/a.csproj", "dotnet", "", true, aToB), project("b/b.csproj", "dotnet", "", true, bToA))

	primaryOnly := mustBuild(t, input, Request{Project: "a/a.csproj"}, Limits{})
	if _, found := primaryOnly.PrimaryPaths["b/B.cs"]; found || len(primaryOnly.RelatedPaths) != 0 {
		t.Fatal("declared relationship expanded source population")
	}
	if len(primaryOnly.Report.Relations) != 1 || primaryOnly.Report.Relations[0].Target != "b/b.csproj" {
		t.Fatalf("direct relation missing: %#v", primaryOnly.Report.Relations)
	}

	withRelated := mustBuild(t, input, Request{Project: "a/a.csproj", Related: []string{"b/b.csproj"}}, Limits{})
	if _, found := withRelated.RelatedPaths["b/b.csproj"]["b/B.cs"]; !found {
		t.Fatal("explicit related project source was not selected")
	}
	if len(withRelated.Report.Relations) != 2 {
		t.Fatalf("cycle should be retained without traversal: %#v", withRelated.Report.Relations)
	}
}

func TestQueriesAreBoundedAndReverseCandidateImpact(t *testing.T) {
	input := completeInput([]File{{Path: "Directory.Build.props", Size: 1}, {Path: "a/a.csproj", Size: 1}, {Path: "b/b.csproj", Size: 1}},
		project("Directory.Build.props", "dotnet-configuration", "", true),
		project("a/a.csproj", "dotnet", "", true), project("b/b.csproj", "dotnet", "", true))
	result := mustBuild(t, input, Request{Project: "a/a.csproj", Related: []string{"b/b.csproj"}}, Limits{})
	impact, err := result.AffectedProjects(context.Background(), "Directory.Build.props", QueryOptions{Limit: 1})
	if err != nil || impact.Status != "partial" || impact.Omitted != 1 || len(impact.Projects) != 1 {
		t.Fatalf("impact query = %#v, %v", impact, err)
	}
	query, err := result.ProjectContext(context.Background(), "a/a.csproj", QueryOptions{Limit: 1})
	if err != nil || query.Status != "partial" || query.Omitted == 0 {
		t.Fatalf("project query = %#v, %v", query, err)
	}
}

func TestAffectedByModeNeedsNoPrimaryAndDoesNotSelectSource(t *testing.T) {
	input := completeInput([]File{{Path: "Directory.Build.props", Size: 1}, {Path: "a/a.csproj", Size: 1}, {Path: "a/A.cs", Size: 1}, {Path: "b/b.csproj", Size: 1}},
		project("Directory.Build.props", "dotnet-configuration", "", true),
		project("a/a.csproj", "dotnet", "", true), project("b/b.csproj", "dotnet", "", true))
	result := mustBuild(t, input, Request{AffectedBy: "Directory.Build.props"}, Limits{})
	if result.Report.PrimaryProject != nil || len(result.PrimaryPaths) != 0 || result.Report.Scope.Role != "affected-by" {
		t.Fatalf("affected-by query created a primary selection: %#v", result.Report)
	}
	if result.Report.AffectedProjects == nil || len(result.Report.AffectedProjects.Projects) != 2 {
		t.Fatalf("affected projects = %#v", result.Report.AffectedProjects)
	}
	if _, err := Build(context.Background(), input, Request{Project: "a/a.csproj", AffectedBy: "Directory.Build.props"}, Limits{}); err == nil {
		t.Fatal("mixed project and affected-by request accepted")
	}
}

func TestDeterminismCancellationAndOutputTrimmingDoNotChangeSelection(t *testing.T) {
	files := []File{{Path: "app/app.csproj", Size: 1}}
	for i := byte('a'); i <= 'z'; i++ {
		files = append(files, File{Path: "app/" + string(i) + ".cs", Size: 1})
	}
	input := completeInput(files, project("app/app.csproj", "dotnet", "", true))
	first := mustBuild(t, input, Request{Project: "app/app.csproj"}, Limits{OutputBytes: 4096})
	slices.Reverse(input.Inventory)
	second := mustBuild(t, input, Request{Project: "app/app.csproj"}, Limits{OutputBytes: 4096})
	if !slices.Equal(first.PrimaryPathList(), second.PrimaryPathList()) || len(first.PrimaryPaths) != len(files) {
		t.Fatal("input order or output trimming changed exact selection")
	}
	encoded, err := json.Marshal(first.Report)
	if err != nil || len(encoded) > 4096 || first.Report.Coverage.OmittedOutputRecords == 0 {
		t.Fatalf("bounded report: bytes=%d omitted=%d err=%v", len(encoded), first.Report.Coverage.OmittedOutputRecords, err)
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Build(cancelled, input, Request{Project: "app/app.csproj"}, Limits{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation error = %v", err)
	}
}

func TestWorkLimitCannotProduceApparentlyCompleteSelection(t *testing.T) {
	input := completeInput([]File{{Path: "app/app.csproj", Size: 1}, {Path: "app/A.cs", Size: 1}}, project("app/app.csproj", "dotnet", "", true))
	result := mustBuild(t, input, Request{Project: "app/app.csproj"}, Limits{Work: 1})
	if result.Report.Status != "partial" || len(result.PrimaryPaths) != 0 || result.Report.Coverage.UnresolvedFiles != 2 {
		t.Fatalf("work-capped selection = %#v", result.Report)
	}
}

func TestOutputBoundTrimsAllRelatedPopulationsWithoutChangingExecutionSets(t *testing.T) {
	files := []File{{Path: "p0/p0.csproj", Size: 1}}
	records := []ProjectRecord{project("p0/p0.csproj", "dotnet", "", true)}
	related := []string{}
	for p := 1; p <= 3; p++ {
		id := fmt.Sprintf("p%d/p%d.csproj", p, p)
		files = append(files, File{Path: id, Size: 1})
		records = append(records, project(id, "dotnet", "", true))
		related = append(related, id)
		for n := 0; n < 80; n++ {
			files = append(files, File{Path: fmt.Sprintf("p%d/file-%03d.cs", p, n), Size: 1})
		}
	}
	result := mustBuild(t, completeInput(files, records...), Request{Project: "p0/p0.csproj", Related: related}, Limits{OutputBytes: 4096})
	for _, id := range related {
		if len(result.RelatedPaths[id]) != 81 {
			t.Fatalf("execution set %s has %d paths", id, len(result.RelatedPaths[id]))
		}
	}
	data, _ := json.Marshal(result.Report)
	if len(data) > 4096 || result.Report.Coverage.OmittedOutputRecords == 0 {
		t.Fatalf("related output bytes=%d report=%#v", len(data), result.Report.Coverage)
	}
}

func TestAffectedOutputTrimPreservesPrivateReverseIndex(t *testing.T) {
	files := []File{{Path: "Directory.Build.props", Size: 1}}
	records := []ProjectRecord{project("Directory.Build.props", "dotnet-configuration", "", true)}
	for n := 0; n < 200; n++ {
		id := fmt.Sprintf("p%03d/p.csproj", n)
		files = append(files, File{Path: id, Size: 1})
		records = append(records, project(id, "dotnet", "", true))
	}
	result := mustBuild(t, completeInput(files, records...), Request{AffectedBy: "Directory.Build.props"}, Limits{OutputBytes: 4096})
	data, _ := json.Marshal(result.Report)
	full, err := result.AffectedProjects(context.Background(), "Directory.Build.props", QueryOptions{Limit: 1000})
	if err != nil || len(data) > 4096 || len(full.Projects) != 200 || result.Report.Coverage.OmittedOutputRecords == 0 {
		t.Fatalf("affected trim: bytes=%d full=%d omitted=%d err=%v", len(data), len(full.Projects), result.Report.Coverage.OmittedOutputRecords, err)
	}
}

func TestRejectsInvalidAndUnsupportedSelectedRecords(t *testing.T) {
	files := []File{{Path: "bad/pyproject.toml", Size: 1}, {Path: "web/package.json", Size: 1}}
	input := completeInput(files, project("bad/pyproject.toml", "python", "", false), project("web/package.json", "npm", "web", true))
	for _, id := range []string{"bad/pyproject.toml", "web/package.json", "missing.csproj"} {
		if _, err := Build(context.Background(), input, Request{Project: id}, Limits{}); err == nil {
			t.Fatalf("selected %q unexpectedly accepted", id)
		}
	}
}
