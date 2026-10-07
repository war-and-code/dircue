package cli

import (
	"context"
	"path"
	"slices"
	"strings"

	"github.com/war-and-code/dircue/pkg/assessment"
	"github.com/war-and-code/dircue/pkg/declarations"
	"github.com/war-and-code/dircue/pkg/deployables"
	"github.com/war-and-code/dircue/pkg/intentmap"
	"github.com/war-and-code/dircue/pkg/mapbuild"
	"github.com/war-and-code/dircue/pkg/mapdoc"
	"github.com/war-and-code/dircue/pkg/pathrole"
	"github.com/war-and-code/dircue/pkg/profile"
)

// deployableEntryPointKinds classifies every known provider:kind pair produced
// by pkg/deployables. True means the kind represents a deployment entry point
// (a runnable or shippable unit). False means the kind is excluded: it describes
// supporting infrastructure, a CI/CD orchestration workflow, or build tooling
// rather than a deployable workload.
//
// Any provider:kind pair not present in this map is unclassified. Tests enforce
// that the map stays complete whenever new kinds are added to pkg/deployables.
var deployableEntryPointKinds = map[string]bool{
	// Container runtimes and image builders
	"dockerfile:container_build": true,
	"skaffold:container_build":   true, // Associations produced by assessmentSkaffoldEntries.
	"compose:service":            true,

	// Kubernetes workloads vs. supporting resources
	"kubernetes:workload":       true,
	"kubernetes:service":        false,
	"kubernetes:infrastructure": false,
	"kubernetes:resource":       false,

	// Helm: application charts are entry points; library charts are excluded
	"helm:infrastructure": true,  // application chart (type absent or "application")
	"helm:library":        false, // library chart (type: library)

	// Terraform infrastructure-as-code (not a runnable workload itself)
	"terraform:infrastructure": false,

	// CloudFormation: compute resources are entry points; other resources are excluded
	"cloudformation:infrastructure": false,
	"cloudformation:function":       true,
	"cloudformation:container_task": true,

	// CI/CD pipeline definitions (orchestration, not entry points)
	"github-actions:workflow": false,
	"gitlab-ci:workflow":      false,
	"jenkins:workflow":        false,
	"tekton:workflow":         false,

	// Process and archive declarations
	"maven:archive":    true,
	"procfile:process": true,

	// Build invocations are build tooling, not deployable units
	"makefile:build_invocation": false,

	// Platform-specific service and function definitions
	"aspire-apphost:service":       true,
	"serverless-framework:service": true,
}

// Entry-point assessment reuses manifest interfaces and the map's static
// deployment associations. It does not run the source-code intent detectors.
func enrichAssessmentEntryPoints(ctx context.Context, report *profile.Report, deployment *deployables.Report) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if report.Assessment == nil || report.Assessment.Structure == nil {
		return nil
	}
	intent := intentmap.New(intentmap.Options{})
	if report.Declarations != nil {
		intent.AddDeclarations(report.Declarations.Projects)
	}
	observations, err := intent.Finish(ctx)
	if err != nil {
		return err
	}
	manifestEntries := assessmentManifestEntries(report.Declarations)
	assessment.SetStructureEntryPoints(report.Assessment, manifestEntries)
	document, err := mapbuild.Build(report, mapbuild.Options{Deployables: deployment, Intent: observations})
	if err != nil {
		// Association failure must not discard independently counted inventory.
		setAssessmentEntryPointCoverage(report.Assessment, assessment.StructureCoverage{
			Scope: "entry_points", Ecosystem: "all", Status: "partial",
			Reasons: []string{"entry_point_association_unavailable", "source_entry_points_not_inspected"},
		})
		return assessment.ValidateReport(report.Assessment)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	result := assessmentEntries(document)
	skaffoldEntries := assessmentSkaffoldEntries(document, deployment)
	entries := append(manifestEntries, result.entries...)
	entries = append(entries, skaffoldEntries...)
	assessment.SetStructureEntryPoints(report.Assessment, entries)
	report.Assessment.Structure.ExcludedNonEntryKinds = result.excluded
	reasons := []string{"manifest_and_deployment_entry_point_catalog", "source_entry_points_not_inspected"}
	if deployment != nil && deployment.Status != "complete" {
		reasons = append(reasons, "deployment_observations_incomplete")
	}
	if report.Declarations != nil && report.Declarations.Status != "complete" {
		reasons = append(reasons, "declaration_observations_incomplete")
	}
	for _, entry := range entries {
		if entry.State != "declared" {
			reasons = append(reasons, "entry_point_associations_qualified")
			break
		}
	}
	slices.Sort(reasons)
	setAssessmentEntryPointCoverage(report.Assessment, assessment.StructureCoverage{
		Scope: "entry_points", Ecosystem: "all", Status: "partial", Reasons: slices.Compact(reasons),
	})
	return assessment.ValidateReport(report.Assessment)
}

func setAssessmentEntryPointCoverage(report *assessment.Report, coverage assessment.StructureCoverage) {
	report.Structure.Coverage = slices.DeleteFunc(report.Structure.Coverage, func(old assessment.StructureCoverage) bool {
		return old.Scope == coverage.Scope && old.Ecosystem == coverage.Ecosystem
	})
	report.Structure.Coverage = append(report.Structure.Coverage, coverage)
	slices.SortFunc(report.Structure.Coverage, func(a, b assessment.StructureCoverage) int {
		return strings.Compare(a.Scope+"\x00"+a.Ecosystem, b.Scope+"\x00"+b.Ecosystem)
	})
}

// deploymentAssessmentResult holds the entry-point observations and the count
// of deployable nodes that were excluded because their kind is not an entry
// point (e.g. kubernetes infrastructure and service resources).
type deploymentAssessmentResult struct {
	entries  []assessment.StructureEntryPoint
	excluded map[string]int64 // "provider:kind" → excluded node count
}

func assessmentEntries(document mapdoc.Document) deploymentAssessmentResult {
	components := make(map[string]string)
	for _, node := range document.Nodes {
		if node.Kind == mapdoc.NodeComponent && len(node.Evidence) > 0 {
			components[node.ID] = node.Evidence[0].Path
		}
	}
	associations := make(map[string][]mapdoc.Edge)
	for _, edge := range document.Edges {
		if (edge.Type == mapdoc.EdgeBuilds || edge.Type == mapdoc.EdgeRuns) && components[edge.To] != "" {
			associations[edge.From] = append(associations[edge.From], edge)
		}
	}
	entries := []assessment.StructureEntryPoint{}
	excluded := map[string]int64{}
	for _, node := range document.Nodes {
		if len(node.Evidence) == 0 {
			continue
		}
		switch node.Kind {
		case mapdoc.NodeDeployable:
			provider := node.Properties["provider"]
			kind := node.Properties["kind"]
			isEntry, classified := deployableEntryPointKinds[provider+":"+kind]
			if !classified || !isEntry {
				excluded[provider+":"+kind]++
				continue
			}
			// Skaffold container_build entries are assembled by assessmentSkaffoldEntries
			// from declared build_context references; they do not receive EdgeBuilds edges
			// in the map and must not be added here as "unassociated".
			if provider == "skaffold" {
				continue
			}
			filename := node.Evidence[0].Path
			base := assessment.StructureEntryPoint{EvidencePath: filename, Role: pathrole.Of(filename), Name: node.Name, Basis: string(node.Evidence[0].Basis), State: "declared", Ecosystem: "other"}
			if node.Coverage.Status != mapdoc.CoverageComplete {
				nodeReasons := node.Coverage.Reasons
				if len(nodeReasons) == 0 {
					nodeReasons = []string{"entry_point_node_partially_qualified"}
				}
				base.State = "qualified"
				base.Reason = strings.Join(nodeReasons, ";")
			}
			base.Kind = "deployment:" + provider + ":" + kind
			edges := associations[node.ID]
			if len(edges) == 0 {
				base.State, base.Reason = "unassociated", "entry_point_owner_unresolved"
				entries = append(entries, base)
				continue
			}
			for _, edge := range edges {
				entry := base
				entry.Kind = string(edge.Type) + ":" + base.Kind
				entry.ProjectID = components[edge.To]
				entry.Ecosystem = assessment.EcosystemForManifest(entry.ProjectID)
				if edge.Coverage.Status == mapdoc.CoverageComplete {
					entry.State = "associated"
					entry.Reason = ""
				} else {
					edgeReasons := edge.Coverage.Reasons
					if len(edgeReasons) == 0 {
						edgeReasons = []string{"entry_point_association_partially_qualified"}
					}
					entry.State = "qualified"
					entry.Reason = strings.Join(edgeReasons, ";")
				}
				entry.Basis = deploymentEntryBasis(edge)
				entries = append(entries, entry)
			}
		}
	}
	if len(excluded) == 0 {
		excluded = nil
	}
	return deploymentAssessmentResult{entries: entries, excluded: excluded}
}

// deploymentEntryBasis derives the evidence basis for an association edge.
// Edge coverage reasons record the actual inference method used by observers.go
// and take precedence over the generic evidence basis on the edge itself.
// Every reason string produced by addRelationship in observers.go must be
// listed here; unlisted reasons fall through to the evidence basis.
func deploymentEntryBasis(edge mapdoc.Edge) string {
	for _, r := range edge.Coverage.Reasons {
		switch r {
		// Explicit declarations in configuration files
		case "declared_context_matches_component_root",
			"service_declares_build_context",
			"maven_pom_declares_war_packaging":
			return "declared_config"
		// Layout proximity — no explicit path declaration, positional match
		case "dockerfile_co_located_with_component",
			"dockerfile_co_located_with_multiple_components":
			return "directory_co_location"
		// Image reference matching — indirect, via image tag or basename
		case "image_matches_compose_build_declaration",
			"kubernetes_image_matches_skaffold_artifact_and_context":
			return "image_reference_match"
		// Rule-based inference — heuristic patterns with structural evidence
		case "dockerfile_copy_source_matches_maven_archive",
			"dockerfile_copy_and_build_evidence_matches_cargo_component",
			"image_basename_matches_unique_component_with_dockerfile_evidence":
			return "rule_inferred"
		}
	}
	if len(edge.Evidence) > 0 {
		return string(edge.Evidence[0].Basis)
	}
	return "rule_inferred"
}

// assessmentSkaffoldEntries produces entry-point observations for
// skaffold:container_build definitions by matching their declared build_context
// references directly against component roots in the document. This is done at
// the assessment layer rather than in the map because Skaffold nodes do not
// receive EdgeBuilds edges (the guard in observers.go prevents dual build paths:
// one via Skaffold context matching and one via Kubernetes image matching).
//
// Association rules per build_context reference:
//   - unresolved context (dynamic template): emit unassociated,
//     reason "entry_point_build_context_unresolved"
//   - resolved context matches zero components: emit unassociated,
//     reason "entry_point_owner_unresolved"
//   - resolved context matches exactly one component: emit associated,
//     no reason
//   - resolved context matches several components: emit one qualified row
//     per component, reason "build_context_matches_multiple_projects"
func assessmentSkaffoldEntries(document mapdoc.Document, deployment *deployables.Report) []assessment.StructureEntryPoint {
	if deployment == nil {
		return nil
	}
	// Index component roots: cleaned root path → all component evidence paths.
	rootToComponents := map[string][]string{}
	for _, node := range document.Nodes {
		if node.Kind == mapdoc.NodeComponent && len(node.Evidence) > 0 {
			if root := node.Properties["root"]; root != "" {
				cleaned := path.Clean(root)
				rootToComponents[cleaned] = append(rootToComponents[cleaned], node.Evidence[0].Path)
			}
		}
	}
	var entries []assessment.StructureEntryPoint
	for _, def := range deployment.Definitions {
		if def.Provider != "skaffold" || def.Kind != "container_build" {
			continue
		}
		for _, ref := range def.References {
			if ref.Kind != "build_context" {
				continue
			}
			base := assessment.StructureEntryPoint{
				EvidencePath: def.Path,
				Role:         pathrole.Of(def.Path),
				Kind:         "deployment:skaffold:container_build",
				Name:         def.Name,
				Basis:        "declared_config",
			}
			// Unresolved (dynamic) context: cannot match any component.
			if ref.Qualification == "unresolved" {
				base.State = "unassociated"
				base.Reason = "entry_point_build_context_unresolved"
				entries = append(entries, base)
				continue
			}
			resolved := path.Clean(path.Join(path.Dir(def.Path), ref.Value))
			components := rootToComponents[resolved]
			switch len(components) {
			case 0:
				base.State = "unassociated"
				base.Reason = "entry_point_owner_unresolved"
				entries = append(entries, base)
			case 1:
				base.Kind = "builds:" + base.Kind
				base.State = "associated"
				base.Reason = ""
				base.ProjectID = components[0]
				base.Ecosystem = assessment.EcosystemForManifest(components[0])
				entries = append(entries, base)
			default:
				for _, projID := range components {
					entry := base
					entry.Kind = "builds:" + base.Kind
					entry.State = "qualified"
					entry.Reason = "build_context_matches_multiple_projects"
					entry.ProjectID = projID
					entry.Ecosystem = assessment.EcosystemForManifest(projID)
					entries = append(entries, entry)
				}
			}
		}
	}
	return entries
}

// Manifest interfaces are projected directly so different interface kinds
// with the same name cannot collapse through the map's node identity rules.
func assessmentManifestEntries(report *declarations.Report) []assessment.StructureEntryPoint {
	entries := []assessment.StructureEntryPoint{}
	if report == nil {
		return entries
	}
	for _, project := range report.Projects {
		for _, declaration := range project.Interfaces {
			if !assessmentEntryInterface(declaration.Kind) {
				continue
			}
			entry := assessment.StructureEntryPoint{
				ProjectID: project.ID, EvidencePath: declaration.Evidence,
				Ecosystem: assessment.EcosystemForManifest(project.ID), Role: pathrole.Of(project.ID),
				Kind: "manifest_interface:" + declaration.Kind, Name: declaration.Name,
				Target: declaration.Target, Basis: "declared_manifest", State: "declared",
			}
			if declaration.State != "declared" && declaration.State != "resolved" || declaration.Condition != "" {
				entry.State = "qualified"
				entry.Reason = "entry_point_declaration_requires_evaluation"
			}
			entries = append(entries, entry)
		}
	}
	return entries
}

func assessmentEntryInterface(kind string) bool {
	switch kind {
	case "entrypoint", "script", "binary", "dotnet-application", "main-class", "maven-main-class", "gradle-main-class", "spring-boot-main-class", "cargo-bin", "cargo-default-run", "cargo-build-script", "cargo-test", "cargo-bench", "cargo-example", "python-console-script", "python-gui-script", "python-entry-point":
		return true
	default:
		return false
	}
}
