package environments

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"dircue/pkg/declarations"
)

func envInput(files map[string]string, starts []Invocation) Input {
	in := Input{Source: "directory", InventoryComplete: true, InvocationStarts: starts, Declarations: declarations.Report{Status: "complete"}}
	for p, v := range files {
		in.Inventory = append(in.Inventory, File{Path: p, Size: int64(len(v))})
	}
	in.ReadSelected = func(ctx context.Context, p string, max int64) ([]byte, int64, error) {
		if err := ctx.Err(); err != nil {
			return nil, 0, err
		}
		v, ok := files[p]
		if !ok {
			return nil, 0, errors.New("not selected")
		}
		return []byte(v), int64(len(v)), nil
	}
	return in
}

func TestNearestGlobalJSONAndExplicitStart(t *testing.T) {
	in := envInput(map[string]string{"global.json": `{"sdk":{"version":"8.0.100","rollForward":"disable"}}`, "src/global.json": `{"sdk":{"version":"9.0.200","rollForward":"latestFeature","allowPrerelease":false}}`}, []Invocation{{"app", "src/app"}, {"root", "tools"}})
	r, err := Analyze(context.Background(), in, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Selections) != 2 || r.Selections[0].GlobalJSON != "src/global.json" || r.Selections[0].SDKVersion != "9.0.200" || r.Selections[1].GlobalJSON != "global.json" {
		t.Fatalf("selections: %+v", r.Selections)
	}
	if r.Selections[0].AllowPrerelease == nil || *r.Selections[0].AllowPrerelease {
		t.Fatalf("allow prerelease: %+v", r.Selections[0])
	}
}

func TestMalformedAndUnsupportedGlobalJSON(t *testing.T) {
	tests := []struct{ name, body, code string }{
		{"malformed", `{"sdk":`, "invalid-global-json"},
		{"version range", `{"sdk":{"version":">=8"}}`, "unsupported-sdk-version"},
		{"roll forward", `{"sdk":{"version":"8.0.100","rollForward":"sideways"}}`, "unsupported-roll-forward"},
		{"prerelease type", `{"sdk":{"allowPrerelease":"yes"}}`, "invalid-allow-prerelease"},
		{"null sdk", `{"sdk":null}`, "invalid-global-json-sdk"},
		{"null prerelease", `{"sdk":{"allowPrerelease":null}}`, "invalid-allow-prerelease"},
		{"duplicate sdk field", `{"sdk":{"version":"8.0.100","version":"9.0.100"}}`, "invalid-global-json"},
		{"duplicate escaped sdk field", `{"sdk":{"version":"8.0.100","versi\u006fn":"9.0.100"}}`, "invalid-global-json"},
		{"unknown sdk field", `{"sdk":{"version":"8.0.100","futurePolicy":true}}`, "unsupported-sdk-field"},
		{"unterminated comment", `{"sdk":{}} /*`, "invalid-global-json"},
		{"null root", `null`, "invalid-global-json"},
		{"wrong sdk casing", `{"SDK":{"version":"8.0.100"}}`, "unsupported-sdk-casing"},
		{"trailing prerelease dot", `{"sdk":{"version":"8.0.100-preview."}}`, "unsupported-sdk-version"},
		{"missing object property", `{"sdk":{,}}`, "invalid-global-json"},
		{"missing array value", `{"sdk":{"paths":[,]}}`, "invalid-global-json"},
		{"repeated comma", `{"sdk":{"paths":["x",,]}}`, "invalid-global-json"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, err := Analyze(context.Background(), envInput(map[string]string{"global.json": tt.body}, []Invocation{{"p", "."}}), Limits{})
			if err != nil {
				t.Fatal(err)
			}
			if r.Status != "partial" || len(r.Diagnostics) == 0 || r.Diagnostics[0].Code != tt.code || r.Selections[0].State != "unresolved" {
				t.Fatalf("report: %+v", r)
			}
		})
	}
}

func TestDeepGlobalJSONAndReportValidation(t *testing.T) {
	body := strings.Repeat(`[`, 130) + `0` + strings.Repeat(`]`, 130)
	r, err := Analyze(context.Background(), envInput(map[string]string{"global.json": body}, []Invocation{{"p", "."}}), Limits{})
	if err != nil {
		t.Fatal(err)
	}
	if r.Selections[0].State != "unresolved" {
		t.Fatalf("deep JSON accepted: %+v", r)
	}
	valid, err := Analyze(context.Background(), envInput(nil, nil), Limits{})
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateReport(valid); err != nil {
		t.Fatal(err)
	}
	valid.Coverage.Contexts++
	if err := ValidateReport(valid); err == nil {
		t.Fatal("inconsistent coverage accepted")
	}
}

func TestNormalizeActualManifestFacts(t *testing.T) {
	cases := []struct{ name, body, kind, dimension string }{
		{"go.mod", "module x\ngo 1.22\ntoolchain go1.23.1\n", "go-language-minimum", "language-minimum"},
		{"package.json", `{"name":"x","packageManager":"npm@11.0.0","engines":{"node":">=20"}}`, "npm-engine", "runtime-constraint"},
		{"pyproject.toml", "[project]\nname='x'\nversion='1.0'\nrequires-python='>=3.11,<4'\n", "python-requires-python", "runtime-constraint"},
		{"Cargo.toml", "[package]\nname='x'\nversion='1.0.0'\nedition='2021'\nrust-version='1.70'\n", "cargo-rust-version", "language-minimum"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			doc := declarations.Parse(tc.name, []byte(tc.body))
			if doc == nil || !doc.Parsed {
				t.Fatalf("parse: %+v", doc)
			}
			in := envInput(nil, nil)
			in.ProjectRecords = []declarations.ProjectRecord{{Project: *doc.Project, Parsed: true, Complete: true}}
			r, err := Analyze(context.Background(), in, Limits{})
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, q := range r.Requirements {
				if q.Kind == tc.kind && q.Dimension == tc.dimension {
					found = true
				}
			}
			if !found {
				t.Fatalf("missing %s/%s: %+v", tc.kind, tc.dimension, r.Requirements)
			}
		})
	}
}

func TestSharedGlobalJSONReadOnceAndInternalInventoryOmission(t *testing.T) {
	in := envInput(map[string]string{"global.json": `{"sdk":{"version":"8.0.100"}}`}, []Invocation{{"a", "a"}, {"b", "b"}})
	reads := 0
	original := in.ReadSelected
	in.ReadSelected = func(ctx context.Context, p string, n int64) ([]byte, int64, error) {
		reads++
		return original(ctx, p, n)
	}
	r, err := Analyze(context.Background(), in, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	if reads != 1 || r.Coverage.GlobalJSONRead != 1 || len(r.Selections) != 2 || r.Status != "complete" || r.Selections[0].SDKVersion != "8.0.100" || r.Selections[1].SDKVersion != "8.0.100" {
		t.Fatalf("reads=%d report=%+v", reads, r)
	}
	in.Inventory = append(in.Inventory, File{Path: "a/global.json", Size: 2})
	r, err = Analyze(context.Background(), in, Limits{InventoryPaths: 1})
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range r.Selections {
		if s.State != "unresolved" {
			t.Fatalf("omitted inventory produced selection: %+v", s)
		}
	}
}

func TestReportSourceIdentityValidation(t *testing.T) {
	r, err := Analyze(context.Background(), envInput(nil, nil), Limits{})
	if err != nil {
		t.Fatal(err)
	}
	r.Source = "other"
	if ValidateReport(r) == nil {
		t.Fatal("accepted unsupported source")
	}
	r.Source, r.Tree = "directory", "unexpected-tree"
	if ValidateReport(r) == nil {
		t.Fatal("accepted directory tree identity")
	}
	r.Source, r.Tree = "git", ""
	if ValidateReport(r) == nil {
		t.Fatal("accepted git source without tree")
	}
}

func TestCommentsPathsAndNoSDKProbe(t *testing.T) {
	in := envInput(map[string]string{"global.json": "{/* pinned */\n\"sdk\":{\"version\":\"10.0.100\",\"paths\":[\".dotnet\",\"$host$\"]}}"}, []Invocation{{"p", "."}})
	r, err := Analyze(context.Background(), in, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	if r.Selections[0].RollForward != "patch" || len(r.Boundaries) != 1 || r.Boundaries[0].Reason != "sdk-search-paths-unresolved" {
		t.Fatalf("report: %+v", r)
	}
}

func TestGlobalJSONMatchesDotnetAcceptedTextAndEmptyPolicySemantics(t *testing.T) {
	tests := []struct {
		name, body, state, version string
		boundaries                 int
	}{
		{"bom and trailing commas", "\xef\xbb\xbf{\"sdk\":{\"version\":\"8.0.100\",},}", "declared", "8.0.100", 0},
		{"null paths", `{"sdk":{"paths":null}}`, "unconstrained", "", 0},
		{"empty paths", `{"sdk":{"paths":[]}}`, "unconstrained", "", 0},
		{"paths only", `{"sdk":{"paths":[".dotnet"]}}`, "unconstrained", "", 1},
		{"empty sdk", `{"sdk":{}}`, "unconstrained", "", 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, err := Analyze(t.Context(), envInput(map[string]string{"global.json": tt.body}, []Invocation{{"p", "."}}), Limits{})
			if err != nil {
				t.Fatal(err)
			}
			if got := r.Selections[0]; got.State != tt.state || got.SDKVersion != tt.version || got.RollForward != map[bool]string{true: "patch", false: ""}[tt.version != ""] || len(r.Boundaries) != tt.boundaries {
				t.Fatalf("report: %+v", r)
			}
		})
	}
}

func TestSharedGlobalJSONDiagnosticsAreFileLevelAndContextsIncludeSolutions(t *testing.T) {
	in := envInput(map[string]string{"global.json": `{"sdk":{"futurePolicy":true}}`}, nil)
	in.ProjectRecords = []declarations.ProjectRecord{
		{Project: declarations.Project{ID: "App.sln", Root: ".", Kind: "solution"}, Parsed: true, Complete: true},
		{Project: declarations.Project{ID: "src/App.csproj", Root: "src", Kind: "dotnet"}, Parsed: true, Complete: true},
	}
	r, err := Analyze(t.Context(), in, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Selections) != 2 || len(r.Diagnostics) != 1 || r.Diagnostics[0].Code != "unsupported-sdk-field" {
		t.Fatalf("report: %+v", r)
	}
}

func TestSharedPropertiesAreDisclosedWithoutInferringBuildSemantics(t *testing.T) {
	in := envInput(map[string]string{"Directory.Build.props": `<Project><PropertyGroup><TargetFramework>net8.0</TargetFramework></PropertyGroup></Project>`}, []Invocation{{"src/App.csproj", "src"}})
	r, err := Analyze(t.Context(), in, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, b := range r.Boundaries {
		if b.Reason == "shared-properties-applicability-unresolved" && b.ProjectID == "src/App.csproj" {
			found = true
		}
	}
	if !found || len(r.Requirements) != 0 {
		t.Fatalf("report: %+v", r)
	}
}

func TestLoneGlobalJSONGetsModeledContext(t *testing.T) {
	in := envInput(map[string]string{"global.json": `{"sdk":{"version":"8.0.100"}}`}, nil)
	in.ProjectRecords = []declarations.ProjectRecord{{Project: declarations.Project{ID: "global.json", Root: ".", Kind: "dotnet-configuration"}, Parsed: true, Complete: true}}
	r, err := Analyze(t.Context(), in, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Selections) != 1 || r.Selections[0].SDKVersion != "8.0.100" || r.Selections[0].ProjectID != "global.json" {
		t.Fatalf("report: %+v", r)
	}
}

func TestSingleRequirementPythonConflictIsNotDuplicated(t *testing.T) {
	in := envInput(nil, nil)
	in.ProjectRecords = []declarations.ProjectRecord{{Project: declarations.Project{ID: "pyproject.toml", Root: ".", Requirements: []declarations.Requirement{{Kind: "python-requires-python", Value: ">=3.8,<3.8", State: "declared", Evidence: "pyproject.toml"}}}, Parsed: true, Complete: true}}
	r, err := Analyze(t.Context(), in, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Conflicts) != 1 || len(r.Conflicts[0].Values) != 1 || len(r.Conflicts[0].Evidence) != 1 {
		t.Fatalf("conflicts: %+v", r.Conflicts)
	}
}

func TestNonregularNearestGlobalJSONBlocksParentFallback(t *testing.T) {
	in := envInput(map[string]string{"global.json": `{"sdk":{"version":"8.0.100"}}`}, []Invocation{{"p", "src/app"}})
	in.Inventory = append(in.Inventory, File{Path: "src/global.json", NonRegular: true})
	r, err := Analyze(context.Background(), in, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	if r.Selections[0].GlobalJSON != "src/global.json" || r.Selections[0].State != "unresolved" || r.Selections[0].SDKVersion != "" {
		t.Fatalf("selection: %+v", r.Selections[0])
	}
}

func TestNormalizationDoesNotReadManifestOrCrossProjectConflict(t *testing.T) {
	reads := 0
	in := envInput(nil, nil)
	in.ReadSelected = func(context.Context, string, int64) ([]byte, int64, error) {
		reads++
		return nil, 0, errors.New("unexpected")
	}
	in.ProjectRecords = []declarations.ProjectRecord{
		{Project: declarations.Project{ID: "a/go.mod", Root: "a", Requirements: []declarations.Requirement{{Kind: "go-language-minimum", Value: "1.22", State: "declared", Evidence: "a/go.mod"}}}, Parsed: true, Complete: true},
		{Project: declarations.Project{ID: "b/pyproject.toml", Root: "b", Requirements: []declarations.Requirement{{Kind: "python-requires-python", Value: ">=3.12", State: "declared", Evidence: "b/pyproject.toml"}}}, Parsed: true, Complete: true},
	}
	r, err := Analyze(context.Background(), in, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	if reads != 0 || len(r.Requirements) != 2 || len(r.Conflicts) != 0 {
		t.Fatalf("reads=%d report=%+v", reads, r)
	}
}

func TestPythonIntersectionSameContextAndUnsupportedConstraint(t *testing.T) {
	in := envInput(nil, nil)
	in.ProjectRecords = []declarations.ProjectRecord{{Project: declarations.Project{ID: "pyproject.toml", Root: ".", Requirements: []declarations.Requirement{
		{Kind: "python-requires-python", Value: ">=3.12", State: "declared", Evidence: "pyproject.toml"},
		{Kind: "python-requires-python", Value: "<3.11", State: "declared", Evidence: "nested/pyproject.toml"},
		{Kind: "python-requires-python", Value: "~=3.12", State: "declared", Evidence: "unsupported.toml"},
	}}, Parsed: true, Complete: true}}
	r, err := Analyze(context.Background(), in, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Conflicts) != 1 || r.Conflicts[0].ContextID != "pyproject.toml" {
		t.Fatalf("conflicts: %+v", r.Conflicts)
	}
	found := false
	for _, q := range r.Requirements {
		if q.Value == "~=3.12" && q.State == "unresolved" {
			found = true
		}
	}
	if !found {
		t.Fatalf("unsupported constraint was not visible: %+v", r.Requirements)
	}
}

func TestPythonNumericVersionOverflowIsUnresolved(t *testing.T) {
	in := envInput(nil, nil)
	in.ProjectRecords = []declarations.ProjectRecord{{Project: declarations.Project{ID: "pyproject.toml", Root: ".", Requirements: []declarations.Requirement{
		{Kind: "python-requires-python", Value: ">=18446744073709551616", State: "declared", Evidence: "pyproject.toml"},
		{Kind: "python-requires-python", Value: "<1", State: "declared", Evidence: "pyproject.toml"},
	}}, Parsed: true, Complete: true}}
	r, err := Analyze(context.Background(), in, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, q := range r.Requirements {
		if q.Value == ">=18446744073709551616" && q.State == "unresolved" {
			found = true
		}
	}
	if r.Status != "partial" || len(r.Conflicts) != 0 || !found {
		t.Fatalf("overflow created a constraint: %+v", r)
	}
}

func TestBudgetCancellationAndDeterminism(t *testing.T) {
	in := envInput(map[string]string{"z/global.json": `{}`, "a/global.json": `{}`}, []Invocation{{"z", "z"}, {"a", "a"}})
	r1, err := Analyze(context.Background(), in, Limits{InventoryPaths: 1})
	if err != nil {
		t.Fatal(err)
	}
	in.Inventory[0], in.Inventory[1] = in.Inventory[1], in.Inventory[0]
	r2, err := Analyze(context.Background(), in, Limits{InventoryPaths: 1})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(r1, r2) {
		t.Fatalf("nondeterministic:\n%+v\n%+v", r1, r2)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Analyze(ctx, in, Limits{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel err=%v", err)
	}
}

// TestJSONCLeniencyIsDisclosedAndDoesNotDegradeStatus covers r100/03 C1:
// a global.json accepted through the JSONC path (leading `//` comment, block
// comment, trailing comma, or BOM plus comment) must parse to the declared
// SDK, keep environments.status "complete", and emit a documented
// informational diagnostic explaining the leniency instead of leaving the
// consumer to guess why coverage was reported as partial.
func TestJSONCLeniencyIsDisclosedAndDoesNotDegradeStatus(t *testing.T) {
	tests := []struct{ name, body string }{
		{"line comment", "// pinned\n{\"sdk\":{\"version\":\"7.0.100\"}}"},
		{"block comment", "{/* pinned */\"sdk\":{\"version\":\"7.0.100\"}}"},
		{"trailing comma", `{"sdk":{"version":"7.0.100",}}`},
		{"bom and line comment", "\xef\xbb\xbf// pinned\n{\"sdk\":{\"version\":\"7.0.100\"}}"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := envInput(map[string]string{"global.json": tt.body}, []Invocation{{"p", "."}})
			r, err := Analyze(context.Background(), in, Limits{})
			if err != nil {
				t.Fatal(err)
			}
			if r.Status != "complete" {
				t.Fatalf("status=%q body=%q report=%+v", r.Status, tt.body, r)
			}
			if len(r.Selections) != 1 || r.Selections[0].SDKVersion != "7.0.100" || r.Selections[0].State != "declared" {
				t.Fatalf("selection: %+v", r.Selections)
			}
			var found bool
			for _, d := range r.Diagnostics {
				if d.Code == "global-json-lenient-syntax" && d.Path == "global.json" {
					found = true
				}
			}
			if !found {
				t.Fatalf("missing lenient-syntax disclosure: %+v", r.Diagnostics)
			}
		})
	}
}

// TestDeclarationsPartialSurfacesAsBoundaryNotStatus covers r100/03 C1: when
// the upstream declarations pass is partial (for reasons that the environments
// module's independent parser can accept, e.g., JSONC), the environments
// module must report its own coverage rather than mirror the upstream label.
// The partial state is surfaced as an informational boundary so the consumer
// can still see it.
func TestDeclarationsPartialSurfacesAsBoundaryNotStatus(t *testing.T) {
	in := envInput(map[string]string{"global.json": `{"sdk":{"version":"7.0.100"}}`}, []Invocation{{"p", "."}})
	in.Declarations = declarations.Report{Status: "partial", Diagnostics: []declarations.Diagnostic{{Path: "global.json", Code: "invalid-json", Message: "upstream parser rejected"}}}
	r, err := Analyze(context.Background(), in, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	if r.Status != "complete" {
		t.Fatalf("status: %+v", r)
	}
	var found bool
	for _, b := range r.Boundaries {
		if b.Reason == "declarations-partial" {
			found = true
		}
	}
	if !found {
		t.Fatalf("no declarations-partial boundary: %+v", r.Boundaries)
	}
}

// TestNonDotnetGlobalJSONDoesNotCreateContext covers r100/10 F6: a
// global.json outside a modeled .NET project (Jekyll `_data/global.json`,
// Hugo, or any other JSON data file that shares the name) must not attract
// an environments-side selection or diagnostic. Environments only considers
// global.json files that projects successfully parsed as a .NET SDK config,
// or that sit under an ancestor of a parsed dotnet/solution project.
func TestNonDotnetGlobalJSONDoesNotCreateContext(t *testing.T) {
	in := envInput(map[string]string{"_data/global.json": `[{"key":"val"},]`}, nil)
	// Simulate what pkg/projects does with a filename-classified global.json
	// whose contents are not a .NET SDK document: retained as a
	// dotnet-configuration record with Parsed=false.
	in.ProjectRecords = []declarations.ProjectRecord{{Project: declarations.Project{ID: "_data/global.json", Root: "_data", Kind: "dotnet-configuration"}, Parsed: false, Complete: false}}
	r, err := Analyze(context.Background(), in, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Selections) != 0 {
		t.Fatalf("non-.NET global.json created a modeled context: %+v", r.Selections)
	}
	for _, d := range r.Diagnostics {
		if strings.Contains(d.Path, "global.json") {
			t.Fatalf("non-.NET global.json attracted a diagnostic: %+v", d)
		}
	}
}

// TestGlobalJSONReadErrorContinuePolicy covers r100/10 F1: an unreadable
// selected global.json under --on-error continue must degrade to a
// per-path diagnostic and unresolved selection state without aborting
// the aggregate. The default fail policy still returns a fixed error
// without leaking the caller's underlying I/O message.
func TestGlobalJSONReadErrorContinuePolicy(t *testing.T) {
	in := envInput(map[string]string{"global.json": `{"sdk":{"version":"8.0.100"}}`}, []Invocation{{"p", "."}})
	in.ReadSelected = func(context.Context, string, int64) ([]byte, int64, error) {
		return nil, 0, errors.New("secret-source-error")
	}
	if _, err := Analyze(context.Background(), in, Limits{}); err == nil {
		t.Fatal("default policy no longer fails on read error")
	} else if strings.Contains(err.Error(), "secret") {
		t.Fatal("caller I/O error leaked")
	}
	in.ErrorPolicy = "continue"
	r, err := Analyze(context.Background(), in, Limits{})
	if err != nil {
		t.Fatalf("continue policy returned error: %v", err)
	}
	if r.Status != "partial" || len(r.Selections) != 1 || r.Selections[0].State != "unresolved" {
		t.Fatalf("selection not unresolved: %+v", r)
	}
	var found bool
	for _, d := range r.Diagnostics {
		if d.Code == "file-read-error" && d.Path == "global.json" {
			found = true
		}
	}
	if !found {
		t.Fatalf("no file-read-error diagnostic: %+v", r.Diagnostics)
	}
}

func TestGlobalJSONContinueDoesNotSwallowReaderCancellation(t *testing.T) {
	for _, want := range []error{context.Canceled, context.DeadlineExceeded} {
		in := envInput(map[string]string{"global.json": `{"sdk":{"version":"8.0.100"}}`}, []Invocation{{"p", "."}})
		in.ErrorPolicy = "continue"
		in.ReadSelected = func(context.Context, string, int64) ([]byte, int64, error) {
			return nil, 0, fmt.Errorf("selected reader stopped: %w", want)
		}
		if _, err := Analyze(context.Background(), in, Limits{}); !errors.Is(err, want) {
			t.Fatalf("continue swallowed %v: %v", want, err)
		}
	}
}

func TestGlobalJSONReadErrorIsCachedAcrossContexts(t *testing.T) {
	contents := `{"sdk":{"version":"8.0.100"}}`
	in := envInput(map[string]string{"global.json": contents}, []Invocation{{"a", "src/a"}, {"b", "src/b"}})
	in.ErrorPolicy = "continue"
	reads := 0
	in.ReadSelected = func(context.Context, string, int64) ([]byte, int64, error) {
		reads++
		if reads == 1 {
			return nil, 0, errors.New("transient selected-source failure")
		}
		return []byte(contents), int64(len(contents)), nil
	}
	r, err := Analyze(context.Background(), in, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	if reads != 1 {
		t.Fatalf("selected global.json read %d times", reads)
	}
	if len(r.Selections) != 2 || r.Selections[0].State != "unresolved" || r.Selections[1].State != "unresolved" {
		t.Fatalf("contexts observed inconsistent source states: %+v", r.Selections)
	}
	if len(r.Diagnostics) != 1 || r.Diagnostics[0].Code != "file-read-error" {
		t.Fatalf("read failure diagnostics not deduplicated: %+v", r.Diagnostics)
	}
	if r.Coverage.OmittedFiles != 1 || r.Coverage.GlobalJSONCandidates != 2 || r.Coverage.GlobalJSONRead != 0 {
		t.Fatalf("read omission coverage: %+v", r.Coverage)
	}
}
