package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/war-and-code/dircue/pkg/mapdoc"
	git "github.com/war-and-code/dircue/third_party/go-git"
)

func TestMapRetainsUnbornGitFallbackWarning(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\nfunc main() {}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := git.PlainInit(root, false); err != nil {
		t.Fatal(err)
	}
	out, stderr, err := invoke("map", "--json", root)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stderr, "git_no_commits_or_corrupt_gitdir") {
		t.Fatalf("map stderr omitted Git fallback warning: %q", stderr)
	}
	var document mapdoc.Document
	if err := json.Unmarshal([]byte(out), &document); err != nil {
		t.Fatal(err)
	}
	if document.Source.Mode != "directory" {
		t.Fatalf("map source = %q, want directory", document.Source.Mode)
	}
}

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

// TestMapSummaryPrioritizesLinkedDeployablesAndLabelsCountsAsDeclarations
// verifies two related display rules:
//
//  1. Among runnable deployables (container builds, workloads, …) the ones with
//     the most builds+runs edges come first. Unlinked ones are last.
//  2. CI workflows (kind "workflow") are collapsed into a "+N CI workflows"
//     secondary count when other runnable deployables are present, so that a
//     large CI system doesn't bury the container builds and workloads.
//  3. Declarations are counted (not runtime instances).
//
// It also verifies that when only CI workflows exist (no other runnable
// deployables), those workflows are shown by name with the link-count order.
func TestMapSummaryPrioritizesLinkedDeployablesAndLabelsCountsAsDeclarations(t *testing.T) {
	d := mapdoc.New()
	d.Status = mapdoc.CoveragePartial
	component := mapdoc.NewNode(mapdoc.NodeComponent, []string{"services/api"}, "go")
	component.Name = "api"
	component.Properties = map[string]string{"root": "services/api", "ecosystem": "go"}
	otherComponent := mapdoc.NewNode(mapdoc.NodeComponent, []string{"services/worker"}, "go")
	otherComponent.Name = "worker"
	otherComponent.Properties = map[string]string{"root": "services/worker", "ecosystem": "go"}

	// Three container builds with different link counts: multi-linked, run-only,
	// and unlinked. They should appear in that order, before the CI workflow count.
	builds := []mapdoc.Node{
		mapdoc.NewNode(mapdoc.NodeDeployable, []string{"images/multi/Dockerfile"}, "container_build-multi"),
		mapdoc.NewNode(mapdoc.NodeDeployable, []string{"images/run/Dockerfile"}, "container_build-run"),
		mapdoc.NewNode(mapdoc.NodeDeployable, []string{"images/unlinked/Dockerfile"}, "container_build-unlinked"),
	}
	for i := range builds {
		builds[i].Name = []string{"multi build", "run build", "unlinked build"}[i]
		builds[i].Properties = map[string]string{"kind": "container_build", "provider": "dockerfile"}
	}

	// Five CI workflows — two of them linked to components, three unlinked.
	// They must all be collapsed into "+5 CI workflows" because there are runnable
	// deployables (the container builds) above.
	workflows := []mapdoc.Node{
		mapdoc.NewNode(mapdoc.NodeDeployable, []string{"z-unlinked.yml"}, "workflow-z"),
		mapdoc.NewNode(mapdoc.NodeDeployable, []string{"linked/build.yml"}, "wf-build"),
		mapdoc.NewNode(mapdoc.NodeDeployable, []string{"linked/run.yml"}, "wf-run"),
		mapdoc.NewNode(mapdoc.NodeDeployable, []string{"linked/mixed.yml"}, "wf-mixed"),
		mapdoc.NewNode(mapdoc.NodeDeployable, []string{"a-unlinked.yml"}, "workflow-a"),
	}
	for i := range workflows {
		workflows[i].Name = []string{"z-unlinked", "build declaration", "run declaration", "mixed declaration", "a-unlinked"}[i]
		workflows[i].Properties = map[string]string{"kind": "workflow"}
	}
	d.Nodes = append(d.Nodes, component, otherComponent)
	d.Nodes = append(d.Nodes, builds...)
	d.Nodes = append(d.Nodes, workflows...)

	// multi build: 2 runs + 1 build = 3 total.
	// run build:   1 run.
	// unlinked build: 0.
	// Workflow links exist but don't affect the display because workflows are counted.
	d.Edges = []mapdoc.Edge{
		mapdoc.NewEdge(mapdoc.EdgeRuns, builds[0].ID, component.ID, "multi-run-api"),
		mapdoc.NewEdge(mapdoc.EdgeRuns, builds[0].ID, otherComponent.ID, "multi-run-worker"),
		mapdoc.NewEdge(mapdoc.EdgeBuilds, builds[0].ID, component.ID, "multi-build-api"),
		mapdoc.NewEdge(mapdoc.EdgeRuns, builds[1].ID, component.ID, "run-api"),
		// Workflow edges exist but workflows are counted, not listed.
		mapdoc.NewEdge(mapdoc.EdgeBuilds, workflows[1].ID, component.ID, "wf-build-api"),
		mapdoc.NewEdge(mapdoc.EdgeRuns, workflows[2].ID, component.ID, "wf-run-api"),
		mapdoc.NewEdge(mapdoc.EdgeRuns, workflows[3].ID, component.ID, "wf-mixed-run-api"),
		mapdoc.NewEdge(mapdoc.EdgeBuilds, workflows[3].ID, component.ID, "wf-mixed-build-api"),
	}
	var output bytes.Buffer
	if err := writeMapSummary(&output, d); err != nil {
		t.Fatal(err)
	}
	text := output.String()

	// --- CI workflows are collapsed ---
	if !strings.Contains(text, "+5 CI workflows") {
		t.Fatalf("CI workflows were not counted as secondary; want '+5 CI workflows':\n%s", text)
	}
	// Workflow names must not appear as individual entries.
	for _, wfName := range []string{"mixed declaration [workflow]", "run declaration [workflow]", "build declaration [workflow]"} {
		if strings.Contains(text, wfName) {
			t.Fatalf("workflow appeared by name when other runnable deployables exist: %q:\n%s", wfName, text)
		}
	}

	// --- Runnable deployable ordering: most-linked first ---
	for _, want := range []string{
		"multi build [container_build] → builds api, runs api",
		"run build [container_build] → runs api",
		"unlinked build [container_build]",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("summary missing %q:\n%s", want, text)
		}
	}
	posMulti := strings.Index(text, "multi build [container_build]")
	posRun := strings.Index(text, "run build [container_build]")
	posUnlinked := strings.Index(text, "unlinked build [container_build]")
	if posMulti < 0 || posRun <= posMulti || posUnlinked <= posRun {
		t.Fatalf("runnable deployables not ranked by link count:\n%s", text)
	}

	// The "+N CI workflows" count must appear in the "Deployables:" header line,
	// and the named container-build entries follow on the next lines. Verify the
	// "Deployables:" line carries the count but NOT the individual workflow names.
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(line, "Deployables:") {
			if !strings.Contains(line, "+5 CI workflows") {
				t.Fatalf("Deployables header line missing '+5 CI workflows':\n%s", line)
			}
			break
		}
	}

	// No runtime-instance language.
	if strings.Contains(strings.ToLower(text), "runtime instance") {
		t.Fatalf("summary implies a runtime count rather than declaration count:\n%s", text)
	}

	// --- When only CI workflows are present, show them by name ---
	dWorkflowOnly := mapdoc.New()
	dWorkflowOnly.Status = mapdoc.CoveragePartial
	wfOnly := []mapdoc.Node{
		mapdoc.NewNode(mapdoc.NodeDeployable, []string{"z-only.yml"}, "wf-only-z"),
		mapdoc.NewNode(mapdoc.NodeDeployable, []string{"a-only.yml"}, "wf-only-a"),
	}
	for i := range wfOnly {
		wfOnly[i].Name = []string{"z-workflow", "a-workflow"}[i]
		wfOnly[i].Properties = map[string]string{"kind": "workflow"}
	}
	dWorkflowOnly.Nodes = append(dWorkflowOnly.Nodes, wfOnly...)
	var wfOnlyOut bytes.Buffer
	if err := writeMapSummary(&wfOnlyOut, dWorkflowOnly); err != nil {
		t.Fatal(err)
	}
	wfOnlyText := wfOnlyOut.String()
	if strings.Contains(wfOnlyText, "CI workflows") {
		t.Fatalf("CI-workflow-only repo should show workflows by name, not as a count:\n%s", wfOnlyText)
	}
	if !strings.Contains(wfOnlyText, "z-workflow [workflow]") || !strings.Contains(wfOnlyText, "a-workflow [workflow]") {
		t.Fatalf("CI-workflow-only repo did not list workflow names:\n%s", wfOnlyText)
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

// TestMapSummaryDisambiguatesDuplicateComponentNames verifies that when
// multiple components share a display name and ecosystem, the summary text
// appends the root-directory basename in parentheses to distinguish them (#fix-4).
// This test fails on 427c2f8 (dedup drops duplicates silently) and passes after.
func TestMapSummaryDisambiguatesDuplicateComponentNames(t *testing.T) {
	d := mapdoc.New()
	d.Status = mapdoc.CoverageComplete

	// Simulate a Gradle multi-project with five "api" sub-modules.
	apiRoots := []string{
		"modules/core/api",
		"modules/auth/api",
		"modules/payment/api",
		"modules/user/api",
		"modules/notification/api",
	}
	for i, root := range apiRoots {
		n := mapdoc.NewNode(mapdoc.NodeComponent, []string{root + "/build.gradle"}, "comp-api-"+root)
		n.Name = "api"
		n.Properties = map[string]string{
			"ecosystem": "gradle",
			"role":      "primary",
			"root":      root,
		}
		_ = i
		d.Nodes = append(d.Nodes, n)
	}

	// Add one component with a unique name to confirm it is displayed as-is.
	unique := mapdoc.NewNode(mapdoc.NodeComponent, []string{"app/build.gradle"}, "comp-app")
	unique.Name = "app"
	unique.Properties = map[string]string{"ecosystem": "gradle", "role": "primary", "root": "app"}
	d.Nodes = append(d.Nodes, unique)

	var output bytes.Buffer
	if err := writeMapSummary(&output, d); err != nil {
		t.Fatal(err)
	}
	summary := output.String()

	// Each of the first four api modules must appear disambiguated.
	if !strings.Contains(summary, "api (") {
		t.Errorf("duplicate api modules should be disambiguated; summary:\n%s", summary)
	}
	// The unique "app" component must not be parenthesized.
	if strings.Contains(summary, "app (") {
		t.Errorf("unique component 'app' should not be disambiguated; summary:\n%s", summary)
	}
}
