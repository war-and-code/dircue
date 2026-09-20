package schema_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"dircue/pkg/reportdiff"
	profileschema "dircue/schema"

	"github.com/santhosh-tekuri/jsonschema/v5"
)

func TestEmbeddedProfileValidator(t *testing.T) {
	root := t.TempDir()
	for _, args := range [][]string{
		{"analyze", "all", "--json", root},
		{"analyze", "all", "--json", "--declarations", "--projects", "--discovery", "--metrics", root},
	} {
		value := schemaOutput(t, args)
		if err := profileschema.ValidateProfile(value); err != nil {
			t.Fatal(err)
		}
		value["schema_version"] = "unsupported"
		if err := profileschema.ValidateProfile(value); err == nil {
			t.Fatal("embedded validator accepted an unsupported schema")
		}
	}
}

func TestSavedReportComparisonSchema(t *testing.T) {
	compiled, err := jsonschema.Compile("comparison.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	manifest := filepath.Join(root, "go.mod")
	write := func(version string) {
		t.Helper()
		if err := os.WriteFile(manifest, []byte("module example.invalid/demo\ngo "+version+"\n"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write("1.25.0")
	base := schemaOutput(t, []string{"analyze", "all", "--declarations", "--metrics", "--files", "--json", root})
	write("1.26.0")
	head := schemaOutput(t, []string{"analyze", "all", "--declarations", "--metrics", "--files", "--json", root})
	load := func(value map[string]any) *reportdiff.Snapshot {
		t.Helper()
		data, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		snapshot, err := reportdiff.Load(bytes.NewReader(data))
		if err != nil {
			t.Fatal(err)
		}
		return snapshot
	}
	for _, pair := range [][2]map[string]any{{base, base}, {base, head}, {head, base}} {
		report, err := reportdiff.Compare(load(pair[0]), load(pair[1]))
		if err != nil {
			t.Fatal(err)
		}
		data, err := json.Marshal(report)
		if err != nil {
			t.Fatal(err)
		}
		var value any
		if err := json.Unmarshal(data, &value); err != nil {
			t.Fatal(err)
		}
		if err := compiled.Validate(value); err != nil {
			t.Fatalf("%v\n%s", err, data)
		}
	}
}
