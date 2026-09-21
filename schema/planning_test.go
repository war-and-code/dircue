package schema_test

import (
	"context"
	"encoding/json"
	"testing"

	"dircue/pkg/capabilities"
	"dircue/pkg/planning"
	"dircue/pkg/profile"
	"github.com/santhosh-tekuri/jsonschema/v5"
)

func TestCapabilityDescriptorSchema(t *testing.T) {
	s, err := jsonschema.Compile("capabilities.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(capabilities.Dircue("test"))
	var value any
	if err := json.Unmarshal(b, &value); err != nil {
		t.Fatal(err)
	}
	if err := s.Validate(value); err != nil {
		t.Fatal(err)
	}
}

func TestPlanningSchemaCompiles(t *testing.T) {
	s, err := jsonschema.Compile("planning.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	r, err := planning.Build(context.Background(), planning.Input{Profile: &profile.Report{SchemaVersion: "1.0.0", Root: "declared-only"}, ReportSHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Capabilities: capabilities.Dircue("test"), Selection: planning.Selection{Modules: []string{"metrics"}}})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(r)
	var value any
	if err := json.Unmarshal(b, &value); err != nil {
		t.Fatal(err)
	}
	if err := s.Validate(value); err != nil {
		t.Fatal(err)
	}
}
