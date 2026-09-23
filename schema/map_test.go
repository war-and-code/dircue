package schema_test

import (
	"strings"
	"testing"

	"dircue/pkg/mapdoc"
)

func schemaMapNode(kind mapdoc.NodeKind, source mapdoc.EvidenceSource) mapdoc.Node {
	n := mapdoc.NewNode(kind, []string{"."}, string(kind))
	n.Coverage = mapdoc.Coverage{Status: mapdoc.CoverageComplete, Reasons: []string{}}
	n.Evidence = []mapdoc.Evidence{{Basis: mapdoc.BasisRuleInferred, Path: ".", SourceKind: source, Rule: &mapdoc.Producer{ID: "fixture", Version: "1"}}}
	return n
}

func TestMapSchemaDirectoryBindingAndAggregateEvidence(t *testing.T) {
	_, compiled := compileExportPair(t, "map")
	base := mapdoc.Document{
		SchemaVersion: mapdoc.SchemaVersion,
		Kind:          "map",
		Status:        mapdoc.CoverageComplete,
		Source:        mapdoc.Source{Mode: "directory"},
		Coverage: []mapdoc.QuestionCoverage{{
			Question: mapdoc.QuestionSourceBinding,
			Scope:    ".",
			Coverage: mapdoc.Coverage{Status: mapdoc.CoverageUnknown, Reasons: []string{"full selected tree content was not read"}},
		}},
		Nodes: []mapdoc.Node{schemaMapNode(mapdoc.NodeContent, mapdoc.SourceDirectory)},
		Edges: []mapdoc.Edge{},
	}
	if err := compiled.Validate(exportJSONValue(t, base)); err != nil {
		t.Fatalf("unbound directory map rejected: %v", err)
	}
	bound := base
	bound.Source.Digest = &mapdoc.Digest{Algorithm: "sha256", Scope: "full_selected_tree", Value: strings.Repeat("a", 64)}
	bound.Coverage = []mapdoc.QuestionCoverage{}
	if err := compiled.Validate(exportJSONValue(t, bound)); err != nil {
		t.Fatalf("explicitly bound directory map rejected: %v", err)
	}
	missingQualification := base
	missingQualification.Coverage = []mapdoc.QuestionCoverage{}
	if err := compiled.Validate(exportJSONValue(t, missingQualification)); err == nil {
		t.Fatal("unbound directory map without qualified source coverage accepted")
	}
	wrongAggregate := base
	wrongAggregate.Nodes = []mapdoc.Node{schemaMapNode(mapdoc.NodeComponent, mapdoc.SourceDirectory)}
	if err := compiled.Validate(exportJSONValue(t, wrongAggregate)); err == nil {
		t.Fatal("directory aggregate evidence accepted for a non-content node")
	}
}
