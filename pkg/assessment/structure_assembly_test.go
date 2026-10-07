package assessment

import (
	"testing"

	"github.com/war-and-code/dircue/pkg/declarations"
)

func TestStructureAssemblyNameReferencesAreQualifiedLinks(t *testing.T) {
	ref := func(name string) declarations.Requirement {
		return declarations.Requirement{Kind: "assembly-reference", Value: name, State: "declared", Evidence: "x"}
	}
	projects := []declarations.Project{
		{ID: "src/App/App.csproj", Root: "src/App", Kind: "dotnet", Requirements: []declarations.Requirement{ref("Company.Core"), ref("System.Xml"), ref("Shared")}},
		{ID: "src/Core/Core.csproj", Root: "src/Core", Kind: "dotnet", Requirements: []declarations.Requirement{{Kind: "assembly-name", Value: "Company.Core", State: "declared", Evidence: "src/Core/Core.csproj"}}},
		{ID: "src/A/Shared.csproj", Root: "src/A", Kind: "dotnet"},
		{ID: "src/B/Shared.csproj", Root: "src/B", Kind: "dotnet"},
		{ID: "src/Dyn/Dyn.csproj", Root: "src/Dyn", Kind: "dotnet", Requirements: []declarations.Requirement{{Kind: "assembly-name", Value: "$(Prefix).Dyn", State: "unresolved", Evidence: "src/Dyn/Dyn.csproj"}}},
		{ID: "tools/Tool/Tool.csproj", Root: "tools/Tool", Kind: "dotnet", Requirements: []declarations.Requirement{ref("company.core")}},
	}
	r := finishStructureProjects(t, projects)
	d := r.Structure.Dependencies
	if !hasQualifiedResolution(d, "nuget", "assembly-reference", "assembly_name_match") || !hasQualifiedResolution(d, "nuget", "assembly-reference", "ambiguous_assembly_name_match") {
		t.Fatalf("assembly-name references were not qualified: %+v", d.QualifiedReferences)
	}
	for _, row := range d.QualifiedReferences {
		if row.Kind == "assembly-reference" && row.Resolution == "assembly_name_match" && row.Count != 2 {
			t.Fatalf("case-insensitive assembly matches: %+v", row)
		}
	}
	// Assembly references never become definite edges, but they join groups
	// in the qualified view: App, Core, and Tool become one group.
	if d.DefiniteEdges.Count != 0 || d.ConnectedGroups.Count != 6 || d.ConnectedGroupsWithQualified.Count != 4 {
		t.Fatalf("edges=%d groups=%d with qualified=%d", d.DefiniteEdges.Count, d.ConnectedGroups.Count, d.ConnectedGroupsWithQualified.Count)
	}
	if !coverageReason(r.Structure, "dependency_connectivity", "nuget", "assembly_name_references_to_local_projects") {
		t.Fatalf("missing assembly-name coverage reason: %+v", r.Structure.Coverage)
	}
	if err := ValidateReport(r); err != nil {
		t.Fatal(err)
	}
}
