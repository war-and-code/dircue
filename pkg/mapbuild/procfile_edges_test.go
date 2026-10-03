package mapbuild

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/war-and-code/dircue/pkg/declarations"
	"github.com/war-and-code/dircue/pkg/deployables"
	"github.com/war-and-code/dircue/pkg/discovery"
	"github.com/war-and-code/dircue/pkg/mapdoc"
	"github.com/war-and-code/dircue/pkg/profile"
)

func TestProcfileObserveToMapBuildUsesOnlySelectedSourceTarget(t *testing.T) {
	inputs := map[string]string{
		"Procfile":           "web: gunicorn app.main:app -b 0.0.0.0:$PORT -w 3\n",
		"app/main.py":        "app = object()\n",
		"app/pyproject.toml": "[project]\nname='app'\n",
		"elsewhere/main.py":  "app = object()\n",
	}
	candidates := make([]deployables.Candidate, 0, len(inputs))
	for name, body := range inputs {
		name, body := name, body
		candidates = append(candidates, deployables.Candidate{Path: name, Size: int64(len(body)), Read: func(context.Context, int64) ([]byte, int64, error) {
			return []byte(body), int64(len(body)), nil
		}})
	}
	deployableReport, err := deployables.Observe(context.Background(), candidates, deployables.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if deployableReport.Status != "complete" || len(deployableReport.Definitions) != 1 {
		t.Fatalf("Procfile report: status=%s omissions=%v definitions=%+v", deployableReport.Status, deployableReport.Omissions, deployableReport.Definitions)
	}
	definition := deployableReport.Definitions[0]
	if definition.Name != "web" || len(definition.References) != 1 || definition.References[0].SourcePath != "app/main.py" || definition.References[0].Qualification != "local" {
		t.Fatalf("selected target resolution: %+v", definition)
	}
	profileReport := &profile.Report{
		Discovery:    &discovery.Report{Status: "complete", Source: discovery.Source{Mode: "directory", Consistency: "live_directory_metadata"}},
		Declarations: &declarations.Report{Status: "complete", Projects: []declarations.Project{{ID: "app/pyproject.toml", Root: "app", Kind: "python"}}},
	}
	mapDoc, err := Build(profileReport, Options{Deployables: deployableReport})
	if err != nil {
		t.Fatal(err)
	}
	if count := countProcfileEdges(mapDoc); count != 1 {
		t.Fatalf("Build did not invoke Procfile mapping: edges=%+v", mapDoc.Edges)
	}
	// A repeated helper call must not duplicate the edge already emitted by Build.
	addProcfileEdges(&mapDoc, deployableReport)
	if count := countProcfileEdges(mapDoc); count != 1 {
		t.Fatalf("Procfile run edges = %d; map: %+v", count, mapDoc.Edges)
	}
	encoded, err := json.Marshal(mapDoc)
	if err != nil {
		t.Fatal(err)
	}
	for _, withheld := range []string{"-b 0.0.0.0", "$PORT", "-w 3", "gunicorn"} {
		if strings.Contains(string(encoded), withheld) {
			t.Errorf("Procfile command detail %q leaked into map", withheld)
		}
	}
}

func countProcfileEdges(doc mapdoc.Document) int {
	count := 0
	for _, edge := range doc.Edges {
		if edge.Type == mapdoc.EdgeRuns && len(edge.Evidence) > 0 && edge.Evidence[0].Path == "Procfile" {
			count++
		}
	}
	return count
}

func TestProcfileTargetMakesPartialRunEdgeToUniqueOwningComponent(t *testing.T) {
	doc := mapdoc.New()
	component := mapdoc.NewNode(mapdoc.NodeComponent, []string{"app/pyproject.toml", "app"}, "python")
	component.Properties = map[string]string{"root": "app", "ecosystem": "python"}
	component.Evidence = []mapdoc.Evidence{{Basis: mapdoc.BasisDeclaredConfig, Path: "app/pyproject.toml", SourceKind: mapdoc.SourceConfiguration, Rule: &mapdoc.Producer{ID: "dircue/component-declarations", Version: "1.0.0"}}}
	doc.Nodes = append(doc.Nodes, component)
	report := &deployables.Report{Definitions: []deployables.Definition{{
		Kind: "process", Provider: "procfile", Name: "web", Path: "Procfile", Coverage: "complete",
		Evidence:   []deployables.Evidence{{Field: "process", Value: "web", Line: 1, Basis: "procfile-process"}},
		References: []deployables.Reference{{Kind: "process_target", Value: "app.main", Qualification: "local", SourcePath: "app/main.py", Evidence: deployables.Evidence{Field: "command target", Value: "app.main", Line: 1, Basis: "procfile-target"}}},
	}}}
	addDeployables(&doc, report)
	addProcfileEdges(&doc, report)
	if len(doc.Edges) != 1 || doc.Edges[0].Type != mapdoc.EdgeRuns || doc.Edges[0].From == "" || doc.Edges[0].To != component.ID {
		t.Fatalf("Procfile run edge: %+v", doc.Edges)
	}
	edge := doc.Edges[0]
	if edge.Coverage.Status != mapdoc.CoveragePartial || len(edge.Evidence) != 3 || edge.Evidence[0].Path != "Procfile" || edge.Evidence[0].Span == nil || edge.Evidence[0].Span.StartLine != 1 {
		t.Fatalf("run edge lost static qualification or source evidence: %+v", edge)
	}
	if edge.Evidence[0].Rule == nil || edge.Evidence[0].Rule.ID != "dircue/deployables/procfile-target" {
		t.Fatalf("unexpected evidence rule: %+v", edge.Evidence[0])
	}
	if edge.Properties["reason"] != "procfile_literal_target_matches_component" || edge.Evidence[1].Path != "app/main.py" || edge.Evidence[2].Path != "app/pyproject.toml" {
		t.Fatalf("run edge does not explain its target/owner binding: %+v", edge)
	}
}

func TestProcfileDoesNotChooseAmbiguousOrNameOnlyComponentOwner(t *testing.T) {
	for name, components := range map[string][]mapdoc.Node{
		"ambiguous same root": {
			procfileTestComponent("app/pyproject.toml", "app", "python"),
			procfileTestComponent("app/package.json", "app", "npm"),
		},
		"same name elsewhere": {
			procfileTestComponent("elsewhere/pyproject.toml", "elsewhere", "python"),
		},
		"nearest incompatible component": {
			procfileTestComponent("pyproject.toml", ".", "python"),
			procfileTestComponent("app/package.json", "app", "npm"),
		},
	} {
		t.Run(name, func(t *testing.T) {
			doc := mapdoc.New()
			doc.Nodes = append(doc.Nodes, components...)
			report := procfileTestReport("app/main.py")
			addDeployables(&doc, report)
			addProcfileEdges(&doc, report)
			if len(doc.Edges) != 0 {
				t.Fatalf("invented a run edge: %+v", doc.Edges)
			}
			for _, node := range doc.Nodes {
				if node.Kind != mapdoc.NodeDeployable || node.Properties["provider"] != "procfile" {
					continue
				}
				for _, fact := range node.Facts {
					if fact.Name == "process_target" && (fact.Coverage.Status != mapdoc.CoveragePartial || len(fact.Coverage.Reasons) == 0) {
						t.Fatalf("unlinked target not qualified: %+v", fact)
					}
				}
			}
		})
	}
}

func TestDuplicateProcfileProcessesDoNotChooseFirstTarget(t *testing.T) {
	doc := mapdoc.New()
	doc.Nodes = append(doc.Nodes,
		procfileTestComponent("pyproject.toml", ".", "python"),
		procfileTestComponent("app/pyproject.toml", "app", "python"),
	)
	report := procfileTestReport("app/main.py")
	report.Definitions[0].Coverage = "qualified"
	report.Definitions[0].References[0] = deployables.Reference{Kind: "process_target", Value: "unresolved", Qualification: "unresolved", Evidence: deployables.Evidence{Field: "command target", Line: 1, Basis: "procfile-target"}}
	addDeployables(&doc, report)
	addProcfileEdges(&doc, report)
	if len(doc.Edges) != 0 {
		t.Fatalf("duplicate process emitted a chosen runs edge: %+v", doc.Edges)
	}
}

func procfileTestComponent(manifest, root, ecosystem string) mapdoc.Node {
	node := mapdoc.NewNode(mapdoc.NodeComponent, []string{manifest, root}, ecosystem)
	node.Properties = map[string]string{"root": root, "ecosystem": ecosystem}
	return node
}

func procfileTestReport(targetPath string) *deployables.Report {
	return &deployables.Report{Definitions: []deployables.Definition{{
		Kind: "process", Provider: "procfile", Name: "web", Path: "Procfile", Coverage: "complete",
		Evidence:   []deployables.Evidence{{Field: "process", Value: "web", Line: 1, Basis: "procfile-process"}},
		References: []deployables.Reference{{Kind: "process_target", Value: "app.main", Qualification: "local", SourcePath: targetPath, Evidence: deployables.Evidence{Field: "command target", Value: "app.main", Line: 1, Basis: "procfile-target"}}},
	}}}
}
