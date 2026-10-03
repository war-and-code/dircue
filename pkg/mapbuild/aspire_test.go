package mapbuild

import (
	"testing"

	"github.com/war-and-code/dircue/pkg/declarations"
	"github.com/war-and-code/dircue/pkg/deployables"
	"github.com/war-and-code/dircue/pkg/discovery"
	"github.com/war-and-code/dircue/pkg/mapdoc"
	"github.com/war-and-code/dircue/pkg/profile"
)

func TestAspireRunsNeedsExactAppHostReferenceAndSDK(t *testing.T) {
	app := declarations.Project{ID: "src/AppHost/AppHost.csproj", Root: "src/AppHost", Kind: "dotnet", Requirements: []declarations.Requirement{{Kind: "dotnet-sdk", Value: "Aspire.AppHost.Sdk/9.0.0", State: "declared", Evidence: "src/AppHost/AppHost.csproj"}}, References: []declarations.Reference{{Kind: "project-reference", Value: "../Api/Api.csproj", Target: "src/Api/Api.csproj", TargetStatus: "present", State: "declared", Evidence: "src/AppHost/AppHost.csproj"}}}
	target := declarations.Project{ID: "src/Api/Api.csproj", Root: "src/Api", Kind: "dotnet"}
	lookalike := declarations.Project{ID: "other/Api/Api.csproj", Root: "other/Api", Kind: "dotnet"}
	deployablesReport := &deployables.Report{Status: "complete", Definitions: []deployables.Definition{{Kind: "service", Provider: "aspire-apphost", Name: "AppHost", Path: "src/AppHost/Program.cs", Coverage: "qualified", Evidence: []deployables.Evidence{{Field: "aspire-apphost", Line: 1, Basis: "aspire-apphost-top-level-builder"}}, References: []deployables.Reference{{Kind: "aspire_project", Value: "Api", Qualification: "declared", Evidence: deployables.Evidence{Field: "AddProject", Line: 4, Basis: "aspire-csharp-top-level-static"}}}}}}
	report := &profile.Report{Discovery: &discovery.Report{Status: "complete", Source: discovery.Source{Mode: "directory"}}, Declarations: &declarations.Report{Status: "complete", Projects: []declarations.Project{app, target, lookalike}}}
	doc, err := Build(report, Options{Deployables: deployablesReport})
	if err != nil {
		t.Fatal(err)
	}
	var runs []mapdoc.Edge
	components := map[string]string{}
	for _, n := range doc.Nodes {
		if n.Kind == mapdoc.NodeComponent {
			components[n.Properties["root"]] = n.ID
		}
	}
	for _, e := range doc.Edges {
		if e.Type == mapdoc.EdgeRuns {
			runs = append(runs, e)
		}
	}
	if len(runs) != 1 || runs[0].To != components["src/Api"] || runs[0].From == "" {
		t.Fatalf("Aspire edge must bind exact referenced .NET project: runs=%+v components=%v", runs, components)
	}
	if runs[0].Coverage.Status != mapdoc.CoveragePartial {
		t.Fatalf("declared launch must remain partial: %+v", runs[0])
	}
}

func TestAspireRunsRejectsCustomExcludedConditionalAndWrongSDK(t *testing.T) {
	base := declarations.Project{ID: "src/AppHost/AppHost.csproj", Root: "src/AppHost", Kind: "dotnet", Requirements: []declarations.Requirement{{Kind: "dotnet-sdk", Value: "Aspire.AppHost.Sdk/9.0.0", State: "declared"}}}
	target := declarations.Project{ID: "src/Api/Api.csproj", Root: "src/Api", Kind: "dotnet"}
	ref := declarations.Reference{Kind: "project-reference", Value: "../Api/Api.csproj", Target: "src/Api/Api.csproj", TargetStatus: "present", State: "declared"}
	def := deployables.Definition{Kind: "service", Provider: "aspire-apphost", Name: "AppHost", Path: "src/AppHost/Program.cs", Evidence: []deployables.Evidence{{Field: "apphost", Basis: "test"}}, References: []deployables.Reference{{Kind: "aspire_project", Value: "Api", Qualification: "declared", Evidence: deployables.Evidence{Field: "AddProject", Line: 3, Basis: "test"}}}}
	cases := []struct {
		name   string
		mutate func(*declarations.Project, *declarations.Reference)
	}{
		{"custom type", func(_ *declarations.Project, r *declarations.Reference) { r.AspireCustomName = true }},
		{"excluded resource", func(_ *declarations.Project, r *declarations.Reference) { r.AspireResource = "false" }},
		{"conditional reference", func(_ *declarations.Project, r *declarations.Reference) { r.State = "conditional" }},
		{"wrong sdk", func(p *declarations.Project, _ *declarations.Reference) {
			p.Requirements[0].Value = "Microsoft.NET.Sdk"
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			app := base
			app.References = []declarations.Reference{ref}
			tc.mutate(&app, &app.References[0])
			r := &profile.Report{Discovery: &discovery.Report{Status: "complete", Source: discovery.Source{Mode: "directory"}}, Declarations: &declarations.Report{Projects: []declarations.Project{app, target}}}
			d, e := Build(r, Options{Deployables: &deployables.Report{Status: "complete", Definitions: []deployables.Definition{def}}})
			if e != nil {
				t.Fatal(e)
			}
			for _, edge := range d.Edges {
				if edge.Type == mapdoc.EdgeRuns {
					t.Fatalf("unexpected run edge: %+v", edge)
				}
			}
		})
	}
}
