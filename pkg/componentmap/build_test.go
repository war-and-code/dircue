package componentmap

import (
	"encoding/json"
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
