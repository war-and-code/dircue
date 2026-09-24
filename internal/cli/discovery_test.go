package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/war-and-code/dircue/pkg/profile"
)

func TestDiscoveryAndGraphCLI(t *testing.T) {
	root := t.TempDir()
	files := map[string]string{
		"A/A.csproj":      `<Project><ItemGroup><ProjectReference Include="../B/B.csproj"/></ItemGroup></Project>`,
		"B/B.csproj":      `<Project><ItemGroup><ProjectReference Include="../A/A.csproj"/></ItemGroup></Project>`,
		"A/Program.cs":    "class Program {}",
		"data/events.xml": "not even valid XML",
		"vendor/drop.dll": "filename evidence, not a validated binary",
	}
	for name, data := range files {
		full := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(full), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
	run := func(args ...string) profile.Report {
		t.Helper()
		var out, stderr bytes.Buffer
		args = append(args, "--json", "--source", "directory", root)
		if err := Execute(context.Background(), args, &out, &stderr); err != nil {
			t.Fatalf("%v: %v (%s)", args, err, stderr.String())
		}
		var r profile.Report
		if err := json.Unmarshal(out.Bytes(), &r); err != nil {
			t.Fatal(err)
		}
		return r
	}
	d := run("analyze", "discovery")
	if d.SchemaVersion != profile.EnhancedSchemaVersion || d.Discovery == nil || d.Discovery.Inventory.Files != int64(len(files)) {
		t.Fatalf("discovery: %+v", d)
	}
	if len(d.Languages) != 0 || d.Projects != nil || d.Metrics != nil || d.Structure != nil || len(d.Frameworks) != 0 {
		t.Fatal("discovery must not enable content analysis")
	}
	if d.Discovery.Scope.ContentInspection != "none" || d.Discovery.ClassificationBytesRead != 0 {
		t.Fatal("discovery must be metadata-only")
	}
	foundArtifact := false
	for _, c := range d.Discovery.Candidates {
		if c.Path == "vendor/drop.dll" {
			foundArtifact = true
		}
	}
	if !foundArtifact {
		t.Fatal("language exclusion must not hide the artifact")
	}
	g := run("analyze", "graph")
	if g.Graph == nil || g.Projects == nil || g.Graph.Coverage.UniqueEdges != 2 || len(g.Graph.Cycles) != 1 {
		t.Fatalf("graph: %+v", g.Graph)
	}
	combined := run("analyze", "all", "--discovery", "--graph", "--metrics")
	if combined.Discovery == nil || combined.Graph == nil || combined.Metrics == nil || combined.SchemaVersion != profile.EnhancedSchemaVersion {
		t.Fatal("combined requested modules missing")
	}
	if !bytes.Equal(mustJSON(t, combined.Discovery), mustJSON(t, d.Discovery)) || !bytes.Equal(mustJSON(t, combined.Graph), mustJSON(t, g.Graph)) {
		t.Fatal("module results differ between standalone and combined invocation")
	}
	legacy := run("analyze", "all")
	if legacy.Discovery != nil || legacy.Graph != nil || legacy.SchemaVersion != profile.SchemaVersion {
		t.Fatal("optional modules leaked into default analysis")
	}
	for _, args := range [][]string{{"analyze", "discovery", root}, {"analyze", "graph", root}, {"analyze", "all", "--graph", "--discovery", root}} {
		var out, stderr bytes.Buffer
		if err := Execute(context.Background(), args, &out, &stderr); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out.String(), "status:") {
			t.Fatalf("missing readable status: %s", out.String())
		}
	}
}

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	b, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestDiscoveryAndGraphLimitsAndErrors(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "A.csproj"), []byte("<Project/>"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"discovery", "graph"} {
		var out, stderr bytes.Buffer
		if err := Execute(context.Background(), []string{"analyze", mode, "--tree-size", "1", "--json", root}, &out, &stderr); err != nil {
			t.Fatal(err)
		}
		var r profile.Report
		if err := json.Unmarshal(out.Bytes(), &r); err != nil {
			t.Fatal(err)
		}
		if len(r.Warnings) == 0 {
			t.Fatal("tree limit requires coverage warning")
		}
		if mode == "discovery" && (r.Discovery == nil || r.Discovery.Status == "complete") {
			t.Fatal("limited discovery cannot be complete")
		}
		if mode == "graph" && (r.Graph == nil || r.Graph.Status == "complete") {
			t.Fatal("limited graph cannot be complete")
		}
		out.Reset()
		if err := Execute(context.Background(), []string{"analyze", mode, filepath.Join(root, "A.csproj")}, &out, &stderr); err == nil || out.Len() != 0 {
			t.Fatal("file input must fail without a success report")
		}
	}
}
