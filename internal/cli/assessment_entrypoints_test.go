package cli

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/war-and-code/dircue/pkg/declarations"
	"github.com/war-and-code/dircue/pkg/deployables"
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
		if c.Scope == "entry_points" && c.Ecosystem == "all" && c.Status == "partial" &&
			slices.Contains(c.Reasons, "entry_point_association_unavailable") &&
			slices.Contains(c.Reasons, "source_entry_points_not_inspected") {
			qualified = true
		}
	}
	if !qualified {
		t.Fatal("association failure or uninspected source-entrypoint scope was not disclosed")
	}
}

func TestAssessmentDeploymentUsesCanonicalProjectEcosystem(t *testing.T) {
	d := mapdoc.Document{
		Nodes: []mapdoc.Node{
			{ID: "component", Kind: mapdoc.NodeComponent, Evidence: []mapdoc.Evidence{{Path: "App.csproj"}}, Properties: map[string]string{"ecosystem": "dotnet"}},
			{ID: "docker", Kind: mapdoc.NodeDeployable, Name: "Dockerfile", Evidence: []mapdoc.Evidence{{Path: "Dockerfile", Basis: mapdoc.BasisDeclaredConfig}}, Coverage: mapdoc.Coverage{Status: mapdoc.CoverageComplete}, Properties: map[string]string{"provider": "dockerfile", "kind": "container_build"}},
		},
		Edges: []mapdoc.Edge{{From: "docker", To: "component", Type: mapdoc.EdgeBuilds, Coverage: mapdoc.Coverage{Status: mapdoc.CoveragePartial, Reasons: []string{"literal_project_path"}}}},
	}
	result := assessmentEntries(d)
	entries := result.entries
	if len(entries) != 1 || entries[0].Ecosystem != "nuget" || entries[0].State != "qualified" || entries[0].ProjectID != "App.csproj" {
		t.Fatalf("deployment association taxonomy/qualification mismatch: %+v", entries)
	}
}

func TestAssessmentCompleteCoverageEdgeBecomesAssociated(t *testing.T) {
	d := mapdoc.Document{
		Nodes: []mapdoc.Node{
			{ID: "svc", Kind: mapdoc.NodeComponent, Evidence: []mapdoc.Evidence{{Path: "go.mod"}}},
			{ID: "compose", Kind: mapdoc.NodeDeployable, Name: "web", Evidence: []mapdoc.Evidence{{Path: "docker-compose.yml", Basis: mapdoc.BasisDeclaredConfig}}, Coverage: mapdoc.Coverage{Status: mapdoc.CoverageComplete}, Properties: map[string]string{"provider": "compose", "kind": "service"}},
		},
		Edges: []mapdoc.Edge{{From: "compose", To: "svc", Type: mapdoc.EdgeRuns, Coverage: mapdoc.Coverage{Status: mapdoc.CoverageComplete}}},
	}
	result := assessmentEntries(d)
	entries := result.entries
	if len(entries) != 1 || entries[0].State != "associated" || entries[0].Reason != "" {
		t.Fatalf("complete-coverage edge must produce associated state with empty reason: %+v", entries)
	}
}

func TestAssessmentKubernetesNonWorkloadKindsExcluded(t *testing.T) {
	d := mapdoc.Document{
		Nodes: []mapdoc.Node{
			{ID: "comp", Kind: mapdoc.NodeComponent, Evidence: []mapdoc.Evidence{{Path: "go.mod"}}},
			{ID: "workload", Kind: mapdoc.NodeDeployable, Name: "Deployment", Evidence: []mapdoc.Evidence{{Path: "deploy.yaml", Basis: mapdoc.BasisDeclaredConfig}}, Coverage: mapdoc.Coverage{Status: mapdoc.CoverageComplete}, Properties: map[string]string{"provider": "kubernetes", "kind": "workload"}},
			{ID: "svc", Kind: mapdoc.NodeDeployable, Name: "Service", Evidence: []mapdoc.Evidence{{Path: "svc.yaml", Basis: mapdoc.BasisDeclaredConfig}}, Coverage: mapdoc.Coverage{Status: mapdoc.CoverageComplete}, Properties: map[string]string{"provider": "kubernetes", "kind": "service"}},
			{ID: "infra", Kind: mapdoc.NodeDeployable, Name: "ConfigMap", Evidence: []mapdoc.Evidence{{Path: "cm.yaml", Basis: mapdoc.BasisDeclaredConfig}}, Coverage: mapdoc.Coverage{Status: mapdoc.CoverageComplete}, Properties: map[string]string{"provider": "kubernetes", "kind": "infrastructure"}},
			{ID: "res", Kind: mapdoc.NodeDeployable, Name: "VirtualService", Evidence: []mapdoc.Evidence{{Path: "vs.yaml", Basis: mapdoc.BasisDeclaredConfig}}, Coverage: mapdoc.Coverage{Status: mapdoc.CoverageComplete}, Properties: map[string]string{"provider": "kubernetes", "kind": "resource"}},
		},
		Edges: []mapdoc.Edge{{From: "workload", To: "comp", Type: mapdoc.EdgeRuns, Coverage: mapdoc.Coverage{Status: mapdoc.CoverageComplete}}},
	}
	result := assessmentEntries(d)
	if len(result.entries) != 1 || result.entries[0].Name != "Deployment" {
		t.Fatalf("non-workload kubernetes kinds must be excluded from entry points: %+v", result.entries)
	}
	if result.excluded["kubernetes:service"] != 1 || result.excluded["kubernetes:infrastructure"] != 1 || result.excluded["kubernetes:resource"] != 1 {
		t.Fatalf("excluded kind counts incorrect: %+v", result.excluded)
	}
}

// TestAssessmentSkaffoldBuildContextAssociatesWithProject verifies that
// assessmentSkaffoldEntries matches a build_context reference to a component
// root and produces an associated entry (single component → associated, no reason).
// Skaffold nodes are not processed by assessmentEntries (the map guard prevents
// EdgeBuilds edges for Skaffold); this test exercises the assessment-layer path.
func TestAssessmentSkaffoldBuildContextAssociatesWithProject(t *testing.T) {
	d := mapdoc.Document{
		Nodes: []mapdoc.Node{
			{ID: "comp", Kind: mapdoc.NodeComponent, Evidence: []mapdoc.Evidence{{Path: "go.mod"}}, Properties: map[string]string{"root": "."}},
		},
	}
	dep := &deployables.Report{
		Definitions: []deployables.Definition{{
			Provider: "skaffold", Kind: "container_build",
			Name: "gcr.io/myapp/server",
			Path: "skaffold.yaml",
			References: []deployables.Reference{
				{Kind: "build_context", Value: ".", Qualification: "local"},
			},
		}},
	}
	entries := assessmentSkaffoldEntries(d, dep)
	if len(entries) != 1 || entries[0].State != "associated" || entries[0].ProjectID != "go.mod" {
		t.Fatalf("skaffold build context must associate with a project (single component → associated): %+v", entries)
	}
	if entries[0].Reason != "" {
		t.Fatalf("skaffold associated entry must have empty reason, got: %s", entries[0].Reason)
	}
	if entries[0].Basis != "declared_config" {
		t.Fatalf("skaffold entry basis should be declared_config: %s", entries[0].Basis)
	}
	if entries[0].Kind != "builds:deployment:skaffold:container_build" {
		t.Fatalf("associated skaffold entry kind = %q", entries[0].Kind)
	}
}

// TestAssessmentSkaffoldMultipleComponentsProducesQualified verifies that when
// a build_context resolves to multiple components, one qualified row per
// component is emitted with reason "build_context_matches_multiple_projects".
func TestAssessmentSkaffoldMultipleComponentsProducesQualified(t *testing.T) {
	d := mapdoc.Document{
		Nodes: []mapdoc.Node{
			{ID: "comp1", Kind: mapdoc.NodeComponent, Evidence: []mapdoc.Evidence{{Path: "go.mod"}}, Properties: map[string]string{"root": "."}},
			{ID: "comp2", Kind: mapdoc.NodeComponent, Evidence: []mapdoc.Evidence{{Path: "package.json"}}, Properties: map[string]string{"root": "."}},
		},
	}
	dep := &deployables.Report{
		Definitions: []deployables.Definition{{
			Provider: "skaffold", Kind: "container_build",
			Name: "gcr.io/myapp/shared",
			Path: "skaffold.yaml",
			References: []deployables.Reference{
				{Kind: "build_context", Value: ".", Qualification: "local"},
			},
		}},
	}
	entries := assessmentSkaffoldEntries(d, dep)
	if len(entries) != 2 {
		t.Fatalf("multi-component context must produce one row per component, got: %+v", entries)
	}
	for _, e := range entries {
		if e.State != "qualified" || e.Reason != "build_context_matches_multiple_projects" {
			t.Fatalf("multi-component entry must be qualified with reason build_context_matches_multiple_projects: %+v", e)
		}
	}
}

// TestAssessmentSkaffoldUnresolvedContextProducesUnassociated verifies that a
// build_context with dynamic/unresolved qualification emits an unassociated row
// with reason "entry_point_build_context_unresolved" (not silently skipped).
func TestAssessmentSkaffoldUnresolvedContextProducesUnassociated(t *testing.T) {
	d := mapdoc.Document{
		Nodes: []mapdoc.Node{
			{ID: "comp", Kind: mapdoc.NodeComponent, Evidence: []mapdoc.Evidence{{Path: "go.mod"}}, Properties: map[string]string{"root": "."}},
		},
	}
	dep := &deployables.Report{
		Definitions: []deployables.Definition{{
			Provider: "skaffold", Kind: "container_build",
			Name: "gcr.io/myapp/server",
			Path: "skaffold.yaml",
			References: []deployables.Reference{
				{Kind: "build_context", Value: "{{.Values.context}}", Qualification: "unresolved"},
			},
		}},
	}
	entries := assessmentSkaffoldEntries(d, dep)
	if len(entries) != 1 || entries[0].State != "unassociated" || entries[0].Reason != "entry_point_build_context_unresolved" {
		t.Fatalf("unresolved context must produce unassociated with entry_point_build_context_unresolved: %+v", entries)
	}
}

// TestAssessmentSkaffoldNoMatchProducesUnassociated verifies that a resolved
// build_context with no matching component emits unassociated with
// reason "entry_point_owner_unresolved".
func TestAssessmentSkaffoldNoMatchProducesUnassociated(t *testing.T) {
	d := mapdoc.Document{
		Nodes: []mapdoc.Node{
			{ID: "comp", Kind: mapdoc.NodeComponent, Evidence: []mapdoc.Evidence{{Path: "services/api/go.mod"}}, Properties: map[string]string{"root": "services/api"}},
		},
	}
	dep := &deployables.Report{
		Definitions: []deployables.Definition{{
			Provider: "skaffold", Kind: "container_build",
			Name: "gcr.io/myapp/frontend",
			Path: "skaffold.yaml",
			References: []deployables.Reference{
				{Kind: "build_context", Value: "services/frontend", Qualification: "local"},
			},
		}},
	}
	entries := assessmentSkaffoldEntries(d, dep)
	if len(entries) != 1 || entries[0].State != "unassociated" || entries[0].Reason != "entry_point_owner_unresolved" {
		t.Fatalf("unmatched context must produce unassociated with entry_point_owner_unresolved: %+v", entries)
	}
	// An unassociated row names the declaration, like other unassociated deployment rows.
	if entries[0].Kind != "deployment:skaffold:container_build" {
		t.Fatalf("unassociated skaffold entry kind = %q", entries[0].Kind)
	}
}

// TestAssessmentDeployableEntryPointTableClassifiesAllKnownKinds checks that
// every provider:kind pair produced by pkg/deployables is present in
// deployableEntryPointKinds. If pkg/deployables adds a new kind, this test
// will fail until the table is updated with an explicit classification.
func TestAssessmentDeployableEntryPointTableClassifiesAllKnownKinds(t *testing.T) {
	// Exhaustive list from pkg/deployables/parse.go and pkg/deployables/types.go.
	knownKinds := []string{
		"dockerfile:container_build",
		"skaffold:container_build",
		"compose:service",
		"kubernetes:workload",
		"kubernetes:service",
		"kubernetes:infrastructure",
		"kubernetes:resource",
		"helm:infrastructure",
		"helm:library",
		"terraform:infrastructure",
		"cloudformation:infrastructure",
		"cloudformation:function",
		"cloudformation:container_task",
		"github-actions:workflow",
		"gitlab-ci:workflow",
		"jenkins:workflow",
		"tekton:workflow",
		"maven:archive",
		"procfile:process",
		"makefile:build_invocation",
		"aspire-apphost:service",
		"serverless-framework:service",
	}
	for _, pk := range knownKinds {
		if _, ok := deployableEntryPointKinds[pk]; !ok {
			t.Errorf("known provider:kind %q is missing from deployableEntryPointKinds", pk)
		}
	}
}

// TestAssessmentUnclassifiedKindIsExcluded verifies that a provider:kind pair
// not present in deployableEntryPointKinds is excluded from entry points.
func TestAssessmentUnclassifiedKindIsExcluded(t *testing.T) {
	d := mapdoc.Document{
		Nodes: []mapdoc.Node{
			{ID: "comp", Kind: mapdoc.NodeComponent, Evidence: []mapdoc.Evidence{{Path: "go.mod"}}},
			{ID: "u", Kind: mapdoc.NodeDeployable, Name: "thing", Evidence: []mapdoc.Evidence{{Path: "thing.yaml", Basis: mapdoc.BasisDeclaredConfig}}, Coverage: mapdoc.Coverage{Status: mapdoc.CoverageComplete}, Properties: map[string]string{"provider": "future-provider", "kind": "future-kind"}},
		},
	}
	result := assessmentEntries(d)
	if len(result.entries) != 0 {
		t.Fatalf("unclassified provider:kind must not produce entry points: %+v", result.entries)
	}
	if result.excluded["future-provider:future-kind"] != 1 {
		t.Fatalf("unclassified kind must be counted in excluded: %+v", result.excluded)
	}
}

// TestAssessmentDeploymentEntryBasisAllReasons verifies that deploymentEntryBasis
// maps every reason string produced by observers.go to the correct basis value.
func TestAssessmentDeploymentEntryBasisAllReasons(t *testing.T) {
	tests := []struct {
		reason string
		want   string
	}{
		{"declared_context_matches_component_root", "declared_config"},
		{"service_declares_build_context", "declared_config"},
		{"maven_pom_declares_war_packaging", "declared_config"},
		// dockerfile_copy_source_matches_maven_archive infers from COPY instructions → rule_inferred
		{"dockerfile_copy_source_matches_maven_archive", "rule_inferred"},
		{"dockerfile_copy_and_build_evidence_matches_cargo_component", "rule_inferred"},
		{"dockerfile_co_located_with_component", "directory_co_location"},
		{"dockerfile_co_located_with_multiple_components", "directory_co_location"},
		{"image_matches_compose_build_declaration", "image_reference_match"},
		{"kubernetes_image_matches_skaffold_artifact_and_context", "image_reference_match"},
		{"image_basename_matches_unique_component_with_dockerfile_evidence", "rule_inferred"},
	}
	for _, tc := range tests {
		edge := mapdoc.Edge{Coverage: mapdoc.Coverage{Reasons: []string{tc.reason}}}
		if got := deploymentEntryBasis(edge); got != tc.want {
			t.Errorf("deploymentEntryBasis(%q) = %q, want %q", tc.reason, got, tc.want)
		}
	}
	// No reasons and no edge evidence falls back to rule_inferred (heuristic match,
	// not an explicit config declaration).
	if got := deploymentEntryBasis(mapdoc.Edge{}); got != "rule_inferred" {
		t.Errorf("deploymentEntryBasis(no reasons) = %q, want %q", got, "rule_inferred")
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

// TestAssessmentCloudFormationComputeKindsAreEntryPoints verifies that
// CloudFormation Lambda/SAM functions and ECS/Batch/AppRunner compute resources
// are classified as entry points, while plain infrastructure resources are excluded.
func TestAssessmentCloudFormationComputeKindsAreEntryPoints(t *testing.T) {
	d := mapdoc.Document{
		Nodes: []mapdoc.Node{
			{ID: "fn", Kind: mapdoc.NodeDeployable, Name: "MyFunction", Evidence: []mapdoc.Evidence{{Path: "template.yaml", Basis: mapdoc.BasisDeclaredConfig}}, Coverage: mapdoc.Coverage{Status: mapdoc.CoverageComplete}, Properties: map[string]string{"provider": "cloudformation", "kind": "function"}},
			{ID: "task", Kind: mapdoc.NodeDeployable, Name: "MyTask", Evidence: []mapdoc.Evidence{{Path: "template.yaml", Basis: mapdoc.BasisDeclaredConfig}}, Coverage: mapdoc.Coverage{Status: mapdoc.CoverageComplete}, Properties: map[string]string{"provider": "cloudformation", "kind": "container_task"}},
			{ID: "bucket", Kind: mapdoc.NodeDeployable, Name: "MyBucket", Evidence: []mapdoc.Evidence{{Path: "template.yaml", Basis: mapdoc.BasisDeclaredConfig}}, Coverage: mapdoc.Coverage{Status: mapdoc.CoverageComplete}, Properties: map[string]string{"provider": "cloudformation", "kind": "infrastructure"}},
		},
	}
	result := assessmentEntries(d)
	if len(result.entries) != 2 {
		t.Fatalf("cloudformation:function and cloudformation:container_task must be entry points, got: %+v", result.entries)
	}
	for _, e := range result.entries {
		if e.State != "unassociated" {
			t.Fatalf("unmatched cloudformation compute resource must be unassociated: %+v", e)
		}
	}
	if result.excluded["cloudformation:infrastructure"] != 1 {
		t.Fatalf("cloudformation:infrastructure must be excluded: %+v", result.excluded)
	}
}

// TestAssessmentHelmLibraryChartExcluded verifies that helm:library charts are
// excluded from entry points, while helm:infrastructure (application) charts are
// treated as entry points.
func TestAssessmentHelmLibraryChartExcluded(t *testing.T) {
	d := mapdoc.Document{
		Nodes: []mapdoc.Node{
			{ID: "app", Kind: mapdoc.NodeDeployable, Name: "my-app", Evidence: []mapdoc.Evidence{{Path: "charts/my-app/Chart.yaml", Basis: mapdoc.BasisDeclaredConfig}}, Coverage: mapdoc.Coverage{Status: mapdoc.CoverageComplete}, Properties: map[string]string{"provider": "helm", "kind": "infrastructure"}},
			{ID: "lib", Kind: mapdoc.NodeDeployable, Name: "my-lib", Evidence: []mapdoc.Evidence{{Path: "charts/my-lib/Chart.yaml", Basis: mapdoc.BasisDeclaredConfig}}, Coverage: mapdoc.Coverage{Status: mapdoc.CoverageComplete}, Properties: map[string]string{"provider": "helm", "kind": "library"}},
		},
	}
	result := assessmentEntries(d)
	if len(result.entries) != 1 || result.entries[0].Name != "my-app" {
		t.Fatalf("helm:library must be excluded; helm:infrastructure must be an entry point: entries=%+v", result.entries)
	}
	if result.excluded["helm:library"] != 1 {
		t.Fatalf("helm:library must be counted in excluded: %+v", result.excluded)
	}
}

// TestAssessmentObserversReasonsCoveredByBasisTable verifies that every reason
// string produced by addRelationship in pkg/mapbuild/observers.go is present in
// the deploymentEntryBasis switch statement. Unlisted reasons fall through to
// the edge evidence basis, which may be incorrect.
func TestAssessmentObserversReasonsCoveredByBasisTable(t *testing.T) {
	observersPath := filepath.Join("..", "..", "pkg", "mapbuild", "observers.go")
	content, err := os.ReadFile(observersPath)
	if err != nil {
		t.Fatalf("cannot read observers.go: %v", err)
	}
	// Reasons produced by addRelationship calls in observers.go.
	knownObserverReasons := []string{
		"declared_context_matches_component_root",
		"service_declares_build_context",
		"maven_pom_declares_war_packaging",
		"dockerfile_copy_source_matches_maven_archive",
		"dockerfile_copy_and_build_evidence_matches_cargo_component",
		"dockerfile_co_located_with_component",
		"dockerfile_co_located_with_multiple_components",
		"image_matches_compose_build_declaration",
		"kubernetes_image_matches_skaffold_artifact_and_context",
	}
	for _, reason := range knownObserverReasons {
		if !strings.Contains(string(content), `"`+reason+`"`) {
			t.Errorf("reason %q is in basis table but not found in observers.go", reason)
		}
		// Verify each is mapped by deploymentEntryBasis.
		edge := mapdoc.Edge{Coverage: mapdoc.Coverage{Reasons: []string{reason}}}
		got := deploymentEntryBasis(edge)
		if got == "" {
			t.Errorf("deploymentEntryBasis(%q) returned empty string", reason)
		}
	}
}
