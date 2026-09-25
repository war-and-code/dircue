package schema_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v5"
	"github.com/war-and-code/dircue/pkg/capabilities"
	"github.com/war-and-code/dircue/pkg/planning"
	"github.com/war-and-code/dircue/pkg/profile"
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
