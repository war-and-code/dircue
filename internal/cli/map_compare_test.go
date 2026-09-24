package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/war-and-code/dircue/pkg/mapdiff"
	"github.com/war-and-code/dircue/pkg/mapdoc"
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

// TestMapCompareRejectsProfileDocument verifies that dircue map compare returns
// a helpful error when given a legacy profile document and points to dircue compare.
func TestMapCompareRejectsProfileDocument(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\nfunc main() {}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	// Build a legacy profile document.
	profileOut, _, err := invoke("analyze", "all", "--json", "--source", "directory", root)
	if err != nil {
		t.Fatal(err)
	}
	profileFile := filepath.Join(root, "profile.json")
	if err := os.WriteFile(profileFile, []byte(profileOut), 0600); err != nil {
		t.Fatal(err)
	}
	// Also need a valid map to form the argument pair.
	mapOut, _, err := invoke("map", "--source", "directory", "--json", root)
	if err != nil {
		t.Fatal(err)
	}
	mapFile := filepath.Join(root, "map.json")
	if err := os.WriteFile(mapFile, []byte(mapOut), 0600); err != nil {
		t.Fatal(err)
	}
	// Passing a profile as either argument should produce a helpful error.
	_, stderr, err := invoke("map", "compare", profileFile, mapFile)
	if err == nil {
		t.Fatal("expected error when passing profile document to map compare")
	}
	if !strings.Contains(err.Error()+stderr, "dircue compare") {
		t.Fatalf("error should mention 'dircue compare'; got err=%v stderr=%q", err, stderr)
	}
}

func TestMapCompareMarkdownFormat(t *testing.T) {
	base := writeComparisonMapFixture(t, "base.json", mapComparisonFixture("before", "aaa", true))
	head := writeComparisonMapFixture(t, "head.json", mapComparisonFixture("after", "bbb", true))
	out, stderr, err := invoke("map", "compare", "--format", "markdown", base, head)
	if err != nil || stderr != "" {
		t.Fatalf("stderr=%q err=%v", stderr, err)
	}
	if !strings.Contains(out, "## Architecture diff:") {
		t.Fatalf("markdown output missing heading:\n%s", out)
	}
	if !strings.Contains(out, "**Source binding:**") {
		t.Fatalf("markdown output missing source binding:\n%s", out)
	}
}

func TestMapCompareMarkdownUnchanged(t *testing.T) {
	base := writeComparisonMapFixture(t, "base.json", mapComparisonFixture("svc", "aaa", true))
	head := writeComparisonMapFixture(t, "head.json", mapComparisonFixture("svc", "aaa", true))
	out, stderr, err := invoke("map", "compare", "--format", "markdown", base, head)
	if err != nil || stderr != "" {
		t.Fatalf("stderr=%q err=%v", stderr, err)
	}
	if !strings.Contains(out, "No material changes") {
		t.Fatalf("expected unchanged status in markdown:\n%s", out)
	}
}

func TestMapCompareChangedExitsZero(t *testing.T) {
	// A changed comparison always exits 0; only I/O or usage errors are non-zero.
	base := writeComparisonMapFixture(t, "base.json", mapComparisonFixture("before", "aaa", true))
	head := writeComparisonMapFixture(t, "head.json", mapComparisonFixture("after", "bbb", true))
	_, stderr, err := invoke("map", "compare", base, head)
	if err != nil || stderr != "" {
		t.Fatalf("expected exit 0 for changed comparison: stderr=%q err=%v", stderr, err)
	}
}

func TestMapCompareRejectsUnknownFormat(t *testing.T) {
	base := writeComparisonMapFixture(t, "base.json", mapComparisonFixture("svc", "aaa", true))
	head := writeComparisonMapFixture(t, "head.json", mapComparisonFixture("svc", "aaa", true))
	_, _, err := invoke("map", "compare", "--format", "xml", base, head)
	if err == nil || !strings.Contains(err.Error(), "unsupported --format") {
		t.Fatalf("expected error for unknown format; got %v", err)
	}
}

func TestMapCompareMarkdownDisclosesIndeterminateRemoval(t *testing.T) {
	base := writeComparisonMapFixture(t, "base.json", mapComparisonFixture("service", "aaa", true))
	headDoc := mapComparisonFixture("unused", "bbb", false)
	headDoc.Nodes = nil
	head := writeComparisonMapFixture(t, "head.json", headDoc)
	out, stderr, err := invoke("map", "compare", "--format", "markdown", base, head)
	if err != nil || stderr != "" {
		t.Fatalf("stderr=%q err=%v", stderr, err)
	}
	if !strings.Contains(out, "indeterminate") && !strings.Contains(out, "Indeterminate") {
		t.Fatalf("markdown hides indeterminate removal:\n%s", out)
	}
}
