package providerjoin_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"dircue/pkg/mapdoc"
	"dircue/pkg/providerjoin"
)

func attachment(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "report.json")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func component() mapdoc.Node {
	n := mapdoc.NewNode(mapdoc.NodeComponent, []string{"services/api"}, "go")
	n.Name = "api"
	n.Properties = map[string]string{"language": "Go"}
	n.Coverage = mapdoc.Coverage{Status: mapdoc.CoverageComplete}
	n.Evidence = []mapdoc.Evidence{{Basis: mapdoc.BasisDeclaredConfig, Path: "services/api/go.mod", SourceKind: mapdoc.SourceConfiguration, Rule: &mapdoc.Producer{ID: "test", Version: "1"}}}
	return n
}

func TestSyftFactsAreBoundAndFindingsAreNotPossible(t *testing.T) {
	report := `{"descriptor":{"name":"syft","version":"1.20"},"source":{"metadata":{"dircue_snapshot_tree":"abc"}},"artifacts":[{"id":"a","name":"app","version":"1","type":"go-module","purl":"pkg:golang/app@1","locations":[{"path":"services/api/go.mod"},{"path":"/Users/alice/secret"}]},{"id":"b","name":"dep","version":"2","type":"go-module","purl":"pkg:golang/dep@2"}],"artifactRelationships":[{"parent":"a","child":"b","type":"dependency-of"}]}`
	r, err := providerjoin.Join(context.Background(), providerjoin.Input{Snapshot: providerjoin.Snapshot{Mode: "git", Tree: "abc"}, Nodes: []mapdoc.Node{component()}}, []providerjoin.Attachment{{Kind: "syft-json", Path: attachment(t, report)}}, providerjoin.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Nodes) != 3 || len(r.Edges) != 4 {
		t.Fatalf("nodes/edges = %d/%d", len(r.Nodes), len(r.Edges))
	}
	if r.Ledger[0].Binding != providerjoin.BindingVerified {
		t.Fatalf("binding=%s", r.Ledger[0].Binding)
	}
	associated := false
	for _, edge := range r.Edges {
		associated = associated || edge.Type == mapdoc.EdgePackagedIn && edge.To == component().ID
	}
	if !associated {
		t.Fatal("package location was not associated with its nearest component")
	}
	encoded, marshalErr := mapdoc.Marshal(mapdoc.Document{SchemaVersion: mapdoc.SchemaVersion, Kind: "map", Status: mapdoc.CoveragePartial, Source: mapdoc.Source{Mode: "git", Revision: "HEAD", Tree: "abc"}, Coverage: r.Coverage, Nodes: append([]mapdoc.Node{component()}, r.Nodes...), Edges: r.Edges})
	if marshalErr != nil {
		t.Fatal(marshalErr)
	}
	if strings.Contains(string(encoded), "/Users/") {
		t.Fatal("host path leaked")
	}
}

func TestMismatchAndUnboundDirectoryStayQualified(t *testing.T) {
	p := attachment(t, `{"descriptor":{"name":"syft","version":"1"},"source":{"metadata":{"dircue_snapshot_tree":"other"}},"artifacts":[]}`)
	r, err := providerjoin.Join(context.Background(), providerjoin.Input{Snapshot: providerjoin.Snapshot{Mode: "git", Tree: "selected"}}, []providerjoin.Attachment{{Kind: "syft", Path: p}}, providerjoin.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if r.Ledger[0].Binding != providerjoin.BindingMismatch || r.Ledger[0].Reason != "report_tree_mismatch" {
		t.Fatalf("ledger=%+v", r.Ledger[0])
	}
	p = attachment(t, `{"descriptor":{"name":"syft","version":"1"},"artifacts":[]}`)
	r, err = providerjoin.Join(context.Background(), providerjoin.Input{Snapshot: providerjoin.Snapshot{Mode: "directory"}}, []providerjoin.Attachment{{Kind: "syft", Path: p}}, providerjoin.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if r.Ledger[0].Binding != providerjoin.BindingUnknown {
		t.Fatalf("binding=%s", r.Ledger[0].Binding)
	}
}

func TestSyftDuplicateCoordinatesRemainDistinctWithoutRelativeLocations(t *testing.T) {
	report := `{"descriptor":{"name":"syft","version":"1"},"artifacts":[` +
		`{"id":"first","name":"demo","version":"1","type":"go-module","purl":"pkg:golang/demo@1","locations":[{"path":"/absolute/one"}]},` +
		`{"id":"second","name":"demo","version":"1","type":"go-module","purl":"pkg:golang/demo@1","locations":[{"path":"/absolute/two"}]}` +
		`]}`
	r, err := providerjoin.Join(context.Background(), providerjoin.Input{Snapshot: providerjoin.Snapshot{Mode: "directory"}}, []providerjoin.Attachment{{Kind: "syft-json", Path: attachment(t, report)}}, providerjoin.Options{})
	if err != nil {
		t.Fatal(err)
	}
	ids := map[string]bool{}
	for _, node := range r.Nodes {
		if node.Kind != mapdoc.NodePackage {
			continue
		}
		if ids[node.ID] {
			t.Fatalf("duplicate package node ID %q", node.ID)
		}
		ids[node.ID] = true
	}
	if len(ids) != 2 {
		t.Fatalf("package nodes=%d; want 2", len(ids))
	}
}

func TestSARIFImportsRunMetadataButNotResults(t *testing.T) {
	p := attachment(t, `{"version":"2.1.0","runs":[{"tool":{"driver":{"name":"lint","version":"2"}},"artifacts":[{"location":{"uri":"services/api/main.go"}},{"location":{"uri":"file:///etc/passwd"}}],"invocations":[{"executionSuccessful":true}],"properties":{"dircue_snapshot_tree":"abc"},"results":[{"ruleId":"secret","message":{"text":"vulnerability"}}]}]}`)
	r, err := providerjoin.Join(context.Background(), providerjoin.Input{Snapshot: providerjoin.Snapshot{Mode: "git", Tree: "abc"}, Nodes: []mapdoc.Node{component()}}, []providerjoin.Attachment{{Kind: "sarif", Path: p}}, providerjoin.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Nodes) != 1 || len(r.Edges) != 1 || len(r.Ledger[0].CoveredFiles) != 1 {
		t.Fatalf("result=%+v", r)
	}
	if strings.Contains(strings.ToLower(r.Nodes[0].Facts[0].Value), "vulnerability") {
		t.Fatal("SARIF result imported")
	}
	encoded, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "secret") || strings.Contains(string(encoded), "vulnerability") {
		t.Fatal("SARIF finding or verdict leaked")
	}
	for _, coverage := range r.Coverage {
		if coverage.Question == "packages" && coverage.Status != mapdoc.CoverageUnknown {
			t.Fatalf("SARIF claimed package coverage: %+v", coverage)
		}
		if coverage.Question == "analyzer_coverage" && coverage.Status == mapdoc.CoverageComplete {
			t.Fatalf("artifact list claimed complete analyzer coverage: %+v", coverage)
		}
	}
}

func TestNoirEndpointsAndRouterAreInert(t *testing.T) {
	p := attachment(t, `{"version":"0.20","properties":{"dircue_snapshot_tree":"abc"},"endpoints":[{"method":"get","path":"/users","file":"services/api/main.go","line":8,"params":[{"name":"id","type":"query"}]}]}`)
	r, err := providerjoin.Join(context.Background(), providerjoin.Input{Snapshot: providerjoin.Snapshot{Mode: "git", Tree: "abc"}, Nodes: []mapdoc.Node{component()}}, []providerjoin.Attachment{{Kind: "noir-json", Path: p}}, providerjoin.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Nodes) != 2 || len(r.Edges) != 2 {
		t.Fatalf("nodes/edges=%d/%d", len(r.Nodes), len(r.Edges))
	}
	if got := r.Nodes[0].Properties["route"] + r.Nodes[1].Properties["route"]; !strings.Contains(got, "/users") {
		t.Fatalf("Noir route was not retained as an interface identity property: %+v", r.Nodes)
	}
	if len(r.Plans) != 4 || r.Plans[0].Argv == nil {
		t.Fatalf("plans=%+v", r.Plans)
	}
	for _, plan := range r.Plans {
		if plan.Tool != "syft" && len(plan.Argv) != 0 {
			t.Fatalf("unverified argv for %s: %v", plan.Tool, plan.Argv)
		}
	}
}

func TestNoirContractComparisonRequiresExactDeclaredHTTPIdentity(t *testing.T) {
	declared := mapdoc.NewNode(mapdoc.NodeInterface, []string{"openapi.yaml"}, "declared:get-users")
	declared.Properties = map[string]string{"kind": "http", "method": "GET", "route": "/users", "basis": "declared_contract"}
	declared.Coverage = mapdoc.Coverage{Status: mapdoc.CoverageComplete}
	declared.Evidence = []mapdoc.Evidence{{Basis: mapdoc.BasisDeclaredConfig, Path: "openapi.yaml", SourceKind: mapdoc.SourceConfiguration}}
	input := providerjoin.Input{Snapshot: providerjoin.Snapshot{Mode: "directory"}, Nodes: []mapdoc.Node{declared}}

	matched, err := providerjoin.Join(context.Background(), input, []providerjoin.Attachment{{Kind: "noir-json", Path: attachment(t, `[{"method":"get","path":"/users","file":"app.go"}]`)}}, providerjoin.Options{})
	if err != nil {
		t.Fatal(err)
	}
	var comparison mapdoc.Fact
	for _, node := range matched.Nodes {
		if node.Kind == mapdoc.NodeInterface && len(node.Facts) > 0 {
			comparison = node.Facts[0]
		}
	}
	if comparison.State != "exact_match" || comparison.Value != declared.ID {
		t.Fatalf("comparison=%+v", comparison)
	}

	unmatched, err := providerjoin.Join(context.Background(), input, []providerjoin.Attachment{{Kind: "noir-json", Path: attachment(t, `[{"method":"post","path":"/users","file":"app.go"}]`)}}, providerjoin.Options{})
	if err != nil {
		t.Fatal(err)
	}
	for _, edge := range unmatched.Edges {
		if edge.Type == mapdoc.EdgeConflictsWith {
			t.Fatalf("fabricated conflict without a shared contract identity: %+v", edge)
		}
	}
	for _, node := range unmatched.Nodes {
		if node.Kind == mapdoc.NodeInterface && (len(node.Facts) != 1 || node.Facts[0].State != "no_exact_match") {
			t.Fatalf("qualified unmatched comparison missing: %+v", node)
		}
	}
}

func TestBifrostImportsOnlyBoundedStructuralQueryFacts(t *testing.T) {
	report := `{"results":[` +
		`{"result_type":"structural_match","path":"services/api/main.go","language":"go","kind":"call","start_line":8,"end_line":9,"text":"password = secret","enclosing_symbol":"serve"},` +
		`{"result_type":"call_site","path":"services/api/main.go","language":"go","range":{"start_line":12,"end_line":12},"call_kind":"direct","caller":{"id":"decl:caller","fq_name":"api.serve"},"callee":{"id":"decl:callee","fq_name":"db.Open"}},` +
		`{"result_type":"taint_finding","path":"services/api/main.go","severity":"critical","message":"vulnerability"},` +
		`{"result_type":"file","path":"../../outside","language":"go"}` +
		`],"truncated":false}`
	r, err := providerjoin.Join(context.Background(), providerjoin.Input{Snapshot: providerjoin.Snapshot{Mode: "directory"}, Nodes: []mapdoc.Node{component()}}, []providerjoin.Attachment{{Kind: "bifrost-code-query-json", Path: attachment(t, report)}}, providerjoin.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Nodes) != 1 || len(r.Nodes[0].Facts) != 2 {
		t.Fatalf("nodes=%+v", r.Nodes)
	}
	if r.Ledger[0].ReportKind != "bifrost-code-query-json" || r.Ledger[0].State != "selected_query_reported" || len(r.Ledger[0].CoveredFiles) != 1 {
		t.Fatalf("ledger=%+v", r.Ledger[0])
	}
	encoded, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"password = secret", "critical", "vulnerability", "../../outside", "taint_finding"} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("provider finding, source snippet, or escaping path leaked: %q in %s", forbidden, encoded)
		}
	}
	if !strings.Contains(string(encoded), "api.serve") || !strings.Contains(string(encoded), "db.Open") {
		t.Fatalf("neutral call relation facts missing: %s", encoded)
	}
}

func TestBifrostRequiresOrdinaryEnvelopeAndQualifiesIncompleteResults(t *testing.T) {
	for _, body := range []string{`{"format":"code_query_explain_v1"}`, `{"results":[]}`, `{"result":{"results":[],"truncated":false}}`} {
		if _, err := providerjoin.Join(context.Background(), providerjoin.Input{}, []providerjoin.Attachment{{Kind: "bifrost-json", Path: attachment(t, body)}}, providerjoin.Options{}); err == nil {
			t.Fatalf("accepted non-ordinary Bifrost envelope: %s", body)
		}
	}
	report := `{"results":[{"result_type":"declaration","path":"app.py","language":"python","fq_name":"app.main","provenance_truncated":true}],"truncated":true,"diagnostics":[{"code":"budget"}]}`
	r, err := providerjoin.Join(context.Background(), providerjoin.Input{}, []providerjoin.Attachment{{Kind: "bifrost-json", Path: attachment(t, report)}}, providerjoin.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if r.Ledger[0].State != "partial_query_reported" {
		t.Fatalf("ledger=%+v", r.Ledger[0])
	}
	reasons := strings.Join(r.Nodes[0].Facts[0].Coverage.Reasons, ",")
	for _, required := range []string{"provider_results_truncated", "provider_diagnostics_reported", "provider_provenance_truncated"} {
		if !strings.Contains(reasons, required) {
			t.Fatalf("missing %q in %q", required, reasons)
		}
	}
	two := `{"results":[{"result_type":"file","path":"a.py"},{"result_type":"file","path":"b.py"}],"truncated":false}`
	if _, err := providerjoin.Join(context.Background(), providerjoin.Input{}, []providerjoin.Attachment{{Kind: "bifrost-json", Path: attachment(t, two)}}, providerjoin.Options{MaxRecords: 1}); err == nil {
		t.Fatal("accepted Bifrost report above the record bound")
	}
}

func TestRouterDoesNotRouteNonSourceDirectory(t *testing.T) {
	n := mapdoc.NewNode(mapdoc.NodeContent, []string{"photo.jpg"}, "role:media")
	n.Properties = map[string]string{"role": "media"}
	if plans := providerjoin.Route(providerjoin.Input{Nodes: []mapdoc.Node{n}}); len(plans) != 0 {
		t.Fatalf("non-source plans: %+v", plans)
	}
}

func TestRouterUsesDeclaredComponentRootNotManifest(t *testing.T) {
	n := component()
	n.Paths = []string{"services/api/go.mod", "services/api"}
	n.Properties["root"] = "services/api"
	// Recreate the canonical ID after changing identity paths.
	n.ID = mapdoc.NodeID(n.Kind, n.Paths, n.Discriminator)
	for _, plan := range providerjoin.Route(providerjoin.Input{Nodes: []mapdoc.Node{n}}) {
		if plan.Scope != "services/api" {
			t.Fatalf("manifest used as scope: %+v", plan)
		}
	}
}

func TestIgnoredSARIFResultsDoNotChangePortableIdentity(t *testing.T) {
	base := `{"version":"2.1.0","runs":[{"tool":{"driver":{"name":"lint","version":"2"}},"artifacts":[{"location":{"uri":"services/api/main.go"}}],"properties":{"dircue_snapshot_tree":"abc"},"results":[%s]}]}`
	input := providerjoin.Input{Snapshot: providerjoin.Snapshot{Mode: "git", Tree: "abc"}, Nodes: []mapdoc.Node{component()}}
	first, err := providerjoin.Join(context.Background(), input, []providerjoin.Attachment{{Kind: "sarif", Path: attachment(t, fmt.Sprintf(base, `{"ruleId":"one","message":{"text":"/home/alice"}}`))}}, providerjoin.Options{})
	if err != nil {
		t.Fatal(err)
	}
	second, err := providerjoin.Join(context.Background(), input, []providerjoin.Attachment{{Kind: "sarif", Path: attachment(t, fmt.Sprintf(base, `{"ruleId":"two","message":{"text":"/Users/bob"}}`))}}, providerjoin.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if first.Nodes[0].ID != second.Nodes[0].ID {
		t.Fatalf("ignored results changed identity: %s != %s", first.Nodes[0].ID, second.Nodes[0].ID)
	}
}

func TestDescriptorsAreVersionedAndUnique(t *testing.T) {
	seen := map[string]bool{}
	for _, d := range providerjoin.Descriptors() {
		if d.Tool == "" || d.Version == "" || d.ReportKind == "" || seen[d.Tool] {
			t.Fatalf("invalid descriptor: %+v", d)
		}
		seen[d.Tool] = true
	}
	for _, required := range []string{"syft", "noir", "opentaint", "bifrost", "bca", "scc", "semgrep", "opengrep", "codeql", "sarif"} {
		if !seen[required] {
			t.Fatalf("missing descriptor %s", required)
		}
	}
}

func TestBoundsAndTrailingJSON(t *testing.T) {
	p := attachment(t, `{} {}`)
	if _, err := providerjoin.Join(context.Background(), providerjoin.Input{}, []providerjoin.Attachment{{Kind: "syft", Path: p}}, providerjoin.Options{}); err == nil {
		t.Fatal("accepted trailing JSON")
	}
	p = attachment(t, strings.Repeat("x", 10))
	if _, err := providerjoin.Join(context.Background(), providerjoin.Input{}, []providerjoin.Attachment{{Kind: "syft", Path: p}}, providerjoin.Options{MaxReportBytes: 4}); err == nil {
		t.Fatal("accepted oversized report")
	}
}

func TestAttachmentCountIsBounded(t *testing.T) {
	items := make([]providerjoin.Attachment, 17)
	for i := range items {
		items[i] = providerjoin.Attachment{Kind: "syft-json", Path: attachment(t, `{"artifacts":[]}`)}
	}
	if _, err := providerjoin.Join(context.Background(), providerjoin.Input{}, items, providerjoin.Options{}); err == nil || !strings.Contains(err.Error(), "16-report") {
		t.Fatalf("attachment bound error=%v", err)
	}
}

func TestRepeatedEquivalentSyftAttachmentDeduplicatesPackages(t *testing.T) {
	p := attachment(t, `{"descriptor":{"name":"syft","version":"1"},"artifacts":[{"id":"a","name":"demo","version":"1","type":"go-module","locations":[{"path":"go.mod"}]}]}`)
	r, err := providerjoin.Join(context.Background(), providerjoin.Input{}, []providerjoin.Attachment{{Kind: "syft-json", Path: p}, {Kind: "syft-json", Path: p}}, providerjoin.Options{})
	if err != nil {
		t.Fatal(err)
	}
	packages := 0
	for _, node := range r.Nodes {
		if node.Kind == mapdoc.NodePackage {
			packages++
		}
	}
	if packages != 1 {
		t.Fatalf("packages=%d, nodes=%+v", packages, r.Nodes)
	}
}

func TestSyftVirtualRootPathsAreConfinedAndHostPathsRejected(t *testing.T) {
	fixture, err := os.ReadFile(filepath.Join("..", "..", "tests", "packageevidence", "fixtures", "syft-1.52.0.json"))
	if err != nil {
		t.Fatal(err)
	}
	r, err := providerjoin.Join(context.Background(), providerjoin.Input{}, []providerjoin.Attachment{{Kind: "syft-json", Path: attachment(t, string(fixture))}}, providerjoin.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Ledger) == 0 || !slices.Contains(r.Ledger[0].CoveredFiles, "dotnet/app/app.deps.json") {
		t.Fatalf("virtual-root location missing: %+v", r.Ledger)
	}
	hostile := `{"artifacts":[{"id":"a","name":"x","locations":[{"path":"/Users/alice/secret"},{"path":"/../../escape"}]}]}`
	r, err = providerjoin.Join(context.Background(), providerjoin.Input{}, []providerjoin.Attachment{{Kind: "syft-json", Path: attachment(t, hostile)}}, providerjoin.Options{})
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(r)
	if strings.Contains(string(encoded), "Users/alice") || strings.Contains(string(encoded), "escape") {
		t.Fatalf("host path leaked: %s", encoded)
	}
	legitimate := `{"source":{"type":"directory"},"artifacts":[{"id":"a","name":"x","locations":[{"path":"/home/example.go"},{"path":"/../../escape"}]}]}`
	r, err = providerjoin.Join(context.Background(), providerjoin.Input{}, []providerjoin.Attachment{{Kind: "syft-json", Path: attachment(t, legitimate)}}, providerjoin.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(r.Ledger[0].CoveredFiles, "home/example.go") || slices.Contains(r.Ledger[0].CoveredFiles, "escape") {
		t.Fatalf("directory virtual roots=%+v", r.Ledger[0])
	}
}
