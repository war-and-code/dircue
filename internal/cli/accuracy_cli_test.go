package cli

import (
	"encoding/json"
	"strings"
	"testing"

	"dircue/internal/atlas"
)

// TestCapabilitiesAccuracyPlainText checks the plain-text output of
// dircue capabilities --accuracy (no --json flag).
func TestCapabilitiesAccuracyPlainText(t *testing.T) {
	out, stderr, err := invoke("capabilities", "--accuracy")
	if err != nil {
		t.Fatalf("capabilities --accuracy: stderr=%q err=%v", stderr, err)
	}
	if stderr != "" {
		t.Errorf("unexpected stderr: %q", stderr)
	}
	if !strings.Contains(out, "Accuracy cards:") {
		t.Errorf("plain-text output missing 'Accuracy cards:' header; got: %q", out[:min(len(out), 200)])
	}
	// Kind lines are indented with two spaces ("  kind: ...").
	// Every kind line must contain "correct", "CI lower=", and "path-scoped".
	kindLineCount := 0
	for _, line := range strings.Split(out, "\n") {
		if !strings.HasPrefix(line, "  ") {
			continue // header or scope-note line
		}
		kindLineCount++
		if !strings.Contains(line, "correct") {
			t.Errorf("kind line missing 'correct': %q", line)
		}
		if !strings.Contains(line, "CI lower=") {
			t.Errorf("kind line missing 'CI lower=': %q", line)
		}
		if !strings.Contains(line, "path-scoped") {
			t.Errorf("kind line missing 'path-scoped': %q", line)
		}
		// All current kinds have < 30 labels; every line must flag insufficient_labels
		if !strings.Contains(line, "INSUFFICIENT") {
			t.Errorf("kind line missing INSUFFICIENT label warning: %q", line)
		}
	}
	if kindLineCount == 0 {
		t.Errorf("no kind lines found in output; got: %q", out[:min(len(out), 400)])
	}
}

// TestCapabilitiesAccuracyJSON checks that --accuracy --json emits valid JSON
// matching the embedded atlas.AccuracyCards structure.
func TestCapabilitiesAccuracyJSON(t *testing.T) {
	out, stderr, err := invoke("capabilities", "--accuracy", "--json")
	if err != nil {
		t.Fatalf("capabilities --accuracy --json: stderr=%q err=%v", stderr, err)
	}
	if stderr != "" {
		t.Errorf("unexpected stderr: %q", stderr)
	}
	var cards atlas.AccuracyCards
	if err := json.Unmarshal([]byte(out), &cards); err != nil {
		t.Fatalf("output is not valid JSON: %v\ngot: %q", err, out[:min(len(out), 400)])
	}
	if cards.SchemaVersion == "" {
		t.Error("schema_version is empty")
	}
	if cards.LabelSource == "" {
		t.Error("label_source is empty")
	}
	if cards.ScopeNote == "" {
		t.Error("scope_note is empty")
	}
}

// TestCapabilitiesAccuracyMutuallyExclusive checks that --accuracy cannot be
// combined with --cli, --guide, or --schema.
func TestCapabilitiesAccuracyMutuallyExclusive(t *testing.T) {
	for _, combo := range [][]string{
		{"capabilities", "--accuracy", "--cli"},
		{"capabilities", "--accuracy", "--guide"},
		{"capabilities", "--accuracy", "--schema", "profile"},
	} {
		_, _, err := invoke(combo...)
		if err == nil {
			t.Errorf("expected error combining %v but got none", combo)
		}
	}
}

// TestCapabilitiesAccuracyFalseRejected checks that --accuracy=false is rejected.
func TestCapabilitiesAccuracyFalseRejected(t *testing.T) {
	_, _, err := invoke("capabilities", "--accuracy=false")
	if err == nil {
		t.Error("expected error for --accuracy=false but got none")
	}
}

// TestCapabilitiesAccuracyDoesNotScan verifies that --accuracy returns without
// requiring any source path, and produces deterministic output on repeated calls.
func TestCapabilitiesAccuracyDoesNotScan(t *testing.T) {
	out1, stderr1, err1 := invoke("capabilities", "--accuracy", "--json")
	out2, stderr2, err2 := invoke("capabilities", "--accuracy", "--json")
	if err1 != nil || err2 != nil {
		t.Fatalf("invocation errors: %v %v", err1, err2)
	}
	if stderr1 != "" || stderr2 != "" {
		t.Errorf("unexpected stderr: %q %q", stderr1, stderr2)
	}
	if out1 != out2 {
		t.Error("--accuracy output is not deterministic across two identical calls")
	}
}

// TestCapabilitiesAccuracyCatalogContract verifies that the catalog records the
// accuracy-cards output contract for dircue capabilities.
func TestCapabilitiesAccuracyCatalogContract(t *testing.T) {
	out, stderr, err := invoke("capabilities", "--cli", "--json")
	if err != nil || stderr != "" {
		t.Fatalf("capabilities --cli --json: stderr=%q err=%v", stderr, err)
	}
	var contract cliContract
	if err := json.Unmarshal([]byte(out), &contract); err != nil {
		t.Fatal(err)
	}
	// Find capabilities command
	var capCmd *cliCommandContract
	for i, cmd := range contract.Commands {
		if len(cmd.Path) == 2 && cmd.Path[0] == "dircue" && cmd.Path[1] == "capabilities" {
			capCmd = &contract.Commands[i]
			break
		}
	}
	if capCmd == nil {
		t.Fatal("capabilities command not found in contract")
	}
	// Check accuracy-cards output contract is listed
	found := false
	for _, oc := range capCmd.OutputContracts {
		if oc == "accuracy-cards" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("accuracy-cards not in capabilities output_contracts: %v", capCmd.OutputContracts)
	}
	// Check --accuracy flag is listed
	foundFlag := false
	for _, f := range capCmd.Flags {
		if f.Name == "accuracy" {
			foundFlag = true
			break
		}
	}
	if !foundFlag {
		t.Errorf("--accuracy flag not in capabilities flags catalog")
	}
	// Check restrictions mention --accuracy
	foundRestriction := false
	for _, r := range capCmd.Restrictions {
		if strings.Contains(r, "--accuracy") {
			foundRestriction = true
			break
		}
	}
	if !foundRestriction {
		t.Errorf("no restriction mentioning --accuracy in capabilities restrictions: %v", capCmd.Restrictions)
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
