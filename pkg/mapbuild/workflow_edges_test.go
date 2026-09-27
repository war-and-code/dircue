package mapbuild

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/war-and-code/dircue/pkg/deployables"
	"github.com/war-and-code/dircue/pkg/mapdoc"
)

// workflowEdgesDoc builds a minimal map document with two components and one
// github-actions workflow that declares a working-directory for a step.
func workflowEdgesDoc(t *testing.T, stepWD string) (*mapdoc.Document, *deployables.Report) {
	t.Helper()

	rootComp := mapdoc.NewNode(mapdoc.NodeComponent, []string{"go.mod"}, "go")
	rootComp.Name = "root"
	rootComp.Properties = map[string]string{"root": "."}
	rootComp.Coverage = mapdoc.Coverage{Status: mapdoc.CoverageComplete}
	rootComp.Evidence = []mapdoc.Evidence{{Basis: mapdoc.BasisDeclaredConfig, Path: "go.mod", SourceKind: mapdoc.SourceConfiguration, Rule: &mapdoc.Producer{ID: "test", Version: "1"}}}

	apiComp := mapdoc.NewNode(mapdoc.NodeComponent, []string{"services/api/go.mod"}, "go")
	apiComp.Name = "api"
	apiComp.Properties = map[string]string{"root": "services/api"}
	apiComp.Coverage = mapdoc.Coverage{Status: mapdoc.CoverageComplete}
	apiComp.Evidence = []mapdoc.Evidence{{Basis: mapdoc.BasisDeclaredConfig, Path: "services/api/go.mod", SourceKind: mapdoc.SourceConfiguration, Rule: &mapdoc.Producer{ID: "test", Version: "1"}}}

	d := &mapdoc.Document{
		SchemaVersion: mapdoc.SchemaVersion,
		Kind:          "map",
		Nodes:         []mapdoc.Node{rootComp, apiComp},
		Edges:         []mapdoc.Edge{},
	}

	content := "name: CI\non: [push]\njobs:\n  build:\n    steps:\n      - run: make\n        working-directory: " + stepWD + "\n"
	cb := []byte(content)
	files := []deployables.Candidate{
		{Path: ".github/workflows/ci.yml", Size: int64(len(cb)),
			Read: func(_ context.Context, _ int64) ([]byte, int64, error) { return cb, int64(len(cb)), nil }},
	}
	r, err := deployables.Observe(context.Background(), files, deployables.Options{})
	if err != nil {
		t.Fatal(err)
	}
	return d, r
}

// TestWorkflowEdgesExactComponentMatch verifies an exact working-directory →
// component root association.
func TestWorkflowEdgesExactComponentMatch(t *testing.T) {
	d, r := workflowEdgesDoc(t, "services/api")
	addDeployables(d, r)
	addWorkflowComponentEdges(d, r)

	var found []mapdoc.Edge
	for _, e := range d.Edges {
		if e.Type == mapdoc.EdgeBuilds {
			found = append(found, e)
		}
	}
	if len(found) == 0 {
		t.Fatalf("no EdgeBuilds edges emitted; all edges=%+v", d.Edges)
	}
	var ok bool
	for _, e := range found {
		// From is the deployable (workflow) node; Discriminator contains the
		// resolved path so we can verify it without hashing the component ID.
		if strings.Contains(e.Discriminator, "services/api") {
			ok = true
			if e.Coverage.Status != mapdoc.CoveragePartial {
				t.Errorf("expected partial coverage; got %v", e.Coverage.Status)
			}
			if len(e.Coverage.Reasons) == 0 || e.Coverage.Reasons[0] != "workflow_working_directory_association" {
				t.Errorf("expected reason workflow_working_directory_association; got %v", e.Coverage.Reasons)
			}
		}
	}
	if !ok {
		t.Errorf("no workflow→api edge; found edges=%+v", found)
	}
}

// TestWorkflowEdgesAncestorLookup verifies that a subdirectory of a component
// root resolves to the ancestor component.
func TestWorkflowEdgesAncestorLookup(t *testing.T) {
	d, r := workflowEdgesDoc(t, "services/api/cmd")
	addDeployables(d, r)
	addWorkflowComponentEdges(d, r)

	var found bool
	for _, e := range d.Edges {
		if e.Type == mapdoc.EdgeBuilds && strings.Contains(e.Discriminator, "services/api") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected ancestor-resolved workflow→api edge; edges=%+v", d.Edges)
	}
}

// An ambiguous nearer root blocks attribution to a broader ancestor. The
// workflow path is inside services/api, whose duplicate component roots make
// ownership unclear even though the repository root has a single component.
func TestWorkflowEdgesAmbiguousNearestRootDoesNotFallBack(t *testing.T) {
	d, r := workflowEdgesDoc(t, "services/api/cmd")
	duplicate := mapdoc.NewNode(mapdoc.NodeComponent, []string{"services/api/package.json"}, "npm")
	duplicate.Name = "api-package"
	duplicate.Properties = map[string]string{"root": "services/api"}
	duplicate.Coverage = mapdoc.Coverage{Status: mapdoc.CoverageComplete}
	d.Nodes = append(d.Nodes, duplicate)
	addDeployables(d, r)
	addWorkflowComponentEdges(d, r)

	for _, e := range d.Edges {
		if e.Type == mapdoc.EdgeBuilds {
			t.Errorf("ambiguous nearest root incorrectly fell back to broader component: %+v", e)
		}
	}
}

// TestWorkflowEdgesUnresolvedNotEmitted verifies that an expression-bearing
// working-directory never produces a component association.
func TestWorkflowEdgesUnresolvedNotEmitted(t *testing.T) {
	content := "name: CI\non: [push]\njobs:\n  build:\n    steps:\n      - run: make\n        working-directory: ${{ matrix.dir }}\n"
	cb := []byte(content)
	files := []deployables.Candidate{
		{Path: ".github/workflows/ci.yml", Size: int64(len(cb)),
			Read: func(_ context.Context, _ int64) ([]byte, int64, error) { return cb, int64(len(cb)), nil }},
	}
	r, err := deployables.Observe(context.Background(), files, deployables.Options{})
	if err != nil {
		t.Fatal(err)
	}
	comp := mapdoc.NewNode(mapdoc.NodeComponent, []string{"go.mod"}, "go")
	comp.Name = "root"
	comp.Properties = map[string]string{"root": "."}
	comp.Coverage = mapdoc.Coverage{Status: mapdoc.CoverageComplete}
	comp.Evidence = []mapdoc.Evidence{{Basis: mapdoc.BasisDeclaredConfig, Path: "go.mod", SourceKind: mapdoc.SourceConfiguration, Rule: &mapdoc.Producer{ID: "test", Version: "1"}}}
	d := &mapdoc.Document{Nodes: []mapdoc.Node{comp}, Edges: []mapdoc.Edge{}}
	addDeployables(d, r)
	addWorkflowComponentEdges(d, r)

	for _, e := range d.Edges {
		if e.Type == mapdoc.EdgeBuilds {
			t.Errorf("unexpected EdgeBuilds for expression working-directory: %+v", e)
		}
	}
}

// TestWorkflowEdgesNilReport verifies that a nil report does not panic.
func TestWorkflowEdgesNilReport(t *testing.T) {
	d := &mapdoc.Document{}
	addWorkflowComponentEdges(d, nil)
}

// TestWorkflowEdgesNoDuplicates verifies that repeated calls do not add
// duplicate edges.
func TestWorkflowEdgesNoDuplicates(t *testing.T) {
	d, r := workflowEdgesDoc(t, "services/api")
	addDeployables(d, r)
	addWorkflowComponentEdges(d, r)
	countBefore := 0
	for _, e := range d.Edges {
		if e.Type == mapdoc.EdgeBuilds {
			countBefore++
		}
	}
	addWorkflowComponentEdges(d, r)
	countAfter := 0
	for _, e := range d.Edges {
		if e.Type == mapdoc.EdgeBuilds {
			countAfter++
		}
	}
	if countBefore != countAfter {
		t.Errorf("duplicate edges added on second call: before=%d after=%d", countBefore, countAfter)
	}
}

// TestWorkflowEdgesOnDircueItself is an informational integration test that
// runs addWorkflowComponentEdges against dircue's own .github/workflows/ and
// the dircue module component. It logs the edge count for evidence.
func TestWorkflowEdgesOnDircueItself(t *testing.T) {
	repoRoot := filepath.Join("..", "..")
	wfDir := filepath.Join(repoRoot, ".github", "workflows")
	entries, err := os.ReadDir(wfDir)
	if err != nil {
		t.Skip("cannot read workflow directory:", err)
	}
	var files []deployables.Candidate
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".yml") && !strings.HasSuffix(name, ".yaml") {
			continue
		}
		abs := filepath.Join(wfDir, name)
		rel := ".github/workflows/" + name
		absCopy := abs
		files = append(files, deployables.Candidate{
			Path: rel,
			Size: entry2size(entry),
			Read: func(_ context.Context, limit int64) ([]byte, int64, error) {
				data, rerr := os.ReadFile(absCopy)
				if rerr != nil {
					return nil, 0, rerr
				}
				if int64(len(data)) > limit {
					return nil, int64(len(data)), nil
				}
				return data, int64(len(data)), nil
			},
		})
	}
	if len(files) == 0 {
		t.Skip("no workflow files found")
	}
	r, err := deployables.Observe(context.Background(), files, deployables.Options{})
	if err != nil {
		t.Fatal(err)
	}

	comp := mapdoc.NewNode(mapdoc.NodeComponent, []string{"go.mod"}, "go")
	comp.Name = "dircue"
	comp.Properties = map[string]string{"root": "."}
	comp.Coverage = mapdoc.Coverage{Status: mapdoc.CoverageComplete}
	comp.Evidence = []mapdoc.Evidence{{Basis: mapdoc.BasisDeclaredConfig, Path: "go.mod", SourceKind: mapdoc.SourceConfiguration, Rule: &mapdoc.Producer{ID: "test", Version: "1"}}}
	d := &mapdoc.Document{Nodes: []mapdoc.Node{comp}, Edges: []mapdoc.Edge{}}
	addDeployables(d, r)
	addWorkflowComponentEdges(d, r)

	buildsCount := 0
	for _, e := range d.Edges {
		if e.Type == mapdoc.EdgeBuilds {
			buildsCount++
		}
	}
	t.Logf("dircue repo: %d workflow files → %d builds edges", len(files), buildsCount)
}

func entry2size(e os.DirEntry) int64 {
	info, err := e.Info()
	if err != nil {
		return 0
	}
	return info.Size()
}

// A working directory inside a directory the workflow fills with
// actions/checkout is a run-time checkout, not the committed tree. With a
// single root component, the ancestor walk used to attribute it to that
// component.
func TestWorkflowCheckoutPathIsNotAttributed(t *testing.T) {
	d, _ := workflowEdgesDoc(t, "unused")
	d.Nodes = d.Nodes[:1] // only the root component
	content := []byte("name: Docs\non: [push]\njobs:\n  publish:\n    steps:\n      - uses: actions/checkout@v4\n        with:\n          repository: example/docs\n          path: other-docs\n      - run: make\n        working-directory: other-docs/site\n")
	files := []deployables.Candidate{{Path: ".github/workflows/docs.yml", Size: int64(len(content)),
		Read: func(_ context.Context, _ int64) ([]byte, int64, error) { return content, int64(len(content)), nil }}}
	r, err := deployables.Observe(context.Background(), files, deployables.Options{})
	if err != nil {
		t.Fatal(err)
	}
	addDeployables(d, r)
	addWorkflowComponentEdges(d, r)
	for _, e := range d.Edges {
		if e.Type == mapdoc.EdgeBuilds {
			t.Fatalf("working directory inside a checkout path was attributed: %+v", e)
		}
	}
	found := false
	for _, n := range d.Nodes {
		for _, f := range n.Facts {
			if f.Name == "working_directory" && f.Value == "other-docs/site" {
				found = true
				if f.Coverage.Status != mapdoc.CoveragePartial || !slices.Contains(f.Coverage.Reasons, "named_repository_checkout") {
					t.Errorf("working_directory fact coverage = %+v, want partial named_repository_checkout", f.Coverage)
				}
			}
		}
	}
	if !found {
		t.Fatal("working_directory fact missing")
	}
}

// A working directory absent from a completely inventoried tree is created at
// run time (here by git clone) and is not attributed to the enclosing project.
// A directory that exists still resolves to its nearest component root.
func TestWorkflowWorkingDirectoryMustExistInTree(t *testing.T) {
	for _, tc := range []struct {
		dir, reason string
		wantEdge    bool
	}{
		{dir: "tooling", reason: "path_not_in_repository"},
		{dir: "cmd/tool", wantEdge: true},
	} {
		t.Run(tc.dir, func(t *testing.T) {
			d, _ := workflowEdgesDoc(t, "unused")
			d.Nodes = d.Nodes[:1] // only the root component
			d.Coverage = []mapdoc.QuestionCoverage{{Question: "content", Scope: ".", Coverage: mapdoc.Coverage{Status: mapdoc.CoverageComplete}}}
			content := []byte("name: Tools\non: [push]\njobs:\n  run:\n    steps:\n      - run: git clone https://example.test/tooling.git tooling\n      - run: make\n        working-directory: " + tc.dir + "\n")
			read := func(b []byte) func(context.Context, int64) ([]byte, int64, error) {
				return func(context.Context, int64) ([]byte, int64, error) { return b, int64(len(b)), nil }
			}
			files := []deployables.Candidate{
				{Path: ".github/workflows/tools.yml", Size: int64(len(content)), Read: read(content)},
				{Path: "go.mod", Size: 1, Read: read([]byte("m"))},
				{Path: "cmd/tool/main.go", Size: 1, Read: read([]byte("p"))},
			}
			r, err := deployables.Observe(context.Background(), files, deployables.Options{})
			if err != nil {
				t.Fatal(err)
			}
			addDeployables(d, r)
			addWorkflowComponentEdges(d, r)
			edges := 0
			for _, e := range d.Edges {
				if e.Type == mapdoc.EdgeBuilds {
					edges++
				}
			}
			if (edges > 0) != tc.wantEdge {
				t.Fatalf("builds edges = %d, want edge %v", edges, tc.wantEdge)
			}
			for _, n := range d.Nodes {
				for _, f := range n.Facts {
					if f.Name == "working_directory" && tc.reason != "" && !slices.Contains(f.Coverage.Reasons, tc.reason) {
						t.Errorf("working_directory coverage = %+v, want reason %s", f.Coverage, tc.reason)
					}
				}
			}
		})
	}
}

// A checkout of this repository below the workspace keeps its layout, so a
// working directory inside it names the repository path after the checkout
// prefix.
func TestWorkflowSelfCheckoutPathMapsIntoRepository(t *testing.T) {
	for _, tc := range []struct{ checkout, dir, wantRoot string }{
		{checkout: "path: pr", dir: "./pr", wantRoot: "."},
		{checkout: "path: pr", dir: "pr/services/api", wantRoot: "services/api"},
		{checkout: "path: pr\n          repository: ${{ github.repository }}", dir: "pr/services/api", wantRoot: "services/api"},
	} {
		t.Run(tc.checkout+" "+tc.dir, func(t *testing.T) {
			d, _ := workflowEdgesDoc(t, "unused")
			content := []byte("name: Size\non: [pull_request]\njobs:\n  size:\n    steps:\n      - uses: actions/checkout@v4\n        with:\n          " + tc.checkout + "\n      - run: npm ci\n        working-directory: " + tc.dir + "\n")
			files := []deployables.Candidate{{Path: ".github/workflows/size.yml", Size: int64(len(content)),
				Read: func(_ context.Context, _ int64) ([]byte, int64, error) { return content, int64(len(content)), nil }}}
			r, err := deployables.Observe(context.Background(), files, deployables.Options{})
			if err != nil {
				t.Fatal(err)
			}
			addDeployables(d, r)
			addWorkflowComponentEdges(d, r)
			roots := map[string]string{}
			for _, n := range d.Nodes {
				if n.Kind == mapdoc.NodeComponent {
					roots[n.ID] = n.Properties["root"]
				}
			}
			var got []string
			for _, e := range d.Edges {
				if e.Type == mapdoc.EdgeBuilds {
					got = append(got, roots[e.To])
				}
			}
			if len(got) != 1 || got[0] != tc.wantRoot {
				t.Fatalf("builds edge roots = %v, want [%s]", got, tc.wantRoot)
			}
		})
	}
}

// Checkout paths belong to the job that declares them, and only to steps after
// the checkout. A named-repository checkout in one job must not affect a
// working directory in another job.
func TestWorkflowCheckoutPathsAreScopedToTheirJob(t *testing.T) {
	for _, tc := range []struct {
		name, workflow string
		wantEdge       bool
	}{
		{"other job", "jobs:\n  tools:\n    steps:\n      - uses: actions/checkout@v4\n        with:\n          repository: example/tools\n          path: services\n  build:\n    steps:\n      - uses: actions/checkout@v4\n      - run: make\n        working-directory: services/api\n", true},
		{"same job", "jobs:\n  build:\n    steps:\n      - uses: actions/checkout@v4\n        with:\n          repository: example/tools\n          path: services\n      - run: make\n        working-directory: services/api\n", false},
		{"later step", "jobs:\n  build:\n    steps:\n      - run: make\n        working-directory: services/api\n      - uses: actions/checkout@v4\n        with:\n          repository: example/tools\n          path: services\n", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d, _ := workflowEdgesDoc(t, "unused")
			content := []byte("name: Build\non: [push]\n" + tc.workflow)
			files := []deployables.Candidate{{Path: ".github/workflows/build.yml", Size: int64(len(content)),
				Read: func(_ context.Context, _ int64) ([]byte, int64, error) { return content, int64(len(content)), nil }}}
			r, err := deployables.Observe(context.Background(), files, deployables.Options{})
			if err != nil {
				t.Fatal(err)
			}
			addDeployables(d, r)
			addWorkflowComponentEdges(d, r)
			edges := 0
			for _, e := range d.Edges {
				if e.Type == mapdoc.EdgeBuilds {
					edges++
				}
			}
			if (edges == 1) != tc.wantEdge {
				t.Fatalf("builds edges = %d, want edge %v", edges, tc.wantEdge)
			}
		})
	}
}
