package lockfiles

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/war-and-code/dircue/pkg/declarations"
	"github.com/war-and-code/dircue/pkg/projects"
)

// nugetReasonInput builds a minimal input for a single NuGet project at
// root/id with the given lockfile content and a package-reference declaration
// so that association reaches "missing" (not "not_applicable") when the lock
// is absent.
func nugetReasonInput(root, id, lockPath, lockContent string) Input {
	req := declarations.Requirement{Kind: "package-reference", Value: "A@1.0", State: "declared"}
	record := nugetRecord(root, id, req)
	files := map[string]string{lockPath: lockContent}
	return testInput([]declarations.ProjectRecord{record}, files, true)
}

// nugetReasonContext runs Analyze and returns the context for the given
// manifest path. It does NOT call ValidateReport because some expected states
// ("unsupported") pair with a "partial" report status, which is fine.
func nugetReasonContext(t *testing.T, in Input, manifest string) Context {
	t.Helper()
	r, err := Analyze(context.Background(), in, Limits{})
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	c := contextByManifest(r, manifest)
	if c.ManifestPath == "" {
		t.Fatalf("no context for %s: contexts=%+v diagnostics=%+v", manifest, r.Contexts, r.Diagnostics)
	}
	return c
}

func TestNuGetUpdateOnlyProjectIsNotReportedAsMissingLock(t *testing.T) {
	manifest := "App.csproj"
	data := []byte(`<Project><ItemGroup Condition="'$(TargetFramework)' == 'net8.0'"><PackageReference Update="Imported.Package" Version="2.0" /></ItemGroup></Project>`)
	doc := projects.ParseDotnet(manifest, data)
	if len(doc.Projects) != 1 {
		t.Fatalf("projects: %+v", doc.Projects)
	}
	for _, req := range doc.Projects[0].Requirements {
		if req.Kind == "package-reference" {
			t.Fatalf("Update was treated as a direct package declaration: %+v", req)
		}
	}
	project := doc.Projects[0]
	record := declarations.ProjectRecord{
		Project:  declarations.Project{ID: project.ID, Root: project.Root, Kind: project.Kind, Requirements: project.Requirements},
		Parsed:   true,
		Complete: true,
	}
	in := testInput([]declarations.ProjectRecord{record}, map[string]string{manifest: string(data)}, true)
	got := nugetReasonContext(t, in, manifest)
	if got.AssociationState != "not_applicable" {
		t.Fatalf("Update-only project was reported as requiring a lockfile: %+v", got)
	}
}

// nugetReasonBoundaryReason returns the first boundary reason on a context, or
// "".
func nugetReasonBoundaryReason(c Context) string {
	if len(c.Boundaries) == 0 {
		return ""
	}
	return c.Boundaries[0].Reason
}

func TestNuGetReasonLockfileParseCodes(t *testing.T) {
	// All parse-error codes produce AssociationState=="unsupported" and a
	// boundary reason equal to the code.
	for _, tc := range []struct {
		code    string
		content string
	}{
		{
			"unsupported-nuget-lockfile-version",
			`{"version":99,"dependencies":{"net8.0":{}}}`,
		},
		{
			"nuget-targets-missing",
			`{"version":1,"dependencies":{}}`,
		},
		{
			"duplicate-nuget-package-id",
			`{"version":1,"dependencies":{"net8.0":{"Alpha":{"type":"Direct"},"alpha":{"type":"Transitive"}}}}`,
		},
		{
			"invalid-nuget-target",
			`{"version":1,"dependencies":{"net8.0":"not-an-object"}}`,
		},
		{
			"invalid-nuget-package-entry",
			`{"version":1,"dependencies":{"net8.0":{"A":"not-an-object"}}}`,
		},
		{
			"nuget-package-type-missing",
			`{"version":1,"dependencies":{"net8.0":{"A":{}}}}`,
		},
		{
			"unsupported-nuget-package-type",
			`{"version":1,"dependencies":{"net8.0":{"A":{"type":"unknown-kind"}}}}`,
		},
	} {
		t.Run(tc.code, func(t *testing.T) {
			in := nugetReasonInput("src/App", "src/App/App.csproj", "src/App/packages.lock.json", tc.content)
			c := nugetReasonContext(t, in, "src/App/App.csproj")
			if c.AssociationState != "unsupported" {
				t.Fatalf("code=%s: AssociationState=%q want unsupported; context=%+v", tc.code, c.AssociationState, c)
			}
			got := nugetReasonBoundaryReason(c)
			if got != tc.code {
				t.Fatalf("code=%s: boundary reason=%q want %q; context=%+v", tc.code, got, tc.code, c)
			}
		})
	}
}

func TestNuGetReasonNuGetProjectOrCentralTransitiveUnexamined(t *testing.T) {
	// "nuget-project-or-central-transitive-unexamined" is returned as
	// checkReason from compareNuGet and appended as a boundary reason.
	lock := `{"version":1,"dependencies":{"net8.0":{"A":{"type":"Direct"},"B":{"type":"Project"}}}}`
	in := nugetReasonInput("src/App", "src/App/App.csproj", "src/App/packages.lock.json", lock)
	c := nugetReasonContext(t, in, "src/App/App.csproj")
	if c.AssociationState != "observed" {
		t.Fatalf("AssociationState=%q want observed; context=%+v", c.AssociationState, c)
	}
	const want = "nuget-project-or-central-transitive-unexamined"
	for _, b := range c.Boundaries {
		if b.Reason == want {
			return
		}
	}
	t.Fatalf("boundary reason %q not found; boundaries=%+v", want, c.Boundaries)
}

func TestNuGetReasonMultipleTargetFrameworks(t *testing.T) {
	// "multiple-target-frameworks" is returned as checkReason when the lock
	// has more than one target framework.
	lock := `{"version":1,"dependencies":{"net6.0":{"A":{"type":"Direct"}},"net8.0":{"A":{"type":"Direct"}}}}`
	in := nugetReasonInput("src/App", "src/App/App.csproj", "src/App/packages.lock.json", lock)
	c := nugetReasonContext(t, in, "src/App/App.csproj")
	if c.AssociationState != "observed" {
		t.Fatalf("AssociationState=%q want observed; context=%+v", c.AssociationState, c)
	}
	const want = "multiple-target-frameworks"
	for _, b := range c.Boundaries {
		if b.Reason == want {
			return
		}
	}
	t.Fatalf("boundary reason %q not found; boundaries=%+v", want, c.Boundaries)
}

func TestNuGetReasonNuGetLockfileOwnerUnresolved(t *testing.T) {
	// A lock candidate exists in the project's directory but its name does not
	// match "packages.lock.json" or "packages.App.lock.json".
	req := declarations.Requirement{Kind: "package-reference", Value: "A@1.0", State: "declared"}
	record := nugetRecord("src/App", "src/App/App.csproj", req)
	files := map[string]string{
		"src/App/App.csproj":               "<Project />",
		"src/App/packages.Other.lock.json": `{"version":1,"dependencies":{"net8.0":{"A":{"type":"Direct"}}}}`,
	}
	in := testInput([]declarations.ProjectRecord{record}, files, true)
	c := nugetReasonContext(t, in, "src/App/App.csproj")
	if c.AssociationState != "indeterminate" {
		t.Fatalf("AssociationState=%q want indeterminate; context=%+v", c.AssociationState, c)
	}
	const want = "nuget-lockfile-owner-unresolved"
	if nugetReasonBoundaryReason(c) != want {
		t.Fatalf("boundary reason=%q want %q; context=%+v", nugetReasonBoundaryReason(c), want, c)
	}
}

func TestNuGetReasonStaticModelCodes(t *testing.T) {
	// Each static-model reason names the selected file that caused it.
	lock := `{"version":1,"dependencies":{"net8.0":{"A":{"type":"Direct"}}}}`
	for _, tc := range []struct {
		reason, path string
		files        map[string]string
		check        bool
	}{
		{"nuget-msbuild-input-unmodeled", "src/App/App.csproj", map[string]string{
			"src/App/App.csproj": `<Project Sdk="Microsoft.Build.NoTargets/3.7.0">` + pkgA + `</Project>`,
		}, false},
		{"nuget-msbuild-input-unmodeled", "Directory.Build.props", map[string]string{
			"src/App/App.csproj":    `<Project Sdk="Microsoft.NET.Sdk">` + pkgA + `</Project>`,
			"Directory.Build.props": `<Project><Import Project="Sdk.props" Sdk="Microsoft.DotNet.Arcade.Sdk" /></Project>`,
		}, false},
		{"nuget-lock-path-conditional", "src/App/App.csproj", map[string]string{
			"src/App/App.csproj":         `<Project Sdk="Microsoft.NET.Sdk"><Choose><When Condition="'$(CI)' == 'true'"><PropertyGroup><NuGetLockFilePath>ci.lock.json</NuGetLockFilePath></PropertyGroup></When></Choose>` + pkgA + `</Project>`,
			"src/App/packages.lock.json": lock,
		}, false},
		{"nuget-lock-path-conditional", "src/App/App.csproj", map[string]string{
			"src/App/App.csproj":         `<Project Sdk="Microsoft.NET.Sdk"><Target Name="Pin" BeforeTargets="Restore"><PropertyGroup><NuGetLockFilePath>t.lock.json</NuGetLockFilePath></PropertyGroup></Target>` + pkgA + `</Project>`,
			"src/App/packages.lock.json": lock,
		}, false},
		{"nuget-custom-lock-path-unresolved", "Directory.Build.props", map[string]string{
			"src/App/App.csproj":    `<Project Sdk="Microsoft.NET.Sdk">` + pkgA + `</Project>`,
			"Directory.Build.props": `<Project><PropertyGroup><NuGetLockFilePath>$(LockRoot)/app.lock.json</NuGetLockFilePath></PropertyGroup></Project>`,
		}, false},
		{"nuget-package-reference-conditional", "src/App/App.csproj", map[string]string{
			"src/App/App.csproj":         `<Project Sdk="Microsoft.NET.Sdk">` + pkgA + `<ItemGroup Condition="'$(OS)' == 'Windows_NT'"><PackageReference Include="B" Version="1.0" /></ItemGroup></Project>`,
			"src/App/packages.lock.json": lock,
		}, true},
		{"nuget-package-reference-dynamic", "src/App/App.csproj", map[string]string{
			"src/App/App.csproj":         `<Project Sdk="Microsoft.NET.Sdk">` + pkgA + `<ItemGroup><PackageReference Include="$(ExtraPackage)" Version="1.0" /></ItemGroup></Project>`,
			"src/App/packages.lock.json": lock,
		}, true},
	} {
		t.Run(tc.reason+" "+tc.path, func(t *testing.T) {
			record := nugetRecord("src/App", "src/App/App.csproj", declarations.Requirement{Kind: "package-reference", Value: "A@1.0", State: "declared"})
			in := testInput([]declarations.ProjectRecord{record}, tc.files, true)
			r, err := Analyze(context.Background(), in, Limits{})
			if err != nil {
				t.Fatal(err)
			}
			if err := ValidateReport(r); err != nil {
				t.Fatal(err)
			}
			c := contextByManifest(r, "src/App/App.csproj")
			e := c.NuGetEvidence
			if tc.check {
				if c.AssociationState != "observed" || len(c.Checks) != 1 || c.Checks[0].Status != "indeterminate" || !slices.Contains(e.CheckReasons, tc.reason) || e.CheckState != "indeterminate" {
					t.Fatalf("check reason %q: %+v %+v", tc.reason, c, e)
				}
			} else if c.AssociationState != "indeterminate" || c.OutcomeReason() != tc.reason {
				t.Fatalf("ownership reason=%q want %q: %+v", c.OutcomeReason(), tc.reason, c)
			}
			if !slices.ContainsFunc(e.Causes, func(cause NuGetCause) bool {
				return cause.Reason == tc.reason && cause.Path == tc.path && cause.Detail != ""
			}) {
				t.Fatalf("no cause %s at %s: %+v", tc.reason, tc.path, e.Causes)
			}
		})
	}
}

func TestNuGetReasonFileReadError(t *testing.T) {
	// "file-read-error" appears as both a Boundary.Reason and a
	// Diagnostic.Code when ReadSelected returns an error and ErrorPolicy is
	// "continue".
	req := declarations.Requirement{Kind: "package-reference", Value: "A@1.0", State: "declared"}
	record := nugetRecord("src/App", "src/App/App.csproj", req)
	in := testInput([]declarations.ProjectRecord{record}, map[string]string{
		"src/App/App.csproj":         "<Project />",
		"src/App/packages.lock.json": `{"version":1,"dependencies":{"net8.0":{"A":{"type":"Direct"}}}}`,
	}, true)
	in.ErrorPolicy = "continue"
	in.ReadSelected = func(_ context.Context, p string, _ int64) ([]byte, int64, error) {
		if p == "src/App/App.csproj" {
			return []byte("<Project />"), int64(len("<Project />")), nil
		}
		return nil, 0, errors.New("simulated read error")
	}
	r, err := Analyze(context.Background(), in, Limits{})
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	c := contextByManifest(r, "src/App/App.csproj")
	if c.ManifestPath == "" {
		t.Fatalf("no context; diagnostics=%+v", r.Diagnostics)
	}
	if c.AssociationState != "indeterminate" {
		t.Fatalf("AssociationState=%q want indeterminate; context=%+v", c.AssociationState, c)
	}
	const want = "file-read-error"
	if nugetReasonBoundaryReason(c) != want {
		t.Fatalf("boundary reason=%q want %q; context=%+v", nugetReasonBoundaryReason(c), want, c)
	}
	for _, d := range r.Diagnostics {
		if d.Code == want {
			return
		}
	}
	t.Fatalf("diagnostic code %q not found; diagnostics=%+v", want, r.Diagnostics)
}

func TestNuGetReasonIncompleteLockfileRead(t *testing.T) {
	// "incomplete-lockfile-read" is emitted when ReadSelected returns a size
	// mismatch relative to the inventory entry.
	req := declarations.Requirement{Kind: "package-reference", Value: "A@1.0", State: "declared"}
	record := nugetRecord("src/App", "src/App/App.csproj", req)
	lockBody := `{"version":1,"dependencies":{"net8.0":{"A":{"type":"Direct"}}}}`
	in := testInput([]declarations.ProjectRecord{record}, map[string]string{
		"src/App/App.csproj":         "<Project />",
		"src/App/packages.lock.json": lockBody,
	}, true)
	// Override ReadSelected to report a different size for the lockfile.
	realRead := in.ReadSelected
	in.ReadSelected = func(ctx context.Context, p string, limit int64) ([]byte, int64, error) {
		data, size, err := realRead(ctx, p, limit)
		if p == "src/App/packages.lock.json" && err == nil {
			// Return more bytes than the data slice length to trigger mismatch.
			return data, size + 1, nil
		}
		return data, size, err
	}
	r, err := Analyze(context.Background(), in, Limits{})
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	c := contextByManifest(r, "src/App/App.csproj")
	if c.ManifestPath == "" {
		t.Fatalf("no context; diagnostics=%+v", r.Diagnostics)
	}
	if c.AssociationState != "indeterminate" {
		t.Fatalf("AssociationState=%q want indeterminate; context=%+v", c.AssociationState, c)
	}
	const want = "incomplete-lockfile-read"
	if nugetReasonBoundaryReason(c) != want {
		t.Fatalf("boundary reason=%q want %q; context=%+v", nugetReasonBoundaryReason(c), want, c)
	}
	for _, d := range r.Diagnostics {
		if d.Code == want {
			return
		}
	}
	t.Fatalf("diagnostic code %q not found; diagnostics=%+v", want, r.Diagnostics)
}

func TestNuGetReasonInvalidInventoryPath(t *testing.T) {
	// "invalid-inventory-path" is emitted as a Diagnostic when an inventory
	// file has a non-root-relative path (e.g. contains "..").
	req := declarations.Requirement{Kind: "package-reference", Value: "A@1.0", State: "declared"}
	record := nugetRecord("src/App", "src/App/App.csproj", req)
	in := testInput([]declarations.ProjectRecord{record}, map[string]string{
		"src/App/App.csproj": "<Project />",
	}, true)
	// Inject an invalid inventory path.
	in.Inventory = append(in.Inventory, File{Path: "../etc/passwd", Size: 5})
	r, err := Analyze(context.Background(), in, Limits{})
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	const want = "invalid-inventory-path"
	for _, d := range r.Diagnostics {
		if d.Code == want {
			return
		}
	}
	t.Fatalf("diagnostic code %q not found; diagnostics=%+v", want, r.Diagnostics)
}

func TestNuGetReasonInvalidProjectRecord(t *testing.T) {
	// "invalid-project-record" is emitted as a Diagnostic when a ProjectRecord
	// has a path that is not a confined root-relative path.
	req := declarations.Requirement{Kind: "package-reference", Value: "A@1.0", State: "declared"}
	// Build an invalid record directly (bad ID with "..").
	badRecord := declarations.ProjectRecord{
		Parsed: true, Complete: true,
		Project: declarations.Project{
			ID:           "../outside/App.csproj",
			Root:         "../outside",
			Kind:         "dotnet",
			Requirements: []declarations.Requirement{req},
		},
	}
	in := Input{
		Source:            "directory",
		InventoryComplete: true,
		Declarations:      declarations.Report{Status: "complete"},
		ProjectRecords:    []declarations.ProjectRecord{badRecord},
		Inventory:         []File{},
		ReadSelected:      func(_ context.Context, _ string, _ int64) ([]byte, int64, error) { return nil, 0, errors.New("no") },
	}
	r, err := Analyze(context.Background(), in, Limits{})
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	const want = "invalid-project-record"
	for _, d := range r.Diagnostics {
		if d.Code == want {
			return
		}
	}
	t.Fatalf("diagnostic code %q not found; diagnostics=%+v", want, r.Diagnostics)
}

func TestNuGetReasonInventoryIncompleteAndAssociation(t *testing.T) {
	req := declarations.Requirement{Kind: "package-reference", Value: "A@1.0", State: "declared"}
	lockBody := `{"version":1,"dependencies":{"net8.0":{"A":{"type":"Direct"}}}}`

	t.Run("inventory-incomplete-association", func(t *testing.T) {
		// When inventory is incomplete and a lockfile is found, the "observed"
		// association overrides to "indeterminate" with this reason.
		record := nugetRecord("src/App", "src/App/App.csproj", req)
		in := testInput([]declarations.ProjectRecord{record}, map[string]string{
			"src/App/App.csproj":         `<Project Sdk="Microsoft.NET.Sdk">` + pkgA + `</Project>`,
			"src/App/packages.lock.json": lockBody,
		}, false) // inventoryComplete = false
		c := nugetReasonContext(t, in, "src/App/App.csproj")
		if c.AssociationState != "indeterminate" {
			t.Fatalf("AssociationState=%q want indeterminate; context=%+v", c.AssociationState, c)
		}
		const want = "inventory-incomplete-association"
		if nugetReasonBoundaryReason(c) != want {
			t.Fatalf("boundary reason=%q want %q; context=%+v", nugetReasonBoundaryReason(c), want, c)
		}
	})

	t.Run("inventory-incomplete", func(t *testing.T) {
		// When inventory is incomplete and no lockfile is found, the "missing"
		// association overrides to "indeterminate" with this reason.
		record := nugetRecord("src/App", "src/App/App.csproj", req)
		in := testInput([]declarations.ProjectRecord{record}, map[string]string{
			"src/App/App.csproj": `<Project Sdk="Microsoft.NET.Sdk">` + pkgA + `</Project>`,
		}, false) // inventoryComplete = false, no lockfile → "missing"
		c := nugetReasonContext(t, in, "src/App/App.csproj")
		if c.AssociationState != "indeterminate" {
			t.Fatalf("AssociationState=%q want indeterminate; context=%+v", c.AssociationState, c)
		}
		const want = "inventory-incomplete"
		if nugetReasonBoundaryReason(c) != want {
			t.Fatalf("boundary reason=%q want %q; context=%+v", nugetReasonBoundaryReason(c), want, c)
		}
	})
}

func TestNuGetReasonLockfileUnreadable(t *testing.T) {
	// "lockfile-unreadable" is set when the lockfile's File entry is NonRegular.
	req := declarations.Requirement{Kind: "package-reference", Value: "A@1.0", State: "declared"}
	record := nugetRecord("src/App", "src/App/App.csproj", req)
	in := testInput([]declarations.ProjectRecord{record}, map[string]string{
		"src/App/App.csproj":         "<Project />",
		"src/App/packages.lock.json": `{"version":1}`,
	}, true)
	// Mark the lockfile as non-regular.
	for i, f := range in.Inventory {
		if f.Path == "src/App/packages.lock.json" {
			in.Inventory[i].NonRegular = true
		}
	}
	c := nugetReasonContext(t, in, "src/App/App.csproj")
	if c.AssociationState != "indeterminate" {
		t.Fatalf("AssociationState=%q want indeterminate; context=%+v", c.AssociationState, c)
	}
	const want = "lockfile-unreadable"
	if nugetReasonBoundaryReason(c) != want {
		t.Fatalf("boundary reason=%q want %q; context=%+v", nugetReasonBoundaryReason(c), want, c)
	}
}

func TestNuGetReasonLockfileLimit(t *testing.T) {
	// "lockfile-limit" is emitted when the project's lockfile exceeds the
	// bounded lockfile admission limit.
	req := declarations.Requirement{Kind: "package-reference", Value: "A@1.0", State: "declared"}
	record := nugetRecord("src/App", "src/App/App.csproj", req)
	lockBody := `{"version":1,"dependencies":{"net8.0":{"A":{"type":"Direct"}}}}`
	files := map[string]string{
		"src/App/App.csproj":         "<Project />",
		"aaa/packages.lock.json":     lockBody, // sorted first; consumes the one allowed slot
		"src/App/packages.lock.json": lockBody, // sorted second; over the limit
	}
	in := testInput([]declarations.ProjectRecord{record}, files, true)
	r, err := Analyze(context.Background(), in, Limits{Lockfiles: 1})
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	c := contextByManifest(r, "src/App/App.csproj")
	if c.ManifestPath == "" {
		t.Fatalf("no context; contexts=%+v", r.Contexts)
	}
	if c.AssociationState != "indeterminate" {
		t.Fatalf("AssociationState=%q want indeterminate; context=%+v", c.AssociationState, c)
	}
	const want = "lockfile-limit"
	for _, b := range c.Boundaries {
		if b.Reason == want {
			return
		}
	}
	t.Fatalf("boundary reason %q not found; boundaries=%+v", want, c.Boundaries)
}
