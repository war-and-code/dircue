package schema_test

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v5"
	"github.com/war-and-code/dircue/internal/cli"
)

func TestPackageEvidenceSchema(t *testing.T) {
	s, err := jsonschema.Compile("profile.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	fixture, err := filepath.Abs("../tests/packageevidence/fixtures/syft-1.52.0.json")
	if err != nil {
		t.Fatal(err)
	}
	for _, flags := range [][]string{{}, {"--syft-root", "/"}, {"--syft-root", "/", "--graph", "--discovery", "--metrics"}} {
		args := append([]string{"analyze", "all", "--json", "--syft-report", fixture}, flags...)
		args = append(args, t.TempDir())
		var out, stderr bytes.Buffer
		if err := cli.Execute(context.Background(), args, &out, &stderr); err != nil {
			t.Fatal(err)
		}
		var value map[string]any
		if err := json.Unmarshal(out.Bytes(), &value); err != nil {
			t.Fatal(err)
		}
		if err := s.Validate(value); err != nil {
			t.Fatalf("%v\n%s", err, out.String())
		}
		for _, v := range []string{"1.0.0", "1.1.0", "1.2.0"} {
			value["schema_version"] = v
			if err := s.Validate(value); err == nil {
				t.Fatalf("oldschema%s accepted package evidence", v)
			}
		}
		value["schema_version"] = "1.3.0"
		value["package_evidence"].(map[string]any)["invented_field"] = true
		if err := s.Validate(value); err == nil {
			t.Fatal("schema accepted unknown package evidence field")
		}
	}
}
