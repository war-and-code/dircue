package declarations

import (
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"
)

func TestNPMDeclarationsDoNotRetainCommandsOrURLs(t *testing.T) {
	d := ParseNPM("app/package.json", []byte(`{
 "name":"@example/app","version":"1.2.3","private":true,
 "packageManager":"npm@11.0.0","engines":{"node":">=22","npm":"^11"},
 "scripts":{"test":"echo SECRET_COMMAND","lint:fix":"some command"},
 "bin":{"example":"bin/app.js"},
 "dependencies":{"library":"^1.0.0","local":"file:../local","remote":"https://user:SECRET_URL@example.org/archive.tgz"},
 "devDependencies":{"dev":"~2.0.0"},
 "optionalDependencies":{"opt":"*"},"peerDependencies":{"peer":">=3"}
}`))
	if !d.Parsed || d.Project.Name != "@example/app" || d.Project.Version != "1.2.3" {
		t.Fatalf("package identity: %+v", d)
	}
	if len(d.Project.References) != 6 {
		t.Fatalf("dependencies: %+v", d.Project.References)
	}
	for _, ref := range d.Project.References {
		if ref.Kind == "npm-local-dependency" && ref.Target != "local/package.json" {
			t.Errorf("local target: %+v", ref)
		}
		if strings.HasPrefix(ref.Value, "library") && (ref.Target != "" || ref.TargetStatus != "external") {
			t.Errorf("name-based local inference: %+v", ref)
		}
	}
	encoded, _ := json.Marshal(d)
	if strings.Contains(string(encoded), "SECRET") || strings.Contains(string(encoded), "some command") || strings.Contains(string(encoded), "user:") {
		t.Fatalf("raw executable or URL retained: %s", encoded)
	}
	gotNames := map[string]bool{}
	for _, iface := range d.Project.Interfaces {
		gotNames[iface.Name] = true
		if iface.Name == "example" && iface.Target != "app/bin/app.js" {
			t.Errorf("entrypoint: %+v", iface)
		}
	}
	for _, name := range []string{"test", "lint:fix", "node", "npm", "example"} {
		if !gotNames[name] {
			t.Errorf("missing interface %q", name)
		}
	}
}

func TestNPMWorkspaceMembershipAndIndependentPackages(t *testing.T) {
	root := ParseNPM("package.json", []byte(`{"name":"root","workspaces":["packages/*","!packages/excluded","missing"],"dependencies":{"member":"^1.0.0"}}`))
	members := map[string]string{
		"packages/a/package.json":             `{"name":"member"}`,
		"packages/b/package.json":             `{"name":"other"}`,
		"packages/excluded/package.json":      `{"name":"excluded"}`,
		"packages/.hidden/package.json":       `{"name":"hidden"}`,
		"unrelated/package.json":              `{"name":"unrelated"}`,
		"node_modules/installed/package.json": `{"name":"installed"}`,
	}
	docs, files := npmFixtureDocuments(root, members)
	ResolveNPM(docs, files)
	want := map[string]string{"packages/a/package.json": "npm-workspace-member/resolved", "packages/b/package.json": "npm-workspace-member/resolved", "packages/excluded/package.json": "npm-workspace-excluded/resolved", "missing/package.json": "npm-workspace-member/missing"}
	for _, ref := range root.Project.References {
		if ref.Kind == "npm-dependency" {
			if ref.Target != "" || ref.TargetStatus != "external" {
				t.Fatalf("nearby name treated as local dependency: %+v", ref)
			}
			continue
		}
		if got := ref.Kind + "/" + ref.State; got != want[ref.Target] {
			t.Errorf("target %s got %s want %s", ref.Target, got, want[ref.Target])
		}
		delete(want, ref.Target)
	}
	if len(want) != 0 {
		t.Errorf("missing relationships: %+v", want)
	}
}

func npmFixtureDocuments(root *Document, members map[string]string) ([]*Document, map[string]bool) {
	docs := []*Document{root}
	files := map[string]bool{root.Project.ID: true}
	for name, content := range members {
		files[name] = true
		docs = append(docs, ParseNPM(name, []byte(content)))
	}
	return docs, files
}

func TestNPMWorkspaceObjectReinclusionAndDuplicateIdentities(t *testing.T) {
	root := ParseNPM("repo/package.json", []byte(`{"workspaces":{"packages":["packages/**","!packages/b/**","packages/b/a"]}}`))
	docs, files := npmFixtureDocuments(root, map[string]string{
		"repo/packages/a/package.json":   `{"name":"duplicate"}`,
		"repo/packages/b/a/package.json": `{"name":"duplicate"}`,
		"repo/packages/b/c/package.json": `{"name":"c"}`,
		"other/packages/a/package.json":  `{"name":"unrelated"}`,
	})
	ResolveNPM(docs, files)
	if len(root.Project.References) != 3 {
		t.Fatalf("reinclusion follows npm removal of the earlier exclusion: %+v", root.Project.References)
	}
	for _, ref := range root.Project.References {
		if strings.HasSuffix(ref.Target, "/a/package.json") && ref.State != "unresolved" {
			t.Errorf("duplicate member identity considered resolved: %+v", ref)
		}
	}
	if len(root.Diagnostics) != 1 || root.Diagnostics[0].Code != "duplicate-npm-workspace-name" {
		t.Fatalf("duplicate diagnostic: %+v", root.Diagnostics)
	}
}

func TestNPMUnknownPatternsPreventGuessedMembership(t *testing.T) {
	for _, input := range []string{
		`{"workspaces":["packages/*","packages/{a,b}"]}`,
		`{"workspaces":["../external"]}`,
		`{"workspaces":["/absolute"]}`,
		`{"workspaces":["C:/SECRET_PATH"]}`,
		`{"workspaces":["packages/@(a|b)"]}`,
		`{"workspaces":[2]}`,
		`{"workspaces":true}`,
	} {
		root := ParseNPM("package.json", []byte(input))
		docs, files := npmFixtureDocuments(root, map[string]string{"packages/a/package.json": `{"name":"a"}`})
		ResolveNPM(docs, files)
		if len(root.Diagnostics) == 0 || len(root.Project.References) != 0 {
			t.Fatalf("unsupported pattern inferred membership: %+v", root)
		}
	}
}

func TestNPMLimitsAndInstalledWorkspaceExclusion(t *testing.T) {
	patterns := make([]string, MaxPatterns+1)
	for i := range patterns {
		patterns[i] = fmt.Sprintf("packages/p%d", i)
	}
	content, _ := json.Marshal(map[string]any{"workspaces": patterns})
	d := ParseNPM("package.json", content)
	ResolveNPM([]*Document{d}, map[string]bool{"package.json": true})
	if len(d.Diagnostics) == 0 || len(d.Project.References) != 0 {
		t.Fatal("pattern cap did not stop expansion")
	}
	root := ParseNPM("package.json", []byte(`{"workspaces":["node_modules/installed"]}`))
	ResolveNPM([]*Document{root}, map[string]bool{"package.json": true, "node_modules/installed/package.json": true})
	if len(root.Project.References) != 0 {
		t.Fatal("installed dependency became a workspace member")
	}
	scripts := map[string]string{}
	for i := 0; i < MaxObservationsPerManifest+20; i++ {
		scripts[fmt.Sprintf("script%d", i)] = "secret command"
	}
	content, _ = json.Marshal(map[string]any{"scripts": scripts})
	d = ParseNPM("package.json", content)
	if len(d.Project.Interfaces)+len(d.Project.Requirements) > MaxObservationsPerManifest || len(d.Diagnostics) != 1 {
		t.Fatalf("observation cap: %+v", d)
	}
}

func TestNPMMatchBudgetDoesNotKeepPartlyExcludedMembers(t *testing.T) {
	patterns := make([]string, 64)
	for i := range patterns {
		patterns[i] = fmt.Sprintf("packages/*%d*", i)
	}
	patterns = append(patterns, "!packages/app1")
	content, _ := json.Marshal(map[string]any{"workspaces": patterns})
	root := ParseNPM("package.json", content)
	files := map[string]bool{"package.json": true}
	for i := 0; i < 2000; i++ {
		files[fmt.Sprintf("packages/app%d/package.json", i)] = true
	}
	ResolveNPM([]*Document{root}, files)
	if len(root.Project.References) != 0 {
		t.Fatal("partial workspace expansion retained membership")
	}
	found := false
	for _, diagnostic := range root.Diagnostics {
		found = found || diagnostic.Code == "npm-workspace-match-limit"
	}
	if !found {
		t.Fatal("workspace check cap missing diagnostic")
	}
}

func TestNPMInvalidAndPartialTargets(t *testing.T) {
	root := ParseNPM("package.json", []byte(`{"workspaces":["packages/*"],"dependencies":{"missing":"file:./missing","broken":"file:./packages/broken","external":"file:/SECRET_PATH"}}`))
	docs, files := npmFixtureDocuments(root, map[string]string{"packages/broken/package.json": `{"name":"a","name":"b"}`, "packages/good/package.json": `{"name":"good"}`})
	ResolveNPM(docs, files)
	for _, ref := range root.Project.References {
		switch ref.Target {
		case "packages/broken/package.json":
			if ref.State != "unresolved" || ref.TargetStatus != "present" {
				t.Errorf("broken target: %+v", ref)
			}
		case "missing/package.json":
			if ref.State != "missing" {
				t.Errorf("missing target: %+v", ref)
			}
		case "":
			if ref.State != "unresolved" || ref.TargetStatus != "external" {
				t.Errorf("external target: %+v", ref)
			}
		}
	}
	encoded, _ := json.Marshal(docs)
	if strings.Contains(string(encoded), "SECRET_PATH") {
		t.Fatal("external absolute path was retained")
	}
	for _, input := range []string{`{"name":"a","name":"b"}`, `[]`, `null`, `{`, `{"name":` + strings.Repeat("[", 70) + `0` + strings.Repeat("]", 70) + `}`} {
		d := ParseNPM("package.json", []byte(input))
		if d.Parsed || len(d.Diagnostics) == 0 || len(d.Project.Requirements) != 0 {
			t.Fatalf("invalid JSON accepted: %+v", d)
		}
	}
	if ParseNPM("node_modules/foo/package.json", []byte(`{}`)) != nil || ParseNPM("package-lock.json", nil) != nil {
		t.Fatal("noncandidate path parsed")
	}
}

func TestNPMWorkspaceOrderDeterministic(t *testing.T) {
	build := func(reverse bool) *Project {
		root := ParseNPM("package.json", []byte(`{"workspaces":["packages/*"]}`))
		a := ParseNPM("packages/a/package.json", []byte(`{"name":"a"}`))
		b := ParseNPM("packages/b/package.json", []byte(`{"name":"b"}`))
		docs := []*Document{root, a, b}
		if reverse {
			docs = []*Document{b, a, root}
		}
		ResolveNPM(docs, map[string]bool{"package.json": true, "packages/a/package.json": true, "packages/b/package.json": true})
		return root.Project
	}
	if !reflect.DeepEqual(build(false), build(true)) {
		t.Fatal("inventory order changed workspace output")
	}
}

func TestNPMMatchDotDirectories(t *testing.T) {
	for _, tc := range []struct {
		pattern, candidate string
		want               bool
	}{
		{"packages/*", "packages/.hidden", false},
		{"packages/.*", "packages/.hidden", true},
		{"**/app", ".hidden/app", false},
		{"**/.hidden/app", "nested/.hidden/app", true},
		{"packages/**", "packages", true},
		{"packages/[ab]", "packages/a", true},
	} {
		got, err := npmMatch(tc.pattern, tc.candidate)
		if err != nil || got != tc.want {
			t.Errorf("%s vs %s = %v, %v", tc.pattern, tc.candidate, got, err)
		}
	}
}

// Expected members were independently checked with npm 11.16.0's
// @npmcli/map-workspaces 5.0.3, without installing or executing project code.
func TestNPMWorkspaceOracleCases(t *testing.T) {
	for _, tc := range []struct {
		name       string
		workspaces any
		members    []string
	}{
		{"simple", []string{"packages/*"}, []string{"packages/a/package.json"}},
		{"object", map[string]any{"packages": []string{"packages/*"}}, []string{"packages/a/package.json"}},
		{"exclusion", []string{"packages/**", "!packages/b/**"}, []string{"packages/a/package.json"}},
		{"reinclusion", []string{"packages/**", "!packages/b/**", "packages/b/a"}, []string{"packages/a/package.json", "packages/b/a/package.json", "packages/b/c/package.json"}},
		{"explicit-hidden", []string{"packages/.*"}, []string{"packages/.hidden/package.json"}},
		{"installed", []string{"node_modules/installed"}, []string{}},
		{"nested", []string{"packages/**"}, []string{"packages/a/package.json", "packages/b/a/package.json", "packages/b/c/package.json"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			content, _ := json.Marshal(map[string]any{"workspaces": tc.workspaces})
			root := ParseNPM("package.json", content)
			docs, files := npmFixtureDocuments(root, map[string]string{
				"packages/a/package.json":             `{"name":"a"}`,
				"packages/b/a/package.json":           `{"name":"ba"}`,
				"packages/b/c/package.json":           `{"name":"bc"}`,
				"packages/.hidden/package.json":       `{"name":"hidden"}`,
				"node_modules/installed/package.json": `{"name":"installed"}`,
			})
			ResolveNPM(docs, files)
			actual := []string{}
			for _, ref := range root.Project.References {
				if ref.Kind == "npm-workspace-member" && ref.State == "resolved" {
					actual = append(actual, ref.Target)
				}
			}
			slices.Sort(actual)
			if !reflect.DeepEqual(actual, tc.members) {
				t.Fatalf("members %v != upstream %v", actual, tc.members)
			}
		})
	}
}

func FuzzParseNPMDeclarations(f *testing.F) {
	f.Add(`{"name":"app","workspaces":["packages/*"]}`)
	f.Add(`{"name":"a","name":"b"}`)
	f.Fuzz(func(t *testing.T, input string) {
		d := ParseNPM("package.json", []byte(input))
		if d == nil || d.Project == nil {
			t.Fatal("recognized path lost")
		}
		ResolveNPM([]*Document{d}, map[string]bool{"package.json": true})
		if _, err := json.Marshal(d.Project); err != nil {
			t.Fatal(err)
		}
	})
}

func TestNPMSharedBudgetIncludesOutsidePrefixChecks(t *testing.T) {
	first := ParseNPM("first/package.json", []byte(`{"workspaces":["packages/*"]}`))
	second := ParseNPM("second/package.json", []byte(`{"workspaces":["packages/*"]}`))
	paths := []string{"unrelated/a/package.json", "unrelated/b/package.json"}
	budget := &npmResolutionBudget{remaining: 2}
	npmResolveWorkspace(first, first.Data.(*npmDeclarationData), map[string]*Document{}, paths, map[string]bool{}, budget)
	if budget.remaining != 0 {
		t.Fatal("out-of-prefix paths bypassed the work budget")
	}
	npmResolveWorkspace(second, second.Data.(*npmDeclarationData), map[string]*Document{}, paths, map[string]bool{}, budget)
	found := false
	for _, diagnostic := range second.Diagnostics {
		found = found || diagnostic.Code == "npm-resolution-limit"
	}
	if !found || len(second.Project.References) != 0 {
		t.Fatal("shared budget did not stop later workspace expansion")
	}
}

// @npmcli/map-workspaces 5.0.3 reports EDUPLICATEWORKSPACE for these two
// nameless packages: both receive the directory-derived identity "widget".
func TestNPMDirectoryDerivedIdentityConflicts(t *testing.T) {
	for _, members := range []map[string]string{
		{"one/widget/package.json": `{}`, "two/widget/package.json": `{}`},
		{"one/widget/package.json": `{}`, "two/other/package.json": `{"name":"widget"}`},
		{"one/@scope/widget/package.json": `{}`, "two/other/package.json": `{"name":"@scope/widget"}`},
	} {
		root := ParseNPM("package.json", []byte(`{"workspaces":["**"]}`))
		docs, files := npmFixtureDocuments(root, members)
		ResolveNPM(docs, files)
		for _, ref := range root.Project.References {
			if ref.Kind == "npm-workspace-member" && ref.State != "unresolved" {
				t.Fatalf("directory identity conflict resolved: %+v", ref)
			}
		}
		found := false
		for _, diag := range root.Diagnostics {
			found = found || diag.Code == "duplicate-npm-workspace-name"
		}
		if !found {
			t.Fatal("directory-derived duplicate identity was not diagnosed")
		}
		for _, d := range docs[1:] {
			if members[d.Project.ID] == `{}` && d.Project.Name != "" {
				t.Fatal("directory fallback emitted as declared package name")
			}
		}
	}
}
