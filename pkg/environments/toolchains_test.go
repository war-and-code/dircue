package environments

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestToolchainLiteralSubsetAndNoWinnerBoundaries(t *testing.T) {
	in := envInput(map[string]string{
		".python-version":       "3.11.8\n3.12.2\n# comment\n",
		".node-version":         "22.4.0\n",
		".nvmrc":                "20.5.1 # nvm comment\n",
		"apps/a/.node-version":  "18.20.0\n",
		"rust-toolchain.toml":   "[toolchain]\nchannel = \"stable\"\ncomponents = [\"rustfmt\"]\n",
		"apps/a/rust-toolchain": "nightly-2025-01-01\n",
	}, nil)
	r, err := Analyze(t.Context(), in, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.ToolchainDeclarations) != 6 {
		t.Fatalf("declarations = %+v", r.ToolchainDeclarations)
	}
	if len(r.Conflicts) != 1 || r.Conflicts[0].Dimension != "toolchain-declaration" || r.Conflicts[0].Values[0] == r.Conflicts[0].Values[1] {
		t.Fatalf("same-scope conflicting node declarations: %+v", r.Conflicts)
	}
	nested := 0
	for _, b := range r.Boundaries {
		if b.Reason == "nested-toolchain-declaration" {
			nested++
		}
	}
	if nested != 3 {
		t.Fatalf("nested declarations should be retained in both ecosystems without selecting winners: %+v", r.Boundaries)
	}
	if err := ValidateReport(r); err != nil {
		t.Fatalf("validate current report: %v", err)
	}
}

func TestToolchainMalformedUnsupportedAndBoundedValues(t *testing.T) {
	files := map[string]string{
		".python-version":              "3.10\n3.11\n3.12\n",
		"app/.nvmrc":                   "20\n21\n",
		"app/rust-toolchain.toml":      "[toolchain]\nchannel = \"$HOME/nightly\"\n",
		"other/rust-toolchain.toml":    "[toolchain\nchannel = \"stable\"\n",
		"pathonly/rust-toolchain.toml": "[toolchain]\npath = \"/opt/rust\"\n",
		"unknown/rust-toolchain.toml":  "[toolchain]\nchannel = \"stable\"\nfuture = true\n",
		"deep/rust-toolchain.toml":     "[toolchain]\ncomponents = " + strings.Repeat("[", 65) + "0" + strings.Repeat("]", 65) + "\nchannel = \"stable\"\n",
	}
	r, err := Analyze(t.Context(), envInput(files, nil), Limits{})
	if err != nil {
		t.Fatal(err)
	}
	if r.Status != "partial" || len(r.Diagnostics) != 6 {
		t.Fatalf("malformed/unsupported declarations not surfaced: status=%s diag=%+v", r.Status, r.Diagnostics)
	}
	for _, d := range r.Diagnostics {
		if strings.Contains(d.Message, "$HOME") || strings.Contains(d.Message, "/opt/rust") {
			t.Fatalf("raw declaration leaked in diagnostic: %+v", d)
		}
	}
	for _, d := range r.ToolchainDeclarations {
		if d.SourcePath == ".python-version" && (d.State != "declared" || len(d.Values) != 3) {
			t.Fatalf("multiple Python selectors should be retained: %+v", d)
		}
		if d.SourcePath == "pathonly/rust-toolchain.toml" && (d.State != "unresolved" || len(d.Values) != 0) {
			t.Fatalf("path-based rustup declaration must remain unresolved: %+v", d)
		}
	}
	tooManyPythonSelectors := strings.Repeat("3.12\n", 17)
	r, err = Analyze(t.Context(), envInput(map[string]string{".python-version": tooManyPythonSelectors}, nil), Limits{})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.ToolchainDeclarations) != 1 || r.ToolchainDeclarations[0].State != "unsupported" || len(r.ToolchainDeclarations[0].Values) != 0 {
		t.Fatalf("unbounded Python selector list accepted: %+v", r.ToolchainDeclarations)
	}
}

func TestToolchainFileBoundsSelectionAndReadFailures(t *testing.T) {
	in := envInput(map[string]string{"a/.nvmrc": "20\n", "b/.nvmrc": "21\n", "c/.nvmrc": "22\n"}, nil)
	r, err := Analyze(t.Context(), in, Limits{ToolchainFiles: 2})
	if err != nil {
		t.Fatal(err)
	}
	if r.Status != "partial" || r.Coverage.ToolchainCandidates != 3 || r.Coverage.OmittedToolchainFiles != 1 || len(r.ToolchainDeclarations) != 2 || r.ToolchainDeclarations[0].SourcePath != "a/.nvmrc" {
		t.Fatalf("file limit was not deterministic/visible: %+v", r)
	}
	if _, err := Analyze(t.Context(), envInput(map[string]string{".nvmrc": "20\n"}, nil), Limits{ToolchainFileBytes: 1}); err != nil {
		t.Fatalf("bounded oversize should yield partial evidence rather than error: %v", err)
	}
	r, err = Analyze(t.Context(), envInput(map[string]string{"a/.nvmrc": "20\n", "b/.nvmrc": "21\n"}, nil), Limits{ToolchainInputBytes: 4})
	if err != nil || r.Coverage.ToolchainRead != 1 || r.Coverage.ToolchainBytes != 3 || r.Coverage.OmittedToolchainFiles != 1 {
		t.Fatalf("aggregate toolchain bytes should bound reads: report=%+v err=%v", r, err)
	}
	in = envInput(map[string]string{".nvmrc": "20\n"}, nil)
	in.ReadSelected = func(context.Context, string, int64) ([]byte, int64, error) { return nil, 0, errors.New("secret path") }
	if _, err := Analyze(t.Context(), in, Limits{}); err == nil || strings.Contains(err.Error(), "secret") {
		t.Fatalf("default read errors should fail without leaking underlying error: %v", err)
	}
	in.ErrorPolicy = "continue"
	r, err = Analyze(t.Context(), in, Limits{})
	if err != nil || r.Status != "partial" || len(r.Diagnostics) != 1 || r.Diagnostics[0].Code != "file-read-error" {
		t.Fatalf("continue read-error contract: report=%+v err=%v", r, err)
	}
}

func TestToolchainCancellationAndOldReportCompatibility(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := Analyze(ctx, envInput(map[string]string{".nvmrc": "20\n"}, nil), Limits{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled analysis error = %v", err)
	}

	current, err := Analyze(t.Context(), envInput(nil, nil), Limits{})
	if err != nil {
		t.Fatal(err)
	}
	current.ProviderVersion = LegacyProviderVersion
	current.SemanticsReference = legacySemanticsReference
	current.Limits.ToolchainFiles = 0
	current.Limits.ToolchainFileBytes = 0
	current.Limits.ToolchainInputBytes = 0
	if err := ValidateReport(current); err != nil {
		t.Fatalf("legacy 1.0 report should remain valid: %v", err)
	}
	legacyJSON, err := json.Marshal(current)
	if err != nil {
		t.Fatal(err)
	}
	for _, newerField := range []string{"toolchain_files", "toolchain_candidates", "toolchain_declarations"} {
		if strings.Contains(string(legacyJSON), newerField) {
			t.Fatalf("legacy report reserialization added field %q: %s", newerField, legacyJSON)
		}
	}
	current.ToolchainDeclarations = []ToolchainDeclaration{{SourcePath: ".nvmrc", Tool: "node", Kind: "nvmrc", Values: []string{"20"}, State: "declared", ScopeDirectory: ".", Applicability: toolchainApplicability}}
	if err := ValidateReport(current); err == nil {
		t.Fatal("1.0 report containing 1.1 declarations accepted")
	}
}

func TestToolchainDeclarationsOnlyComeFromSelectedInventory(t *testing.T) {
	in := envInput(map[string]string{".nvmrc": "20\n", "../outside/.nvmrc": "21\n"}, nil)
	// The observer only considers paths admitted by the selected inventory.
	in.Inventory = []File{{Path: ".nvmrc", Size: 3}}
	r, err := Analyze(t.Context(), in, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.ToolchainDeclarations) != 1 || r.ToolchainDeclarations[0].Values[0] != "20" {
		t.Fatalf("unselected path affected declarations: %+v", r.ToolchainDeclarations)
	}
}
