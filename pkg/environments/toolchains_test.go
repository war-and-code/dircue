package environments

import (
	"context"
	"encoding/json"
	"errors"
	"path"
	"strings"
	"testing"

	"github.com/war-and-code/dircue/pkg/declarations"
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
	if nested != 2 {
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

func TestRustToolchainRejectsMalformedOptionalFields(t *testing.T) {
	for _, content := range []string{
		"[toolchain]\nchannel = \"stable\"\ncomponents = \"rustfmt\"\n",
		"[toolchain]\nchannel = \"stable\"\ntargets = [\"x86_64-unknown-linux-gnu\", 7]\n",
		"[toolchain]\nchannel = \"stable\"\nprofile = \"fast\"\n",
	} {
		r, err := Analyze(t.Context(), envInput(map[string]string{"rust-toolchain.toml": content}, nil), Limits{})
		if err != nil {
			t.Fatal(err)
		}
		if r.Status != "partial" || len(r.ToolchainDeclarations) != 1 || r.ToolchainDeclarations[0].State != "declared" || len(r.ToolchainDeclarations[0].Values) != 1 || r.ToolchainDeclarations[0].Values[0] != "stable" {
			t.Fatalf("supported channel literal should be retained while malformed metadata degrades coverage: content=%q report=%+v", content, r)
		}
		if err := ValidateReport(r); err != nil {
			t.Fatalf("analyzer report should validate: %v", err)
		}
		forgedComplete := cloneEnvironmentReport(t, r)
		forgedComplete.Status = "complete"
		if err := ValidateReport(forgedComplete); err == nil {
			t.Fatalf("complete report with malformed Rust metadata diagnostic was accepted: %+v", forgedComplete)
		}
	}

	valid := "[toolchain]\nchannel = \"nightly-2025-01-01\"\ncomponents = [\"rustfmt\", \"rustc-dev\"]\ntargets = [\"x86_64-unknown-linux-gnu\"]\nprofile = \"minimal\"\n"
	r, err := Analyze(t.Context(), envInput(map[string]string{"rust-toolchain.toml": valid}, nil), Limits{})
	if err != nil || r.Status != "complete" || r.ToolchainDeclarations[0].State != "declared" {
		t.Fatalf("valid rustup metadata rejected: report=%+v err=%v", r, err)
	}
}

func TestNVMRCReservedPairsAndCredentialLookingSelectors(t *testing.T) {
	r, err := Analyze(t.Context(), envInput(map[string]string{".nvmrc": "lts/* # latest LTS\nfuture_option=value\n"}, nil), Limits{})
	if err != nil || r.Status != "complete" || len(r.ToolchainDeclarations) != 1 || r.ToolchainDeclarations[0].Values[0] != "lts/*" {
		t.Fatalf("nvm's documented comment and reserved key/value syntax should retain its selector: report=%+v err=%v", r, err)
	}
	secretLike := "AKIAIOSFODNN7EXAMPLE"
	r, err = Analyze(t.Context(), envInput(map[string]string{".python-version": secretLike}, nil), Limits{})
	if err != nil || r.Status != "partial" || len(r.ToolchainDeclarations) != 1 || r.ToolchainDeclarations[0].State != "unsupported" || len(r.ToolchainDeclarations[0].Values) != 0 {
		t.Fatalf("credential-looking selector must not be emitted as a version: report=%+v err=%v", r, err)
	}
	encoded, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), secretLike) {
		t.Fatalf("credential-looking selector leaked into report: %s", encoded)
	}
}

func TestNestedUnresolvedToolchainStillHasLookupBoundary(t *testing.T) {
	r, err := Analyze(t.Context(), envInput(map[string]string{
		".node-version":       "20\n",
		"apps/service/.nvmrc": "20\n21\n",
	}, nil), Limits{})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, boundary := range r.Boundaries {
		if boundary.Reason == "nested-toolchain-declaration" && boundary.Path == "apps/service/.nvmrc" {
			found = true
		}
	}
	if !found {
		t.Fatalf("nested candidate should remain visible even when its contents are unsupported: %+v", r.Boundaries)
	}
}

func TestToolchainReaderCancellationAfterSuccessfulReadIsFatal(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	in := envInput(map[string]string{".nvmrc": "20\n"}, nil)
	in.ReadSelected = func(context.Context, string, int64) ([]byte, int64, error) {
		cancel()
		return []byte("20\n"), 3, nil
	}
	if _, err := Analyze(ctx, in, Limits{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("successful read after cancellation should abort analysis, got %v", err)
	}
}

func TestRustTOMLPreflightBoundsDottedKeyDepth(t *testing.T) {
	exactDepthKey := strings.TrimSuffix(strings.Repeat("segment.", 63), ".")
	if !tomlDottedNestingWithinBound([]byte(exactDepthKey+" = 1\n"), 64) {
		t.Fatal("dotted key exactly at the depth bound should pass")
	}
	deepKey := strings.TrimSuffix(strings.Repeat("segment.", 65), ".")
	if tomlDottedNestingWithinBound([]byte(deepKey+" = 1\n"), 64) {
		t.Fatal("deep dotted key passed the structural depth bound")
	}
	compound := "[toolchain]\n" + strings.TrimSuffix(strings.Repeat("segment.", 64), ".") + " = 1\n"
	if tomlDottedNestingWithinBound([]byte(compound), 64) {
		t.Fatal("table and dotted-key depth must share one bound")
	}
	combinedAtLimit := "[toolchain]\n" + strings.TrimSuffix(strings.Repeat("segment.", 62), ".") + " = 1\n"
	if !tomlDottedNestingWithinBound([]byte(combinedAtLimit), 64) {
		t.Fatal("combined table and dotted-key depth exactly at the bound should pass")
	}
	inlineKey := strings.TrimSuffix(strings.Repeat("segment.", 65), ".")
	inline := "[toolchain]\nchannel = \"stable\"\ncomponents = { " + inlineKey + " = 1 }\n"
	if tomlDottedNestingWithinBound([]byte(inline), 64) {
		t.Fatal("dotted keys nested in inline tables must share the structural depth bound")
	}
	if values, state, _ := parseRustToolchainTOML([]byte(inline)); state != "unresolved" || len(values) != 0 {
		t.Fatalf("inline dotted-key depth should be rejected before TOML decoding: values=%v state=%s", values, state)
	}
	multilineArray := "[toolchain]\ncomponents = [\n  [\"rustfmt\"]\n]\n"
	if !tomlDottedNestingWithinBound([]byte(multilineArray), 64) {
		t.Fatal("array continuation beginning with `[` must not be interpreted as a table header")
	}
	validMultilineArray := "[toolchain]\nchannel = \"stable\"\ncomponents = [\n  \"rustfmt\"\n]\n"
	if values, state, code := parseRustToolchainTOML([]byte(validMultilineArray)); state != "declared" || code != "" || len(values) != 1 || values[0] != "stable" {
		t.Fatalf("valid rustup array should pass preflight: values=%v state=%s code=%s", values, state, code)
	}
	ordinary := "[\"tool.chain\"]\nvalue = \"a.b.c\" # comment.with.dots\n"
	if !tomlDottedNestingWithinBound([]byte(ordinary), 64) {
		t.Fatal("dots inside quoted keys, values, and comments should not count as nesting")
	}
}

func TestToolchainNestedConflictBoundariesGrowLinearly(t *testing.T) {
	declarations := make([]ToolchainDeclaration, 4090)
	scope := "."
	for i := range declarations {
		if i > 0 {
			scope = path.Join(scope, "a")
		}
		source := path.Join(scope, ".node-version")
		declarations[i] = ToolchainDeclaration{SourcePath: source, Tool: "node", Kind: "node-version", Values: []string{"20"}, State: "declared", ScopeDirectory: scope}
	}
	report := &Report{ToolchainDeclarations: declarations}
	detectToolchainConflicts(report)
	if len(report.Boundaries) != len(declarations)-1 {
		t.Fatalf("nested boundaries should be capped at one per nested candidate, got %d for %d declarations", len(report.Boundaries), len(declarations))
	}
	if len(report.Conflicts) != 0 {
		t.Fatalf("identical nested selectors should not produce same-scope conflicts: %+v", report.Conflicts)
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

func TestValidateReportRejectsInvalidToolchainIdentityAndCompleteUnknowns(t *testing.T) {
	valid, err := Analyze(t.Context(), envInput(map[string]string{"apps/café project/.nvmrc": "20\n"}, nil), Limits{})
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateReport(valid); err != nil {
		t.Fatalf("ordinary Unicode/space path should remain valid: %v", err)
	}
	mutations := []struct {
		name   string
		change func(*Report)
	}{
		{"traversal source", func(r *Report) {
			r.ToolchainDeclarations[0].SourcePath = "../outside/.nvmrc"
			r.ToolchainDeclarations[0].ScopeDirectory = "../outside"
		}},
		{"absolute source", func(r *Report) {
			r.ToolchainDeclarations[0].SourcePath = "/outside/.nvmrc"
			r.ToolchainDeclarations[0].ScopeDirectory = "/outside"
		}},
		{"noncanonical source", func(r *Report) { r.ToolchainDeclarations[0].SourcePath = "apps/../apps/café project/.nvmrc" }},
		{"scope mismatch", func(r *Report) { r.ToolchainDeclarations[0].ScopeDirectory = "." }},
		{"kind mismatch", func(r *Report) { r.ToolchainDeclarations[0].Kind = "python-version" }},
		{"tool mismatch", func(r *Report) { r.ToolchainDeclarations[0].Tool = "python" }},
		{"applicability overclaim", func(r *Report) { r.ToolchainDeclarations[0].Applicability = "installed and selected" }},
	}
	for _, tt := range mutations {
		t.Run(tt.name, func(t *testing.T) {
			candidate := cloneEnvironmentReport(t, valid)
			tt.change(candidate)
			if err := ValidateReport(candidate); err == nil {
				t.Fatal("invalid toolchain identity was accepted")
			}
		})
	}

	unsupported, err := Analyze(t.Context(), envInput(map[string]string{".nvmrc": "20\n21\n"}, nil), Limits{})
	if err != nil {
		t.Fatal(err)
	}
	unsupported.Status = "complete"
	unsupported.Diagnostics = []Diagnostic{}
	if err := ValidateReport(unsupported); err == nil {
		t.Fatal("complete report with unsupported toolchain selector was accepted")
	}
	unresolved, err := Analyze(t.Context(), envInput(map[string]string{".nvmrc": "20\n"}, nil), Limits{})
	if err != nil {
		t.Fatal(err)
	}
	unresolved.Status = "complete"
	unresolved.ToolchainDeclarations[0].State = "unresolved"
	unresolved.ToolchainDeclarations[0].Values = nil
	if err := ValidateReport(unresolved); err == nil {
		t.Fatal("complete report with unresolved toolchain selector was accepted")
	}
}

func TestCompleteEnvironmentAllowsDocumentedJSONCLeniencyDiagnostic(t *testing.T) {
	r, err := Analyze(t.Context(), envInput(map[string]string{"global.json": "// pinned\n{\"sdk\":{\"version\":\"7.0.100\"}}"}, []Invocation{{"project", "."}}), Limits{})
	if err != nil {
		t.Fatal(err)
	}
	if r.Status != "complete" || len(r.Diagnostics) != 1 || r.Diagnostics[0].Code != "global-json-lenient-syntax" {
		t.Fatalf("fixture did not exercise informational diagnostic: %+v", r)
	}
	if err := ValidateReport(r); err != nil {
		t.Fatalf("informational JSONC diagnostic must not invalidate complete coverage: %v", err)
	}
}

func TestCompleteEnvironmentAllowsUnresolvedProjectRequirement(t *testing.T) {
	// The eShop MAUI project declares a static target-framework list plus a
	// conditional MSBuild expression that the observer intentionally leaves
	// unresolved. That unresolved declaration does not make the environment
	// inventory itself incomplete, and must not cause report validation to
	// reject the report produced by Analyze.
	body := `<Project Sdk="Microsoft.NET.Sdk"><PropertyGroup>` +
		`<TargetFrameworks>net10.0-android;net10.0-ios;net10.0-maccatalyst;net10.0</TargetFrameworks>` +
		`<TargetFrameworks Condition="$([MSBuild]::IsOSPlatform('windows'))">$(TargetFrameworks);net10.0-windows10.0.19041.0</TargetFrameworks>` +
		`</PropertyGroup></Project>`
	doc := declarations.Parse("src/ClientApp/ClientApp.csproj", []byte(body))
	if doc == nil || !doc.Parsed || doc.Project == nil {
		t.Fatalf("fixture did not parse as static project metadata: %+v", doc)
	}
	foundUnresolved := false
	for _, requirement := range doc.Project.Requirements {
		if requirement.Kind == "target-framework" && requirement.State == "unresolved" {
			foundUnresolved = true
		}
	}
	if !foundUnresolved {
		t.Fatalf("fixture no longer exercises unresolved target-framework metadata: %+v", doc.Project.Requirements)
	}
	in := envInput(nil, nil)
	in.ProjectRecords = []declarations.ProjectRecord{{Project: *doc.Project, Parsed: true, Complete: true}}
	r, err := Analyze(t.Context(), in, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	if r.Status != "complete" {
		t.Fatalf("unresolved project metadata changed environment coverage: %+v", r)
	}
	if err := ValidateReport(r); err != nil {
		t.Fatalf("valid complete report with unresolved project metadata was rejected: %v", err)
	}
}

func cloneEnvironmentReport(t *testing.T, r *Report) *Report {
	t.Helper()
	data, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	var clone Report
	if err := json.Unmarshal(data, &clone); err != nil {
		t.Fatal(err)
	}
	return &clone
}

func TestValidatorRejectsMultipleNodeSelectors(t *testing.T) {
	in := Input{Source: "directory", InventoryComplete: true, Inventory: []File{{Path: ".nvmrc", Size: 3}}, ReadSelected: func(context.Context, string, int64) ([]byte, int64, error) { return []byte("20\n"), 3, nil }}
	report, err := Analyze(context.Background(), in, Limits{})
	if err != nil || ValidateReport(report) != nil {
		t.Fatalf("valid single-selector input failed: %v", err)
	}
	report.ToolchainDeclarations[0].Values = []string{"20", "22"}
	if ValidateReport(report) == nil {
		t.Fatal("multiple Node selectors accepted as one declared toolchain")
	}
}
