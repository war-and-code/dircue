package componentmap

import (
	"encoding/json"
	"fmt"
	"slices"
	"testing"

	"github.com/war-and-code/dircue/pkg/declarations"
)

func TestBuildNormalizesPolyglotComponentsAndLocalRelationships(t *testing.T) {
	projects := []declarations.Project{
		{ID: "package.json", Root: ".", Kind: "npm", Name: "web", References: []declarations.Reference{{Kind: "npm-workspace-member", Target: "ui/package.json", TargetStatus: "present", State: "resolved", Evidence: "package.json"}}},
		{ID: "ui/package.json", Root: "ui", Kind: "npm", Name: "ui", References: []declarations.Reference{{Kind: "npm-local-dependency", Value: "core", Target: "rust/core/Cargo.toml", TargetStatus: "present", State: "resolved", Evidence: "ui/package.json"}}},
		{ID: "go.work", Root: ".", Kind: "go-workspace", References: []declarations.Reference{{Kind: "go-workspace-member", Target: "cmd/go.mod", TargetStatus: "present", State: "resolved", Evidence: "go.work"}}},
		{ID: "cmd/go.mod", Root: "cmd", Kind: "go", Name: "example/cmd"},
		{ID: "rust/Cargo.toml", Root: "rust", Kind: "cargo-workspace", References: []declarations.Reference{{Kind: "cargo-workspace-member", Target: "rust/core/Cargo.toml", TargetStatus: "present", State: "declared", Evidence: "rust/Cargo.toml"}}},
		{ID: "rust/core/Cargo.toml", Root: "rust/core", Kind: "cargo", Name: "core"},
		{ID: "py/pyproject.toml", Root: "py", Kind: "python-workspace", References: []declarations.Reference{{Kind: "uv-workspace-member", Target: "py/lib/pyproject.toml", TargetStatus: "present", State: "resolved", Evidence: "py/pyproject.toml"}}},
		{ID: "py/lib/pyproject.toml", Root: "py/lib", Kind: "python-uv", Name: "lib"},
		{ID: "java/pom.xml", Root: "java", Kind: "maven", References: []declarations.Reference{{Kind: "module", Target: "java/api/pom.xml", TargetStatus: "present", State: "declared", Evidence: "java/pom.xml"}}},
		{ID: "java/api/pom.xml", Root: "java/api", Kind: "maven"},
		{ID: "gradle/settings.gradle", Root: "gradle", Kind: "gradle", References: []declarations.Reference{{Kind: "gradle-module", Target: "gradle/app", TargetStatus: "present", State: "conditional", Condition: "Gradle script evaluation", Evidence: "gradle/settings.gradle"}}},
		{ID: "gradle/app/build.gradle", Root: "gradle/app", Kind: "gradle"},
		{ID: "gradle/gradle.properties", Root: "gradle", Kind: "jvm-configuration"},
		{ID: "dotnet/repo.sln", Root: "dotnet", Kind: "solution", References: []declarations.Reference{{Kind: "solution-member", Target: "dotnet/app.csproj", TargetStatus: "present", State: "declared", Evidence: "dotnet/repo.sln"}}},
		{ID: "dotnet/app.csproj", Root: "dotnet", Kind: "dotnet"},
	}
	f := Build(&declarations.Report{Status: "complete", Projects: projects})
	if len(f.Components) != len(projects)-1 {
		t.Fatalf("components = %d, want %d", len(f.Components), len(projects)-1)
	}
	for _, ecosystem := range []string{"npm", "go", "cargo", "python-uv", "maven", "gradle", "dotnet"} {
		if !slices.ContainsFunc(f.Components, func(c Component) bool { return c.Ecosystem == ecosystem }) {
			t.Errorf("missing ecosystem %q", ecosystem)
		}
	}
	for _, want := range [][3]string{
		{"member_of", "ui/package.json", "package.json"},
		{"depends_on_local", "ui/package.json", "rust/core/Cargo.toml"},
		{"member_of", "cmd/go.mod", "go.work"},
		{"member_of", "rust/core/Cargo.toml", "rust/Cargo.toml"},
		{"member_of", "py/lib/pyproject.toml", "py/pyproject.toml"},
		{"member_of", "java/api/pom.xml", "java/pom.xml"},
		{"member_of", "gradle/app/build.gradle", "gradle/settings.gradle"},
		{"member_of", "dotnet/app.csproj", "dotnet/repo.sln"},
	} {
		if !slices.ContainsFunc(f.Relationships, func(r Relationship) bool { return r.Type == want[0] && r.From == want[1] && r.To == want[2] }) {
			t.Errorf("missing relationship %v", want)
		}
	}
	gradle := relationship(t, f, "gradle-module")
	if gradle.Coverage != "partial" || gradle.Condition == "" {
		t.Fatalf("conditional Gradle observation lost qualification: %+v", gradle)
	}
}

func TestBuildQualifiesUnknownsAndIsDeterministic(t *testing.T) {
	input := &declarations.Report{Status: "partial", Projects: []declarations.Project{
		{ID: "b/package.json", Root: "b", Kind: "npm"},
		{ID: "package.json", Root: ".", Kind: "npm", References: []declarations.Reference{
			{Kind: "npm-workspace-member", Value: "missing", Target: "missing/package.json", TargetStatus: "unresolved", State: "unresolved", Evidence: "package.json"},
			{Kind: "npm-local-dependency", Value: "remote", TargetStatus: "external", State: "declared", Evidence: "package.json"},
		}},
	}}
	a := Build(input)
	slices.Reverse(input.Projects)
	b := Build(input)
	aj, _ := json.Marshal(a)
	bj, _ := json.Marshal(b)
	if string(aj) != string(bj) {
		t.Fatalf("order changed output:\n%s\n%s", aj, bj)
	}
	if a.Coverage.Status != "partial" || len(a.QualifiedReferences) != 2 {
		t.Fatalf("unknown coverage was rendered as absence: %+v", a)
	}
	if a.QualifiedReferences[0].Reason == "" || a.QualifiedReferences[1].Reason == "" {
		t.Fatalf("qualified reasons missing: %+v", a.QualifiedReferences)
	}
}

func TestGoLocalReplacementRequiresMatchingRequirementActivation(t *testing.T) {
	for _, tc := range []struct {
		name, required, requiredState, replacement string
		wantEdge                                   bool
	}{
		{name: "unused replacement", wantEdge: false},
		{name: "direct matching requirement", required: "example.org/lib@v1.2.3", wantEdge: true},
		{name: "different required version", required: "example.org/lib@v2.0.0", wantEdge: false},
		{name: "different required module", required: "example.org/other@v1.2.3", wantEdge: false},
		{name: "unresolved requirement", required: "example.org/lib@v1.2.3", requiredState: "unresolved", wantEdge: false},
		{name: "versionless replacement applies to required module", required: "example.org/lib@v9.0.0", replacement: "example.org/lib", wantEdge: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			replacement := tc.replacement
			if replacement == "" {
				replacement = "example.org/lib@v1.2.3"
			}
			references := []declarations.Reference{{Kind: "go-local-replacement", Value: "../lib", Target: "lib/go.mod", TargetStatus: "present", State: "declared", Condition: replacement, Evidence: "app/go.mod"}}
			if tc.required != "" {
				state := tc.requiredState
				if state == "" {
					state = "declared"
				}
				references = append(references, declarations.Reference{Kind: "go-require", Value: tc.required, State: state, TargetStatus: "external", Evidence: "app/go.mod"})
			}
			fragment := Build(&declarations.Report{Status: "complete", Projects: []declarations.Project{
				{ID: "app/go.mod", Root: "app", Kind: "go", References: references},
				{ID: "lib/go.mod", Root: "lib", Kind: "go"},
			}})
			hasEdge := slices.ContainsFunc(fragment.Relationships, func(r Relationship) bool {
				return r.DeclarationKind == "go-local-replacement" && r.From == "app/go.mod" && r.To == "lib/go.mod" && r.Coverage == "complete"
			})
			if hasEdge != tc.wantEdge {
				t.Fatalf("replacement edge presence = %v, want %v; fragment=%+v", hasEdge, tc.wantEdge, fragment)
			}
			if !tc.wantEdge {
				if !slices.ContainsFunc(fragment.QualifiedReferences, func(q QualifiedReference) bool {
					return q.DeclarationKind == "go-local-replacement" && q.Reason == "go_replacement_activation_unresolved" && q.Target == "lib/go.mod"
				}) {
					t.Fatalf("inactive replacement was not preserved as a qualified local observation: %+v", fragment)
				}
			}
		})
	}
}

func TestGoWorkspaceReplacementRetainsPartialWorkspaceObservation(t *testing.T) {
	fragment := Build(&declarations.Report{Status: "complete", Projects: []declarations.Project{
		{ID: "go.work", Root: ".", Kind: "go-workspace", References: []declarations.Reference{{Kind: "go-local-replacement", Value: "../lib", Target: "lib/go.mod", TargetStatus: "present", State: "declared", Condition: "example.org/lib@v1.2.3", Evidence: "go.work"}}},
		{ID: "lib/go.mod", Root: "lib", Kind: "go"},
	}})
	if !slices.ContainsFunc(fragment.Relationships, func(r Relationship) bool {
		return r.DeclarationKind == "go-local-replacement" && r.From == "go.work" && r.To == "lib/go.mod" && r.Coverage == "partial"
	}) {
		t.Fatalf("go.work replacement must remain an observed but unproven dependency relationship: %+v", fragment)
	}
}

func BenchmarkBuildGoReplacementActivation(b *testing.B) {
	for _, count := range []int{1000, 2000, 4000} {
		b.Run(fmt.Sprintf("replacements_%d", count), func(b *testing.B) {
			references := make([]declarations.Reference, 0, count*2)
			projects := make([]declarations.Project, 0, count+1)
			for i := 0; i < count; i++ {
				module := fmt.Sprintf("example.org/library/%04d", i)
				target := fmt.Sprintf("libs/lib-%04d/go.mod", i)
				references = append(references,
					declarations.Reference{Kind: "go-local-replacement", Value: fmt.Sprintf("../libs/lib-%04d", i), Target: target, State: "declared", Condition: module + "@v1.0.0", Evidence: "app/go.mod"},
					declarations.Reference{Kind: "go-require", Value: module + "@v1.0.0", State: "declared", Evidence: "app/go.mod"},
				)
				projects = append(projects, declarations.Project{ID: target, Root: fmt.Sprintf("libs/lib-%04d", i), Kind: "go", Requirements: []declarations.Requirement{}, References: []declarations.Reference{}})
			}
			projects = append(projects, declarations.Project{ID: "app/go.mod", Root: "app", Kind: "go", References: references})
			report := &declarations.Report{Status: "complete", Projects: projects}
			check := Build(report)
			if len(check.Components) != count+1 || len(check.Relationships) != count || len(check.QualifiedReferences) != 0 {
				b.Fatalf("replacement activation facts differ: %d components, %d relationships, %d qualified", len(check.Components), len(check.Relationships), len(check.QualifiedReferences))
			}
			for _, edge := range check.Relationships {
				if edge.Type != "depends_on_local" || edge.From != "app/go.mod" || edge.DeclarationKind != "go-local-replacement" || edge.Coverage != "complete" {
					b.Fatalf("replacement activation lost its declared dependency: %+v", edge)
				}
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				result := Build(report)
				if len(result.Components) != count+1 || len(result.Relationships) != count || len(result.QualifiedReferences) != 0 {
					b.Fatal("replacement activation populations changed during measurement")
				}
			}
		})
	}
}

func TestPythonWithoutUVIsNotLabeledUV(t *testing.T) {
	fragment := Build(&declarations.Report{Status: "complete", Projects: []declarations.Project{
		{ID: "services/email/requirements.txt", Root: "services/email", Kind: "python"},
		{ID: "workspace/pyproject.toml", Root: "workspace", Kind: "python-workspace"},
	}})
	got := map[string]string{}
	for _, component := range fragment.Components {
		got[component.Key] = component.Ecosystem
	}
	if got["services/email/requirements.txt"] != "python" || got["workspace/pyproject.toml"] != "python-uv" {
		t.Fatalf("Python ecosystem attribution: %+v", got)
	}
}

func TestUnnamedDotnetProjectUsesUniqueCsprojStemUnderGenericSrcRoot(t *testing.T) {
	fragment := Build(&declarations.Report{Status: "complete", Projects: []declarations.Project{
		{ID: "src/cartservice/src/cartservice.csproj", Root: "src/cartservice/src", Kind: "dotnet"},
		{ID: "src/frontend/src/Web.csproj", Root: "src/frontend/src", Kind: "dotnet", Name: "frontend-web"},
		{ID: "services/api/Api.csproj", Root: "services/api", Kind: "dotnet"},
	}})
	got := map[string]string{}
	for _, component := range fragment.Components {
		got[component.Key] = component.Name
	}
	if got["src/cartservice/src/cartservice.csproj"] != "cartservice" {
		t.Fatalf("generic src root did not get its unique project stem: %+v", got)
	}
	if got["src/frontend/src/Web.csproj"] != "frontend-web" {
		t.Fatalf("explicit project name was replaced: %+v", got)
	}
	if got["services/api/Api.csproj"] != "" {
		t.Fatalf("fallback changed a non-generic root: %+v", got)
	}
}

func TestMapFactsRetainsQualifiedReferencesOnComponent(t *testing.T) {
	f := Build(&declarations.Report{Status: "complete", Projects: []declarations.Project{{
		ID: "package.json", Root: ".", Kind: "npm", Name: "app", References: []declarations.Reference{{Kind: "npm-local-dependency", Value: "missing", Target: "missing/package.json", TargetStatus: "missing", State: "missing", Evidence: "package.json"}},
	}}})
	nodes, edges := MapFacts(f)
	if len(nodes) != 1 || len(edges) != 0 || len(nodes[0].Facts) != 1 {
		t.Fatalf("qualified reference not retained as a component fact: nodes=%+v edges=%+v", nodes, edges)
	}
	fact := nodes[0].Facts[0]
	if fact.Coverage.Status != "unknown" || fact.Properties["reason"] != "target_missing" {
		t.Fatalf("qualified reference lost uncertainty: %+v", fact)
	}
}

func TestMapFactsRetainsOnlyPackageRequirements(t *testing.T) {
	f := Build(&declarations.Report{Status: "complete", Projects: []declarations.Project{{
		ID: "package.json", Root: ".", Kind: "npm",
		Requirements: []declarations.Requirement{{Kind: "java-toolchain", Value: "21", State: "declared", Evidence: "package.json"}},
		References:   []declarations.Reference{{Kind: "npm-dependency", Value: "lodash@^4", State: "declared", TargetStatus: "external"}},
	}}})
	nodes, _ := MapFacts(f)
	if len(nodes) != 1 || len(nodes[0].Facts) != 1 {
		t.Fatalf("facts=%+v", nodes)
	}
	fact := nodes[0].Facts[0]
	if fact.Kind != "declared_requirement" || fact.Name != "npm-dependency" || fact.Value != "lodash@^4" || fact.Properties["ecosystem"] != "npm" {
		t.Fatalf("fact=%+v", fact)
	}
	if len(fact.Evidence) != 1 || fact.Evidence[0].Path != "package.json" {
		t.Fatalf("fallback evidence=%+v", fact.Evidence)
	}
}

func TestMapFactsAttributesDotNetPrimaryLanguageFromProjectExtension(t *testing.T) {
	for manifest, want := range map[string]string{
		"src/App.csproj": "C#",
		"src/App.vbproj": "Visual Basic .NET",
		"src/App.fsproj": "F#",
	} {
		fragment := Build(&declarations.Report{Status: "complete", Projects: []declarations.Project{{ID: manifest, Root: "src", Kind: "dotnet"}}})
		nodes, _ := MapFacts(fragment)
		if len(nodes) != 1 || nodes[0].Properties["language"] != want || nodes[0].Properties["language_basis"] != "project_file_extension" || nodes[0].Properties["language_scope"] != "declared_primary_project_language" {
			t.Fatalf("%s properties=%+v", manifest, nodes)
		}
	}
}

func relationship(t *testing.T, f Fragment, declarationKind string) Relationship {
	t.Helper()
	for _, r := range f.Relationships {
		if r.DeclarationKind == declarationKind {
			return r
		}
	}
	t.Fatalf("relationship %q not found", declarationKind)
	return Relationship{}
}

func TestMavenReactorSiblingDependency(t *testing.T) {
	// webapp depends on api — both are modules in the same reactor.
	// The dependency should resolve to a depends_on_local edge.
	report := &declarations.Report{
		Status: "complete",
		Projects: []declarations.Project{
			{
				ID: "pom.xml", Root: ".", Kind: "maven",
				Requirements: []declarations.Requirement{
					{Kind: "maven-groupId", Value: "org.example", State: "declared", Evidence: "pom.xml"},
					{Kind: "maven-artifactId", Value: "parent", State: "declared", Evidence: "pom.xml"},
				},
			},
			{
				ID: "api/pom.xml", Root: "api", Kind: "maven",
				Requirements: []declarations.Requirement{
					{Kind: "maven-groupId", Value: "org.example", State: "declared", Evidence: "api/pom.xml"},
					{Kind: "maven-artifactId", Value: "api", State: "declared", Evidence: "api/pom.xml"},
					{Kind: "maven-version", Value: "1.0.0", State: "declared", Evidence: "api/pom.xml"},
				},
			},
			{
				ID: "webapp/pom.xml", Root: "webapp", Kind: "maven",
				Requirements: []declarations.Requirement{
					{Kind: "maven-groupId", Value: "org.example.web", State: "declared", Evidence: "webapp/pom.xml"},
					{Kind: "maven-artifactId", Value: "webapp", State: "declared", Evidence: "webapp/pom.xml"},
					{Kind: "maven-dependency", Value: "org.example:api:1.0.0", State: "declared", Evidence: "webapp/pom.xml"},
				},
			},
		},
	}
	f := Build(report)
	var siblingEdge *Relationship
	for i := range f.Relationships {
		r := &f.Relationships[i]
		if r.DeclarationKind == "maven-sibling-dependency" {
			siblingEdge = r
		}
	}
	if siblingEdge == nil {
		t.Fatal("expected depends_on_local maven-sibling-dependency edge, got none")
	}
	if siblingEdge.Type != "depends_on_local" {
		t.Errorf("edge type = %q, want depends_on_local", siblingEdge.Type)
	}
	if siblingEdge.From != "webapp/pom.xml" || siblingEdge.To != "api/pom.xml" {
		t.Errorf("edge from=%q to=%q, want webapp/pom.xml → api/pom.xml", siblingEdge.From, siblingEdge.To)
	}
	if siblingEdge.Coverage != "complete" {
		t.Errorf("edge coverage = %q, want complete", siblingEdge.Coverage)
	}
}

func TestMavenReactorSiblingDependencyTestScope(t *testing.T) {
	// A conditional sibling dependency (for example from a Maven profile)
	// should produce coverage=partial.
	report := &declarations.Report{
		Status: "complete",
		Projects: []declarations.Project{
			{
				ID: "api/pom.xml", Root: "api", Kind: "maven",
				Requirements: []declarations.Requirement{
					{Kind: "maven-groupId", Value: "org.example", State: "declared", Evidence: "api/pom.xml"},
					{Kind: "maven-artifactId", Value: "api", State: "declared", Evidence: "api/pom.xml"},
				},
			},
			{
				ID: "test/pom.xml", Root: "test", Kind: "maven",
				Requirements: []declarations.Requirement{
					{Kind: "maven-groupId", Value: "org.example", State: "declared", Evidence: "test/pom.xml"},
					{Kind: "maven-artifactId", Value: "test", State: "declared", Evidence: "test/pom.xml"},
				},
			},
			{
				ID: "webapp/pom.xml", Root: "webapp", Kind: "maven",
				Requirements: []declarations.Requirement{
					{Kind: "maven-groupId", Value: "org.example", State: "declared", Evidence: "webapp/pom.xml"},
					{Kind: "maven-artifactId", Value: "webapp", State: "declared", Evidence: "webapp/pom.xml"},
					// test-scope sibling dependency
					{Kind: "maven-dependency", Value: "org.example:test", State: "conditional", Condition: "test scope", Evidence: "webapp/pom.xml"},
				},
			},
		},
	}
	f := Build(report)
	var siblingEdge *Relationship
	for i := range f.Relationships {
		r := &f.Relationships[i]
		if r.DeclarationKind == "maven-sibling-dependency" {
			siblingEdge = r
		}
	}
	if siblingEdge == nil {
		t.Fatal("expected depends_on_local maven-sibling-dependency edge, got none")
	}
	if siblingEdge.Coverage != "partial" {
		t.Errorf("test-scope sibling edge coverage = %q, want partial", siblingEdge.Coverage)
	}
}

func TestMavenSiblingVersionMustAgree(t *testing.T) {
	for _, tc := range []struct {
		name, dependencyVersion, wantCoverage string
		wantEdge                              bool
	}{
		{"matching", "1.0.0", "complete", true},
		{"different", "2.0.0", "", false},
		{"unversioned", "", "partial", true},
		{"unresolved", "${api.version}", "partial", true},
		{"version_range", "[1.0.0,2.0.0)", "partial", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dep := "org.example:api"
			state := "declared"
			if tc.dependencyVersion != "" {
				dep += ":" + tc.dependencyVersion
			}
			if tc.name == "unresolved" {
				state = "unresolved"
			}
			report := &declarations.Report{Status: "complete", Projects: []declarations.Project{
				{ID: "api/pom.xml", Root: "api", Kind: "maven", Requirements: []declarations.Requirement{
					{Kind: "maven-groupId", Value: "org.example", State: "declared"},
					{Kind: "maven-artifactId", Value: "api", State: "declared"},
					{Kind: "maven-version", Value: "1.0.0", State: "declared"},
				}},
				{ID: "web/pom.xml", Root: "web", Kind: "maven", Requirements: []declarations.Requirement{
					{Kind: "maven-dependency", Value: dep, State: state},
				}},
			}}
			found := false
			for _, edge := range Build(report).Relationships {
				if edge.DeclarationKind != "maven-sibling-dependency" {
					continue
				}
				found = true
				if edge.Coverage != tc.wantCoverage {
					t.Errorf("coverage = %q, want %q", edge.Coverage, tc.wantCoverage)
				}
			}
			if found != tc.wantEdge {
				t.Errorf("edge found = %t, want %t", found, tc.wantEdge)
			}
		})
	}
}

func TestMavenReactorSiblingDependencyGroupIdInheritance(t *testing.T) {
	// A module without its own groupId inherits from the parent.
	report := &declarations.Report{
		Status: "complete",
		Projects: []declarations.Project{
			{
				ID: "core/pom.xml", Root: "core", Kind: "maven",
				Requirements: []declarations.Requirement{
					// No maven-groupId: inherits from parent "org.apache.maven"
					{Kind: "maven-parent", Value: "org.apache.maven:parent:4.0.0", State: "declared", Evidence: "core/pom.xml"},
					{Kind: "maven-artifactId", Value: "maven-core", State: "declared", Evidence: "core/pom.xml"},
				},
			},
			{
				ID: "cli/pom.xml", Root: "cli", Kind: "maven",
				Requirements: []declarations.Requirement{
					{Kind: "maven-parent", Value: "org.apache.maven:parent:4.0.0", State: "declared", Evidence: "cli/pom.xml"},
					{Kind: "maven-artifactId", Value: "maven-cli", State: "declared", Evidence: "cli/pom.xml"},
					// depends on sibling via inherited groupId
					{Kind: "maven-dependency", Value: "org.apache.maven:maven-core", State: "declared", Evidence: "cli/pom.xml"},
				},
			},
		},
	}
	f := Build(report)
	var siblingEdge *Relationship
	for i := range f.Relationships {
		r := &f.Relationships[i]
		if r.DeclarationKind == "maven-sibling-dependency" {
			siblingEdge = r
		}
	}
	if siblingEdge == nil {
		t.Fatal("expected depends_on_local for groupId-inheriting sibling, got none")
	}
	if siblingEdge.From != "cli/pom.xml" || siblingEdge.To != "core/pom.xml" {
		t.Errorf("edge from=%q to=%q, want cli/pom.xml → core/pom.xml", siblingEdge.From, siblingEdge.To)
	}
}

func TestMavenReactorSiblingDependencyAmbiguous(t *testing.T) {
	// Two components with the same coordinates yield no edge.
	report := &declarations.Report{
		Status: "complete",
		Projects: []declarations.Project{
			{
				ID: "a/pom.xml", Root: "a", Kind: "maven",
				Requirements: []declarations.Requirement{
					{Kind: "maven-groupId", Value: "org.example", State: "declared", Evidence: "a/pom.xml"},
					{Kind: "maven-artifactId", Value: "dup", State: "declared", Evidence: "a/pom.xml"},
				},
			},
			{
				ID: "b/pom.xml", Root: "b", Kind: "maven",
				Requirements: []declarations.Requirement{
					{Kind: "maven-groupId", Value: "org.example", State: "declared", Evidence: "b/pom.xml"},
					{Kind: "maven-artifactId", Value: "dup", State: "declared", Evidence: "b/pom.xml"},
				},
			},
			{
				ID: "webapp/pom.xml", Root: "webapp", Kind: "maven",
				Requirements: []declarations.Requirement{
					{Kind: "maven-groupId", Value: "org.example", State: "declared", Evidence: "webapp/pom.xml"},
					{Kind: "maven-artifactId", Value: "webapp", State: "declared", Evidence: "webapp/pom.xml"},
					{Kind: "maven-dependency", Value: "org.example:dup", State: "declared", Evidence: "webapp/pom.xml"},
				},
			},
		},
	}
	f := Build(report)
	for _, r := range f.Relationships {
		if r.DeclarationKind == "maven-sibling-dependency" {
			t.Errorf("expected no sibling edge (ambiguous coordinates), got %+v", r)
		}
	}
}

// The counts are stated in README.md, CHANGELOG.md and docs/releases/1.0.0.md;
// tests/ci/test_doc_counts.py checks those statements. Update them together.
func TestComponentKindCounts(t *testing.T) {
	if !slices.IsSorted(componentKinds) || len(slices.Compact(slices.Clone(componentKinds))) != len(componentKinds) {
		t.Fatalf("componentKinds must be sorted and unique: %v", componentKinds)
	}
	ecosystems := map[string]bool{}
	for _, kind := range componentKinds {
		e := ecosystem(kind)
		if e == "unknown" {
			t.Errorf("component kind %q has no ecosystem", kind)
		}
		ecosystems[e] = true
	}
	if len(componentKinds) != 36 || len(ecosystems) != 27 {
		t.Errorf("component kinds = %d, ecosystem values = %d; want 36 and 27", len(componentKinds), len(ecosystems))
	}
}
