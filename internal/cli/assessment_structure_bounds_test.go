package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/war-and-code/dircue/pkg/assessment"
	"github.com/war-and-code/dircue/pkg/profile"
)

// Graph metrics take their completeness from the evidence they rest on: a
// truncated tree makes vertices a lower bound and the group count neither
// bound, and a self-reference alone does not qualify the edge count.
func TestAssessmentStructureMetricsCarryGlobalReasons(t *testing.T) {
	root := t.TempDir()
	files := map[string]string{"package.json": `{"name":"root","workspaces":["a","b","c"]}`}
	for _, name := range []string{"a", "b", "c"} {
		files[name+"/package.json"] = `{"name":"` + name + `","dependencies":{"a":"workspace:*"}}`
	}
	for name, content := range files {
		filename := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(filename), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filename, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	full, _ := executeAssessmentJSON(t, "analyze", "assessment", "--source", "directory", "--json", root)
	d := full.Assessment.Structure.Dependencies
	if d.Projects.Count != 4 || d.Projects.Completeness != "complete" || d.DefiniteEdges.Count != 2 || d.DefiniteEdges.Completeness != "complete" || d.ConnectedGroups.Count != 2 || d.ConnectedGroups.Completeness != "complete" {
		t.Fatalf("complete graph: projects=%+v edges=%+v groups=%+v", d.Projects, d.DefiniteEdges, d.ConnectedGroups)
	}
	var out, stderr bytes.Buffer
	if err := Execute(context.Background(), []string{"analyze", "assessment", "--source", "directory", "--tree-size", "3", "--json", root}, &out, &stderr); err != nil {
		t.Fatal(err)
	}
	var truncated profile.Report
	if err := json.Unmarshal(out.Bytes(), &truncated); err != nil {
		t.Fatal(err)
	}
	if err := assessment.ValidateReport(truncated.Assessment); err != nil {
		t.Fatal(err)
	}
	d = truncated.Assessment.Structure.Dependencies
	if d.Projects.Completeness != "lower_bound" || !slices.Contains(d.Projects.Reasons, "declarations_skipped") ||
		d.DefiniteEdges.Completeness != "lower_bound" || d.ConnectedGroups.Completeness != "observed_only" || !slices.Contains(d.ConnectedGroups.Reasons, "declarations_skipped") {
		t.Fatalf("truncated graph claims completeness: projects=%+v edges=%+v groups=%+v", d.Projects, d.DefiniteEdges, d.ConnectedGroups)
	}
}
