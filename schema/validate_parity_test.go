package schema_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"

	upstream "github.com/santhosh-tekuri/jsonschema/v5"
	"github.com/war-and-code/dircue/internal/cli"
	private "github.com/war-and-code/dircue/schema"
)

func parityOracle(t *testing.T) *upstream.Schema {
	t.Helper()
	c := upstream.NewCompiler()
	c.LoadURL = func(string) (io.ReadCloser, error) { return nil, fmt.Errorf("resource is not bundled") }
	for _, name := range []string{"profile.schema.json", "declarations.schema.json", "formats.schema.json", "hotspots.schema.json", "focus.schema.json", "availability.schema.json", "explanation.schema.json", "environments.schema.json", "lockfiles.schema.json", "assessment.schema.json"} {
		data, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		if err := c.AddResource("https://dircue.invalid/schema/"+name, bytes.NewReader(data)); err != nil {
			t.Fatal(err)
		}
	}
	s, err := c.Compile("https://dircue.invalid/schema/profile.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestPrivateValidatorProfileParity(t *testing.T) {
	oracle := parityOracle(t)
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.test/parity\ngo 1.26.0\n"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, flags := range [][]string{nil, {"--declarations"}, {"--discovery"}, {"--formats"}, {"--environments"}, {"--lockfiles"}, {"--environments", "--lockfiles"}, {"--assessment"}} {
		args := append([]string{"analyze", "all", "--json"}, flags...)
		var out, stderr bytes.Buffer
		if err := cli.Execute(context.Background(), append(args, root), &out, &stderr); err != nil {
			t.Fatal(err)
		}
		var original map[string]any
		decoder := json.NewDecoder(&out)
		decoder.UseNumber()
		if err := decoder.Decode(&original); err != nil {
			t.Fatal(err)
		}
		check := func(label string, value any, wantValid bool) {
			t.Helper()
			a, b := private.ValidateProfile(value), oracle.Validate(value)
			if (a == nil) != wantValid || (a == nil) != (b == nil) {
				t.Fatalf("%s: private=%v upstream=%v expected_valid=%v", label, a, b, wantValid)
			}
		}
		check("actual CLI report", original, true)
		check("wrong root type", []any{}, false)
		clone := func() map[string]any {
			copy := make(map[string]any, len(original))
			for k, v := range original {
				copy[k] = v
			}
			return copy
		}
		value := clone()
		summary := map[string]any{"path": "venv", "kind": "virtualenv", "ecosystem": "python", "marker": "venv/pyvenv.cfg", "basis": "environment-marker", "entries": json.Number("2"), "bytes": json.Number("10"), "bounded": true, "lower_bound": true, "reason": "environment_tree_entry_limit"}
		value["summarized_trees"] = []any{summary}
		check("existing summarized-tree reason", value, true)
		summary["reason"] = 7
		check("invalid summarized-tree reason", value, false)
		summary["reason"] = "environment_tree_entry_limit"
		summary["unknown"] = true
		check("unknown summarized-tree property", value, false)
		for _, required := range []string{"root", "schema_version", "summary", "languages", "warnings"} {
			value := clone()
			delete(value, required)
			check("missing "+required, value, false)
		}
		for key, replacement := range map[string]any{"schema_version": "999.0.0", "root": 7, "languages": "invalid", "warnings": map[string]any{}, "unknown": true} {
			value := clone()
			value[key] = replacement
			check("invalid "+key, value, false)
		}
		if declaration, ok := original["declarations"].(map[string]any); ok {
			declaration["status"] = "not-a-status"
			check("invalid declaration status", original, false)
		}
	}
}

// Release-generated input artifacts live outside the source tree. Ordinary
// test runs use the generated reports above; release audits supply this path.
func TestPrivateValidatorHistoricalProfileParity(t *testing.T) {
	directory := os.Getenv("DIRCUE_JSONSCHEMA_HISTORICAL_REPORTS")
	if directory == "" {
		t.Skip("set DIRCUE_JSONSCHEMA_HISTORICAL_REPORTS for release-generated report artifacts")
	}
	oracle := parityOracle(t)
	files, err := filepath.Glob(filepath.Join(directory, "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	checked := 0
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		var decoded any
		decoder := json.NewDecoder(bytes.NewReader(data))
		decoder.UseNumber()
		if err := decoder.Decode(&decoded); err != nil {
			t.Fatal(err)
		}
		value, object := decoded.(map[string]any)
		if !object {
			continue
		}
		if _, report := value["schema_version"]; !report {
			continue
		}
		if value["summary"] == nil {
			continue
		}
		checked++
		if a, b := private.ValidateProfile(value), oracle.Validate(value); a != nil || b != nil {
			t.Fatalf("%s: private=%v upstream=%v", filepath.Base(file), a, b)
		}
		value["schema_version"] = "unsupported"
		if private.ValidateProfile(value) == nil || oracle.Validate(value) == nil {
			t.Fatal("mutated historical version accepted")
		}
	}
	if checked == 0 {
		t.Fatal("no historical profiles found")
	}
	t.Logf("validated %d historical reports plus invalid mutations against unmodified upstream", checked)
}
