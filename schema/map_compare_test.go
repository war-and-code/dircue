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

// TestMapCompareSchema verifies that 'dircue map compare --json' output
// validates against the exported map-compare schema.
func TestMapCompareSchema(t *testing.T) {
	compiled, err := jsonschema.Compile("map-compare.schema.json")
	if err != nil {
		t.Fatal(err)
	}

	// Build two slightly different maps to compare.
	root := t.TempDir()
	files := map[string]string{
		"main.go":      "package main\nfunc main() {}\n",
		"go.mod":       "module example.invalid/demo\ngo 1.24\n",
		"package.json": `{"name":"demo","dependencies":{"react":"^19"}}`,
	}
	for path, content := range files {
		full := filepath.Join(root, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}

	// Produce base map.
	baseFile := filepath.Join(t.TempDir(), "base.json")
	var baseOut bytes.Buffer
	if err := cli.Execute(context.Background(),
		[]string{"map", "--json", "--source", "directory", root},
		&baseOut, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(baseFile, baseOut.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}

	// Produce head map (same source, creating a stable comparison).
	headFile := filepath.Join(t.TempDir(), "head.json")
	var headOut bytes.Buffer
	if err := cli.Execute(context.Background(),
		[]string{"map", "--json", "--source", "directory", root},
		&headOut, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(headFile, headOut.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}

	// Run map compare --json.
	var compareOut bytes.Buffer
	if err := cli.Execute(context.Background(),
		[]string{"map", "compare", "--json", baseFile, headFile},
		&compareOut, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}

	// Parse and validate against the schema.
	var value any
	if err := json.Unmarshal(compareOut.Bytes(), &value); err != nil {
		t.Fatalf("map compare --json emitted invalid JSON: %v\n%s", err, compareOut.String())
	}
	if err := compiled.Validate(value); err != nil {
		t.Fatalf("map compare --json output does not conform to map-compare schema: %v\n%s", err, compareOut.String())
	}

	// Verify top-level contract fields.
	obj := value.(map[string]any)
	if obj["kind"] != "map_comparison" {
		t.Errorf("expected kind map_comparison, got %v", obj["kind"])
	}
	if obj["schema_version"] != "1.0.0" {
		t.Errorf("expected schema_version 1.0.0, got %v", obj["schema_version"])
	}
}
