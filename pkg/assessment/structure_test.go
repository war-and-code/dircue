package assessment

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/war-and-code/dircue/pkg/declarations"
	"github.com/war-and-code/dircue/pkg/discovery"
	"github.com/war-and-code/dircue/pkg/lockfiles"
)

func TestStructureSummarizesExplicitGroupsAndObservedDependencyGraph(t *testing.T) {
	refs := func(in ...declarations.Reference) []declarations.Reference { return in }
	project := func(id, root, kind string, references ...declarations.Reference) declarations.Project {
		return declarations.Project{ID: id, Root: root, Kind: kind, References: refs(references...), Requirements: []declarations.Requirement{}, Interfaces: []declarations.Interface{}}
	}
	projects := []declarations.Project{
		project("packages/package.json", "packages", "npm", declarations.Reference{Kind: "npm-workspace-member", Value: "a", Target: "packages/a/package.json", TargetStatus: "present", State: "resolved", Evidence: "packages/package.json"}, declarations.Reference{Kind: "npm-workspace-member", Value: "b", Target: "packages/b/package.json", TargetStatus: "present", State: "resolved", Evidence: "packages/package.json"}, declarations.Reference{Kind: "npm-workspace-member", Value: "a", Target: "packages/a/package.json", TargetStatus: "present", State: "conditional", Condition: "platform-specific", Evidence: "packages/package.json"}),
		project("packages/a/package.json", "packages/a", "npm", declarations.Reference{Kind: "npm-local-dependency", Value: "b", Target: "packages/b/package.json", TargetStatus: "present", State: "resolved", Evidence: "packages/a/package.json"}, declarations.Reference{Kind: "npm-local-dependency", Value: "b", Target: "packages/b/package.json", TargetStatus: "present", State: "resolved", Evidence: "packages/a/package.json"}, declarations.Reference{Kind: "npm-local-dependency", Value: "b", Target: "packages/b/package.json", TargetStatus: "present", State: "conditional", Condition: "os == linux", Evidence: "packages/a/package.json"}, declarations.Reference{Kind: "npm-local-dependency", Value: "gone", Target: "packages/gone/package.json", TargetStatus: "missing", State: "missing", Evidence: "packages/a/package.json"}),
		project("packages/b/package.json", "packages/b", "npm"),
		project("python/pyproject.toml", "python", "python-workspace", declarations.Reference{Kind: "uv-workspace-member", Value: "app", Target: "python/app/pyproject.toml", TargetStatus: "present", State: "resolved", Evidence: "python/pyproject.toml"}),
		project("python/app/pyproject.toml", "python/app", "python"),
		project("maven/pom.xml", "maven", "maven", declarations.Reference{Kind: "module", Value: "lib", Target: "maven/lib/pom.xml", TargetStatus: "present", State: "resolved", Evidence: "maven/pom.xml"}),
		project("maven/lib/pom.xml", "maven/lib", "maven"),
		project("go/go.work", "go", "go-workspace", declarations.Reference{Kind: "go-workspace-member", Value: ".", Target: "go/app/go.mod", TargetStatus: "present", State: "resolved", Evidence: "go/go.work"}),
		project("go/app/go.mod", "go/app", "go"),
		project("cargo/Cargo.toml", "cargo", "cargo-workspace", declarations.Reference{Kind: "cargo-workspace-member", Value: "app", Target: "cargo/app/Cargo.toml", TargetStatus: "present", State: "resolved", Evidence: "cargo/Cargo.toml"}),
		project("cargo/app/Cargo.toml", "cargo/app", "cargo"),
		project("gradle/settings.gradle", "gradle", "gradle", declarations.Reference{Kind: "gradle-module", Value: ":app", Target: "gradle/app", TargetStatus: "present", State: "resolved", Evidence: "gradle/settings.gradle"}, declarations.Reference{Kind: "gradle-module", Value: ":missing", Target: "gradle/missing", TargetStatus: "missing", State: "missing", Evidence: "gradle/settings.gradle"}),
		project("gradle/app/build.gradle", "gradle/app", "gradle"),
		project("dotnet/App.sln", "dotnet", "solution", declarations.Reference{Kind: "solution-member", Value: "App", Target: "dotnet/App.csproj", TargetStatus: "present", State: "declared", Evidence: "dotnet/App.sln"}),
		project("dotnet/App.csproj", "dotnet", "dotnet", declarations.Reference{Kind: "project-reference", Value: "Lib.csproj", Target: "dotnet/Lib.csproj", TargetStatus: "present", State: "resolved", Evidence: "dotnet/App.csproj"}),
		project("dotnet/Lib.csproj", "dotnet", "dotnet"),
	}
	report := &declarations.Report{Source: "directory", Status: "complete", Projects: projects, Diagnostics: []declarations.Diagnostic{}}
	records := make([]declarations.ProjectRecord, 0, len(projects))
	collector := New("directory", "")
	for _, p := range projects {
		records = append(records, declarations.ProjectRecord{Project: p, Parsed: true, Complete: true})
		collector.Add(discovery.File{Path: p.ID, Size: 1})
	}
	locks := &lockfiles.Report{Source: "directory", Status: "complete", Contexts: []lockfiles.Context{}}
	assessment, err := collector.Finish(Evidence{Declarations: report, Lockfiles: locks, Records: records})
	if err != nil {
		t.Fatal(err)
	}
	s := assessment.Structure
	if s == nil {
		t.Fatal("new assessments must contain structure")
	}
	if got := populationCount(s, "parsed_projects", "npm"); got != 3 {
		t.Fatalf("npm project population = %d, want 3", got)
	}
	if got := populationCount(s, "parsed_projects", "nuget"); got != 2 {
		t.Fatalf("NuGet project population = %d, want 2", got)
	}
	if got := s.WorkspaceGroupCount; got != 6 {
		t.Fatalf("workspace groups = %d, want 6", got)
	}
	var npmGroup *StructureGroup
	for i := range s.WorkspaceGroups {
		if s.WorkspaceGroups[i].ID == "packages/package.json" {
			npmGroup = &s.WorkspaceGroups[i]
		}
	}
	if npmGroup == nil || npmGroup.MemberCount != 2 || npmGroup.UnresolvedMemberCount != 1 || npmGroup.UnresolvedMembers[0].State != "conditional" {
		t.Fatalf("qualified workspace membership was counted as definite: %+v", npmGroup)
	}
	if got := s.SolutionGroupCount; got != 1 {
		t.Fatalf("solution groups = %d, want 1", got)
	}
	if len(s.SolutionGroups) != 1 || s.SolutionGroups[0].ID != "dotnet/App.sln" || s.SolutionGroups[0].MemberCount != 1 || s.SolutionGroups[0].Members[0] != "dotnet/App.csproj" {
		t.Fatalf("solution group facts: %+v", s.SolutionGroups)
	}
	if got := s.Dependencies.DefiniteEdges.Count; got != 2 {
		t.Fatalf("definite directed edges = %d, want 2 (duplicates deduplicated)", got)
	}
	if got := s.Dependencies.ConnectedGroups.Count; got != 10 {
		t.Fatalf("observed weak groups = %d, want 10", got)
	}
	if s.Dependencies.QualifiedReferenceCount == 0 {
		t.Fatal("conditional and missing references were not qualified")
	}
	// After the F1 fix, npm-local-dependency relationships are always definite
	// regardless of Condition (which npm parsers use only for section names, not
	// genuine build conditions). Only the missing-target reference is qualified.
	if !hasQualified(s.Dependencies, "npm", "npm-local-dependency", "missing") {
		t.Fatalf("qualified npm references: %+v", s.Dependencies.QualifiedReferences)
	}
	if err := ValidateReport(assessment); err != nil {
		t.Fatalf("ValidateReport: %v", err)
	}
	corruptGroupTotals := *assessment
	corruptGroupTotals.Structure = cloneStructureForTest(assessment.Structure)
	corruptGroupTotals.Structure.WorkspaceGroupCount++
	if err := ValidateReport(&corruptGroupTotals); err == nil {
		t.Fatal("corrupt workspace group total was accepted")
	}
	corruptQualifiedTotals := *assessment
	corruptQualifiedTotals.Structure = cloneStructureForTest(assessment.Structure)
	corruptQualifiedTotals.Structure.Dependencies.QualifiedReferenceCount++
	if err := ValidateReport(&corruptQualifiedTotals); err == nil {
		t.Fatal("corrupt qualified-reference total was accepted")
	}
}

func TestStructureQualifiesSelfProjectReferenceInsteadOfDroppingIt(t *testing.T) {
	p := declarations.Project{ID: "src/App.csproj", Root: "src", Kind: "dotnet", Requirements: []declarations.Requirement{}, Interfaces: []declarations.Interface{}, References: []declarations.Reference{{Kind: "project-reference", Value: "App.csproj", Target: "src/App.csproj", TargetStatus: "present", State: "resolved", Evidence: "src/App.csproj"}}}
	a := finishStructureProjects(t, []declarations.Project{p})
	if a.Structure.Dependencies.DefiniteEdges.Count != 0 {
		t.Fatalf("self-reference must not be a definite dependency edge: %+v", a.Structure.Dependencies)
	}
	if !hasQualifiedResolution(a.Structure.Dependencies, "nuget", "project-reference", "self_reference") {
		t.Fatalf("self-reference was dropped rather than qualified: %+v", a.Structure.Dependencies.QualifiedReferences)
	}
	if !coverageReason(a.Structure, "project_dependencies", "nuget", "self_reference_qualified") {
		t.Fatalf("self-reference qualification reason missing: %+v", a.Structure.Coverage)
	}
}

func TestStructureKeepsUnactivatedGoReplacementQualified(t *testing.T) {
	app := declarations.Project{ID: "app/go.mod", Root: "app", Kind: "go", Requirements: []declarations.Requirement{}, Interfaces: []declarations.Interface{}, References: []declarations.Reference{{Kind: "go-local-replacement", Value: "../lib", Target: "lib/go.mod", TargetStatus: "present", State: "declared", Condition: "example.org/lib@v1.2.3", Evidence: "app/go.mod"}}}
	lib := declarations.Project{ID: "lib/go.mod", Root: "lib", Kind: "go", Requirements: []declarations.Requirement{}, Interfaces: []declarations.Interface{}, References: []declarations.Reference{}}
	a := finishStructureProjects(t, []declarations.Project{app, lib})
	d := a.Structure.Dependencies
	if d.DefiniteEdges.Count != 0 || d.ConnectedGroups.Count != 2 || d.ConnectedGroupsWithQualified.Count != 1 {
		t.Fatalf("inactive replacement became a definite edge or was omitted from qualified connectivity: %+v", d)
	}
	if !hasQualifiedResolution(d, "go", "go-local-replacement", "present_qualified") || !coverageReason(a.Structure, "project_dependencies", "go", "go_replacement_activation_unresolved") {
		t.Fatalf("inactive replacement did not retain its qualified status and completeness reason: qualified=%+v coverage=%+v", d.QualifiedReferences, a.Structure.Coverage)
	}
	if got := coverageStatus(a.Structure, "dependency_connectivity", "go"); got != "partial" {
		t.Fatalf("connectivity coverage = %q, want partial because replacement activation is unresolved", got)
	}
}

func TestStructureMavenCoordinatesNeedExplicitUnconditionalReactorMembership(t *testing.T) {
	project := func(id, artifact string, refs ...declarations.Reference) declarations.Project {
		root := path.Dir(id)
		return declarations.Project{ID: id, Root: root, Kind: "maven", Requirements: []declarations.Requirement{{Kind: "maven-groupId", Value: "x", State: "declared", Evidence: id}, {Kind: "maven-artifactId", Value: artifact, State: "declared", Evidence: id}, {Kind: "maven-version", Value: "1", State: "declared", Evidence: id}}, References: refs, Interfaces: []declarations.Interface{}}
	}
	dependency := declarations.Requirement{Kind: "maven-dependency", Value: "x:lib:1", State: "declared", Evidence: "app/pom.xml"}
	t.Run("unrelated coordinate match stays qualified", func(t *testing.T) {
		app := project("app/pom.xml", "app")
		app.Requirements = append(app.Requirements, dependency)
		lib := project("archive/pom.xml", "lib")
		a := finishStructureProjects(t, []declarations.Project{app, lib})
		if a.Structure.Dependencies.DefiniteEdges.Count != 0 || !hasQualifiedResolution(a.Structure.Dependencies, "maven", "maven-sibling-dependency", "coordinate_match_without_declared_reactor") {
			t.Fatalf("coordinate coincidence became an edge: %+v", a.Structure.Dependencies)
		}
		if !coverageReason(a.Structure, "project_dependencies", "maven", "coordinate_match_without_declared_reactor") {
			t.Fatalf("coordinate-only qualification not disclosed: %+v", a.Structure.Coverage)
		}
	})
	t.Run("explicit reactor siblings can resolve", func(t *testing.T) {
		root := project("pom.xml", "root", declarations.Reference{Kind: "module", Value: "app", Target: "app/pom.xml", TargetStatus: "present", State: "resolved", Evidence: "pom.xml"}, declarations.Reference{Kind: "module", Value: "lib", Target: "lib/pom.xml", TargetStatus: "present", State: "resolved", Evidence: "pom.xml"})
		app := project("app/pom.xml", "app")
		app.Requirements = append(app.Requirements, dependency)
		lib := project("lib/pom.xml", "lib")
		a := finishStructureProjects(t, []declarations.Project{root, app, lib})
		if a.Structure.Dependencies.DefiniteEdges.Count != 1 || len(a.Structure.Dependencies.Edges) != 1 || a.Structure.Dependencies.Edges[0].From != "app/pom.xml" || a.Structure.Dependencies.Edges[0].To != "lib/pom.xml" {
			t.Fatalf("explicit reactor dependency not retained: %+v", a.Structure.Dependencies)
		}
	})
	t.Run("exact-case reactor dependency survives legacy matcher normalization", func(t *testing.T) {
		req := func(kind, value string) declarations.Requirement {
			return declarations.Requirement{Kind: kind, Value: value, State: "declared"}
		}
		root := declarations.Project{ID: "pom.xml", Root: ".", Kind: "maven", Requirements: []declarations.Requirement{
			req("maven-groupId", "Org.Example"), req("maven-artifactId", "root"), req("maven-version", "1"),
		}, References: []declarations.Reference{
			{Kind: "module", Target: "app/pom.xml", TargetStatus: "present", State: "resolved", Evidence: "pom.xml"},
			{Kind: "module", Target: "lib/pom.xml", TargetStatus: "present", State: "resolved", Evidence: "pom.xml"},
		}, Interfaces: []declarations.Interface{}}
		app := declarations.Project{ID: "app/pom.xml", Root: "app", Kind: "maven", Requirements: []declarations.Requirement{
			req("maven-groupId", "Org.Example"), req("maven-artifactId", "app"), req("maven-version", "1"),
			{Kind: "maven-dependency", Value: "Org.Example:Lib:1", State: "declared", Evidence: "app/pom.xml"},
		}, References: []declarations.Reference{}, Interfaces: []declarations.Interface{}}
		lib := declarations.Project{ID: "lib/pom.xml", Root: "lib", Kind: "maven", Requirements: []declarations.Requirement{
			req("maven-groupId", "Org.Example"), req("maven-artifactId", "Lib"), req("maven-version", "1"),
		}, References: []declarations.Reference{}, Interfaces: []declarations.Interface{}}
		a := finishStructureProjects(t, []declarations.Project{root, app, lib})
		found := false
		for _, edge := range a.Structure.Dependencies.Edges {
			if edge.From == app.ID && edge.To == lib.ID && edge.Kind == "maven-sibling-dependency" {
				found = true
			}
		}
		if !found {
			t.Fatalf("case-exact Maven reactor dependency was dropped: edges=%+v qualified=%+v", a.Structure.Dependencies.Edges, a.Structure.Dependencies.QualifiedReferences)
		}
	})
	t.Run("qualified versions do not produce an exact-case edge", func(t *testing.T) {
		for _, tt := range []struct {
			name, dependencyVersion, targetVersion, targetVersionState string
		}{
			{name: "conditional target version", dependencyVersion: "1", targetVersion: "1", targetVersionState: "conditional"},
			{name: "unresolved target version", dependencyVersion: "1", targetVersion: "1", targetVersionState: "unresolved"},
			{name: "range dependency version", dependencyVersion: "[1,2)", targetVersion: "1", targetVersionState: "declared"},
			{name: "interpolated dependency version", dependencyVersion: "${lib.version}", targetVersion: "1", targetVersionState: "declared"},
		} {
			t.Run(tt.name, func(t *testing.T) {
				declared := func(kind, value string) declarations.Requirement {
					return declarations.Requirement{Kind: kind, Value: value, State: "declared"}
				}
				root := declarations.Project{ID: "pom.xml", Root: ".", Kind: "maven", Requirements: []declarations.Requirement{
					declared("maven-groupId", "Org.Example"), declared("maven-artifactId", "root"), declared("maven-version", "1"),
				}, References: []declarations.Reference{
					{Kind: "module", Target: "app/pom.xml", TargetStatus: "present", State: "resolved", Evidence: "pom.xml"},
					{Kind: "module", Target: "lib/pom.xml", TargetStatus: "present", State: "resolved", Evidence: "pom.xml"},
				}}
				app := declarations.Project{ID: "app/pom.xml", Root: "app", Kind: "maven", Requirements: []declarations.Requirement{
					declared("maven-groupId", "Org.Example"), declared("maven-artifactId", "app"), declared("maven-version", "1"),
					{Kind: "maven-dependency", Value: "Org.Example:Lib:" + tt.dependencyVersion, State: "declared", Evidence: "app/pom.xml"},
				}}
				lib := declarations.Project{ID: "lib/pom.xml", Root: "lib", Kind: "maven", Requirements: []declarations.Requirement{
					declared("maven-groupId", "Org.Example"), declared("maven-artifactId", "Lib"),
					{Kind: "maven-version", Value: tt.targetVersion, State: tt.targetVersionState},
				}}
				a := finishStructureProjects(t, []declarations.Project{root, app, lib})
				for _, edge := range a.Structure.Dependencies.Edges {
					if edge.From == app.ID && edge.To == lib.ID && edge.Kind == "maven-sibling-dependency" {
						t.Fatalf("qualified coordinate/version evidence was promoted to an edge: %+v", edge)
					}
				}
				if !coverageReason(a.Structure, "project_dependencies", "maven", "maven_dependency_version_unresolved") {
					t.Fatalf("uncertain version did not qualify Maven dependency coverage: %+v", a.Structure.Coverage)
				}
			})
		}
	})
	t.Run("conditional reactor membership is insufficient", func(t *testing.T) {
		root := project("pom.xml", "root", declarations.Reference{Kind: "module", Value: "app", Target: "app/pom.xml", TargetStatus: "present", State: "conditional", Condition: "profile:test", Evidence: "pom.xml"}, declarations.Reference{Kind: "module", Value: "lib", Target: "lib/pom.xml", TargetStatus: "present", State: "resolved", Evidence: "pom.xml"})
		app := project("app/pom.xml", "app")
		app.Requirements = append(app.Requirements, dependency)
		lib := project("lib/pom.xml", "lib")
		a := finishStructureProjects(t, []declarations.Project{root, app, lib})
		if a.Structure.Dependencies.DefiniteEdges.Count != 0 || !hasQualifiedResolution(a.Structure.Dependencies, "maven", "maven-sibling-dependency", "coordinate_match_without_declared_reactor") {
			t.Fatalf("conditional reactor membership was treated as definite: %+v", a.Structure.Dependencies)
		}
	})
	t.Run("reactor coordinate case mismatch stays qualified", func(t *testing.T) {
		root := project("pom.xml", "root", declarations.Reference{Kind: "module", Value: "app", Target: "app/pom.xml", TargetStatus: "present", State: "resolved", Evidence: "pom.xml"}, declarations.Reference{Kind: "module", Value: "lib", Target: "lib/pom.xml", TargetStatus: "present", State: "resolved", Evidence: "pom.xml"})
		app := project("app/pom.xml", "app")
		app.Requirements = append(app.Requirements, declarations.Requirement{Kind: "maven-dependency", Value: "X:Lib:1", State: "declared", Evidence: "app/pom.xml"})
		lib := project("lib/pom.xml", "lib")
		a := finishStructureProjects(t, []declarations.Project{root, app, lib})
		if a.Structure.Dependencies.DefiniteEdges.Count != 0 || !hasQualifiedResolution(a.Structure.Dependencies, "maven", "maven-sibling-dependency", "coordinate_case_mismatch") {
			t.Fatalf("case-insensitive coordinate match became definite: %+v", a.Structure.Dependencies)
		}
	})
	t.Run("ambiguous coordinates remain qualified", func(t *testing.T) {
		app := project("app/pom.xml", "app")
		app.Requirements = append(app.Requirements, dependency)
		a := finishStructureProjects(t, []declarations.Project{app, project("one/pom.xml", "lib"), project("two/pom.xml", "lib")})
		if a.Structure.Dependencies.DefiniteEdges.Count != 0 || !hasQualifiedResolution(a.Structure.Dependencies, "maven", "maven-sibling-dependency", "ambiguous_coordinate_match") {
			t.Fatalf("ambiguous coordinate targets were not qualified: %+v", a.Structure.Dependencies)
		}
	})
}

func TestStructureMavenLegacyEdgesRequireLiteralTargetEvidence(t *testing.T) {
	for _, tt := range []struct {
		name, kind, state, reason string
	}{
		{name: "conditional target version", kind: "maven-version", state: "conditional", reason: "maven_dependency_version_unresolved"},
		{name: "unresolved target version", kind: "maven-version", state: "unresolved", reason: "maven_dependency_version_unresolved"},
		{name: "conditional target coordinate", kind: "maven-artifactId", state: "conditional", reason: "maven_target_coordinate_unresolved"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			declared := func(kind, value string) declarations.Requirement {
				return declarations.Requirement{Kind: kind, Value: value, State: "declared"}
			}
			root := declarations.Project{ID: "pom.xml", Root: ".", Kind: "maven", Requirements: []declarations.Requirement{
				declared("maven-groupId", "x"), declared("maven-artifactId", "root"), declared("maven-version", "1"),
			}, References: []declarations.Reference{
				{Kind: "module", Target: "app/pom.xml", TargetStatus: "present", State: "resolved", Evidence: "pom.xml"},
				{Kind: "module", Target: "lib/pom.xml", TargetStatus: "present", State: "resolved", Evidence: "pom.xml"},
			}}
			app := declarations.Project{ID: "app/pom.xml", Root: "app", Kind: "maven", Requirements: []declarations.Requirement{
				declared("maven-groupId", "x"), declared("maven-artifactId", "app"), declared("maven-version", "1"),
				{Kind: "maven-dependency", Value: "x:lib:1", State: "declared", Evidence: "app/pom.xml"},
			}}
			lib := declarations.Project{ID: "lib/pom.xml", Root: "lib", Kind: "maven", Requirements: []declarations.Requirement{
				declared("maven-groupId", "x"),
				{Kind: "maven-artifactId", Value: "lib", State: "declared"},
				declared("maven-version", "1"),
			}}
			if tt.kind == "maven-version" {
				lib.Requirements[2] = declarations.Requirement{Kind: tt.kind, Value: "1", State: tt.state}
			} else {
				lib.Requirements[1] = declarations.Requirement{Kind: tt.kind, Value: "lib", State: tt.state}
			}
			a := finishStructureProjects(t, []declarations.Project{root, app, lib})
			for _, edge := range a.Structure.Dependencies.Edges {
				if edge.From == app.ID && edge.To == lib.ID && edge.Kind == "maven-sibling-dependency" {
					t.Fatalf("legacy Maven relationship was promoted without literal target evidence: %+v", edge)
				}
			}
			if !coverageReason(a.Structure, "project_dependencies", "maven", tt.reason) {
				t.Fatalf("missing Maven completeness qualification %q: %+v", tt.reason, a.Structure.Coverage)
			}
		})
	}
}

func TestStructureValidationRejectsOverflowedPopulationsAndInflatedComponents(t *testing.T) {
	base := finishStructureProjects(t, []declarations.Project{{ID: "App.csproj", Root: ".", Kind: "dotnet", Requirements: []declarations.Requirement{}, References: []declarations.Reference{}, Interfaces: []declarations.Interface{}}})
	populationOverflow := *base
	populationOverflow.Structure = cloneStructureForTest(base.Structure)
	populationOverflow.Structure.Populations = []StructurePopulation{
		{Population: "alpha", Ecosystem: "a", Role: "primary", Metric: metric(math.MaxInt64, "test", false, []string{})},
		{Population: "beta", Ecosystem: "b", Role: "primary", Metric: metric(math.MaxInt64, "test", false, []string{})},
		{Population: "gamma", Ecosystem: "c", Role: "primary", Metric: metric(3, "test", false, []string{})},
	}
	slices.SortFunc(populationOverflow.Structure.Populations, func(a, b StructurePopulation) int {
		return strings.Compare(structurePopulationKey(a.Population, a.Ecosystem, a.Role), structurePopulationKey(b.Population, b.Ecosystem, b.Role))
	})
	if err := ValidateReport(&populationOverflow); err == nil {
		t.Fatal("overflowed structural population total was accepted")
	}

	inflated := *base
	inflated.Structure = cloneStructureForTest(base.Structure)
	c := &inflated.Structure.Dependencies.Components[0]
	c.ProjectCount = math.MaxInt64
	c.OmittedProjects = math.MaxInt64 - 1
	if err := ValidateReport(&inflated); err == nil {
		t.Fatal("component project total exceeding graph vertices was accepted")
	}
}

func BenchmarkMavenCoordinateIndexManyProjects(b *testing.B) {
	projects := make([]declarations.Project, 2048)
	for i := range projects {
		projects[i] = declarations.Project{
			ID: fmt.Sprintf("module-%04d/pom.xml", i), Kind: "maven",
			Requirements: []declarations.Requirement{
				{Kind: "maven-groupId", Value: "example.group", State: "declared"},
				{Kind: "maven-artifactId", Value: fmt.Sprintf("module-%04d", i), State: "declared"},
			},
		}
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		index := newMavenCoordinateIndex(projects)
		if len(index.exact) != len(projects) || len(index.folded) != len(projects) || len(index.byID) != len(projects) {
			b.Fatal("coordinate index did not retain all unique projects")
		}
	}
}

func TestStructureValidationRejectsOverflowedGroupMemberSamples(t *testing.T) {
	root := declarations.Project{ID: "package.json", Root: ".", Kind: "npm", Requirements: []declarations.Requirement{{Kind: "npm-workspace-root", Value: "packages/*", State: "declared", Evidence: "package.json"}}, References: []declarations.Reference{}, Interfaces: []declarations.Interface{}}
	member := declarations.Project{ID: "packages/app/package.json", Root: "packages/app", Kind: "npm", Requirements: []declarations.Requirement{}, References: []declarations.Reference{}, Interfaces: []declarations.Interface{}}
	base := finishStructureProjects(t, []declarations.Project{root, member})
	if len(base.Structure.WorkspaceGroups) != 1 {
		t.Fatalf("test setup did not produce a workspace group: %+v", base.Structure)
	}
	corrupted := *base
	corrupted.Structure = cloneStructureForTest(base.Structure)
	g := &corrupted.Structure.WorkspaceGroups[0]
	g.Members = []string{member.ID}
	g.OmittedMembers = math.MaxInt64
	g.MemberCount = math.MaxInt64
	if err := ValidateReport(&corrupted); err == nil {
		t.Fatal("overflowed workspace member sample total was accepted")
	}
}

func TestStructureValidationChecksFullyRetainedGraphPartitionButAllowsOmittedComponents(t *testing.T) {
	projects := []declarations.Project{
		{ID: "app/package.json", Root: "app", Kind: "npm", Requirements: []declarations.Requirement{}, References: []declarations.Reference{}, Interfaces: []declarations.Interface{}},
		{ID: "lib/package.json", Root: "lib", Kind: "npm", Requirements: []declarations.Requirement{}, References: []declarations.Reference{}, Interfaces: []declarations.Interface{}},
	}
	base := finishStructureProjects(t, projects)
	crossing := *base
	crossing.Structure = cloneStructureForTest(base.Structure)
	d := &crossing.Structure.Dependencies
	d.Edges = []StructureEdge{{From: "app/package.json", To: "lib/package.json", Ecosystem: "npm", Kind: "npm-local-dependency"}}
	d.DefiniteEdges = metric(1, "test observed edge", false, []string{})
	d.Components[0].EdgeCount = 1
	if err := ValidateReport(&crossing); err == nil {
		t.Fatal("fully retained edge crossing two connected components was accepted")
	}

	truncated := *base
	truncated.Structure = cloneStructureForTest(base.Structure)
	truncated.Structure.Dependencies.Components = truncated.Structure.Dependencies.Components[:1]
	truncated.Structure.Dependencies.OmittedComponents = 1
	truncated.Structure.Dependencies.ConnectedGroups = metric(2, "weakly connected groups in the retained observed dependency graph", false, []string{})
	if err := ValidateReport(&truncated); err != nil {
		t.Fatalf("valid retained component sample rejected when another component is omitted: %v", err)
	}
}

func TestStructureValidationRequiresSourceEntryCoverageDisclosure(t *testing.T) {
	base := finishStructureProjects(t, []declarations.Project{{ID: "App.csproj", Root: ".", Kind: "dotnet", Requirements: []declarations.Requirement{}, References: []declarations.Reference{}, Interfaces: []declarations.Interface{}}})
	missing := *base
	missing.Structure = cloneStructureForTest(base.Structure)
	missing.Structure.Coverage = slices.DeleteFunc(missing.Structure.Coverage, func(c StructureCoverage) bool { return c.Scope == "entry_points" && c.Ecosystem == "all" })
	if err := ValidateReport(&missing); err == nil {
		t.Fatal("missing source entry-point coverage was accepted")
	}
	complete := *base
	complete.Structure = cloneStructureForTest(base.Structure)
	for i := range complete.Structure.Coverage {
		if complete.Structure.Coverage[i].Scope == "entry_points" && complete.Structure.Coverage[i].Ecosystem == "all" {
			complete.Structure.Coverage[i] = StructureCoverage{Scope: "entry_points", Ecosystem: "all", Status: "complete", Reasons: []string{}}
		}
	}
	if err := ValidateReport(&complete); err == nil {
		t.Fatal("zero entry points with falsely complete source coverage were accepted")
	}
}

func TestStructureEntryPointRowAndAssociationTotalsReconcile(t *testing.T) {
	base := finishStructureProjects(t, []declarations.Project{{ID: "app/package.json", Root: "app", Kind: "npm", Requirements: []declarations.Requirement{}, References: []declarations.Reference{}, Interfaces: []declarations.Interface{}}})
	SetStructureEntryPoints(base, []StructureEntryPoint{
		{ProjectID: "app/package.json", EvidencePath: "app/Dockerfile", Ecosystem: "npm", Role: "primary", Kind: "builds:container", Name: "image", Basis: "test", State: "declared"},
		{ProjectID: "app/package.json", EvidencePath: "app/Dockerfile", Ecosystem: "npm", Role: "primary", Kind: "runs:container", Name: "image", Basis: "test", State: "declared"},
		{EvidencePath: "README.md", Ecosystem: "npm", Role: "primary", Kind: "container-launch", Name: "image", Basis: "test", Reason: "owner_unresolved", State: "unassociated"},
	})
	if base.Structure.EntryPointCount != 2 || base.Structure.EntryPointRowCount != 3 || base.Structure.EntryPointAssociationCount != 2 {
		t.Fatalf("entry-point declaration, row, and association totals = %d/%d/%d, want 2/3/2", base.Structure.EntryPointCount, base.Structure.EntryPointRowCount, base.Structure.EntryPointAssociationCount)
	}
	if err := ValidateReport(base); err != nil {
		t.Fatalf("valid row totals rejected: %v", err)
	}
	for _, mutate := range []func(*StructureReport){
		func(s *StructureReport) { s.EntryPointRowCount++ },
		func(s *StructureReport) { s.EntryPointAssociationCount++ },
		func(s *StructureReport) { s.EntryPointAssociationCount-- },
		func(s *StructureReport) {
			associated := make([]StructureEntryPoint, 0, 2)
			for _, e := range s.EntryPoints {
				if e.ProjectID != "" {
					associated = append(associated, e)
				}
			}
			s.EntryPoints = associated
			s.OmittedEntryPoints = 1
			s.EntryPointCount = 3 // Two retained rows describe one declaration; one omitted row can add at most one.
		},
	} {
		bad := *base
		bad.Structure = cloneStructureForTest(base.Structure)
		mutate(bad.Structure)
		if err := ValidateReport(&bad); err == nil {
			t.Fatalf("corrupted entry-point totals were accepted: %+v", bad.Structure)
		}
	}
}

func TestStructureConnectedGroupCountIsExactForObservedGraph(t *testing.T) {
	base := finishStructureProjects(t, []declarations.Project{{ID: "App.csproj", Root: ".", Kind: "dotnet", Requirements: []declarations.Requirement{}, References: []declarations.Reference{}, Interfaces: []declarations.Interface{}}})
	lowerBound := *base
	lowerBound.Structure = cloneStructureForTest(base.Structure)
	lowerBound.Structure.Dependencies.ConnectedGroups = metric(base.Structure.Dependencies.ConnectedGroups.Count, "test observed graph", true, []string{"graph_edges_may_be_missing"})
	if err := ValidateReport(&lowerBound); err == nil {
		t.Fatal("observed connected-group count incorrectly marked as a lower bound was accepted")
	}
}

func finishStructureProjects(t *testing.T, projects []declarations.Project) *Report {
	t.Helper()
	report := &declarations.Report{Source: "directory", Status: "complete", Projects: projects, Diagnostics: []declarations.Diagnostic{}}
	records := make([]declarations.ProjectRecord, 0, len(projects))
	collector := New("directory", "")
	for _, p := range projects {
		records = append(records, declarations.ProjectRecord{Project: p, Parsed: true, Complete: true})
		collector.Add(discovery.File{Path: p.ID, Size: 1})
	}
	out, err := collector.Finish(Evidence{Declarations: report, Lockfiles: &lockfiles.Report{Source: "directory", Status: "complete", Contexts: []lockfiles.Context{}}, Records: records})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func hasQualifiedResolution(d StructureDependencies, ecosystem, kind, resolution string) bool {
	for _, row := range d.QualifiedReferences {
		if row.Ecosystem == ecosystem && row.Kind == kind && row.Resolution == resolution {
			return true
		}
	}
	return false
}

func cloneStructureForTest(in *StructureReport) *StructureReport {
	out := *in
	out.Populations = append([]StructurePopulation(nil), in.Populations...)
	out.Coverage = append([]StructureCoverage(nil), in.Coverage...)
	out.WorkspaceGroups = append([]StructureGroup(nil), in.WorkspaceGroups...)
	out.SolutionGroups = append([]StructureGroup(nil), in.SolutionGroups...)
	out.Dependencies.Edges = append([]StructureEdge(nil), in.Dependencies.Edges...)
	out.Dependencies.Components = append([]StructureConnectedComponent(nil), in.Dependencies.Components...)
	out.Dependencies.QualifiedReferences = append([]StructureQualifiedCount(nil), in.Dependencies.QualifiedReferences...)
	out.EntryPoints = append([]StructureEntryPoint(nil), in.EntryPoints...)
	return &out
}

func TestStructureCoverageIsScopedAndCandidateRowsIgnoreParserDiagnostics(t *testing.T) {
	npm := declarations.Project{ID: "packages/package.json", Root: "packages", Kind: "npm", References: []declarations.Reference{}, Requirements: []declarations.Requirement{}, Interfaces: []declarations.Interface{}}
	maven := declarations.Project{ID: "service/pom.xml", Root: "service", Kind: "maven", References: []declarations.Reference{}, Requirements: []declarations.Requirement{}, Interfaces: []declarations.Interface{}}
	report := &declarations.Report{Source: "directory", Status: "complete", Projects: []declarations.Project{npm, maven}, Diagnostics: []declarations.Diagnostic{{Path: npm.ID, Code: "unmatched-npm-workspace-pattern"}}}
	collector := New("directory", "")
	for _, p := range report.Projects {
		collector.Add(discovery.File{Path: p.ID, Size: 1})
	}
	records := []declarations.ProjectRecord{{Project: npm, Parsed: true, Complete: true}, {Project: maven, Parsed: true, Complete: true}}
	assessment, err := collector.Finish(Evidence{Declarations: report, Lockfiles: &lockfiles.Report{Source: "directory", Status: "complete", Contexts: []lockfiles.Context{}}, Records: records})
	if err != nil {
		t.Fatal(err)
	}
	if got := populationMetric(assessment.Structure, "filename_candidates", "npm").Completeness; got != "complete" {
		t.Fatalf("candidate completeness inherited parser uncertainty: %s", got)
	}
	if got := populationMetric(assessment.Structure, "parsed_projects", "npm").Completeness; got != "complete" {
		t.Fatalf("known npm project population was tainted by a workspace diagnostic: %s", got)
	}
	if got := populationMetric(assessment.Structure, "parsed_projects", "maven").Completeness; got != "complete" {
		t.Fatalf("unrelated Maven project completeness = %s", got)
	}
	if coverageStatus(assessment.Structure, "workspace_membership", "npm") != "partial" || coverageStatus(assessment.Structure, "workspace_membership", "maven") != "complete" {
		t.Fatalf("workspace coverage: %+v", assessment.Structure.Coverage)
	}
	if coverageStatus(assessment.Structure, "project_dependencies", "npm") != "complete" {
		t.Fatalf("workspace diagnostic tainted unrelated npm dependency scope: %+v", assessment.Structure.Coverage)
	}
	dependencyDecls := *report
	dependencyDecls.Diagnostics = []declarations.Diagnostic{{Path: npm.ID, Code: "unsupported-npm-dependency"}}
	collector3 := New("directory", "")
	collector3.Add(discovery.File{Path: npm.ID, Size: 1})
	assessment3, err := collector3.Finish(Evidence{Declarations: &dependencyDecls, Lockfiles: &lockfiles.Report{Source: "directory", Status: "complete", Contexts: []lockfiles.Context{}}, Records: records})
	if err != nil {
		t.Fatal(err)
	}
	if populationMetric(assessment3.Structure, "parsed_projects", "npm").Completeness != "complete" || coverageStatus(assessment3.Structure, "workspace_membership", "npm") != "complete" || coverageStatus(assessment3.Structure, "project_dependencies", "npm") != "partial" {
		t.Fatalf("dependency diagnostic crossed population or membership scope: %+v", assessment3.Structure)
	}
	collector2 := New("directory", "")
	collector2.Add(discovery.File{Path: npm.ID, Size: 1})
	collector2.Partial("directory_read_error")
	assessment2, err := collector2.Finish(Evidence{Declarations: &declarations.Report{Source: "directory", Status: "complete", Projects: []declarations.Project{npm}, Diagnostics: []declarations.Diagnostic{}}, Lockfiles: &lockfiles.Report{Source: "directory", Status: "complete", Contexts: []lockfiles.Context{}}, Records: []declarations.ProjectRecord{{Project: npm, Parsed: true, Complete: true}}})
	if err != nil {
		t.Fatal(err)
	}
	if got := populationMetric(assessment2.Structure, "filename_candidates", "npm").Completeness; got != "lower_bound" {
		t.Fatalf("candidate row should reflect inventory omission: %s", got)
	}
}

func TestStructureDistinctRootsPartitionByEcosystemAndHighestPriorityRole(t *testing.T) {
	projects := []declarations.Project{
		{ID: "src/App.csproj", Root: "src", Kind: "dotnet", Requirements: []declarations.Requirement{}, References: []declarations.Reference{}, Interfaces: []declarations.Interface{}},
		{ID: "src/App.Tests.csproj", Root: "src", Kind: "dotnet", Requirements: []declarations.Requirement{}, References: []declarations.Reference{}, Interfaces: []declarations.Interface{}},
		{ID: "src/package.json", Root: "src", Kind: "npm", Requirements: []declarations.Requirement{}, References: []declarations.Reference{}, Interfaces: []declarations.Interface{}},
	}
	report := &declarations.Report{Source: "directory", Status: "complete", Projects: projects, Diagnostics: []declarations.Diagnostic{}}
	records := make([]declarations.ProjectRecord, 0, len(projects))
	c := New("directory", "")
	for _, p := range projects {
		records = append(records, declarations.ProjectRecord{Project: p, Parsed: true, Complete: true})
		c.Add(discovery.File{Path: p.ID, Size: 1})
	}
	r, err := c.Finish(Evidence{Declarations: report, Lockfiles: &lockfiles.Report{Source: "directory", Status: "complete", Contexts: []lockfiles.Context{}}, Records: records})
	if err != nil {
		t.Fatal(err)
	}
	if got := populationCount(r.Structure, "distinct_roots", "nuget"); got != 1 {
		t.Fatalf("NuGet distinct roots = %d, want 1", got)
	}
	if got := populationCount(r.Structure, "distinct_roots", "npm"); got != 1 {
		t.Fatalf("npm distinct roots = %d, want 1", got)
	}
	if got := populationCountRole(r.Structure, "distinct_roots", "nuget", "test"); got != 1 {
		t.Fatalf("highest-priority role for shared NuGet root = %d test roots, want 1", got)
	}
	if got := populationCountRole(r.Structure, "distinct_roots", "nuget", "primary"); got != 0 {
		t.Fatalf("shared NuGet root counted in multiple role rows: %d", got)
	}
}

func TestStructureDisclosesUnappliedSharedMSBuildProjectReferences(t *testing.T) {
	project := declarations.Project{ID: "app/App.csproj", Root: "app", Kind: "dotnet", Requirements: []declarations.Requirement{}, References: []declarations.Reference{}, Interfaces: []declarations.Interface{}}
	config := declarations.Project{ID: "Directory.Build.props", Root: ".", Kind: "dotnet-configuration", References: []declarations.Reference{{Kind: "project-reference", Value: "Shared.csproj", Evidence: "Directory.Build.props", State: "declared"}}, Requirements: []declarations.Requirement{}, Interfaces: []declarations.Interface{}}
	decls := &declarations.Report{
		Source: "directory", Status: "complete", Projects: []declarations.Project{project, config}, Diagnostics: []declarations.Diagnostic{},
	}
	c := New("directory", "")
	c.Add(discovery.File{Path: project.ID, Size: 1})
	r, err := c.Finish(Evidence{Declarations: decls, Lockfiles: &lockfiles.Report{Source: "directory", Status: "complete", Contexts: []lockfiles.Context{}}, Records: []declarations.ProjectRecord{{Project: project, Parsed: true, Complete: true}}})
	if err != nil {
		t.Fatal(err)
	}
	if got := coverageStatus(r.Structure, "project_dependencies", "nuget"); got != "partial" || !coverageReason(r.Structure, "project_dependencies", "nuget", "shared_msbuild_project_references_not_applied") {
		t.Fatalf("shared MSBuild ProjectReference omission not disclosed in dependency coverage: %+v", r.Structure.Coverage)
	}
	if got := coverageStatus(r.Structure, "workspace_membership", "nuget"); got != "complete" {
		t.Fatalf("shared ProjectReference tainted workspace coverage: %+v", r.Structure.Coverage)
	}
}

func TestAssessmentVersionCompatibilityAndEntryPointHook(t *testing.T) {
	project := declarations.Project{ID: "app/package.json", Root: "app", Kind: "npm", References: []declarations.Reference{}, Requirements: []declarations.Requirement{}, Interfaces: []declarations.Interface{}}
	c := New("directory", "")
	c.Add(discovery.File{Path: project.ID, Size: 1})
	r, err := c.Finish(Evidence{Declarations: &declarations.Report{Source: "directory", Status: "complete", Projects: []declarations.Project{project}, Diagnostics: []declarations.Diagnostic{}}, Lockfiles: &lockfiles.Report{Source: "directory", Status: "complete", Contexts: []lockfiles.Context{}}, Records: []declarations.ProjectRecord{{Project: project, Parsed: true, Complete: true}}})
	if err != nil {
		t.Fatal(err)
	}
	if coverageStatus(r.Structure, "entry_points", "all") != "partial" || !coverageReason(r.Structure, "entry_points", "all", "entry_point_observer_not_run") {
		t.Fatalf("core assessment did not disclose the unrun entry-point observer: %+v", r.Structure.Coverage)
	}
	SetStructureEntryPoints(r, []StructureEntryPoint{{ProjectID: project.ID, EvidencePath: "app/server.js", Ecosystem: "npm", Role: "primary", Kind: "node-entry", Name: "server", Basis: "map-observation", State: "declared"}, {EvidencePath: "Dockerfile", Ecosystem: "npm", Role: "primary", Kind: "container-launch", Basis: "static-container-observation", Reason: "owner_unresolved", State: "qualified"}})
	if r.Structure.EntryPointCount != 2 || r.Structure.EntryPointRowCount != 2 || r.Structure.EntryPointAssociationCount != 1 || len(r.Structure.EntryPoints) != 2 || r.Structure.EntryPoints[0].ProjectID != "" {
		t.Fatalf("entry points did not retain unassociated declarations deterministically: %+v", r.Structure)
	}
	if err := ValidateReport(r); err != nil {
		t.Fatal(err)
	}
	if !coverageReason(r.Structure, "entry_points", "all", "entry_point_scope_not_established") {
		t.Fatalf("entry-point setter did not preserve conservative scope qualification: %+v", r.Structure.Coverage)
	}
	longPathReport := *r
	longPathReport.Structure = cloneStructureForTest(r.Structure)
	SetStructureEntryPoints(&longPathReport, []StructureEntryPoint{{ProjectID: project.ID, EvidencePath: "app/" + strings.Repeat("x", MaxEvidencePathBytes), Ecosystem: "npm", Role: "primary", Kind: "oversized-path", Basis: "test", State: "declared"}})
	if longPathReport.Structure.EntryPointCount != 1 || longPathReport.Structure.EntryPointRowCount != 1 || longPathReport.Structure.EntryPointAssociationCount != 1 || longPathReport.Structure.OmittedEntryPoints != 1 || len(longPathReport.Structure.EntryPoints) != 0 {
		t.Fatalf("oversized entry evidence was not counted and omitted: %+v", longPathReport.Structure)
	}
	if err := ValidateReport(&longPathReport); err != nil {
		t.Fatalf("bounded oversized entry report invalid: %v", err)
	}
	byteBoundReport := *r
	byteBoundReport.Structure = cloneStructureForTest(r.Structure)
	largeName := strings.Repeat("entry", 3200)
	entries := make([]StructureEntryPoint, StructureEntryPointLimit)
	for i := range entries {
		entries[i] = StructureEntryPoint{ProjectID: project.ID, EvidencePath: fmt.Sprintf("app/entry-%03d.js", i), Ecosystem: "npm", Role: "primary", Kind: "node-entry", Name: largeName, Basis: "test", State: "declared"}
	}
	SetStructureEntryPoints(&byteBoundReport, entries)
	if byteBoundReport.Structure.EntryPointCount != StructureEntryPointLimit || byteBoundReport.Structure.EntryPointRowCount != StructureEntryPointLimit || byteBoundReport.Structure.EntryPointAssociationCount != StructureEntryPointLimit || byteBoundReport.Structure.OmittedEntryPoints <= 0 || len(byteBoundReport.Structure.EntryPoints)+int(byteBoundReport.Structure.OmittedEntryPoints) != int(byteBoundReport.Structure.EntryPointRowCount) {
		t.Fatalf("byte-bound entry sample lost its exact aggregate: retained=%d omitted=%d total=%d", len(byteBoundReport.Structure.EntryPoints), byteBoundReport.Structure.OmittedEntryPoints, byteBoundReport.Structure.EntryPointCount)
	}
	if err := ValidateReport(&byteBoundReport); err != nil {
		t.Fatalf("byte-bounded structural report invalid: %v", err)
	}
	legacy := *r
	legacy.Version = LegacyVersion
	legacy.Structure = nil
	legacy.Lockfiles = slices.Clone(r.Lockfiles)
	for i := range legacy.Lockfiles {
		legacy.Lockfiles[i].Checks, legacy.Lockfiles[i].NuGetPresence, legacy.Lockfiles[i].Causes, legacy.Lockfiles[i].OmittedCauses = nil, nil, nil, 0
	}
	legacy.LockfilesOverall.Checks, legacy.LockfilesOverall.NuGetPresence, legacy.LockfilesOverall.Causes, legacy.LockfilesOverall.OmittedCauses = nil, nil, nil, 0
	if err := ValidateReport(&legacy); err != nil {
		t.Fatalf("legacy saved assessment rejected: %v", err)
	}
	newWithoutStructure := *r
	newWithoutStructure.Structure = nil
	if err := ValidateReport(&newWithoutStructure); err == nil {
		t.Fatal("1.1 report without structure was accepted")
	}
	b, err := json.Marshal(r)
	if err != nil || !strings.Contains(string(b), `"structure"`) {
		t.Fatalf("structure missing from JSON: %v", err)
	}
}

func TestAssessmentCounts257ParsedSolutionGroupsAcrossSampleCap(t *testing.T) {
	const solution = "Microsoft Visual Studio Solution File, Format Version 12.00\r\n"
	declCollector := declarations.New("directory", "", 0)
	declCollector.EnableProjectRecords()
	assessmentCollector := New("directory", "")
	for i := 0; i < StructureGroupLimit+1; i++ {
		name := fmt.Sprintf("solutions/s%03d.sln", i)
		data := []byte(solution)
		declCollector.Add(name, &declarations.Candidate{Path: name, Size: int64(len(data)), Read: func(context.Context, int64) ([]byte, int64, error) {
			return data, int64(len(data)), nil
		}})
		assessmentCollector.Add(discovery.File{Path: name, Size: int64(len(data))})
	}
	declReport, err := declCollector.Finish(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	r, err := assessmentCollector.Finish(Evidence{
		Declarations:         declReport,
		Records:              declCollector.ProjectRecords(),
		InterpretedManifests: declCollector.InterpretedPaths(),
		Lockfiles:            &lockfiles.Report{Source: "directory", Status: "complete", Contexts: []lockfiles.Context{}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if r.Structure.SolutionGroupCount != StructureGroupLimit+1 || len(r.Structure.SolutionGroups) != StructureGroupLimit || r.Structure.OmittedSolutionGroups != 1 {
		t.Fatalf("solution group exact/sample counts disagree: total=%d retained=%d omitted=%d", r.Structure.SolutionGroupCount, len(r.Structure.SolutionGroups), r.Structure.OmittedSolutionGroups)
	}
	var populationTotal int64
	for _, row := range r.Structure.Populations {
		if row.Population == "solution_groups" {
			populationTotal += row.Metric.Count
		}
	}
	if populationTotal != StructureGroupLimit+1 {
		t.Fatalf("solution group population total = %d, want %d", populationTotal, StructureGroupLimit+1)
	}
	if err := ValidateReport(r); err != nil {
		t.Fatalf("ValidateReport: %v", err)
	}
}

func TestStructureByteBudgetKeepsExactGroupMemberTotals(t *testing.T) {
	project := declarations.Project{ID: "app/package.json", Root: "app", Kind: "npm", References: []declarations.Reference{}, Requirements: []declarations.Requirement{}, Interfaces: []declarations.Interface{}}
	c := New("directory", "")
	c.Add(discovery.File{Path: project.ID, Size: 1})
	r, err := c.Finish(Evidence{Declarations: &declarations.Report{Source: "directory", Status: "complete", Projects: []declarations.Project{project}, Diagnostics: []declarations.Diagnostic{}}, Lockfiles: &lockfiles.Report{Source: "directory", Status: "complete", Contexts: []lockfiles.Context{}}, Records: []declarations.ProjectRecord{{Project: project, Parsed: true, Complete: true}}})
	if err != nil {
		t.Fatal(err)
	}
	const groupCount = 64
	const memberCount = 64
	groups := make([]StructureGroup, 0, groupCount)
	for i := 0; i < groupCount; i++ {
		members := make([]string, 0, memberCount)
		for j := 0; j < memberCount; j++ {
			members = append(members, fmt.Sprintf("%s-%04d", strings.Repeat("a", MaxEvidencePathBytes-6), j))
		}
		slices.Sort(members)
		id := fmt.Sprintf("workspace/%03d/package.json", i)
		groups = append(groups, StructureGroup{ID: id, Ecosystem: "npm", Role: "primary", Kind: "workspace", MemberCount: memberCount, Members: members, UnresolvedMembers: []StructureUnresolvedMember{}, MembershipCoverage: StructureCoverage{Scope: "workspace_membership", Ecosystem: "npm", Status: "complete", Reasons: []string{}}})
	}
	r.Structure.WorkspaceGroups = groups
	r.Structure.WorkspaceGroupCount = groupCount
	r.Structure.Populations = append(r.Structure.Populations, StructurePopulation{Population: "workspace_groups", Ecosystem: "npm", Role: "primary", Metric: metric(groupCount, "test groups", false, []string{})})
	slices.SortFunc(r.Structure.Populations, func(a, b StructurePopulation) int {
		return strings.Compare(a.Population+"\x00"+a.Ecosystem+"\x00"+a.Role, b.Population+"\x00"+b.Ecosystem+"\x00"+b.Role)
	})
	boundStructureForReport(r)
	var observedMembers, omittedMembers int64
	for _, group := range r.Structure.WorkspaceGroups {
		observedMembers += group.MemberCount
		omittedMembers += group.OmittedMembers
	}
	if r.Structure.WorkspaceGroupCount != groupCount || int64(len(r.Structure.WorkspaceGroups))+r.Structure.OmittedWorkspaceGroups != groupCount || observedMembers != int64(len(r.Structure.WorkspaceGroups))*memberCount || omittedMembers+r.Structure.OmittedWorkspaceGroups == 0 {
		t.Fatalf("byte cap changed group/member aggregates: groups=%d retained=%d observed_members=%d omitted_members=%d", r.Structure.WorkspaceGroupCount, len(r.Structure.WorkspaceGroups), observedMembers, omittedMembers)
	}
	if err := ValidateReport(r); err != nil {
		t.Fatalf("byte-bounded group report invalid: %v", err)
	}
}

func TestStructureRetainsExplicitEmptyWorkspaceGroups(t *testing.T) {
	npm := declarations.Project{ID: "npm/package.json", Root: "npm", Kind: "npm", Requirements: []declarations.Requirement{{Kind: "npm-workspace-root", Value: "declared", State: "declared", Evidence: "npm/package.json"}}, References: []declarations.Reference{{Kind: "npm-workspace-member", Value: "packages/unresolved", State: "unresolved", Evidence: "npm/package.json"}}, Interfaces: []declarations.Interface{}}
	goWork := declarations.Project{ID: "go/go.work", Root: "go", Kind: "go-workspace", Requirements: []declarations.Requirement{}, References: []declarations.Reference{}, Interfaces: []declarations.Interface{}}
	decls := &declarations.Report{Source: "directory", Status: "complete", Projects: []declarations.Project{npm, goWork}, Diagnostics: []declarations.Diagnostic{}}
	c := New("directory", "")
	c.Add(discovery.File{Path: npm.ID, Size: 1})
	c.Add(discovery.File{Path: goWork.ID, Size: 1})
	r, err := c.Finish(Evidence{Declarations: decls, Lockfiles: &lockfiles.Report{Source: "directory", Status: "complete", Contexts: []lockfiles.Context{}}, Records: []declarations.ProjectRecord{{Project: npm, Parsed: true, Complete: true}, {Project: goWork, Parsed: true, Complete: true}}})
	if err != nil {
		t.Fatal(err)
	}
	if r.Structure.WorkspaceGroupCount != 2 || len(r.Structure.WorkspaceGroups) != 2 {
		t.Fatalf("empty groups absent: %+v", r.Structure.WorkspaceGroups)
	}
	for _, g := range r.Structure.WorkspaceGroups {
		if g.ID == npm.ID {
			if g.MemberCount != 0 || g.UnresolvedMemberCount != 1 || g.UnresolvedMembers[0].Value != "packages/unresolved" || g.UnresolvedMembers[0].Reason == "" || g.MembershipCoverage.Status != "complete" {
				t.Fatalf("unresolved npm group facts: %+v", g)
			}
			continue
		}
		if g.MemberCount != 0 || g.UnresolvedMemberCount != 0 || g.MembershipCoverage.Status != "complete" {
			t.Fatalf("empty group facts: %+v", g)
		}
	}
	if err := ValidateReport(r); err != nil {
		t.Fatal(err)
	}
	unsafe := *r
	unsafe.Structure = cloneStructureForTest(r.Structure)
	for i := range unsafe.Structure.WorkspaceGroups {
		if unsafe.Structure.WorkspaceGroups[i].ID == npm.ID {
			unsafe.Structure.WorkspaceGroups[i].UnresolvedMembers = append([]StructureUnresolvedMember(nil), unsafe.Structure.WorkspaceGroups[i].UnresolvedMembers...)
			unsafe.Structure.WorkspaceGroups[i].UnresolvedMembers[0].Value = "bad\x01value"
		}
	}
	if err := ValidateReport(&unsafe); err == nil {
		t.Fatal("control character in unresolved member was accepted")
	}
}

func TestStructureRetainsExplicitEmptyMavenAggregatorFromPrivateRecord(t *testing.T) {
	project := declarations.Project{ID: "maven/pom.xml", Root: "maven", Kind: "maven", Requirements: []declarations.Requirement{}, References: []declarations.Reference{}, Interfaces: []declarations.Interface{}}
	decls := &declarations.Report{Source: "directory", Status: "complete", Projects: []declarations.Project{project}, Diagnostics: []declarations.Diagnostic{}}
	c := New("directory", "")
	c.Add(discovery.File{Path: project.ID, Size: 1})
	r, err := c.Finish(Evidence{Declarations: decls, Lockfiles: &lockfiles.Report{Source: "directory", Status: "complete", Contexts: []lockfiles.Context{}}, Records: []declarations.ProjectRecord{{Project: project, Parsed: true, Complete: true, WorkspaceDeclared: true}}})
	if err != nil {
		t.Fatal(err)
	}
	if r.Structure.WorkspaceGroupCount != 1 || len(r.Structure.WorkspaceGroups) != 1 || r.Structure.WorkspaceGroups[0].ID != project.ID || r.Structure.WorkspaceGroups[0].MemberCount != 0 {
		t.Fatalf("empty Maven aggregator missing: %+v", r.Structure.WorkspaceGroups)
	}
	if err := ValidateReport(r); err != nil {
		t.Fatal(err)
	}
}

func populationCount(s *StructureReport, name, ecosystem string) int64 {
	return populationMetric(s, name, ecosystem).Count
}
func populationCountRole(s *StructureReport, name, ecosystem, role string) int64 {
	for _, row := range s.Populations {
		if row.Population == name && row.Ecosystem == ecosystem && row.Role == role {
			return row.Metric.Count
		}
	}
	return 0
}
func populationMetric(s *StructureReport, name, ecosystem string) Metric {
	for _, row := range s.Populations {
		if row.Population == name && row.Ecosystem == ecosystem {
			return row.Metric
		}
	}
	return Metric{}
}
func coverageStatus(s *StructureReport, scope, ecosystem string) string {
	for _, row := range s.Coverage {
		if row.Scope == scope && row.Ecosystem == ecosystem {
			return row.Status
		}
	}
	return ""
}
func TestValidateStructureEntryPointStateReasonConstraints(t *testing.T) {
	project := declarations.Project{ID: "app/package.json", Root: "app", Kind: "npm", References: []declarations.Reference{}, Requirements: []declarations.Requirement{}, Interfaces: []declarations.Interface{}}
	c := New("directory", "")
	c.Add(discovery.File{Path: project.ID, Size: 1})
	base, err := c.Finish(Evidence{Declarations: &declarations.Report{Source: "directory", Status: "complete", Projects: []declarations.Project{project}, Diagnostics: []declarations.Diagnostic{}}, Lockfiles: &lockfiles.Report{Source: "directory", Status: "complete", Contexts: []lockfiles.Context{}}, Records: []declarations.ProjectRecord{{Project: project, Parsed: true, Complete: true}}})
	if err != nil {
		t.Fatal(err)
	}

	validCases := []StructureEntryPoint{
		{EvidencePath: "app/a.js", Ecosystem: "npm", Role: "primary", Kind: "k", Basis: "b", State: "declared"},
		{EvidencePath: "app/b.js", Ecosystem: "npm", Role: "primary", Kind: "k", Basis: "b", State: "associated"},
		{EvidencePath: "app/c.js", Ecosystem: "npm", Role: "primary", Kind: "k", Basis: "b", State: "qualified", Reason: "r"},
		{EvidencePath: "app/d.js", Ecosystem: "npm", Role: "primary", Kind: "k", Basis: "b", State: "unassociated", Reason: "r"},
	}
	SetStructureEntryPoints(base, validCases)
	if err := ValidateReport(base); err != nil {
		t.Fatalf("valid state/reason combinations failed validation: %v", err)
	}

	invalidCases := []struct {
		name   string
		state  string
		reason string
	}{
		{"declared with reason", "declared", "should_be_empty"},
		{"associated with reason", "associated", "should_be_empty"},
		{"qualified without reason", "qualified", ""},
		{"unassociated without reason", "unassociated", ""},
		{"invalid state", "unknown_state", ""},
	}
	for _, tc := range invalidCases {
		r := *base
		r.Structure = cloneStructureForTest(base.Structure)
		// SetStructureEntryPoints stores the entry as-is; ValidateReport checks constraints.
		SetStructureEntryPoints(&r, []StructureEntryPoint{{EvidencePath: "app/x.js", Ecosystem: "npm", Role: "primary", Kind: "k", Basis: "b", State: tc.state, Reason: tc.reason}})
		if err := ValidateReport(&r); err == nil {
			t.Errorf("case %q: expected validation error for state=%q reason=%q", tc.name, tc.state, tc.reason)
		}
	}
}

func coverageReason(s *StructureReport, scope, ecosystem, reason string) bool {
	for _, row := range s.Coverage {
		if row.Scope == scope && row.Ecosystem == ecosystem {
			return slices.Contains(row.Reasons, reason)
		}
	}
	return false
}
func hasQualified(d StructureDependencies, ecosystem, kind, state string) bool {
	for _, row := range d.QualifiedReferences {
		if row.Ecosystem == ecosystem && row.Kind == kind && row.State == state {
			return true
		}
	}
	return false
}

// TestStructureMavenParentCoordinateVerification covers F8: a parent
// relationship must only become a definite dependency edge when the target
// POM's declared coordinates match the declared parent GA (case-sensitive)
// and, when both versions are literal, the versions agree.
func TestStructureMavenParentCoordinateVerification(t *testing.T) {
	req := func(kind, value string) declarations.Requirement {
		return declarations.Requirement{Kind: kind, Value: value, State: "declared", Evidence: ""}
	}
	parentRef := func(from, to string) declarations.Reference {
		return declarations.Reference{Kind: "parent", Value: to, Target: to, TargetStatus: "present", State: "resolved", Evidence: from}
	}
	t.Run("matching coordinates become definite edge", func(t *testing.T) {
		parent := declarations.Project{ID: "pom.xml", Root: ".", Kind: "maven",
			Requirements: []declarations.Requirement{req("maven-groupId", "x"), req("maven-artifactId", "parent"), req("maven-version", "1")},
			References:   []declarations.Reference{}}
		child := declarations.Project{ID: "child/pom.xml", Root: "child", Kind: "maven",
			Requirements: []declarations.Requirement{req("maven-groupId", "x"), req("maven-artifactId", "child"), req("maven-version", "1"), req("maven-parent", "x:parent:1")},
			References:   []declarations.Reference{parentRef("child/pom.xml", "pom.xml")}}
		a := finishStructureProjects(t, []declarations.Project{parent, child})
		found := false
		for _, e := range a.Structure.Dependencies.Edges {
			if e.From == child.ID && e.To == parent.ID && e.Kind == "parent" {
				found = true
			}
		}
		if !found {
			t.Fatalf("verified parent did not produce a definite edge: edges=%+v qualified=%+v", a.Structure.Dependencies.Edges, a.Structure.Dependencies.QualifiedReferences)
		}
	})
	t.Run("default ../pom.xml path still needs coordinate verification", func(t *testing.T) {
		// A pre-4.1 Maven child declares a parent but the file at ../pom.xml
		// belongs to a different project. This should be qualified.
		unrelated := declarations.Project{ID: "pom.xml", Root: ".", Kind: "maven",
			Requirements: []declarations.Requirement{req("maven-groupId", "other"), req("maven-artifactId", "unrelated"), req("maven-version", "1")},
			References:   []declarations.Reference{}}
		child := declarations.Project{ID: "child/pom.xml", Root: "child", Kind: "maven",
			Requirements: []declarations.Requirement{req("maven-groupId", "x"), req("maven-artifactId", "child"), req("maven-version", "1"), req("maven-parent", "x:real-parent:1")},
			References:   []declarations.Reference{parentRef("child/pom.xml", "pom.xml")}}
		a := finishStructureProjects(t, []declarations.Project{unrelated, child})
		if a.Structure.Dependencies.DefiniteEdges.Count != 0 {
			t.Fatalf("GA-mismatched default parent produced a definite edge: %+v", a.Structure.Dependencies.Edges)
		}
		if !hasQualifiedResolution(a.Structure.Dependencies, "maven", "parent", "parent_coordinates_mismatch") {
			t.Fatalf("GA mismatch not qualified: %+v", a.Structure.Dependencies.QualifiedReferences)
		}
	})
	t.Run("maven 4.1 default parent already partial", func(t *testing.T) {
		// A Maven 4.1 model sets a condition on the default parent reference.
		// isDefiniteRelationship treats it as partial; no F8 verification needed.
		parent := declarations.Project{ID: "pom.xml", Root: ".", Kind: "maven",
			Requirements: []declarations.Requirement{req("maven-groupId", "x"), req("maven-artifactId", "parent"), req("maven-version", "1")},
			References:   []declarations.Reference{}}
		// Simulate a Maven 4.1 parent reference: State "conditional" with condition.
		child := declarations.Project{ID: "child/pom.xml", Root: "child", Kind: "maven",
			Requirements: []declarations.Requirement{req("maven-groupId", "x"), req("maven-artifactId", "child"), req("maven-version", "1"), req("maven-parent", "x:parent:1")},
			References: []declarations.Reference{{Kind: "parent", Value: "pom.xml", Target: "pom.xml", TargetStatus: "present", State: "conditional",
				Condition: "Maven default parent lookup; reactor and repository resolution not evaluated", Evidence: "child/pom.xml"}}}
		a := finishStructureProjects(t, []declarations.Project{parent, child})
		if a.Structure.Dependencies.DefiniteEdges.Count != 0 {
			t.Fatalf("Maven 4.1 conditional parent produced a definite edge: %+v", a.Structure.Dependencies.Edges)
		}
		if !hasQualifiedResolution(a.Structure.Dependencies, "maven", "parent", "qualified_target") {
			t.Fatalf("Maven 4.1 conditional parent not qualified: %+v", a.Structure.Dependencies.QualifiedReferences)
		}
	})
	t.Run("GA case mismatch is qualified as parent_coordinates_mismatch", func(t *testing.T) {
		parent := declarations.Project{ID: "pom.xml", Root: ".", Kind: "maven",
			Requirements: []declarations.Requirement{req("maven-groupId", "Org.Example"), req("maven-artifactId", "Parent"), req("maven-version", "1")},
			References:   []declarations.Reference{}}
		child := declarations.Project{ID: "child/pom.xml", Root: "child", Kind: "maven",
			Requirements: []declarations.Requirement{req("maven-groupId", "Org.Example"), req("maven-artifactId", "child"), req("maven-version", "1"), req("maven-parent", "org.example:parent:1")},
			References:   []declarations.Reference{parentRef("child/pom.xml", "pom.xml")}}
		a := finishStructureProjects(t, []declarations.Project{parent, child})
		if a.Structure.Dependencies.DefiniteEdges.Count != 0 {
			t.Fatalf("case-mismatched parent produced a definite edge: %+v", a.Structure.Dependencies.Edges)
		}
		if !hasQualifiedResolution(a.Structure.Dependencies, "maven", "parent", "parent_coordinates_mismatch") {
			t.Fatalf("GA case mismatch not qualified: %+v", a.Structure.Dependencies.QualifiedReferences)
		}
	})
	t.Run("version mismatch is qualified as parent_version_mismatch", func(t *testing.T) {
		parent := declarations.Project{ID: "pom.xml", Root: ".", Kind: "maven",
			Requirements: []declarations.Requirement{req("maven-groupId", "x"), req("maven-artifactId", "parent"), req("maven-version", "2")},
			References:   []declarations.Reference{}}
		child := declarations.Project{ID: "child/pom.xml", Root: "child", Kind: "maven",
			Requirements: []declarations.Requirement{req("maven-groupId", "x"), req("maven-artifactId", "child"), req("maven-version", "2"), req("maven-parent", "x:parent:1")},
			References:   []declarations.Reference{parentRef("child/pom.xml", "pom.xml")}}
		a := finishStructureProjects(t, []declarations.Project{parent, child})
		if a.Structure.Dependencies.DefiniteEdges.Count != 0 {
			t.Fatalf("version-mismatched parent produced a definite edge: %+v", a.Structure.Dependencies.Edges)
		}
		if !hasQualifiedResolution(a.Structure.Dependencies, "maven", "parent", "parent_version_mismatch") {
			t.Fatalf("version mismatch not qualified: %+v", a.Structure.Dependencies.QualifiedReferences)
		}
	})
	t.Run("unresolved parent coordinates are qualified", func(t *testing.T) {
		parent := declarations.Project{ID: "pom.xml", Root: ".", Kind: "maven",
			Requirements: []declarations.Requirement{req("maven-groupId", "x"), req("maven-artifactId", "parent"), req("maven-version", "1")},
			References:   []declarations.Reference{}}
		child := declarations.Project{ID: "child/pom.xml", Root: "child", Kind: "maven",
			Requirements: []declarations.Requirement{req("maven-groupId", "x"), req("maven-artifactId", "child"), req("maven-version", "1"), {Kind: "maven-parent", Value: "${project.groupId}:parent:1", State: "declared"}},
			References:   []declarations.Reference{parentRef("child/pom.xml", "pom.xml")}}
		a := finishStructureProjects(t, []declarations.Project{parent, child})
		if a.Structure.Dependencies.DefiniteEdges.Count != 0 {
			t.Fatalf("unresolved parent coord produced a definite edge: %+v", a.Structure.Dependencies.Edges)
		}
		if !hasQualifiedResolution(a.Structure.Dependencies, "maven", "parent", "parent_coordinates_unresolved") {
			t.Fatalf("unresolved coords not qualified: %+v", a.Structure.Dependencies.QualifiedReferences)
		}
	})
	t.Run("groupId inherited from target parent is included in coordinate index", func(t *testing.T) {
		// The child's groupId comes from its own maven-parent (inherited). The
		// target's coordinate index must resolve it via the first two parts of
		// the maven-parent value when maven-groupId is absent.
		grandparent := declarations.Project{ID: "pom.xml", Root: ".", Kind: "maven",
			Requirements: []declarations.Requirement{req("maven-groupId", "x"), req("maven-artifactId", "root"), req("maven-version", "1")},
			References:   []declarations.Reference{}}
		parent := declarations.Project{ID: "parent/pom.xml", Root: "parent", Kind: "maven",
			// No maven-groupId; inherits "x" from maven-parent first part.
			Requirements: []declarations.Requirement{req("maven-parent", "x:root:1"), req("maven-artifactId", "parent"), req("maven-version", "1")},
			References:   []declarations.Reference{}}
		child := declarations.Project{ID: "child/pom.xml", Root: "child", Kind: "maven",
			Requirements: []declarations.Requirement{req("maven-groupId", "x"), req("maven-artifactId", "child"), req("maven-version", "1"), req("maven-parent", "x:parent:1")},
			References:   []declarations.Reference{parentRef("child/pom.xml", "parent/pom.xml")}}
		a := finishStructureProjects(t, []declarations.Project{grandparent, parent, child})
		found := false
		for _, e := range a.Structure.Dependencies.Edges {
			if e.From == child.ID && e.To == parent.ID && e.Kind == "parent" {
				found = true
			}
		}
		if !found {
			t.Fatalf("parent with inherited groupId not a definite edge: edges=%+v qualified=%+v", a.Structure.Dependencies.Edges, a.Structure.Dependencies.QualifiedReferences)
		}
	})
}

// TestStructureLocalDependenciesExcludesMavenParent proves that the released
// local_dependencies metric (1.4.0) is not affected by Maven parent references.
// Parent edges are structural-only; adding them to the top-level metric would
// silently change a released contract.
func TestStructureLocalDependenciesExcludesMavenParent(t *testing.T) {
	req := func(kind, value string) declarations.Requirement {
		return declarations.Requirement{Kind: kind, Value: value, State: "declared"}
	}
	parent := declarations.Project{ID: "pom.xml", Root: ".", Kind: "maven",
		Requirements: []declarations.Requirement{req("maven-groupId", "x"), req("maven-artifactId", "parent"), req("maven-version", "1")},
		References:   []declarations.Reference{}}
	child := declarations.Project{ID: "child/pom.xml", Root: "child", Kind: "maven",
		Requirements: []declarations.Requirement{req("maven-groupId", "x"), req("maven-artifactId", "child"), req("maven-version", "1"), req("maven-parent", "x:parent:1")},
		References:   []declarations.Reference{{Kind: "parent", Value: "pom.xml", Target: "pom.xml", TargetStatus: "present", State: "resolved", Evidence: "child/pom.xml"}}}
	a := finishStructureProjects(t, []declarations.Project{parent, child})
	// local_dependencies counts only non-parent local relationships.
	if a.LocalDependencies.Count != 0 {
		t.Fatalf("local_dependencies counted a parent reference: %d (count should be 0)", a.LocalDependencies.Count)
	}
	// The parent edge must still appear in the structural dependency graph.
	found := false
	for _, e := range a.Structure.Dependencies.Edges {
		if e.From == child.ID && e.To == parent.ID && e.Kind == "parent" {
			found = true
		}
	}
	if !found {
		t.Fatalf("verified parent not a structural edge: edges=%+v", a.Structure.Dependencies.Edges)
	}
}

// TestDiagnosticRelationshipScopeIsExhaustive verifies that every diagnostic
// code emitted by declaration parsers appears in diagnosticRelationshipScopeTable
// with a valid scope value. Add new parser codes to knownParserCodes when a
// parser is extended; the test then fails until the code also appears in the
// table, enforcing the invariant that every code is intentionally classified.
func TestDiagnosticRelationshipScopeIsExhaustive(t *testing.T) {
	for code, scope := range diagnosticRelationshipScopeTable {
		switch scope {
		case "membership", "dependencies", "both", "none":
		default:
			t.Errorf("code %q has invalid scope %q", code, scope)
		}
	}
	// Scan the parser sources for every literal diagnostic code, so a new
	// code fails here until it is classified.
	patterns := []*regexp.Regexp{
		regexp.MustCompile(`AddDiagnostic\([^,()]+, "([a-z0-9_-]+)"`),
		regexp.MustCompile(`Code: +"([a-z0-9_-]+)"`),
		regexp.MustCompile(`Diagnostic\{[A-Za-z.]+, "([a-z0-9_-]+)"`),
		regexp.MustCompile(`fail\("([a-z0-9_-]+)"`),
	}
	found := 0
	for _, dir := range []string{"../declarations", "../projects"} {
		files, err := filepath.Glob(filepath.Join(dir, "*.go"))
		if err != nil {
			t.Fatal(err)
		}
		for _, file := range files {
			if strings.HasSuffix(file, "_test.go") {
				continue
			}
			src, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			for _, re := range patterns {
				for _, m := range re.FindAllStringSubmatch(string(src), -1) {
					found++
					if _, ok := diagnosticRelationshipScopeTable[m[1]]; !ok {
						t.Errorf("%s emits diagnostic %q, which has no structural scope", filepath.Base(file), m[1])
					}
				}
			}
		}
	}
	if found < 100 {
		t.Fatalf("source scan found only %d diagnostic codes; the patterns are stale", found)
	}
}

func TestStructureEcosystemNamesAreCanonical(t *testing.T) {
	uvRoot := declarations.Project{
		ID: "app/pyproject.toml", Root: "app", Kind: "python-workspace",
		Requirements: []declarations.Requirement{},
		References: []declarations.Reference{
			{Kind: "uv-workspace-member", Value: "lib", Target: "app/lib/pyproject.toml", TargetStatus: "present", State: "resolved", Evidence: "app/pyproject.toml"},
		},
		Interfaces: []declarations.Interface{},
	}
	uvMember := declarations.Project{
		ID: "app/lib/pyproject.toml", Root: "app/lib", Kind: "python-uv",
		Requirements: []declarations.Requirement{},
		References:   []declarations.Reference{},
		Interfaces:   []declarations.Interface{},
	}
	sln := declarations.Project{
		ID: "repo.sln", Root: ".", Kind: "solution",
		Requirements: []declarations.Requirement{},
		References: []declarations.Reference{
			{Kind: "solution-member", Value: "Lib", Target: "Lib.csproj", TargetStatus: "present", State: "declared", Evidence: "repo.sln"},
		},
		Interfaces: []declarations.Interface{},
	}
	csproj := declarations.Project{
		ID: "Lib.csproj", Root: ".", Kind: "dotnet",
		Requirements: []declarations.Requirement{},
		References:   []declarations.Reference{},
		Interfaces:   []declarations.Interface{},
	}
	a := finishStructureProjects(t, []declarations.Project{uvRoot, uvMember, sln, csproj})
	s := a.Structure

	// python-workspace group must use "python", not "python-uv".
	var uvGroup *StructureGroup
	for i := range s.WorkspaceGroups {
		if s.WorkspaceGroups[i].ID == "app/pyproject.toml" {
			uvGroup = &s.WorkspaceGroups[i]
		}
	}
	if uvGroup == nil {
		t.Fatal("python-workspace group not found")
	}
	if uvGroup.Ecosystem != "python" {
		t.Fatalf("python-workspace group ecosystem = %q, want %q", uvGroup.Ecosystem, "python")
	}

	// Solution group must use "dotnet".
	if len(s.SolutionGroups) != 1 {
		t.Fatalf("solution group count = %d, want 1", len(s.SolutionGroups))
	}
	if s.SolutionGroups[0].Ecosystem != "dotnet" {
		t.Fatalf("solution group ecosystem = %q, want %q", s.SolutionGroups[0].Ecosystem, "dotnet")
	}

	// Coverage rows must not carry "python-uv".
	for _, c := range s.Coverage {
		if c.Ecosystem == "python-uv" {
			t.Fatalf("coverage row uses non-canonical ecosystem %q: scope=%q", c.Ecosystem, c.Scope)
		}
	}

	// ValidateReport must accept the report (validator enforces the invariant).
	if err := ValidateReport(a); err != nil {
		t.Fatalf("ValidateReport failed for canonical-ecosystem report: %v", err)
	}
}
