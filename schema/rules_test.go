package schema_test

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"dircue/internal/cli"
	"github.com/santhosh-tekuri/jsonschema/v5"
)

func TestRulesSchema(t *testing.T) {
	s, err := jsonschema.Compile("profile.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	policy := filepath.Join(t.TempDir(), "rules.json")
	for name, data := range map[string]string{"a.txt": "marker", "b.txt": "no match"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(policy, []byte(`{"schema_version":"1.0.0","rules":[{"id":"text","match":{"extensions":[".txt"]}},{"id":"marker","match":{"extensions":[".txt"]},"content":{"contains_utf8":"marker"}}]}`), 0600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"analyze", "rules"}, {"analyze", "all"}, {"analyze", "rules", "--tree-size", "1"}, {"analyze", "rules", "--max-file-bytes", "1"}} {
		var out, stderr bytes.Buffer
		args = append(args, "--rules-file", policy, "--json", "--source", "directory", root)
		if err := cli.Execute(context.Background(), args, &out, &stderr); err != nil {
			t.Fatal(err)
		}
		var value map[string]any
		if err := json.Unmarshal(out.Bytes(), &value); err != nil {
			t.Fatal(err)
		}
		if err := s.Validate(value); err != nil {
			t.Fatalf("%v: %v\n%s", args, err, out.String())
		}
		for _, version := range []string{"1.0.0", "1.1.0", "1.2.0"} {
			value["schema_version"] = version
			if err := s.Validate(value); err == nil {
				t.Fatalf("%s accepted rules", version)
			}
		}
		value["schema_version"] = "1.3.0"
		r := value["rules"].(map[string]any)
		r["unexpected"] = true
		if err := s.Validate(value); err == nil {
			t.Fatal("unknown report member accepted")
		}
		delete(r, "unexpected")
		r["source_consistency"] = "selected_git_tree"
		if err := s.Validate(value); err == nil {
			t.Fatal("directory source claimed Git consistency")
		}
		r["source_consistency"] = "live_directory"
		observations := r["observations"].([]any)
		for _, item := range observations {
			ob := item.(map[string]any)
			if ob["evidence"] == "content" {
				saved := ob["source_sha256"]
				delete(ob, "source_sha256")
				if err := s.Validate(value); err == nil {
					t.Fatal("content match lost digest")
				}
				ob["source_sha256"] = saved
			}
		}
		delete(value, "rules")
		if err := s.Validate(value); err == nil {
			t.Fatal("1.3.0 accepted no new module")
		}
	}
}
