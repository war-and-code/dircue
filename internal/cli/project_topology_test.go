package cli

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/war-and-code/dircue/pkg/mapdoc"
)

// This fixture goes through the CLI's actual collectors and map wiring. Package
// parser tests alone cannot catch a working observer that map never calls.
func TestMapProjectTopologyAcrossSelectedSourcesAndWorkerCounts(t *testing.T) {
	root := t.TempDir()
	files := map[string]string{
		"settings.gradle.kts":              "rootProject.name = \"suite\"\ninclude(\":java\")\nproject(\":java\").projectDir = file(\"java-service\")\n",
		"java-service/build.gradle.kts":    "plugins { java }\n",
		"java-service/src/Main.java":       "class Main {}\n",
		"api/pyproject.toml":               "[project]\nname = \"api\"\nversion = \"1\"\n",
		"api/app.py":                       "def app():\n    pass\n",
		"Procfile":                         "web: gunicorn api.app:app -b 0.0.0.0:$PORT -w 3\n",
		"Host.AppHost/Host.AppHost.csproj": `<Project Sdk="Aspire.AppHost.Sdk/13.5.3"><ItemGroup><ProjectReference Include="../Service/Service.csproj"/></ItemGroup></Project>`,
		"Host.AppHost/Program.cs":          "var builder = DistributedApplication.CreateBuilder(args);\nbuilder.AddProject<Projects.Service>(\"service\");\nbuilder.Build().Run();\n",
		"Service/Service.csproj":           `<Project Sdk="Microsoft.NET.Sdk.Web"><PropertyGroup><TargetFramework>net10.0</TargetFramework></PropertyGroup></Project>`,
		"Service/Program.cs":               "System.Console.WriteLine(\"hello\");\n",
	}
	writeCLILockfileFixture(t, root, files)
	gitPath, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git unavailable")
	}
	for _, args := range [][]string{{"init", "-q"}, {"add", "."}, {"commit", "-qm", "topology fixture"}} {
		base := []string{"-C", root, "-c", "core.hooksPath=" + os.DevNull, "-c", "commit.gpgsign=false", "-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid"}
		command := exec.Command(gitPath, append(base, args...)...)
		command.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_CONFIG_NOSYSTEM=1")
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("fixture git %v: %s: %v", args, output, err)
		}
	}
	var semanticBaseline []string
	for _, source := range []string{"directory", "git"} {
		var rawBaseline string
		for _, workers := range []string{"1", "8"} {
			output, stderr, err := invoke("map", "--json", "--source", source, "--workers", workers, root)
			if err != nil || stderr != "" {
				t.Fatalf("map %s workers=%s: %v %s", source, workers, err, stderr)
			}
			if rawBaseline != "" && output != rawBaseline {
				t.Fatalf("map %s changed with worker count", source)
			}
			rawBaseline = output
			if strings.Contains(output, root) {
				t.Fatal("map leaked the fixture's host path")
			}
			var document mapdoc.Document
			if err := json.Unmarshal([]byte(output), &document); err != nil {
				t.Fatal(err)
			}
			if err := mapdoc.Validate(document); err != nil {
				t.Fatal(err)
			}
			ids := map[string]mapdoc.Node{}
			for _, node := range document.Nodes {
				ids[node.ID] = node
			}
			var selected []string
			checks := []struct {
				kind     mapdoc.EdgeType
				from, to string
			}{
				{mapdoc.EdgeMemberOf, "java-service/build.gradle.kts", "settings.gradle.kts"},
				{mapdoc.EdgeRuns, "Procfile", "api/pyproject.toml"},
				{mapdoc.EdgeRuns, "Host.AppHost/Program.cs", "Service/Service.csproj"},
			}
			for _, check := range checks {
				var matches []mapdoc.Edge
				for _, edge := range document.Edges {
					if edge.Type == check.kind && slices.Contains(ids[edge.From].Paths, check.from) && slices.Contains(ids[edge.To].Paths, check.to) {
						matches = append(matches, edge)
					}
				}
				if len(matches) != 1 || matches[0].Coverage.Status != mapdoc.CoveragePartial || len(matches[0].Evidence) == 0 {
					t.Fatalf("want one qualified %s %s → %s; found %+v", check.kind, check.from, check.to, matches)
				}
				selected = append(selected, matches[0].ID)
			}
			if semanticBaseline == nil {
				semanticBaseline = selected
			} else if !reflect.DeepEqual(selected, semanticBaseline) {
				t.Fatalf("selected-source edge identities changed: %v vs %v", selected, semanticBaseline)
			}
		}
	}

	// The selected Git tree remains fixed when a local launch declaration changes.
	if err := os.WriteFile(filepath.Join(root, "Procfile"), []byte("web: curl https://example.invalid\n"), 0600); err != nil {
		t.Fatal(err)
	}
	output, _, err := invoke("map", "--json", "--source", "git", root)
	if err != nil || !strings.Contains(output, "procfile_process_target_static") {
		t.Fatalf("Git map did not preserve the committed Procfile target: %v", err)
	}
	output, _, err = invoke("map", "--json", "--source", "directory", root)
	if err != nil || strings.Contains(output, "procfile_process_target_static") {
		t.Fatalf("directory map ignored the current Procfile: %v", err)
	}
}
