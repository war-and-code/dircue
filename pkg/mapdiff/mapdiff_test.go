package mapdiff_test

import (
	"bytes"
	"strings"
	"testing"

	"dircue/pkg/mapdiff"
	"dircue/pkg/mapdoc"
)

func testDocument(tree string, component mapdoc.Node, status mapdoc.CoverageStatus) mapdoc.Document {
	reasons := []string{}
	if status != mapdoc.CoverageComplete {
		reasons = []string{"test limit"}
	}
	return mapdoc.Document{
		SchemaVersion: mapdoc.SchemaVersion,
		Kind:          "map",
		Status:        status,
		Source:        mapdoc.Source{Mode: "git", Revision: tree, Tree: tree},
		Coverage: []mapdoc.QuestionCoverage{
			{Question: "components", Scope: ".", Coverage: mapdoc.Coverage{Status: status, Reasons: reasons}},
			{Question: "source_binding", Scope: ".", Coverage: mapdoc.Coverage{Status: mapdoc.CoverageComplete}},
		},
		Nodes: []mapdoc.Node{component},
		Edges: []mapdoc.Edge{},
	}
}

func component(name string) mapdoc.Node {
	node := mapdoc.NewNode(mapdoc.NodeComponent, []string{"go.mod"}, "go")
	node.Name = name
	node.Coverage = mapdoc.Coverage{Status: mapdoc.CoverageComplete}
	node.Evidence = []mapdoc.Evidence{{
		Basis: mapdoc.BasisDeclaredConfig, Path: "go.mod", SourceKind: mapdoc.SourceConfiguration,
		Rule: &mapdoc.Producer{ID: "go-manifest", Version: "1"},
	}}
	return node
}

func TestCompareFindsMaterialChangesByStableID(t *testing.T) {
	baseNode := component("before")
	headNode := component("after")
	report, err := mapdiff.Compare(testDocument("aaa", baseNode, mapdoc.CoverageComplete), testDocument("bbb", headNode, mapdoc.CoverageComplete))
	if err != nil {
		t.Fatal(err)
	}
	if report.Status != "changed" || report.SourceBinding != "different" || report.Counts.Material != 1 {
		t.Fatalf("unexpected summary: %#v", report)
	}
	if len(report.Changes) != 1 || report.Changes[0].Status != "changed" || !report.Changes[0].Material || !bytes.Equal([]byte(strings.Join(report.Changes[0].Fields, ",")), []byte("name")) {
		t.Fatalf("unexpected changes: %#v", report.Changes)
	}
}

func TestPartialHeadDoesNotConfirmRemoval(t *testing.T) {
	baseNode := component("service")
	head := testDocument("bbb", component("temporary"), mapdoc.CoveragePartial)
	head.Nodes = nil
	report, err := mapdiff.Compare(testDocument("aaa", baseNode, mapdoc.CoverageComplete), head)
	if err != nil {
		t.Fatal(err)
	}
	if report.Status != "indeterminate" || report.Counts.IndeterminateRemoval != 1 || report.Counts.Material != 0 {
		t.Fatalf("unexpected removal summary: %#v", report)
	}
	if got := report.Changes[0]; got.Certainty != "indeterminate" || got.Reason == "" {
		t.Fatalf("removal lacks caveat: %#v", got)
	}
}

func TestEvidenceOnlyChangeIsNotMaterial(t *testing.T) {
	baseNode := component("service")
	headNode := component("service")
	headNode.Evidence[0].Rule.Version = "2"
	report, err := mapdiff.Compare(testDocument("aaa", baseNode, mapdoc.CoverageComplete), testDocument("bbb", headNode, mapdoc.CoverageComplete))
	if err != nil {
		t.Fatal(err)
	}
	if report.Status != "unchanged" || report.Counts.Changed != 1 || report.Counts.Material != 0 || report.ObserverCompatibility != "different" || len(report.Caveats) < 2 {
		t.Fatalf("producer-only change became material: %#v", report)
	}
}

func TestUnboundDirectoriesExposeSourceCaveat(t *testing.T) {
	node := component("service")
	base := testDocument("aaa", node, mapdoc.CoverageComplete)
	head := testDocument("bbb", node, mapdoc.CoverageComplete)
	for _, document := range []*mapdoc.Document{&base, &head} {
		document.Source = mapdoc.Source{Mode: "directory"}
		document.Coverage = append(document.Coverage, mapdoc.QuestionCoverage{
			Question: mapdoc.QuestionSourceBinding, Scope: ".",
			Coverage: mapdoc.Coverage{Status: mapdoc.CoverageUnknown, Reasons: []string{"directory not fully hashed"}},
		})
		document.Coverage[1] = document.Coverage[len(document.Coverage)-1]
		document.Coverage = document.Coverage[:2]
	}
	report, err := mapdiff.Compare(base, head)
	if err != nil {
		t.Fatal(err)
	}
	if report.SourceBinding != "unknown" || len(report.Caveats) == 0 {
		t.Fatalf("missing source caveat: %#v", report)
	}
}

func TestComparisonOutputIsDeterministic(t *testing.T) {
	a := component("a")
	b := component("b")
	b.Paths = []string{"b/go.mod"}
	b.ID = mapdoc.NodeID(b.Kind, b.Paths, b.Discriminator)
	b.Evidence[0].Path = "b/go.mod"
	base := testDocument("aaa", a, mapdoc.CoverageComplete)
	head := testDocument("bbb", b, mapdoc.CoverageComplete)
	first, err := mapdiff.Compare(base, head)
	if err != nil {
		t.Fatal(err)
	}
	second, err := mapdiff.Compare(base, head)
	if err != nil {
		t.Fatal(err)
	}
	left, _ := mapdiff.Marshal(first)
	right, _ := mapdiff.Marshal(second)
	if !bytes.Equal(left, right) {
		t.Fatalf("nondeterministic output:\n%s\n%s", left, right)
	}
}

func FuzzStrictMapSelfComparison(f *testing.F) {
	seed, err := mapdoc.Marshal(documentForFuzz())
	if err != nil {
		f.Fatal(err)
	}
	f.Add(seed)
	f.Add([]byte(`{"kind":"map"}`))
	f.Fuzz(func(t *testing.T, data []byte) {
		document, err := mapdoc.UnmarshalStrict(data)
		if err != nil {
			return
		}
		first, err := mapdiff.Compare(document, document)
		if err != nil {
			t.Fatal(err)
		}
		second, err := mapdiff.Compare(document, document)
		if err != nil {
			t.Fatal(err)
		}
		left, err := mapdiff.Marshal(first)
		if err != nil {
			t.Fatal(err)
		}
		right, err := mapdiff.Marshal(second)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(left, right) || first.Status != "unchanged" || first.Counts.Material != 0 || len(first.Changes) != 0 {
			t.Fatalf("self-comparison invariant failed:\n%s\n%s", left, right)
		}
	})
}

func documentForFuzz() mapdoc.Document {
	node := component("fuzz")
	return testDocument("fuzz-tree", node, mapdoc.CoverageComplete)
}
