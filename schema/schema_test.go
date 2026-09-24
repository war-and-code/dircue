package schema_test

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/war-and-code/dircue/internal/cli"

	"github.com/santhosh-tekuri/jsonschema/v5"
)

// Validate actual CLI output against the schema, including empty collections
// and warnings.
func TestAllOutputConformsToSchema(t *testing.T) {
	schema, err := jsonschema.Compile("profile.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	for _, populated := range []bool{false, true} {
		root := t.TempDir()
		if populated {
			files := map[string]string{
				"main.go":                     "package main\nfunc main() {}\n",
				"web/package.json":            `{"dependencies":{"react":"^19"}}`,
				"broken/package.json":         `{broken`,
				".github/workflows/build.yml": "name: build\n",
			}
			for path, content := range files {
				full := filepath.Join(root, filepath.FromSlash(path))
				if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(full, []byte(content), 0644); err != nil {
					t.Fatal(err)
				}
			}
		}
		var stdout, stderr bytes.Buffer
		if err := cli.Execute(context.Background(), []string{"analyze", "all", "--json", "--breakdown", root}, &stdout, &stderr); err != nil {
			t.Fatal(err)
		}
		var value any
		if err := json.Unmarshal(stdout.Bytes(), &value); err != nil {
			t.Fatal(err)
		}
		if err := schema.Validate(value); err != nil {
			t.Fatalf("%v\n%s", err, stdout.String())
		}
		if populated {
			object := value.(map[string]any)
			for _, field := range []string{"languages", "ecosystems", "frameworks", "layouts", "warnings"} {
				if len(object[field].([]any)) == 0 {
					t.Errorf("missing populated %s", field)
				}
			}
			foundCI := false
			for _, item := range object["layouts"].([]any) {
				finding := item.(map[string]any)
				if finding["name"] == "github-actions" && finding["root"] == "." {
					foundCI = true
				}
			}
			if !foundCI {
				t.Error("GitHub Actions file must reach layout hooks despite Enry's vendor classification")
			}
		}
	}
}

func TestMetricsOutputConformsToSchema(t *testing.T) {
	compiled, err := jsonschema.Compile("profile.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	files := map[string]string{
		"Example.java": "// Example\nclass Example {\n void run() { if (true) {} }\n}\n",
		"events.xml":   "<events>\n<event/>\n</events>\n",
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	for _, args := range [][]string{
		{"analyze", "metrics", "--json", root},
		{"analyze", "metrics", "--json", "--files", root},
		{"analyze", "metrics", "--json", "--files", "--metrics-scope", "text", root},
		{"analyze", "all", "--json", "--metrics", "--files", "--metrics-max-file-bytes", "1", root},
		{"analyze", "metrics", "--json", "--files", t.TempDir()},
		{"analyze", "metrics", "--json", "--files", "--tree-size", "1", root},
	} {
		var stdout, stderr bytes.Buffer
		if err := cli.Execute(context.Background(), args, &stdout, &stderr); err != nil {
			t.Fatal(err)
		}
		var value any
		if err := json.Unmarshal(stdout.Bytes(), &value); err != nil {
			t.Fatal(err)
		}
		if err := compiled.Validate(value); err != nil {
			t.Fatalf("%v: %v\n%s", args, err, stdout.String())
		}
		object := value.(map[string]any)
		object["schema_version"] = "1.0.0"
		if err := compiled.Validate(value); err == nil {
			t.Fatal("schema 1.0.0 must reject metrics")
		}
		object["schema_version"] = "1.1.0"
		delete(object, "metrics")
		if err := compiled.Validate(value); err == nil {
			t.Fatal("schema 1.1.0 must require metrics")
		}
	}
}
