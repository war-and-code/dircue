package coverageledger_test

import (
	"testing"

	"dircue/pkg/coverageledger"
	"dircue/pkg/mapdoc"
)

func component(language string) mapdoc.Node {
	n := mapdoc.NewNode(mapdoc.NodeComponent, []string{"services/api", "services/api/go.mod"}, "go")
	n.Properties = map[string]string{"root": "services/api", "language": language}
	n.Coverage = mapdoc.Coverage{Status: mapdoc.CoverageComplete}
	n.Evidence = []mapdoc.Evidence{{Basis: mapdoc.BasisDeclaredConfig, Path: "services/api/go.mod", SourceKind: mapdoc.SourceConfiguration, Rule: &mapdoc.Producer{ID: "test", Version: "1"}}}
	return n
}

func TestReconcileDistinguishesRunStatesWithoutClaimingCompleteness(t *testing.T) {
	c := component("go")
	d := mapdoc.New()
	d.Nodes = []mapdoc.Node{c}
	d.CoverageLedger = []mapdoc.CoverageLedgerEntry{
		{Tool: "bifrost", ReportKind: "bifrost-code-query-json", Scope: ".", Binding: "verified", Ran: true, CoveredFiles: []string{"services/api/main.go"}, State: "selected_query_reported"},
		{Tool: "scc", ReportKind: "scc-json", Scope: "services/api", Binding: "verified", Ran: true, CoveredFiles: []string{}, State: "tool_error", Reason: "process_failed"},
	}
	coverageledger.Reconcile(&d)
	got := map[string]mapdoc.AnalyzerCoverageEntry{}
	for _, entry := range d.AnalyzerCoverage {
		got[entry.Tool] = entry
	}
	if got["bifrost"].NotCovered != "unknown" || len(got["bifrost"].CoveredFiles) != 1 || !got["bifrost"].Ran {
		t.Fatalf("bifrost=%+v", got["bifrost"])
	}
	if got["scc"].NotCovered != "tool_error" {
		t.Fatalf("scc=%+v", got["scc"])
	}
	if got["syft"].NotCovered != "not_run" || !got["syft"].ExpectedApplicable {
		t.Fatalf("syft=%+v", got["syft"])
	}
	if got["opentaint"].NotCovered != "unsupported_language" {
		t.Fatalf("opentaint=%+v", got["opentaint"])
	}
}

func TestReconcileReportsPrerequisiteAndUnknownLanguageHonestly(t *testing.T) {
	java := component("java")
	unknown := component("")
	unknown.ID = mapdoc.NodeID(unknown.Kind, unknown.Paths, "unknown")
	d := mapdoc.New()
	d.Nodes = []mapdoc.Node{java, unknown}
	coverageledger.Reconcile(&d)
	for _, entry := range d.AnalyzerCoverage {
		if entry.ComponentID == java.ID && entry.Tool == "opentaint" && entry.NotCovered != "prerequisite_unmet" {
			t.Fatalf("opentaint=%+v", entry)
		}
		if entry.ComponentID == unknown.ID && entry.NotCovered != "unknown" {
			t.Fatalf("unknown language fabricated support: %+v", entry)
		}
	}
}

func TestGenericSARIFProducerStaysUnknown(t *testing.T) {
	c := component("go")
	d := mapdoc.New()
	d.Nodes = []mapdoc.Node{c}
	d.CoverageLedger = []mapdoc.CoverageLedgerEntry{{Tool: "custom-lint", ReportKind: "sarif", Scope: ".", Binding: "unknown", Ran: true, CoveredFiles: []string{}, State: "unknown", Reason: "report_has_no_snapshot_identity"}}
	coverageledger.Reconcile(&d)
	for _, entry := range d.AnalyzerCoverage {
		if entry.Tool == "custom-lint" {
			if entry.NotCovered != "unknown" || entry.DescriptorSource == "" {
				t.Fatalf("generic=%+v", entry)
			}
			return
		}
	}
	t.Fatal("generic SARIF entry missing")
}

func TestUnboundProviderFilesDoNotBecomeSelectedSourceCoverage(t *testing.T) {
	c := component("go")
	d := mapdoc.New()
	d.Nodes = []mapdoc.Node{c}
	d.CoverageLedger = []mapdoc.CoverageLedgerEntry{{Tool: "bifrost", ReportKind: "bifrost-code-query-json", Scope: ".", Binding: "unknown", Ran: true, CoveredFiles: []string{"services/api/main.go"}, State: "selected_query_reported", Reason: "report_has_no_snapshot_identity"}}
	coverageledger.Reconcile(&d)
	for _, entry := range d.AnalyzerCoverage {
		if entry.Tool == "bifrost" {
			if !entry.Ran || len(entry.CoveredFiles) != 0 || entry.NotCovered != "unknown" || entry.Reason != "provider_snapshot_binding_unknown" {
				t.Fatalf("entry=%+v", entry)
			}
			return
		}
	}
	t.Fatal("bifrost entry missing")
}

func TestBuiltInDescriptorsAreVersionedAndSourced(t *testing.T) {
	seen := map[string]bool{}
	for _, descriptor := range coverageledger.Descriptors() {
		if descriptor.Tool == "" || descriptor.Source == "" || coverageledger.DescriptorVersion == "" || seen[descriptor.Tool] {
			t.Fatalf("invalid descriptor: %+v", descriptor)
		}
		seen[descriptor.Tool] = true
	}
	for _, tool := range []string{"syft", "opentaint", "noir", "bifrost", "bca", "scc", "sarif"} {
		if !seen[tool] {
			t.Fatalf("missing %s descriptor", tool)
		}
	}
}

func TestNoSourcePopulationProducesNoSyntheticAnalyzerBlindSpots(t *testing.T) {
	d := mapdoc.New()
	d.Coverage = []mapdoc.QuestionCoverage{{Question: "content", Scope: ".", Coverage: mapdoc.Coverage{Status: mapdoc.CoverageComplete}}}
	coverageledger.Reconcile(&d)
	if len(d.AnalyzerCoverage) != 0 || len(d.AnalyzerBlindSpots) != 0 {
		t.Fatalf("synthetic analyzer accounting: coverage=%+v blind_spots=%+v", d.AnalyzerCoverage, d.AnalyzerBlindSpots)
	}
	for _, question := range d.Coverage {
		if question.Question == "analyzer_coverage" {
			if question.Status != mapdoc.CoverageUnknown || len(question.Reasons) != 1 || question.Reasons[0] != "no_source_population_observed" {
				t.Fatalf("analyzer coverage=%+v", question)
			}
			return
		}
	}
	t.Fatal("analyzer_coverage question missing")
}
