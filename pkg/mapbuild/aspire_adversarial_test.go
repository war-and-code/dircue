package mapbuild

import (
	"context"
	"testing"

	"github.com/war-and-code/dircue/pkg/declarations"
	"github.com/war-and-code/dircue/pkg/deployables"
	"github.com/war-and-code/dircue/pkg/discovery"
	"github.com/war-and-code/dircue/pkg/mapdoc"
	"github.com/war-and-code/dircue/pkg/profile"
)

func TestAspireCLIObservationDoesNotLinkEscapedOrCustomBindings(t *testing.T) {
	base := "var builder = DistributedApplication.CreateBuilder(args);\nbuilder.AddProject<Projects.Service>(\"service\");\n"
	cases := []struct {
		name   string
		source string
	}{
		{"escaped framework type", base + "static class @DistributedApplication { public static FakeBuilder CreateBuilder(string[] args) => new(); }\nclass FakeBuilder { public FakeBuilder AddProject<T>(string name) => this; }\n"},
		{"escaped generated Projects type", base + "class @Projects { public class Service {} }\n"},
		{"custom generic extension", base + "static class FakeExtensions { public static object AddProject<T>(this IDistributedApplicationBuilder builder, string name) => new(); }\n"},
		{"Unicode escaped framework local", "var Distri\\u0062utedApplication = Custom.Fake;\n" + base},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			source := []byte(tc.source)
			observed, err := deployables.Observe(context.Background(), []deployables.Candidate{{
				Path: "src/AppHost/Program.cs", Size: int64(len(source)),
				Read: func(_ context.Context, _ int64) ([]byte, int64, error) { return source, int64(len(source)), nil },
			}}, deployables.Options{})
			if err != nil {
				t.Fatal(err)
			}
			app := declarations.Project{
				ID: "src/AppHost/AppHost.csproj", Root: "src/AppHost", Kind: "dotnet",
				Requirements: []declarations.Requirement{{Kind: "dotnet-sdk", Value: "Aspire.AppHost.Sdk/9.0.0", State: "declared"}},
				References:   []declarations.Reference{{Kind: "project-reference", Value: "../Service/Service.csproj", Target: "src/Service/Service.csproj", TargetStatus: "present", State: "declared", Evidence: "src/AppHost/AppHost.csproj"}},
			}
			report := &profile.Report{
				Discovery:    &discovery.Report{Status: "complete", Source: discovery.Source{Mode: "directory"}},
				Declarations: &declarations.Report{Status: "complete", Projects: []declarations.Project{app, {ID: "src/Service/Service.csproj", Root: "src/Service", Kind: "dotnet"}}},
			}
			doc, err := Build(report, Options{Deployables: observed})
			if err != nil {
				t.Fatal(err)
			}
			for _, edge := range doc.Edges {
				if edge.Type == mapdoc.EdgeRuns {
					t.Fatalf("ambiguous source binding produced a runs edge: %+v", edge)
				}
			}
		})
	}
}

func TestAspireCLIObservationDoesNotLinkAfterBuilderPassedByReference(t *testing.T) {
	for _, mode := range []string{"ref", "out"} {
		t.Run(mode, func(t *testing.T) {
			source := []byte("var builder = DistributedApplication.CreateBuilder(args);\nReplace(" + mode + " builder);\nbuilder.AddProject<Projects.Service>(\"service\");\nstatic void Replace(" + mode + " IDistributedApplicationBuilder value) { value = null!; }\n")
			observed, err := deployables.Observe(context.Background(), []deployables.Candidate{{
				Path: "src/AppHost/Program.cs", Size: int64(len(source)),
				Read: func(_ context.Context, _ int64) ([]byte, int64, error) { return source, int64(len(source)), nil },
			}}, deployables.Options{})
			if err != nil {
				t.Fatal(err)
			}
			app := declarations.Project{
				ID: "src/AppHost/AppHost.csproj", Root: "src/AppHost", Kind: "dotnet",
				Requirements: []declarations.Requirement{{Kind: "dotnet-sdk", Value: "Aspire.AppHost.Sdk/9.0.0", State: "declared"}},
				References:   []declarations.Reference{{Kind: "project-reference", Value: "../Service/Service.csproj", Target: "src/Service/Service.csproj", TargetStatus: "present", State: "declared", Evidence: "src/AppHost/AppHost.csproj"}},
			}
			report := &profile.Report{
				Discovery:    &discovery.Report{Status: "complete", Source: discovery.Source{Mode: "directory"}},
				Declarations: &declarations.Report{Status: "complete", Projects: []declarations.Project{app, {ID: "src/Service/Service.csproj", Root: "src/Service", Kind: "dotnet"}}},
			}
			doc, err := Build(report, Options{Deployables: observed})
			if err != nil {
				t.Fatal(err)
			}
			for _, edge := range doc.Edges {
				if edge.Type == mapdoc.EdgeRuns {
					t.Fatalf("builder passed by %s reference produced a runs edge: %+v", mode, edge)
				}
			}
		})
	}
}

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

func TestAspireRunsDoNotIgnoreUncertainGeneratedNameCollisions(t *testing.T) {
	baseApp := declarations.Project{
		ID: "src/AppHost/AppHost.csproj", Root: "src/AppHost", Kind: "dotnet",
		Requirements: []declarations.Requirement{{Kind: "dotnet-sdk", Value: "Aspire.AppHost.Sdk/9.0.0", State: "declared"}},
	}
	defaultRef := declarations.Reference{Kind: "project-reference", Value: "../Service/Service.csproj", Target: "src/Service/Service.csproj", TargetStatus: "present", State: "declared", Evidence: "src/AppHost/AppHost.csproj"}
	service := declarations.Project{ID: "src/Service/Service.csproj", Root: "src/Service", Kind: "dotnet"}
	deployable := deployables.Definition{
		Kind: "service", Provider: "aspire-apphost", Name: "AppHost", Path: "src/AppHost/Program.cs", Coverage: "qualified",
		Evidence:   []deployables.Evidence{{Field: "apphost", Line: 1, Basis: "aspire-apphost-top-level-builder"}},
		References: []deployables.Reference{{Kind: "aspire_project", Value: "Service", Qualification: "declared", Evidence: deployables.Evidence{Field: "AddProject", Line: 4, Basis: "aspire-csharp-top-level-static"}}},
	}
	cases := []struct {
		name string
		ref  declarations.Reference
	}{
		{
			name: "duplicate excluded target",
			ref:  declarations.Reference{Kind: "project-reference", Value: "../Service/Service.csproj", Target: "src/Service/Service.csproj", TargetStatus: "present", State: "declared", AspireResource: "false", Evidence: "src/AppHost/AppHost.csproj"},
		},
		{
			name: "conditional duplicate target",
			ref:  declarations.Reference{Kind: "project-reference", Value: "../Service/Service.csproj", Target: "src/Service/Service.csproj", TargetStatus: "present", State: "conditional", Condition: "'$(Flavor)' == 'service'", Evidence: "src/AppHost/AppHost.csproj"},
		},
		{
			name: "dynamic unresolved target could generate same identifier",
			ref:  declarations.Reference{Kind: "project-reference", Value: "../$(ServiceDirectory)/Service.csproj", State: "unresolved", Evidence: "src/AppHost/AppHost.csproj"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			app := baseApp
			app.References = []declarations.Reference{defaultRef, tc.ref}
			report := &profile.Report{
				Discovery:    &discovery.Report{Status: "complete", Source: discovery.Source{Mode: "directory"}},
				Declarations: &declarations.Report{Status: "complete", Projects: []declarations.Project{app, service}},
			}
			doc, err := Build(report, Options{Deployables: &deployables.Report{Status: "complete", Definitions: []deployables.Definition{deployable}}})
			if err != nil {
				t.Fatal(err)
			}
			for _, edge := range doc.Edges {
				if edge.Type == mapdoc.EdgeRuns {
					t.Fatalf("uncertain same-name reference was ignored: %+v", edge)
				}
			}
		})
	}
}
