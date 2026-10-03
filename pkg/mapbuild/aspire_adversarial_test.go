package mapbuild

import (
	"testing"

	"github.com/war-and-code/dircue/pkg/declarations"
	"github.com/war-and-code/dircue/pkg/deployables"
	"github.com/war-and-code/dircue/pkg/discovery"
	"github.com/war-and-code/dircue/pkg/mapdoc"
	"github.com/war-and-code/dircue/pkg/profile"
)

func TestAspireRunsDoNotGuessAcrossCustomGeneratedNameReferences(t *testing.T) {
	app := declarations.Project{
		ID: "src/AppHost/AppHost.csproj", Root: "src/AppHost", Kind: "dotnet",
		Requirements: []declarations.Requirement{{Kind: "dotnet-sdk", Value: "Aspire.AppHost.Sdk/9.0.0", State: "declared"}},
		References: []declarations.Reference{
			{Kind: "project-reference", Value: "../Service/Service.csproj", Target: "src/Service/Service.csproj", TargetStatus: "present", State: "declared", Evidence: "src/AppHost/AppHost.csproj"},
			{Kind: "project-reference", Value: "../Other/Other.csproj", Target: "src/Other/Other.csproj", TargetStatus: "present", State: "declared", Evidence: "src/AppHost/AppHost.csproj", AspireCustomName: true},
		},
	}
	service := declarations.Project{ID: "src/Service/Service.csproj", Root: "src/Service", Kind: "dotnet"}
	other := declarations.Project{ID: "src/Other/Other.csproj", Root: "src/Other", Kind: "dotnet"}
	deployable := deployables.Definition{
		Kind: "service", Provider: "aspire-apphost", Name: "AppHost", Path: "src/AppHost/Program.cs", Coverage: "qualified",
		Evidence:   []deployables.Evidence{{Field: "apphost", Line: 1, Basis: "aspire-apphost-top-level-builder"}},
		References: []deployables.Reference{{Kind: "aspire_project", Value: "Service", Qualification: "declared", Evidence: deployables.Evidence{Field: "AddProject", Line: 4, Basis: "aspire-csharp-top-level-static"}}},
	}
	report := &profile.Report{
		Discovery:    &discovery.Report{Status: "complete", Source: discovery.Source{Mode: "directory"}},
		Declarations: &declarations.Report{Status: "complete", Projects: []declarations.Project{app, service, other}},
	}
	doc, err := Build(report, Options{Deployables: &deployables.Report{Status: "complete", Definitions: []deployables.Definition{deployable}}})
	if err != nil {
		t.Fatal(err)
	}
	for _, edge := range doc.Edges {
		if edge.Type == mapdoc.EdgeRuns {
			t.Fatalf("custom generated-name metadata must block guessing a default-name run edge: %+v", edge)
		}
	}
}
