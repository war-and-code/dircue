package schema_test

import (
	"bytes"
	"context"
	"dircue/internal/cli"
	"encoding/json"
	"github.com/santhosh-tekuri/jsonschema/v5"
	"os"
	"path/filepath"
	"testing"
)

func TestStandaloneOutputSchemas(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "package.json"), []byte(`{"dependencies":{"react":"1"}}`), 0600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		args []string
	}{
		{"languages", []string{"--json", root}},
		{"languages", []string{"analyze", "languages", "--json", "--breakdown", root}},
		{"findings", []string{"analyze", "ecosystems", "--json", root}},
		{"findings", []string{"analyze", "frameworks", "--json", root}},
	} {
		schema, err := jsonschema.Compile(tc.name + ".schema.json")
		if err != nil {
			t.Fatal(err)
		}
		var out, stderr bytes.Buffer
		if err := cli.Execute(context.Background(), tc.args, &out, &stderr); err != nil {
			t.Fatal(err)
		}
		var value any
		if err := json.Unmarshal(out.Bytes(), &value); err != nil {
			t.Fatal(err)
		}
		if err := schema.Validate(value); err != nil {
			t.Fatalf("%v: %v", tc.args, err)
		}
		if tc.name == "languages" {
			value.(map[string]any)["Go"].(map[string]any)["unexpected"] = true
		} else {
			rows := value.([]any)
			if len(rows) == 0 {
				t.Fatalf("empty %v", tc.args)
			}
			rows[0].(map[string]any)["unexpected"] = true
		}
		if err := schema.Validate(value); err == nil {
			t.Fatalf("accepted unknown field: %v", tc.args)
		}
	}
}
