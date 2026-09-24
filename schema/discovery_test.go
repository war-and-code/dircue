package schema_test

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v5"
	"github.com/war-and-code/dircue/internal/cli"
)

func TestDiscoveryAndGraphSchema(t *testing.T) {
	s, err := jsonschema.Compile("profile.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "A.csproj"), []byte(`<Project><ItemGroup><ProjectReference Include="absent.csproj"/></ItemGroup></Project>`), 0600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"analyze", "discovery"}, {"analyze", "graph"}, {"analyze", "all", "--graph", "--discovery", "--metrics"}, {"analyze", "discovery", "--tree-size", "1"}, {"analyze", "graph", "--tree-size", "1"}} {
		var out, stderr bytes.Buffer
		args = append(args, "--json", "--source", "directory", root)
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
				t.Fatalf("%s accepted new modules", version)
			}
		}
		v["schema_version"] = "1.3.0"
		if _, exists := v["graph"]; exists {
			saved := v["projects"]
			delete(v, "projects")
			if err := s.Validate(v); err == nil {
				t.Fatal("graph must retain its input project evidence")
			}
			v["projects"] = saved
		}
		delete(v, "graph")
		delete(v, "discovery")
		if err := s.Validate(v); err == nil {
			t.Fatal("1.3.0 must have a new optional module")
		}
	}
}
