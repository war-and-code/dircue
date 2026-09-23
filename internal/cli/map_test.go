package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/war-and-code/dircue/pkg/mapdoc"
)

func TestMapOneShotPortableEvidenceAndBudget(t *testing.T) {
	root := t.TempDir()
	for name, content := range map[string]string{
		"go.mod":    "module example.test/app\n\ngo 1.22\n",
		"main.go":   "package main\nfunc main() {}\n",
		"README.md": "This claims Redis and an HTTP /admin route.\n",
	} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	out, stderr, err := invoke("map", "--source", "directory", "--json", root)
	if err != nil || stderr != "" {
		t.Fatalf("map: %v %s", err, stderr)
	}
	var d mapdoc.Document
	if err := json.Unmarshal([]byte(out), &d); err != nil {
		t.Fatal(err)
	}
	if err := mapdoc.Validate(d); err != nil {
		t.Fatal(err)
	}
	if len(d.Nodes) == 0 || d.Source.Mode != "directory" || strings.Contains(out, root) {
		t.Fatalf("map missing nodes, wrong source or leaks root: %s", out)
	}
	for _, n := range d.Nodes {
		if n.Kind == mapdoc.NodeCapability && strings.Contains(strings.ToLower(n.Name), "redis") || n.Kind == mapdoc.NodeInterface && strings.Contains(n.Name, "/admin") {
			t.Fatalf("documentation became non-documentation evidence: %+v", n)
		}
	}
	other, _, err := invoke("map", "--source", "directory", "--json", root)
	if err != nil || other != out {
		t.Fatal("map was not deterministic")
	}
	limited, _, err := invoke("map", "--source", "directory", "--json", "--budget-files", "1", root)
	if err != nil {
		t.Fatalf("budget should return explicit partial map with exit zero: %v", err)
	}
	var partial mapdoc.Document
	if err := json.Unmarshal([]byte(limited), &partial); err != nil || partial.Status != mapdoc.CoveragePartial {
		t.Fatalf("budget result = %s; err=%v", limited, err)
	}
	var reason bool
	for _, q := range partial.Coverage {
		if q.Question == "content" && q.Status != mapdoc.CoverageComplete && strings.Contains(strings.Join(q.Reasons, ","), "tree_size_limit") {
			reason = true
		}
	}
	if !reason {
		t.Fatalf("budget crossing lacks explicit content reason: %s", limited)
	}
}

func TestMapSummaryAndFormatSelection(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "notes.txt"), []byte("plain notes\n"), 0600); err != nil {
		t.Fatal(err)
	}
	summary, stderr, err := invoke("map", "--summary", root)
	if err != nil || stderr != "" || !strings.Contains(summary, "Directory map") || len(strings.Split(strings.TrimSpace(summary), "\n")) > 40 {
		t.Fatalf("summary: %q %q %v", summary, stderr, err)
	}
	if _, _, err := invoke("map", "--summary", "--json", root); err == nil {
		t.Fatal("ambiguous output selectors accepted")
	}
	if _, _, err := invoke("map", "--budget-files", "2", "--tree-size", "2", root); err == nil {
		t.Fatal("conflicting inventory limits accepted")
	}
}

func TestMapSummaryShowsDominantLanguagesAndDisclosesTruncation(t *testing.T) {
	document := mapdoc.New()
	document.Status = mapdoc.CoverageComplete
	for _, language := range []struct {
		name       string
		percentage string
	}{
		{"AspectJ", "0.0608"}, {"CSS", "0.0020"}, {"FreeMarker", "0.0572"},
		{"Go Template", "0.0009"}, {"Groovy", "0.0132"}, {"HTML", "0.0022"},
		{"Java", "98.9000"}, {"Kotlin", "0.9637"},
	} {
		node := mapdoc.NewNode(mapdoc.NodeContent, []string{"."}, "language:"+language.name)
		node.Name = language.name
		node.Properties = map[string]string{"role": "language_population", "percentage": language.percentage}
		node.Coverage = mapdoc.Coverage{Status: mapdoc.CoverageComplete, Reasons: []string{}}
		node.Evidence = []mapdoc.Evidence{{Basis: mapdoc.BasisRuleInferred, Path: ".", SourceKind: mapdoc.SourceDirectory, Rule: &mapdoc.Producer{ID: "test", Version: "1"}}}
		document.Nodes = append(document.Nodes, node)
	}
	var output bytes.Buffer
	if err := writeMapSummary(&output, document); err != nil {
		t.Fatal(err)
	}
	languageLine := "Languages: Java 98.9%, Kotlin 1.0%, AspectJ 0.1%, FreeMarker 0.1%, Groovy 0.0%, HTML 0.0% (+2 more)"
	if !strings.Contains(output.String(), languageLine) {
		t.Fatalf("dominant languages were hidden or truncation was not disclosed:\n%s", output.String())
	}
}

func TestMapSummaryNamesUnitsAndHidesInternalCoverageCodes(t *testing.T) {
	d := mapdoc.New()
	d.Status = mapdoc.CoveragePartial
	d.Source.Mode = "directory"
	component := mapdoc.NewNode(mapdoc.NodeComponent, []string{"services/api/App.csproj", "services/api"}, "dotnet")
	component.Properties = map[string]string{"root": "services/api", "ecosystem": "dotnet"}
	deployable := mapdoc.NewNode(mapdoc.NodeDeployable, []string{"services/api/Dockerfile"}, "container")
	deployable.Name = "api container"
	deployable.Properties = map[string]string{"kind": "container"}
	capability := mapdoc.NewNode(mapdoc.NodeCapability, []string{"services/api/appsettings.json"}, "redis")
	capability.Name = "cache:redis"
	d.Nodes = []mapdoc.Node{component, deployable, capability}
	d.Edges = []mapdoc.Edge{mapdoc.NewEdge(mapdoc.EdgeBuilds, deployable.ID, component.ID, "Dockerfile context")}
	d.Coverage = []mapdoc.QuestionCoverage{{Question: "packages", Scope: ".", Coverage: mapdoc.Coverage{Status: mapdoc.CoverageUnknown, Reasons: []string{"observer_not_yet_bound_to_map"}}}}
	var output bytes.Buffer
	if err := writeMapSummary(&output, d); err != nil {
		t.Fatal(err)
	}
	text := output.String()
	for _, want := range []string{"api [dotnet]", "api container [container] → builds api", "cache:redis", "Packages: no complete package inventory is established"} {
		if !strings.Contains(text, want) {
			t.Fatalf("summary missing %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "observer_not_yet_bound_to_map") || len(strings.Split(strings.TrimSpace(text), "\n")) > 40 {
		t.Fatalf("summary leaks implementation codes or exceeds one screen:\n%s", text)
	}
}

func TestMapSummaryPrioritizesLinkedDeployablesAndLabelsCountsAsDeclarations(t *testing.T) {
	d := mapdoc.New()
	d.Status = mapdoc.CoveragePartial
	component := mapdoc.NewNode(mapdoc.NodeComponent, []string{"services/api"}, "go")
	component.Name = "api"
	component.Properties = map[string]string{"root": "services/api", "ecosystem": "go"}
	otherComponent := mapdoc.NewNode(mapdoc.NodeComponent, []string{"services/worker"}, "go")
	otherComponent.Name = "worker"
	otherComponent.Properties = map[string]string{"root": "services/worker", "ecosystem": "go"}
	deployables := []mapdoc.Node{
		mapdoc.NewNode(mapdoc.NodeDeployable, []string{"z-unlinked.yml"}, "workflow-z"),
		mapdoc.NewNode(mapdoc.NodeDeployable, []string{"linked/build.yml"}, "build"),
		mapdoc.NewNode(mapdoc.NodeDeployable, []string{"linked/run.yml"}, "run"),
		mapdoc.NewNode(mapdoc.NodeDeployable, []string{"linked/mixed.yml"}, "mixed"),
		mapdoc.NewNode(mapdoc.NodeDeployable, []string{"a-unlinked.yml"}, "workflow-a"),
	}
	for i := range deployables {
		deployables[i].Name = []string{"z-unlinked", "build declaration", "run declaration", "mixed declaration", "a-unlinked"}[i]
		deployables[i].Properties = map[string]string{"kind": "workflow"}
	}
	d.Nodes = append(d.Nodes, component, otherComponent)
	d.Nodes = append(d.Nodes, deployables...)
	d.Edges = []mapdoc.Edge{
		mapdoc.NewEdge(mapdoc.EdgeBuilds, deployables[1].ID, component.ID, "build-api"),
		mapdoc.NewEdge(mapdoc.EdgeRuns, deployables[2].ID, component.ID, "run-api"),
		mapdoc.NewEdge(mapdoc.EdgeRuns, deployables[3].ID, component.ID, "run-api-1"),
		mapdoc.NewEdge(mapdoc.EdgeRuns, deployables[3].ID, otherComponent.ID, "run-api-2"),
		mapdoc.NewEdge(mapdoc.EdgeBuilds, deployables[3].ID, component.ID, "build-api"),
	}
	var output bytes.Buffer
	if err := writeMapSummary(&output, d); err != nil {
		t.Fatal(err)
	}
	text := output.String()
	for _, want := range []string{
		"Relationship declarations: 3 runs, 2 builds",
		"mixed declaration [workflow] → builds api, runs api",
		"run declaration [workflow] → runs api",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("summary missing %q:\n%s", want, text)
		}
	}
	first := strings.Index(text, "mixed declaration [workflow]")
	second := strings.Index(text, "run declaration [workflow]")
	third := strings.Index(text, "build declaration [workflow]")
	unlinked := strings.Index(text, "a-unlinked [workflow]")
	if first < 0 || second <= first || third <= second || unlinked <= third {
		t.Fatalf("linked entries were not ranked before unlinked deployables:\n%s", text)
	}
	if strings.Contains(strings.ToLower(text), "runtime instance") {
		t.Fatalf("summary implies a runtime count rather than declaration count:\n%s", text)
	}
}

func TestMapFileByteLimitQualifiesContentCoverage(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n"), 0600); err != nil {
		t.Fatal(err)
	}
	out, _, err := invoke("map", "--source", "directory", "--json", "--max-file-bytes", "1", root)
	if err != nil {
		t.Fatal(err)
	}
	var d mapdoc.Document
	if err := json.Unmarshal([]byte(out), &d); err != nil {
		t.Fatal(err)
	}
	if d.Status != mapdoc.CoveragePartial {
		t.Fatalf("file byte limit failed to qualify document: %s", out)
	}
	for _, q := range d.Coverage {
		if q.Question == "content" {
			if q.Status != mapdoc.CoveragePartial || !strings.Contains(strings.Join(q.Reasons, ","), "file_too_large") {
				t.Fatalf("content coverage lost file byte limit: %+v", q)
			}
			return
		}
	}
	t.Fatal("missing content coverage")
}
