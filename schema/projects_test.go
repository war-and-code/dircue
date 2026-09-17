package schema_test

import (
	"bytes"
	"context"
	"encoding/json"
	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
	"os"
	"path/filepath"
	"testing"
	"time"

	"dircue/internal/cli"
	"dircue/pkg/profile"
	"dircue/pkg/structure"
	"github.com/santhosh-tekuri/jsonschema/v5"
)

func projectFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	files := map[string]string{
		"global.json":           `{"sdk":{"version":"8.0.300"}}`,
		"Directory.Build.props": `<Project><PropertyGroup><LangVersion>preview</LangVersion></PropertyGroup></Project>`,
		"App.slnx":              `<Solution><Project Path="app/App.csproj"/></Solution>`,
		"app/App.csproj":        `<Project Sdk="Microsoft.NET.Sdk"><PropertyGroup><TargetFrameworks>net8.0;net9.0</TargetFrameworks></PropertyGroup><ItemGroup><ProjectReference Include="../core/Core.csproj"/><ProjectReference Include="$(Unknown)/Other.csproj" Condition="'$(OS)'=='Windows_NT'"/></ItemGroup></Project>`,
		"core/Core.csproj":      `<Project><Import Project="absent.props"/></Project>`,
		"app/App.cs":            "class App { static void Main() {} }\n",
		"java/pom.xml":          `<project><modelVersion>4.0.0</modelVersion><groupId>example</groupId><artifactId>app</artifactId><version>1</version><modules><module>lib</module></modules><properties><maven.compiler.release>21</maven.compiler.release></properties></project>`,
		"java/lib/pom.xml":      `<project><parent><groupId>example</groupId><artifactId>app</artifactId><version>1</version></parent><artifactId>lib</artifactId></project>`,
		"broken/Bad.csproj":     `<Project>`,
		"events.xml":            "<events/>\n",
		"README.md":             "Example project inventory.\n",
	}
	for name, content := range files {
		filename := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(filename), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filename, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func schemaOutput(t *testing.T, args []string) map[string]any {
	t.Helper()
	var stdout, stderr bytes.Buffer
	if err := cli.Execute(context.Background(), args, &stdout, &stderr); err != nil {
		t.Fatalf("%v: %v (%s)", args, err, stderr.String())
	}
	var value map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &value); err != nil {
		t.Fatal(err)
	}
	return value
}
func validateProfile(t *testing.T, compiled *jsonschema.Schema, value any) {
	t.Helper()
	if err := compiled.Validate(value); err != nil {
		data, _ := json.MarshalIndent(value, "", "  ")
		t.Fatalf("%v\n%s", err, data)
	}
}

func TestProjectsSchemaCLI(t *testing.T) {
	compiled, err := jsonschema.Compile("profile.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	root := projectFixture(t)
	for _, args := range [][]string{
		{"analyze", "projects", "--json", root},
		{"analyze", "all", "--json", "--projects", root},
		{"analyze", "all", "--json", "--projects", "--metrics", root},
		{"analyze", "projects", "--json", "--tree-size", "1", root},
		{"analyze", "projects", "--json", "--max-file-bytes", "1", root},
		{"analyze", "projects", "--json", t.TempDir()},
	} {
		value := schemaOutput(t, args)
		validateProfile(t, compiled, value)
		if value["schema_version"] != "1.2.0" {
			t.Fatal("project output must use schema 1.2.0")
		}
		for _, old := range []string{"1.0.0", "1.1.0"} {
			value["schema_version"] = old
			if compiled.Validate(value) == nil {
				t.Fatalf("%s accepted projects", old)
			}
		}
	}
}

func TestExpandedSchemaRejectsInvalidContracts(t *testing.T) {
	compiled, err := jsonschema.Compile("profile.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	value := schemaOutput(t, []string{"analyze", "projects", "--json", projectFixture(t)})
	data, _ := json.Marshal(value)
	for name, mutate := range map[string]func(map[string]any){
		"missing expansion": func(v map[string]any) { delete(v, "projects") },
		"invalid state": func(v map[string]any) {
			p := v["projects"].(map[string]any)["projects"].([]any)[1].(map[string]any)
			p["requirements"].([]any)[0].(map[string]any)["state"] = "assumed"
		},
		"unknown project field": func(v map[string]any) {
			p := v["projects"].(map[string]any)["projects"].([]any)[0].(map[string]any)
			p["build_succeeded"] = true
		},
		"negative bytes": func(v map[string]any) { v["projects"].(map[string]any)["unassigned"].(map[string]any)["bytes"] = -1 },
		"directory tree": func(v map[string]any) {
			v["projects"].(map[string]any)["tree"] = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
		},
		"unknown composition": func(v map[string]any) {
			v["projects"].(map[string]any)["composition"].([]any)[0].(map[string]any)["name"] = "proven-safe"
		},
	} {
		t.Run(name, func(t *testing.T) {
			var changed map[string]any
			json.Unmarshal(data, &changed)
			mutate(changed)
			if compiled.Validate(changed) == nil {
				t.Fatal("invalid contract accepted")
			}
		})
	}
}

func TestStructureSchemaContract(t *testing.T) {
	compiled, err := jsonschema.Compile("profile.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	value := schemaOutput(t, []string{"analyze", "all", "--json", t.TempDir()})
	observations := map[string]uint64{}
	for _, k := range []string{"classes", "interfaces", "records", "structs", "enums", "methods", "constructors", "properties", "imports", "lambdas", "local_functions", "syntax_nodes", "error_nodes", "missing_nodes"} {
		observations[k] = 0
	}
	observations["syntax_nodes"] = 1
	files := []structure.File{{Path: "A.java", Language: "Java", Status: "complete", ParseCount: 1, Observations: observations, Metrics: json.RawMessage(`{"cyclomatic":{"sum":1}}`), Provenance: &structure.Provenance{BCA: "big-code-analysis@2.2.0", TreeSitter: "0.26.12", Grammar: "tree-sitter-java@0.23.5"}}}
	report := profile.StructureReport{SupportedLanguages: structure.Capabilities(), ObservationFiles: map[string]int64{}, Engine: "big-code-analysis", EngineVersion: "2.2.0", Status: "complete", Scope: "source", Source: "directory", MaxFileBytes: 8388608, AnalyzedFiles: 1, ParseCount: 1, Omissions: map[string]int64{}, Observations: observations, Files: &files}
	encoded, _ := json.Marshal(report)
	var body map[string]any
	json.Unmarshal(encoded, &body)
	value["structure"] = body
	value["schema_version"] = "1.2.0"
	validateProfile(t, compiled, value)
	for _, old := range []string{"1.0.0", "1.1.0"} {
		value["schema_version"] = old
		if compiled.Validate(value) == nil {
			t.Fatalf("%s accepted structure", old)
		}
	}
	value["schema_version"] = "1.2.0"
	file := body["files"].([]any)[0].(map[string]any)
	file["parse_count"] = 2
	if compiled.Validate(value) == nil {
		t.Fatal("two parses accepted")
	}
	file["parse_count"] = 1
	file["status"] = "partial"
	if compiled.Validate(value) == nil {
		t.Fatal("partial missing syntax evidence accepted")
	}
	file["syntax_errors"] = true
	file["reason"] = "syntax_errors"
	validateProfile(t, compiled, value)
	for _, capability := range structure.Capabilities() {
		t.Run(capability.Language, func(t *testing.T) {
			file["language"] = capability.Language
			file["provenance"].(map[string]any)["grammar"] = capability.Grammar
			counts := map[string]any{}
			for _, key := range capability.Observations {
				counts[key] = float64(0)
			}
			counts["syntax_nodes"] = float64(1)
			file["observations"] = counts
			validateProfile(t, compiled, value)
			if capability.Language != "Java" && capability.Language != "C#" {
				counts["classes"] = float64(0)
				if compiled.Validate(value) == nil {
					t.Fatal("unavailable declaration observation accepted")
				}
			}
		})
	}
}

func TestNativeStructureCLIConformsToSchema(t *testing.T) {
	worker := os.Getenv("DIRCUE_STRUCTURAL_WORKER")
	if worker == "" {
		t.Skip("native structural worker not supplied")
	}
	compiled, err := jsonschema.Compile("profile.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	root := projectFixture(t)
	for name, source := range map[string]string{"main.py": "def hello():\n    return 1\n", "main.go": "package main\nfunc main() {}\n", "main.tsx": "export const App = () => <div/>;\n"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(source), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "Broken.java"), []byte("class {"), 0600); err != nil {
		t.Fatal(err)
	}
	// Windows may require privileges for symlink creation; exercise it when available.
	_ = os.Symlink("app/App.cs", filepath.Join(root, "linked.cs"))
	for _, args := range [][]string{
		{"analyze", "structure", "--json", "--files", "--structural-worker", worker, root},
		{"analyze", "all", "--json", "--projects", "--metrics", "--structure", "--files", "--structural-worker", worker, root},
		{"analyze", "structure", "--json", "--structural-worker", worker, "--structural-max-file-bytes", "1", root},
		{"analyze", "structure", "--json", "--structural-worker", worker, t.TempDir()},
	} {
		validateProfile(t, compiled, schemaOutput(t, args))
	}
}

func TestProjectsGitSchema(t *testing.T) {
	root := projectFixture(t)
	repo, err := git.PlainInit(root, false)
	if err != nil {
		t.Fatal(err)
	}
	tree, err := repo.Worktree()
	if err != nil {
		t.Fatal(err)
	}
	if err := tree.AddWithOptions(&git.AddOptions{All: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := tree.Commit("fixture", &git.CommitOptions{Author: &object.Signature{Name: "Fixture", Email: "fixture@example.invalid", When: time.Unix(1, 0)}}); err != nil {
		t.Fatal(err)
	}
	compiled, err := jsonschema.Compile("profile.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	value := schemaOutput(t, []string{"analyze", "projects", "--json", "--source", "git", root})
	validateProfile(t, compiled, value)
	body := value["projects"].(map[string]any)
	if body["source"] != "git" || body["tree"] == "" {
		t.Fatal("missing Git provenance")
	}
	delete(body, "tree")
	if compiled.Validate(value) == nil {
		t.Fatal("Git source without tree accepted")
	}
}
