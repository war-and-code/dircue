package projects

import "testing"

func TestParseDotnetRecordsAssemblyReferencesAndName(t *testing.T) {
	doc := ParseDotnet("src/App/App.csproj", []byte(`<Project Sdk="Microsoft.NET.Sdk">
  <PropertyGroup><AssemblyName>Company.App</AssemblyName></PropertyGroup>
  <ItemGroup>
    <Reference Include="Company.Core, Version=1.0.0.0, Culture=neutral" />
    <Reference Include=" System.Xml ; Other" Condition="'$(OS)' == 'Windows_NT'" />
  </ItemGroup>
</Project>`))
	got := map[string]string{}
	for _, p := range doc.Projects {
		for _, r := range p.Requirements {
			if r.Kind == "assembly-reference" || r.Kind == "assembly-name" {
				got[r.Kind+":"+r.Value] = r.State
			}
		}
	}
	want := map[string]string{"assembly-name:Company.App": "declared", "assembly-reference:Company.Core": "declared", "assembly-reference:System.Xml": "conditional", "assembly-reference:Other": "conditional"}
	if len(got) != len(want) {
		t.Fatalf("got %v want %v", got, want)
	}
	for k, v := range want {
		if got[k] != v {
			t.Fatalf("got %v want %v", got, want)
		}
	}
}
