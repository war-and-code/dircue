package lockfiles

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"reflect"
	"slices"
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

// nugetProjectXML writes the SDK-style project file that the static NuGet
// evaluator reads for a test record: one PackageReference per declared
// package-reference requirement, conditional requirements under a condition.
// pkgA is a PackageReference item group for package A.
const pkgA = `<ItemGroup><PackageReference Include="A" Version="1.0" /></ItemGroup>`

func nugetProjectXML(record declarations.ProjectRecord) string {
	var b strings.Builder
	b.WriteString(`<Project Sdk="Microsoft.NET.Sdk"><ItemGroup>`)
	for _, req := range record.Project.Requirements {
		if req.Kind != "package-reference" {
			continue
		}
		id, version, _ := strings.Cut(req.Value, "@")
		condition := ""
		if req.State == "conditional" {
			condition = ` Condition="'$(Configuration)' == 'Debug'"`
		}
		b.WriteString(`<PackageReference Include="` + id + `" Version="` + version + `"` + condition + ` />`)
	}
	b.WriteString(`</ItemGroup></Project>`)
	return b.String()
}

func testInput(records []declarations.ProjectRecord, content map[string]string, complete bool) Input {
	if content == nil {
		content = map[string]string{}
	}
	for _, record := range records {
		if record.Project.Kind == "dotnet" && IsNuGetLockProject(record.Project.ID) {
			if _, ok := content[record.Project.ID]; !ok {
				content[record.Project.ID] = nugetProjectXML(record)
			}
		}
	}
	in := Input{Source: "directory", InventoryComplete: complete, SelectedFilesComplete: complete, Declarations: declarations.Report{Status: "complete"}, ProjectRecords: records, Inventory: []File{}, SelectedFiles: []File{}}
	for p, body := range content {
		in.Inventory = append(in.Inventory, File{Path: p, Size: int64(len(body))})
		in.SelectedFiles = append(in.SelectedFiles, File{Path: p, Size: int64(len(body))})
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

func TestDuplicateProjectRecordsAreConservativeAndDoNotEmitDuplicateContexts(t *testing.T) {
	record := npmRecord("app", npmRef("a@1.0.0", "dependencies"))
	lock := `{"lockfileVersion":3,"packages":{"":{"dependencies":{"a":"1.0.0"}}}}`
	in := testInput([]declarations.ProjectRecord{record, record}, map[string]string{"app/package-lock.json": lock}, true)
	r, err := Analyze(context.Background(), in, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	if r.Status != "partial" || r.Coverage.ProjectRecords != 2 || r.Coverage.OmittedContexts != 1 || len(r.Contexts) != 1 || r.Contexts[0].AssociationState != "indeterminate" || len(r.Contexts[0].Checks) != 0 {
		t.Fatalf("duplicate project evidence was silently trusted or emitted twice: %+v", r)
	}
	if len(r.Diagnostics) != 1 || r.Diagnostics[0].Code != "duplicate-project-record" {
		t.Fatalf("duplicate evidence was not disclosed: %+v", r.Diagnostics)
	}
	if err := ValidateReport(r); err != nil {
		t.Fatalf("duplicate-record handling emitted an invalid report: %v", err)
	}
}

func TestAnalyzeRejectsNegativeOmissionCounts(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*Input)
	}{
		{"inventory", func(in *Input) { in.OmittedFiles = -1 }},
		{"declarations", func(in *Input) { in.Declarations.Coverage.OmittedFiles = -1 }},
		{"declaration diagnostics", func(in *Input) { in.Declarations.Coverage.OmittedDiagnostics = -1 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			in := Input{Source: "directory", InventoryComplete: true}
			tc.mutate(&in)
			if report, err := Analyze(context.Background(), in, Limits{}); err == nil {
				t.Fatalf("negative omission count produced a report: %+v", report)
			}
		})
	}
}

func TestAnalyzeRejectsLimitsAboveReportMaximums(t *testing.T) {
	for _, limits := range []Limits{
		{InventoryPaths: DefaultMaxInventoryPaths + 1},
		{Lockfiles: DefaultMaxLockfiles + 1},
		{FileBytes: DefaultMaxFileBytes + 1},
		{InputBytes: DefaultMaxInputBytes + 1},
		{PackageNames: DefaultMaxPackageNames + 1},
		{Contexts: DefaultMaxContexts + 1},
		{OutputBytes: DefaultMaxOutputBytes + 1},
	} {
		if report, err := Analyze(context.Background(), Input{Source: "directory"}, limits); err == nil {
			t.Fatalf("over-maximum limit produced an unverifiable report: %+v", report.Limits)
		}
	}
}

func TestAnalyzeRejectsInputAboveReportMaximums(t *testing.T) {
	for _, tc := range []struct {
		name  string
		input func() Input
	}{
		{"inventory", func() Input {
			return Input{Source: "directory", Inventory: make([]File, DefaultMaxInventoryPaths+1)}
		}},
		{"project records", func() Input {
			return Input{Source: "directory", ProjectRecords: make([]declarations.ProjectRecord, DefaultMaxInventoryPaths+1)}
		}},
		{"declaration projects", func() Input {
			return Input{Source: "directory", Declarations: declarations.Report{Projects: make([]declarations.Project, DefaultMaxInventoryPaths+1)}}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if report, err := Analyze(context.Background(), tc.input(), Limits{}); err == nil {
				t.Fatalf("over-maximum caller input produced a report: %+v", report.Coverage)
			}
		})
	}
}

func TestConflictingDuplicateInventoryMetadataKeepsAssociationIndeterminate(t *testing.T) {
	record := npmRecord("app", npmRef("a@1.0.0", "dependencies"))
	lock := `{"lockfileVersion":3,"packages":{"":{"dependencies":{"a":"1.0.0"}}}}`
	in := testInput([]declarations.ProjectRecord{record}, map[string]string{"app/package-lock.json": lock}, true)
	in.Inventory = append(in.Inventory, File{Path: "app/package-lock.json", Size: int64(len(lock)) + 1})
	r, err := Analyze(context.Background(), in, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	if r.Status != "partial" || r.Coverage.OmittedFiles == 0 || len(r.Diagnostics) != 1 || r.Diagnostics[0].Code != "conflicting-inventory-entry" ||
		len(r.Contexts) != 1 || r.Contexts[0].AssociationState != "indeterminate" || len(r.Contexts[0].Checks) != 0 {
		t.Fatalf("conflicting inventory metadata was treated as certain: %+v", r)
	}
	if err := ValidateReport(r); err != nil {
		t.Fatalf("qualified duplicate-inventory report was invalid: %v", err)
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
	if len(r.Contexts[0].Boundaries) != 1 || r.Contexts[0].Boundaries[0].Reason != "npm-v11-shrinkwrap-selection" || !strings.Contains(r.Contexts[0].Checks[0].Explanation, "npm v12 ignores") {
		t.Fatalf("npm shrinkwrap selection did not disclose its version scope: %+v", r.Contexts[0])
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

func TestOmittedDeclarationDiagnosticsPreventNPMMatch(t *testing.T) {
	record := npmRecord("app", npmRef("a@1.0.0", "dependencies"))
	lock := `{"lockfileVersion":3,"packages":{"":{"dependencies":{"a":"1.0.0"}}}}`
	in := testInput([]declarations.ProjectRecord{record}, map[string]string{"app/package-lock.json": lock}, true)
	in.Declarations.Coverage.OmittedDiagnostics = 1
	r, err := Analyze(context.Background(), in, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	if r.Status != "partial" || len(r.Contexts) != 1 || r.Contexts[0].Checks[0].Status != "indeterminate" || len(r.Contexts[0].Boundaries) != 1 || r.Contexts[0].Boundaries[0].Reason != "npm-manifest-declarations-unresolved" {
		t.Fatalf("omitted diagnostics still allowed a conclusive npm match: %+v", r)
	}
	if err := ValidateReport(r); err != nil {
		t.Fatalf("qualified omitted-diagnostic report was invalid: %v", err)
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
			if err := ValidateReport(r); err != nil {
				t.Fatalf("analysis emitted an invalid report: %v; report=%+v", err, r)
			}
		})
	}
}

func TestNPMWorkspaceSharedLockUsesExactMemberDescriptorAndReadsOnce(t *testing.T) {
	// Independent format oracle: npm CLI 11.19.0 generated the receipt at
	// .cache/assessment140/locks/oracle.json with `--package-lock-only
	// --ignore-scripts --offline`; npm's v11 docs say packages keys are
	// root-relative and `""` names the root package:
	// https://docs.npmjs.com/cli/v11/configuring-npm/package-lock-json/#packages
	root := npmRecord(".")
	root.Project.Requirements = []declarations.Requirement{{Kind: "npm-workspace-root", Value: "true", State: "declared", Evidence: "package.json"}}
	root.Project.References = []declarations.Reference{
		{Kind: "npm-workspace-member", Value: "packages/*", Target: "packages/app/package.json", State: "resolved", Evidence: "package.json"},
		{Kind: "npm-workspace-member", Value: "packages/*", Target: "packages/tool/package.json", State: "resolved", Evidence: "package.json"},
	}
	member := npmRecord("packages/app", npmRef("alpha@^1.0.0", "dependencies"))
	member2 := npmRecord("packages/tool", npmRef("beta@~2.0.0", "devDependencies"))
	lock := `{"lockfileVersion":3,"packages":{"":{"dependencies":{"root-only":"1"}},"packages/app":{"dependencies":{"alpha":"^1.0.0"}},"packages/tool":{"devDependencies":{"beta":"~2.0.0"}}}}`
	in := testInput([]declarations.ProjectRecord{root, member, member2}, map[string]string{"package-lock.json": lock}, true)
	in.WorkspaceLocks = true
	reads := map[string]int{}
	read := in.ReadSelected
	in.ReadSelected = func(ctx context.Context, p string, max int64) ([]byte, int64, error) {
		reads[p]++
		return read(ctx, p, max)
	}
	r, err := Analyze(context.Background(), in, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	if reads["package-lock.json"] != 1 || r.Coverage.LockfilesRead != 1 {
		t.Fatalf("shared lock was not read/cached once: reads=%v coverage=%+v", reads, r.Coverage)
	}
	for manifest, expected := range map[string]string{"packages/app/package.json": "match", "packages/tool/package.json": "match"} {
		var found bool
		for _, c := range r.Contexts {
			if c.ManifestPath != manifest {
				continue
			}
			found = true
			if c.AssociationState != "observed" || c.LockfilePath != "package-lock.json" || len(c.Checks) != 1 || c.Checks[0].Status != expected {
				t.Fatalf("member did not compare against its own package descriptor: %+v", c)
			}
			if len(c.Boundaries) != 1 || c.Boundaries[0].Reason != "npm-workspace-lock-ownership-observed" {
				t.Fatalf("shared ownership evidence was not named: %+v", c.Boundaries)
			}
		}
		if !found {
			t.Fatalf("missing member context %q", manifest)
		}
	}
	if err := ValidateReport(r); err != nil {
		t.Fatalf("invalid workspace report: %v", err)
	}
}
func TestNPMWorkspaceAssociationRequiresCompleteUnambiguousOwnership(t *testing.T) {
	makeRoot := func(target string) declarations.ProjectRecord {
		r := npmRecord(".")
		r.Project.Requirements = []declarations.Requirement{{Kind: "npm-workspace-root", Value: "true", State: "declared", Evidence: "package.json"}}
		r.Project.References = []declarations.Reference{{Kind: "npm-workspace-member", Value: "packages/*", Target: target, State: "resolved", Evidence: "package.json"}}
		return r
	}
	member := npmRecord("packages/app", npmRef("alpha@1.0.0", "dependencies"))
	shared := `{"lockfileVersion":3,"packages":{"":{"dependencies":{}},"packages/app":{"dependencies":{"alpha":"1.0.0"}}}}`
	for _, tc := range []struct {
		name     string
		roots    []declarations.ProjectRecord
		content  map[string]string
		diags    []declarations.Diagnostic
		complete bool
	}{
		{"missing member descriptor", []declarations.ProjectRecord{makeRoot(member.Project.ID)}, map[string]string{"package-lock.json": `{"lockfileVersion":3,"packages":{"":{"dependencies":{}}}}`}, nil, true},
		{"unresolved member of a listing ancestor", []declarations.ProjectRecord{func() declarations.ProjectRecord {
			r := makeRoot(member.Project.ID)
			r.Project.References = append(r.Project.References, declarations.Reference{Kind: "npm-workspace-member", Target: "packages/other/package.json", State: "unresolved", Evidence: "package.json"})
			return r
		}()}, map[string]string{"package-lock.json": shared}, nil, true},
		{"duplicate member names make npm reject the workspace", []declarations.ProjectRecord{makeRoot(member.Project.ID)}, map[string]string{"package-lock.json": shared}, []declarations.Diagnostic{{Path: "package.json", Code: "duplicate-npm-workspace-name"}}, true},
		{"omitted diagnostics", []declarations.ProjectRecord{makeRoot(member.Project.ID)}, map[string]string{"package-lock.json": shared}, nil, true},
		{"unsupported workspace declaration", []declarations.ProjectRecord{makeRoot(member.Project.ID)}, map[string]string{"package-lock.json": shared}, []declarations.Diagnostic{{Path: "package.json", Code: "unsupported-npm-workspace-pattern"}}, true},
		{"incomplete parent inventory", []declarations.ProjectRecord{makeRoot(member.Project.ID)}, map[string]string{"package-lock.json": shared}, nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			records := append(slices.Clone(tc.roots), member)
			in := testInput(records, tc.content, true)
			in.WorkspaceLocks = true
			in.Declarations.Diagnostics = tc.diags
			if !tc.complete {
				in.Declarations.Coverage.OmittedFiles = 1
			}
			if tc.name == "omitted diagnostics" {
				in.Declarations.Coverage.OmittedDiagnostics = 1
			}
			r, err := Analyze(context.Background(), in, Limits{})
			if err != nil {
				t.Fatal(err)
			}
			for _, c := range r.Contexts {
				if c.ManifestPath == member.Project.ID && (c.AssociationState != "indeterminate" || len(c.Checks) > 0 && c.Checks[0].Status != "indeterminate") {
					t.Fatalf("ambiguous/missing evidence became a comparison: %+v", c)
				}
			}
			if err := ValidateReport(r); err != nil {
				t.Fatalf("invalid qualified report: %v", err)
			}
		})
	}
}

func TestNPMWorkspaceMalformedSelectedAncestorBlocksOwnerButUnrelatedDoesNot(t *testing.T) {
	owner := npmRecord(".")
	owner.Project.Requirements = []declarations.Requirement{{Kind: "npm-workspace-root", Value: "true", State: "declared"}}
	owner.Project.References = []declarations.Reference{{Kind: "npm-workspace-member", Target: "packages/app/package.json", State: "resolved", Evidence: "package.json"}}
	member := npmRecord("packages/app", npmRef("alpha@1.0.0", "dependencies"))
	shared := `{"lockfileVersion":3,"packages":{"":{"dependencies":{}},"packages/app":{"dependencies":{"alpha":"1.0.0"}}}}`
	invalidIntermediate := npmRecord("packages")
	invalidIntermediate.Parsed = false
	validButIncompleteIntermediate := npmRecord("packages")
	validButIncompleteIntermediate.Complete = false
	malformedWorkspaceIntermediate := npmRecord("packages")
	workspaceDiagnostic := declarations.Diagnostic{Path: malformedWorkspaceIntermediate.Project.ID, Code: "unsupported-npm-workspaces"}
	unrelatedMalformed := npmRecord("outside")
	unrelatedMalformed.Parsed = false

	for _, tc := range []struct {
		name            string
		intermediate    declarations.ProjectRecord
		hasIntermediate bool
		diagnostics     []declarations.Diagnostic
		want            string
	}{
		{name: "unparsed selected intermediate", intermediate: invalidIntermediate, hasIntermediate: true, want: "indeterminate"},
		{name: "incomplete selected intermediate", intermediate: validButIncompleteIntermediate, hasIntermediate: true, want: "indeterminate"},
		{name: "malformed workspace declaration", intermediate: malformedWorkspaceIntermediate, hasIntermediate: true, diagnostics: []declarations.Diagnostic{workspaceDiagnostic}, want: "indeterminate"},
		{name: "unrelated malformed package does not poison ownership", intermediate: unrelatedMalformed, hasIntermediate: true, want: "observed"},
		{name: "complete ordinary intermediate remains eligible", intermediate: npmRecord("packages"), hasIntermediate: true, want: "observed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			records := []declarations.ProjectRecord{owner}
			if tc.hasIntermediate {
				records = append(records, tc.intermediate)
			}
			records = append(records, member)
			in := testInput(records, map[string]string{"package-lock.json": shared}, true)
			in.WorkspaceLocks = true
			in.Declarations.Diagnostics = tc.diagnostics
			report, err := Analyze(context.Background(), in, Limits{})
			if err != nil {
				t.Fatal(err)
			}
			c := contextByManifest(report, member.Project.ID)
			if c.AssociationState != tc.want {
				t.Fatalf("association=%q want %q: %+v", c.AssociationState, tc.want, c)
			}
			if tc.want == "observed" && (len(c.Checks) != 1 || c.Checks[0].Status != "match") {
				t.Fatalf("complete ownership did not compare member declaration: %+v", c)
			}
			if tc.want == "indeterminate" && len(c.Checks) != 0 {
				t.Fatalf("incomplete ownership produced a comparison: %+v", c)
			}
		})
	}
}

// Oracle: npm 11.12.1 `npm prefix --offline` in a member prints the nearest
// ancestor whose workspaces list it, skips ancestors that do not list it, and
// stops there even when a farther ancestor also lists the member.
func TestNPMWorkspaceNearestListingAncestorOwnsMember(t *testing.T) {
	workspace := func(root string, targets ...string) declarations.ProjectRecord {
		r := npmRecord(root)
		r.Project.Requirements = []declarations.Requirement{{Kind: "npm-workspace-root", Value: "true", State: "declared"}}
		for _, target := range targets {
			r.Project.References = append(r.Project.References, declarations.Reference{Kind: "npm-workspace-member", Target: target, State: "resolved", Evidence: r.Project.ID})
		}
		return r
	}
	member := npmRecord("repo/packages/app", npmRef("alpha@1.0.0", "dependencies"))
	shared := `{"lockfileVersion":3,"packages":{"":{"dependencies":{}},"packages/app":{"dependencies":{"alpha":"1.0.0"}}}}`
	outerShared := `{"lockfileVersion":3,"packages":{"":{"dependencies":{}},"repo/packages/app":{"dependencies":{"alpha":"1.0.0"}}}}`
	unparsedAbove := npmRecord(".")
	unparsedAbove.Parsed = false
	unparsedIntermediate := npmRecord("repo")
	unparsedIntermediate.Parsed = false
	for _, tc := range []struct {
		name    string
		records []declarations.ProjectRecord
		files   map[string]string
		state   string
		lock    string
		reason  string
	}{
		{"unparsed package above the owner is not consulted", []declarations.ProjectRecord{unparsedAbove, workspace("repo", member.Project.ID), member}, map[string]string{"repo/package-lock.json": shared}, "observed", "repo/package-lock.json", "npm-workspace-lock-ownership-observed"},
		{"farther listing ancestor loses to the nearest", []declarations.ProjectRecord{workspace(".", member.Project.ID), workspace("repo", member.Project.ID), member}, map[string]string{"package-lock.json": outerShared, "repo/package-lock.json": shared}, "observed", "repo/package-lock.json", "npm-workspace-lock-ownership-observed"},
		{"non-listing intermediate is skipped", []declarations.ProjectRecord{workspace(".", member.Project.ID), npmRecord("repo"), member}, map[string]string{"package-lock.json": outerShared}, "observed", "package-lock.json", "npm-workspace-lock-ownership-observed"},
		{"nearest owner without a lockfile", []declarations.ProjectRecord{workspace(".", member.Project.ID), workspace("repo", member.Project.ID), member}, map[string]string{"package-lock.json": outerShared}, "missing", "", "npm-workspace-root-lockfile-not-present"},
		{"unparsed intermediate could list the member", []declarations.ProjectRecord{workspace(".", member.Project.ID), unparsedIntermediate, member}, map[string]string{"package-lock.json": outerShared}, "indeterminate", "", "npm-workspace-lock-owner-incomplete"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			in := testInput(tc.records, tc.files, true)
			in.WorkspaceLocks = true
			report, err := Analyze(context.Background(), in, Limits{})
			if err != nil {
				t.Fatal(err)
			}
			c := contextByManifest(report, member.Project.ID)
			if c.AssociationState != tc.state || c.LockfilePath != tc.lock || len(c.Boundaries) != 1 || c.Boundaries[0].Reason != tc.reason {
				t.Fatalf("got %+v", c)
			}
			if tc.state == "observed" && (len(c.Checks) != 1 || c.Checks[0].Status != "match") {
				t.Fatalf("owner lock did not compare the member descriptor: %+v", c)
			}
			if err := ValidateReport(report); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestNPMWorkspaceLockStatesFollowTheOwnerDirectory(t *testing.T) {
	root := npmRecord(".")
	root.Project.Requirements = []declarations.Requirement{{Kind: "npm-workspace-root", Value: "true", State: "declared"}}
	root.Project.References = []declarations.Reference{{Kind: "npm-workspace-member", Target: "packages/app/package.json", State: "resolved", Evidence: "package.json"}}
	member := npmRecord("packages/app", npmRef("alpha@1.0.0", "dependencies"))
	valid := `{"lockfileVersion":3,"packages":{"":{"dependencies":{}},"packages/app":{"dependencies":{"alpha":"1.0.0"}}}}`
	for _, tc := range []struct {
		name        string
		records     []declarations.ProjectRecord
		files       map[string]string
		association string
	}{
		{"unsupported v1 lock", []declarations.ProjectRecord{root, member}, map[string]string{"package-lock.json": `{"lockfileVersion":1,"dependencies":{}}`}, "unsupported"},
		// npm never reads a lockfile in a directory without package.json.
		{"lock in a directory without package.json", []declarations.ProjectRecord{root, member}, map[string]string{"package-lock.json": valid, "packages/package-lock.json": `{"lockfileVersion":3,"packages":{"":{"dependencies":{}}}}`}, "observed"},
		{"ancestor lock without a selected ancestor manifest", []declarations.ProjectRecord{member}, map[string]string{"package-lock.json": valid}, "missing"},
		{"shrinkwrap takes precedence at the owner", []declarations.ProjectRecord{root, member}, map[string]string{"package-lock.json": valid, "npm-shrinkwrap.json": valid}, "observed"},
		{"Yarn lock at the owner", []declarations.ProjectRecord{root, member}, map[string]string{"yarn.lock": "# yarn lockfile v1\n"}, "unsupported"},
		{"pnpm workspace file at the owner", []declarations.ProjectRecord{root, member}, map[string]string{"pnpm-workspace.yaml": "packages: []\n"}, "unsupported"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			in := testInput(tc.records, tc.files, true)
			in.WorkspaceLocks = true
			r, err := Analyze(context.Background(), in, Limits{})
			if err != nil {
				t.Fatal(err)
			}
			c := contextByManifest(r, member.Project.ID)
			if c.AssociationState != tc.association {
				t.Fatalf("association=%q want %q: %+v", c.AssociationState, tc.association, c)
			}
			if c.AssociationState == "indeterminate" && len(c.Checks) != 0 || c.AssociationState == "unsupported" && len(c.Checks) == 1 && c.Checks[0].Status != "indeterminate" {
				t.Fatalf("unknown/corrupt evidence became a check result: %+v", c)
			}
		})
	}
}

func TestNPMNestedWorkspaceMemberRemainsIndeterminate(t *testing.T) {
	owner := npmRecord(".")
	owner.Project.Requirements = []declarations.Requirement{{Kind: "npm-workspace-root", Value: "true", State: "declared"}}
	owner.Project.References = []declarations.Reference{{Kind: "npm-workspace-member", Target: "packages/nested/package.json", State: "resolved", Evidence: "package.json"}}
	member := npmRecord("packages/nested", npmRef("alpha@1.0.0", "dependencies"))
	member.Project.Requirements = []declarations.Requirement{{Kind: "npm-workspace-root", Value: "true", State: "declared"}}
	in := testInput([]declarations.ProjectRecord{owner, member}, map[string]string{"package-lock.json": `{"lockfileVersion":3,"packages":{"":{"dependencies":{}},"packages/nested":{"dependencies":{"alpha":"1.0.0"}}}}`}, true)
	in.WorkspaceLocks = true
	r, err := Analyze(context.Background(), in, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	c := contextByManifest(r, member.Project.ID)
	if c.AssociationState != "indeterminate" || len(c.Checks) != 0 {
		t.Fatalf("nested workspace membership was treated as unambiguous: %+v", c)
	}
}

func TestNPMWorkspaceEmptyRootAndPrivateEmptyMember(t *testing.T) {
	member := npmRecord("apps/private")
	member.Project.Requirements = []declarations.Requirement{{Kind: "npm-private", Value: "true", State: "declared"}}
	member.Project.References = []declarations.Reference{}
	root := npmRecord(".")
	root.Project.Requirements = []declarations.Requirement{{Kind: "npm-workspace-root", Value: "true", State: "declared"}}
	// A workspace root with an empty workspaces declaration cannot own the
	// present package merely because its path is beneath the root.
	in := testInput([]declarations.ProjectRecord{root, member}, map[string]string{"package-lock.json": `{"lockfileVersion":3,"packages":{"":{"dependencies":{}},"apps/private":{"dependencies":{}}}}`}, true)
	in.WorkspaceLocks = true
	r, err := Analyze(context.Background(), in, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	c := contextByManifest(r, member.Project.ID)
	if c.AssociationState != "not_applicable" || len(c.Checks) != 0 || len(c.Boundaries) != 1 || c.Boundaries[0].Reason != "npm-ancestor-lock-not-shared" {
		t.Fatalf("empty workspace root inferred membership: %+v", c)
	}

	root.Project.References = []declarations.Reference{{Kind: "npm-workspace-member", Target: member.Project.ID, State: "resolved", Evidence: "package.json"}}
	in = testInput([]declarations.ProjectRecord{root, member}, map[string]string{"package-lock.json": `{"lockfileVersion":3,"packages":{"":{"dependencies":{}},"apps/private":{"dependencies":{}}}}`}, true)
	in.WorkspaceLocks = true
	r, err = Analyze(context.Background(), in, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	c = contextByManifest(r, member.Project.ID)
	if c.AssociationState != "observed" || len(c.Checks) != 1 || c.Checks[0].Status != "match" || c.Checks[0].Compared != 0 {
		t.Fatalf("valid private dependency-free member was not represented accurately: %+v", c)
	}
}

func TestNPMWorkspaceScopedConditionalPeerAndPackageManagerEligibility(t *testing.T) {
	root := npmRecord(".")
	root.Project.Requirements = []declarations.Requirement{{Kind: "npm-workspace-root", Value: "true", State: "declared"}, {Kind: "package-manager", Value: "npm@11.19.0", State: "declared"}}
	member := npmRecord("packages/app", npmRef("@scope/widget@^2.0.0", "peerDependencies"))
	root.Project.References = []declarations.Reference{{Kind: "npm-workspace-member", Target: member.Project.ID, State: "resolved", Evidence: "package.json"}}
	member.Project.References[0].State = "conditional"
	lock := `{"lockfileVersion":3,"packages":{"":{"dependencies":{}},"packages/app":{"peerDependencies":{"@scope/widget":"^2.0.0"}}}}`
	in := testInput([]declarations.ProjectRecord{root, member}, map[string]string{"package-lock.json": lock}, true)
	in.WorkspaceLocks = true
	r, err := Analyze(context.Background(), in, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	c := contextByManifest(r, member.Project.ID)
	if c.AssociationState != "observed" || len(c.Checks) != 1 || c.Checks[0].Status != "indeterminate" || len(c.Checks[0].Unexpected) != 1 || c.Checks[0].Unexpected[0] != "@scope/widget" {
		t.Fatalf("scoped conditional peer dependency lost its uncertainty: %+v", c)
	}

	for _, manager := range []string{"yarn@4.0.0", "pnpm@9.0.0"} {
		t.Run(manager, func(t *testing.T) {
			own := npmRecord("app", npmRef("alpha@1.0.0", "dependencies"))
			own.Project.Requirements = []declarations.Requirement{{Kind: "package-manager", Value: manager, State: "declared"}}
			ownLock := `{"lockfileVersion":3,"packages":{"":{"dependencies":{"alpha":"1.0.0"}}}}`
			input := testInput([]declarations.ProjectRecord{own}, map[string]string{"app/package-lock.json": ownLock}, true)
			input.WorkspaceLocks = true
			report, analyzeErr := Analyze(context.Background(), input, Limits{})
			if analyzeErr != nil {
				t.Fatal(analyzeErr)
			}
			ctxReport := report.Contexts[0]
			if ctxReport.AssociationState != "unsupported" || len(ctxReport.Checks) != 0 || len(ctxReport.Boundaries) != 1 || ctxReport.Boundaries[0].Reason != "npm-alternative-package-manager" {
				t.Fatalf("%s was falsely treated as an npm lock owner: %+v", manager, ctxReport)
			}
			input.WorkspaceLocks = false
			legacy, analyzeErr := Analyze(context.Background(), input, Limits{})
			if analyzeErr != nil || !reflect.DeepEqual(legacy.Contexts, report.Contexts) {
				t.Fatalf("modes disagree for %s: report=%+v err=%v", manager, legacy, analyzeErr)
			}
			owner := npmRecord(".")
			owner.Project.Requirements = []declarations.Requirement{{Kind: "npm-workspace-root", Value: "true", State: "declared"}, {Kind: "package-manager", Value: manager, State: "declared"}}
			workspaceMember := npmRecord("packages/app", npmRef("alpha@1.0.0", "dependencies"))
			owner.Project.References = []declarations.Reference{{Kind: "npm-workspace-member", Target: workspaceMember.Project.ID, State: "resolved", Evidence: "package.json"}}
			workspaceInput := testInput([]declarations.ProjectRecord{owner, workspaceMember}, map[string]string{"package-lock.json": `{"lockfileVersion":3,"packages":{"":{"dependencies":{}},"packages/app":{"dependencies":{"alpha":"1.0.0"}}}}`}, true)
			workspaceInput.WorkspaceLocks = true
			workspaceReport, analyzeErr := Analyze(context.Background(), workspaceInput, Limits{})
			if analyzeErr != nil {
				t.Fatal(analyzeErr)
			}
			workspaceContext := contextByManifest(workspaceReport, workspaceMember.Project.ID)
			if workspaceContext.AssociationState != "unsupported" || len(workspaceContext.Checks) != 0 || workspaceContext.Boundaries[0].Path != "package.json" {
				t.Fatalf("workspace owned by %s was falsely eligible for npm lock coverage: %+v", manager, workspaceContext)
			}
		})
	}
}

func TestNPMWorkspaceMemberDescriptorIsInterpretedAlone(t *testing.T) {
	duplicate := parseLock("npm", []byte(`{"lockfileVersion":3,"packages":{"":{"dependencies":{}},"":{"dependencies":{}}}}`))
	if duplicate.state != "unsupported" || duplicate.reason != "invalid-lockfile-json" {
		t.Fatalf("duplicate JSON package entries accepted: %+v", duplicate)
	}
	// An unrelated malformed descriptor (for example a linked package outside
	// the root) neither blocks the member comparison nor spends name budget.
	lock := parseLock("npm", []byte(`{"lockfileVersion":3,"packages":{"":{"dependencies":{"a":"1"}},"packages/app":{"dependencies":{"b":"1"}},"../outside":7,"packages/bad":{"dependencies":[]}}}`))
	if lock.state != "supported" {
		t.Fatalf("root entry rejected: %+v", lock)
	}
	for _, tc := range []struct{ location, state, reason string }{
		{"packages/app", "supported", ""},
		{"packages/none", "indeterminate", "npm-workspace-lock-member-entry-missing"},
		{"../outside", "unsupported", "invalid-npm-package-descriptor"},
		{"packages/bad", "unsupported", "invalid-npm-direct-table"},
	} {
		tables, state, reason := lock.npmMember(tc.location)
		if state != tc.state || reason != tc.reason || state == "supported" && tables["dependencies"]["b"] != "1" {
			t.Fatalf("%s: state=%q reason=%q tables=%v", tc.location, state, reason, tables)
		}
	}
}

func TestNPMWorkspaceModeOptInLeavesLegacyReportUnchanged(t *testing.T) {
	root := npmRecord(".")
	root.Project.Requirements = []declarations.Requirement{{Kind: "npm-workspace-root", Value: "true", State: "declared"}}
	root.Project.References = []declarations.Reference{{Kind: "npm-workspace-member", Target: "app/package.json", State: "resolved", Evidence: "package.json"}}
	member := npmRecord("app", npmRef("a@1.0.0", "dependencies"))
	in := testInput([]declarations.ProjectRecord{root, member}, map[string]string{"package-lock.json": `{"lockfileVersion":3,"packages":{"":{"dependencies":{}},"app":{"dependencies":{"a":"1.0.0"}}}}`}, true)
	legacy, err := Analyze(context.Background(), in, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(legacy)
	in.WorkspaceLocks = true
	optIn, err := Analyze(context.Background(), in, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	legacyMember, optMember := contextByManifest(legacy, "app/package.json"), contextByManifest(optIn, "app/package.json")
	if legacyMember.AssociationState != "indeterminate" || optMember.AssociationState != "observed" || len(optMember.Boundaries) == 0 {
		t.Fatalf("opt-in association did not separate historical and new behavior: legacy=%+v opted=%+v", legacyMember, optMember)
	}
	encodedAgain, _ := json.Marshal(legacy)
	if !bytes.Equal(encoded, encodedAgain) {
		t.Fatal("opt-in analysis mutated the prior report")
	}
}

func TestWorkspaceModePreservesLiteralPOSIXBackslashProjectIdentity(t *testing.T) {
	lock := `{"lockfileVersion":3,"packages":{"":{"dependencies":{"a":"1.0.0"}}}}`
	backslash := npmRecord(`a\b`, npmRef("a@1.0.0", "dependencies"))
	slash := npmRecord("a/b", npmRef("a@1.0.0", "dependencies"))
	backslashLock := `a\b/package-lock.json`
	slashLock := "a/b/package-lock.json"
	in := testInput([]declarations.ProjectRecord{backslash, slash}, map[string]string{backslashLock: lock, slashLock: lock}, true)
	in.WorkspaceLocks = true
	report, err := Analyze(context.Background(), in, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Contexts) != 2 {
		t.Fatalf("literal backslash path aliased or poisoned the other project: %+v", report.Contexts)
	}
	byID := map[string]Context{}
	for _, ctx := range report.Contexts {
		byID[ctx.ProjectID] = ctx
	}
	for _, expected := range []struct{ id, lock string }{{`a\b/package.json`, backslashLock}, {"a/b/package.json", slashLock}} {
		ctx, ok := byID[expected.id]
		if !ok || ctx.ManifestPath != expected.id || ctx.LockfilePath != expected.lock || ctx.AssociationState != "observed" || len(ctx.Checks) != 1 || ctx.Checks[0].Status != "match" {
			t.Fatalf("selected POSIX identity lost for %q: %+v", expected.id, report.Contexts)
		}
	}
	if len(report.Semantics) != 3 || report.Semantics[2] != npmWorkspaceSemantics {
		t.Fatalf("opt-in path semantics are not identified: %v", report.Semantics)
	}
	if err := ValidateReport(report); err != nil {
		t.Fatalf("opt-in POSIX path report did not validate: %v", err)
	}
}

func contextByManifest(r *Report, manifest string) Context {
	for _, c := range r.Contexts {
		if c.ManifestPath == manifest {
			return c
		}
	}
	return Context{}
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

func TestNuGetCaseFoldedDuplicatePackageIDsAreUnsupported(t *testing.T) {
	record := nugetRecord("src/App", "src/App/App.csproj", declarations.Requirement{Kind: "package-reference", Value: "Foo@1.0", State: "declared"})
	for _, entries := range []string{
		`"Foo":{"type":"transitive"},"foo":{"type":"direct"}`,
		`"Foo":{"type":"direct"},"foo":{"type":"direct"}`,
	} {
		lock := `{"version":1,"dependencies":{"net8.0":{` + entries + `}}}`
		r, err := Analyze(context.Background(), testInput([]declarations.ProjectRecord{record}, map[string]string{"src/App/packages.lock.json": lock}, true), Limits{})
		if err != nil {
			t.Fatal(err)
		}
		ctx := r.Contexts[0]
		if r.Status != "partial" || ctx.AssociationState != "unsupported" || len(ctx.Checks) != 1 || ctx.Checks[0].Status != "indeterminate" ||
			len(ctx.Checks[0].Missing)+len(ctx.Checks[0].Mismatched)+len(ctx.Checks[0].Unexpected) != 0 {
			t.Fatalf("case-folded duplicate package IDs became direct-presence evidence: %+v", r)
		}
		if err := ValidateReport(r); err != nil {
			t.Fatalf("qualified duplicate-package report was invalid: %v", err)
		}
	}
}

func TestAnalyzeOutputWithUnsupportedNuGetVersionPassesReportValidation(t *testing.T) {
	record := nugetRecord("src/App", "src/App/App.csproj", declarations.Requirement{Kind: "package-reference", Value: "A@1.0", State: "declared"})
	for _, version := range []string{"3", "17"} {
		t.Run(version, func(t *testing.T) {
			lock := `{"version":` + version + `,"dependencies":{"net8.0":{"A":{"type":"Direct"}}}}`
			r, err := Analyze(context.Background(), testInput([]declarations.ProjectRecord{record}, map[string]string{"src/App/packages.lock.json": lock}, true), Limits{})
			if err != nil {
				t.Fatal(err)
			}
			if r.Contexts[0].AssociationState != "unsupported" {
				t.Fatalf("unsupported NuGet version was not retained as unsupported: %+v", r.Contexts[0])
			}
			if err := ValidateReport(r); err != nil {
				t.Fatalf("analyzer output for unsupported NuGet version was rejected: %v; report=%+v", err, r)
			}
		})
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

func TestNuGetCaseVariantLockNamesRemainUnresolved(t *testing.T) {
	record := nugetRecord("src/App", "src/App/App.csproj", declarations.Requirement{Kind: "package-reference", Value: "A@1.0", State: "declared"})
	lock := `{"version":1,"dependencies":{"net8.0":{"A":{"type":"Direct"}}}}`
	for _, name := range []string{"Packages.lock.json", "packages.app.lock.json"} {
		t.Run(name, func(t *testing.T) {
			path := "src/App/" + name
			r, err := Analyze(context.Background(), testInput([]declarations.ProjectRecord{record}, map[string]string{path: lock}, true), Limits{})
			if err != nil {
				t.Fatal(err)
			}
			ctx := r.Contexts[0]
			if r.Coverage.LockCandidates != 1 || ctx.AssociationState != "indeterminate" || len(ctx.Checks) != 0 || len(ctx.Boundaries) != 1 || ctx.Boundaries[0].Reason != "nuget-lockfile-case-unresolved" {
				t.Fatalf("case-variant NuGet lock name was missed or confidently associated: report=%+v", r)
			}
			if err := ValidateReport(r); err != nil {
				t.Fatalf("case-variant report is invalid: %v", err)
			}
		})
	}
}

func TestNuGetLockOwnershipIsUnresolvedWhenProjectInventoryWasOmitted(t *testing.T) {
	record := nugetRecord("src/App", "src/App/App.csproj", declarations.Requirement{Kind: "package-reference", Value: "A@1.0", State: "declared"})
	lock := `{"version":1,"dependencies":{"net8.0":{"A":{"type":"Direct"}}}}`
	in := testInput([]declarations.ProjectRecord{record}, map[string]string{"src/App/packages.lock.json": lock}, true)
	in.Declarations.Status = "partial"
	in.Declarations.Coverage.OmittedFiles = 1
	r, err := Analyze(context.Background(), in, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	ctx := r.Contexts[0]
	if ctx.AssociationState != "indeterminate" || len(ctx.Checks) != 0 || len(ctx.Boundaries) != 1 || ctx.Boundaries[0].Reason != "project-inventory-incomplete-association" {
		t.Fatalf("incomplete project inventory was used to claim NuGet lock ownership: %+v", ctx)
	}
	if err := ValidateReport(r); err != nil {
		t.Fatalf("incomplete-inventory report is invalid: %v", err)
	}
}

func TestDeclaredCompleteStatusCannotHideOmittedProjectInventory(t *testing.T) {
	record := nugetRecord("src/App", "src/App/App.csproj", declarations.Requirement{Kind: "package-reference", Value: "A@1.0", State: "declared"})
	lock := `{"version":1,"dependencies":{"net8.0":{"A":{"type":"Direct"}}}}`
	in := testInput([]declarations.ProjectRecord{record}, map[string]string{"src/App/packages.lock.json": lock}, true)
	in.Declarations.Coverage.OmittedFiles = 1
	r, err := Analyze(context.Background(), in, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	if r.Status != "partial" || r.Contexts[0].AssociationState != "indeterminate" || len(r.Contexts[0].Checks) != 0 {
		t.Fatalf("mis-stated complete declaration coverage produced a complete association: %+v", r)
	}
	if err := ValidateReport(r); err != nil {
		t.Fatalf("qualified omitted-project report was invalid: %v", err)
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
	if len(r.Contexts) != 2 {
		t.Fatalf("C# and F# projects should both have NuGet lock contexts: %+v", r.Contexts)
	}
	for _, c := range r.Contexts {
		if c.AssociationState != "indeterminate" || c.Boundaries[0].Reason != "ambiguous-nuget-lockfile-owner" {
			t.Fatalf("generic NuGet lock was assigned despite a same-directory second owner: %+v", r.Contexts)
		}
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
		{"custom property missing path", `<Project><PropertyGroup><NuGetLockFilePath>elsewhere.lock.json</NuGetLockFilePath></PropertyGroup>` + pkgA + `</Project>`, "missing", "lockfile-not-present"},
		{"comment does not count", `<Project><!-- <NuGetLockFilePath>elsewhere.lock.json</NuGetLockFilePath> -->` + pkgA + `</Project>`, "observed", ""},
		{"malformed XML unresolved", `<Project><NuGetLockFilePath>elsewhere.lock.json</Project>`, "indeterminate", "nuget-project-config-unresolved"},
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

func TestNuGetCustomLockOwnersAreResolvedPerActualPath(t *testing.T) {
	lock := `{"version":1,"dependencies":{"net8.0":{"A":{"type":"Direct"}}}}`
	var records []declarations.ProjectRecord
	content := map[string]string{}
	for i := 0; i < 20; i++ {
		root := fmt.Sprintf("src/P%02d", i)
		manifest := path.Join(root, fmt.Sprintf("P%02d.csproj", i))
		records = append(records, nugetRecord(root, manifest, declarations.Requirement{Kind: "package-reference", Value: "A@1.0", State: "declared"}))
		custom := path.Join("locks", fmt.Sprintf("P%02d.data", i))
		if i < 2 {
			custom = "locks/shared.data"
		}
		content[manifest] = `<Project><PropertyGroup><NuGetLockFilePath>../../` + custom + `</NuGetLockFilePath></PropertyGroup>` + pkgA + `</Project>`
		content[custom] = lock
	}
	in := testInput(records, content, true)
	r, err := Analyze(context.Background(), in, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Contexts) != 20 {
		t.Fatalf("expected all contexts, got %d", len(r.Contexts))
	}
	for _, c := range r.Contexts {
		idx := strings.TrimSuffix(path.Base(c.ManifestPath), ".csproj")
		if idx == "P00" || idx == "P01" {
			if c.AssociationState != "indeterminate" || c.OutcomeReason() != "ambiguous-nuget-lockfile-owner" {
				t.Errorf("shared actual path was not marked ambiguous: %+v", c)
			}
		} else if c.AssociationState != "observed" || c.LockfilePath != path.Join("locks", idx+".data") {
			t.Errorf("independent custom path became unknown: %+v", c)
		}
	}
}

func TestNuGetCustomPathCollidesWithDefaultAndNonNuGetMSBuildOwner(t *testing.T) {
	lock := `{"version":1,"dependencies":{"net8.0":{"A":{"type":"Direct"}}}}`
	first := nugetRecord("src/App", "src/App/App.csproj", declarations.Requirement{Kind: "package-reference", Value: "A@1.0", State: "declared"})
	second := nugetRecord("src/App", "src/App/Worker.csproj")
	other := declarations.ProjectRecord{Parsed: true, Complete: true, Project: declarations.Project{ID: "src/App/Tool.wixproj", Root: "src/App", Kind: "dotnet"}}
	for _, tc := range []struct {
		name    string
		second  declarations.ProjectRecord
		content map[string]string
	}{
		{"default lock path", second, map[string]string{
			"src/App/App.csproj":    `<Project><PropertyGroup><NuGetLockFilePath>packages.Worker.lock.json</NuGetLockFilePath></PropertyGroup></Project>`,
			"src/App/Worker.csproj": `<Project />`, "src/App/packages.Worker.lock.json": lock,
		}},
		{"non-NuGet MSBuild project", other, map[string]string{
			"src/App/App.csproj":   `<Project><PropertyGroup><NuGetLockFilePath>../../locks/tool.bin</NuGetLockFilePath></PropertyGroup></Project>`,
			"src/App/Tool.wixproj": `<Project><PropertyGroup><NuGetLockFilePath>../../locks/tool.bin</NuGetLockFilePath></PropertyGroup></Project>`,
			"locks/tool.bin":       lock,
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			in := testInput([]declarations.ProjectRecord{first, tc.second}, tc.content, true)
			r, err := Analyze(context.Background(), in, Limits{})
			if err != nil {
				t.Fatal(err)
			}
			if r.Contexts[0].AssociationState != "indeterminate" || r.Contexts[0].OutcomeReason() != "ambiguous-nuget-lockfile-owner" {
				t.Fatalf("custom path had a second potential owner: %+v", r.Contexts[0])
			}
		})
	}
}

func TestNuGetCustomPathClaimsBlockUnknownPotentialOwners(t *testing.T) {
	lock := `{"version":1,"dependencies":{"net8.0":{"A":{"type":"Direct"}}}}`
	first := nugetRecord("src/App", "src/App/App.csproj", declarations.Requirement{Kind: "package-reference", Value: "A@1.0", State: "declared"})
	second := nugetRecord("tools/Worker", "tools/Worker/Worker.fsproj")
	in := testInput([]declarations.ProjectRecord{first, second}, map[string]string{
		"src/App/App.csproj":         `<Project><PropertyGroup><NuGetLockFilePath>../../locks/shared.data</NuGetLockFilePath></PropertyGroup>` + pkgA + `</Project>`,
		"tools/Worker/Worker.fsproj": `<Project><PropertyGroup><NuGetLockFilePath>../../locks/shared.data</PropertyGroup>`,
		"locks/shared.data":          lock,
	}, true)
	r, err := Analyze(context.Background(), in, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	if r.Contexts[0].AssociationState != "indeterminate" || r.Contexts[0].NuGetEvidence.PresenceState != "observed" {
		t.Fatalf("unparsed potential owner was ignored or known presence was lost: %+v", r.Contexts[0])
	}

	// An attributed omitted MSBuild project may point at any custom path, even
	// when its directory differs from the known project's root.
	in = testInput([]declarations.ProjectRecord{first}, map[string]string{
		"src/App/App.csproj": `<Project><PropertyGroup><NuGetLockFilePath>../../locks/shared.data</NuGetLockFilePath></PropertyGroup>` + pkgA + `</Project>`,
		"locks/shared.data":  lock,
	}, true)
	in.Declarations.Coverage.OmittedFiles = 1
	in.DeclarationOmissionsAttributed = true
	in.DeclarationOmissions = []string{"other/Unknown.csproj"}
	r, err = Analyze(context.Background(), in, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	if r.Contexts[0].AssociationState != "indeterminate" || r.Contexts[0].NuGetEvidence.PresenceState != "observed" {
		t.Fatalf("omitted potential owner was ignored or known presence was lost: %+v", r.Contexts[0])
	}
}

func TestNuGetMSBuildXMLRequiresOneCompleteRootAndIgnoresUnselectedMalformedXML(t *testing.T) {
	for _, input := range []string{
		`<Project /><Project />`,
		`<Project />trailing-junk`,
	} {
		if _, ok := parseNuGetMSBuildFile(context.Background(), []byte(input), 1024); ok {
			t.Errorf("accepted trailing XML content %q", input)
		}
	}
	if _, ok := parseNuGetMSBuildFile(context.Background(), []byte(`<Project /> <!-- trailing comment -->`), 1024); !ok {
		t.Fatal("valid trailing XML comment was rejected")
	}
	manifest := nugetRecord("src/App", "src/App/App.csproj", declarations.Requirement{Kind: "package-reference", Value: "A@1.0", State: "declared"})
	lock := `{"version":1,"dependencies":{"net8.0":{"A":{"type":"Direct"}}}}`
	in := testInput([]declarations.ProjectRecord{manifest}, map[string]string{
		"src/App/packages.lock.json": lock,
		"unrelated.xml":              `<Project><broken`,
	}, true)
	r, err := Analyze(context.Background(), in, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	if r.Contexts[0].AssociationState != "observed" || r.Contexts[0].Checks[0].Status != "match" {
		t.Fatalf("unselected malformed XML tainted static evidence: %+v", r.Contexts[0])
	}
}

func TestNuGetSharedProjectInputsKeepLockfileClaimIndeterminate(t *testing.T) {
	record := nugetRecord("src/App", "src/App/App.csproj", declarations.Requirement{Kind: "package-reference", Value: "A@1.0", State: "declared"})
	lock := `{"version":1,"dependencies":{"net8.0":{"A":{"type":"Direct"}}}}`
	in := testInput([]declarations.ProjectRecord{record}, map[string]string{
		"src/App/App.csproj":         `<Project Sdk="Microsoft.NET.Sdk">` + pkgA + `</Project>`,
		"Directory.Packages.props":   `<Project><PropertyGroup><ManagePackageVersionsCentrally Condition="'$(EnableCPM)' == 'true'">true</ManagePackageVersionsCentrally><CentralPackageTransitivePinningEnabled>true</CentralPackageTransitivePinningEnabled></PropertyGroup><ItemGroup Condition="'$(TargetFramework)' == 'net10.0'"><PackageVersion Include="A" Version="$(CentralVersion)" /></ItemGroup></Project>`,
		"src/App/packages.lock.json": lock,
	}, true)
	r, err := Analyze(context.Background(), in, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	c := r.Contexts[0]
	if c.AssociationState != "observed" || c.Checks[0].Status != "match" || r.Status != "complete" {
		t.Fatalf("version-only central input changed direct-ID presence claim: %+v", c)
	}

	in = testInput([]declarations.ProjectRecord{record}, map[string]string{
		"src/App/App.csproj":    `<Project Sdk="Microsoft.NET.Sdk" />`,
		"Directory.Build.props": `<Project><ItemGroup><PackageReference Include="FromShared" Version="1.0" /></ItemGroup></Project>`,
	}, true)
	r, err = Analyze(context.Background(), in, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	// A statically kept PackageReference from Directory.Build.props applies
	// to the project, so the absent conventional lock is missing.
	if c := r.Contexts[0]; c.AssociationState != "missing" || c.OutcomeReason() != "lockfile-not-present" || c.NuGetEvidence.LockPathBasis != "conventional" {
		t.Fatalf("shared PackageReference did not make the lock expected: %+v", c)
	}
}

func TestNuGetLockPathUsesProvenOrderedLiteralLayersAndImports(t *testing.T) {
	record := nugetRecord("src/App", "src/App/App.csproj", declarations.Requirement{Kind: "package-reference", Value: "A@1.0", State: "declared"})
	lock := `{"version":1,"dependencies":{"net8.0":{"A":{"type":"Direct"}}}}`
	for _, tc := range []struct {
		name     string
		content  map[string]string
		wantPath string
	}{
		{
			name: "project overrides default props",
			content: map[string]string{
				"Directory.Build.props": `<Project><PropertyGroup><NuGetLockFilePath>props.lock</NuGetLockFilePath></PropertyGroup></Project>`,
				"src/App/App.csproj":    `<Project Sdk="Microsoft.NET.Sdk"><PropertyGroup><NuGetLockFilePath>project.lock</NuGetLockFilePath></PropertyGroup>` + pkgA + `</Project>`,
				"src/App/project.lock":  lock,
			},
			wantPath: "src/App/project.lock",
		},
		{
			name: "default targets overrides project",
			content: map[string]string{
				"Directory.Build.props":   `<Project><PropertyGroup><NuGetLockFilePath>props.lock</NuGetLockFilePath></PropertyGroup></Project>`,
				"src/App/App.csproj":      `<Project Sdk="Microsoft.NET.Sdk"><PropertyGroup><NuGetLockFilePath>project.lock</NuGetLockFilePath></PropertyGroup>` + pkgA + `</Project>`,
				"Directory.Build.targets": `<Project><PropertyGroup><NuGetLockFilePath>targets.lock</NuGetLockFilePath></PropertyGroup></Project>`,
				"src/App/targets.lock":    lock,
			},
			wantPath: "src/App/targets.lock",
		},
		{
			name: "later bounded props import wins",
			content: map[string]string{
				"Directory.Build.props": `<Project><Import Project="A.props" /><Import Project="B.props" /></Project>`,
				"A.props":               `<Project><PropertyGroup><NuGetLockFilePath>first.lock</NuGetLockFilePath></PropertyGroup></Project>`,
				"B.props":               `<Project><PropertyGroup><NuGetLockFilePath>second.lock</NuGetLockFilePath></PropertyGroup></Project>`,
				"src/App/App.csproj":    `<Project Sdk="Microsoft.NET.Sdk">` + pkgA + `</Project>`,
				"src/App/second.lock":   lock,
			},
			wantPath: "src/App/second.lock",
		},
		{
			name: "reversed bounded props imports reverse result",
			content: map[string]string{
				"Directory.Build.props": `<Project><Import Project="B.props" /><Import Project="A.props" /></Project>`,
				"A.props":               `<Project><PropertyGroup><NuGetLockFilePath>first.lock</NuGetLockFilePath></PropertyGroup></Project>`,
				"B.props":               `<Project><PropertyGroup><NuGetLockFilePath>second.lock</NuGetLockFilePath></PropertyGroup></Project>`,
				"src/App/App.csproj":    `<Project Sdk="Microsoft.NET.Sdk">` + pkgA + `</Project>`,
				"src/App/first.lock":    lock,
			},
			wantPath: "src/App/first.lock",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, err := Analyze(context.Background(), testInput([]declarations.ProjectRecord{record}, tc.content, true), Limits{})
			if err != nil {
				t.Fatal(err)
			}
			got := r.Contexts[0]
			if got.AssociationState != "observed" || got.LockfilePath != tc.wantPath || got.Checks[0].Status != "match" {
				t.Fatalf("ordered literal custom path was not selected: %+v", got)
			}
		})
	}
}

func TestNuGetThisFileDirectoryImportDoesNotDoubleJoinOrTrustDecoy(t *testing.T) {
	record := nugetRecord("build/app", "build/app/App.csproj", declarations.Requirement{Kind: "package-reference", Value: "Foo@1.0", State: "declared"})
	lock := `{"version":1,"dependencies":{"net8.0":{"Foo":{"type":"Direct"}}}}`
	in := testInput([]declarations.ProjectRecord{record}, map[string]string{
		"build/Directory.Build.props":  `<Project><Import Project="$(MSBuildThisFileDirectory)common.props" /></Project>`,
		"build/common.props":           `<Project><ItemGroup><PackageReference Include="Hidden" Version="1.0" /></ItemGroup></Project>`,
		"build/build/common.props":     `<Project />`,
		"build/app/App.csproj":         `<Project Sdk="Microsoft.NET.Sdk"><ItemGroup><PackageReference Include="Foo" Version="1.0" /></ItemGroup></Project>`,
		"build/app/packages.lock.json": lock,
	}, true)
	r, err := Analyze(context.Background(), in, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	got := r.Contexts[0]
	// The import resolves to build/common.props, not the doubled-path decoy,
	// so its unconditional Hidden reference is compared like the project's.
	if got.AssociationState != "observed" || len(got.Checks) != 1 || got.Checks[0].Status != "different" || !slices.Equal(got.Checks[0].Missing, []string{"hidden"}) {
		t.Fatalf("real imported PackageReference was bypassed through a doubled-path decoy: %+v", got)
	}
}

func TestNuGetImplicitDirectoryTargetsRequireKnownSDKImportModel(t *testing.T) {
	record := nugetRecord("src/App", "src/App/App.csproj", declarations.Requirement{Kind: "package-reference", Value: "Foo@1.0", State: "declared"})
	lock := `{"version":1,"dependencies":{"net10.0":{"Foo":{"type":"Direct"}}}}`
	for _, tc := range []struct {
		name       string
		project    string
		shared     map[string]string
		wantState  string
		wantPath   string
		wantReason string
	}{
		{
			name:      "bare Project does not implicitly import ancestor targets",
			project:   `<Project><PropertyGroup><NuGetLockFilePath>right.lock.json</NuGetLockFilePath></PropertyGroup><ItemGroup><PackageReference Include="Foo" Version="1.0" /></ItemGroup></Project>`,
			shared:    map[string]string{"Directory.Build.targets": `<Project><PropertyGroup><NuGetLockFilePath>wrong.lock.json</NuGetLockFilePath></PropertyGroup></Project>`},
			wantState: "observed", wantPath: "src/App/right.lock.json",
		},
		{
			name:      "recognized SDK imports default targets after project",
			project:   `<Project Sdk="Microsoft.NET.Sdk"><PropertyGroup><NuGetLockFilePath>right.lock.json</NuGetLockFilePath></PropertyGroup><ItemGroup><PackageReference Include="Foo" Version="1.0" /></ItemGroup></Project>`,
			shared:    map[string]string{"Directory.Build.targets": `<Project><PropertyGroup><NuGetLockFilePath>wrong.lock.json</NuGetLockFilePath></PropertyGroup></Project>`},
			wantState: "observed", wantPath: "src/App/wrong.lock.json",
		},
		{
			name:      "recognized Web SDK imports default targets after project",
			project:   `<Project Sdk="Microsoft.NET.Sdk.Web"><PropertyGroup><NuGetLockFilePath>right.lock.json</NuGetLockFilePath></PropertyGroup><ItemGroup><PackageReference Include="Foo" Version="1.0" /></ItemGroup></Project>`,
			shared:    map[string]string{"Directory.Build.targets": `<Project><PropertyGroup><NuGetLockFilePath>wrong.lock.json</NuGetLockFilePath></PropertyGroup></Project>`},
			wantState: "observed", wantPath: "src/App/wrong.lock.json",
		},
		{
			name:      "recognized Razor SDK imports default targets after project",
			project:   `<Project Sdk="Microsoft.NET.Sdk.Razor"><PropertyGroup><NuGetLockFilePath>right.lock.json</NuGetLockFilePath></PropertyGroup><ItemGroup><PackageReference Include="Foo" Version="1.0" /></ItemGroup></Project>`,
			shared:    map[string]string{"Directory.Build.targets": `<Project><PropertyGroup><NuGetLockFilePath>wrong.lock.json</NuGetLockFilePath></PropertyGroup></Project>`},
			wantState: "observed", wantPath: "src/App/wrong.lock.json",
		},
		{
			name:      "recognized Worker SDK imports default targets after project",
			project:   `<Project Sdk="Microsoft.NET.Sdk.Worker"><PropertyGroup><NuGetLockFilePath>right.lock.json</NuGetLockFilePath></PropertyGroup><ItemGroup><PackageReference Include="Foo" Version="1.0" /></ItemGroup></Project>`,
			shared:    map[string]string{"Directory.Build.targets": `<Project><PropertyGroup><NuGetLockFilePath>wrong.lock.json</NuGetLockFilePath></PropertyGroup></Project>`},
			wantState: "observed", wantPath: "src/App/wrong.lock.json",
		},
		{
			name:      "unknown SDK chain is unmodeled",
			project:   `<Project Sdk="Acme.Custom"><PropertyGroup><NuGetLockFilePath>right.lock.json</NuGetLockFilePath></PropertyGroup><ItemGroup><PackageReference Include="Foo" Version="1.0" /></ItemGroup></Project>`,
			shared:    map[string]string{"Directory.Build.targets": `<Project><PropertyGroup><NuGetLockFilePath>wrong.lock.json</NuGetLockFilePath></PropertyGroup></Project>`},
			wantState: "indeterminate", wantReason: "nuget-msbuild-input-unmodeled",
		},
		{
			name:      "project can disable default targets",
			project:   `<Project Sdk="Microsoft.NET.Sdk"><PropertyGroup><ImportDirectoryBuildTargets>false</ImportDirectoryBuildTargets><NuGetLockFilePath>right.lock.json</NuGetLockFilePath></PropertyGroup><ItemGroup><PackageReference Include="Foo" Version="1.0" /></ItemGroup></Project>`,
			shared:    map[string]string{"Directory.Build.targets": `<Project><PropertyGroup><NuGetLockFilePath>wrong.lock.json</NuGetLockFilePath></PropertyGroup></Project>`},
			wantState: "observed", wantPath: "src/App/right.lock.json",
		},
		{
			name:      "dynamic targets enable control is conditional",
			project:   `<Project Sdk="Microsoft.NET.Sdk"><PropertyGroup><ImportDirectoryBuildTargets>$(ImportTargets)</ImportDirectoryBuildTargets><NuGetLockFilePath>right.lock.json</NuGetLockFilePath></PropertyGroup><ItemGroup><PackageReference Include="Foo" Version="1.0" /></ItemGroup></Project>`,
			shared:    map[string]string{"Directory.Build.targets": `<Project><PropertyGroup><NuGetLockFilePath>wrong.lock.json</NuGetLockFilePath></PropertyGroup></Project>`},
			wantState: "indeterminate", wantReason: "nuget-lock-path-conditional",
		},
		{
			name:    "props can disable default targets",
			project: `<Project Sdk="Microsoft.NET.Sdk"><PropertyGroup><NuGetLockFilePath>right.lock.json</NuGetLockFilePath></PropertyGroup><ItemGroup><PackageReference Include="Foo" Version="1.0" /></ItemGroup></Project>`,
			shared: map[string]string{
				"Directory.Build.props":   `<Project><PropertyGroup><ImportDirectoryBuildTargets>false</ImportDirectoryBuildTargets></PropertyGroup></Project>`,
				"Directory.Build.targets": `<Project><PropertyGroup><NuGetLockFilePath>wrong.lock.json</NuGetLockFilePath></PropertyGroup></Project>`,
			},
			wantState: "observed", wantPath: "src/App/right.lock.json",
		},
		{
			name:    "imported props can disable default targets",
			project: `<Project Sdk="Microsoft.NET.Sdk"><PropertyGroup><NuGetLockFilePath>right.lock.json</NuGetLockFilePath></PropertyGroup><ItemGroup><PackageReference Include="Foo" Version="1.0" /></ItemGroup></Project>`,
			shared: map[string]string{
				"Directory.Build.props":   `<Project><Import Project="controls.props" /></Project>`,
				"controls.props":          `<Project><PropertyGroup><ImportDirectoryBuildTargets>false</ImportDirectoryBuildTargets></PropertyGroup></Project>`,
				"Directory.Build.targets": `<Project><PropertyGroup><NuGetLockFilePath>wrong.lock.json</NuGetLockFilePath></PropertyGroup></Project>`,
			},
			wantState: "observed", wantPath: "src/App/right.lock.json",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			content := map[string]string{
				"src/App/App.csproj":      tc.project,
				"src/App/right.lock.json": lock,
				"src/App/wrong.lock.json": lock,
			}
			for p, v := range tc.shared {
				content[p] = v
			}
			r, err := Analyze(context.Background(), testInput([]declarations.ProjectRecord{record}, content, true), Limits{})
			if err != nil {
				t.Fatal(err)
			}
			got := r.Contexts[0]
			if got.AssociationState != tc.wantState {
				t.Fatalf("association=%s want %s: %+v", got.AssociationState, tc.wantState, got)
			}
			if tc.wantPath != "" && (got.LockfilePath != tc.wantPath || got.Checks[0].Status != "match") {
				t.Fatalf("selected path=%q want %q: %+v", got.LockfilePath, tc.wantPath, got)
			}
			if tc.wantReason != "" && (got.OutcomeReason() != tc.wantReason || len(got.Checks) != 0) {
				t.Fatalf("uncertainty reason=%q want %q: %+v", got.OutcomeReason(), tc.wantReason, got)
			}
		})
	}
}

func TestNuGetCustomAndConventionalPathCollisionIsSymmetric(t *testing.T) {
	customOwner := nugetRecord("src/A", "src/A/A.csproj", declarations.Requirement{Kind: "package-reference", Value: "A@1.0", State: "declared"})
	conventionalOwner := nugetRecord("src/B", "src/B/B.csproj", declarations.Requirement{Kind: "package-reference", Value: "B@1.0", State: "declared"})
	lock := `{"version":1,"dependencies":{"net8.0":{"A":{"type":"Direct"},"B":{"type":"Direct"}}}}`
	in := testInput([]declarations.ProjectRecord{customOwner, conventionalOwner}, map[string]string{
		"src/A/A.csproj":           `<Project><PropertyGroup><NuGetLockFilePath>../B/packages.lock.json</NuGetLockFilePath></PropertyGroup></Project>`,
		"src/B/B.csproj":           `<Project />`,
		"src/B/packages.lock.json": lock,
	}, true)
	r, err := Analyze(context.Background(), in, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Contexts) != 2 {
		t.Fatalf("expected both contexts: %+v", r.Contexts)
	}
	for _, got := range r.Contexts {
		if got.AssociationState != "indeterminate" || got.OutcomeReason() != "ambiguous-nuget-lockfile-owner" || len(got.Checks) != 0 {
			t.Errorf("shared custom/default path had asymmetric ownership: %+v", got)
		}
	}
}

func TestNuGetMSBuildReservedPathAnchors(t *testing.T) {
	// MSBuild property names are case-insensitive, and
	// $(MSBuildProjectDirectory) has no trailing separator, so a following
	// name concatenates onto the directory name (SDK 10.0.401 check).
	for _, tc := range []struct {
		raw, definedIn, manifest string
		want                     string
		anchored                 bool
		status                   anchorStatus
	}{
		{"$(MSBuildProjectDirectory)/locks/a.data", "src/App/App.csproj", "src/App/App.csproj", "src/App/locks/a.data", true, anchorOK},
		{"$(msbuildprojectdirectory)/a.data", "src/App/App.csproj", "src/App/App.csproj", "src/App/a.data", true, anchorOK},
		{"$(MSBuildProjectDirectory)lock.json", "src/App/App.csproj", "src/App/App.csproj", "src/Applock.json", true, anchorOK},
		{"$(MSBuildProjectDirectory)lock.json", "App.csproj", "App.csproj", "", true, anchorOutside},
		{"prefix/$(MSBuildProjectDirectory)/a.data", "src/App/App.csproj", "src/App/App.csproj", "", false, anchorDynamic},
		{"$(MSBuildThisFileDirectory)common.props", "build/Directory.Build.props", "src/App/App.csproj", "build/common.props", true, anchorOK},
		{"$(MSBuildThisFileDirectory)common.props", "Directory.Build.props", "App.csproj", "common.props", true, anchorOK},
		{"$(MSBuildProjectName).lock.json", "src/App/App.csproj", "src/App/App.csproj", "App.lock.json", false, anchorOK},
	} {
		got, anchored, status := resolveMSBuildAnchors(tc.raw, tc.definedIn, tc.manifest)
		if got != tc.want || anchored != tc.anchored || status != tc.status {
			t.Errorf("resolveMSBuildAnchors(%q, %q, %q) = %q, %v, %v; want %q, %v, %v", tc.raw, tc.definedIn, tc.manifest, got, anchored, status, tc.want, tc.anchored, tc.status)
		}
	}
	for _, tc := range []struct {
		raw  string
		want []nugetLockValue
	}{
		{"", []nugetLockValue{{kind: nugetLockDefault}}},
		{"locks/a.data", []nugetLockValue{{kind: nugetLockLiteral, value: "src/App/locks/a.data"}}},
		{"../../../x.json", []nugetLockValue{{kind: nugetLockOutside}}},
		{"/abs/x.json", []nugetLockValue{{kind: nugetLockOutside}}},
		{"$(Root)/x.json", []nugetLockValue{{kind: nugetLockOpen}}},
		{"locks/$(TargetFramework).json", []nugetLockValue{{kind: nugetLockPattern, value: "src/App/locks/*.json"}, {kind: nugetLockOpen}}},
		{`locks\a.data`, []nugetLockValue{{kind: nugetLockLiteral, value: "src/App/locks/a.data"}, {kind: nugetLockLiteral, value: `src/App/locks\a.data`}}},
	} {
		got := nugetLockValues(tc.raw, "src/App/App.csproj", "src/App/App.csproj")
		if len(got) != len(tc.want) {
			t.Errorf("nugetLockValues(%q) = %+v; want %+v", tc.raw, got, tc.want)
			continue
		}
		for i := range got {
			if got[i].kind != tc.want[i].kind || got[i].value != tc.want[i].value {
				t.Errorf("nugetLockValues(%q)[%d] = %+v; want %+v", tc.raw, i, got[i], tc.want[i])
			}
		}
	}
	if got := nugetDefaultName("src/My App/My App.csproj"); got != "packages.My_App.lock.json" {
		t.Errorf("default name with spaces = %q", got)
	}
}

func TestNuGetForeignXMLNamespaceCannotAssertMSBuildOwnership(t *testing.T) {
	record := nugetRecord("src/App", "src/App/App.csproj", declarations.Requirement{Kind: "package-reference", Value: "A@1.0", State: "declared"})
	lock := `{"version":1,"dependencies":{"net8.0":{"A":{"type":"Direct"}}}}`
	in := testInput([]declarations.ProjectRecord{record}, map[string]string{
		"src/App/App.csproj":  `<Project xmlns="urn:not-msbuild"><PropertyGroup><NuGetLockFilePath>custom.data</NuGetLockFilePath></PropertyGroup></Project>`,
		"src/App/custom.data": lock,
	}, true)
	r, err := Analyze(context.Background(), in, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	got := r.Contexts[0]
	if got.AssociationState != "indeterminate" || len(got.Checks) != 0 {
		t.Fatalf("foreign-namespace elements were treated as MSBuild: %+v", got)
	}
}

func TestNuGetOwnershipSurvivesLockfileContentFailures(t *testing.T) {
	record := nugetRecord("src/App", "src/App/App.csproj", declarations.Requirement{Kind: "package-reference", Value: "A@1.0", State: "declared"})
	for _, tc := range []struct {
		name   string
		lock   string
		limits Limits
		state  string
		reason string
	}{
		{name: "invalid JSON", lock: `{`, state: "unsupported", reason: "invalid-lockfile-json"},
		{name: "unsupported format", lock: `{"version":9,"dependencies":{}}`, state: "unsupported", reason: "unsupported-nuget-lockfile-version"},
		{name: "file read limit", lock: `{"version":1,"dependencies":{}}`, limits: Limits{FileBytes: 20}, state: "indeterminate", reason: "lockfile-unreadable"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			in := testInput([]declarations.ProjectRecord{record}, map[string]string{
				"src/App/App.csproj":         `<Project />`,
				"src/App/packages.lock.json": tc.lock,
			}, true)
			r, err := Analyze(context.Background(), in, tc.limits)
			if err != nil {
				t.Fatal(err)
			}
			got := r.Contexts[0]
			if got.AssociationState != tc.state || got.NuGetEvidence == nil || got.NuGetEvidence.OwnershipState != "observed" || got.NuGetEvidence.PresenceState != "observed" || got.LockfilePath != "src/App/packages.lock.json" || got.OutcomeReason() != tc.reason {
				t.Fatalf("content failure erased independent path ownership: %+v", got)
			}
			if err := ValidateReport(r); err != nil {
				t.Fatalf("valid report rejected: %v", err)
			}
		})
	}
}

func TestNuGetInvalidMSBuildElementCaseCannotAssertCustomOwnership(t *testing.T) {
	record := nugetRecord("src/App", "src/App/App.csproj", declarations.Requirement{Kind: "package-reference", Value: "A@1.0", State: "declared"})
	for _, project := range []string{
		`<project><PropertyGroup><NuGetLockFilePath>custom.data</NuGetLockFilePath></PropertyGroup></project>`,
		`<Project><propertygroup><NuGetLockFilePath>custom.data</NuGetLockFilePath></propertygroup></Project>`,
		`<Project><import Project="Shared.props" /><PropertyGroup><NuGetLockFilePath>custom.data</NuGetLockFilePath></PropertyGroup></Project>`,
	} {
		t.Run(project, func(t *testing.T) {
			in := testInput([]declarations.ProjectRecord{record}, map[string]string{
				"src/App/App.csproj":         project,
				"src/App/custom.data":        `{"version":1,"dependencies":{"net8.0":{"A":{"type":"Direct"}}}}`,
				"src/App/packages.lock.json": `{"version":1,"dependencies":{"net8.0":{"A":{"type":"Direct"}}}}`,
			}, true)
			r, err := Analyze(context.Background(), in, Limits{})
			if err != nil {
				t.Fatal(err)
			}
			got := r.Contexts[0]
			if got.AssociationState != "indeterminate" || got.NuGetEvidence.PresenceState != "observed" || got.NuGetEvidence.OwnershipState != "indeterminate" || len(got.Checks) != 0 {
				t.Fatalf("invalid MSBuild element casing promoted ownership: %+v", got)
			}
		})
	}
}

func TestNuGetCustomPathCaseAliasDoesNotClaimAbsence(t *testing.T) {
	record := nugetRecord("src/App", "src/App/App.csproj", declarations.Requirement{Kind: "package-reference", Value: "A@1.0", State: "declared"})
	lock := `{"version":1,"dependencies":{"net8.0":{"A":{"type":"Direct"}}}}`
	in := testInput([]declarations.ProjectRecord{record}, map[string]string{
		"src/App/App.csproj":  `<Project><PropertyGroup><NuGetLockFilePath>custom.data</NuGetLockFilePath></PropertyGroup></Project>`,
		"src/App/CUSTOM.data": lock,
	}, true)
	r, err := Analyze(context.Background(), in, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	got := r.Contexts[0]
	if got.AssociationState != "indeterminate" || got.NuGetEvidence.PresenceState != "observed" || got.NuGetEvidence.CandidateCount != 1 || got.NuGetEvidence.CandidatePaths[0] != "src/App/CUSTOM.data" {
		t.Fatalf("case-alias candidate was mistaken for absence or exact ownership: %+v", got)
	}
}

func TestNuGetPresenceIsIndependentFromOwnershipAndDirectCheck(t *testing.T) {
	record := nugetRecord("src/App", "src/App/App.csproj", declarations.Requirement{Kind: "package-reference", Value: "A@1.0", State: "declared"})
	lock := `{"version":1,"dependencies":{"net8.0":{"A":{"type":"Direct"}}}}`
	in := testInput([]declarations.ProjectRecord{record}, map[string]string{
		"src/App/App.csproj":         `<Project><Import Project="$(SharedProps)" /><ItemGroup><PackageReference Include="A" /></ItemGroup></Project>`,
		"src/App/packages.lock.json": lock,
	}, true)
	r, err := Analyze(context.Background(), in, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	c := r.Contexts[0]
	if c.NuGetEvidence == nil || c.NuGetEvidence.PresenceState != "observed" || c.NuGetEvidence.CandidateCount != 1 || len(c.NuGetEvidence.CandidatePaths) != 1 {
		t.Fatalf("known candidate presence was lost: %+v", c)
	}
	if c.AssociationState != "indeterminate" || len(c.Checks) != 0 {
		t.Fatalf("unknown ownership was conflated with presence: %+v", c)
	}
}

func TestNuGetExplicitMSBuildImportIsEvaluated(t *testing.T) {
	record := nugetRecord("src/App", "src/App/App.csproj", declarations.Requirement{Kind: "package-reference", Value: "Declared@1.0", State: "declared"})
	lock := `{"version":1,"dependencies":{"net8.0":{"Declared":{"type":"Direct"},"Imported":{"type":"Direct"}}}}`
	project := `<Project Sdk="Microsoft.NET.Sdk"><Import Project="../Shared.props" /><ItemGroup><PackageReference Include="Declared" Version="1.0" /></ItemGroup></Project>`
	for _, tc := range []struct {
		name, shared string
		want, check  string
		reason       string
	}{
		{"selected import adds a reference", `<Project><ItemGroup><PackageReference Include="Imported" Version="1.0" /></ItemGroup></Project>`, "observed", "match", ""},
		{"selected import sets a conditional lock path", `<Project><PropertyGroup Condition="'$(CI)' == 'true'"><NuGetLockFilePath>ci.lock.json</NuGetLockFilePath></PropertyGroup></Project>`, "indeterminate", "", "nuget-lock-path-conditional"},
		{"absent unconditional import fails evaluation", "", "indeterminate", "", "nuget-project-config-unresolved"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			content := map[string]string{"src/App/App.csproj": project, "src/App/packages.lock.json": lock, "src/App/ci.lock.json": lock}
			if tc.shared != "" {
				content["src/Shared.props"] = tc.shared
			}
			r, err := Analyze(context.Background(), testInput([]declarations.ProjectRecord{record}, content, true), Limits{})
			if err != nil {
				t.Fatal(err)
			}
			if err := ValidateReport(r); err != nil {
				t.Fatal(err)
			}
			c := r.Contexts[0]
			if c.AssociationState != tc.want || tc.check != "" && (len(c.Checks) != 1 || c.Checks[0].Status != tc.check) || tc.reason != "" && c.OutcomeReason() != tc.reason {
				t.Fatalf("explicit import: %+v", c)
			}
			if tc.reason != "" && (len(c.NuGetEvidence.Causes) == 0 || c.NuGetEvidence.Causes[0].Path == "") {
				t.Fatalf("uncertainty has no named cause: %+v", c.NuGetEvidence)
			}
		})
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

func TestCancellationAfterSuccessfulLockReadIsFatal(t *testing.T) {
	rec := npmRecord("app", npmRef("a@1.0.0", "dependencies"))
	lock := `{"lockfileVersion":3,"packages":{"":{"dependencies":{"a":"1.0.0"}}}}`
	in := testInput([]declarations.ProjectRecord{rec}, map[string]string{"app/package-lock.json": lock}, true)
	ctx, cancel := context.WithCancel(context.Background())
	in.ReadSelected = func(context.Context, string, int64) ([]byte, int64, error) {
		cancel()
		return []byte(lock), int64(len(lock)), nil
	}
	if _, err := Analyze(ctx, in, Limits{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("successful read that canceled the caller context returned %v; want context.Canceled", err)
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

func TestInvalidPathsAndProjectRecordCoverageRemainValidReports(t *testing.T) {
	good := npmRecord("app", npmRef("a@1.0.0", "dependencies"))
	invalidProjects := []declarations.ProjectRecord{
		npmRecord("bad\x00nul", npmRef("b@1.0.0", "dependencies")),
		npmRecord(string([]byte{'b', 'a', 'd', 0xff}), npmRef("c@1.0.0", "dependencies")),
		npmRecord(strings.Repeat("x", 8193), npmRef("d@1.0.0", "dependencies")),
	}
	records := append([]declarations.ProjectRecord{good}, invalidProjects...)
	in := testInput(records, map[string]string{
		"app/package-lock.json": `{"lockfileVersion":3,"packages":{"":{"dependencies":{"a":"1.0.0"}}}}`,
	}, true)
	in.Inventory = append(in.Inventory,
		File{Path: "bad\x00nul/package-lock.json", Size: 1},
		File{Path: string([]byte{'b', 'a', 'd', 0xff}) + "/package-lock.json", Size: 1},
		File{Path: strings.Repeat("x", 8193) + "/package-lock.json", Size: 1},
	)
	r, err := Analyze(context.Background(), in, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	if r.Status != "partial" || r.Coverage.ProjectRecords != 4 || r.Coverage.OmittedContexts != 3 || len(r.Contexts) != 1 {
		t.Fatalf("invalid records were not accounted for: %+v", r)
	}
	if err := ValidateReport(r); err != nil {
		t.Fatalf("analysis produced a report that fails its own validator: %v", err)
	}
	encoded, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "bad\\u0000nul") {
		t.Fatalf("invalid path leaked into report: %s", encoded)
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
