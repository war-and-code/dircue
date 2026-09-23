package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"dircue/pkg/mapdoc"
	"dircue/pkg/providerjoin"
	"dircue/pkg/sariflocate"
)

func TestMapAttachmentAndRouting(t *testing.T) {
	root := t.TempDir()
	writeMapSourceFixture(t, root)
	syftPath := filepath.Join(t.TempDir(), "syft.json")
	syft := `{"descriptor":{"name":"syft","version":"1.2.3"},"artifacts":[{"id":"pkg-1","name":"example","version":"1.0.0","type":"go-module","purl":"pkg:golang/example@1.0.0","locations":[{"path":"go.mod"}]}]}`
	if err := os.WriteFile(syftPath, []byte(syft), 0600); err != nil {
		t.Fatal(err)
	}
	out, stderr, err := invoke("map", "--source", "directory", "--json", "--attach", "syft-json="+syftPath, root)
	if err != nil || stderr != "" {
		t.Fatalf("attach: %v %q", err, stderr)
	}
	var doc mapdoc.Document
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatal(err)
	}
	if err := mapdoc.Validate(doc); err != nil {
		t.Fatal(err)
	}
	var pkg, tool bool
	for _, node := range doc.Nodes {
		pkg = pkg || node.Kind == mapdoc.NodePackage && node.Name == "example"
		tool = tool || node.Kind == mapdoc.NodeToolRun && node.Properties["binding"] == string(providerjoin.BindingUnknown)
	}
	if !pkg || !tool {
		t.Fatalf("attached nodes missing: %s", out)
	}
	if len(doc.CoverageLedger) != 1 || doc.CoverageLedger[0].Tool != "syft" || doc.CoverageLedger[0].Binding != "unknown" || !doc.CoverageLedger[0].Ran {
		t.Fatalf("attached provider run missing from map coverage ledger: %+v", doc.CoverageLedger)
	}
	mapPath := filepath.Join(t.TempDir(), "map.json")
	if err := os.WriteFile(mapPath, []byte(out), 0600); err != nil {
		t.Fatal(err)
	}
	routes, stderr, err := invoke("map", "route", "--json", mapPath)
	if err != nil || stderr != "" {
		t.Fatalf("route: %v %q", err, stderr)
	}
	var plans []providerjoin.Plan
	if err := json.Unmarshal([]byte(routes), &plans); err != nil || len(plans) == 0 {
		t.Fatalf("plans: %v %s", err, routes)
	}
	for _, plan := range plans {
		if plan.Tool == "syft" {
			if !strings.Contains(strings.Join(plan.Argv, " "), "{") {
				t.Fatalf("verified route is not an inert placeholder plan: %+v", plan)
			}
			continue
		}
		if len(plan.Argv) != 0 || len(plan.Prerequisites) == 0 || plan.Prerequisites[0].Name != "exact_invocation" || plan.Prerequisites[0].Observed || plan.ReportKind == "" || plan.Reason == "" {
			t.Fatalf("unverified route exposed argv without an explicit prerequisite: %+v", plan)
		}
	}
}

func TestMapLocateAnnotatesSARIFAndSummarizes(t *testing.T) {
	root := t.TempDir()
	writeMapSourceFixture(t, root)
	mapJSON, _, err := invoke("map", "--source", "directory", "--json", root)
	if err != nil {
		t.Fatal(err)
	}
	mapPath := filepath.Join(t.TempDir(), "map.json")
	if err := os.WriteFile(mapPath, []byte(mapJSON), 0600); err != nil {
		t.Fatal(err)
	}
	sarifPath := filepath.Join(t.TempDir(), "results.sarif")
	sarif := `{"version":"2.1.0","runs":[{"tool":{"driver":{"name":"demo","version":"1"}},"results":[{"locations":[{"physicalLocation":{"artifactLocation":{"uri":"main.go"},"region":{"startLine":1}}}]}]}]}`
	if err := os.WriteFile(sarifPath, []byte(sarif), 0600); err != nil {
		t.Fatal(err)
	}
	located, stderr, err := invoke("map", "locate", mapPath, sarifPath)
	if err != nil || stderr != "" || !json.Valid([]byte(located)) || !strings.Contains(located, sariflocate.PropertyName) {
		t.Fatalf("locate: err=%v stderr=%q output=%s", err, stderr, located)
	}
	summary, _, err := invoke("map", "locate", "--summary", mapPath, sarifPath)
	if err != nil || !strings.Contains(summary, `"resolutions"`) || strings.Contains(summary, `"runs":[{"tool"`) {
		t.Fatalf("summary: err=%v output=%s", err, summary)
	}
}

func TestMapProviderInputErrorsAreActionable(t *testing.T) {
	root := t.TempDir()
	writeMapSourceFixture(t, root)
	if _, _, err := invoke("map", "--attach", "syft.json", root); err == nil || !strings.Contains(err.Error(), "KIND=PATH") {
		t.Fatalf("malformed attachment error = %v", err)
	}
	if _, _, err := invoke("map", "route", "--workers", "2", "missing.json"); err == nil || !strings.Contains(err.Error(), "does not apply") {
		t.Fatalf("saved-map flag error = %v", err)
	}
}

func writeMapSourceFixture(t *testing.T, root string) {
	t.Helper()
	for name, content := range map[string]string{
		"go.mod":  "module example.test/map\n\ngo 1.22\n",
		"main.go": "package main\nfunc main() {}\n",
	} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
}
