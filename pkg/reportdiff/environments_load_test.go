package reportdiff

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"github.com/war-and-code/dircue/pkg/declarations"
	"github.com/war-and-code/dircue/pkg/environments"
)

func environmentProfileJSON(t *testing.T) []byte {
	t.Helper()
	p := emptyProfile()
	p.SchemaVersion = "1.7.0"
	var err error
	p.Declarations, err = declarations.New("directory", "", 0).Finish(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	p.Environments, err = environments.Analyze(context.Background(), environments.Input{Source: "directory", InventoryComplete: true, Declarations: *p.Declarations}, environments.Limits{})
	if err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestReadEvidenceRejectsEnvironmentSemanticInconsistency(t *testing.T) {
	valid := environmentProfileJSON(t)
	if _, _, err := ReadEvidence(bytes.NewReader(valid)); err != nil {
		t.Fatalf("valid environment report: %v", err)
	}
	var value map[string]any
	if err := json.Unmarshal(valid, &value); err != nil {
		t.Fatal(err)
	}
	env := value["environments"].(map[string]any)
	coverage := env["coverage"].(map[string]any)
	coverage["global_json_read"] = float64(1)
	b, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = ReadEvidence(bytes.NewReader(b)); err == nil {
		t.Fatal("accepted impossible environment coverage")
	}
}
