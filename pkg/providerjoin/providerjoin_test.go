package providerjoin_test

import (
	"context"
	"os"
	"path/filepath"
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
	if len(r.Nodes) != 3 || len(r.Edges) != 3 {
		t.Fatalf("nodes/edges = %d/%d", len(r.Nodes), len(r.Edges))
	}
	if r.Ledger[0].Binding != providerjoin.BindingVerified {
		t.Fatalf("binding=%s", r.Ledger[0].Binding)
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
	if len(r.Plans) != 4 || r.Plans[0].Argv == nil {
		t.Fatalf("plans=%+v", r.Plans)
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
