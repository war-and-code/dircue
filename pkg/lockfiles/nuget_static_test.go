package lockfiles

import (
	"context"
	"fmt"
	"path"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/war-and-code/dircue/pkg/declarations"
)

func TestNuGetDetailTruncationPreservesUTF8AndBounds(t *testing.T) {
	for _, input := range []string{
		strings.Repeat("x", nugetMaxDetailBytes+20),
		strings.Repeat("x", nugetMaxDetailBytes-1) + "🧭" + strings.Repeat("y", 20),
	} {
		got := nugetDetail(input)
		if !utf8.ValidString(got) {
			t.Fatalf("nugetDetail returned invalid UTF-8: %q", got)
		}
		if len(got) > nugetMaxDetailBytes+3 {
			t.Fatalf("nugetDetail returned %d bytes, want at most %d", len(got), nugetMaxDetailBytes+3)
		}
	}
}

// nugetWant is the expected outcome for one MSBuild project. Empty fields are
// not checked.
type nugetWant struct {
	state, reason, lock, basis, check, presence, causePath string
	missing                                                []string
}

const lockA = `{"version":1,"dependencies":{"net8.0":{"A":{"type":"Direct"}}}}`

// analyzeNuGetFiles builds one project record per MSBuild project file in
// files and returns the validated report.
func analyzeNuGetFiles(t *testing.T, files map[string]string) *Report {
	t.Helper()
	var records []declarations.ProjectRecord
	for p := range files {
		if isMSBuildProjectRecord(path.Ext(p)) {
			records = append(records, nugetRecord(path.Dir(p), p))
		}
	}
	slices.SortFunc(records, func(a, b declarations.ProjectRecord) int { return strings.Compare(a.Project.ID, b.Project.ID) })
	r, err := Analyze(context.Background(), testInput(records, files, true), Limits{})
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateReport(r); err != nil {
		t.Fatalf("validate: %v", err)
	}
	return r
}

func checkNuGetWant(t *testing.T, r *Report, manifest string, want nugetWant) {
	t.Helper()
	c := contextByManifest(r, manifest)
	if c.ManifestPath == "" {
		t.Fatalf("no context for %s", manifest)
	}
	e := c.NuGetEvidence
	fail := func(field, got, wantValue string) {
		t.Helper()
		t.Errorf("%s %s=%q want %q\ncontext=%+v\nevidence=%+v", manifest, field, got, wantValue, c, *e)
	}
	if want.state != "" && c.AssociationState != want.state {
		fail("association", c.AssociationState, want.state)
	}
	if want.reason != "" && c.OutcomeReason() != want.reason {
		fail("reason", c.OutcomeReason(), want.reason)
	}
	if want.lock != "" && c.LockfilePath != want.lock {
		fail("lockfile", c.LockfilePath, want.lock)
	}
	if want.basis != "" && e.LockPathBasis != want.basis {
		fail("basis", e.LockPathBasis, want.basis)
	}
	if want.check != "" && e.CheckState != want.check {
		fail("check", e.CheckState, want.check)
	}
	if want.presence != "" && e.PresenceState != want.presence {
		fail("presence", e.PresenceState, want.presence)
	}
	if want.causePath != "" && !slices.ContainsFunc(e.Causes, func(cause NuGetCause) bool { return cause.Path == want.causePath }) {
		fail("cause path", fmt.Sprint(e.Causes), want.causePath)
	}
	if want.missing != nil && (len(c.Checks) != 1 || !slices.Equal(c.Checks[0].Missing, want.missing)) {
		fail("missing", fmt.Sprint(c.Checks), fmt.Sprint(want.missing))
	}
}

func TestNuGetStaticModelRules(t *testing.T) {
	sdk := func(body string) string { return `<Project Sdk="Microsoft.NET.Sdk">` + body + pkgA + `</Project>` }
	for _, tc := range []struct {
		name  string
		files map[string]string
		want  map[string]nugetWant
	}{
		{
			// F3: a project-level Choose decides the path at evaluation time.
			name: "project Choose lock path is conditional",
			files: map[string]string{
				"src/App/App.csproj":         sdk(`<Choose><When Condition="'$(CI)' == 'true'"><PropertyGroup><NuGetLockFilePath>ci.lock.json</NuGetLockFilePath></PropertyGroup></When><Otherwise /></Choose>`),
				"src/App/packages.lock.json": lockA,
				"src/App/ci.lock.json":       lockA,
			},
			want: map[string]nugetWant{"src/App/App.csproj": {state: "indeterminate", reason: "nuget-lock-path-conditional", basis: "conditional", presence: "observed", causePath: "src/App/App.csproj"}},
		},
		{
			// F3: a property set inside a Target applies only when that target
			// runs; restore reads the evaluated value.
			name: "Target property assignment keeps the evaluated path possible",
			files: map[string]string{
				"src/App/App.csproj":         sdk(`<Target Name="Pin" BeforeTargets="_GenerateRestoreProjectSpec"><PropertyGroup><NuGetLockFilePath>pinned.lock.json</NuGetLockFilePath></PropertyGroup></Target>`),
				"src/App/packages.lock.json": lockA,
			},
			want: map[string]nugetWant{"src/App/App.csproj": {state: "indeterminate", reason: "nuget-lock-path-conditional", basis: "conditional"}},
		},
		{
			// F4: an Sdk-attributed import resolves inside the SDK, not next to
			// the importing file.
			name: "Sdk-attributed import in Directory.Build.props is unmodeled",
			files: map[string]string{
				"Directory.Build.props":      `<Project><Import Project="Sdk.props" Sdk="Microsoft.DotNet.Arcade.Sdk" /></Project>`,
				"Sdk.props":                  `<Project><PropertyGroup><NuGetLockFilePath>decoy.lock.json</NuGetLockFilePath></PropertyGroup></Project>`,
				"src/App/App.csproj":         sdk(""),
				"src/App/packages.lock.json": lockA,
			},
			want: map[string]nugetWant{"src/App/App.csproj": {state: "indeterminate", reason: "nuget-msbuild-input-unmodeled", basis: "open_unmodeled", presence: "observed", causePath: "Directory.Build.props"}},
		},
		{
			// F5: shared-file control flow affects ownership like project XML.
			name: "Choose in Directory.Build.props affects ownership",
			files: map[string]string{
				"Directory.Build.props":      `<Project><Choose><When Condition="'$(Configuration)' == 'Release'"><PropertyGroup><NuGetLockFilePath>$(MSBuildThisFileDirectory)locks/$(MSBuildProjectName).json</NuGetLockFilePath></PropertyGroup></When></Choose></Project>`,
				"src/App/App.csproj":         sdk(""),
				"src/App/packages.lock.json": lockA,
				"locks/App.json":             lockA,
			},
			want: map[string]nugetWant{"src/App/App.csproj": {state: "indeterminate", reason: "nuget-lock-path-conditional", basis: "conditional", causePath: "Directory.Build.props"}},
		},
		{
			name: "CustomAfterMicrosoftCommonTargets hook is followed",
			files: map[string]string{
				"Directory.Build.props":      `<Project><PropertyGroup><CustomAfterMicrosoftCommonTargets>$(MSBuildThisFileDirectory)build/after.targets</CustomAfterMicrosoftCommonTargets></PropertyGroup></Project>`,
				"build/after.targets":        `<Project><PropertyGroup><NuGetLockFilePath>hooked.lock.json</NuGetLockFilePath></PropertyGroup></Project>`,
				"src/App/App.csproj":         sdk(""),
				"src/App/packages.lock.json": lockA,
				"src/App/hooked.lock.json":   lockA,
			},
			want: map[string]nugetWant{"src/App/App.csproj": {state: "observed", lock: "src/App/hooked.lock.json", basis: "custom_literal", check: "match"}},
		},
		{
			// F7: shared inputs that set unrelated properties do not make the
			// lock path or package set uncertain.
			name: "harmless shared inputs keep the conventional owner",
			files: map[string]string{
				"Directory.Build.props":      `<Project><PropertyGroup><LangVersion>latest</LangVersion><Nullable>enable</Nullable><ImportDirectoryBuildProps>false</ImportDirectoryBuildProps></PropertyGroup><ItemGroup Condition="'$(IsTestProject)' == 'true'"><Using Include="Xunit" /></ItemGroup></Project>`,
				"Directory.Packages.props":   `<Project><PropertyGroup><ManagePackageVersionsCentrally>true</ManagePackageVersionsCentrally></PropertyGroup><ItemGroup><PackageVersion Include="A" Version="1.0" /></ItemGroup></Project>`,
				"Directory.Build.targets":    `<Project><Target Name="Stamp" AfterTargets="Build"><Message Text="built" /></Target></Project>`,
				"src/App/App.csproj":         sdk(""),
				"src/App/packages.lock.json": lockA,
			},
			want: map[string]nugetWant{"src/App/App.csproj": {state: "observed", lock: "src/App/packages.lock.json", basis: "conventional", check: "match", presence: "observed"}},
		},
		{
			name: "assign-when-unset idiom is deterministic",
			files: map[string]string{
				"Directory.Build.props":      `<Project><PropertyGroup><NuGetLockFilePath Condition="'$(NuGetLockFilePath)' == ''">$(MSBuildProjectDirectory)/locks/$(MSBuildProjectName).json</NuGetLockFilePath></PropertyGroup></Project>`,
				"src/App/App.csproj":         sdk(""),
				"src/App/locks/App.json":     lockA,
				"src/App/packages.lock.json": lockA,
			},
			want: map[string]nugetWant{"src/App/App.csproj": {state: "observed", lock: "src/App/locks/App.json", basis: "custom_literal", check: "match"}},
		},
		{
			name: "GetPathOfFileAbove import is resolved within the snapshot",
			files: map[string]string{
				"src/Directory.Build.props":  `<Project><Import Project="$([MSBuild]::GetPathOfFileAbove('Directory.Build.props', '$(MSBuildThisFileDirectory)../'))" /></Project>`,
				"Directory.Build.props":      `<Project><PropertyGroup><NuGetLockFilePath>above.lock.json</NuGetLockFilePath></PropertyGroup></Project>`,
				"src/App/App.csproj":         sdk(""),
				"src/App/above.lock.json":    lockA,
				"src/App/packages.lock.json": lockA,
			},
			want: map[string]nugetWant{"src/App/App.csproj": {state: "observed", lock: "src/App/above.lock.json"}},
		},
		{
			// MSBuild searches for Directory.Build.props with File.Exists, so
			// a lower-case file is found only on case-insensitive systems.
			name: "case-variant Directory.Build.props is file-system dependent",
			files: map[string]string{
				"directory.build.props":      `<Project><PropertyGroup><NuGetLockFilePath>x.lock.json</NuGetLockFilePath></PropertyGroup></Project>`,
				"src/App/App.csproj":         sdk(""),
				"src/App/packages.lock.json": lockA,
			},
			want: map[string]nugetWant{"src/App/App.csproj": {state: "indeterminate", reason: "nuget-lock-path-conditional", basis: "conditional", presence: "observed", causePath: "directory.build.props"}},
		},
		{
			// $(MSBuildProjectDirectory) has no trailing separator.
			name: "project directory concatenation names a sibling path",
			files: map[string]string{
				"src/App/App.csproj":         sdk(`<PropertyGroup><NuGetLockFilePath>$(MSBuildProjectDirectory).lock.json</NuGetLockFilePath></PropertyGroup>`),
				"src/App.lock.json":          lockA,
				"src/App/packages.lock.json": lockA,
			},
			want: map[string]nugetWant{"src/App/App.csproj": {state: "observed", lock: "src/App.lock.json"}},
		},
		{
			name: "root project directory concatenation leaves the snapshot",
			files: map[string]string{
				"App.csproj":         sdk(`<PropertyGroup><NuGetLockFilePath>$(MSBuildProjectDirectory)lock.json</NuGetLockFilePath></PropertyGroup>`),
				".lock.json":         lockA,
				"packages.lock.json": lockA,
			},
			want: map[string]nugetWant{"App.csproj": {state: "indeterminate", reason: "nuget-custom-lock-path-unresolved", basis: "outside_snapshot"}},
		},
		{
			// NuGet replaces spaces in the project name (SDK 10.0.401 check).
			name: "project-specific default name replaces spaces",
			files: map[string]string{
				"src/My App/My App.csproj":             sdk(""),
				"src/My App/packages.My_App.lock.json": lockA,
			},
			want: map[string]nugetWant{"src/My App/My App.csproj": {state: "observed", lock: "src/My App/packages.My_App.lock.json", basis: "conventional"}},
		},
		{
			name: "project-specific default name is preferred",
			files: map[string]string{
				"src/App/App.csproj":             sdk(""),
				"src/App/packages.App.lock.json": lockA,
				"src/App/packages.lock.json":     lockA,
			},
			want: map[string]nugetWant{"src/App/App.csproj": {state: "observed", lock: "src/App/packages.App.lock.json"}},
		},
		{
			name: "backslash import is resolved",
			files: map[string]string{
				"Directory.Build.props":      `<Project><Import Project="build\common.props" /></Project>`,
				"build/common.props":         `<Project><ItemGroup><PackageReference Include="Shared" Version="1.0" /></ItemGroup></Project>`,
				"src/App/App.csproj":         sdk(""),
				"src/App/packages.lock.json": lockA,
			},
			want: map[string]nugetWant{"src/App/App.csproj": {state: "observed", check: "different", missing: []string{"shared"}}},
		},
		{
			name: "backslash lock path differs by platform",
			files: map[string]string{
				"src/App/App.csproj":         sdk(`<PropertyGroup><NuGetLockFilePath>locks\app.json</NuGetLockFilePath></PropertyGroup>`),
				"src/App/locks/app.json":     lockA,
				"src/App/packages.lock.json": lockA,
			},
			want: map[string]nugetWant{"src/App/App.csproj": {state: "indeterminate", reason: "nuget-lock-path-conditional", presence: "observed"}},
		},
		{
			// 1.4.0 ignored the user file, which Microsoft.Common.targets
			// imports after the project body.
			name: "project user file is imported",
			files: map[string]string{
				"src/App/App.csproj":         sdk(""),
				"src/App/App.csproj.user":    `<Project><PropertyGroup><NuGetLockFilePath>user.lock.json</NuGetLockFilePath></PropertyGroup></Project>`,
				"src/App/user.lock.json":     lockA,
				"src/App/packages.lock.json": lockA,
			},
			want: map[string]nugetWant{"src/App/App.csproj": {state: "observed", lock: "src/App/user.lock.json"}},
		},
		{
			// 1.4.0 reported "different" for a reference removed later.
			name: "PackageReference Remove is honored",
			files: map[string]string{
				"Directory.Build.props":      `<Project><ItemGroup><PackageReference Include="Analyzers" Version="1.0" /></ItemGroup></Project>`,
				"src/App/App.csproj":         sdk(`<ItemGroup><PackageReference Remove="Analyzers" /></ItemGroup>`),
				"src/App/packages.lock.json": lockA,
			},
			want: map[string]nugetWant{"src/App/App.csproj": {state: "observed", check: "match"}},
		},
		{
			name: "conditional PackageReference keeps the check indeterminate",
			files: map[string]string{
				"src/App/App.csproj":         sdk(`<ItemGroup><PackageReference Include="WinOnly" Version="1.0" Condition="'$(OS)' == 'Windows_NT'" /></ItemGroup>`),
				"src/App/packages.lock.json": lockA,
			},
			want: map[string]nugetWant{"src/App/App.csproj": {state: "observed", check: "indeterminate", causePath: "src/App/App.csproj"}},
		},
		{
			name: "legacy toolset imports mark the SDK phases",
			files: map[string]string{
				"Directory.Build.targets":    `<Project><PropertyGroup><NuGetLockFilePath>legacy.lock.json</NuGetLockFilePath></PropertyGroup></Project>`,
				"src/App/App.csproj":         `<Project ToolsVersion="15.0"><Import Project="$(MSBuildExtensionsPath)\$(MSBuildToolsVersion)\Microsoft.Common.props" />` + pkgA + `<Import Project="$(MSBuildToolsPath)\Microsoft.CSharp.targets" /></Project>`,
				"src/App/legacy.lock.json":   lockA,
				"src/App/packages.lock.json": lockA,
			},
			want: map[string]nugetWant{"src/App/App.csproj": {state: "observed", lock: "src/App/legacy.lock.json"}},
		},
		{
			name: "property that redirects SDK imports is unmodeled",
			files: map[string]string{
				"Directory.Build.props":      `<Project><PropertyGroup><NuGetTargets>$(MSBuildThisFileDirectory)custom/NuGet.targets</NuGetTargets></PropertyGroup></Project>`,
				"src/App/App.csproj":         sdk(""),
				"src/App/packages.lock.json": lockA,
			},
			want: map[string]nugetWant{"src/App/App.csproj": {state: "indeterminate", reason: "nuget-msbuild-input-unmodeled", causePath: "Directory.Build.props"}},
		},
		{
			// MSBuild wildcards are * and ?; matches stay possible because the
			// order of wildcard imports is not modeled.
			name: "wildcard import with ? makes its matches possible",
			files: map[string]string{
				"Directory.Build.props":      `<Project><Import Project="props/?.props" /></Project>`,
				"props/a.props":              `<Project><PropertyGroup><NuGetLockFilePath>a.lock.json</NuGetLockFilePath></PropertyGroup></Project>`,
				"src/App/App.csproj":         sdk(""),
				"src/App/packages.lock.json": lockA,
			},
			want: map[string]nugetWant{"src/App/App.csproj": {state: "indeterminate", reason: "nuget-lock-path-conditional", basis: "conditional", causePath: "props/a.props"}},
		},
		{
			// Brackets are literal in MSBuild paths, not a character class.
			name: "wildcard import treats brackets literally",
			files: map[string]string{
				"Directory.Build.props":      `<Project><Import Project="props/[ab]*.props" /></Project>`,
				"props/a.props":              `<Project><PropertyGroup><NuGetLockFilePath>a.lock.json</NuGetLockFilePath></PropertyGroup></Project>`,
				"src/App/App.csproj":         sdk(""),
				"src/App/packages.lock.json": lockA,
			},
			want: map[string]nugetWant{"src/App/App.csproj": {state: "observed", lock: "src/App/packages.lock.json", basis: "conventional", check: "match"}},
		},
		{
			name: "project without package references is not applicable",
			files: map[string]string{
				"src/App/App.csproj": `<Project Sdk="Microsoft.NET.Sdk"><PropertyGroup><TargetFramework>net8.0</TargetFramework></PropertyGroup></Project>`,
			},
			want: map[string]nugetWant{"src/App/App.csproj": {state: "not_applicable", check: "not_compared", presence: "not_observed"}},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := analyzeNuGetFiles(t, tc.files)
			for manifest, want := range tc.want {
				checkNuGetWant(t, r, manifest, want)
			}
		})
	}
}

// F6: an unmodeled SDK qualifies only the project that uses it. A project
// whose lock path is open can name any file, so it qualifies others and is
// named as the culprit.
func TestNuGetOwnershipCollisionsNameTheCulprit(t *testing.T) {
	base := map[string]string{
		"src/App/App.csproj":         `<Project Sdk="Microsoft.NET.Sdk">` + pkgA + `</Project>`,
		"src/App/packages.lock.json": lockA,
		"src/Lib/Lib.csproj":         `<Project Sdk="Microsoft.NET.Sdk">` + pkgA + `</Project>`,
		"src/Lib/packages.lock.json": lockA,
	}
	with := func(extra map[string]string) map[string]string {
		out := map[string]string{}
		for k, v := range base {
			out[k] = v
		}
		for k, v := range extra {
			out[k] = v
		}
		return out
	}

	r := analyzeNuGetFiles(t, with(map[string]string{
		"src/Tests/Tests.csproj": `<Project Sdk="MSTest.Sdk/3.6.0">` + pkgA + `</Project>`,
		"src/Host/Host.csproj":   `<Project Sdk="Microsoft.NET.Sdk"><Sdk Name="Aspire.AppHost.Sdk" Version="9.0.0" />` + pkgA + `</Project>`,
	}))
	checkNuGetWant(t, r, "src/App/App.csproj", nugetWant{state: "observed", lock: "src/App/packages.lock.json", check: "match"})
	checkNuGetWant(t, r, "src/Lib/Lib.csproj", nugetWant{state: "observed", lock: "src/Lib/packages.lock.json", check: "match"})
	checkNuGetWant(t, r, "src/Tests/Tests.csproj", nugetWant{state: "indeterminate", reason: "nuget-msbuild-input-unmodeled", causePath: "src/Tests/Tests.csproj"})

	r = analyzeNuGetFiles(t, with(map[string]string{
		"tools/Gen/Gen.csproj": `<Project Sdk="Microsoft.NET.Sdk"><PropertyGroup><NuGetLockFilePath>$(LockDir)/gen.json</NuGetLockFilePath></PropertyGroup>` + pkgA + `</Project>`,
	}))
	for _, manifest := range []string{"src/App/App.csproj", "src/Lib/Lib.csproj"} {
		checkNuGetWant(t, r, manifest, nugetWant{state: "indeterminate", reason: "ambiguous-nuget-lockfile-owner"})
		c := contextByManifest(r, manifest)
		if c.Boundaries[0].Path != "tools/Gen/Gen.csproj" {
			t.Errorf("%s ambiguity does not name the culprit: %+v", manifest, c.Boundaries)
		}
	}
	checkNuGetWant(t, r, "tools/Gen/Gen.csproj", nugetWant{state: "indeterminate", reason: "nuget-custom-lock-path-unresolved", basis: "open_evidence"})

	// A visible pattern helps find existing candidates, but an unexpanded
	// property can contain path separators and escape that glob. It remains an
	// open ownership claim rather than proving other projects' paths are safe.
	r = analyzeNuGetFiles(t, with(map[string]string{
		"tools/Gen/Gen.csproj": `<Project Sdk="Microsoft.NET.Sdk"><PropertyGroup><NuGetLockFilePath>locks/$(TargetFramework).json</NuGetLockFilePath></PropertyGroup>` + pkgA + `</Project>`,
	}))
	checkNuGetWant(t, r, "src/App/App.csproj", nugetWant{state: "indeterminate", reason: "ambiguous-nuget-lockfile-owner"})
	checkNuGetWant(t, r, "src/Lib/Lib.csproj", nugetWant{state: "indeterminate", reason: "ambiguous-nuget-lockfile-owner"})
	checkNuGetWant(t, r, "tools/Gen/Gen.csproj", nugetWant{state: "indeterminate", reason: "nuget-custom-lock-path-unresolved", basis: "open_evidence"})
}

func TestNuGetDynamicLockPathCannotProveMissingOutsidePattern(t *testing.T) {
	r := analyzeNuGetFiles(t, map[string]string{
		"src/App/App.csproj":     `<Project Sdk="Microsoft.NET.Sdk"><PropertyGroup><LockDirectory>../..</LockDirectory><NuGetLockFilePath>locks/$(LockDirectory)/packages.lock.json</NuGetLockFilePath></PropertyGroup>` + pkgA + `</Project>`,
		"src/packages.lock.json": lockA,
	})
	checkNuGetWant(t, r, "src/App/App.csproj", nugetWant{state: "indeterminate", reason: "nuget-custom-lock-path-unresolved", basis: "open_evidence", presence: "unknown"})
}

func TestNuGetLaterLiteralClosesDynamicPathClaim(t *testing.T) {
	r := analyzeNuGetFiles(t, map[string]string{
		"src/App/App.csproj":         `<Project Sdk="Microsoft.NET.Sdk"><PropertyGroup><NuGetLockFilePath>$(UnknownPath)/dynamic.lock.json</NuGetLockFilePath><NuGetLockFilePath>final.lock.json</NuGetLockFilePath></PropertyGroup>` + pkgA + `</Project>`,
		"src/App/final.lock.json":    lockA,
		"src/App/packages.lock.json": lockA,
	})
	checkNuGetWant(t, r, "src/App/App.csproj", nugetWant{state: "observed", lock: "src/App/final.lock.json", basis: "custom_literal", check: "match"})
}

func TestNuGetTargetLockPathSurvivesLaterEvaluationLiteral(t *testing.T) {
	r := analyzeNuGetFiles(t, map[string]string{
		"src/App/App.csproj":          `<Project Sdk="Microsoft.NET.Sdk"><Target Name="SetLockPath" BeforeTargets="_GenerateRestoreProjectSpec"><PropertyGroup><NuGetLockFilePath>target.lock.json</NuGetLockFilePath></PropertyGroup></Target><PropertyGroup><NuGetLockFilePath>evaluated.lock.json</NuGetLockFilePath></PropertyGroup>` + pkgA + `</Project>`,
		"src/App/evaluated.lock.json": lockA,
		"src/App/target.lock.json":    lockA,
	})
	checkNuGetWant(t, r, "src/App/App.csproj", nugetWant{state: "indeterminate", reason: "nuget-lock-path-conditional", basis: "conditional", presence: "observed"})
}

func TestNuGetExplicitSDKVersionsAreUnmodeled(t *testing.T) {
	for _, tc := range []struct {
		name, project string
	}{
		{"project attribute", `<Project Sdk="Microsoft.NET.Sdk/10.0.401">` + pkgA + `</Project>`},
		{"Sdk element", `<Project><Sdk Name="Microsoft.NET.Sdk" Version="10.0.401" />` + pkgA + `</Project>`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := analyzeNuGetFiles(t, map[string]string{
				"src/App/App.csproj":         tc.project,
				"src/App/packages.lock.json": lockA,
			})
			checkNuGetWant(t, r, "src/App/App.csproj", nugetWant{state: "indeterminate", reason: "nuget-msbuild-input-unmodeled", presence: "observed"})
		})
	}
}

func TestNuGetGlobalJSONMSBuildSDKOverrideBoundaries(t *testing.T) {
	project := `<Project Sdk="Microsoft.NET.Sdk">` + pkgA + `</Project>`
	t.Run("ordinary .NET SDK version does not override project SDK", func(t *testing.T) {
		r := analyzeNuGetFiles(t, map[string]string{
			"global.json":                `{"sdk":{"version":"10.0.401"}}`,
			"src/App/App.csproj":         project,
			"src/App/packages.lock.json": lockA,
		})
		checkNuGetWant(t, r, "src/App/App.csproj", nugetWant{state: "observed", lock: "src/App/packages.lock.json", check: "match"})
	})
	t.Run("matching msbuild SDK version override is unmodeled", func(t *testing.T) {
		r := analyzeNuGetFiles(t, map[string]string{
			"global.json":                `{"msbuild-sdks":{"Microsoft.NET.Sdk":"10.0.401"}}`,
			"src/App/App.csproj":         project,
			"src/App/packages.lock.json": lockA,
		})
		checkNuGetWant(t, r, "src/App/App.csproj", nugetWant{state: "indeterminate", reason: "nuget-msbuild-input-unmodeled", presence: "observed", causePath: "global.json"})
	})
	t.Run("commented nested global JSON does not hide ancestor override", func(t *testing.T) {
		r := analyzeNuGetFiles(t, map[string]string{
			"global.json": `{"msbuild-sdks":{"Microsoft.NET.Sdk":"10.0.401"}}`,
			"src/App/global.json": `{
// only selects the .NET SDK
"sdk":{"version":"10.0.401"}}`,
			"src/App/App.csproj":         project,
			"src/App/packages.lock.json": lockA,
		})
		checkNuGetWant(t, r, "src/App/App.csproj", nugetWant{state: "indeterminate", reason: "nuget-msbuild-input-unmodeled", presence: "observed", causePath: "global.json"})
	})
	t.Run("override for unrelated SDK does not affect modeled SDK", func(t *testing.T) {
		r := analyzeNuGetFiles(t, map[string]string{
			"global.json":                `{"msbuild-sdks":{"Example.Custom.Sdk":"1.2.3"}}`,
			"src/App/App.csproj":         project,
			"src/App/packages.lock.json": lockA,
		})
		checkNuGetWant(t, r, "src/App/App.csproj", nugetWant{state: "observed", lock: "src/App/packages.lock.json", check: "match"})
	})
	t.Run("malformed global JSON is qualified", func(t *testing.T) {
		r := analyzeNuGetFiles(t, map[string]string{
			"global.json":                `{"msbuild-sdks":`,
			"src/App/App.csproj":         project,
			"src/App/packages.lock.json": lockA,
		})
		checkNuGetWant(t, r, "src/App/App.csproj", nugetWant{state: "indeterminate", reason: "nuget-msbuild-input-unmodeled", presence: "observed", causePath: "global.json"})
	})
	t.Run("case-fold duplicate SDK keys are qualified deterministically", func(t *testing.T) {
		files := map[string]string{
			"global.json":                `{"msbuild-sdks":{"Microsoft.NET.Sdk":"1.0.0","microsoft.net.sdk":"2.0.0"}}`,
			"src/App/App.csproj":         project,
			"src/App/packages.lock.json": lockA,
		}
		var first NuGetEvidence
		for i := 0; i < 10; i++ {
			r := analyzeNuGetFiles(t, files)
			checkNuGetWant(t, r, "src/App/App.csproj", nugetWant{state: "indeterminate", reason: "nuget-msbuild-input-unmodeled", presence: "observed", causePath: "global.json"})
			got := *contextByManifest(r, "src/App/App.csproj").NuGetEvidence
			if i == 0 {
				first = got
			} else if !reflect.DeepEqual(first, got) {
				t.Fatalf("NuGet evidence changed across repeated parsing:\nfirst=%+v\ngot=%+v", first, got)
			}
		}
	})
	t.Run("unreadable global JSON is qualified", func(t *testing.T) {
		files := map[string]string{
			"global.json":                `{"msbuild-sdks":{"Microsoft.NET.Sdk":"10.0.401"}}`,
			"src/App/App.csproj":         project,
			"src/App/packages.lock.json": lockA,
		}
		record := nugetRecord("src/App", "src/App/App.csproj")
		in := testInput([]declarations.ProjectRecord{record}, files, true)
		for i := range in.SelectedFiles {
			if in.SelectedFiles[i].Path == "global.json" {
				in.SelectedFiles[i].NonRegular = true
			}
		}
		r, err := Analyze(context.Background(), in, Limits{})
		if err != nil {
			t.Fatal(err)
		}
		if err := ValidateReport(r); err != nil {
			t.Fatalf("validate: %v", err)
		}
		checkNuGetWant(t, r, "src/App/App.csproj", nugetWant{state: "indeterminate", reason: "nuget-msbuild-input-unmodeled", presence: "observed", causePath: "global.json"})
	})
}

func TestNuGetGlobalJSONIsReadOnceAcrossProjects(t *testing.T) {
	files := map[string]string{
		"global.json":              `{"sdk":{"version":"10.0.401"}}`,
		"src/A/A.csproj":           `<Project Sdk="Microsoft.NET.Sdk">` + pkgA + `</Project>`,
		"src/A/packages.lock.json": lockA,
		"src/B/B.csproj":           `<Project Sdk="Microsoft.NET.Sdk">` + pkgA + `</Project>`,
		"src/B/packages.lock.json": lockA,
	}
	records := []declarations.ProjectRecord{nugetRecord("src/A", "src/A/A.csproj"), nugetRecord("src/B", "src/B/B.csproj")}
	in := testInput(records, files, true)
	read := in.ReadSelected
	reads := map[string]int{}
	in.ReadSelected = func(ctx context.Context, p string, limit int64) ([]byte, int64, error) {
		reads[p]++
		return read(ctx, p, limit)
	}
	r, err := Analyze(context.Background(), in, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateReport(r); err != nil {
		t.Fatalf("validate: %v", err)
	}
	if reads["global.json"] != 1 {
		t.Fatalf("global.json read %d times, want one cached read", reads["global.json"])
	}
	for _, manifest := range []string{"src/A/A.csproj", "src/B/B.csproj"} {
		checkNuGetWant(t, r, manifest, nugetWant{state: "observed", check: "match"})
	}
}

func TestNuGetGlobalJSONParsedOncePerSelectedPath(t *testing.T) {
	files := map[string]string{
		"global.json":        `{"sdk":{"version":"10.0.401"}}`,
		"src/App/App.csproj": `<Project Sdk="Microsoft.NET.Sdk">` + pkgA + `</Project>`,
	}
	record := nugetRecord("src/App", "src/App/App.csproj")
	in := testInput([]declarations.ProjectRecord{record}, files, true)
	selected := make(map[string]File, len(in.SelectedFiles))
	for _, f := range in.SelectedFiles {
		selected[f.Path] = f
	}
	inputBytes := int64(0)
	w := &nugetWalker{
		ctx:        context.Background(),
		in:         in,
		index:      newNuGetFileIndex(selected, true),
		limits:     defaults(Limits{}),
		inputBytes: &inputBytes,
		reads:      map[string]selectedFileRead{},
		globalSDKs: map[string]nugetGlobalSDKParse{},
	}
	first := newNuGetEval(record.Project.ID)
	if err := w.nugetGlobalSDKOverride(first, record.Project.ID, []string{"Microsoft.NET.Sdk"}); err != nil {
		t.Fatal(err)
	}
	if !w.globalSDKs["global.json"].valid {
		t.Fatalf("first parse was not cached as valid: %+v", w.globalSDKs["global.json"])
	}
	// A later caller must use the cached parsed document, not reparse even
	// though the lower-level selected-byte cache is shared separately.
	read := w.reads["global.json"]
	read.data = []byte(`not JSON`)
	w.reads["global.json"] = read
	second := newNuGetEval(record.Project.ID)
	if err := w.nugetGlobalSDKOverride(second, record.Project.ID, []string{"Microsoft.NET.Sdk"}); err != nil {
		t.Fatal(err)
	}
	if len(second.unmodeled) != 0 {
		t.Fatalf("second call reparsed the selected bytes: %+v", second.unmodeled)
	}
}

func TestNuGetEvidenceBoundsCauses(t *testing.T) {
	var body strings.Builder
	for i := 0; i < nugetMaxCauses+3; i++ {
		fmt.Fprintf(&body, `<ItemGroup Condition="'$(V%d)' == 'true'"><PackageReference Include="P%d" Version="1.0" /></ItemGroup>`, i, i)
	}
	files := map[string]string{"src/App/App.csproj": `<Project Sdk="Microsoft.NET.Sdk">` + body.String() + pkgA + `</Project>`, "src/App/packages.lock.json": lockA}
	r := analyzeNuGetFiles(t, files)
	e := contextByManifest(r, "src/App/App.csproj").NuGetEvidence
	if len(e.Causes) != nugetMaxCauses || e.OmittedCauses != 3 {
		t.Fatalf("causes=%d omitted=%d", len(e.Causes), e.OmittedCauses)
	}
}

// F10: evaluation and ownership stay near linear in projects and files.
func TestNuGetStaticModelScalesToLargeRepositories(t *testing.T) {
	if testing.Short() {
		t.Skip("timing test")
	}
	const projects = 2000
	var records []declarations.ProjectRecord
	content := map[string]string{
		"Directory.Build.props":    `<Project><PropertyGroup><LangVersion>latest</LangVersion></PropertyGroup></Project>`,
		"Directory.Packages.props": `<Project><PropertyGroup><ManagePackageVersionsCentrally>true</ManagePackageVersionsCentrally></PropertyGroup></Project>`,
	}
	in := Input{Source: "directory", InventoryComplete: true, SelectedFilesComplete: true, Declarations: declarations.Report{Status: "complete"}}
	for i := 0; i < projects; i++ {
		root := fmt.Sprintf("src/g%02d/P%04d", i%40, i)
		manifest := root + fmt.Sprintf("/P%04d.csproj", i)
		records = append(records, nugetRecord(root, manifest))
		content[manifest] = `<Project Sdk="Microsoft.NET.Sdk">` + pkgA + `</Project>`
		content[root+"/packages.lock.json"] = lockA
		for j := 0; j < 40; j++ {
			in.Inventory = append(in.Inventory, File{Path: fmt.Sprintf("%s/src/F%02d.cs", root, j), Size: 10})
		}
	}
	in.ProjectRecords = records
	in.Inventory = append(in.Inventory, testInput(nil, content, true).Inventory...)
	in.SelectedFiles = testInput(nil, content, true).SelectedFiles
	in.ReadSelected = testInput(nil, content, true).ReadSelected
	start := time.Now()
	r, err := Analyze(context.Background(), in, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	elapsed := time.Since(start)
	// Every owner is unique; past the lockfile limit only the read is skipped.
	for _, c := range r.Contexts {
		if c.AssociationState != "observed" && c.OutcomeReason() != "lockfile-limit" {
			t.Fatalf("unexpected ownership: %+v", c)
		}
	}
	if len(r.Contexts) != projects {
		t.Fatalf("contexts=%d want %d", len(r.Contexts), projects)
	}
	// 1.4.0 took about 1.5s on this shape; the 1.5.0 candidate took 4.4s.
	if elapsed > 3*time.Second {
		t.Fatalf("2,000-project analysis took %s", elapsed)
	}
	t.Logf("2,000 projects, %d inventory paths: %s", len(in.Inventory), elapsed)
}

// Anchors are ASCII, but the surrounding text may not be. Lowercasing can
// change a string's byte length, so matching must not index a lowered copy.
func TestReplaceFoldKeepsByteOffsetsWithNonASCIIText(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"İİİİ/$(msbuildprojectname).lock.json", "İİİİ/App.lock.json"},
		{"$(MSBuildProjectName)/$(MSBUILDPROJECTNAME)", "App/App"},
		{"KİK", "KİK"},
	} {
		if got := replaceFold(tc.in, "$(MSBuildProjectName)", "App"); got != tc.want {
			t.Errorf("replaceFold(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
