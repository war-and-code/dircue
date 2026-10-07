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
			switch value.Completeness {
			case "upper_bound":
				m.reasons = appendReason(m.reasons, "assessment_measurement_is_upper_bound")
			case "observed_only":
				m.reasons = appendReason(m.reasons, "assessment_measurement_is_observed_only")
			default:
				m.reasons = appendReason(m.reasons, "assessment_measurement_is_lower_bound")
			}
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
		// Check and presence partitions are exact totals. Causes are a bounded
		// sample, so they stay metadata like other retained examples.
		if row.Checks != nil {
			m.add("lockfile_checks:"+row.Ecosystem, row.Checks)
		}
		if row.NuGetPresence != nil {
			m.add("lockfile_presence:"+row.Ecosystem, row.NuGetPresence)
		}
		if len(row.Causes) > 0 || row.OmittedCauses > 0 {
			causes, _ := m.metadata["lockfile_causes"].(map[string]any)
			if causes == nil {
				causes = map[string]any{}
				m.metadata["lockfile_causes"] = causes
			}
			causes[row.Ecosystem] = map[string]any{"causes": row.Causes, "omitted_causes": row.OmittedCauses}
		}
	}
	if r.Structure != nil {
		addStructureComparison(&m, r.Structure)
	} else {
		m.complete = false
		m.observedOnly = true
		m.reasons = appendReason(m.reasons, "structural_assessment_not_measured")
	}
	result["assessment"] = m
}

func addStructureComparison(m *moduleData, structure *assessment.StructureReport) {
	// Metrics() already contributes the cross-ecosystem and ecosystem-role
	// structural population rows plus observed dependency graph totals.
	addExact := func(id string, count int64, scope string, complete bool, reasons []string) {
		metric := assessment.Metric{Count: count, Scope: scope, Completeness: "complete", Reasons: []string{}}
		if !complete {
			metric.Completeness = "lower_bound"
			metric.Reasons = reasons
		}
		// Structural scope qualification is attached to this scoped value. It
		// must not make unrelated assessment populations globally incomplete.
		m.add(id, metric)
	}
	coverageCompleteness := func(scope, ecosystem string) (bool, []string) {
		found := false
		complete := true
		reasons := []string{}
		for _, row := range structure.Coverage {
			if row.Scope != scope || ecosystem != "all" && row.Ecosystem != ecosystem {
				continue
			}
			found = true
			if row.Status != "complete" {
				complete = false
				reasons = append(reasons, row.Reasons...)
			}
		}
		if !found {
			return false, []string{"coverage_row_unavailable"}
		}
		return complete, sortedStrings(reasons)
	}
	for _, coverage := range structure.Coverage {
		id := "structure:coverage:" + key(coverage.Scope, coverage.Ecosystem)
		m.add(id, coverage)
		if coverage.Status != "complete" {
			m.reasons = appendReason(m.reasons, "structural_coverage_partial:"+coverage.Scope+":"+coverage.Ecosystem)
		}
	}
	// These are exact totals for the named retained observations. Coverage rows
	// above qualify what they mean; a partial observer must not make unrelated
	// project and inventory comparisons unavailable.
	complete, reasons := coverageCompleteness("workspace_membership", "all")
	addExact("structure:workspace_group_total", structure.WorkspaceGroupCount, "declared workspace/module groups retained by assessment", complete, reasons)
	complete, reasons = coverageCompleteness("solution_membership", "all")
	addExact("structure:solution_group_total", structure.SolutionGroupCount, "declared solution groups retained by assessment", complete, reasons)
	complete, reasons = coverageCompleteness("entry_points", "all")
	addExact("structure:entry_point_total", structure.EntryPointCount, "distinct entry-point declarations; observer coverage is reported separately", complete, reasons)
	addExact("structure:entry_point_association_total", structure.EntryPointAssociationCount, "entry-point rows with a named project; observer coverage is reported separately", complete, reasons)
	addExact("structure:entry_point_row_total", structure.EntryPointRowCount, "entry-point rows, including unassociated declarations; observer coverage is reported separately", complete, reasons)
	complete, reasons = coverageCompleteness("project_dependencies", "all")
	addExact("structure:qualified_reference_total", structure.Dependencies.QualifiedReferenceCount, "qualified dependency declarations in counted parsed projects", complete, reasons)
	// Group, entry-point, and qualified-reference rows are bounded examples.
	// Keep them available to reviewers as metadata, never as identities whose
	// disappearance can be inferred when a sample cap is crossed.
	m.metadata["structural_samples"] = map[string]any{
		"workspace_groups":                   structure.WorkspaceGroups,
		"omitted_workspace_groups":           structure.OmittedWorkspaceGroups,
		"solution_groups":                    structure.SolutionGroups,
		"omitted_solution_groups":            structure.OmittedSolutionGroups,
		"entry_points":                       structure.EntryPoints,
		"omitted_entry_points":               structure.OmittedEntryPoints,
		"qualified_references":               structure.Dependencies.QualifiedReferences,
		"omitted_qualified_reference_groups": structure.Dependencies.OmittedQualified,
		"omitted_qualified_reference_count":  structure.Dependencies.OmittedQualifiedReferenceCount,
	}
}
