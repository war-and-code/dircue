package cli

import (
	"context"
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
			Scope: "entry_points", Ecosystem: "all", Status: "partial", Reasons: []string{"entry_point_association_unavailable"},
		})
		return assessment.ValidateReport(report.Assessment)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	entries := append(manifestEntries, assessmentEntries(document)...)
	assessment.SetStructureEntryPoints(report.Assessment, entries)
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

func assessmentEntries(document mapdoc.Document) []assessment.StructureEntryPoint {
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
	for _, node := range document.Nodes {
		if len(node.Evidence) == 0 {
			continue
		}
		filename := node.Evidence[0].Path
		base := assessment.StructureEntryPoint{EvidencePath: filename, Role: pathrole.Of(filename), Name: node.Name, Basis: string(node.Evidence[0].Basis), State: "declared", Ecosystem: "other"}
		if node.Coverage.Status != mapdoc.CoverageComplete {
			base.State = "qualified"
			base.Reason = strings.Join(node.Coverage.Reasons, ";")
		}
		switch node.Kind {
		case mapdoc.NodeDeployable:
			base.Kind = "deployment:" + node.Properties["provider"] + ":" + node.Properties["kind"]
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
				entry.State = "qualified"
				entry.Reason = strings.Join(edge.Coverage.Reasons, ";")
				if len(edge.Evidence) > 0 {
					entry.Basis = string(edge.Evidence[0].Basis)
				}
				entries = append(entries, entry)
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
