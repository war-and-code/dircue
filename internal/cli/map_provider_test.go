package cli

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/war-and-code/dircue/pkg/mapdoc"
	"github.com/war-and-code/dircue/pkg/providerjoin"
	"github.com/war-and-code/dircue/pkg/sariflocate"
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

func TestProviderInputCarriesOnlyCompleteDigestCoverage(t *testing.T) {
	doc := mapdoc.Document{
		Source:   mapdoc.Source{Mode: "directory", Digest: &mapdoc.Digest{Algorithm: "git-sha1", Scope: "all_regular_files", Value: strings.Repeat("a", 40)}},
		Coverage: []mapdoc.QuestionCoverage{{Question: mapdoc.QuestionSourceBinding, Scope: ".", Coverage: mapdoc.Coverage{Status: mapdoc.CoveragePartial}}},
	}
	if got := providerInput(doc).Snapshot.DigestComplete; got {
		t.Fatal("partial source digest was marked complete")
	}
	doc.Coverage[0].Status = mapdoc.CoverageComplete
	if got := providerInput(doc).Snapshot.DigestComplete; !got {
		t.Fatal("complete source digest was not marked complete")
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

func TestMapAcceptsLeadingSlashNoirRouteAndBifrostAttachment(t *testing.T) {
	root := t.TempDir()
	writeMapSourceFixture(t, root)
	noir := filepath.Join(t.TempDir(), "noir.json")
	if err := os.WriteFile(noir, []byte(`[{"method":"GET","path":"/users","file":"main.go"}]`), 0600); err != nil {
		t.Fatal(err)
	}
	bifrost := filepath.Join(t.TempDir(), "bifrost.json")
	if err := os.WriteFile(bifrost, []byte(`{"results":[{"result_type":"file","path":"main.go","language":"go"}],"truncated":false}`), 0600); err != nil {
		t.Fatal(err)
	}
	out, stderr, err := invoke("map", "--source", "directory", "--json", "--attach", "noir-json="+noir, "--attach", "bifrost-code-query-json="+bifrost, root)
	if err != nil || stderr != "" {
		t.Fatalf("attach err=%v stderr=%q", err, stderr)
	}
	var doc mapdoc.Document
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatal(err)
	}
	if err := mapdoc.Validate(doc); err != nil {
		t.Fatal(err)
	}
	foundRoute, foundBifrost := false, false
	for _, node := range doc.Nodes {
		foundRoute = foundRoute || node.Properties["route"] == "/users"
		foundBifrost = foundBifrost || node.Kind == mapdoc.NodeToolRun && node.Name == "bifrost"
	}
	if !foundRoute || !foundBifrost {
		t.Fatalf("attachments absent: route=%t bifrost=%t", foundRoute, foundBifrost)
	}
}

func TestMapImportsPinnedNoirV121HTTPInterfaces(t *testing.T) {
	root := t.TempDir()
	writeMapSourceFixture(t, root)
	fixture, err := filepath.Abs(filepath.Join("..", "..", "pkg", "providerjoin", "testdata", "noir-1.2.1-flask-relative.json"))
	if err != nil {
		t.Fatal(err)
	}
	out, stderr, err := invoke("map", "--source", "directory", "--json", "--attach", "noir-json="+fixture, root)
	if err != nil || stderr != "" {
		t.Fatalf("attach err=%v stderr=%q", err, stderr)
	}
	var doc mapdoc.Document
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatal(err)
	}
	httpInterfaces := 0
	for _, node := range doc.Nodes {
		if node.Kind == mapdoc.NodeInterface && node.Properties["kind"] == "http" {
			httpInterfaces++
			if strings.HasPrefix(node.Properties["route"], "cli://") {
				t.Fatalf("CLI endpoint imported as HTTP: %+v", node)
			}
		}
	}
	if httpInterfaces != 23 {
		t.Fatalf("HTTP interfaces=%d, want 23", httpInterfaces)
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

// minimalSyftJSON is a minimal Syft SBOM JSON without any artifacts.
const minimalSyftJSON = `{"descriptor":{"name":"syft","version":"1.2.3"},"artifacts":[],"artifactRelationships":[],"source":{"type":"directory","target":"."},"distro":{}}`

// TestCallerAssertedBindingContractEndToEnd covers the four clauses of the
// caller-assertion contract described in the PR:
//  1. complete digest + CallerAsserted in directory mode → caller_asserted
//  2. partial/no digest + CallerAsserted → unknown (not caller_asserted)
//  3. git source + CallerAsserted → unknown (not caller_asserted)
//  4. SARIF revision mismatch + CallerAsserted → mismatch (not overridden)
func TestCallerAssertedBindingContractEndToEnd(t *testing.T) {
	root := t.TempDir()
	writeMapSourceFixture(t, root)
	syftPath := filepath.Join(t.TempDir(), "syft.json")
	if err := os.WriteFile(syftPath, []byte(minimalSyftJSON), 0600); err != nil {
		t.Fatal(err)
	}

	ledgerBinding := func(t *testing.T, doc mapdoc.Document) string {
		t.Helper()
		if len(doc.CoverageLedger) == 0 {
			t.Fatal("coverage ledger is empty")
		}
		return doc.CoverageLedger[0].Binding
	}

	// Clause 1: complete directory digest + caller-asserted → caller_asserted
	t.Run("complete_digest_caller_asserted", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			// Windows digests are partial (windows_checkout_semantics), and a
			// caller assertion requires a complete digest.
			t.Skip("caller assertion requires a complete digest, which Windows cannot produce")
		}
		out, stderr, err := invoke("map", "--source", "directory", "--json",
			"--attach", "syft-json="+syftPath,
			"--attach-binding", "caller-asserted",
			root)
		if err != nil {
			t.Fatalf("unexpected exit 1: %v\nstderr: %s", err, stderr)
		}
		var doc mapdoc.Document
		if err := json.Unmarshal([]byte(out), &doc); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if err := mapdoc.Validate(doc); err != nil {
			t.Fatalf("validate: %v", err)
		}
		if b := ledgerBinding(t, doc); b != string(providerjoin.BindingCallerAsserted) {
			t.Fatalf("binding = %q, want %q", b, providerjoin.BindingCallerAsserted)
		}
	})

	// Clause 2: no digest (digest=off) + caller-asserted → unknown
	t.Run("no_digest_caller_asserted", func(t *testing.T) {
		out, stderr, err := invoke("map", "--source", "directory", "--json",
			"--set", "source.digest=off",
			"--attach", "syft-json="+syftPath,
			"--attach-binding", "caller-asserted",
			root)
		if err != nil {
			t.Fatalf("unexpected exit 1: %v\nstderr: %s", err, stderr)
		}
		var doc mapdoc.Document
		if err := json.Unmarshal([]byte(out), &doc); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if b := ledgerBinding(t, doc); b != string(providerjoin.BindingUnknown) {
			t.Fatalf("binding = %q, want unknown (no digest means caller assertion cannot apply)", b)
		}
	})

	// Clause 3: git source + caller-asserted (canCallerAssert requires directory mode)
	t.Run("git_source_caller_asserted_is_not_applicable", func(t *testing.T) {
		git, err := exec.LookPath("git")
		if err != nil {
			t.Skip("git unavailable")
		}
		gitRoot := t.TempDir()
		writeMapSourceFixture(t, gitRoot)
		gitEnv := append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL="+os.DevNull, "HOME="+t.TempDir(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.invalid", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.invalid")
		for _, args := range [][]string{{"init", "-q"}, {"add", "-A"}, {"commit", "-q", "-m", "init"}} {
			cmd := exec.Command(git, args...)
			cmd.Dir, cmd.Env = gitRoot, gitEnv
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("git %v: %s", args, out)
			}
		}
		out, stderr, err := invoke("map", "--source", "git", "--json",
			"--attach", "syft-json="+syftPath,
			"--attach-binding", "caller-asserted",
			gitRoot)
		if err != nil {
			t.Fatalf("unexpected exit 1: %v\nstderr: %s", err, stderr)
		}
		var doc mapdoc.Document
		if err := json.Unmarshal([]byte(out), &doc); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		// In git mode, canCallerAssert is false, so the binding falls back to unknown.
		if b := ledgerBinding(t, doc); b != string(providerjoin.BindingUnknown) {
			t.Fatalf("binding = %q, want unknown (caller-asserted requires directory mode)", b)
		}
	})

	// Clause 4: SARIF with mismatched revisionId + caller-asserted on git source
	// → mismatch (the comparable mismatch must not be overridden).
	t.Run("git_source_sarif_mismatch_not_overridden", func(t *testing.T) {
		git, err := exec.LookPath("git")
		if err != nil {
			t.Skip("git unavailable")
		}
		gitRoot := t.TempDir()
		writeMapSourceFixture(t, gitRoot)
		gitEnv := append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL="+os.DevNull, "HOME="+t.TempDir(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.invalid", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.invalid")
		for _, args := range [][]string{{"init", "-q"}, {"add", "-A"}, {"commit", "-q", "-m", "init"}} {
			cmd := exec.Command(git, args...)
			cmd.Dir, cmd.Env = gitRoot, gitEnv
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("git %v: %s", args, out)
			}
		}
		sarifPath2 := filepath.Join(t.TempDir(), "mismatch.sarif")
		sarif := `{"version":"2.1.0","runs":[{"tool":{"driver":{"name":"test-scanner","version":"1.0"}},"results":[],"versionControlProvenance":[{"revisionId":"` + strings.Repeat("f", 40) + `"}]}]}`
		if err := os.WriteFile(sarifPath2, []byte(sarif), 0600); err != nil {
			t.Fatal(err)
		}
		out, stderr, err := invoke("map", "--source", "git", "--json",
			"--attach", "sarif="+sarifPath2,
			"--attach-binding", "caller-asserted",
			gitRoot)
		if err != nil {
			t.Fatalf("unexpected exit 1: %v\nstderr: %s", err, stderr)
		}
		var doc mapdoc.Document
		if err := json.Unmarshal([]byte(out), &doc); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if err := mapdoc.Validate(doc); err != nil {
			t.Fatalf("validate: %v", err)
		}
		// The SARIF revisionId is clearly wrong; canCallerAssert is false in git mode.
		// The binding must be mismatch, not caller_asserted.
		if b := ledgerBinding(t, doc); b != string(providerjoin.BindingMismatch) {
			t.Fatalf("binding = %q, want mismatch (comparable mismatch must not be overridden)", b)
		}
	})
}
