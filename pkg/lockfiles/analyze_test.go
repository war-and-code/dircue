package lockfiles

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/war-and-code/dircue/pkg/declarations"
)

func npmRecord(root string, refs ...declarations.Reference) declarations.ProjectRecord {
	return declarations.ProjectRecord{Parsed: true, Complete: true, Project: declarations.Project{ID: join(root, "package.json"), Root: root, Kind: "npm", References: refs}}
}

func nugetRecord(root, id string, requirements ...declarations.Requirement) declarations.ProjectRecord {
	return declarations.ProjectRecord{Parsed: true, Complete: true, Project: declarations.Project{ID: id, Root: root, Kind: "dotnet", Requirements: requirements}}
}

func npmRef(value, section string) declarations.Reference {
	return declarations.Reference{Kind: "npm-dependency", Value: value, State: "declared", Condition: section}
}

func testInput(records []declarations.ProjectRecord, content map[string]string, complete bool) Input {
	if content == nil {
		content = map[string]string{}
	}
	for _, record := range records {
		if record.Project.Kind == "dotnet" && strings.HasSuffix(strings.ToLower(record.Project.ID), ".csproj") {
			if _, ok := content[record.Project.ID]; !ok {
				content[record.Project.ID] = "<Project />"
			}
		}
	}
	in := Input{Source: "directory", InventoryComplete: complete, ProjectRecords: records, Inventory: []File{}}
	for p, body := range content {
		in.Inventory = append(in.Inventory, File{Path: p, Size: int64(len(body))})
	}
	in.ReadSelected = func(_ context.Context, p string, limit int64) ([]byte, int64, error) {
		b, ok := content[p]
		if !ok {
			return nil, 0, errors.New("not selected")
		}
		if int64(len(b)) > limit {
			return nil, int64(len(b)), errors.New("over limit")
		}
		return []byte(b), int64(len(b)), nil
	}
	return in
}

func TestNPMDirectTablesMatchAndDifferenceAreNamedSyntacticChecks(t *testing.T) {
	// The npm v2/v3 `packages[""]` table is the root package descriptor;
	// dependencies are literal declarations, not a lockfile solver result.
	// Format reference: https://docs.npmjs.com/files/package-lock.json/
	manifest := npmRecord("web", npmRef("react@^18.2.0", "dependencies"), npmRef("jest@^29.0.0", "devDependencies"))
	lock := `{"lockfileVersion":3,"packages":{"":{"dependencies":{"react":"^18.2.0"},"devDependencies":{"jest":"^29.0.0"}},"node_modules/react":{"version":"18.3.0"}}}`
	r, err := Analyze(context.Background(), testInput([]declarations.ProjectRecord{manifest}, map[string]string{"web/package-lock.json": lock}, true), Limits{})
	if err != nil {
		t.Fatal(err)
	}
	got := r.Contexts[0]
	if got.AssociationState != "observed" || len(got.Checks) != 1 || got.Checks[0].Status != "match" || got.Checks[0].Compared != 2 {
		t.Fatalf("match report: %+v", got)
	}

	lock = `{"lockfileVersion":2,"packages":{"":{"dependencies":{"react":"^17.0.0"},"devDependencies":{"jest":"^29.0.0"}}}}`
	r, err = Analyze(context.Background(), testInput([]declarations.ProjectRecord{manifest}, map[string]string{"web/package-lock.json": lock}, true), Limits{})
	if err != nil {
		t.Fatal(err)
	}
	check := r.Contexts[0].Checks[0]
	if check.Status != "different" || len(check.Mismatched) != 1 || check.Mismatched[0] != "react" {
		t.Fatalf("text difference: %+v", check)
	}
}

func TestNPMShrinkwrapTakesPrecedenceOverPackageLock(t *testing.T) {
	record := npmRecord("app", npmRef("a@1.0.0", "dependencies"))
	packageLock := `{"lockfileVersion":3,"packages":{"":{"dependencies":{"a":"2.0.0"}}}}`
	shrinkwrap := `{"lockfileVersion":3,"packages":{"":{"dependencies":{"a":"1.0.0"}}}}`
	in := testInput([]declarations.ProjectRecord{record}, map[string]string{
		"app/package-lock.json":   packageLock,
		"app/npm-shrinkwrap.json": shrinkwrap,
	}, true)
	r, err := Analyze(context.Background(), in, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Contexts) != 1 || r.Contexts[0].LockfilePath != "app/npm-shrinkwrap.json" || r.Contexts[0].Checks[0].Status != "match" {
		t.Fatalf("npm shrinkwrap did not take documented precedence: %+v", r.Contexts)
	}
}

func TestNPMManifestDependencyDiagnosticsPreventFalseMatch(t *testing.T) {
	manifest := `{"name":"app","dependencies":{"a":"1.0.0","broken":false}}`
	collector := declarations.New("directory", "", 0)
	collector.EnableProjectRecords()
	collector.Add("app/package.json", &declarations.Candidate{
		Path: "app/package.json", Size: int64(len(manifest)),
		Read: func(context.Context, int64) ([]byte, int64, error) {
			return []byte(manifest), int64(len(manifest)), nil
		},
	})
	declarationReport, err := collector.Finish(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	records := collector.ProjectRecords()
	if len(records) != 1 || records[0].Project.References == nil || len(declarationReport.Diagnostics) == 0 {
		t.Fatalf("fixture did not produce a valid declaration plus a bad entry: records=%+v diagnostics=%+v", records, declarationReport.Diagnostics)
	}
	lock := `{"lockfileVersion":3,"packages":{"":{"dependencies":{"a":"1.0.0"}}}}`
	in := testInput(records, map[string]string{"app/package-lock.json": lock}, true)
	in.Declarations = *declarationReport
	r, err := Analyze(context.Background(), in, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	check := r.Contexts[0].Checks[0]
	if check.Status != "indeterminate" || r.Status != "partial" || len(r.Contexts[0].Boundaries) == 0 || r.Contexts[0].Boundaries[0].Reason != "npm-manifest-declarations-unresolved" {
		t.Fatalf("invalid/unobserved manifest fields still yielded a conclusive match: %+v", r.Contexts[0])
	}
	unrelated := in
	unrelated.Declarations.Diagnostics = []declarations.Diagnostic{{Path: "other/package.json", Code: "invalid-npm-dependency", Message: "unrelated fixture"}}
	r, err = Analyze(context.Background(), unrelated, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	if got := r.Contexts[0].Checks[0].Status; got != "match" || r.Status != "partial" {
		t.Fatalf("unrelated manifest diagnostics poisoned a valid independent check: report=%+v", r)
	}
}

func TestNPMDeclarationSplitPreservesScopedNamesAndRejectsAliasSpecs(t *testing.T) {
	record := npmRecord("app",
		npmRef("plain@^1.2.3", "dependencies"),
		npmRef("@scope/widget@~4.5", "dependencies"),
		npmRef("alias@npm:@scope/real-widget@^8", "dependencies"),
		npmRef("gitpkg@git+ssh://git@github.com/org/repo#refs/tags/v1", "dependencies"),
	)
	lock := parseLock("npm", []byte(`{"lockfileVersion":3,"packages":{"":{"dependencies":{"plain":"^1.2.3","@scope/widget":"~4.5"}}}}`))
	check, count, reason := compareNPM(record, lock, DefaultMaxPackageNames)
	if reason != "" || count != 2 || check.Compared != 2 || check.Status != "indeterminate" || len(check.Missing)+len(check.Mismatched)+len(check.Unexpected) != 0 {
		t.Fatalf("unsupported alias/URL specs became package-name comparisons: check=%+v count=%d reason=%q", check, count, reason)
	}
	for _, tc := range []struct {
		input string
		name  string
		spec  string
	}{
		{"plain@^1", "plain", "^1"},
		{"@scope/pkg@~2", "@scope/pkg", "~2"},
	} {
		name, spec, ok := splitNPMDeclaration(tc.input)
		if !ok || name != tc.name || spec != tc.spec {
			t.Fatalf("split %q = %q %q %v", tc.input, name, spec, ok)
		}
	}
}

func TestNPMComparisonBudgetHasDeterministicPartialLists(t *testing.T) {
	record := npmRecord("app", npmRef("b@1", "dependencies"), npmRef("a@1", "dependencies"))
	lock := parseLock("npm", []byte(`{"lockfileVersion":3,"packages":{"":{"dependencies":{"a":"2","b":"2","c":"1"}}}}`))
	var first []byte
	for i := 0; i < 50; i++ {
		check, count, reason := compareNPM(record, lock, 2)
		if reason != "package-name-limit" || count != 2 || check.Compared != 2 || !reflect.DeepEqual(check.Mismatched, []string{"a", "b"}) {
			t.Fatalf("unexpected budget result: %+v count=%d reason=%q", check, count, reason)
		}
		encoded, _ := json.Marshal(check)
		if i > 0 && !bytes.Equal(first, encoded) {
			t.Fatalf("map iteration changed partial report: %s vs %s", first, encoded)
		}
		first = encoded
	}
}

func TestProducedReportRemainsValidForAdversarialNPMDependencyNames(t *testing.T) {
	record := npmRecord("app", npmRef("a@1.0.0", "dependencies"))
	for _, name := range []string{strings.Repeat("x", 257), "bad\x01name"} {
		lock, err := json.Marshal(map[string]any{
			"lockfileVersion": 3,
			"packages":        map[string]any{"": map[string]any{"dependencies": map[string]string{name: "1.0.0"}}},
		})
		if err != nil {
			t.Fatal(err)
		}
		in := testInput([]declarations.ProjectRecord{record}, map[string]string{"app/package-lock.json": string(lock)}, true)
		r, err := Analyze(context.Background(), in, Limits{})
		if err != nil {
			t.Fatal(err)
		}
		if err := ValidateReport(r); err != nil {
			t.Fatalf("analysis emitted an invalid report for dependency name %q: %v; report=%+v", name, err, r)
		}
		if r.Contexts[0].AssociationState != "unsupported" || len(r.Contexts[0].Checks) != 1 || r.Contexts[0].Checks[0].Status != "indeterminate" || len(r.Contexts[0].Checks[0].Unexpected) != 0 {
			t.Fatalf("unsafe dependency name escaped as a discrepancy: %+v", r.Contexts[0])
		}
	}
}

func TestNPMUnsupportedMissingAndUnprovenWorkspaceStayDistinct(t *testing.T) {
	record := npmRecord("app", npmRef("leftpad@1.0.0", "dependencies"))
	for _, tc := range []struct {
		name        string
		content     map[string]string
		complete    bool
		association string
	}{
		{"missing", map[string]string{}, true, "missing"},
		{"incomplete-inventory", map[string]string{}, false, "indeterminate"},
		{"unsupported-version", map[string]string{"app/package-lock.json": `{"lockfileVersion":1,"dependencies":{}}`}, true, "unsupported"},
		{"malformed-lock", map[string]string{"app/package-lock.json": `{"lockfileVersion":3,"packages":{"":{"dependencies":{"leftpad":"1.0.0"}}`}, true, "unsupported"},
		{"candidate-in-incomplete-inventory", map[string]string{"app/package-lock.json": `{"lockfileVersion":3,"packages":{"":{"dependencies":{"leftpad":"1.0.0"}}}}`}, false, "indeterminate"},
		{"ancestor-workspace-lock", map[string]string{"package.json": `{"name":"root","workspaces":["app"]}`, "package-lock.json": `{"lockfileVersion":3,"packages":{"":{"workspaces":["app"]},"app":{"dependencies":{"leftpad":"1.0.0"}}}}`}, true, "indeterminate"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, err := Analyze(context.Background(), testInput([]declarations.ProjectRecord{record}, tc.content, tc.complete), Limits{})
			if err != nil {
				t.Fatal(err)
			}
			if r.Contexts[0].AssociationState != tc.association {
				t.Fatalf("association=%q want %q, report=%+v", r.Contexts[0].AssociationState, tc.association, r.Contexts[0])
			}
		})
	}
}

func TestNuGetV1AndV2DirectPresenceAndMultiTargetUncertainty(t *testing.T) {
	// NuGet v2 retains target dependency maps and Direct entries while adding
	// Project/CentralTransitive entry kinds. Official docs describe the lock
	// file as versioned; NuGet/Home shows v2's additional kinds:
	// https://learn.microsoft.com/en-us/nuget/consume-packages/package-references-in-project-files
	// https://github.com/NuGet/Home/issues/14102
	record := nugetRecord("src/App", "src/App/App.csproj", declarations.Requirement{Kind: "package-reference", Value: "Newtonsoft.Json@[13.0.0,14.0.0)", State: "declared"})
	for _, version := range []int{1, 2} {
		lock := `{"version":` + itoa(version) + `,"dependencies":{"net8.0":{"Newtonsoft.Json":{"type":"Direct","requested":"[13.0.0,14.0.0)","resolved":"13.0.1","contentHash":"hash"},"Other":{"type":"Transitive","resolved":"1.0.0"}}}}`
		r, err := Analyze(context.Background(), testInput([]declarations.ProjectRecord{record}, map[string]string{"src/App/packages.lock.json": lock}, true), Limits{})
		if err != nil {
			t.Fatal(err)
		}
		c := r.Contexts[0]
		if c.AssociationState != "observed" || c.LockfileVersion != itoa(version) || len(c.Checks) != 1 || c.Checks[0].Status != "match" {
			t.Fatalf("NuGet v%d: %+v", version, c)
		}
	}
	lockWithExtendedEntry := `{"version":2,"dependencies":{"net8.0":{"Newtonsoft.Json":{"type":"Direct"},"ProjectRef":{"type":"Project"},"Pinned":{"type":"CentralTransitive"}}}}`
	r, err := Analyze(context.Background(), testInput([]declarations.ProjectRecord{record}, map[string]string{"src/App/packages.lock.json": lockWithExtendedEntry}, true), Limits{})
	if err != nil {
		t.Fatal(err)
	}
	if r.Status != "partial" || len(r.Contexts[0].Boundaries) == 0 || r.Contexts[0].Checks[0].Status != "match" {
		t.Fatalf("v2 extended-kind boundary: %+v", r.Contexts[0])
	}
	lock := `{"version":1,"dependencies":{"net8.0":{"Newtonsoft.Json":{"type":"Direct"},"Other":{"type":"Transitive"}},"net9.0":{"Newtonsoft.Json":{"type":"Direct"}}}}`
	r, err = Analyze(context.Background(), testInput([]declarations.ProjectRecord{record}, map[string]string{"src/App/packages.lock.json": lock}, true), Limits{})
	if err != nil {
		t.Fatal(err)
	}
	if got := r.Contexts[0].Checks[0].Status; got != "indeterminate" {
		t.Fatalf("multi-target status=%q; want indeterminate", got)
	}
}

func TestNuGetMissingConditionalAndAmbiguousSharedLockAreNotMismatchClaims(t *testing.T) {
	conditional := nugetRecord("src/App", "src/App/App.csproj", declarations.Requirement{Kind: "package-reference", Value: "A@1.0", State: "conditional", Condition: "Condition expression"})
	lock := `{"version":1,"dependencies":{"net8.0":{}}}`
	r, err := Analyze(context.Background(), testInput([]declarations.ProjectRecord{conditional}, map[string]string{"src/App/packages.lock.json": lock}, true), Limits{})
	if err != nil {
		t.Fatal(err)
	}
	if got := r.Contexts[0].Checks[0].Status; got != "indeterminate" {
		t.Fatalf("conditional status=%q", got)
	}
	first := nugetRecord("src/App", "src/App/A.csproj", declarations.Requirement{Kind: "package-reference", Value: "A@1.0", State: "declared"})
	second := nugetRecord("src/App", "src/App/B.csproj", declarations.Requirement{Kind: "package-reference", Value: "B@1.0", State: "declared"})
	r, err = Analyze(context.Background(), testInput([]declarations.ProjectRecord{first, second}, map[string]string{"src/App/packages.lock.json": lock}, true), Limits{})
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range r.Contexts {
		if c.AssociationState != "indeterminate" || len(c.Checks) != 0 {
			t.Fatalf("shared lock ownership guessed: %+v", c)
		}
	}
}

func TestNuGetProjectSpecificLocksRequireUniqueFilenameOwnership(t *testing.T) {
	first := nugetRecord("src/App", "src/App/Api.csproj", declarations.Requirement{Kind: "package-reference", Value: "A@1.0", State: "declared"})
	second := nugetRecord("src/App", "src/App/Worker.csproj", declarations.Requirement{Kind: "package-reference", Value: "B@1.0", State: "declared"})
	lock := `{"version":1,"dependencies":{"net8.0":{"A":{"type":"Direct"}}}}`
	in := testInput([]declarations.ProjectRecord{first, second}, map[string]string{"src/App/packages.Api.lock.json": lock, "src/App/packages.Worker.lock.json": lock}, true)
	r, err := Analyze(context.Background(), in, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range r.Contexts {
		if c.AssociationState != "observed" {
			t.Fatalf("unique project-specific lock was not associated: %+v", c)
		}
	}

	in = testInput([]declarations.ProjectRecord{first}, map[string]string{"src/App/packages.Custom.lock.json": lock}, true)
	r, err = Analyze(context.Background(), in, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	if r.Contexts[0].AssociationState != "indeterminate" {
		t.Fatalf("custom lock path guessed: %+v", r.Contexts[0])
	}
}

func TestNuGetGenericLockOwnerCountsAllMSBuildProjectTypes(t *testing.T) {
	contents := map[string]string{
		"src/App/App.csproj":    `<Project><ItemGroup><PackageReference Include="A" Version="1.0" /></ItemGroup></Project>`,
		"src/App/Worker.fsproj": `<Project><ItemGroup><PackageReference Include="B" Version="1.0" /></ItemGroup></Project>`,
	}
	collector := declarations.New("directory", "", 0)
	collector.EnableProjectRecords()
	for name, content := range contents {
		content := content
		collector.Add(name, &declarations.Candidate{
			Path: name, Size: int64(len(content)),
			Read: func(context.Context, int64) ([]byte, int64, error) {
				return []byte(content), int64(len(content)), nil
			},
		})
	}
	collector.Add("src/App/packages.lock.json", nil)
	report, err := collector.Finish(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	records := collector.ProjectRecords()
	if len(records) != 2 {
		t.Fatalf("fixture did not discover both C# and F# projects: %+v", records)
	}
	contents["src/App/packages.lock.json"] = `{"version":1,"dependencies":{"net8.0":{"A":{"type":"Direct"},"B":{"type":"Direct"}}}}`
	in := testInput(records, contents, true)
	in.Declarations = *report
	r, err := Analyze(context.Background(), in, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Contexts) != 1 || r.Contexts[0].AssociationState != "indeterminate" || r.Contexts[0].Boundaries[0].Reason != "ambiguous-nuget-lockfile-owner" {
		t.Fatalf("generic NuGet lock was assigned despite a same-directory F# owner: %+v", r.Contexts)
	}
}

func TestNuGetCustomLockPathIsUnresolvedFromSelectedProjectXML(t *testing.T) {
	record := nugetRecord("src/App", "src/App/App.csproj", declarations.Requirement{Kind: "package-reference", Value: "A@1.0", State: "declared"})
	lock := `{"version":1,"dependencies":{"net8.0":{"A":{"type":"Direct"}}}}`
	for _, tc := range []struct {
		name    string
		project string
		want    string
		reason  string
	}{
		{"custom property", `<Project><PropertyGroup><NuGetLockFilePath>elsewhere.lock.json</NuGetLockFilePath></PropertyGroup></Project>`, "indeterminate", "nuget-custom-lock-path-unresolved"},
		{"comment does not count", `<Project><!-- <NuGetLockFilePath>elsewhere.lock.json</NuGetLockFilePath> --></Project>`, "observed", ""},
		{"malformed XML unresolved", `<Project><NuGetLockFilePath>elsewhere.lock.json</Project>`, "indeterminate", "nuget-custom-lock-path-unresolved"},
		{"malformed before property unresolved", `<Project><PropertyGroup></Project>`, "indeterminate", "nuget-project-config-unresolved"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			in := testInput([]declarations.ProjectRecord{record}, map[string]string{
				"src/App/App.csproj":         tc.project,
				"src/App/packages.lock.json": lock,
			}, true)
			r, err := Analyze(context.Background(), in, Limits{})
			if err != nil {
				t.Fatal(err)
			}
			if got := r.Contexts[0].AssociationState; got != tc.want {
				t.Fatalf("association=%q want %q: %+v", got, tc.want, r.Contexts[0])
			}
			if tc.want == "indeterminate" && (len(r.Contexts[0].Checks) != 0 || r.Contexts[0].Boundaries[0].Reason != tc.reason) {
				t.Fatalf("custom or malformed project config was treated as comparable: %+v", r.Contexts[0])
			}
		})
	}
}

func TestNuGetSharedProjectInputsKeepLockfileClaimIndeterminate(t *testing.T) {
	record := nugetRecord("src/App", "src/App/App.csproj", declarations.Requirement{Kind: "package-reference", Value: "A@1.0", State: "declared"})
	lock := `{"version":1,"dependencies":{"net8.0":{"A":{"type":"Direct"}}}}`
	in := testInput([]declarations.ProjectRecord{record}, map[string]string{
		"Directory.Packages.props":   "<Project />",
		"src/App/packages.lock.json": lock,
	}, true)
	r, err := Analyze(context.Background(), in, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	c := r.Contexts[0]
	if c.AssociationState != "observed" || c.Checks[0].Status != "indeterminate" || r.Status != "partial" {
		t.Fatalf("shared input not disclosed: %+v", c)
	}

	in = testInput([]declarations.ProjectRecord{record}, map[string]string{"Directory.Build.props": "<Project />"}, true)
	r, err = Analyze(context.Background(), in, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	if r.Contexts[0].AssociationState != "indeterminate" {
		t.Fatalf("shared config custom lock path not disclosed: %+v", r.Contexts[0])
	}
}

func TestReadFailureCanContinueButCancellationIsFatal(t *testing.T) {
	rec := npmRecord("app", npmRef("a@1.0.0", "dependencies"))
	in := testInput([]declarations.ProjectRecord{rec}, map[string]string{"app/package-lock.json": `{"lockfileVersion":3,"packages":{"":{"dependencies":{"a":"1.0.0"}}}}`}, true)
	in.ReadSelected = func(context.Context, string, int64) ([]byte, int64, error) { return nil, 0, errors.New("read failure") }
	in.ErrorPolicy = "continue"
	r, err := Analyze(context.Background(), in, Limits{})
	if err != nil || r.Status != "partial" || len(r.Diagnostics) != 1 || r.Contexts[0].Checks[0].Status != "indeterminate" {
		t.Fatalf("continue report=%+v err=%v", r, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Analyze(ctx, in, Limits{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel error=%v", err)
	}
}

func TestBoundedAdmissionAndInputBytesAreDisclosed(t *testing.T) {
	record := npmRecord("app", npmRef("a@1.0.0", "dependencies"))
	lock := `{"lockfileVersion":3,"packages":{"":{"dependencies":{"a":"1.0.0"}}}}`
	in := testInput([]declarations.ProjectRecord{record}, map[string]string{"app/package-lock.json": lock}, true)
	r, err := Analyze(context.Background(), in, Limits{FileBytes: 8})
	if err != nil {
		t.Fatal(err)
	}
	if r.Status != "partial" || r.Contexts[0].Checks[0].Status != "indeterminate" {
		t.Fatalf("file limit not disclosed: %+v", r)
	}
	r, err = Analyze(context.Background(), in, Limits{Lockfiles: 1})
	if err != nil || r.Coverage.LockCandidates != 1 || r.Coverage.LockfilesRead != 1 {
		t.Fatalf("bounded report=%+v err=%v", r, err)
	}
}

func TestUnconfinedCallerPathsAreOmittedWithoutEchoingThem(t *testing.T) {
	good := npmRecord("app", npmRef("a@1.0.0", "dependencies"))
	badID := npmRecord("/Users/private/work/app", npmRef("a@1.0.0", "dependencies"))
	badRoot := npmRecord("../private", npmRef("a@1.0.0", "dependencies"))
	in := testInput([]declarations.ProjectRecord{good, badID, badRoot}, map[string]string{"app/package-lock.json": `{"lockfileVersion":3,"packages":{"":{"dependencies":{"a":"1.0.0"}}}}`}, true)
	in.Inventory = append(in.Inventory, File{Path: "/Users/private/work/secret.json", Size: 1})
	r, err := Analyze(context.Background(), in, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	if r.Status != "partial" || r.Coverage.OmittedContexts != 2 || len(r.Contexts) != 1 {
		t.Fatalf("invalid paths were not omitted: %+v", r)
	}
	b, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "/Users/private") || strings.Contains(string(b), "../private") {
		t.Fatalf("report echoed an unconfined input path: %s", b)
	}
}

func join(a, b string) string {
	if a == "" || a == "." {
		return b
	}
	return a + "/" + b
}

func itoa(v int) string {
	if v == 1 {
		return "1"
	}
	if v == 2 {
		return "2"
	}
	return "0"
}
