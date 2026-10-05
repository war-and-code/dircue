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
	for _, value := range r.Metrics() {
		id := value.Name
		if value.Ecosystem != "" {
			id = "lockfiles:" + key(value.Ecosystem, value.Name)
		}
		add(id, value.Metric)
	}
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
	for _, row := range r.ProjectsByRole {
		m.add("project_role:"+row.Role, row)
	}
	for _, row := range r.ProjectRootsByRole {
		m.add("project_root_role:"+row.Role, row)
	}
	for _, row := range append([]assessment.LockfileEcosystem{r.LockfilesOverall}, r.Lockfiles...) {
		for _, role := range row.ByRole {
			m.add("lockfile_role:"+key(row.Ecosystem, role.Role), role)
		}
		for _, reason := range row.OutcomeReasons {
			m.add("lockfile_reason:"+key(row.Ecosystem, reason.State, reason.Reason), reason)
		}
	}
	result["assessment"] = m
}
