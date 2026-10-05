package reportdiff

import (
	"github.com/war-and-code/dircue/pkg/assessment"
	"github.com/war-and-code/dircue/pkg/profile"
)

// Assessment compares aggregate measurements. Retained examples are metadata,
// so crossing an example limit cannot look like a removed manifest or project.
func assessmentModules(p profile.Report, result map[string]moduleData) {
	m := newModule()
	r := p.Assessment
	if r == nil {
		result["assessment"] = m
		return
	}
	m.present, m.complete, m.status = true, true, "complete"
	m.policy = map[string]any{"version": r.Version, "source_mode": r.Source.Mode, "definitions": r.Definitions}
	m.metadata = map[string]any{"tree": r.Source.Tree, "omitted_candidate_evidence": r.OmittedCandidateEvidence, "omitted_project_root_evidence": r.OmittedProjectRootEvidence, "omitted_workspace_evidence": r.OmittedWorkspaceEvidence, "omitted_local_dependency_evidence": r.OmittedLocalEvidence}
	add := func(id string, value assessment.Metric) {
		m.add(id, value)
		if value.Completeness != "complete" {
			m.complete = false
			m.status = "partial"
			m.reasons = appendReason(m.reasons, "assessment_measurement_is_lower_bound")
		}
	}
	add("inventory:files", r.Inventory.Files)
	add("inventory:bytes", r.Inventory.Bytes)
	add("manifest_candidate_population", r.ManifestCandidatePopulation)
	add("unparsed_manifest_candidates", r.UnparsedManifestCandidates)
	add("filename_candidate_population", r.FilenameCandidatePopulation)
	add("unsupported_ecosystem_projects", r.UnsupportedEcosystemProjects)
	add("projects", r.Projects)
	add("project_roots", r.ProjectRoots)
	add("workspace_membership", r.WorkspaceMembership)
	add("local_dependencies", r.LocalDependencies)
	for _, row := range r.ManifestCandidates {
		m.add("manifest:"+key(row.Filename, row.Kind, row.Ecosystem), row)
	}
	for _, row := range r.FilenameCandidates {
		m.add("filename_kind:"+row.Kind, row)
	}
	for _, row := range r.WorkspaceByKind {
		m.add("workspace_kind:"+row.Kind, row)
	}
	for _, row := range r.LocalDependencyByKind {
		m.add("local_kind:"+row.Kind, row)
	}
	rows := append([]assessment.LockfileEcosystem{r.LockfilesOverall}, r.Lockfiles...)
	for _, row := range rows {
		for name, value := range map[string]assessment.Metric{"projects": row.Projects, "eligible": row.Eligible, "covered": row.Covered, "missing": row.Missing, "not_applicable": row.NotApplicable, "unsupported": row.Unsupported, "unknown": row.Unknown} {
			add("lockfiles:"+key(row.Ecosystem, name), value)
		}
	}
	result["assessment"] = m
}
