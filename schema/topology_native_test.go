package schema_test

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/war-and-code/dircue/internal/cli"
)

// Validate actual CLI output with both the bundled validator and the
// independent upstream implementation used for exported-schema contracts.
func TestProjectLaunchTopologyCLIConformsToExportedMapSchema(t *testing.T) {
	bundled, upstream := compileExportPair(t, "map")
	root, err := filepath.Abs("../tests/map_corpus/fixtures/project-launch-topology")
	if err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if err := cli.Execute(context.Background(), []string{"map", "--source", "directory", "--json", root}, &stdout, &stderr); err != nil {
		t.Fatalf("topology map: %v: %s", err, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("unexpected topology diagnostic: %s", stderr.String())
	}
	var document any
	if err := json.Unmarshal(stdout.Bytes(), &document); err != nil {
		t.Fatal(err)
	}
	if err := bundled.Validate(document); err != nil {
		t.Fatalf("bundled validator rejected actual topology map: %v", err)
	}
	if err := upstream.Validate(document); err != nil {
		t.Fatalf("exported schema rejected actual topology map: %v", err)
	}
}
