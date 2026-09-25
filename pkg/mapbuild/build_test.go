package mapbuild

import (
	"strings"
	"testing"

	"github.com/war-and-code/dircue/pkg/deployables"
	"github.com/war-and-code/dircue/pkg/discovery"
	"github.com/war-and-code/dircue/pkg/intentmap"
	"github.com/war-and-code/dircue/pkg/mapdoc"
	"github.com/war-and-code/dircue/pkg/profile"
)

func TestFilenameHintDoesNotClaimValidatedBinary(t *testing.T) {
	n := fileNode("opaque.lib", "binary", "static_library", "extension", 20)
	if n.Coverage.Status != mapdoc.CoveragePartial || n.Evidence[0].Basis != mapdoc.BasisFilenameHint {
		t.Fatalf("unverified suffix claimed a complete binary: %+v", n)
	}
}

func TestDeclaredBuildAndRunLinksHaveEvidence(t *testing.T) {
	doc := mapdoc.New()
	component := mapdoc.NewNode(mapdoc.NodeComponent, []string{"services/api", "services/api/go.mod"}, "go")
	component.Properties = map[string]string{"root": "services/api"}
	doc.Nodes = append(doc.Nodes, component)
	evidence := deployables.Evidence{Field: "declaration", Line: 1, Basis: "static-field"}
	definitions := []deployables.Definition{
		{Provider: "compose", Kind: "service", Name: "api", Path: "compose.yml", Coverage: "complete", Evidence: []deployables.Evidence{evidence}, References: []deployables.Reference{
			{Kind: "build_context", Value: "services/api", Qualification: "local", Evidence: evidence},
			{Kind: "image", Value: "example/api:v1", Qualification: "external", Evidence: evidence},
		}},
		// Name "api" reflects dockerfileDisplayName("services/api/Dockerfile") = path.Base("services/api") = "api".
		{Provider: "dockerfile", Kind: "container_build", Name: "api", Path: "services/api/Dockerfile", Coverage: "complete", Evidence: []deployables.Evidence{evidence}},
		{Provider: "kubernetes", Kind: "workload", Name: "api", Path: "k8s/api.yaml", Coverage: "qualified", Evidence: []deployables.Evidence{evidence}, References: []deployables.Reference{{Kind: "image", Value: "example/api:v1", Qualification: "external", Evidence: evidence}}},
		{Provider: "cloudformation", Kind: "infrastructure", Name: "function", Path: "template.yaml", Coverage: "qualified", Evidence: []deployables.Evidence{evidence}, References: []deployables.Reference{{Kind: "code_uri", Value: "services/api", Qualification: "local", Evidence: evidence}}},
	}
	addDeployables(&doc, &deployables.Report{Status: "complete", Definitions: definitions})
	builds, runs := 0, 0
	for _, edge := range doc.Edges {
		if edge.To != component.ID {
			t.Fatalf("edge points to wrong component: %+v", edge)
		}
		if edge.Coverage.Status != mapdoc.CoveragePartial || len(edge.Evidence) == 0 {
			t.Fatalf("declared link lacks qualification or evidence: %+v", edge)
		}
		switch edge.Type {
		case mapdoc.EdgeBuilds:
			builds++
		case mapdoc.EdgeRuns:
			runs++
		}
	}
	if builds != 3 || runs != 2 {
		t.Fatalf("want Dockerfile, Compose and SAM builds plus Compose and Kubernetes runs; got builds=%d runs=%d", builds, runs)
	}
	for _, question := range doc.Coverage {
		if question.Question == "deployables" && question.Status != mapdoc.CoveragePartial {
			t.Fatalf("bounded parser claimed complete deployable catalog: %+v", question)
		}
	}
}

func TestKubernetesImageDoesNotInferRunFromDockerfileAndBasename(t *testing.T) {
	doc := mapdoc.New()
	component := mapdoc.NewNode(mapdoc.NodeComponent, []string{"src/emailservice", "src/emailservice/go.mod"}, "go")
	component.Properties = map[string]string{"root": "src/emailservice"}
	doc.Nodes = append(doc.Nodes, component)
	workloadEvidence := deployables.Evidence{Field: "image", Value: "registry.example/demo/emailservice:v1", Line: 22, Basis: "kubernetes-container-field"}
	dockerEvidence := deployables.Evidence{Field: "FROM", Value: "python:3.13", Line: 1, Basis: "dockerfile-instruction"}
	definitions := []deployables.Definition{
		{Provider: "kubernetes", Kind: "workload", Name: "emailservice", Path: "k8s/emailservice.yaml", Coverage: "qualified", Evidence: []deployables.Evidence{{Field: "kind", Value: "Deployment", Line: 2, Basis: "kubernetes-field"}}, References: []deployables.Reference{{Kind: "image", Value: workloadEvidence.Value, Qualification: "external", Evidence: workloadEvidence}}},
		// Name "emailservice" reflects dockerfileDisplayName("src/emailservice/Dockerfile").
		{Provider: "dockerfile", Kind: "container_build", Name: "emailservice", Path: "src/emailservice/Dockerfile", Coverage: "complete", References: []deployables.Reference{{Kind: "base_image_or_stage", Value: dockerEvidence.Value, Qualification: "external", Evidence: dockerEvidence}}},
	}
	addDeployables(&doc, &deployables.Report{Status: "complete", Definitions: definitions})
	for _, edge := range doc.Edges {
		if edge.Type == mapdoc.EdgeRuns {
			t.Fatalf("Dockerfile co-location and matching basename are insufficient to infer a run link: %+v", edge)
		}
	}
}

func TestKubernetesImageDoesNotChooseAmongDuplicateComponentRoots(t *testing.T) {
	doc := mapdoc.New()
	for _, root := range []string{"one/api", "two/api"} {
		component := mapdoc.NewNode(mapdoc.NodeComponent, []string{root}, "go")
		component.Properties = map[string]string{"root": root}
		doc.Nodes = append(doc.Nodes, component)
	}
	evidence := deployables.Evidence{Field: "image", Value: "registry.example/api:v1", Line: 1, Basis: "kubernetes-container-field"}
	definitions := []deployables.Definition{
		{Provider: "kubernetes", Kind: "workload", Name: "api", Path: "k8s/api.yaml", Coverage: "qualified", Evidence: []deployables.Evidence{{Field: "kind", Value: "Deployment", Line: 1, Basis: "kubernetes-field"}}, References: []deployables.Reference{{Kind: "image", Value: evidence.Value, Qualification: "external", Evidence: evidence}}},
		// Names reflect dockerfileDisplayName: path.Base(path.Dir(…)) = "api" for both.
		// The paths differ so the node IDs are distinct.
		{Provider: "dockerfile", Kind: "container_build", Name: "api", Path: "one/api/Dockerfile", Coverage: "complete", References: []deployables.Reference{{Kind: "base_image_or_stage", Value: "alpine", Qualification: "external", Evidence: deployables.Evidence{Field: "FROM", Line: 1, Basis: "dockerfile-instruction"}}}},
		{Provider: "dockerfile", Kind: "container_build", Name: "api", Path: "two/api/Dockerfile", Coverage: "complete", References: []deployables.Reference{{Kind: "base_image_or_stage", Value: "alpine", Qualification: "external", Evidence: deployables.Evidence{Field: "FROM", Line: 1, Basis: "dockerfile-instruction"}}}},
	}
	addDeployables(&doc, &deployables.Report{Status: "complete", Definitions: definitions})
	for _, edge := range doc.Edges {
		if edge.Type == mapdoc.EdgeRuns {
			t.Fatalf("ambiguous component match emitted a run link: %+v", edge)
		}
	}
}

func TestKubernetesImageDoesNotClaimAncestorComponentFromNestedDockerfile(t *testing.T) {
	doc := mapdoc.New()
	outer := mapdoc.NewNode(mapdoc.NodeComponent, []string{"src/cartservice"}, "dotnet")
	outer.Properties = map[string]string{"root": "src/cartservice"}
	doc.Nodes = append(doc.Nodes, outer)
	evidence := deployables.Evidence{Field: "image", Value: "cartservice:v1", Line: 1, Basis: "kubernetes-container-field"}
	definitions := []deployables.Definition{
		{Provider: "kubernetes", Kind: "workload", Name: "cartservice", Path: "k8s/cartservice.yaml", Coverage: "qualified", Evidence: []deployables.Evidence{evidence}, References: []deployables.Reference{{Kind: "image", Value: evidence.Value, Qualification: "external", Evidence: evidence}}},
		// Name "src" reflects dockerfileDisplayName("src/cartservice/src/Dockerfile") = path.Base("src/cartservice/src") = "src".
		{Provider: "dockerfile", Kind: "container_build", Name: "src", Path: "src/cartservice/src/Dockerfile", Coverage: "complete", Evidence: []deployables.Evidence{{Field: "FROM", Line: 1, Basis: "dockerfile-instruction"}}},
	}
	addDeployables(&doc, &deployables.Report{Status: "complete", Definitions: definitions})
	for _, edge := range doc.Edges {
		if edge.Type == mapdoc.EdgeRuns {
			t.Fatalf("nested Dockerfile alone linked an ancestor component: %+v", edge)
		}
	}
}

func TestSkaffoldArtifactSelectsNestedComponentByFullNormalizedImage(t *testing.T) {
	doc := mapdoc.New()
	outer := mapdoc.NewNode(mapdoc.NodeComponent, []string{"src/cartservice"}, "dotnet")
	outer.Properties = map[string]string{"root": "src/cartservice"}
	nested := mapdoc.NewNode(mapdoc.NodeComponent, []string{"src/cartservice/src"}, "dotnet")
	nested.Properties = map[string]string{"root": "src/cartservice/src"}
	doc.Nodes = append(doc.Nodes, outer, nested)
	artifactImage := deployables.Evidence{Field: "image", Value: "registry.example/team/cartservice:build", Line: 10, Basis: "skaffold-artifact"}
	artifactContext := deployables.Evidence{Field: "context", Value: "src/cartservice/src", Line: 11, Basis: "skaffold-artifact"}
	kubeImage := deployables.Evidence{Field: "image", Value: "registry.example/team/cartservice:v1@sha256:1234", Line: 22, Basis: "kubernetes-container-field"}
	definitions := []deployables.Definition{
		{Provider: "skaffold", Kind: "container_build", Name: "cartservice", Path: "skaffold.yaml", Coverage: "complete", Evidence: []deployables.Evidence{artifactImage, artifactContext}, References: []deployables.Reference{{Kind: "image", Value: artifactImage.Value, Qualification: "local", Evidence: artifactImage}, {Kind: "build_context", Value: artifactContext.Value, Qualification: "local", Evidence: artifactContext}}},
		{Provider: "kubernetes", Kind: "workload", Name: "cartservice", Path: "k8s/cartservice.yaml", Coverage: "qualified", Evidence: []deployables.Evidence{{Field: "kind", Value: "Deployment", Line: 2, Basis: "kubernetes-field"}}, References: []deployables.Reference{{Kind: "image", Value: kubeImage.Value, Qualification: "external", Evidence: kubeImage}}},
	}
	addDeployables(&doc, &deployables.Report{Status: "complete", Definitions: definitions})
	for _, edge := range doc.Edges {
		if edge.Type != mapdoc.EdgeRuns {
			continue
		}
		if edge.To != nested.ID || edge.To == outer.ID || len(edge.Evidence) != 3 || edge.Coverage.Status != mapdoc.CoveragePartial {
			t.Fatalf("Skaffold mapping did not select and qualify the nested component: %+v", edge)
		}
		if edge.Evidence[0].Path != "k8s/cartservice.yaml" || edge.Evidence[1].Path != "skaffold.yaml" || edge.Evidence[2].Path != "skaffold.yaml" {
			t.Fatalf("run link lacks Kubernetes/Skaffold evidence: %+v", edge.Evidence)
		}
		return
	}
	t.Fatal("expected a Kubernetes run edge through the exact Skaffold image repository and context")
}

func TestKubernetesImageDoesNotMatchSkaffoldByBasenameAcrossRegistries(t *testing.T) {
	doc := mapdoc.New()
	component := mapdoc.NewNode(mapdoc.NodeComponent, []string{"src/api", "src/api/go.mod"}, "go")
	component.Properties = map[string]string{"root": "src/api"}
	doc.Nodes = append(doc.Nodes, component)
	artifact := deployables.Evidence{Field: "image", Value: "registry.example/team-a/api:v1", Line: 4, Basis: "skaffold-artifact"}
	context := deployables.Evidence{Field: "context", Value: "src/api", Line: 5, Basis: "skaffold-artifact"}
	workload := deployables.Evidence{Field: "image", Value: "other.example/team-b/api:v1", Line: 10, Basis: "kubernetes-container-field"}
	definitions := []deployables.Definition{
		{Provider: "skaffold", Kind: "container_build", Name: "api", Path: "skaffold.yaml", Coverage: "complete", References: []deployables.Reference{
			{Kind: "image", Value: artifact.Value, Qualification: "declared", Evidence: artifact},
			{Kind: "build_context", Value: context.Value, Qualification: "local", Evidence: context},
		}},
		{Provider: "kubernetes", Kind: "workload", Name: "api", Path: "k8s/api.yaml", Coverage: "qualified", Evidence: []deployables.Evidence{{Field: "kind", Value: "Deployment", Line: 1, Basis: "kubernetes-field"}}, References: []deployables.Reference{{Kind: "image", Value: workload.Value, Qualification: "external", Evidence: workload}}},
	}
	addDeployables(&doc, &deployables.Report{Status: "complete", Definitions: definitions})
	for _, edge := range doc.Edges {
		if edge.Type == mapdoc.EdgeRuns {
			t.Fatalf("same basename from unrelated registries produced a run link: %+v", edge)
		}
	}
}

func TestComposeAndKubernetesImagesMatchNormalizedFullRepository(t *testing.T) {
	doc := mapdoc.New()
	component := mapdoc.NewNode(mapdoc.NodeComponent, []string{"src/api", "src/api/go.mod"}, "go")
	component.Properties = map[string]string{"root": "src/api"}
	doc.Nodes = append(doc.Nodes, component)
	evidence := deployables.Evidence{Field: "image", Value: "registry.example/team/api:build", Line: 4, Basis: "compose-field"}
	context := deployables.Evidence{Field: "build", Value: "src/api", Line: 5, Basis: "compose-build-field"}
	kubeEvidence := deployables.Evidence{Field: "image", Value: "registry.example/team/api:release@sha256:1234", Line: 10, Basis: "kubernetes-container-field"}
	definitions := []deployables.Definition{
		{Provider: "compose", Kind: "service", Name: "api", Path: "compose.yml", Coverage: "complete", Evidence: []deployables.Evidence{evidence}, References: []deployables.Reference{
			{Kind: "build_context", Value: context.Value, Qualification: "local", Evidence: context},
			{Kind: "image", Value: evidence.Value, Qualification: "declared", Evidence: evidence},
		}},
		{Provider: "kubernetes", Kind: "workload", Name: "api", Path: "k8s/api.yaml", Coverage: "qualified", Evidence: []deployables.Evidence{{Field: "kind", Value: "Deployment", Line: 1, Basis: "kubernetes-field"}}, References: []deployables.Reference{{Kind: "image", Value: kubeEvidence.Value, Qualification: "external", Evidence: kubeEvidence}}},
	}
	addDeployables(&doc, &deployables.Report{Status: "complete", Definitions: definitions})
	runs := 0
	for _, edge := range doc.Edges {
		if edge.Type == mapdoc.EdgeRuns {
			runs++
			if edge.To != component.ID || edge.Coverage.Status != mapdoc.CoveragePartial || len(edge.Evidence) == 0 {
				t.Fatalf("normalized exact image link lost its target or evidence: %+v", edge)
			}
		}
	}
	if runs != 2 {
		t.Fatalf("want Compose declaration plus matching Kubernetes declaration, got %d run links", runs)
	}
}

func TestDuplicateObserverIdentityDoesNotDiscardWholeMap(t *testing.T) {
	evidence := deployables.Evidence{Field: "kind", Value: "Deployment", Line: 1, Basis: "static-field"}
	definition := deployables.Definition{Provider: "kubernetes", Kind: "workload", Name: "my-deploy", Path: "deploy/deployment.yaml", Coverage: "qualified", Evidence: []deployables.Evidence{evidence}}
	report := &profile.Report{Discovery: &discovery.Report{Status: "complete", Source: discovery.Source{Mode: "directory"}}}
	doc, err := Build(report, Options{Deployables: &deployables.Report{Status: "complete", Definitions: []deployables.Definition{definition, definition}}})
	if err != nil {
		t.Fatalf("duplicate observer identity discarded the map: %v", err)
	}
	count := 0
	for _, n := range doc.Nodes {
		if n.Kind == mapdoc.NodeDeployable {
			count++
			if n.Coverage.Status != mapdoc.CoveragePartial {
				t.Fatalf("duplicate was not qualified: %+v", n)
			}
		}
	}
	if count != 1 {
		t.Fatalf("retained deployable count=%d", count)
	}
	for _, q := range doc.Coverage {
		if q.Question == "deployables" && q.Status == mapdoc.CoveragePartial && len(q.Reasons) == 2 {
			return
		}
	}
	t.Fatal("duplicate omission was not disclosed in deployable coverage")
}

func TestContentPopulationsHaveUserMeaningfulStableIdentity(t *testing.T) {
	makeReport := func(first, second string) *profile.Report {
		return &profile.Report{Discovery: &discovery.Report{
			Categories: []discovery.Group{
				{Name: "source_candidate", Basis: first, Counts: discovery.Counts{Files: 2, Bytes: 20}},
				{Name: "source_candidate", Basis: second, Counts: discovery.Counts{Files: 3, Bytes: 30}},
				{Name: "documentation_candidate", Basis: first, Counts: discovery.Counts{Files: 1, Bytes: 10}},
			},
			Roles: []discovery.Group{
				{Name: "documentation", Basis: second, Counts: discovery.Counts{Files: 1, Bytes: 10}},
			},
		}}
	}
	first := contentNodes(makeReport("enry_extension", "enry_filename"))
	second := contentNodes(makeReport("enry_path", "gitattributes"))
	if len(first) != 2 || len(second) != 2 {
		t.Fatalf("expected one source and one documentation population: %d, %d", len(first), len(second))
	}
	byRole := func(nodes []mapdoc.Node) map[string]mapdoc.Node {
		out := map[string]mapdoc.Node{}
		for _, node := range nodes {
			out[node.Properties["role"]] = node
		}
		return out
	}
	a, b := byRole(first), byRole(second)
	if a["source"].ID != b["source"].ID || a["documentation"].ID != b["documentation"].ID {
		t.Fatalf("Enry mechanism changed population identities: %+v vs %+v", a, b)
	}
	if a["source"].Properties["files"] != "5" || a["source"].Properties["bytes"] != "50" {
		t.Fatalf("source groups were not combined: %+v", a["source"].Properties)
	}
	if a["documentation"].Properties["files"] != "1" {
		t.Fatalf("documentation category and role were double-counted: %+v", a["documentation"].Properties)
	}
}

func TestSingleMapDoesNotClaimMaterialChangeQuestion(t *testing.T) {
	report := &profile.Report{Discovery: &discovery.Report{Status: "complete", Source: discovery.Source{Mode: "directory"}}}
	doc, err := Build(report, Options{})
	if err != nil {
		t.Fatal(err)
	}
	for _, question := range doc.Coverage {
		if question.Question == "material_change" {
			t.Fatal("a single directory map cannot answer a comparison question")
		}
	}
}

func TestAuxiliaryPathRolesAreHintsNotDeletedFacts(t *testing.T) {
	for _, test := range []struct{ filename, role string }{
		// No role — caller assigns primary
		{"services/api/App.csproj", ""},
		// Fixture
		{"tests/map_corpus/fixtures/app/package.json", "fixture"},
		{"crates/ruff_linter/resources/test/fixtures/isort/pyproject.toml", "fixture"},
		{"testdata/myapp/pyproject.toml", "fixture"},
		// Fixture — evaluation truth data (new path segments)
		{"crates/ty_completion_eval/truth/scope-ok/pyproject.toml", "fixture"},
		{"tests/cases/mycase/pyproject.toml", "fixture"},
		{"tests/snapshots/snap_foo/pyproject.toml", "fixture"},
		{"tests/corpus/sample/pyproject.toml", "fixture"},
		{"tests/golden/out.json", "fixture"},
		// Test
		{"src/Tests/Compiler.Tests.csproj", "test"},
		{"crates/ruff_mdtest/Cargo.toml", ""}, // "mdtest" suffix is not "_test"; no role assigned
		{"crates/ty_test/Cargo.toml", "test"},
		{"tests/App.UnitTests/App.UnitTests.csproj", "test"},
		{"tests/App.FunctionalTests/App.FunctionalTests.csproj", "test"},
		// Example
		{"examples/demo/Dockerfile", "example"},
		{"samples/web/package.json", "example"},
		{"demo/app/go.mod", "example"},
		// Vendored
		{"vendor/lib/Cargo.toml", "vendored"},
		{"node_modules/react/package.json", "vendored"},
		{"operator/.bingo/go.mod", "vendored"},
		// Docs
		{"docs/website/package.json", "docs"},
		{"RELEASING/pyproject.toml", "docs"},
		{"translations/setup.cfg", "docs"},
		// Tooling (new: benchmarks segment)
		{"scripts/benchmark/pyproject.toml", "tooling"},
		{"scripts/benchmarks/Cargo.toml", "tooling"},
		{"scripts/bench/pyproject.toml", "tooling"},
		// JVM package paths below a source set are not project layout.
		{"src/main/java/org/springframework/samples/petclinic/PetClinicApplication.java", ""},
		{"src/test/java/org/example/app/AppTest.java", "test"},
		{"examples/web/src/main/kotlin/demo/App.kt", "example"},
		// Only .NET project files use the name-suffix test rule.
		{"cmd/latest/latest.go", ""},
	} {
		if got := mapPathRole(test.filename); got != test.role {
			t.Errorf("%s: role %q, want %q", test.filename, got, test.role)
		}
	}
}

func TestPlaceholderNameRole(t *testing.T) {
	for _, test := range []struct{ name, want string }{
		{"test", "test"},
		{"Test", "test"},
		{"TEST", "test"},
		{"tests", "test"},
		{"example", "example"},
		{"examples", "example"},
		{"sample", "example"},
		{"samples", "example"},
		{"demo", "fixture"},
		{"fixture", "fixture"},
		{"fixtures", "fixture"},
		{"dummy", "fixture"},
		{"foo", "fixture"},
		{"my-project", "fixture"},
		{"my_project", "fixture"},
		// real names — not placeholders
		{"ruff", ""},
		{"myapp", ""},
		{"api-gateway", ""},
		{"TestFramework", ""},
	} {
		if got := placeholderNameRole(test.name); got != test.want {
			t.Errorf("placeholderNameRole(%q) = %q, want %q", test.name, got, test.want)
		}
	}
}

// ── P0: Attribution honesty (ACC-F01 regression) ─────────────────────────────

func TestContainmentAttributionProducesPartialEdge(t *testing.T) {
	// Regression for P0/ACC-F01: an edge from a component to a capability whose
	// projectID was assigned by directory-containment must be partial with reason
	// "attributed_by_directory_containment", not complete.
	doc := mapdoc.New()
	component := mapdoc.NewNode(mapdoc.NodeComponent, []string{"app", "app/go.mod"}, "go")
	component.Properties = map[string]string{"root": "app"}
	doc.Nodes = append(doc.Nodes, component)

	obs := intentmap.Observation{
		Kind:               intentmap.KindCapability,
		Name:               "cache:redis",
		ProjectID:          "app/go.mod",
		ProjectAttribution: "directory_containment",
		State:              "observed",
		Basis:              "declared_config",
		Path:               "app/.env.production.sample",
	}
	report := &intentmap.Report{
		Coverage:     intentmap.Coverage{Status: "complete"},
		Observations: []intentmap.Observation{obs},
	}
	addIntent(&doc, report)

	for _, edge := range doc.Edges {
		if edge.Type != mapdoc.EdgeUsesCapability {
			continue
		}
		if edge.Coverage.Status != mapdoc.CoveragePartial {
			t.Errorf("containment-attributed edge coverage = %q, want partial", edge.Coverage.Status)
		}
		found := false
		for _, reason := range edge.Coverage.Reasons {
			if reason == "attributed_by_directory_containment" {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("edge coverage reasons = %v, want attributed_by_directory_containment", edge.Coverage.Reasons)
		}
		return
	}
	t.Fatal("no uses_capability edge was created")
}

func TestExplicitlyDeclaredCapabilityEdgeIsNotDowngraded(t *testing.T) {
	// A capability declared explicitly in a manifest (ProjectAttribution empty)
	// must produce a complete edge — not partial.
	doc := mapdoc.New()
	component := mapdoc.NewNode(mapdoc.NodeComponent, []string{"app", "app/go.mod"}, "go")
	component.Properties = map[string]string{"root": "app"}
	doc.Nodes = append(doc.Nodes, component)

	obs := intentmap.Observation{
		Kind:      intentmap.KindCapability,
		Name:      "cache:redis",
		ProjectID: "app/go.mod",
		// ProjectAttribution is intentionally empty: declared by manifest
		State: "declared",
		Basis: "declared_dependency",
		Path:  "app/go.mod",
	}
	report := &intentmap.Report{
		Coverage:     intentmap.Coverage{Status: "complete"},
		Observations: []intentmap.Observation{obs},
	}
	addIntent(&doc, report)

	for _, edge := range doc.Edges {
		if edge.Type != mapdoc.EdgeUsesCapability {
			continue
		}
		if edge.Coverage.Status != mapdoc.CoverageComplete {
			t.Errorf("explicitly declared capability edge coverage = %q, want complete; reasons: %v", edge.Coverage.Status, edge.Coverage.Reasons)
		}
		for _, reason := range edge.Coverage.Reasons {
			if strings.Contains(reason, "containment") {
				t.Errorf("explicit edge carries containment reason unexpectedly: %v", edge.Coverage.Reasons)
			}
		}
		return
	}
	t.Fatal("no uses_capability edge was created")
}

// ── F-12: Capability granularity ─────────────────────────────────────────────

func TestCapabilityGranularityOneNodePerComponent(t *testing.T) {
	// Regression for F-12: multiple capability observations for the same
	// (name, projectID) must produce exactly one capability node with aggregated
	// evidence, not one node per file path.
	doc := mapdoc.New()
	component := mapdoc.NewNode(mapdoc.NodeComponent, []string{"svc", "svc/go.mod"}, "go")
	component.Properties = map[string]string{"root": "svc"}
	doc.Nodes = append(doc.Nodes, component)

	// 5 observations for the same capability from 5 different source files.
	var obs []intentmap.Observation
	for i := 0; i < 5; i++ {
		obs = append(obs, intentmap.Observation{
			Kind:      intentmap.KindCapability,
			Name:      "datastore:postgresql",
			ProjectID: "svc/go.mod",
			State:     "observed",
			Basis:     "imported",
			Path:      "svc/internal/" + string(rune('a'+i)) + ".go",
		})
	}
	report := &intentmap.Report{
		Coverage:     intentmap.Coverage{Status: "complete"},
		Observations: obs,
	}
	addIntent(&doc, report)

	capNodes := 0
	for _, n := range doc.Nodes {
		if n.Kind == mapdoc.NodeCapability && n.Name == "datastore:postgresql" {
			capNodes++
			count := n.Properties["evidence_path_count"]
			if count != "5" {
				t.Errorf("evidence_path_count = %q, want 5", count)
			}
			if len(n.Evidence) != 5 {
				t.Errorf("evidence entries = %d, want 5", len(n.Evidence))
			}
		}
	}
	if capNodes != 1 {
		t.Errorf("capability nodes = %d, want 1 (one per component, not one per file)", capNodes)
	}
}

func TestCapabilityGranularityEvidencePathsAreCapped(t *testing.T) {
	// Ensure that when total observations exceed maxCapabilityEvidencePaths,
	// the node still stores only maxCapabilityEvidencePaths evidence entries
	// but evidence_path_count reflects the real total.
	doc := mapdoc.New()
	component := mapdoc.NewNode(mapdoc.NodeComponent, []string{"svc", "svc/go.mod"}, "go")
	component.Properties = map[string]string{"root": "svc"}
	doc.Nodes = append(doc.Nodes, component)

	total := maxCapabilityEvidencePaths + 10
	var obs []intentmap.Observation
	for i := 0; i < total; i++ {
		obs = append(obs, intentmap.Observation{
			Kind:      intentmap.KindCapability,
			Name:      "cache:redis",
			ProjectID: "svc/go.mod",
			State:     "observed",
			Basis:     "imported",
			Path:      "svc/pkg/f" + string(rune('a'+i%26)) + ".go",
		})
	}
	report := &intentmap.Report{
		Coverage:     intentmap.Coverage{Status: "complete"},
		Observations: obs,
	}
	addIntent(&doc, report)

	for _, n := range doc.Nodes {
		if n.Kind == mapdoc.NodeCapability && n.Name == "cache:redis" {
			if len(n.Evidence) > maxCapabilityEvidencePaths {
				t.Errorf("evidence entries = %d, must not exceed maxCapabilityEvidencePaths=%d", len(n.Evidence), maxCapabilityEvidencePaths)
			}
			return
		}
	}
	t.Fatal("no cache:redis capability node created")
}

func TestDockerfileMultipleCoLocatedComponentsEmitsPartialBuildsToEach(t *testing.T) {
	// A root Dockerfile co-located with two components (e.g. mastodon's Ruby +
	// Node apps in the same root directory) should produce partial builds edges
	// to EACH component with reason dockerfile_co_located_with_multiple_components,
	// rather than being silently dropped because "exactly one co-located component"
	// doesn't hold.
	doc := mapdoc.New()
	ruby := mapdoc.NewNode(mapdoc.NodeComponent, []string{"."}, "ruby")
	ruby.Name = "Mastodon"
	ruby.Properties = map[string]string{"root": "."}
	npm := mapdoc.NewNode(mapdoc.NodeComponent, []string{"."}, "npm")
	npm.Name = "@mastodon/mastodon"
	npm.Properties = map[string]string{"root": "."}
	doc.Nodes = append(doc.Nodes, ruby, npm)

	ev := deployables.Evidence{Field: "FROM", Value: "ruby:3.3", Line: 1, Basis: "dockerfile-instruction"}
	// Name "(root)" reflects dockerfileDisplayName("Dockerfile") for the root Dockerfile.
	definitions := []deployables.Definition{
		{Provider: "dockerfile", Kind: "container_build", Name: "(root)", Path: "Dockerfile", Coverage: "complete", Evidence: []deployables.Evidence{ev}},
	}
	addDeployables(&doc, &deployables.Report{Status: "complete", Definitions: definitions})

	buildsEdges := 0
	for _, edge := range doc.Edges {
		if edge.Type != mapdoc.EdgeBuilds {
			continue
		}
		if edge.Coverage.Status != mapdoc.CoveragePartial {
			t.Fatalf("multi-component builds edge should be partial: %+v", edge)
		}
		found := false
		for _, r := range edge.Coverage.Reasons {
			if r == "dockerfile_co_located_with_multiple_components" {
				found = true
			}
		}
		if !found {
			t.Fatalf("multi-component builds edge missing expected reason: %+v", edge)
		}
		buildsEdges++
	}
	if buildsEdges != 2 {
		t.Fatalf("want 2 partial builds edges (one per co-located component), got %d", buildsEdges)
	}
}

func TestRootDockerfileCopyingMavenArchiveLinksArchiveModule(t *testing.T) {
	doc := mapdoc.New()
	aggregator := mapdoc.NewNode(mapdoc.NodeComponent, []string{"."}, "maven")
	aggregator.Name = "openmrs"
	aggregator.Properties = map[string]string{"root": "."}
	webapp := mapdoc.NewNode(mapdoc.NodeComponent, []string{"webapp", "webapp/pom.xml"}, "maven")
	webapp.Name = "openmrs-webapp"
	webapp.Properties = map[string]string{"root": "webapp"}
	doc.Nodes = append(doc.Nodes, aggregator, webapp)
	archiveEvidence := deployables.Evidence{Field: "packaging", Value: "war", Line: 5, Basis: "maven-pom-field"}
	copyEvidence := deployables.Evidence{Field: "COPY source", Value: "webapp/target/openmrs.war", Line: 8, Basis: "dockerfile-instruction"}
	definitions := []deployables.Definition{
		{Provider: "maven", Kind: "archive", Format: "war", Name: "openmrs.war", Path: "webapp/pom.xml", Coverage: "complete", Evidence: []deployables.Evidence{archiveEvidence}},
		{Provider: "dockerfile", Kind: "container_build", Name: "(root)", Path: "Dockerfile", Coverage: "complete", Evidence: []deployables.Evidence{{Field: "FROM", Line: 1, Basis: "dockerfile-instruction"}}, References: []deployables.Reference{{Kind: "copy_source", Value: copyEvidence.Value, Qualification: "local", Evidence: copyEvidence}}},
	}
	addDeployables(&doc, &deployables.Report{Status: "complete", Definitions: definitions})
	dockerID := mapdoc.NewNode(mapdoc.NodeDeployable, []string{"Dockerfile"}, "dockerfile:container_build:(root)").ID
	for _, edge := range doc.Edges {
		if edge.From != dockerID || edge.Type != mapdoc.EdgeBuilds {
			continue
		}
		if edge.To != webapp.ID {
			t.Fatalf("root Dockerfile attributed to %q; want WAR module %q", edge.To, webapp.ID)
		}
		if edge.Coverage.Status != mapdoc.CoveragePartial || len(edge.Coverage.Reasons) != 1 || edge.Coverage.Reasons[0] != "dockerfile_copy_source_matches_maven_archive" {
			t.Fatalf("artifact attribution must remain qualified: %+v", edge)
		}
		if len(edge.Evidence) != 1 || edge.Evidence[0].Path != "Dockerfile" || edge.Evidence[0].Span == nil || edge.Evidence[0].Span.StartLine != 8 {
			t.Fatalf("edge must cite the COPY source: %+v", edge.Evidence)
		}
		return
	}
	t.Fatal("no root Dockerfile builds edge emitted")
}

func TestGoModuleDisplayName(t *testing.T) {
	for _, test := range []struct{ module, want string }{
		{"github.com/grafana/loki/v3", "loki"},
		{"github.com/grafana/loki", "loki"},
		{"dircue", "dircue"},
		{"example.com/tools/v2/cmd", "cmd"},
	} {
		if got := goModuleDisplayName(test.module); got != test.want {
			t.Errorf("goModuleDisplayName(%q) = %q, want %q", test.module, got, test.want)
		}
	}
}
