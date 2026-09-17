package projects

import (
	"encoding/json"
	"fmt"
	"reflect"
	"testing"
)

func TestCollectorNearestUniqueOwnershipAndComposition(t *testing.T) {
	c := New("directory", "")
	add := func(name string, size int64, role string, doc Document) { c.Add(name, size, role, doc) }
	add("root.go", 10, "source", Document{})
	add("app/app.csproj", 20, "configuration", Document{Projects: []Project{{ID: "app/app.csproj", Root: "app", Kind: "dotnet"}}})
	add("app/main.cs", 30, "source", Document{})
	add("app/tests/test.cs", 40, "test", Document{})
	add("app/nested/pom.xml", 50, "configuration", Parse("app/nested/pom.xml", []byte(`<project><artifactId>child</artifactId></project>`)))
	add("app/nested/Main.java", 60, "source", Document{})
	add("ambiguous/a.csproj", 70, "configuration", Document{Projects: []Project{{ID: "ambiguous/a.csproj", Root: "ambiguous", Kind: "dotnet"}}})
	add("ambiguous/b.csproj", 80, "configuration", Document{Projects: []Project{{ID: "ambiguous/b.csproj", Root: "ambiguous", Kind: "dotnet"}}})
	add("ambiguous/main.cs", 90, "source", Document{})
	c.Omit() // An omitted symlink is not present inventory and has no inferred owner.
	r := c.Finish()
	if r.Status != "partial" || r.OmittedFiles != 1 {
		t.Fatalf("coverage: %+v", r)
	}
	if r.Unassigned != (Counts{Files: 1, Bytes: 10}) || r.Ambiguous != (Counts{Files: 3, Bytes: 240}) {
		t.Fatalf("ownership: %+v", r)
	}
	for _, p := range r.Projects {
		switch p.ID {
		case "app/app.csproj":
			if p.Files != 3 || p.Bytes != 90 {
				t.Errorf("outer: %+v", p)
			}
		case "app/nested/pom.xml":
			if p.Files != 2 || p.Bytes != 110 {
				t.Errorf("nested: %+v", p)
			}
		default:
			if p.Files != 0 || p.Bytes != 0 {
				t.Errorf("ambiguous double count: %+v", p)
			}
		}
	}
	assertCollectorCounts(t, r, 9, 450)
	before, _ := json.Marshal(r)
	after, _ := json.Marshal(c.Finish())
	if string(before) != string(after) {
		t.Fatal("Finish counted inventory twice")
	}
}

func TestCollectorGenericCoalescingAndNonOwningGroups(t *testing.T) {
	c := New("directory", "")
	for _, name := range []string{"python/setup.py", "python/requirements.txt", "python/pyproject.toml"} {
		c.Add(name, 10, "configuration", Parse(name, nil))
	}
	c.Add("python/code.py", 20, "source", Document{})
	c.Add("go.work", 4, "configuration", Parse("go.work", nil))
	c.Add("root.sln", 5, "configuration", Document{Projects: []Project{{ID: "root.sln", Root: ".", Kind: "solution"}}})
	c.Add("unassigned.txt", 6, "data", Document{})
	r := c.Finish()
	if len(r.Projects) != 3 {
		t.Fatalf("projects: %+v", r.Projects)
	}
	var python Project
	for _, p := range r.Projects {
		if p.Kind == "python" {
			python = p
		} else if p.Files != 0 {
			t.Fatalf("group owns files: %+v", p)
		}
	}
	if python.ID != "python/pyproject.toml" || len(python.Evidence) != 3 || len(python.Requirements) != 3 || python.Files != 4 {
		t.Fatalf("coalescing: %+v", python)
	}
	if r.Unassigned.Files != 3 {
		t.Fatalf("group ownership: %+v", r.Unassigned)
	}
	assertCollectorCounts(t, r, 7, 65)
}

func TestCollectorReferencesUseSelectedInventory(t *testing.T) {
	c := New("directory", "")
	refs := []Reference{
		{Kind: "gradle-module", Target: "module", Value: ":module", State: "conditional", Condition: "Gradle script evaluation"},
		{Kind: "gradle-module", Target: "missing", Value: ":missing", State: "conditional"},
		{Kind: "project-reference", Target: "module/present.csproj", State: "declared"},
		{Kind: "project-reference", Target: "symlink.csproj", State: "declared"},
		{Kind: "project-reference", Target: "module/present.csproj", State: "unresolved"},
		{Kind: "project-reference", Target: "module/../module/present.csproj", State: "declared"},
		{Kind: "project-reference", Value: "$(custom)", State: "unresolved"},
	}
	c.Add("settings.gradle", 1, "configuration", Document{References: refs})
	c.Add("module/present.csproj", 2, "configuration", Document{})
	c.Omit()
	r := c.Finish()
	actual := r.Configurations[0].References
	for _, ref := range actual {
		switch {
		case ref.Value == ":module":
			if ref.TargetStatus != "present" || ref.State != "conditional" {
				t.Errorf("present directory: %+v", ref)
			}
		case ref.Value == ":missing":
			if ref.TargetStatus != "missing" || ref.State != "conditional" {
				t.Errorf("missing conditional directory: %+v", ref)
			}
		case ref.Target == "symlink.csproj":
			if ref.TargetStatus != "missing" || ref.State != "missing" {
				t.Errorf("omitted target: %+v", ref)
			}
		case ref.Target == "":
			if ref.TargetStatus != "unresolved" || ref.State != "unresolved" {
				t.Errorf("dynamic target: %+v", ref)
			}
		case ref.State == "unresolved":
			if ref.TargetStatus != "present" {
				t.Errorf("unresolved preserved: %+v", ref)
			}
		default:
			if ref.TargetStatus != "present" || ref.State != "resolved" || ref.Target != "module/present.csproj" {
				t.Errorf("file target: %+v", ref)
			}
		}
	}
}

func TestCollectorRejectsEscapingReferenceTargets(t *testing.T) {
	for _, target := range []string{"../outside", "/absolute", "C:/outside", "dir\\outside", "dir/../../outside", "bad\x00path"} {
		c := New("directory", "")
		c.Add("project.csproj", 1, "configuration", Document{References: []Reference{{Target: target, State: "declared", Evidence: "project.csproj"}}})
		r := c.Finish()
		ref := r.Configurations[0].References[0]
		if ref.Target != "" || ref.TargetStatus != "unresolved" || ref.State != "unresolved" || r.Status != "partial" || len(r.Diagnostics) != 1 {
			t.Fatalf("target %q: %+v", target, r)
		}
	}
}

func TestCollectorConfigurationFamiliesAndAncestry(t *testing.T) {
	c := New("directory", "")
	configuration := func(name string) {
		c.Add(name, 1, "configuration", Document{Requirements: []Requirement{{Kind: "test", Value: "declared", State: "declared", Evidence: name}}})
	}
	for _, name := range []string{"global.json", "Directory.Build.props", "toolchains.xml", "settings.gradle.kts", "gradle/wrapper/gradle-wrapper.properties", "nested/Directory.Build.props", "sibling/Directory.Build.props", "nested/gradle.properties"} {
		configuration(name)
	}
	for _, p := range []Project{{ID: "nested/a.csproj", Root: "nested", Kind: "dotnet"}, {ID: "nested/pom.xml", Root: "nested", Kind: "maven"}, {ID: "nested/build.gradle.kts", Root: "nested", Kind: "gradle"}, {ID: "nested/pyproject.toml", Root: "nested", Kind: "python"}} {
		c.Add(p.ID, 1, "configuration", Document{Projects: []Project{p}})
	}
	r := c.Finish()
	wants := map[string][]string{"dotnet": {"Directory.Build.props", "global.json", "nested/Directory.Build.props"}, "maven": {"toolchains.xml"}, "gradle": {"gradle/wrapper/gradle-wrapper.properties", "nested/gradle.properties", "settings.gradle.kts"}, "python": {}}
	for _, p := range r.Projects {
		if !reflect.DeepEqual(p.ConfigurationCandidates, wants[p.Kind]) {
			t.Errorf("%s candidates: %v", p.Kind, p.ConfigurationCandidates)
		}
	}
}

func TestCollectorDeterministicObservationsAndDiagnostics(t *testing.T) {
	build := func(reverse bool) *Report {
		c := New("directory", "")
		requirements := []Requirement{{Kind: "z", Value: "last", Evidence: "p"}, {Kind: "a", Value: "first", Evidence: "p"}}
		references := []Reference{{Kind: "z", Value: "last", State: "unresolved"}, {Kind: "a", Value: "first", State: "unresolved"}}
		if reverse {
			requirements[0], requirements[1] = requirements[1], requirements[0]
			references[0], references[1] = references[1], references[0]
		}
		c.Add("p", 1, "data", Document{Projects: []Project{{ID: "p", Root: ".", Kind: "generic", Requirements: requirements, References: references, Evidence: []string{"p"}}}})
		c.Add("unknown.txt", 1, "data", Document{Diagnostics: []Diagnostic{{Path: "unknown.txt", Code: "read-error", Message: "unreadable"}}})
		return c.Finish()
	}
	first, second := build(false), build(true)
	a, _ := json.Marshal(first)
	b, _ := json.Marshal(second)
	if string(a) != string(b) {
		t.Fatalf("unstable observations:\n%s\n%s", a, b)
	}
	if first.Status != "partial" {
		t.Fatal("unknown-file diagnostic did not make report partial")
	}
}

func TestCollectorThousandProjects(t *testing.T) {
	c := New("directory", "")
	for i := 999; i >= 0; i-- {
		root := fmt.Sprintf("services/%04d", i)
		manifest := root + "/pom.xml"
		c.Add(root+"/toolchains.xml", 1, "configuration", Document{Requirements: []Requirement{{Kind: "java-toolchain", Value: "21", State: "declared"}}})
		c.Add(manifest, 2, "configuration", Document{Projects: []Project{{ID: manifest, Root: root, Kind: "maven"}}})
		for j := 0; j < 5; j++ {
			c.Add(fmt.Sprintf("%s/src/deep/%d.java", root, j), 3, "source", Document{})
		}
	}
	r := c.Finish()
	if len(r.Projects) != 1000 {
		t.Fatalf("projects: %d", len(r.Projects))
	}
	for _, p := range r.Projects {
		if p.Files != 7 || p.Bytes != 18 || len(p.ConfigurationCandidates) != 1 {
			t.Fatalf("indexed attribution: %+v", p)
		}
	}
	assertCollectorCounts(t, r, 7000, 18000)
}

func assertCollectorCounts(t *testing.T, r *Report, files, bytes int64) {
	t.Helper()
	assigned := r.Unassigned
	assigned.Files += r.Ambiguous.Files
	assigned.Bytes += r.Ambiguous.Bytes
	for _, p := range r.Projects {
		assigned.Files += p.Files
		assigned.Bytes += p.Bytes
	}
	var roles Counts
	for _, role := range r.Composition {
		roles.Files += role.Files
		roles.Bytes += role.Bytes
	}
	want := Counts{Files: files, Bytes: bytes}
	if assigned != want || roles != want {
		t.Fatalf("count invariant: ownership=%+v composition=%+v expected=%+v", assigned, roles, want)
	}
}

func TestCollectorPreservesConfigurationPresenceWithoutObservations(t *testing.T) {
	c := New("directory", "")
	inputs := map[string]string{
		"Directory.Build.props":         "<Project><PropertyGroup><DefineConstants>EXAMPLE</DefineConstants></PropertyGroup></Project>",
		"NuGet.Config":                  "<configuration />",
		"global.json":                   "{}",
		"gradle.properties":             "org.gradle.jvmargs=-Xmx1g\n",
		"child/Directory.Build.targets": "<not-a-project />",
	}
	for name, content := range inputs {
		c.Add(name, int64(len(content)), "configuration", Parse(name, []byte(content)))
	}
	for _, project := range []Project{{ID: "child/a.csproj", Root: "child", Kind: "dotnet"}, {ID: "child/build.gradle", Root: "child", Kind: "gradle"}} {
		c.Add(project.ID, 1, "configuration", Document{Projects: []Project{project}})
	}
	r := c.Finish()
	if len(r.Configurations) != len(inputs) || r.Status != "partial" || len(r.Diagnostics) != 1 {
		t.Fatalf("configuration presence: %+v", r)
	}
	for _, cfg := range r.Configurations {
		if cfg.Requirements == nil || cfg.References == nil {
			t.Fatalf("null observations: %+v", cfg)
		}
	}
	for _, project := range r.Projects {
		want := 4
		if project.Kind == "gradle" {
			want = 1
		}
		if len(project.ConfigurationCandidates) != want {
			t.Fatalf("candidate presence: %+v", project)
		}
	}
}
