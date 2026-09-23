package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"dircue/pkg/mapdiff"
	"dircue/pkg/mapdoc"
)

func mapComparisonFixture(name, tree string, complete bool) mapdoc.Document {
	status := mapdoc.CoverageComplete
	reasons := []string{}
	if !complete {
		status = mapdoc.CoveragePartial
		reasons = []string{"test_limit"}
	}
	node := mapdoc.NewNode(mapdoc.NodeComponent, []string{"go.mod"}, "go")
	node.Name = name
	node.Coverage = mapdoc.Coverage{Status: mapdoc.CoverageComplete}
	node.Evidence = []mapdoc.Evidence{{Basis: mapdoc.BasisDeclaredConfig, Path: "go.mod", SourceKind: mapdoc.SourceConfiguration, Rule: &mapdoc.Producer{ID: "test", Version: "1"}}}
	return mapdoc.Document{
		SchemaVersion: mapdoc.SchemaVersion, Kind: "map", Status: status,
		Source: mapdoc.Source{Mode: "git", Revision: tree, Tree: tree},
		Coverage: []mapdoc.QuestionCoverage{
			{Question: "components", Scope: ".", Coverage: mapdoc.Coverage{Status: status, Reasons: reasons}},
			{Question: "source_binding", Scope: ".", Coverage: mapdoc.Coverage{Status: mapdoc.CoverageComplete}},
		},
		Nodes: []mapdoc.Node{node}, Edges: []mapdoc.Edge{},
	}
}

func writeComparisonMapFixture(t *testing.T, name string, document mapdoc.Document) string {
	t.Helper()
	data, err := mapdoc.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestMapCompareJSON(t *testing.T) {
	base := writeComparisonMapFixture(t, "base.json", mapComparisonFixture("before", "aaa", true))
	head := writeComparisonMapFixture(t, "head.json", mapComparisonFixture("after", "bbb", true))
	out, stderr, err := invoke("map", "compare", "--json", base, head)
	if err != nil || stderr != "" {
		t.Fatalf("stderr=%q err=%v", stderr, err)
	}
	var report mapdiff.Report
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatal(err)
	}
	if report.Status != "changed" || report.Counts.Material != 1 {
		t.Fatalf("unexpected report: %#v", report)
	}
}

func TestMapCompareTextDisclosesIndeterminateRemoval(t *testing.T) {
	base := writeComparisonMapFixture(t, "base.json", mapComparisonFixture("service", "aaa", true))
	headDoc := mapComparisonFixture("unused", "bbb", false)
	headDoc.Nodes = nil
	head := writeComparisonMapFixture(t, "head.json", headDoc)
	out, stderr, err := invoke("map", "compare", base, head)
	if err != nil || stderr != "" {
		t.Fatalf("stderr=%q err=%v", stderr, err)
	}
	if !strings.Contains(out, "Map comparison: indeterminate") || !strings.Contains(out, "Indeterminate removals: 1") || !strings.Contains(out, "Caveat:") {
		t.Fatalf("text output hides uncertainty:\n%s", out)
	}
}

func TestMapCompareRejectsScanFlagsAndNonMapInput(t *testing.T) {
	document := writeComparisonMapFixture(t, "map.json", mapComparisonFixture("service", "aaa", true))
	if _, _, err := invoke("map", "compare", "--source", "directory", document, document); err == nil || !strings.Contains(err.Error(), "does not apply") {
		t.Fatalf("scan flag accepted: %v", err)
	}
	bad := filepath.Join(t.TempDir(), "bad.json")
	if err := os.WriteFile(bad, []byte(`{"kind":"profile"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := invoke("map", "compare", bad, document); err == nil || !strings.Contains(err.Error(), "base map") {
		t.Fatalf("invalid map accepted: %v", err)
	}
}
