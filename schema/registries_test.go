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

func TestRegistriesSchema(t *testing.T) {
	s, err := jsonschema.Compile("profile.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ".npmrc"), []byte("@example:registry=https://npm.example.test/private\n"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"analyze", "registries"}, {"analyze", "registries", "--discovery"}, {"analyze", "all", "--registries"}, {"analyze", "registries", "--max-file-bytes", "1"}, {"analyze", "registries", "--tree-size", "1"}} {
		var out, stderr bytes.Buffer
		args = append(args, "--source", "directory", "--json", root)
		if err := cli.Execute(context.Background(), args, &out, &stderr); err != nil {
			t.Fatal(err)
		}
		var v map[string]any
		if err := json.Unmarshal(out.Bytes(), &v); err != nil {
			t.Fatal(err)
		}
		if err := s.Validate(v); err != nil {
			t.Fatalf("%v: %v\n%s", args, err, out.String())
		}
		for _, version := range []string{"1.0.0", "1.1.0", "1.2.0"} {
			v["schema_version"] = version
			if err := s.Validate(v); err == nil {
				t.Fatalf("older schema %s accepted registry declarations", version)
			}
		}
		v["schema_version"] = "1.3.0"
		r := v["registries"].(map[string]any)
		r["raw_configuration"] = "sensitive"
		if err := s.Validate(v); err == nil {
			t.Fatal("unexpected raw configuration field accepted")
		}
		delete(r, "raw_configuration")
		scope := r["scope"].(map[string]any)
		scope["network_access"] = true
		if err := s.Validate(v); err == nil {
			t.Fatal("unsupported network capability accepted")
		}
		scope["network_access"] = false
		source := r["source"].(map[string]any)
		source["consistency"] = "selected_git_tree"
		if err := s.Validate(v); err == nil {
			t.Fatal("directory cannot claim selected Git identity")
		}
		source["consistency"] = "live_directory"
		for _, item := range r["configurations"].([]any) {
			cfg := item.(map[string]any)
			for _, row := range cfg["declarations"].([]any) {
				d := row.(map[string]any)
				ep := d["endpoint"].(map[string]any)
				original := ep["origin"]
				for _, bad := range []string{"https://host/private", "https://user:pass@host", "https://host?token=secret", "https://host#secret"} {
					ep["origin"] = bad
					if err := s.Validate(v); err == nil {
						t.Fatal("non-origin URL accepted:", bad)
					}
				}
				ep["origin"] = original
				ep["status"] = "unresolved"
				if err := s.Validate(v); err == nil {
					t.Fatal("unresolved endpoint retained origin")
				}
				ep["status"] = "origin"
				label := d["scope"].(map[string]any)
				label["status"] = "omitted"
				if err := s.Validate(v); err == nil {
					t.Fatal("omitted identifier retained its value")
				}
				label["status"] = "qualified_identifier"
			}
		}
		delete(v, "registries")
		delete(v, "discovery")
		if err := s.Validate(v); err == nil {
			t.Fatal("new schema accepted no enabled development module")
		}
	}
}
