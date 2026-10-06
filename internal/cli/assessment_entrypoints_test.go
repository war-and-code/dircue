package cli

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/war-and-code/dircue/pkg/declarations"
	"github.com/war-and-code/dircue/pkg/mapdoc"
	"github.com/war-and-code/dircue/pkg/scanner"
)

func TestAssessmentSameNameEntryPointsKeepDistinctKinds(t *testing.T) {
	root := t.TempDir()
	const privateCommand = "echo ENTRY_COMMAND_MUST_NOT_LEAK"
	content := `{"name":"entry-fixture","bin":{"start":"cli.js"},"scripts":{"start":"` + privateCommand + `"}}`
	if err := os.WriteFile(filepath.Join(root, "package.json"), []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	r, raw := executeAssessmentJSON(t, "analyze", "assessment", "--source", "directory", "--json", root)
	var kinds []string
	for _, e := range r.Assessment.Structure.EntryPoints {
		if e.Name == "start" && e.ProjectID == "package.json" {
			kinds = append(kinds, e.Kind)
			if e.Ecosystem != "npm" {
				t.Fatalf("manifest taxonomy changed: %+v", e)
			}
		}
	}
	slices.Sort(kinds)
	if !slices.Equal(kinds, []string{"manifest_interface:entrypoint", "manifest_interface:script"}) || r.Assessment.Structure.EntryPointCount != 2 {
		t.Fatalf("same-name interfaces collapsed: kinds=%v report=%+v", kinds, r.Assessment.Structure)
	}
	if strings.Contains(string(raw), privateCommand) {
		t.Fatal("assessment exposed an npm script command")
	}
}

func TestAssessmentAssociationFailureKeepsIndependentEvidence(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "package.json"), []byte(`{"scripts":{"start":"node app.js"}}`), 0600); err != nil {
		t.Fatal(err)
	}
	r, err := scanner.Scan(context.Background(), root, scanner.Options{Source: "directory", Assessment: true})
	if err != nil {
		t.Fatal(err)
	}
	before := r.Assessment.Inventory.Files.Count
	// Removing discovery makes the map association layer unavailable; it must
	// not discard valid inventory or directly declared manifest interfaces.
	r.Discovery = nil
	if err := enrichAssessmentEntryPoints(context.Background(), r, nil); err != nil {
		t.Fatal(err)
	}
	if r.Assessment.Inventory.Files.Count != before || r.Assessment.Structure.EntryPointCount != 1 {
		t.Fatalf("independent observations lost on association error: %+v", r.Assessment)
	}
	var qualified bool
	for _, c := range r.Assessment.Structure.Coverage {
		if c.Scope == "entry_points" && c.Ecosystem == "all" && c.Status == "partial" && slices.Contains(c.Reasons, "entry_point_association_unavailable") {
			qualified = true
		}
	}
	if !qualified {
		t.Fatal("association failure was not disclosed")
	}
}

func TestAssessmentDeploymentUsesCanonicalProjectEcosystem(t *testing.T) {
	d := mapdoc.Document{
		Nodes: []mapdoc.Node{
			{ID: "component", Kind: mapdoc.NodeComponent, Evidence: []mapdoc.Evidence{{Path: "App.csproj"}}, Properties: map[string]string{"ecosystem": "dotnet"}},
			{ID: "docker", Kind: mapdoc.NodeDeployable, Name: "Dockerfile", Evidence: []mapdoc.Evidence{{Path: "Dockerfile", Basis: mapdoc.BasisDeclaredConfig}}, Coverage: mapdoc.Coverage{Status: mapdoc.CoverageComplete}, Properties: map[string]string{"provider": "docker", "kind": "image"}},
		},
		Edges: []mapdoc.Edge{{From: "docker", To: "component", Type: mapdoc.EdgeBuilds, Coverage: mapdoc.Coverage{Status: mapdoc.CoveragePartial, Reasons: []string{"literal_project_path"}}}},
	}
	entries := assessmentEntries(d)
	if len(entries) != 1 || entries[0].Ecosystem != "nuget" || entries[0].State != "qualified" || entries[0].ProjectID != "App.csproj" {
		t.Fatalf("deployment association taxonomy/qualification mismatch: %+v", entries)
	}
}

func TestAssessmentConditionalManifestInterfaceRemainsQualified(t *testing.T) {
	r := &declarations.Report{Projects: []declarations.Project{{ID: "App.csproj", Interfaces: []declarations.Interface{{Kind: "dotnet-application", Name: "App", State: "declared", Evidence: "App.csproj", Condition: "$(Configuration) == Release"}}}}}
	entries := assessmentManifestEntries(r)
	if len(entries) != 1 || entries[0].State != "qualified" || entries[0].Reason == "" || entries[0].Ecosystem != "nuget" {
		t.Fatalf("conditional launch declaration became definite: %+v", entries)
	}
	if got := assessmentManifestEntries(nil); len(got) != 0 {
		t.Fatalf("nil declarations yielded entries: %+v", got)
	}
}
