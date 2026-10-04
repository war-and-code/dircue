package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/war-and-code/dircue/pkg/mapdoc"
)

func TestMapAspireGlobalAliasContextUsesSelectedProjectScope(t *testing.T) {
	program := `using Aspire.Hosting;
var builder = DistributedApplication.CreateBuilder(args);
builder.AddProject<Projects.Api>("api");
`
	appHostProject := `<Project Sdk="Aspire.AppHost.Sdk/9.0.0"><ItemGroup><ProjectReference Include="../Api/Api.csproj" /></ItemGroup></Project>`
	apiProject := `<Project Sdk="Microsoft.NET.Sdk" />`
	fakeSource := `namespace Fake;
public static class DistributedApplication { public static FakeBuilder CreateBuilder(string[] args) => new(); }
public sealed class FakeBuilder { public FakeBuilder AddProject<T>(string name) => this; }
`
	cases := []struct {
		name         string
		files        map[string]string
		wantRun      bool
		wantOmission bool
	}{
		{
			name: "external global alias suppresses false Aspire declaration",
			files: map[string]string{
				"src/AppHost/AppHost.csproj":  appHostProject,
				"src/AppHost/Program.cs":      program,
				"src/AppHost/GlobalUsings.cs": "global using DistributedApplication = Fake.DistributedApplication;\n",
				"src/AppHost/Fake.cs":         fakeSource,
				"src/Api/Api.csproj":          apiProject,
			},
			wantOmission: true,
		},
		{
			name: "comments and strings do not suppress a real declaration",
			files: map[string]string{
				"src/AppHost/AppHost.csproj": appHostProject,
				"src/AppHost/Program.cs":     program,
				"src/AppHost/GlobalUsings.cs": `// global using DistributedApplication = Fake.DistributedApplication;
internal static class Text { const string Value = "global using Projects = Fake.Projects;"; }
`,
				"src/Api/Api.csproj": apiProject,
			},
			wantRun: true,
		},
		{
			name: "nested project alias participates in apphost default compile glob",
			files: map[string]string{
				"src/AppHost/AppHost.csproj":        appHostProject,
				"src/AppHost/Program.cs":            program,
				"src/AppHost/tests/Tests.csproj":    `<Project Sdk="Microsoft.NET.Sdk" />`,
				"src/AppHost/tests/GlobalUsings.cs": "global using DistributedApplication = Fake.DistributedApplication;\n",
				"src/Api/Api.csproj":                apiProject,
			},
			wantOmission: true,
		},
		{
			name: "nested helper Program.cs global alias participates in apphost",
			files: map[string]string{
				"src/AppHost/AppHost.csproj":     appHostProject,
				"src/AppHost/Program.cs":         program,
				"src/AppHost/Helpers/Program.cs": "global using @Projects = Fake.Projects;\n",
				"src/Api/Api.csproj":             apiProject,
			},
			wantOmission: true,
		},
		{
			name: "sibling project alias does not suppress apphost",
			files: map[string]string{
				"src/AppHost/AppHost.csproj": appHostProject,
				"src/AppHost/Program.cs":     program,
				"src/Other/Other.csproj":     apiProject,
				"src/Other/GlobalUsings.cs":  "global using DistributedApplication = Fake.DistributedApplication;\n",
				"src/Api/Api.csproj":         apiProject,
			},
			wantRun: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			for name, content := range tc.files {
				full := filepath.Join(root, filepath.FromSlash(name))
				if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			out, stderr, err := invoke("map", "--source", "directory", "--json", root)
			if err != nil || stderr != "" {
				t.Fatalf("map invocation: err=%v stderr=%q", err, stderr)
			}
			var doc mapdoc.Document
			if err := json.Unmarshal([]byte(out), &doc); err != nil {
				t.Fatalf("map JSON: %v", err)
			}
			if err := mapdoc.Validate(doc); err != nil {
				t.Fatalf("map validation: %v", err)
			}
			runs := 0
			for _, edge := range doc.Edges {
				if edge.Type == mapdoc.EdgeRuns {
					runs++
				}
			}
			if (runs == 1) != tc.wantRun {
				t.Fatalf("runs edge count=%d wantRun=%t", runs, tc.wantRun)
			}
			if tc.wantOmission {
				if !strings.Contains(out, "aspire_global_alias") {
					t.Fatalf("global alias was not disclosed in map coverage: %s", out)
				}
			}
		})
	}
}
