package assessment

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"path"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/war-and-code/dircue/pkg/declarations"
	"github.com/war-and-code/dircue/pkg/discovery"
	"github.com/war-and-code/dircue/pkg/lockfiles"
)

// assessFiles parses files with the declaration collector and assesses them
// with the evidence the scanner passes, including interpreted and omitted
// manifest paths.
func assessFiles(t *testing.T, files map[string]string, locks *lockfiles.Report) *Report {
	t.Helper()
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	slices.Sort(names)
	decl := declarations.New("directory", "", 0)
	decl.EnableProjectRecords()
	assess := New("directory", "")
	for _, name := range names {
		content := []byte(files[name])
		var candidate *declarations.Candidate
		if declarations.IsManifest(name) {
			candidate = &declarations.Candidate{Path: name, Size: int64(len(content)), Read: func(context.Context, int64) ([]byte, int64, error) {
				return slices.Clone(content), int64(len(content)), nil
			}}
		}
		decl.Add(name, candidate)
		assess.Add(discovery.File{Path: name, Size: int64(len(content))})
	}
	report, err := decl.Finish(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	omitted, trees, attributed := decl.OmittedPaths()
	out, err := assess.Finish(Evidence{Declarations: report, Lockfiles: locks, Records: decl.ProjectRecords(), InterpretedManifests: decl.InterpretedPaths(), OmittedDeclarationPaths: omitted, OmittedDeclarationTrees: trees, OmittedDeclarationAttributed: attributed})
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateReport(out); err != nil {
		t.Fatalf("ValidateReport: %v", err)
	}
	return out
}

func lockfileRow(t *testing.T, r *Report, ecosystem string) LockfileEcosystem {
	t.Helper()
	for _, row := range r.Lockfiles {
		if row.Ecosystem == ecosystem {
			return row
		}
	}
	t.Fatalf("no %s lockfile row: %+v", ecosystem, r.Lockfiles)
	return LockfileEcosystem{}
}

func TestUnparsedCountsOnlyManifestsTheParserDidNotInterpret(t *testing.T) {
	files := map[string]string{
		// requirements.txt is parsed and merged into the pyproject record.
		"app/pyproject.toml":   "[project]\nname = \"app\"\nversion = \"1.0\"\n",
		"app/requirements.txt": "requests==2.32.0\n",
		// Dependency groups without a distribution yield no document.
		"lint/pyproject.toml": "[dependency-groups]\ndev = [\"ruff\"]\n",
	}
	r := assessFiles(t, files, nil)
	if r.ManifestCandidatePopulation.Count != 3 || r.UnparsedManifestCandidates.Count != 0 {
		t.Fatalf("interpreted manifests counted as unparsed: population=%+v unparsed=%+v", r.ManifestCandidatePopulation, r.UnparsedManifestCandidates)
	}
	if r.Projects.Count != 1 || r.Projects.Completeness != "complete" {
		t.Fatalf("projects = %+v, want the app project, complete", r.Projects)
	}

	files["web/package.json"] = "{"
	r = assessFiles(t, files, nil)
	if r.UnparsedManifestCandidates.Count != 1 {
		t.Fatalf("invalid manifest not counted as unparsed: %+v", r.UnparsedManifestCandidates)
	}
	if r.Projects.Completeness != "lower_bound" || !slices.Contains(r.Projects.Reasons, "manifest_candidates_unparsed") {
		t.Fatalf("an unparsed manifest must make projects a lower bound: %+v", r.Projects)
	}
}

func TestDeclarationOmissionsQualifyProjectsOnlyWhenTheyMayHideAManifest(t *testing.T) {
	for _, tc := range []struct {
		name       string
		omitted    int64
		paths      []string
		trees      []string
		attributed bool
		lower      bool
	}{
		{name: "non-manifest files", omitted: 2, attributed: true},
		{name: "non-manifest path", omitted: 1, paths: []string{"vendor/link"}, attributed: true},
		{name: "manifest path", omitted: 1, paths: []string{"web/package.json"}, attributed: true, lower: true},
		{name: "unreadable directory", omitted: 1, trees: []string{"packages/locked"}, attributed: true, lower: true},
		{name: "unattributed", omitted: 1, attributed: false, lower: true},
		{name: "more paths than omissions", omitted: 1, paths: []string{"vendor/a", "vendor/b"}, attributed: true, lower: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := declarations.Project{ID: "package.json", Root: ".", Kind: "npm"}
			c := New("directory", "")
			c.Add(discovery.File{Path: p.ID, Size: 2})
			decls := &declarations.Report{Source: "directory", Status: "partial", Projects: []declarations.Project{p}, Coverage: declarations.Coverage{ManifestCandidates: 1, ParsedManifests: 1, OmittedFiles: tc.omitted}}
			r, err := c.Finish(Evidence{Declarations: decls, Records: projectRecords(decls.Projects), InterpretedManifests: []string{p.ID}, OmittedDeclarationPaths: tc.paths, OmittedDeclarationTrees: tc.trees, OmittedDeclarationAttributed: tc.attributed})
			if err != nil {
				t.Fatal(err)
			}
			lower := slices.Contains(r.Projects.Reasons, "declaration_files_omitted")
			if lower != tc.lower || (r.Projects.Completeness == "lower_bound") != tc.lower {
				t.Fatalf("projects = %+v, want lower bound %v", r.Projects, tc.lower)
			}
		})
	}
}

func TestNonProjectRecordsAreExcludedFromProjects(t *testing.T) {
	r := assessFiles(t, map[string]string{
		// Cargo rejects a manifest with neither [package] nor [workspace].
		"crate/Cargo.toml": "[dependencies]\nserde = \"1\"\n",
		// Tool settings without a project or build-system table.
		"lint/pyproject.toml": "[tool.ruff]\nline-length = 100\n",
		// The same file beside a Python lockfile belongs to a project.
		"locked/pyproject.toml": "[tool.ruff]\nline-length = 100\n",
		"locked/uv.lock":        "version = 1\n",
		// A build-system table makes a package.
		"built/pyproject.toml": "[build-system]\nrequires = [\"hatchling\"]\nbuild-backend = \"hatchling.build\"\n",
		// config/application.rb names the Rails app the Gemfile declares.
		"blog/Gemfile":               "source \"https://rubygems.org\"\ngem \"rails\"\n",
		"blog/config/application.rb": "module Blog\n  class Application < Rails::Application\n  end\nend\n",
	}, nil)
	want := []string{"blog", "built", "locked"}
	if r.Projects.Count != 3 || !slices.Equal(r.ProjectRootEvidence, want) {
		t.Fatalf("projects = %d at %v, want roots %v", r.Projects.Count, r.ProjectRootEvidence, want)
	}
	if got := lockfileRow(t, r, "ruby-bundler").Projects.Count; got != 1 {
		t.Fatalf("a Rails app was counted %d times", got)
	}
}

func TestEmptyPythonBuildSystemTableKeepsPyprojectAsProject(t *testing.T) {
	for name, content := range map[string]string{
		"empty/pyproject.toml":  "[build-system]\nrequires = []\n",
		"poetry/pyproject.toml": "[build-system]\nrequires = []\n[tool.poetry]\nname = \"sample\"\nversion = \"1.0\"\n",
	} {
		t.Run(name, func(t *testing.T) {
			r := assessFiles(t, map[string]string{name: content}, nil)
			if r.Projects.Count != 1 || !slices.Contains(r.ProjectRootEvidence, path.Dir(name)) {
				t.Fatalf("pyproject with an empty build-system table was excluded: projects=%+v roots=%v", r.Projects, r.ProjectRootEvidence)
			}
		})
	}
	r := assessFiles(t, map[string]string{
		"setup/pyproject.toml": "[build-system]\nrequires = []\n",
		"setup/setup.py":       "from setuptools import setup\nsetup(name='sample')\n",
	}, nil)
	if r.Projects.Count != 1 || !slices.Contains(r.ProjectRootEvidence, "setup") {
		t.Fatalf("empty build-system pyproject beside setup.py was lost: projects=%+v roots=%v", r.Projects, r.ProjectRootEvidence)
	}
}

func TestUnclassifiedDeclarationKindLowersProjectBounds(t *testing.T) {
	p := declarations.Project{ID: "future/manifest.toml", Root: "future", Kind: "future-kind"}
	c := New("directory", "")
	decls := &declarations.Report{Source: "directory", Status: "complete", Projects: []declarations.Project{p}}
	r, err := c.Finish(Evidence{Declarations: decls, Records: projectRecords(decls.Projects)})
	if err != nil {
		t.Fatal(err)
	}
	if r.Projects.Count != 0 || r.Projects.Completeness != "lower_bound" || !slices.Contains(r.Projects.Reasons, "unclassified_declaration_kind") {
		t.Fatalf("an unknown record kind was counted silently: %+v", r.Projects)
	}
}

func TestUnparsedWorkspaceFilesQualifyRelationshipMetrics(t *testing.T) {
	for _, tc := range []struct {
		file, reason string
	}{
		{"pnpm-workspace.yaml", "pnpm_workspace_unparsed"},
		{"lerna.json", "lerna_workspace_unparsed"},
		{"rush.json", "rush_workspace_unparsed"},
		{"node_modules/dep/pnpm-workspace.yaml", ""},
	} {
		t.Run(tc.file, func(t *testing.T) {
			r := assessFiles(t, map[string]string{tc.file: "{}\n", "packages/a/package.json": `{"name":"a"}`}, nil)
			for _, m := range []Metric{r.WorkspaceMembership, r.LocalDependencies} {
				if tc.reason == "" {
					if m.Completeness != "complete" {
						t.Fatalf("installed dependency file qualified a metric: %+v", m)
					}
					continue
				}
				if m.Completeness != "lower_bound" || !slices.Contains(m.Reasons, tc.reason) {
					t.Fatalf("metric %+v lacks %s", m, tc.reason)
				}
			}
			if r.Projects.Completeness != "complete" {
				t.Fatalf("a workspace file does not hide projects: %+v", r.Projects)
			}
		})
	}
}

func TestVendoredInventoryFollowsLinguistRulesAndAttributes(t *testing.T) {
	yes, no := true, false
	c := New("directory", "")
	for _, f := range []discovery.File{
		{Path: "node_modules/a/index.js", Size: 1},
		{Path: "vendor/b.go", Size: 2},
		{Path: ".venv/lib/c.py", Size: 4},
		{Path: "src/generated.go", Size: 8, Vendored: &yes},
		{Path: "vendor/own.go", Size: 16, Vendored: &no},
		{Path: "main.go", Size: 32},
	} {
		c.Add(f)
	}
	r, err := c.Finish(Evidence{Declarations: &declarations.Report{Source: "directory", Status: "complete"}})
	if err != nil {
		t.Fatal(err)
	}
	if r.Inventory.VendoredFiles.Count != 3 || r.Inventory.VendoredBytes.Count != 11 || r.Inventory.Files.Count != 6 || r.Inventory.Bytes.Count != 63 {
		t.Fatalf("vendored partition: %+v", r.Inventory)
	}
}

func TestLockfilesFormTheirOwnFilenameKind(t *testing.T) {
	c := New("directory", "")
	for _, name := range []string{"package.json", "package-lock.json", "go.mod", "go.sum", "Cargo.lock"} {
		c.Add(discovery.File{Path: name, Size: 1})
	}
	r, err := c.Finish(Evidence{Declarations: &declarations.Report{Source: "directory", Status: "complete"}})
	if err != nil {
		t.Fatal(err)
	}
	kinds := map[string]int64{}
	for _, k := range r.FilenameCandidates {
		kinds[k.Kind] = k.Files
	}
	if kinds["manifest"] != 2 || kinds["lockfile"] != 3 {
		t.Fatalf("filename kinds: %+v", r.FilenameCandidates)
	}
	for _, group := range r.ManifestCandidates {
		if strings.Contains(group.Filename, "lock") || group.Filename == "go.sum" {
			t.Fatalf("lockfile counted as a manifest: %+v", group)
		}
	}
}

func TestLockfileOutcomesPartitionProjectsWithReasonsAndRoles(t *testing.T) {
	type entry struct {
		id, kind, state string
		boundaries      []lockfiles.Boundary
	}
	entries := []entry{
		{"web/package.json", "npm", "observed", []lockfiles.Boundary{{Reason: "npm-v11-shrinkwrap-selection"}}},
		{"test/fixtures/x/package.json", "npm", "observed", nil},
		{"api/package.json", "npm", "missing", []lockfiles.Boundary{{Reason: "lockfile-not-present"}}},
		// An unshared ancestor lockfile does not explain having nothing to lock.
		{"lib/package.json", "npm", "not_applicable", []lockfiles.Boundary{{Reason: "npm-ancestor-lock-not-shared"}}},
		{"yarn/package.json", "npm", "unsupported", []lockfiles.Boundary{{Reason: "npm-v11-shrinkwrap-selection"}, {Reason: "npm-alternative-lockfile-format"}}},
		{"ws/package.json", "npm", "indeterminate", []lockfiles.Boundary{{Reason: "workspace-lockfile-owner-unresolved"}}},
		{"orphan/package.json", "npm", "", nil},
		{"examples/py/pyproject.toml", "python", "", nil},
	}
	c := New("directory", "")
	projects := []declarations.Project{}
	locks := &lockfiles.Report{Source: "directory", Status: "complete", Contexts: []lockfiles.Context{}}
	for _, e := range entries {
		c.Add(discovery.File{Path: e.id, Size: 1})
		projects = append(projects, declarations.Project{ID: e.id, Root: path.Dir(e.id), Kind: e.kind})
		if e.state != "" {
			locks.Contexts = append(locks.Contexts, lockfiles.Context{ProjectID: e.id, Ecosystem: e.kind, ManifestPath: e.id, AssociationState: e.state, Boundaries: e.boundaries})
		}
	}
	decls := &declarations.Report{Source: "directory", Status: "complete", Projects: projects}
	r, err := c.Finish(Evidence{Declarations: decls, Lockfiles: locks, Records: projectRecords(projects)})
	if err != nil {
		t.Fatal(err)
	}
	npm := lockfileRow(t, r, "npm")
	got := [7]int64{npm.Projects.Count, npm.Eligible.Count, npm.Covered.Count, npm.Missing.Count, npm.NotApplicable.Count, npm.Unsupported.Count, npm.Unknown.Count}
	if got != [7]int64{7, 5, 2, 1, 1, 1, 2} {
		t.Fatalf("npm partition [projects eligible covered missing not_applicable unsupported unknown] = %v", got)
	}
	wantReasons := []OutcomeReason{
		{State: "missing", Reason: "lockfile-not-present", Count: 1},
		{State: "not_applicable", Reason: "no-direct-declarations", Count: 1},
		{State: "unknown", Reason: "no-lockfile-context", Count: 1},
		{State: "unknown", Reason: "workspace-lockfile-owner-unresolved", Count: 1},
		{State: "unsupported", Reason: "npm-alternative-lockfile-format", Count: 1},
	}
	if !slices.Equal(npm.OutcomeReasons, wantReasons) {
		t.Fatalf("npm outcome reasons = %+v, want %+v", npm.OutcomeReasons, wantReasons)
	}
	wantRoles := []LockfileRole{
		{Role: "fixture", Projects: 1, Eligible: 1, Covered: 1},
		{Role: "primary", Projects: 6, Eligible: 4, Covered: 1, Missing: 1, NotApplicable: 1, Unsupported: 1, Unknown: 2},
	}
	if !slices.Equal(npm.ByRole, wantRoles) {
		t.Fatalf("npm by_role = %+v, want %+v", npm.ByRole, wantRoles)
	}
	python := lockfileRow(t, r, "python")
	if python.Eligible.Count != 0 || python.Unsupported.Count != 1 || !slices.Equal(python.OutcomeReasons, []OutcomeReason{{State: "unsupported", Reason: "ecosystem-outside-association-scope", Count: 1}}) {
		t.Fatalf("python row: %+v", python)
	}
	if !slices.Equal(r.ProjectsByRole, []RoleCount{{Role: "example", Count: 1}, {Role: "fixture", Count: 1}, {Role: "primary", Count: 6}}) {
		t.Fatalf("projects by role: %+v", r.ProjectsByRole)
	}
	overall := r.LockfilesOverall
	if overall.Projects.Count != 8 || overall.Eligible.Count != 5 || len(overall.OutcomeReasons) != 6 {
		t.Fatalf("overall row: %+v", overall)
	}

	r, err = New("directory", "").Finish(Evidence{Declarations: decls, Records: projectRecords(projects)})
	if err != nil {
		t.Fatal(err)
	}
	if npm := lockfileRow(t, r, "npm"); npm.Unknown.Count != 7 || npm.OutcomeReasons[0].Reason != "lockfiles-not-run" {
		t.Fatalf("absent lockfile report: %+v", npm)
	}
}

func TestProjectRootsTakeTheHighestPriorityRole(t *testing.T) {
	projects := []declarations.Project{
		{ID: "src/App/App.csproj", Root: "src/App", Kind: "dotnet"},
		{ID: "src/App/App.Tests.csproj", Root: "src/App", Kind: "dotnet"},
		{ID: "docs/package.json", Root: "docs", Kind: "npm"},
		{ID: "package.json", Root: ".", Kind: "npm"},
	}
	c := New("directory", "")
	for _, p := range projects {
		c.Add(discovery.File{Path: p.ID, Size: 1})
	}
	decls := &declarations.Report{Source: "directory", Status: "complete", Projects: projects}
	r, err := c.Finish(Evidence{Declarations: decls, Records: projectRecords(projects)})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(r.ProjectsByRole, []RoleCount{{Role: "docs", Count: 1}, {Role: "primary", Count: 2}, {Role: "test", Count: 1}}) {
		t.Fatalf("projects by role: %+v", r.ProjectsByRole)
	}
	if !slices.Equal(r.ProjectRootsByRole, []RoleCount{{Role: "docs", Count: 1}, {Role: "primary", Count: 1}, {Role: "test", Count: 1}}) {
		t.Fatalf("project roots by role: %+v", r.ProjectRootsByRole)
	}
}

func TestMetricsListsEveryAggregateMetric(t *testing.T) {
	r := nativeFixture(t, false)
	names := map[string]bool{}
	for _, m := range r.Metrics() {
		names[m.Ecosystem+":"+m.Name] = true
	}
	metricType := reflect.TypeFor[Metric]()
	jsonName := func(f reflect.StructField) string { return strings.Split(f.Tag.Get("json"), ",")[0] }
	want := []string{}
	for _, f := range reflect.VisibleFields(reflect.TypeFor[Report]()) {
		if f.Type == metricType {
			want = append(want, ":"+jsonName(f))
		}
	}
	for _, f := range reflect.VisibleFields(reflect.TypeFor[InventoryMetrics]()) {
		want = append(want, ":inventory:"+jsonName(f))
	}
	for _, row := range append([]LockfileEcosystem{r.LockfilesOverall}, r.Lockfiles...) {
		for _, f := range reflect.VisibleFields(reflect.TypeFor[LockfileEcosystem]()) {
			if f.Type == metricType {
				want = append(want, row.Ecosystem+":"+jsonName(f))
			}
		}
	}
	if len(r.Lockfiles) == 0 || len(names) != len(want) {
		t.Fatalf("Metrics() lists %d metrics, report has %d", len(names), len(want))
	}
	for _, name := range want {
		if !names[name] {
			t.Fatalf("Metrics() omits %s", name)
		}
	}
}

// manifestNameLiterals returns the string literals in the case clauses of the
// named function, the filename switch that selects manifests.
func manifestNameLiterals(t *testing.T, file, function string) []string {
	t.Helper()
	parsed, err := parser.ParseFile(token.NewFileSet(), file, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, decl := range parsed.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Name.Name != function {
			continue
		}
		ast.Inspect(fn, func(n ast.Node) bool {
			clause, ok := n.(*ast.CaseClause)
			if !ok {
				return true
			}
			for _, expr := range clause.List {
				if lit, ok := expr.(*ast.BasicLit); ok && lit.Kind == token.STRING {
					value, err := strconv.Unquote(lit.Value)
					if err != nil {
						t.Fatal(err)
					}
					out = append(out, value)
				}
			}
			return true
		})
	}
	if len(out) == 0 {
		t.Fatalf("no case literals in %s %s", file, function)
	}
	return out
}

func TestManifestClassesCoverEverySelectedManifestName(t *testing.T) {
	names := []string{"requirements-dev.txt", "requirements/prod.txt", "demo.gemspec", "demo.cabal"}
	for _, source := range []struct{ file, function string }{
		{"../declarations/collect.go", "IsManifest"},
		{"../projects/dotnet.go", "IsDotnet"},
		{"../projects/jvm.go", "IsJVM"},
	} {
		for _, literal := range manifestNameLiterals(t, source.file, source.function) {
			switch {
			case strings.HasPrefix(literal, "."):
				names = append(names, "App"+literal)
			case literal == "application.rb":
				names = append(names, "config/application.rb")
			default:
				names = append(names, literal)
			}
		}
	}
	used := map[string]bool{}
	for _, name := range names {
		selected := "pkg/" + name
		if !declarations.IsManifest(selected) {
			t.Fatalf("%s is not a selected manifest", selected)
		}
		filename, ecosystem, kind := classifyManifest(selected)
		if ecosystem == "other" || !manifestKinds[kind] {
			t.Fatalf("%s classifies as %s %s", selected, ecosystem, kind)
		}
		used[strings.ToLower(filename)] = true
	}
	for key := range manifestClasses {
		if !used[key] {
			t.Fatalf("manifest class %q matches no selected manifest name", key)
		}
	}
}
