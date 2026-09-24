package mapbuild

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"dircue/pkg/deployables"
	"dircue/pkg/mapdoc"
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
