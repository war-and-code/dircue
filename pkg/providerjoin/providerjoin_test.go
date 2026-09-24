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

	"github.com/war-and-code/dircue/pkg/mapdoc"
	"github.com/war-and-code/dircue/pkg/providerjoin"
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

func TestSyftFactsRemainQualifiedAndFindingsAreNotPossible(t *testing.T) {
	report := `{"descriptor":{"name":"syft","version":"1.20"},"artifacts":[{"id":"a","name":"app","version":"1","type":"go-module","purl":"pkg:golang/app@1","locations":[{"path":"services/api/go.mod"},{"path":"/Users/alice/secret"}]},{"id":"b","name":"dep","version":"2","type":"go-module","purl":"pkg:golang/dep@2"}],"artifactRelationships":[{"parent":"a","child":"b","type":"dependency-of"}]}`
	r, err := providerjoin.Join(context.Background(), providerjoin.Input{Snapshot: providerjoin.Snapshot{Mode: "git", Tree: "abc"}, Nodes: []mapdoc.Node{component()}}, []providerjoin.Attachment{{Kind: "syft-json", Path: attachment(t, report)}}, providerjoin.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Nodes) != 3 || len(r.Edges) != 4 {
		t.Fatalf("nodes/edges = %d/%d", len(r.Nodes), len(r.Edges))
	}
	if r.Ledger[0].Binding != providerjoin.BindingUnknown {
		t.Fatalf("binding=%s", r.Ledger[0].Binding)
	}
	for _, coverage := range r.Coverage {
		if coverage.Question == "packages" {
			if coverage.Status != mapdoc.CoveragePartial || !slices.Contains(coverage.Reasons, "syft_cataloger_scope_not_proven_exhaustive") || slices.Contains(coverage.Reasons, "no_syft_report_attached") {
				t.Fatalf("verified Syft report overclaimed package-universe coverage: %+v", coverage)
			}
		}
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

func TestSyftWithoutStandardSourceIdentityStaysUnknown(t *testing.T) {
	// Legacy dircue-specific metadata must not impersonate a source identity.
	p := attachment(t, `{"descriptor":{"name":"syft","version":"1"},"source":{"metadata":{"dircue_snapshot_tree":"selected"}},"artifacts":[]}`)
	r, err := providerjoin.Join(context.Background(), providerjoin.Input{Snapshot: providerjoin.Snapshot{Mode: "git", Tree: "selected"}}, []providerjoin.Attachment{{Kind: "syft", Path: p}}, providerjoin.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if r.Ledger[0].Binding != providerjoin.BindingUnknown {
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

// TestSARIFMultiRepoVCPBinding verifies that SARIF versionControlProvenance
// binding respects same-repository semantics:
//   - verified: a focal repo entry matches AND no same-repo entry differs
//   - mismatch: no entry for the focal repo matches (all differ)
//   - unknown: a same-repo conflict exists (one matches, another differs)
//
// Entries for other repositories are ignored.
func TestSARIFMultiRepoVCPBinding(t *testing.T) {
	t.Run("verified_different_repos_focal_matches", func(t *testing.T) {
		// repo-A matches cafebabe; repo-B has a different revision but is a separate repo → verified.
		body := `{"version":"2.1.0","runs":[{"tool":{"driver":{"name":"lint","version":"1"}},` +
			`"versionControlProvenance":[` +
			`{"repositoryUri":"https://github.com/example/app","revisionId":"cafebabe"},` +
			`{"repositoryUri":"https://github.com/example/other","revisionId":"deadbeef"}` +
			`],"artifacts":[{"location":{"uri":"main.go"}}]}]}`
		r, err := providerjoin.Join(context.Background(),
			providerjoin.Input{Snapshot: providerjoin.Snapshot{Mode: "git", Commit: "cafebabe"}},
			[]providerjoin.Attachment{{Kind: "sarif", Path: attachment(t, body)}},
			providerjoin.Options{})
		if err != nil {
			t.Fatal(err)
		}
		if len(r.Ledger) < 1 || r.Ledger[0].Binding != providerjoin.BindingVerified {
			t.Fatalf("expected BindingVerified (different-repo entry ignored), got ledger=%+v", r.Ledger)
		}
	})
	t.Run("mismatch_all_focal_repo_entries_differ", func(t *testing.T) {
		// All entries for the focal repo (empty URI) differ from the snapshot commit → mismatch.
		body := `{"version":"2.1.0","runs":[{"tool":{"driver":{"name":"lint","version":"1"}},` +
			`"versionControlProvenance":[{"revisionId":"deadbeef"},{"revisionId":"beefdead"}],` +
			`"artifacts":[{"location":{"uri":"package.json"}}]}]}`
		r, err := providerjoin.Join(context.Background(),
			providerjoin.Input{Snapshot: providerjoin.Snapshot{Mode: "git", Commit: "cafebabe"}},
			[]providerjoin.Attachment{{Kind: "sarif", Path: attachment(t, body)}},
			providerjoin.Options{})
		if err != nil {
			t.Fatal(err)
		}
		if len(r.Ledger) < 1 || r.Ledger[0].Binding != providerjoin.BindingMismatch {
			t.Fatalf("expected BindingMismatch, got ledger=%+v", r.Ledger)
		}
		for _, n := range r.Nodes {
			if n.Coverage.Status == mapdoc.CoverageComplete {
				t.Fatalf("mismatched node has complete coverage: %+v", n)
			}
		}
	})
	t.Run("unknown_same_repo_revision_conflict", func(t *testing.T) {
		// Same repo (empty URI) has one entry matching the commit and one differing → unknown (conflict).
		body := `{"version":"2.1.0","runs":[{"tool":{"driver":{"name":"lint","version":"1"}},` +
			`"versionControlProvenance":[{"revisionId":"cafebabe"},{"revisionId":"deadbeef"}],` +
			`"artifacts":[{"location":{"uri":"main.go"}}]}]}`
		r, err := providerjoin.Join(context.Background(),
			providerjoin.Input{Snapshot: providerjoin.Snapshot{Mode: "git", Commit: "cafebabe"}},
			[]providerjoin.Attachment{{Kind: "sarif", Path: attachment(t, body)}},
			providerjoin.Options{})
		if err != nil {
			t.Fatal(err)
		}
		if len(r.Ledger) < 1 || r.Ledger[0].Binding != providerjoin.BindingUnknown {
			t.Fatalf("expected BindingUnknown (same-repo conflict), got ledger=%+v", r.Ledger)
		}
	})
	t.Run("verified_named_repo_all_entries_match", func(t *testing.T) {
		// Named repo, all entries match the snapshot commit → verified.
		body := `{"version":"2.1.0","runs":[{"tool":{"driver":{"name":"lint","version":"1"}},` +
			`"versionControlProvenance":[` +
			`{"repositoryUri":"https://github.com/example/app","revisionId":"cafebabe"},` +
			`{"repositoryUri":"https://github.com/example/app","revisionId":"cafebabe"}` +
			`],"artifacts":[{"location":{"uri":"main.go"}}]}]}`
		r, err := providerjoin.Join(context.Background(),
			providerjoin.Input{Snapshot: providerjoin.Snapshot{Mode: "git", Commit: "cafebabe"}},
			[]providerjoin.Attachment{{Kind: "sarif", Path: attachment(t, body)}},
			providerjoin.Options{})
		if err != nil {
			t.Fatal(err)
		}
		if len(r.Ledger) < 1 || r.Ledger[0].Binding != providerjoin.BindingVerified {
			t.Fatalf("expected BindingVerified (named repo, all match), got ledger=%+v", r.Ledger)
		}
	})
}

func TestSARIFStandardRevisionProvenanceAndEncodedPath(t *testing.T) {
	commit := strings.Repeat("a", 40)
	body := fmt.Sprintf(`{"version":"2.1.0","runs":[{"tool":{"driver":{"name":"lint"}},"versionControlProvenance":[{"repositoryUri":"https://example.test/repo","revisionId":%q}],"artifacts":[{"location":{"uri":"services%%2Fapi%%2Fmain.go"}},{"location":{"uri":"%%2e%%2e%%2f%%2e%%2e%%2fprivate.txt"}}]}]}`, commit)
	r, err := providerjoin.Join(context.Background(), providerjoin.Input{Snapshot: providerjoin.Snapshot{Mode: "git", Commit: commit}, Nodes: []mapdoc.Node{component()}}, []providerjoin.Attachment{{Kind: "sarif", Path: attachment(t, body)}}, providerjoin.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if r.Ledger[0].Binding != providerjoin.BindingVerified || len(r.Ledger[0].CoveredFiles) != 1 || r.Ledger[0].CoveredFiles[0] != "services/api/main.go" {
		t.Fatalf("ledger = %+v", r.Ledger[0])
	}
}

func TestCallerAssertedBindingDoesNotUpgradeCoverage(t *testing.T) {
	body := `{"descriptor":{"name":"syft"},"artifacts":[{"id":"a","name":"demo","type":"npm"}]}`
	digest := &mapdoc.Digest{Algorithm: "git-sha1", Scope: "all_regular_files", Value: strings.Repeat("a", 40)}
	r, err := providerjoin.Join(context.Background(), providerjoin.Input{Snapshot: providerjoin.Snapshot{Mode: "directory", Digest: digest, DigestComplete: true, CallerAsserted: true}}, []providerjoin.Attachment{{Kind: "syft-json", Path: attachment(t, body)}}, providerjoin.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if r.Ledger[0].Binding != providerjoin.BindingCallerAsserted {
		t.Fatalf("binding = %s", r.Ledger[0].Binding)
	}
	for _, n := range r.Nodes {
		if n.Coverage.Status == mapdoc.CoverageComplete {
			t.Fatalf("assertion upgraded node coverage: %+v", n)
		}
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
	p := attachment(t, `{"version":"2.1.0","runs":[{"tool":{"driver":{"name":"lint","version":"2"}},"artifacts":[{"location":{"uri":"services/api/main.go"}},{"location":{"uri":"file:///etc/passwd"}}],"invocations":[{"executionSuccessful":true}],"results":[{"ruleId":"secret","message":{"text":"vulnerability"}}]}]}`)
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
	p := attachment(t, `{"version":"0.20","endpoints":[{"method":"get","path":"/users","file":"services/api/main.go","line":8,"params":[{"name":"id","type":"query"}]}]}`)
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

func TestNoirV121ImportsHTTPCodePathsAndDoesNotFabricateCLIHTTPRoutes(t *testing.T) {
	report, err := os.ReadFile("testdata/noir-1.2.1-flask-relative.json")
	if err != nil {
		t.Fatal(err)
	}
	r, err := providerjoin.Join(context.Background(), providerjoin.Input{Snapshot: providerjoin.Snapshot{Mode: "directory"}}, []providerjoin.Attachment{{Kind: "noir-json", Path: attachment(t, string(report))}}, providerjoin.Options{})
	if err != nil {
		t.Fatal(err)
	}
	interfaces := 0
	multiPath := false
	for _, node := range r.Nodes {
		if node.Kind != mapdoc.NodeInterface {
			continue
		}
		interfaces++
		if node.Properties["kind"] != "http" || strings.HasPrefix(node.Properties["route"], "cli://") {
			t.Fatalf("non-HTTP endpoint fabricated as HTTP: %+v", node)
		}
		if len(node.Paths) > 1 {
			multiPath = true
		}
	}
	if interfaces != 23 {
		t.Fatalf("HTTP interfaces=%d, want 23 from pinned Noir v1.2.1 fixture", interfaces)
	}
	if !multiPath {
		t.Fatal("Noir code_paths were not retained on multi-location endpoint")
	}
	if len(r.Ledger) != 1 || len(r.Ledger[0].CoveredFiles) == 0 {
		t.Fatalf("coverage ledger=%+v", r.Ledger)
	}
}

func TestNoirSanitizesAbsoluteURLsAndQualifiesUnconfinedLocations(t *testing.T) {
	body := `{"endpoints":[{"url":"https://user:secret@example.test/private?q=token#fragment","method":"GET","protocol":"http","details":{"code_paths":[{"path":"/Users/alice/private.go","line":2}]}}]}`
	r, err := providerjoin.Join(context.Background(), providerjoin.Input{Snapshot: providerjoin.Snapshot{Mode: "directory"}}, []providerjoin.Attachment{{Kind: "noir-json", Path: attachment(t, body)}}, providerjoin.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Nodes) != 2 {
		t.Fatalf("nodes=%+v", r.Nodes)
	}
	var endpoint mapdoc.Node
	for _, node := range r.Nodes {
		if node.Kind == mapdoc.NodeInterface {
			endpoint = node
		}
	}
	if endpoint.Properties["route"] != "/private" || endpoint.Coverage.Status != mapdoc.CoveragePartial {
		t.Fatalf("endpoint=%+v", endpoint)
	}
	encoded, _ := json.Marshal(r)
	for _, forbidden := range []string{"secret", "example.test", "token", "/Users/"} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("absolute URL or host path leaked %q: %s", forbidden, encoded)
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
	// Record-limit: 2 results but MaxRecords=1 → first result (a.py) kept, second dropped.
	two := `{"results":[{"result_type":"file","path":"a.py"},{"result_type":"file","path":"b.py"}],"truncated":false}`
	r2, err2 := providerjoin.Join(context.Background(), providerjoin.Input{}, []providerjoin.Attachment{{Kind: "bifrost-json", Path: attachment(t, two)}}, providerjoin.Options{MaxRecords: 1})
	if err2 != nil {
		t.Fatalf("record limit must not cause error: %v", err2)
	}
	if len(r2.Ledger) < 1 || r2.Ledger[0].Reason != "attachment_record_limit_reached" {
		t.Fatalf("expected attachment_record_limit_reached ledger entry, got %+v", r2.Ledger)
	}
	// Exactly one covered file must be retained (a.py, the first result in document order).
	if len(r2.Ledger[0].CoveredFiles) != 1 || r2.Ledger[0].CoveredFiles[0] != "a.py" {
		t.Fatalf("expected CoveredFiles=[a.py], got %v", r2.Ledger[0].CoveredFiles)
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
	base := `{"version":"2.1.0","runs":[{"tool":{"driver":{"name":"lint","version":"2"}},"artifacts":[{"location":{"uri":"services/api/main.go"}}],"results":[%s]}]}`
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

func TestSyftWithoutStandardIdentityDoesNotClaimRequirementAbsence(t *testing.T) {
	type declaration struct{ root, ecosystem, kind, value string }
	declarations := []declaration{
		{"npm", "npm", "npm-dependency", "lodash@^4"},
		{"go", "go", "go-require", "github.com/acme/lib v1.2.0"},
		{"cargo", "cargo", "cargo-dependency", "serde 1"},
		{"python", "python-uv", "python-dependency", "Requests>=2"},
		{"dotnet", "dotnet", "package-reference", "Example.Package@1.2.3"},
		{"maven", "maven", "maven-dependency", "org.example:lib:1.0"},
		{"gradle", "gradle", "gradle-dependency", "org.gradle:thing:2.0"},
		{"npm", "npm", "npm-dependency", "missing@1"},
	}
	var nodes []mapdoc.Node
	byRoot := map[string]int{}
	for _, d := range declarations {
		fact := mapdoc.Fact{Kind: "declared_requirement", Name: d.kind, Value: d.value, State: "declared", Coverage: mapdoc.Coverage{Status: mapdoc.CoverageComplete}}
		if index, ok := byRoot[d.root]; ok {
			fact.Evidence = nodes[index].Evidence
			nodes[index].Facts = append(nodes[index].Facts, fact)
			continue
		}
		n := mapdoc.NewNode(mapdoc.NodeComponent, []string{d.root}, d.ecosystem)
		n.Properties = map[string]string{"root": d.root, "ecosystem": d.ecosystem}
		n.Coverage = mapdoc.Coverage{Status: mapdoc.CoverageComplete}
		n.Evidence = []mapdoc.Evidence{{Basis: mapdoc.BasisDeclaredConfig, Path: d.root + "/manifest", SourceKind: mapdoc.SourceConfiguration, Rule: &mapdoc.Producer{ID: "test", Version: "1"}}}
		fact.Evidence = n.Evidence
		n.Facts = []mapdoc.Fact{fact}
		byRoot[d.root] = len(nodes)
		nodes = append(nodes, n)
	}
	report := `{"descriptor":{"name":"syft","version":"1"},"source":{"type":"directory"},"artifacts":[` +
		`{"id":"npm","name":"lodash","purl":"pkg:npm/lodash@4.17.21","locations":[{"path":"/npm/package-lock.json"}]},` +
		`{"id":"go","name":"github.com/acme/lib","purl":"pkg:golang/github.com/acme/lib@v1.2.0","locations":[{"path":"/go/go.mod"}]},` +
		`{"id":"cargo","name":"serde","purl":"pkg:cargo/serde@1.0","locations":[{"path":"/cargo/Cargo.lock"}]},` +
		`{"id":"python","name":"requests","purl":"pkg:pypi/requests@2.0","locations":[{"path":"/python/uv.lock"}]},` +
		`{"id":"dotnet","name":"Example.Package","purl":"pkg:nuget/Example.Package@1.2.3","locations":[{"path":"/dotnet/obj/project.assets.json"}]},` +
		`{"id":"maven","name":"lib","purl":"pkg:maven/org.example/lib@1.0","locations":[{"path":"/maven/pom.xml"}]},` +
		`{"id":"gradle","name":"thing","purl":"pkg:maven/org.gradle/thing@2.0","locations":[{"path":"/gradle/build.gradle"}]},` +
		`{"id":"extra","name":"transitive","purl":"pkg:npm/transitive@1","locations":[{"path":"/npm/package-lock.json"}]}` +
		`]}`
	r, err := providerjoin.Join(context.Background(), providerjoin.Input{Snapshot: providerjoin.Snapshot{Mode: "git", Tree: "abc"}, Nodes: nodes}, []providerjoin.Attachment{{Kind: "syft-json", Path: attachment(t, report)}}, providerjoin.Options{})
	if err != nil {
		t.Fatal(err)
	}
	candidates := 0
	for _, node := range r.Nodes {
		for _, fact := range node.Facts {
			if fact.State == "no_match_in_supplied_report" || fact.State == "no_matching_retained_declaration" {
				t.Fatalf("unbound Syft report made an absence claim: %+v", fact)
			}
			if strings.Contains(fact.State, "unverified_binding") {
				candidates++
			}
		}
	}
	if candidates == 0 {
		t.Fatalf("unbound Syft report did not retain candidate observations: %+v", r.Nodes)
	}
}

func TestSyftRequirementComparisonRequiresVerifiedBindingAndUnambiguousIdentity(t *testing.T) {
	c := component()
	c.Facts = []mapdoc.Fact{{Kind: "declared_requirement", Name: "go-require", Value: "example.test/dep v1", State: "declared", Coverage: mapdoc.Coverage{Status: mapdoc.CoverageComplete}, Evidence: c.Evidence}}
	base := `{"descriptor":{"name":"syft"},"source":{"type":"directory"%s},"artifacts":[{"id":"a","name":"dep","purl":"pkg:golang/example.test/dep@v1","locations":[{"path":"/services/api/go.mod"}]},{"id":"b","name":"dep","purl":"pkg:golang/example.test/dep@v1","locations":[{"path":"/services/api/go.sum"}]}]}`
	unbound, err := providerjoin.Join(context.Background(), providerjoin.Input{Snapshot: providerjoin.Snapshot{Mode: "git", Tree: "abc"}, Nodes: []mapdoc.Node{c}}, []providerjoin.Attachment{{Kind: "syft-json", Path: attachment(t, fmt.Sprintf(base, ""))}}, providerjoin.Options{})
	if err != nil {
		t.Fatal(err)
	}
	candidate := false
	for _, node := range unbound.Nodes {
		for _, fact := range node.Facts {
			candidate = candidate || fact.State == "ambiguous_reported_identity"
			if fact.State == "no_match_in_supplied_report" || fact.State == "no_matching_retained_declaration" {
				t.Fatalf("unbound report produced absence comparison: %+v", fact)
			}
		}
	}
	if !candidate {
		t.Fatalf("unbound candidate identity missing: %+v", unbound.Nodes)
	}
	digest := &mapdoc.Digest{Algorithm: "git-sha1", Scope: "all_regular_files", Value: strings.Repeat("a", 40)}
	asserted, err := providerjoin.Join(context.Background(), providerjoin.Input{Snapshot: providerjoin.Snapshot{Mode: "directory", Digest: digest, DigestComplete: true, CallerAsserted: true}, Nodes: []mapdoc.Node{c}}, []providerjoin.Attachment{{Kind: "syft-json", Path: attachment(t, fmt.Sprintf(base, ""))}}, providerjoin.Options{})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, node := range asserted.Nodes {
		for _, fact := range node.Facts {
			found = found || fact.State == "candidate_identity_unverified_binding" || fact.State == "ambiguous_reported_identity"
			if fact.State == "no_match_in_supplied_report" || fact.State == "no_matching_retained_declaration" {
				t.Fatalf("caller assertion enabled absence comparison: %+v", fact)
			}
		}
	}
	if !found {
		t.Fatalf("caller-asserted candidate missing: %+v", asserted.Nodes)
	}
}

func TestSyftRequirementComparisonUsesPinnedV152Fixture(t *testing.T) {
	data, err := os.ReadFile("../../tests/packageevidence/fixtures/syft-1.52.0.json")
	if err != nil {
		t.Fatal(err)
	}
	component := mapdoc.NewNode(mapdoc.NodeComponent, []string{"java"}, "maven")
	component.Properties = map[string]string{"root": "java", "ecosystem": "maven"}
	component.Coverage = mapdoc.Coverage{Status: mapdoc.CoverageComplete}
	component.Evidence = []mapdoc.Evidence{{Basis: mapdoc.BasisDeclaredConfig, Path: "java/pom.xml", SourceKind: mapdoc.SourceConfiguration}}
	component.Facts = []mapdoc.Fact{{Kind: "declared_requirement", Name: "maven-dependency", Value: "example:app:1.0.0", State: "declared", Coverage: mapdoc.Coverage{Status: mapdoc.CoverageComplete}, Evidence: component.Evidence}}
	r, err := providerjoin.Join(context.Background(), providerjoin.Input{Snapshot: providerjoin.Snapshot{Mode: "git", Tree: "abc"}, Nodes: []mapdoc.Node{component}}, []providerjoin.Attachment{{Kind: "syft-json", Path: attachment(t, string(data))}}, providerjoin.Options{})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, node := range r.Nodes {
		for _, fact := range node.Facts {
			found = found || fact.Kind == "declared_requirement_match" && fact.Value == "maven:example:app" && fact.State == "candidate_identity_unverified_binding" && slices.Contains(fact.Coverage.Reasons, "provider_snapshot_binding_unknown")
		}
	}
	if !found {
		t.Fatalf("pinned Syft v1.52.0 package did not reconcile: %+v", r.Nodes)
	}
}

// attachmentWithBOM writes a file with a leading UTF-8 byte-order mark.
func attachmentWithBOM(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "report-bom.json")
	bom := []byte{0xEF, 0xBB, 0xBF}
	if err := os.WriteFile(p, append(bom, []byte(body)...), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// TestBOMToleranceSyft verifies that a syft-json file with a leading UTF-8
// BOM is decoded successfully. Windows-native tools commonly emit BOMs.
func TestBOMToleranceSyft(t *testing.T) {
	body := `{"descriptor":{"name":"syft","version":"1.0"},"artifacts":[]}`
	r, err := providerjoin.Join(context.Background(),
		providerjoin.Input{Snapshot: providerjoin.Snapshot{Mode: "directory"}},
		[]providerjoin.Attachment{{Kind: "syft-json", Path: attachmentWithBOM(t, body)}},
		providerjoin.Options{})
	if err != nil {
		t.Fatalf("BOM-prefixed syft attachment failed: %v", err)
	}
	if len(r.Ledger) < 1 {
		t.Fatal("expected a ledger entry")
	}
}

// TestBOMToleranceSARIF verifies that a SARIF file with a leading UTF-8 BOM
// is decoded successfully.
func TestBOMToleranceSARIF(t *testing.T) {
	body := `{"version":"2.1.0","runs":[{"tool":{"driver":{"name":"scanner"}},"artifacts":[]}]}`
	r, err := providerjoin.Join(context.Background(),
		providerjoin.Input{Snapshot: providerjoin.Snapshot{Mode: "directory"}},
		[]providerjoin.Attachment{{Kind: "sarif", Path: attachmentWithBOM(t, body)}},
		providerjoin.Options{})
	if err != nil {
		t.Fatalf("BOM-prefixed SARIF attachment failed: %v", err)
	}
	if len(r.Ledger) < 1 {
		t.Fatal("expected a ledger entry")
	}
}

// TestAttachmentRecordLimitDegrades verifies that a syft attachment exceeding the
// record limit ingests the first N records in document order, marks coverage
// partial with reason attachment_record_limit_reached, and does not exit non-zero.
func TestAttachmentRecordLimitDegrades(t *testing.T) {
	// Build a syft document with 3 artifacts but MaxRecords=2.
	body := `{"descriptor":{"name":"syft","version":"1.0"},"artifacts":[` +
		`{"id":"a","name":"pkg-a","version":"1","type":"go-module","purl":"pkg:golang/a@1"},` +
		`{"id":"b","name":"pkg-b","version":"1","type":"go-module","purl":"pkg:golang/b@1"},` +
		`{"id":"c","name":"pkg-c","version":"1","type":"go-module","purl":"pkg:golang/c@1"}` +
		`]}`
	r, err := providerjoin.Join(context.Background(),
		providerjoin.Input{Snapshot: providerjoin.Snapshot{Mode: "directory"}},
		[]providerjoin.Attachment{{Kind: "syft-json", Path: attachment(t, body)}},
		providerjoin.Options{MaxRecords: 2})
	if err != nil {
		t.Fatalf("record limit should not cause error, got: %v", err)
	}
	if len(r.Ledger) < 1 {
		t.Fatal("expected a ledger entry even on limit")
	}
	entry := r.Ledger[0]
	if entry.Reason != "attachment_record_limit_reached" {
		t.Fatalf("expected reason=attachment_record_limit_reached, got reason=%q state=%q", entry.Reason, entry.State)
	}
	// First 2 of 3 package nodes should be emitted (document order: a, b).
	pkgNames := []string{}
	for _, n := range r.Nodes {
		if n.Kind == "package" {
			pkgNames = append(pkgNames, n.Name)
		}
	}
	if len(pkgNames) != 2 {
		t.Fatalf("expected exactly 2 package nodes (first 2 of 3), got %d: %v", len(pkgNames), pkgNames)
	}
	// Coverage for packages and analyzer_coverage must be partial.
	var pkgCov, analyzerCov *mapdoc.Coverage
	for _, qc := range r.Coverage {
		qc := qc
		switch qc.Question {
		case "packages":
			pkgCov = &qc.Coverage
		case "analyzer_coverage":
			analyzerCov = &qc.Coverage
		}
	}
	if pkgCov == nil || pkgCov.Status != mapdoc.CoveragePartial {
		t.Fatalf("packages question must be partial when record limit reached, got %+v", pkgCov)
	}
	if analyzerCov == nil || analyzerCov.Status != mapdoc.CoveragePartial {
		t.Fatalf("analyzer_coverage question must be partial when record limit reached, got %+v", analyzerCov)
	}
	if !strings.Contains(strings.Join(pkgCov.Reasons, ","), "attachment_record_limit_reached") {
		t.Fatalf("packages partial reasons must include attachment_record_limit_reached, got %v", pkgCov.Reasons)
	}
}

// TestAttachmentRecordLimitSARIF verifies that a SARIF attachment exceeding the
// run-level record limit ingests the first N artifacts in document order and marks
// coverage partial with reason attachment_record_limit_reached.
func TestAttachmentRecordLimitSARIF(t *testing.T) {
	// 1 run with 2 artifacts but MaxRecords=1: first artifact kept, second dropped.
	body := `{"version":"2.1.0","runs":[{"tool":{"driver":{"name":"scanner"}},"artifacts":[` +
		`{"location":{"uri":"a.go"}},{"location":{"uri":"b.go"}}` +
		`]}]}`
	r, err := providerjoin.Join(context.Background(),
		providerjoin.Input{Snapshot: providerjoin.Snapshot{Mode: "directory"}},
		[]providerjoin.Attachment{{Kind: "sarif", Path: attachment(t, body)}},
		providerjoin.Options{MaxRecords: 1})
	if err != nil {
		t.Fatalf("record limit should not cause error, got: %v", err)
	}
	if len(r.Ledger) < 1 {
		t.Fatal("expected a ledger entry even on limit")
	}
	entry := r.Ledger[0]
	if entry.Reason != "attachment_record_limit_reached" {
		t.Fatalf("expected reason=attachment_record_limit_reached, got reason=%q state=%q", entry.Reason, entry.State)
	}
	// Exactly one covered file (a.go, the first artifact) must be retained.
	if len(entry.CoveredFiles) != 1 || entry.CoveredFiles[0] != "a.go" {
		t.Fatalf("expected CoveredFiles=[a.go], got %v", entry.CoveredFiles)
	}
	// analyzer_coverage must be partial with the limit reason.
	var analyzerCov *mapdoc.Coverage
	for _, qc := range r.Coverage {
		qc := qc
		if qc.Question == "analyzer_coverage" {
			analyzerCov = &qc.Coverage
		}
	}
	if analyzerCov == nil || analyzerCov.Status != mapdoc.CoveragePartial {
		t.Fatalf("analyzer_coverage must be partial when record limit reached, got %+v", analyzerCov)
	}
}
