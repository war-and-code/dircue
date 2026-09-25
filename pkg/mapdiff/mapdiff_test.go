package mapdiff_test

import (
	"bytes"
	"slices"
	"strings"
	"testing"

	"github.com/war-and-code/dircue/pkg/mapdiff"
	"github.com/war-and-code/dircue/pkg/mapdoc"
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

func TestProviderCoverageChangeIsSeparateFromSourceMaterial(t *testing.T) {
	node := component("service")
	base := testDocument("aaa", node, mapdoc.CoverageComplete)
	head := testDocument("aaa", node, mapdoc.CoverageComplete)
	base.CoverageLedger = []mapdoc.CoverageLedgerEntry{{
		Tool: "syft", ReportKind: "syft-json", Scope: ".", Binding: "unknown", Ran: true,
		CoveredFiles: []string{"go.mod"}, State: "covered_files_reported", Reason: "report_has_no_snapshot_identity",
	}}
	head.CoverageLedger = []mapdoc.CoverageLedgerEntry{{
		Tool: "syft", ReportKind: "syft-json", Scope: ".", Binding: "unknown", Ran: true,
		CoveredFiles: []string{"go.mod", "sub/go.mod"}, State: "covered_files_reported", Reason: "report_has_no_snapshot_identity",
	}}
	report, err := mapdiff.Compare(base, head)
	if err != nil {
		t.Fatal(err)
	}
	if report.Status != "unchanged" || report.Counts.Material != 0 {
		t.Fatalf("provider coverage became a source-material change: %#v", report)
	}
	if report.CoverageLedgerStatus != "changed" || len(report.CoverageLedgerChanges) != 1 {
		t.Fatalf("provider coverage change not preserved: %#v", report)
	}
	change := report.CoverageLedgerChanges[0]
	if change.Status != "changed" || !slices.Contains(change.Fields, "covered_files") {
		t.Fatalf("unexpected provider coverage detail: %#v", change)
	}
	if !slices.Contains(report.Caveats, "provider run coverage changed separately from source material") {
		t.Fatalf("provider coverage caveat absent: %#v", report.Caveats)
	}
}

func TestProviderAttachmentEntitiesAreSeparateFromSourceMaterial(t *testing.T) {
	node := component("service")
	without := testDocument("same-tree", node, mapdoc.CoverageComplete)
	with := withSyftAttachment(testDocument("same-tree", node, mapdoc.CoverageComplete))

	added, err := mapdiff.Compare(without, with)
	if err != nil {
		t.Fatal(err)
	}
	if added.Status != "unchanged" || added.Counts.Material != 0 || len(added.Changes) != 0 {
		t.Fatalf("provider attachment became a material source change: %#v", added)
	}
	if added.ProviderStatus != "changed" || len(added.ProviderChanges) != 3 {
		t.Fatalf("provider attachment additions not separated: %#v", added)
	}
	for _, change := range added.ProviderChanges {
		if change.Status != "added" || change.Material || change.Certainty != "observed" {
			t.Fatalf("unexpected provider addition: %#v", change)
		}
	}

	removed, err := mapdiff.Compare(with, without)
	if err != nil {
		t.Fatal(err)
	}
	if removed.Status != "unchanged" || removed.Counts.Material != 0 || removed.Counts.IndeterminateRemoval != 0 {
		t.Fatalf("provider removal changed source status: %#v", removed)
	}
	if removed.ProviderStatus != "indeterminate" || len(removed.ProviderChanges) != 3 {
		t.Fatalf("missing provider run did not retain uncertainty: %#v", removed)
	}
}

func TestProviderRemovalRemainsIndeterminateWithoutExhaustiveCoverage(t *testing.T) {
	node := component("service")
	base := withSyftAttachment(testDocument("same-tree", node, mapdoc.CoverageComplete))
	head := testDocument("same-tree", node, mapdoc.CoverageComplete)
	head.CoverageLedger = slices.Clone(base.CoverageLedger)

	report, err := mapdiff.Compare(base, head)
	if err != nil {
		t.Fatal(err)
	}
	if report.Status != "unchanged" || report.ProviderStatus != "indeterminate" {
		t.Fatalf("non-exhaustive provider run confirmed an absence: %#v", report)
	}
	for _, change := range report.ProviderChanges {
		if change.Certainty != "indeterminate" || !strings.Contains(change.Reason, "exhaustive") {
			t.Fatalf("provider removal uncertainty was not retained: %#v", change)
		}
	}
}

func TestProviderFactOnNativeNodeIsNotMaterial(t *testing.T) {
	baseNode := component("service")
	headNode := component("service")
	headNode.Facts = []mapdoc.Fact{{
		Kind: "provider_annotation", Value: "observed", State: "provider_reported",
		Coverage: mapdoc.Coverage{Status: mapdoc.CoveragePartial, Reasons: []string{"provider_scope_not_exhaustive"}},
		Evidence: []mapdoc.Evidence{{
			Basis: mapdoc.BasisProviderReported, Path: "go.mod", SourceKind: mapdoc.SourceFile,
			Provider: &mapdoc.Producer{ID: "external", Version: "1"},
		}},
	}}
	report, err := mapdiff.Compare(testDocument("same-tree", baseNode, mapdoc.CoverageComplete), testDocument("same-tree", headNode, mapdoc.CoverageComplete))
	if err != nil {
		t.Fatal(err)
	}
	if report.Status != "unchanged" || report.Counts.Material != 0 || len(report.Changes) != 1 || report.Changes[0].Material {
		t.Fatalf("provider fact on native node became material: %#v", report)
	}
}

func withSyftAttachment(document mapdoc.Document) mapdoc.Document {
	evidence := []mapdoc.Evidence{{
		Basis: mapdoc.BasisProviderReported, Path: "go.mod", SourceKind: mapdoc.SourceFile,
		Provider: &mapdoc.Producer{ID: "syft", Version: "1.0.0"},
	}}
	tool := mapdoc.NewNode(mapdoc.NodeToolRun, []string{"."}, "syft:attachment:0")
	tool.Name = "syft"
	tool.Coverage = mapdoc.Coverage{Status: mapdoc.CoverageComplete}
	tool.Evidence = slices.Clone(evidence)
	tool.Facts = []mapdoc.Fact{{Kind: "run_metadata", State: "provider_reported", Coverage: mapdoc.Coverage{Status: mapdoc.CoverageComplete}, Evidence: slices.Clone(evidence)}}
	packageNode := mapdoc.NewNode(mapdoc.NodePackage, []string{"go.mod"}, "go:example.org/dependency@v1.0.0")
	packageNode.Name = "example.org/dependency"
	packageNode.Coverage = mapdoc.Coverage{Status: mapdoc.CoverageComplete}
	packageNode.Evidence = slices.Clone(evidence)
	edge := mapdoc.NewEdge(mapdoc.EdgeAnalyzedBy, packageNode.ID, tool.ID, "")
	edge.Coverage = mapdoc.Coverage{Status: mapdoc.CoverageComplete}
	edge.Evidence = slices.Clone(evidence)
	document.Nodes = append(document.Nodes, tool, packageNode)
	document.Edges = append(document.Edges, edge)
	document.CoverageLedger = []mapdoc.CoverageLedgerEntry{{
		Tool: "syft", ReportKind: "syft-json", Scope: ".", Binding: "verified", Ran: true,
		CoveredFiles: []string{"go.mod"}, State: "covered_files_reported",
	}}
	return document
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

func TestWriteMarkdownUnchanged(t *testing.T) {
	node := mapdoc.NewNode(mapdoc.NodeComponent, []string{"go.mod"}, "go")
	node.Name = "api"
	node.Coverage = mapdoc.Coverage{Status: mapdoc.CoverageComplete}
	node.Evidence = []mapdoc.Evidence{{Basis: mapdoc.BasisDeclaredConfig, Path: "go.mod", SourceKind: mapdoc.SourceConfiguration, Rule: &mapdoc.Producer{ID: "test", Version: "1"}}}
	doc := testDocument("abc", node, mapdoc.CoverageComplete)
	report, err := mapdiff.Compare(doc, doc)
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := mapdiff.WriteMarkdown(&buf, report); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "## Architecture diff:") {
		t.Fatalf("missing heading:\n%s", out)
	}
	if !strings.Contains(out, "No material changes") {
		t.Fatalf("expected unchanged label:\n%s", out)
	}
}

func TestWriteMarkdownChanged(t *testing.T) {
	nodeA := mapdoc.NewNode(mapdoc.NodeComponent, []string{"go.mod"}, "go")
	nodeA.Name = "service-a"
	nodeA.Coverage = mapdoc.Coverage{Status: mapdoc.CoverageComplete}
	nodeA.Evidence = []mapdoc.Evidence{{Basis: mapdoc.BasisDeclaredConfig, Path: "go.mod", SourceKind: mapdoc.SourceConfiguration, Rule: &mapdoc.Producer{ID: "test", Version: "1"}}}
	nodeB := mapdoc.NewNode(mapdoc.NodeComponent, []string{"go.mod"}, "go")
	nodeB.Name = "service-b"
	nodeB.Coverage = mapdoc.Coverage{Status: mapdoc.CoverageComplete}
	nodeB.Evidence = []mapdoc.Evidence{{Basis: mapdoc.BasisDeclaredConfig, Path: "go.mod", SourceKind: mapdoc.SourceConfiguration, Rule: &mapdoc.Producer{ID: "test", Version: "1"}}}
	base := testDocument("aaa", nodeA, mapdoc.CoverageComplete)
	head := testDocument("bbb", nodeB, mapdoc.CoverageComplete)
	report, err := mapdiff.Compare(base, head)
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := mapdiff.WriteMarkdown(&buf, report); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "material change") {
		t.Fatalf("expected material change in heading:\n%s", out)
	}
	// Source binding and observer compatibility must appear.
	if !strings.Contains(out, "**Source binding:**") {
		t.Fatalf("missing source binding:\n%s", out)
	}
}

func TestWriteMarkdownIndeterminate(t *testing.T) {
	node := mapdoc.NewNode(mapdoc.NodeComponent, []string{"go.mod"}, "go")
	node.Name = "svc"
	node.Coverage = mapdoc.Coverage{Status: mapdoc.CoverageComplete}
	node.Evidence = []mapdoc.Evidence{{Basis: mapdoc.BasisDeclaredConfig, Path: "go.mod", SourceKind: mapdoc.SourceConfiguration, Rule: &mapdoc.Producer{ID: "test", Version: "1"}}}
	base := testDocument("aaa", node, mapdoc.CoverageComplete)
	headDoc := testDocument("bbb", node, mapdoc.CoveragePartial)
	headDoc.Nodes = nil
	report, err := mapdiff.Compare(base, headDoc)
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := mapdiff.WriteMarkdown(&buf, report); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "indeterminate") && !strings.Contains(out, "Indeterminate") {
		t.Fatalf("indeterminate removal not disclosed:\n%s", out)
	}
}
