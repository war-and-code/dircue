package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	time "time"

	"github.com/war-and-code/dircue/pkg/mapdoc"
	git "github.com/war-and-code/dircue/third_party/go-git"
	"github.com/war-and-code/dircue/third_party/go-git/plumbing/object"
)

func TestMapLaunchFacetsInGitAndDirectoryModesThroughAliases(t *testing.T) {
	root := t.TempDir()
	files := map[string]string{
		"pyproject.toml": `[project]
name = "weather"
[project.scripts]
weather = "weather.cli:main"
[project.gui-scripts]
weather-ui = "weather.ui:launch"
`,
		"src/App/App.csproj": `<Project>
  <PropertyGroup Condition="'$(Configuration)' == 'Release'"><OutputType>WinExe</OutputType></PropertyGroup>
</Project>
`,
		"Dockerfile": `FROM python:3.12 AS builder
ENTRYPOINT ["python3", "-m", "http.server", "--password", "DO_NOT_DISCLOSE"]
FROM python:3.12 AS runtime
CMD ["node", "--token", "DO_NOT_DISCLOSE"]
ENTRYPOINT ["custom-launcher", "DO_NOT_DISCLOSE"]
`,
		"cron.yaml": `apiVersion: batch/v1
kind: CronJob
metadata:
  name: nightly
spec:
  schedule: "0 3 * * *"
  suspend: false
  timeZone: Etc/UTC
  jobTemplate:
    spec:
      template:
        spec:
          restartPolicy: Never
`,
	}
	for name, content := range files {
		full := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	repo, err := git.PlainInit(root, false)
	if err != nil {
		t.Fatal(err)
	}
	worktree, err := repo.Worktree()
	if err != nil {
		t.Fatal(err)
	}
	for name := range files {
		if _, err := worktree.Add(name); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := worktree.Commit("launch fixture", &git.CommitOptions{Author: &object.Signature{Name: "Fixture", Email: "fixture@example.invalid", When: time.Unix(1700000000, 0)}}); err != nil {
		t.Fatal(err)
	}

	for _, args := range [][]string{
		{"map", "--json", root},
		{"dirq", "map", "--json", root},
		{"map", "--source", "directory", "--json", root},
		{"dirq", "map", "--source", "directory", "--json", root},
	} {
		name := strings.Join(args[:len(args)-1], "-")
		t.Run(name, func(t *testing.T) {
			var out, stderr string
			var err error
			if args[0] == "dirq" {
				out, stderr, err = invokeAs("dirq", args[1:]...)
			} else {
				out, stderr, err = invoke(args...)
			}
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
			pythonKinds := map[string]string{}
			var dotnet mapdoc.Node
			facts := map[string]mapdoc.Fact{}
			for _, n := range doc.Nodes {
				if n.Kind == mapdoc.NodeInterface {
					switch n.Properties["interface_kind"] {
					case "python-console-script", "python-gui-script":
						pythonKinds[n.Properties["interface_kind"]] = n.Properties["target"]
					case "dotnet-application":
						dotnet = n
					}
				}
				if n.Kind == mapdoc.NodeDeployable {
					for _, fact := range n.Facts {
						key := n.Name + ":" + fact.Name
						if stage := fact.Properties["stage"]; stage != "" {
							key += ":" + stage
						}
						facts[key] = fact
					}
				}
			}
			if pythonKinds["python-console-script"] != "weather.cli:main" || pythonKinds["python-gui-script"] != "weather.ui:launch" || len(pythonKinds) != 2 {
				t.Fatalf("Python script kinds/targets: %+v", pythonKinds)
			}
			if dotnet.Name != "App" || dotnet.Properties["target"] != "WinExe" || dotnet.Properties["condition"] == "" || dotnet.Evidence[0].Span == nil || dotnet.Evidence[0].Span.StartLine != 2 {
				t.Fatalf(".NET launch interface: %+v", dotnet)
			}
			buildEntry := facts["(root):docker_entrypoint:builder"]
			finalCommand := facts["(root):docker_cmd:runtime"]
			withheldEntry := facts["(root):docker_entrypoint_arguments:builder"]
			if buildEntry.Value != "python3 -m http.server" || buildEntry.Properties["stage"] != "builder" || buildEntry.Properties["stage_is_final"] != "false" {
				t.Fatalf("builder stage launch facet: %+v", buildEntry)
			}
			if finalCommand.Value != "node" || finalCommand.Properties["stage"] != "runtime" || finalCommand.Properties["stage_is_final"] != "true" || finalCommand.Properties["instruction_form"] != "exec" {
				t.Fatalf("final stage command facet: %+v", finalCommand)
			}
			if withheldEntry.State != "withheld_arguments" || withheldEntry.Value != "" {
				t.Fatalf("Docker argv was not withheld: %+v", withheldEntry)
			}
			if facts["nightly:cron_schedule"].Value != "0 3 * * *" || facts["nightly:cron_suspend"].Value != "false" || facts["nightly:cron_timezone"].Value != "" || facts["nightly:cron_timezone"].State != "withheld_value" {
				t.Fatalf("CronJob facets: schedule=%+v suspend=%+v timezone=%+v", facts["nightly:cron_schedule"], facts["nightly:cron_suspend"], facts["nightly:cron_timezone"])
			}
			if strings.Contains(out, "DO_NOT_DISCLOSE") || strings.Contains(out, "--password") || strings.Contains(out, "--token") || strings.Contains(out, "custom-launcher") {
				t.Fatal("Docker arguments or a non-allowlisted executable leaked into the map")
			}
		})
	}
}
