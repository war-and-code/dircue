package mapdoc_test

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"dircue/pkg/mapdoc"
)

func evidence(path string) mapdoc.Evidence {
	return mapdoc.Evidence{Basis: mapdoc.BasisDeclaredConfig, Path: path, SourceKind: mapdoc.SourceConfiguration, Rule: &mapdoc.Producer{ID: "manifest", Version: "1.0.0"}}
}

func document() mapdoc.Document {
	a := mapdoc.NewNode(mapdoc.NodeComponent, []string{"b/package.json", "b/package.json"}, "npm")
	a.Name, a.Coverage, a.Evidence = "b", mapdoc.Coverage{Status: mapdoc.CoverageComplete}, []mapdoc.Evidence{evidence("b/package.json")}
	b := mapdoc.NewNode(mapdoc.NodeContent, []string{"a.go"}, "source")
	b.Name, b.Properties, b.Coverage, b.Evidence = "a.go", map[string]string{"role": "source"}, mapdoc.Coverage{Status: mapdoc.CoverageComplete}, []mapdoc.Evidence{{Basis: mapdoc.BasisFilenameHint, Path: "a.go", SourceKind: mapdoc.SourceFile, Rule: &mapdoc.Producer{ID: "content", Version: "1"}}}
	e := mapdoc.NewEdge(mapdoc.EdgeContains, a.ID, b.ID, "")
	e.Coverage, e.Evidence = mapdoc.Coverage{Status: mapdoc.CoverageComplete}, []mapdoc.Evidence{evidence("b/package.json")}
	return mapdoc.Document{SchemaVersion: mapdoc.SchemaVersion, Kind: "map", Status: mapdoc.CoverageComplete, Source: mapdoc.Source{Mode: "directory", Digest: &mapdoc.Digest{Algorithm: "sha256", Scope: "full_selected_tree", Value: strings.Repeat("a", 64)}}, Coverage: []mapdoc.QuestionCoverage{{Question: "components", Scope: ".", Coverage: mapdoc.Coverage{Status: mapdoc.CoverageComplete}}}, Nodes: []mapdoc.Node{a, b}, Edges: []mapdoc.Edge{e}}
}

func TestMarshalDeterministicAndDetached(t *testing.T) {
	d := document()
	first, err := mapdoc.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	d.Nodes[0], d.Nodes[1] = d.Nodes[1], d.Nodes[0]
	d.Nodes[1].Paths = []string{"b/package.json"}
	second, err := mapdoc.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) {
		t.Fatalf("different canonical output:\n%s\n%s", first, second)
	}
	if !bytes.HasSuffix(first, []byte("\n")) {
		t.Fatal("canonical JSON lacks final newline")
	}
}

func TestStableIDsArePortableAndOrderIndependent(t *testing.T) {
	a := mapdoc.NodeID(mapdoc.NodeComponent, []string{"z", "a"}, "go")
	b := mapdoc.NodeID(mapdoc.NodeComponent, []string{"a", "z"}, "go")
	if a != b {
		t.Fatalf("path order changed id: %q != %q", a, b)
	}
	if a == mapdoc.NodeID(mapdoc.NodeComponent, []string{"a", "z"}, "npm") {
		t.Fatal("discriminator did not affect id")
	}
	if strings.Contains(a, "/") {
		t.Fatalf("id exposed a path: %q", a)
	}
}

func TestRejectsAbsolutePathsAndDocumentationEvidence(t *testing.T) {
	for name, mutate := range map[string]func(*mapdoc.Document){
		"absolute node":      func(d *mapdoc.Document) { d.Nodes[0].Paths = []string{"/tmp/package.json"}; d.Nodes[0].ID = "" },
		"absolute evidence":  func(d *mapdoc.Document) { d.Nodes[0].Evidence[0].Path = `C:\\repo\\package.json` },
		"documentation path": func(d *mapdoc.Document) { d.Nodes[0].Evidence[0].Path = "README.md" },
		"comment source":     func(d *mapdoc.Document) { d.Nodes[0].Evidence[0].SourceKind = mapdoc.SourceComment },
		"docstring source":   func(d *mapdoc.Document) { d.Nodes[0].Evidence[0].SourceKind = mapdoc.SourceDocstring },
	} {
		t.Run(name, func(t *testing.T) {
			d := document()
			mutate(&d)
			if _, err := mapdoc.Marshal(d); err == nil {
				t.Fatal("invalid document accepted")
			}
		})
	}
}

func TestDocumentationContentMayEvidenceItself(t *testing.T) {
	d := document()
	n := mapdoc.NewNode(mapdoc.NodeContent, []string{"README.md"}, "documentation")
	n.Properties = map[string]string{"role": "documentation"}
	n.Coverage = mapdoc.Coverage{Status: mapdoc.CoverageComplete}
	n.Evidence = []mapdoc.Evidence{{Basis: mapdoc.BasisFilenameHint, Path: "README.md", SourceKind: mapdoc.SourceDocumentation, Rule: &mapdoc.Producer{ID: "content", Version: "1"}}}
	d.Nodes = append(d.Nodes, n)
	if _, err := mapdoc.Marshal(d); err != nil {
		t.Fatal(err)
	}
}

func TestUnknownRequiresReasonAndNeverMeansAbsent(t *testing.T) {
	d := document()
	d.Nodes[0].Coverage = mapdoc.Coverage{Status: mapdoc.CoverageUnknown}
	if _, err := mapdoc.Marshal(d); err == nil {
		t.Fatal("unknown coverage without a reason accepted")
	}
	d.Nodes[0].Coverage.Reasons = []string{"manifest exceeded byte limit"}
	if _, err := mapdoc.Marshal(d); err != nil {
		t.Fatal(err)
	}
}

func TestHTTPRoutePropertyIsNotTreatedAsAHostPath(t *testing.T) {
	d := document()
	d.Nodes[0].Properties = map[string]string{"route": "/users/{id}"}
	if _, err := mapdoc.Marshal(d); err != nil {
		t.Fatalf("HTTP route rejected as filesystem path: %v", err)
	}
	d.Nodes[0].Properties = map[string]string{"other": "/Users/alice/secret"}
	if _, err := mapdoc.Marshal(d); err == nil {
		t.Fatal("absolute host path accepted in ordinary property")
	}
}

func TestDirectorySourceMayBeHonestlyUnbound(t *testing.T) {
	d := document()
	d.Source.Digest = nil
	d.Coverage = append(d.Coverage, mapdoc.QuestionCoverage{Question: mapdoc.QuestionSourceBinding, Scope: ".", Coverage: mapdoc.Coverage{Status: mapdoc.CoverageUnknown, Reasons: []string{"full selected tree content was not read"}}})
	if _, err := mapdoc.Marshal(d); err != nil {
		t.Fatal(err)
	}
	d.Coverage[len(d.Coverage)-1].Status = mapdoc.CoverageComplete
	if _, err := mapdoc.Marshal(d); err == nil {
		t.Fatal("unbound directory source reported complete binding")
	}
}

func TestDirectoryDigestRequiresAlgorithmScopeAndValue(t *testing.T) {
	for name, digest := range map[string]*mapdoc.Digest{
		"algorithm": {Scope: "full_selected_tree", Value: strings.Repeat("a", 64)},
		"scope":     {Algorithm: "sha256", Value: strings.Repeat("a", 64)},
		"value":     {Algorithm: "sha256", Scope: "full_selected_tree"},
	} {
		t.Run(name, func(t *testing.T) {
			d := document()
			d.Source.Digest = digest
			if _, err := mapdoc.Marshal(d); err == nil {
				t.Fatal("incomplete digest accepted")
			}
		})
	}
}

func TestCoverageLedgerRequiresConfinedFilesAndBinding(t *testing.T) {
	d := document()
	d.CoverageLedger = []mapdoc.CoverageLedgerEntry{{Tool: "syft", ReportKind: "syft-json", Scope: ".", Binding: "unknown", Ran: true, CoveredFiles: []string{"b/package.json"}, State: "covered_files_reported", Reason: "report_has_no_snapshot_identity"}}
	if _, err := mapdoc.Marshal(d); err != nil {
		t.Fatalf("valid provider-run ledger rejected: %v", err)
	}
	d.CoverageLedger[0].CoveredFiles = []string{"../../outside"}
	if _, err := mapdoc.Marshal(d); err == nil {
		t.Fatal("provider-run ledger accepted a path outside the selected root")
	}
	d.CoverageLedger[0].CoveredFiles = []string{"b/package.json"}
	d.CoverageLedger[0].Binding = "clean"
	if _, err := mapdoc.Marshal(d); err == nil {
		t.Fatal("provider-run ledger accepted an invalid binding")
	}
}

func TestDirectoryEvidenceOnlySupportsContentNodes(t *testing.T) {
	d := document()
	d.Nodes[1].Evidence = []mapdoc.Evidence{{Basis: mapdoc.BasisRuleInferred, Path: ".", SourceKind: mapdoc.SourceDirectory, Rule: &mapdoc.Producer{ID: "inventory", Version: "1"}}}
	if _, err := mapdoc.Marshal(d); err != nil {
		t.Fatal(err)
	}
	d.Nodes[0].Evidence = d.Nodes[1].Evidence
	if _, err := mapdoc.Marshal(d); err == nil {
		t.Fatal("directory aggregate evidence supported a component")
	}
}

func TestDocumentationOnlyRootCanEvidenceDocumentationContent(t *testing.T) {
	d := document()
	n := mapdoc.NewNode(mapdoc.NodeContent, []string{"."}, "documentation")
	n.Properties = map[string]string{"role": "documentation"}
	n.Coverage = mapdoc.Coverage{Status: mapdoc.CoverageComplete}
	n.Evidence = []mapdoc.Evidence{{Basis: mapdoc.BasisRuleInferred, Path: ".", SourceKind: mapdoc.SourceDirectory, Rule: &mapdoc.Producer{ID: "content-population", Version: "1"}}}
	d.Nodes = append(d.Nodes, n)
	if _, err := mapdoc.Marshal(d); err != nil {
		t.Fatal(err)
	}
}

func TestUnmarshalStrict(t *testing.T) {
	raw, err := mapdoc.Marshal(document())
	if err != nil {
		t.Fatal(err)
	}
	var value map[string]any
	if err := json.Unmarshal(raw, &value); err != nil {
		t.Fatal(err)
	}
	value["invented"] = true
	bad, _ := json.Marshal(value)
	if _, err := mapdoc.UnmarshalStrict(bad); err == nil {
		t.Fatal("unknown field accepted")
	}
	if _, err := mapdoc.UnmarshalStrict(append(raw, raw...)); err == nil {
		t.Fatal("trailing JSON accepted")
	}
}
