package schema_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v5"
)

func TestDeclarationsSchemaCLI(t *testing.T) {
	compiled, err := jsonschema.Compile("profile.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	root := projectFixture(t)
	files := map[string]string{
		"package.json":            `{"name":"demo","workspaces":["packages/*"],"scripts":{"test":"secret-command"},"engines":{"node":">=22"}}`,
		"packages/a/package.json": `{"name":"a","version":"1.0.0","bin":{"hello":"hello.js"}}`,
		"go.mod":                  "module example.invalid/demo\ngo 1.26\n",
		"python/pyproject.toml":   "[project]\nname='demo'\nversion='1.0'\nrequires-python='>=3.12'\n[project.scripts]\nhello='demo:main'\n",
		"rust/Cargo.toml":         "[package]\nname='demo'\nversion='1.0.0'\nedition='2021'\n",
	}
	for name, data := range files {
		p := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
	for _, args := range [][]string{
		{"analyze", "declarations", "--json", root},
		{"analyze", "all", "--json", "--declarations", "--projects", "--discovery", "--graph", root},
		{"analyze", "declarations", "--json", "--max-file-bytes", "1", root},
		{"analyze", "declarations", "--json", "--tree-size", "1", root},
		{"analyze", "declarations", "--json", t.TempDir()},
	} {
		v := schemaOutput(t, args)
		validateProfile(t, compiled, v)
		if v["schema_version"] != "1.4.0" {
			t.Fatalf("wrong version %v", v["schema_version"])
		}
		v["schema_version"] = "1.3.0"
		if compiled.Validate(v) == nil {
			t.Fatal("accepted declaration expansion as old schema")
		}
	}
	v := schemaOutput(t, []string{"analyze", "all", "--json", root})
	v["schema_version"] = "1.4.0"
	if compiled.Validate(v) == nil {
		t.Fatal("accepted 1.4 without declarations")
	}
}
