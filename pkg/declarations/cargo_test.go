package declarations

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func cargoTestDocuments(t *testing.T, source map[string]string) ([]*Document, map[string]bool) {
	t.Helper()
	names := []string{}
	for name := range source {
		names = append(names, name)
	}
	slices.Sort(names)
	docs := []*Document{}
	files := map[string]bool{}
	for _, name := range names {
		files[name] = true
		if d := ParseCargo(name, []byte(source[name])); d != nil {
			docs = append(docs, d)
		}
	}
	ResolveCargo(docs, files)
	return docs, files
}
func cargoTestProject(t *testing.T, docs []*Document, id string) *Document {
	t.Helper()
	for _, d := range docs {
		if d.Project.ID == id {
			return d
		}
	}
	t.Fatalf("missing %s", id)
	return nil
}
func cargoTestReference(d *Document, kind, target string) bool {
	for _, ref := range d.Project.References {
		if ref.Kind == kind && ref.Target == target {
			return true
		}
	}
	return false
}
func cargoTestDiagnostic(d *Document, code string) bool {
	for _, diag := range d.Diagnostics {
		if diag.Code == code {
			return true
		}
	}
	return false
}
func cargoTestRequirement(d *Document, kind, value, evidence string) bool {
	for _, req := range d.Project.Requirements {
		if req.Kind == kind && req.Value == value && req.Evidence == evidence {
			return true
		}
	}
	return false
}

func TestCargoWorkspaceFixture(t *testing.T) {
	source := map[string]string{}
	root := "testdata/cargo/workspace"
	err := filepath.WalkDir(root, func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		content, err := os.ReadFile(name)
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(root, name)
		if err != nil {
			return err
		}
		source[filepath.ToSlash(relative)] = string(content)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	docs, _ := cargoTestDocuments(t, source)
	ws := cargoTestProject(t, docs, "Cargo.toml")
	app := cargoTestProject(t, docs, "crates/app/Cargo.toml")
	if ws.Project.Kind != "cargo-workspace" {
		t.Fatalf("kind %q", ws.Project.Kind)
	}
	for _, edge := range [][2]string{{"cargo-workspace-member", "crates/app/Cargo.toml"}, {"cargo-workspace-path-member", "crates/common/Cargo.toml"}, {"cargo-workspace-exclude", "crates/ignored/Cargo.toml"}, {"cargo-default-member", "crates/app/Cargo.toml"}} {
		if !cargoTestReference(ws, edge[0], edge[1]) {
			t.Errorf("missing %v in %+v", edge, ws.Project.References)
		}
	}
	if cargoTestReference(ws, "cargo-workspace-member", "crates/ignored/Cargo.toml") {
		t.Fatal("excluded member admitted")
	}
	if app.Project.Version != "1.2.3" || !cargoTestRequirement(app, "cargo-edition", "2021", "Cargo.toml") {
		t.Fatalf("inheritance: %+v", app.Project)
	}
	found := false
	for _, ref := range app.Project.References {
		if ref.Kind == "cargo-path-dependency" && ref.Evidence == "Cargo.toml" {
			found = true
			if ref.Target != "crates/common/Cargo.toml" || ref.TargetStatus != "present" || !strings.Contains(ref.Condition, "features=base,extra") {
				t.Errorf("inherited dependency %+v", ref)
			}
		}
	}
	if !found {
		t.Fatal("inherited path dependency missing")
	}
	for _, d := range docs {
		if len(d.Diagnostics) > 0 {
			t.Errorf("unexpected diagnostics %s: %+v", d.Project.ID, d.Diagnostics)
		}
	}
	var target, build bool
	for _, iface := range app.Project.Interfaces {
		switch iface.Kind {
		case "cargo-bin":
			target = iface.Target == "crates/app/src/main.rs" && iface.State == "conditional" && iface.Condition == "required-features=cli"
		case "cargo-build-script":
			build = iface.Target == "crates/app/scripts/build.rs" && iface.State == "declared"
		}
	}
	if !target || !build {
		t.Fatalf("interfaces %+v", app.Project.Interfaces)
	}
}

func TestCargoRootPackageAndIndependentNestedProjects(t *testing.T) {
	docs, _ := cargoTestDocuments(t, map[string]string{
		"Cargo.toml":             "[package]\nname='root'\nversion='1.0.0'\n[workspace]\nmembers=['crates/*']\nexclude=['crates/skip']\n",
		"crates/a/Cargo.toml":    "[package]\nname='a'\nversion='1.0.0'\n",
		"crates/skip/Cargo.toml": "[package]\nname='skip'\nversion='1.0.0'\n",
		"independent/Cargo.toml": "[package]\nname='independent'\nversion='1.0.0'\n[workspace]\n",
		"unlisted/Cargo.toml":    "[package]\nname='unlisted'\nversion='1.0.0'\n",
	})
	root := cargoTestProject(t, docs, "Cargo.toml")
	if root.Project.Kind != "cargo" || !cargoTestReference(root, "cargo-workspace-member", "Cargo.toml") || !cargoTestReference(root, "cargo-workspace-member", "crates/a/Cargo.toml") {
		t.Fatalf("root %+v", root.Project)
	}
	if !cargoTestRequirement(root, "cargo-default-selection", "root-package", "Cargo.toml") {
		t.Fatal("missing root default selection")
	}
	if cargoTestReference(root, "cargo-workspace-member", "independent/Cargo.toml") {
		t.Fatal("containment became membership")
	}
	if !cargoTestDiagnostic(cargoTestProject(t, docs, "unlisted/Cargo.toml"), "cargo-unlisted-nested-package") {
		t.Fatal("missing unlisted diagnostic")
	}
}

func TestCargoExternalMissingAndAmbiguousInheritance(t *testing.T) {
	docs, _ := cargoTestDocuments(t, map[string]string{
		"one/Cargo.toml":    "[workspace]\nmembers=['../shared']\n[workspace.package]\nversion='1.0.0'\n",
		"two/Cargo.toml":    "[workspace]\nmembers=['../shared']\n[workspace.package]\nversion='2.0.0'\n",
		"shared/Cargo.toml": "[package]\nname='shared'\nversion.workspace=true\n[dependencies]\nprivate={path='/home/private/credential'}\nmissing={path='../missing'}\n",
	})
	shared := cargoTestProject(t, docs, "shared/Cargo.toml")
	if shared.Project.Version != "" || !cargoTestDiagnostic(shared, "cargo-ambiguous-workspace") || !cargoTestDiagnostic(shared, "cargo-inheritance-unresolved") {
		t.Fatalf("ambiguous %+v", shared)
	}
	encoded, _ := json.Marshal(shared.Project)
	if strings.Contains(string(encoded), "/home/private") {
		t.Fatalf("external host path leaked: %s", encoded)
	}
	var external, missing bool
	for _, ref := range shared.Project.References {
		if ref.TargetStatus == "external" && ref.Value == "[outside-selected-root]" {
			external = true
		}
		if ref.Target == "missing/Cargo.toml" && ref.TargetStatus == "missing" {
			missing = true
		}
	}
	if !external || !missing {
		t.Fatalf("references %+v", shared.Project.References)
	}
}

func TestCargoWorkspacePointerDoesNotInventMembership(t *testing.T) {
	docs, _ := cargoTestDocuments(t, map[string]string{
		"Cargo.toml":          "[workspace]\nmembers=['included']\n[workspace.package]\nversion='1.0.0'\n",
		"included/Cargo.toml": "[package]\nname='included'\nworkspace='..'\nversion.workspace=true\n",
		"unlisted/Cargo.toml": "[package]\nname='unlisted'\nworkspace='..'\nversion.workspace=true\n",
	})
	included := cargoTestProject(t, docs, "included/Cargo.toml")
	unlisted := cargoTestProject(t, docs, "unlisted/Cargo.toml")
	if included.Project.Version != "1.0.0" {
		t.Fatal("explicit member inheritance absent")
	}
	if unlisted.Project.Version != "" || !cargoTestDiagnostic(unlisted, "cargo-workspace-membership-unresolved") {
		t.Fatalf("pointer implied membership: %+v", unlisted)
	}
}

func TestCargoCyclesConditionalReferencesAndRedaction(t *testing.T) {
	docs, _ := cargoTestDocuments(t, map[string]string{
		"Cargo.toml":   "[workspace]\nmembers=['a']\n",
		"a/Cargo.toml": "[package]\nname='a'\n[dependencies]\nb={path='../b',optional=true,features=['feat']}\nremote={git='https://user:secret@example.test/project'}\n",
		"b/Cargo.toml": "[package]\nname='b'\n[dependencies]\na={path='../a'}\n",
	})
	root := cargoTestProject(t, docs, "Cargo.toml")
	a := cargoTestProject(t, docs, "a/Cargo.toml")
	if !cargoTestReference(root, "cargo-workspace-path-member", "b/Cargo.toml") {
		t.Fatal("transitive member absent")
	}
	for _, ref := range a.Project.References {
		if ref.Target == "b/Cargo.toml" && ref.State != "conditional" {
			t.Fatalf("optional edge %+v", ref)
		}
	}
	for _, d := range docs {
		encoded, _ := json.Marshal(d.Project)
		if strings.Contains(string(encoded), "secret") || strings.Contains(string(encoded), "example.test") {
			t.Fatalf("URL leaked: %s", encoded)
		}
	}
}

func TestCargoInvalidAndBoundedInputs(t *testing.T) {
	for _, input := range []string{"[package]\nname='one'\nname='two'", "[package\n", strings.Repeat(" ", int(MaxManifestBytes)+1)} {
		d := ParseCargo("Cargo.toml", []byte(input))
		if d.Parsed || !cargoTestDiagnostic(d, "invalid-cargo-toml") {
			t.Fatalf("invalid input accepted %+v", d)
		}
	}
	for _, input := range []string{"package = 1", "[package]\nname=1\n", "[workspace]\nmembers='crates/*'\n", "[package]\nname='a'\n[dependencies]\nb={path=1}\n"} {
		d := ParseCargo("Cargo.toml", []byte(input))
		if len(d.Diagnostics) == 0 {
			t.Fatalf("no wrong-type diagnostic: %q", input)
		}
	}
	patterns := make([]string, MaxPatterns+1)
	for i := range patterns {
		patterns[i] = "'crates/*'"
	}
	d := ParseCargo("Cargo.toml", []byte("[workspace]\nmembers=["+strings.Join(patterns, ",")+"]"))
	if !cargoTestDiagnostic(d, "cargo-pattern-limit") || len(d.Data.(*cargoData).members) != MaxPatterns {
		t.Fatalf("pattern budget %+v", d)
	}
}

func TestCargoGlobAndDefaultMemberFailures(t *testing.T) {
	docs, _ := cargoTestDocuments(t, map[string]string{
		"Cargo.toml":                 "[workspace]\nmembers=['crates/**','absent','no-match-*']\ndefault-members=['other']\n",
		"crates/deep/lib/Cargo.toml": "[package]\nname='nested'\n",
		"other/Cargo.toml":           "[package]\nname='other'\n",
	})
	root := cargoTestProject(t, docs, "Cargo.toml")
	if !cargoTestReference(root, "cargo-workspace-member", "crates/deep/lib/Cargo.toml") {
		t.Fatal("recursive glob failed")
	}
	for _, code := range []string{"cargo-workspace-pattern-unmatched", "cargo-workspace-member-unavailable", "cargo-default-not-member"} {
		if !cargoTestDiagnostic(root, code) {
			t.Errorf("missing %s in %+v", code, root.Diagnostics)
		}
	}
}

func TestCargoResolutionWorkBudget(t *testing.T) {
	d := NewDocument("Cargo.toml", "cargo-workspace")
	budget := cargoWork{remaining: 0}
	cargoPatternReferences(d, "**", "cargo-workspace-member", []*Document{NewDocument("a/Cargo.toml", "cargo")}, map[string]bool{}, &budget, nil)
	if !cargoTestDiagnostic(d, "cargo-resolution-limit") {
		t.Fatal("work budget unreported")
	}
}

func TestCargoUnavailableTargetsAndBuildScriptOptOut(t *testing.T) {
	docs, _ := cargoTestDocuments(t, map[string]string{
		"Cargo.toml": "[package]\nname='sample'\nbuild=false\n[[bin]]\nname='sample'\npath='missing.rs'\n",
		"build.rs":   "fn main() {}",
	})
	d := docs[0]
	if !cargoTestRequirement(d, "cargo-build-script", "disabled", "Cargo.toml") || !cargoTestDiagnostic(d, "cargo-target-unavailable") {
		t.Fatalf("target availability: %+v", d)
	}
	for _, iface := range d.Project.Interfaces {
		if iface.Kind == "cargo-build-script" {
			t.Fatal("disabled script appeared")
		}
		if iface.Kind == "cargo-bin" && iface.State != "unresolved" {
			t.Fatalf("missing target declared available: %+v", iface)
		}
	}
}

func TestCargoResolutionDeterministicAcrossInventoryOrder(t *testing.T) {
	source := map[string]string{
		"Cargo.toml":          "[workspace]\nmembers=['crates/*']\n[workspace.dependencies]\nunused={path='unused'}\n",
		"crates/a/Cargo.toml": "[package]\nname='a'\n",
		"crates/b/Cargo.toml": "[package]\nname='b'\n",
	}
	names := []string{"Cargo.toml", "crates/a/Cargo.toml", "crates/b/Cargo.toml"}
	run := func(reverse bool) string {
		docs := []*Document{}
		files := map[string]bool{}
		for _, name := range names {
			docs = append(docs, ParseCargo(name, []byte(source[name])))
			files[name] = true
		}
		if reverse {
			slices.Reverse(docs)
		}
		ResolveCargo(docs, files)
		slices.SortFunc(docs, func(a, b *Document) int { return strings.Compare(a.Project.ID, b.Project.ID) })
		projects := []*Project{}
		for _, d := range docs {
			projects = append(projects, d.Project)
		}
		encoded, _ := json.Marshal(projects)
		return string(encoded)
	}
	if run(false) != run(true) {
		t.Fatal("inventory insertion order changed observations")
	}
	docs, _ := cargoTestDocuments(t, source)
	if !cargoTestReference(cargoTestProject(t, docs, "Cargo.toml"), "cargo-workspace-path-dependency", "unused/Cargo.toml") {
		t.Fatal("unused workspace declaration disappeared")
	}
}

func FuzzCargoDeclarations(f *testing.F) {
	for _, seed := range []string{"[package]\nname='sample'\n", "[workspace]\nmembers=['**']\n", "[package]\nname='x'\n[dependencies]\na={path='../outside'}\n", "[package]\nname='duplicate'\nname='other'\n"} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, content []byte) {
		if len(content) > int(MaxManifestBytes)+1 {
			return
		}
		d := ParseCargo("Cargo.toml", content)
		ResolveCargo([]*Document{d}, map[string]bool{"Cargo.toml": true})
		if len(d.Project.Requirements)+len(d.Project.References)+len(d.Project.Interfaces) > MaxObservationsPerManifest || len(d.Diagnostics) > 32 {
			t.Fatal("observation budget exceeded")
		}
		for _, ref := range d.Project.References {
			if strings.HasPrefix(ref.Target, "/") || ref.Target == ".." || strings.HasPrefix(ref.Target, "../") || strings.Contains(ref.Target, ":") {
				t.Fatalf("unsafe target %q", ref.Target)
			}
		}
	})
}

func TestCargoUnsupportedDeclarationsRemainExplicit(t *testing.T) {
	source := map[string]string{
		"Cargo.toml":        "[workspace]\nmembers=['member']\n[workspace.dependencies]\ninvalid={version='1',optional=true}\n[workspace.package]\nversion='1.0.0'\n",
		"member/Cargo.toml": "[package]\nname='member'\nversion='banana'\nedition='2099'\n[dependencies]\ninvalid={workspace=true}\nbad_git={git=1}\nbad_version='banana'\nwrong_inherit={workspace=true,version='1.0'}\n[patch.crates-io]\npatched={path='../patched'}\n",
	}
	docs, _ := cargoTestDocuments(t, source)
	member := cargoTestProject(t, docs, "member/Cargo.toml")
	if member.Project.Version != "" {
		t.Fatal("invalid package version retained as identity")
	}
	for _, code := range []string{"invalid-cargo-declaration", "cargo-dependency-inheritance-unresolved", "cargo-dependency-overrides-unevaluated"} {
		if !cargoTestDiagnostic(member, code) {
			t.Errorf("missing diagnostic %s", code)
		}
	}
	for _, req := range member.Project.Requirements {
		if req.Kind == "cargo-dependency" && req.State != "unresolved" {
			t.Fatalf("invalid dependency treated as supported: %+v", req)
		}
	}
}

func TestCargoIncompleteWorkspaceSelectionDoesNotInferMembership(t *testing.T) {
	for _, selection := range []string{
		"members=['member']\nexclude=['{member,other}/*']\n",
		"members=['member']\nexclude=42\n",
		"members=['member',42]\n",
		"members=['member']\nexclude=['/host/secret']\n",
		"members=['member']\nexclude=[" + strings.Repeat("'other',", MaxPatterns) + "'more']\n",
	} {
		docs, _ := cargoTestDocuments(t, map[string]string{
			"Cargo.toml":        "[workspace]\n" + selection + "[workspace.package]\nversion='1.0.0'\n",
			"member/Cargo.toml": "[package]\nname='member'\nversion.workspace=true\n",
		})
		root := cargoTestProject(t, docs, "Cargo.toml")
		member := cargoTestProject(t, docs, "member/Cargo.toml")
		if !cargoTestDiagnostic(root, "cargo-workspace-selection-unresolved") {
			t.Fatalf("incomplete selection undiagnosed: %q", selection)
		}
		for _, ref := range root.Project.References {
			if (ref.Kind == "cargo-workspace-member" || ref.Kind == "cargo-workspace-path-member") && ref.State == "declared" {
				t.Fatalf("incomplete selection inferred membership: %+v", ref)
			}
		}
		if member.Project.Version != "" || !cargoTestDiagnostic(member, "cargo-inheritance-unresolved") {
			t.Fatalf("incomplete workspace supplied inheritance: %+v", member)
		}
	}
}
