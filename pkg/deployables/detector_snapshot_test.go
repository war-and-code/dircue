package deployables

import (
	"context"
	"reflect"
	"testing"

	"github.com/war-and-code/dircue/pkg/profile"
)

func TestCollectorFinishRetainsInventoryLimitDiagnosticAndIsIdempotent(t *testing.T) {
	c := NewCollector(Options{})
	_, err := c.Detect(context.Background(), profile.File{Path: "Procfile", Size: 20, Content: []byte("web: node server.js\n")})
	if err != nil {
		t.Fatal(err)
	}
	// The cap transition is separately exercised at its real file/byte bounds.
	// Once it occurs, each snapshot must report it exactly once, without mutating
	// the underlying collector or losing the corresponding diagnostic.
	c.targets.capped = true
	c.targets.paths = nil
	first, second := c.Finish(), c.Finish()
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("Finish changed across snapshots: first omissions=%v second=%v", first.Omissions, second.Omissions)
	}
	if first.Status != "partial" || first.Omissions["procfile_inventory_limit"] != 1 {
		t.Fatalf("missing qualified cap omission: %+v", first)
	}
	found := false
	for _, diagnostic := range first.Diagnostics {
		if diagnostic.Code == "procfile_inventory_limit" {
			found = true
		}
	}
	if !found {
		t.Fatalf("inventory cap diagnostic was lost: %+v", first.Diagnostics)
	}
	if c.report.Omissions["procfile_inventory_limit"] != 0 {
		t.Fatal("Finish mutated the collector's omission counts")
	}
}

func TestCollectorFinishDirectorySnapshotDoesNotAliasCollector(t *testing.T) {
	c := NewCollector(Options{})
	if _, err := c.Detect(context.Background(), profile.File{Path: "api/app.py"}); err != nil {
		t.Fatal(err)
	}
	snapshot := c.Finish()
	snapshot.Directories["injected"] = true
	delete(snapshot.Directories, "api")
	next := c.Finish()
	if next.Directories["injected"] || !next.Directories["api"] {
		t.Fatalf("consumer changes escaped the snapshot: %v", next.Directories)
	}
}
