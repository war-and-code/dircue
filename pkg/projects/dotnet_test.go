package projects

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestDotnetProjectDeclarations(t *testing.T) {
	doc := ParseDotnet("src/App/App.csproj", []byte(`<Project Sdk="Microsoft.NET.Sdk"><PropertyGroup Condition="'$(Configuration)' == 'Release'"><TargetFrameworks>net8.0;net9.0</TargetFrameworks><RuntimeIdentifiers>linux-x64;win-x64</RuntimeIdentifiers><LangVersion>preview</LangVersion></PropertyGroup><ItemGroup Condition="'$(TargetFramework)' == 'net8.0'"><ProjectReference Include="..\Core\Core.csproj" Condition="Exists('..\Core\Core.csproj')"/><ProjectReference Include="$(Root)\Optional.csproj"/><Protobuf Include="api.proto"/></ItemGroup><Import Project="..\..\Directory.Build.props"/><PackageReference Include="Example.Package"><Version>1.2.3</Version></PackageReference></Project>`))
	if len(doc.Diagnostics) != 0 || len(doc.Projects) != 1 {
		t.Fatalf("unexpected document: %+v", doc)
	}
	p := doc.Projects[0]
	if p.Kind != "dotnet" || p.ID != "src/App/App.csproj" || p.Root != "src/App" {
		t.Fatalf("project: %+v", p)
	}
	if len(p.References) != 3 {
		t.Fatalf("references: %+v", p.References)
	}
	r := p.References[0]
	if r.Target != "src/Core/Core.csproj" || r.State != "conditional" || !strings.Contains(r.Condition, "TargetFramework") || !strings.Contains(r.Condition, "Exists") {
		t.Fatalf("reference: %+v", r)
	}
	if p.References[1].State != "unresolved" || p.References[1].Target != "" {
		t.Fatalf("dynamic reference: %+v", p.References[1])
	}
	if p.References[2].Target != "Directory.Build.props" {
		t.Fatalf("import: %+v", p.References[2])
	}
	want := map[string]string{"dotnet-sdk": "Microsoft.NET.Sdk", "language-version": "preview", "package-reference": "Example.Package@1.2.3", "code-generation": "Protobuf"}
	for kind, value := range want {
		found := false
		for _, r := range p.Requirements {
			if r.Kind == kind && r.Value == value {
				found = true
			}
		}
		if !found {
			t.Errorf("missing %s=%s: %+v", kind, value, p.Requirements)
		}
	}
	for _, r := range p.Requirements {
		if r.Kind == "target-framework" && r.State != "conditional" {
			t.Errorf("framework lost condition: %+v", r)
		}
	}
}

func TestDotnetSharedConfiguration(t *testing.T) {
	doc := ParseDotnet("Directory.Build.props", []byte(`<Project xmlns="http://schemas.microsoft.com/developer/msbuild/2003"><PropertyGroup><TargetFrameworkVersion>v4.8</TargetFrameworkVersion><LangVersion>$(ChosenVersion)</LangVersion></PropertyGroup><ImportGroup Condition="'$(OS)' == 'Windows_NT'"><Import Project="build/windows.props"/></ImportGroup></Project>`))
	if len(doc.Projects) != 0 || len(doc.Requirements) != 2 || len(doc.References) != 1 {
		t.Fatalf("shared document: %+v", doc)
	}
	if doc.Requirements[1].State != "unresolved" || doc.References[0].State != "conditional" {
		t.Fatalf("lost conditional/dynamic values: %+v", doc)
	}
	global := ParseDotnet("global.json", []byte(`{"sdk":{"version":"8.0.300","rollForward":"latestFeature","allowPrerelease":false},"msbuild-sdks":{"Zulu":"2.0","Alpha":"1.0"}}`))
	if len(global.Requirements) != 5 || global.Requirements[3].Value != "Alpha/1.0" || global.Requirements[4].Value != "Zulu/2.0" {
		t.Fatalf("global: %+v", global)
	}
}

func TestDotnetSolutionMembership(t *testing.T) {
	doc := ParseDotnet("work/App.sln", []byte("Microsoft Visual Studio Solution File, Format Version 12.00\r\n"+`Project("{GUID}") = "Some ""quoted"" App", "src\App.csproj", "{ID}"`+"\r\n"+`Project("{GUID}") = "Folder", "Folder", "{ID}"`+"\r\n"+`Project("{GUID}") = "Library", "..\lib\Library.fsproj", "{ID}"`+"\r\nEndProject\r\n"))
	if len(doc.Diagnostics) != 0 || len(doc.Projects) != 1 || len(doc.Projects[0].References) != 2 {
		t.Fatalf("solution: %+v", doc)
	}
	if doc.Projects[0].References[0].Target != "work/src/App.csproj" || doc.Projects[0].References[1].Target != "lib/Library.fsproj" {
		t.Fatalf("members: %+v", doc.Projects[0].References)
	}
	slnx := ParseDotnet("App.slnx", []byte(`<Solution><Folder Name="/Libraries/"><Project Path="src\Library.vbproj"/></Folder><Project Path="app/App.csproj"/></Solution>`))
	if len(slnx.Projects) != 1 || slnx.Projects[0].Kind != "solution" || len(slnx.Projects[0].References) != 2 || slnx.Projects[0].References[0].Target != "src/Library.vbproj" {
		t.Fatalf("slnx: %+v", slnx)
	}
}

func TestDotnetUnsafeReferencesRemainUnresolved(t *testing.T) {
	for _, value := range []string{"../../escape.csproj", "/tmp/Absolute.csproj", `C:\Work\Other.csproj`, `\\host\share\Other.csproj`, "https://example.com/Other.csproj", "src/*.csproj", "src/[ab].csproj", "$(Root)/Other.csproj", "@(References)", "%(Filename).csproj"} {
		t.Run(value, func(t *testing.T) {
			r := dotnetReference("src/app.csproj", "project-reference", value, "")
			if r.State != "unresolved" || r.Target != "" {
				t.Fatalf("resolved unsafe reference: %+v", r)
			}
		})
	}
}

func TestDotnetRejectsMalformedAndExpensiveXML(t *testing.T) {
	cases := []string{`<Project><ItemGroup></Project>`, `<Project/><Project/>`, `<Project/><`, `<!DOCTYPE Project [<!ENTITY secret SYSTEM "file:///etc/passwd">]><Project>&secret;</Project>`, `<Project Sdk="one" Sdk="two"/>`, strings.Repeat("<Project>", dotnetMaxDepth+1) + strings.Repeat("</Project>", dotnetMaxDepth+1)}
	for _, content := range cases {
		doc := ParseDotnet("App.csproj", []byte(content))
		if len(doc.Diagnostics) == 0 || len(doc.Projects) != 0 {
			t.Fatalf("accepted malformed or excessive XML: %+v", doc)
		}
	}
	large := ParseDotnet("App.csproj", make([]byte, dotnetMaxBytes+1))
	if len(large.Diagnostics) != 1 || large.Diagnostics[0].Code != "manifest-too-large" {
		t.Fatalf("large: %+v", large)
	}
	wrong := ParseDotnet("App.csproj", []byte(`<unrelated/>`))
	if len(wrong.Diagnostics) != 1 || wrong.Diagnostics[0].Code != "unexpected-root" {
		t.Fatalf("wrong root: %+v", wrong)
	}
}

func TestDotnetDoesNotInventoryNugetCredentials(t *testing.T) {
	doc := ParseDotnet("NuGet.Config", []byte(`<configuration><packageSources><add key="private" value="https://username:password@example.com/v3/index.json?token=SECRET"/></packageSources><packageSourceCredentials><private><add key="ClearTextPassword" value="SECRET"/></private></packageSourceCredentials></configuration>`))
	encoded, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "SECRET") || strings.Contains(string(encoded), "password") || len(doc.Diagnostics) != 0 {
		t.Fatalf("credentials escaped: %s", encoded)
	}
	legacy := ParseDotnet("packages.config", []byte(`<packages><package id="Example.Package" version="1.0" targetFramework="net48"/></packages>`))
	if len(legacy.Requirements) != 2 {
		t.Fatalf("packages: %+v", legacy)
	}
}

func TestIsDotnet(t *testing.T) {
	for _, name := range []string{"src/App.csproj", "src/App.FSPROJ", "App.vbproj", "App.sln", "App.slnx", "global.json", "Directory.Build.props", "Directory.Build.targets", "Directory.Packages.props", "NuGet.Config", "packages.config"} {
		if !IsDotnet(name) {
			t.Errorf("not detected: %s", name)
		}
	}
	for _, name := range []string{"package.json", "README.md", "arbitrary.props"} {
		if IsDotnet(name) {
			t.Errorf("unexpected detection: %s", name)
		}
	}
}

func TestDotnetSDKImportsAndConditionalVersions(t *testing.T) {
	doc := ParseDotnet("App.csproj", []byte(`<Project><Import Project="Sdk.props" Sdk="Microsoft.NET.Sdk" Version="8.0.100"/><ItemGroup Condition="'$(OS)'=='Windows_NT'"><PackageReference Include="Example"><Version Condition="'$(TargetFramework)'=='net8.0'">1.0</Version><Version Condition="'$(TargetFramework)'=='net9.0'">2.0</Version></PackageReference></ItemGroup></Project>`))
	p := doc.Projects[0]
	if p.References[0].State != "unresolved" || p.References[0].Target != "" {
		t.Fatalf("SDK import mistaken for repository path: %+v", p.References)
	}
	if len(p.Requirements) != 3 || p.Requirements[0].Value != "Microsoft.NET.Sdk/8.0.100" {
		t.Fatalf("requirements: %+v", p.Requirements)
	}
	for _, r := range p.Requirements[1:] {
		if r.State != "conditional" || !strings.Contains(r.Condition, "OS") || !strings.Contains(r.Condition, "TargetFramework") {
			t.Fatalf("lost package version condition: %+v", r)
		}
	}
}

func TestDotnetSolutionFoldersAndMalformedSolutions(t *testing.T) {
	doc := ParseDotnet("App.sln", []byte("Microsoft Visual Studio Solution File, Format Version 12.00\n"+`Project("{2150E333-8FDC-42A3-9474-1A3956D46DE8}") = "Virtual.csproj", "Virtual.csproj", "{ID}"`+"\n"+`Project("{GUID}") = "Broken`))
	if len(doc.Projects) != 1 || len(doc.Projects[0].References) != 0 || len(doc.Diagnostics) != 1 {
		t.Fatalf("folder or malformed line: %+v", doc)
	}
	for _, content := range []string{"", "hello", "<Solution/>"} {
		bad := ParseDotnet("App.sln", []byte(content))
		if len(bad.Diagnostics) != 1 || len(bad.Projects) != 0 {
			t.Fatalf("bad solution: %+v", bad)
		}
	}
	for _, content := range []string{"null", "[]", `{"sdk":{"version":12}}`, `{"sdk":`} {
		bad := ParseDotnet("global.json", []byte(content))
		if len(bad.Diagnostics) != 1 {
			t.Fatalf("bad global JSON: %+v", bad)
		}
	}
}

func FuzzParseDotnet(f *testing.F) {
	for _, seed := range []string{`<Project Sdk="Microsoft.NET.Sdk"/>`, `<Project><ItemGroup><ProjectReference Include="../other/Other.csproj"/></ItemGroup></Project>`, `<Solution><Project Path="src/App.csproj"/></Solution>`, `<Project><PropertyGroup Condition="x"><TargetFramework>net8.0</TargetFramework></PropertyGroup></Project>`} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, content string) {
		if len(content) > 65536 {
			t.Skip()
		}
		for _, name := range []string{"src/App.csproj", "App.slnx", "global.json", "App.sln", "NuGet.Config"} {
			doc := ParseDotnet(name, []byte(content))
			check := func(ref Reference) {
				if ref.Target != "" && (ref.State == "unresolved" || strings.HasPrefix(ref.Target, "/") || strings.HasPrefix(ref.Target, "../") || ref.Target == "..") {
					t.Fatalf("unsafe reference target: %+v", ref)
				}
			}
			for _, ref := range doc.References {
				check(ref)
			}
			for _, project := range doc.Projects {
				for _, ref := range project.References {
					check(ref)
				}
			}
		}
	})
}

func TestDotnetOtherwisePreservesConditionalContext(t *testing.T) {
	doc := ParseDotnet("App.csproj", []byte(`<Project><Choose><When Condition="'$(OS)'=='Windows_NT'"><PropertyGroup><TargetFramework>net8.0-windows</TargetFramework></PropertyGroup></When><When Condition="'$(OS)'=='Unix'"><PropertyGroup><TargetFramework>net8.0</TargetFramework></PropertyGroup></When><Otherwise><ItemGroup><ProjectReference Include="Fallback.csproj"/></ItemGroup></Otherwise></Choose></Project>`))
	p := doc.Projects[0]
	if len(p.References) != 1 || p.References[0].State != "conditional" || !strings.Contains(p.References[0].Condition, "Not (") || !strings.Contains(p.References[0].Condition, "Windows_NT") || !strings.Contains(p.References[0].Condition, "Unix") {
		t.Fatalf("Otherwise became unconditional: %+v", p)
	}
	for _, r := range p.Requirements {
		if r.State != "conditional" {
			t.Fatalf("When became unconditional: %+v", r)
		}
	}
}

func TestDotnetBoundsInheritedConditionExpansion(t *testing.T) {
	cases := map[string]string{
		"oversized expression":         `<Project Condition="` + strings.Repeat("x", dotnetMaxConditionBytes+1) + `"><PropertyGroup><TargetFramework>net8.0</TargetFramework></PropertyGroup></Project>`,
		"repeated inherited condition": `<Project><ItemGroup Condition="` + strings.Repeat("x", 16000) + `">` + strings.Repeat(`<ProjectReference Include="Library.csproj" Condition="x"/>`, 2000) + `</ItemGroup></Project>`,
		"observation count":            `<Project><PropertyGroup><TargetFrameworks>` + strings.Repeat("net8.0;", dotnetMaxObservations+10) + `</TargetFrameworks></PropertyGroup></Project>`,
	}
	for name, input := range cases {
		t.Run(name, func(t *testing.T) {
			if len(input) > int(MaxManifestBytes) {
				t.Fatal("fixture must fit normal scanner input bound")
			}
			doc := ParseDotnet("App.csproj", []byte(input))
			if len(doc.Diagnostics) != 1 || doc.Diagnostics[0].Code != "declaration-limit" {
				t.Fatalf("missing bounded omission: %+v", doc.Diagnostics)
			}
			if len(doc.Projects) != 1 {
				t.Fatal("valid project identity discarded with partial declarations")
			}
			p := doc.Projects[0]
			if len(p.Requirements)+len(p.References) > dotnetMaxObservations {
				t.Fatal("observation budget exceeded")
			}
			encoded, err := json.Marshal(doc)
			if err != nil {
				t.Fatal(err)
			}
			if len(encoded) > dotnetMaxExpandedBytes {
				t.Fatalf("expanded report exceeded bound: %d", len(encoded))
			}
			for _, ref := range p.References {
				if ref.State != "conditional" || ref.Condition == "" {
					t.Fatal("condition truncation produced unconditional reference")
				}
			}
			collector := New("directory", "")
			collector.Add("App.csproj", int64(len(input)), "configuration", doc)
			if collector.Finish().Status != "partial" {
				t.Fatal("declaration omissions falsely complete")
			}
		})
	}
}

func TestDotnetSolutionAndSDKObservationLimits(t *testing.T) {
	line := `Project("{TYPE}") = "Library", "Library.csproj", "{ID}"` + "\n"
	solution := []byte("Microsoft Visual Studio Solution File, Format Version 12.00\n" + strings.Repeat(line, dotnetMaxObservations+1))
	doc := ParseDotnet("App.sln", solution)
	if len(doc.Diagnostics) != 1 || doc.Diagnostics[0].Code != "declaration-limit" || len(doc.Projects[0].References) != dotnetMaxObservations {
		t.Fatalf("solution limit: %+v", doc.Diagnostics)
	}
	sdks := map[string]string{}
	for i := 0; i < dotnetMaxObservations+1; i++ {
		sdks[fmt.Sprint(i)] = "1.0"
	}
	content, _ := json.Marshal(map[string]any{"msbuild-sdks": sdks})
	doc = ParseDotnet("global.json", content)
	if len(doc.Diagnostics) != 1 || doc.Diagnostics[0].Code != "declaration-limit" || len(doc.Requirements) != dotnetMaxObservations {
		t.Fatalf("SDK limit: %+v", doc.Diagnostics)
	}
}

func TestDotnetPackageVersionOverrideAttributes(t *testing.T) {
	doc := ParseDotnet("App.csproj", []byte(`<Project><ItemGroup Condition="'$(TargetFramework)'=='net8.0'"><PackageReference Include="OnlyOverride" VersionOverride="2.0"/><PackageReference Include="Both" Version="1.0" VersionOverride="3.0"/><PackageReference Include="Dynamic" VersionOverride="$(ChosenVersion)"/></ItemGroup></Project>`))
	if len(doc.Diagnostics) != 0 || len(doc.Projects) != 1 {
		t.Fatalf("invalid fixture: %+v", doc)
	}
	requirements := map[string]Requirement{}
	for _, req := range doc.Projects[0].Requirements {
		requirements[req.Value] = req
	}
	for _, value := range []string{"OnlyOverride@2.0", "Both@1.0", "Both@3.0"} {
		req, ok := requirements[value]
		if !ok || req.State != "conditional" || req.Condition == "" {
			t.Fatalf("declared version lost: %s %+v", value, req)
		}
	}
	if requirements["Dynamic@$(ChosenVersion)"].State != "unresolved" {
		t.Fatal("dynamic override treated as evaluated version")
	}
	if len(requirements) != 4 {
		t.Fatalf("unexpected declarations: %+v", requirements)
	}
}
