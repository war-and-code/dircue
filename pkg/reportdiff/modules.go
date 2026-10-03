package reportdiff

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/war-and-code/dircue/pkg/environments"
	"github.com/war-and-code/dircue/pkg/focus"
	"github.com/war-and-code/dircue/pkg/lockfiles"
	"github.com/war-and-code/dircue/pkg/profile"
)

type entity struct {
	fields   map[string]any
	evidence []string
}

type moduleData struct {
	present      bool
	status       string
	complete     bool
	observedOnly bool
	unsupported  bool
	reasons      []string
	policy       map[string]any
	metadata     map[string]any
	entities     map[string]entity
	duplicate    bool
}

func newModule() moduleData {
	return moduleData{status: "unavailable", policy: map[string]any{}, metadata: map[string]any{}, entities: map[string]entity{}, reasons: []string{}}
}

func (m *moduleData) add(id string, value any, evidence ...string) {
	if id == "" || len(id) > MaxStringBytes {
		m.duplicate = true
		return
	}
	if _, found := m.entities[id]; found {
		m.duplicate = true
		return
	}
	fields := object(value)
	evidence = append([]string{}, evidence...)
	slices.Sort(evidence)
	m.entities[id] = entity{fields: fields, evidence: slices.Compact(evidence)}
}

func object(value any) map[string]any {
	data, _ := json.Marshal(value)
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.UseNumber()
	result := map[string]any{}
	_ = decoder.Decode(&result)
	return result
}

func observedModule(present bool, status, reason string) moduleData {
	m := newModule()
	m.present, m.status = present, status
	m.observedOnly = true
	m.reasons = append(m.reasons, reason)
	return m
}

func moduleInputs(p profile.Report) map[string]moduleData {
	result := map[string]moduleData{}
	contentModules(p, result)
	// These original fields do not identify the classifier or complete source
	// selection policy. Numeric differences are report observations only.
	legacyComplete := len(p.Warnings) == 0
	languages := observedModule(true, "complete", "language_provider_and_selection_provenance_unavailable")
	// Empty legacy arrays do not distinguish an executed profiler with no
	// observations from a module-only command that did not run that profiler.
	// Language rows, analyzed-file counts, or language-byte totals establish
	// that the population ran. Warning-free reports can then prove absence even
	// though their provider/version provenance still limits attribution.
	languages.complete = legacyComplete && legacyLanguagePopulationInspected(p)
	languages.metadata["warnings"] = p.Warnings
	for _, language := range p.Languages {
		fields := object(language)
		fields["population"] = "included_language_statistics"
		fields["denominator_language_bytes"] = p.Summary.LanguageBytes
		languages.add(language.Name, fields, language.Files...)
	}
	result["languages"] = languages
	summary := observedModule(true, "complete", "summary_source_and_selection_provenance_unavailable")
	summary.complete = legacyComplete
	summary.metadata["warnings"] = p.Warnings
	summary.add("inventory", p.Summary)
	result["summary"] = summary
	for name, findings := range map[string][]profile.Finding{"ecosystems": p.Ecosystems, "frameworks": p.Frameworks, "layouts": p.Layouts} {
		m := observedModule(true, "complete", "finding_provider_version_and_selection_provenance_unavailable")
		m.complete = false
		for _, finding := range findings {
			m.add(key(finding.Kind, finding.Name, finding.Root, finding.Detector), finding, finding.Evidence...)
		}
		result[name] = m
	}

	m := newModule()
	if r := p.Declarations; r != nil {
		m.present, m.status = true, r.Status
		m.complete = r.Status == "complete" && r.Coverage.OmittedFiles == 0 && r.Coverage.OmittedDiagnostics == 0 && len(r.Diagnostics) == 0
		m.policy = map[string]any{"provider": r.Provider, "provider_version": r.ProviderVersion, "source": r.Source, "selection": r.Selection, "supported_ecosystems": r.SupportedEcosystems, "limits": r.Limits}
		m.metadata = map[string]any{"tree": r.Tree, "coverage": r.Coverage, "diagnostics": r.Diagnostics}
		if r.Provider == "" || r.ProviderVersion == "" || r.Selection == "" || len(r.SupportedEcosystems) == 0 {
			m.observedOnly = true
			m.complete = false
			m.reasons = append(m.reasons, "declaration_provenance_incomplete")
		}
		if r.Source == "git" && r.Tree == "" {
			m.observedOnly = true
			m.complete = false
			m.reasons = append(m.reasons, "selected_git_tree_unavailable")
		}
		for _, project := range r.Projects {
			m.add(project.ID, project, project.ID)
		}
	}
	result["declarations"] = m

	m = newModule()
	if r := p.Projects; r != nil {
		m = observedModule(true, r.Status, "project_parser_and_selection_versions_unavailable")
		m.complete = r.Status == "complete" && r.OmittedFiles == 0 && len(r.Diagnostics) == 0
		m.policy = map[string]any{"source": r.Source, "attribution": r.Attribution, "max_manifest_bytes": r.MaxManifestBytes}
		m.metadata = map[string]any{"tree": r.Tree, "omitted_files": r.OmittedFiles, "diagnostics": r.Diagnostics}
		for _, project := range r.Projects {
			m.add("project:"+project.ID, project, project.Evidence...)
		}
		for _, config := range r.Configurations {
			m.add("configuration:"+config.Path, config, config.Path)
		}
		for _, role := range r.Composition {
			m.add("composition:"+key(role.Name, role.Basis), role)
		}
		m.add("unassigned", r.Unassigned)
		m.add("ambiguous", r.Ambiguous)
	}
	result["projects"] = m

	m = newModule()
	if r := p.Discovery; r != nil {
		m.present, m.status = true, r.Status
		m.complete = r.Status == "complete" && zeroMap(r.Omissions) && zeroMap(r.OmittedCandidates)
		m.policy = map[string]any{"engine": r.Engine, "rule_version": r.RuleVersion, "linguist_data_commit": r.LinguistDataCommit, "source_mode": r.Source.Mode, "source_consistency": r.Source.Consistency, "scope": r.Scope}
		m.metadata = map[string]any{"tree": r.Source.Tree, "omissions": r.Omissions, "omitted_candidates": r.OmittedCandidates}
		m.metadata["classification_bytes_read"] = r.ClassificationBytesRead
		if r.Engine == "" || r.RuleVersion == "" || r.LinguistDataCommit == "" || r.Source.Consistency == "" || r.Scope.Population == "" || r.Scope.ContentInspection == "" || r.Scope.Attributes == "" || (r.Source.Mode == "git" && r.Source.Tree == "") {
			m.observedOnly, m.complete = true, false
			m.reasons = append(m.reasons, "discovery_provenance_incomplete")
		}
		m.add("inventory", r.Inventory)
		for _, group := range r.Categories {
			m.add("category:"+key(group.Name, group.Basis), group)
		}
		for _, group := range r.Roles {
			m.add("role:"+key(group.Name, group.Basis), group)
		}
		for _, group := range r.CandidateCounts {
			m.add("candidate-count:"+key(group.Name, group.Basis), group)
		}
		for _, candidate := range r.Candidates {
			m.add("candidate:"+key(candidate.Path, candidate.Kind, candidate.Format), candidate, candidate.Path)
		}
	}
	result["discovery"] = m

	m = newModule()
	if r := p.Registries; r != nil {
		m.present, m.status = true, r.Status
		m.complete = r.Status == "complete" && r.Coverage.EnumerationComplete && zeroMap(r.Omissions)
		m.policy = map[string]any{"engine": r.Engine, "rule_version": r.RuleVersion, "source_mode": r.Source.Mode, "source_consistency": r.Source.Consistency, "scope": r.Scope}
		m.metadata = map[string]any{"tree": r.Source.Tree, "coverage": r.Coverage, "omissions": r.Omissions}
		if r.Engine == "" || r.RuleVersion == "" || r.Source.Consistency == "" || r.Scope.Population == "" || r.Scope.Evaluation == "" || len(r.Scope.SupportedConfigurations) == 0 || (r.Source.Mode == "git" && r.Source.Tree == "") {
			m.observedOnly, m.complete = true, false
			m.reasons = append(m.reasons, "registry_provenance_incomplete")
		}
		for _, config := range r.Configurations {
			if config.Status != "complete" || !config.DeclarationCountComplete || config.OmittedDeclarations > 0 {
				m.complete = false
			}
			m.add(key(config.Path, config.Ecosystem), config, config.Path)
		}
	}
	result["registries"] = m

	m = newModule()
	if r := p.PackageEvidence; r != nil {
		m = observedModule(true, r.Status, "package_import_observations_do_not_prove_provider_scan_completeness")
		m.complete = r.Status == "complete" && r.Coverage.Import == "complete" && r.Coverage.ProviderScan == "complete" && r.Coverage.Snapshot == "verified"
		m.policy = map[string]any{"provider": r.Provider, "source_type": r.Source.Type, "limits": r.Limits}
		m.metadata = map[string]any{"coverage": r.Coverage, "source": r.Source, "report_sha256": r.ReportSHA256, "diagnostics": r.Diagnostics}
		for _, pkg := range r.Packages {
			evidence := []string{}
			for _, location := range pkg.Locations {
				if location.Path != "" {
					evidence = append(evidence, location.Path)
				}
			}
			m.add("package:"+pkg.ID, pkg, evidence...)
		}
		for _, relationship := range r.Relationships {
			m.add("relationship:"+key(relationship.Parent, relationship.Child, relationship.Type, relationship.ParentKind, relationship.ChildKind), relationship)
		}
		for _, file := range r.Files {
			evidence := []string{}
			if file.Location.Path != "" {
				evidence = append(evidence, file.Location.Path)
			}
			m.add("file:"+file.ID, file, evidence...)
		}
	}
	result["package_evidence"] = m

	m = newModule()
	if r := p.Metrics; r != nil {
		m = observedModule(true, r.Status, "metrics_classifier_and_full_selection_provenance_unavailable")
		m.complete = r.Status == "complete" && len(r.Skipped) == 0
		m.policy = map[string]any{"engine": r.Engine, "engine_version": r.EngineVersion, "source": r.Source, "scope": r.Scope, "max_file_bytes": r.MaxFileBytes}
		m.metadata = map[string]any{"tree": r.Tree, "skipped": r.Skipped, "file_details_included": r.Files != nil}
		m.add("totals", r.Totals)
		for _, language := range r.Languages {
			fields := object(language)
			fields["population"] = r.Scope
			fields["denominator_files"] = r.Totals.Files
			fields["denominator_lines"] = r.Totals.Lines
			m.add("language:"+key(language.Language, language.Grammar), fields)
		}
		for _, directory := range r.Directories {
			m.add("directory:"+directory.Path, directory, directory.Path)
		}
	}
	result["metrics"] = m
	m = newModule()
	if r := p.Metrics; r != nil && r.Files != nil {
		m = observedModule(true, r.Status, "metrics_classifier_and_full_selection_provenance_unavailable")
		m.complete = r.Status == "complete" && len(r.Skipped) == 0
		m.policy = map[string]any{"engine": r.Engine, "engine_version": r.EngineVersion, "source": r.Source, "scope": r.Scope, "max_file_bytes": r.MaxFileBytes}
		m.metadata = map[string]any{"tree": r.Tree, "skipped": r.Skipped}
		for _, file := range *r.Files {
			m.add(file.Path, file, file.Path)
		}
	}
	result["metrics_files"] = m

	m = newModule()
	if r := p.Rules; r != nil {
		m.present, m.status = true, r.Status
		m.complete = r.Status == "complete" && r.OmittedMatches == 0 && r.ContentOmittedFiles == 0 && len(r.Omissions) == 0
		m.policy = map[string]any{"provider": r.Provider, "evaluator_version": r.EvaluatorVersion, "rules_schema_version": r.RulesSchemaVersion, "rules_sha256": r.RulesSHA256, "source_kind": r.Source.Kind, "source_consistency": r.SourceConsistency, "scope": r.Scope, "order": r.Order, "limits": r.Limits}
		m.metadata = map[string]any{"tree": r.Source.Tree, "omitted_matches": r.OmittedMatches, "content_omitted_files": r.ContentOmittedFiles, "omissions": r.Omissions}
		m.metadata["inventory_files"] = r.InventoryFiles
		m.metadata["candidate_files"] = r.CandidateFiles
		m.metadata["content_candidate_files"] = r.ContentCandidateFiles
		m.metadata["content_eligible_files"] = r.ContentEligibleFiles
		m.metadata["content_admitted_files"] = r.ContentAdmittedFiles
		m.metadata["content_evaluated_files"] = r.ContentEvaluatedFiles
		m.metadata["total_matches"] = r.TotalMatches
		if r.Provider == "" || r.EvaluatorVersion == "" || r.RulesSchemaVersion == "" || r.RulesSHA256 == "" || r.SourceConsistency == "" || r.Scope == "" || (r.Source.Kind == "git" && r.Source.Tree == "") {
			m.observedOnly, m.complete = true, false
			m.reasons = append(m.reasons, "rule_provenance_incomplete")
		}
		for _, rule := range r.Rules {
			m.add("rule:"+rule.ID, rule)
		}
		for _, observation := range r.Observations {
			m.add("observation:"+key(observation.RuleID, observation.Path, observation.Evidence), observation, observation.Path)
		}
	}
	result["rules"] = m

	for name, present := range map[string]bool{"graph": p.Graph != nil, "structure": p.Structure != nil} {
		m := newModule()
		m.present = present
		if present {
			m.unsupported = true
			if name == "graph" {
				m.status = p.Graph.Status
			} else {
				m.status = p.Structure.Status
			}
			m.reasons = append(m.reasons, "detailed_comparison_not_supported_for_this_module")
		}
		result[name] = m
	}
	targetedModules(p, result)
	return result
}

func targetedModules(p profile.Report, result map[string]moduleData) {
	if p.Focus != nil {
		r := p.Focus
		policy := map[string]any{"provider": r.Provider, "provider_version": r.ProviderVersion, "source": r.Source, "role": r.Scope.Role, "rule": r.Scope.Rule, "primary_project": r.Scope.PrimaryProject, "related_projects": sortedStrings(r.Scope.RelatedProjects), "affected_by": r.Scope.AffectedBy}
		metadata := map[string]any{"tree": r.Tree, "scope_id": r.Scope.ID, "coverage": r.Coverage, "limits": r.Limits, "boundaries": r.Boundaries, "omissions": r.Omissions}
		complete := focusComplete(r)
		primary := scopedModule(r.Status, complete, policy, metadata)
		if r.PrimaryProject != nil {
			primary.add("project:"+r.PrimaryProject.ID, *r.PrimaryProject, r.PrimaryProject.ID)
		}
		for _, f := range r.Primary {
			primary.add("file:"+f.Path, f, f.Path)
		}
		result["focus_primary"] = primary
		related := scopedModule(r.Status, complete, policy, metadata)
		for _, population := range r.Related {
			related.add("project:"+population.Project.ID, population.Project, population.Project.ID)
			for _, f := range population.Files {
				related.add("file:"+key(population.Project.ID, f.Path), f, f.Path)
			}
		}
		result["focus_related"] = related
		context := scopedModule(r.Status, complete && r.Coverage.OmittedRecords == 0, policy, metadata)
		for _, item := range r.Context {
			context.add(key(item.ProjectID, item.Path, item.Kind, item.Basis, item.Evidence), item, item.Path, item.Evidence)
		}
		result["focus_context"] = context
		relations := scopedModule(r.Status, complete && r.Coverage.OmittedRelations == 0, policy, metadata)
		for _, item := range r.Relations {
			relations.add(key(item.SourceProject, item.Target, item.Kind, item.Class, item.Value, item.Evidence), item, item.Evidence)
		}
		result["focus_relations"] = relations
		if r.AffectedProjects != nil {
			query := scopedModule(r.AffectedProjects.Status, complete && r.AffectedProjects.Omitted == 0, policy, metadata)
			query.metadata["path"] = r.AffectedProjects.Path
			query.metadata["omitted"] = r.AffectedProjects.Omitted
			for _, item := range r.AffectedProjects.Projects {
				query.add(key(item.ProjectID, item.Basis, item.Applicability, item.Evidence), item, item.Evidence)
			}
			result["focus_affected_projects"] = query
		}
		qualifyLegacyPopulation(result, "repository_population_not_inspected_by_focused_report")
	}
	if p.FocusedMetrics != nil && p.Focus != nil {
		policy := map[string]any{"provider": p.Focus.Provider, "provider_version": p.Focus.ProviderVersion, "source": p.Focus.Source, "role": p.Focus.Scope.Role, "rule": p.Focus.Scope.Rule, "primary_project": p.Focus.Scope.PrimaryProject, "related_projects": sortedStrings(p.Focus.Scope.RelatedProjects), "metrics": metricsPolicy(p.FocusedMetrics.Primary)}
		metadata := map[string]any{"tree": p.Focus.Tree, "scope_id": p.FocusedMetrics.ScopeID}
		parentComplete := focusComplete(p.Focus)
		result["focused_metrics_primary"] = focusedMetricsModule(p.FocusedMetrics.Primary, parentComplete, policy, metadata, "primary")
		relatedContracts := map[string]any{}
		for _, item := range p.FocusedMetrics.Related {
			relatedContracts[item.Project] = metricsPolicy(item.Metrics)
		}
		relatedPolicy := cloneMap(policy)
		relatedPolicy["metrics_by_project"] = relatedContracts
		delete(relatedPolicy, "metrics")
		m := scopedModule("complete", parentComplete, relatedPolicy, metadata)
		for _, item := range p.FocusedMetrics.Related {
			addMetrics(&m, item.Metrics, "project:"+item.Project+":")
			if item.Metrics.Status != "complete" || len(item.Metrics.Skipped) != 0 {
				m.complete, m.observedOnly = false, true
			}
		}
		result["focused_metrics_related"] = m
	}
	if p.Availability != nil {
		availabilityModules(p, result)
	}
	if p.Explanation != nil {
		m := newModule()
		m.present, m.status, m.unsupported = true, p.Explanation.Status, true
		m.reasons = append(m.reasons, "semantic_trace_comparison_not_supported")
		result["explanation"] = m
	}
	if p.Environments != nil {
		result["environments"] = environmentModule(p.Environments)
		if p.Focus == nil && p.Formats == nil && p.Registries == nil && p.Rules == nil && p.PackageEvidence == nil && p.Discovery == nil && p.Graph == nil && p.Projects == nil && p.Structure == nil && p.Metrics == nil && legacyPopulationEmpty(p) {
			qualifyLegacyPopulation(result, "repository_population_not_inspected_by_environment_report")
		}
	}
	if p.Lockfiles != nil {
		result["lockfiles"] = lockfilesModule(p.Lockfiles)
	}
	if p.Focus == nil && (p.Availability != nil || p.Explanation != nil) && p.Formats == nil && p.Declarations == nil && p.Registries == nil && p.Rules == nil && p.PackageEvidence == nil && p.Discovery == nil && p.Graph == nil && p.Projects == nil && p.Structure == nil && p.Metrics == nil {
		qualifyLegacyPopulation(result, "repository_population_not_inspected_by_standalone_targeted_report")
	}
}

func environmentModule(r *environments.Report) moduleData {
	m := newModule()
	m.present, m.status, m.observedOnly = true, r.Status, true
	m.policy = map[string]any{"provider": r.Provider}
	m.metadata = map[string]any{
		"provider_version": r.ProviderVersion, "semantics_reference": r.SemanticsReference,
		"source": r.Source, "tree": r.Tree, "limits": r.Limits, "coverage": r.Coverage,
		"diagnostics": r.Diagnostics,
	}
	m.complete = r.Status == "complete" && r.Coverage.OmittedFiles == 0 && r.Coverage.OmittedRequirements == 0 &&
		r.Coverage.OmittedToolchainFiles == 0 && !environmentsHasNonInformationalDiagnostic(r.Diagnostics)
	m.reasons = append(m.reasons, "environment declarations are observations; installed versions and project applicability were not evaluated")
	for _, item := range r.Requirements {
		if item.State == "unresolved" {
			m.complete = false
		}
		m.add("requirement:"+key(item.ProjectID, item.ContextID, item.Dimension, item.Kind, item.Value, item.Condition), item, item.Evidence)
	}
	for _, item := range r.Selections {
		if item.State == "unresolved" {
			m.complete = false
		}
		m.add("selection:"+key(item.ContextID, item.ProjectID, item.StartDirectory, item.StartBasis), item, nonemptyEvidence(item.GlobalJSON)...)
	}
	for _, item := range r.ToolchainDeclarations {
		if item.State != "declared" {
			m.complete = false
		}
		m.add("toolchain:"+key(item.SourcePath, item.Tool, item.Kind, item.ScopeDirectory), item, item.SourcePath)
	}
	for _, item := range r.Conflicts {
		evidence := sortedStrings(item.Evidence)
		m.add("conflict:"+key(item.ContextID, item.Dimension, strings.Join(evidence, "\x00")), item, evidence...)
	}
	for _, item := range r.Boundaries {
		m.add("boundary:"+key(item.Reason, item.Path, item.ProjectID, item.ContextID), item, item.Path)
	}
	for _, item := range r.Diagnostics {
		m.add("diagnostic:"+key(item.Path, item.Code, item.Message), item, item.Path)
	}
	return m
}

func environmentsHasNonInformationalDiagnostic(diagnostics []environments.Diagnostic) bool {
	for _, diagnostic := range diagnostics {
		if diagnostic.Code != "global-json-lenient-syntax" {
			return true
		}
	}
	return false
}

func lockfilesModule(r *lockfiles.Report) moduleData {
	m := newModule()
	m.present, m.status, m.observedOnly = true, r.Status, true
	m.policy = map[string]any{"provider": r.Provider, "provider_version": r.ProviderVersion, "semantics": r.Semantics, "limits": r.Limits}
	m.metadata = map[string]any{"source": r.Source, "tree": r.Tree, "coverage": r.Coverage, "diagnostics": r.Diagnostics}
	m.complete = r.Status == "complete" && r.Coverage.OmittedFiles == 0 && r.Coverage.OmittedContexts == 0 && len(r.Diagnostics) == 0
	m.reasons = append(m.reasons, "lockfile checks cover named static subsets; resolved dependency graphs and restore success were not evaluated")
	for _, item := range r.Contexts {
		if item.AssociationState == "indeterminate" || item.AssociationState == "unsupported" {
			m.complete = false
		}
		for _, check := range item.Checks {
			if check.Status == "indeterminate" {
				m.complete = false
			}
		}
		m.add("context:"+item.ProjectID, item, nonemptyEvidence(item.ManifestPath, item.LockfilePath)...)
	}
	for _, item := range r.Diagnostics {
		m.add("diagnostic:"+key(item.Path, item.Code, item.Message), item, item.Path)
	}
	return m
}

func legacyPopulationEmpty(p profile.Report) bool {
	return len(p.Languages) == 0 && len(p.Ecosystems) == 0 && len(p.Frameworks) == 0 && len(p.Layouts) == 0 && p.Summary.AnalyzedFiles == 0 && p.Summary.LanguageBytes == 0
}

func legacyLanguagePopulationInspected(p profile.Report) bool {
	// Module-only scans still enumerate paths as scanned/skipped, so those
	// counters do not prove that language classification ran.
	return len(p.Languages) > 0 || p.Summary.AnalyzedFiles > 0 || p.Summary.LanguageBytes > 0
}

func qualifyLegacyPopulation(result map[string]moduleData, reason string) {
	for _, name := range []string{"languages", "summary", "ecosystems", "frameworks", "layouts", "metrics", "metrics_files"} {
		m := result[name]
		m.unsupported, m.complete = true, false
		m.reasons = append(m.reasons, reason)
		result[name] = m
	}
}

func scopedModule(status string, complete bool, policy, metadata map[string]any) moduleData {
	m := newModule()
	m.present, m.status, m.complete = true, status, complete
	m.policy, m.metadata = policy, metadata
	if !complete {
		m.observedOnly = true
	}
	return m
}

func focusComplete(r *focus.Report) bool {
	c := r.Coverage
	return r.Status == "complete" && c.OmittedFiles == 0 && c.OmittedProjects == 0 && c.OmittedRecords == 0 && c.OmittedRelations == 0 && c.OmittedOutputRecords == 0 && c.AmbiguousFiles == 0 && c.UnresolvedFiles == 0 && len(r.Omissions) == 0
}

func focusedMetricsModule(r *profile.MetricsReport, parentComplete bool, policy, metadata map[string]any, prefix string) moduleData {
	if r == nil {
		return newModule()
	}
	m := scopedModule(r.Status, parentComplete && r.Status == "complete" && len(r.Skipped) == 0, policy, metadata)
	addMetrics(&m, r, prefix+":")
	return m
}

func metricsPolicy(r *profile.MetricsReport) map[string]any {
	if r == nil {
		return map[string]any{"present": false}
	}
	return map[string]any{"present": true, "engine": r.Engine, "engine_version": r.EngineVersion, "source": r.Source, "scope": r.Scope, "max_file_bytes": r.MaxFileBytes, "file_details_included": r.Files != nil}
}

func cloneMap(in map[string]any) map[string]any {
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func addMetrics(m *moduleData, r *profile.MetricsReport, prefix string) {
	m.add(prefix+"totals", r.Totals)
	for _, language := range r.Languages {
		m.add(prefix+"language:"+key(language.Language, language.Grammar), language)
	}
	for _, directory := range r.Directories {
		m.add(prefix+"directory:"+directory.Path, directory, directory.Path)
	}
	if r.Files != nil {
		for _, file := range *r.Files {
			m.add(prefix+"file:"+file.Path, file, file.Path)
		}
	}
}

func sortedStrings(in []string) []string {
	out := append([]string{}, in...)
	slices.Sort(out)
	return out
}

func nonemptyEvidence(values ...string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value != "" {
			result = append(result, value)
		}
	}
	return result
}

func availabilityModules(p profile.Report, result map[string]moduleData) {
	r := p.Availability
	policy := map[string]any{"provider": r.Provider, "provider_version": r.ProviderVersion, "source_mode": r.Source.Mode, "source_consistency": r.Source.Consistency, "checkout_metadata": r.Source.CheckoutMetadata, "bounds": r.Bounds}
	metadata := map[string]any{"tree": r.Source.Tree, "coverage": r.Coverage, "counts": r.Counts, "omissions": r.Omissions}
	baseComplete := r.Status == "complete" && r.Coverage.SelectedInventoryComplete && r.Coverage.OmittedEvidence == 0 && r.Coverage.OmittedDiagnostics == 0
	makeModule := func(complete bool) moduleData { return scopedModule(r.Status, complete, policy, metadata) }
	lfs := makeModule(baseComplete && r.Coverage.OmittedPointerFiles == 0)
	for _, item := range r.LFS {
		lfs.add(item.Path, item, item.Path)
	}
	result["availability_lfs"] = lfs
	boundariesComplete := baseComplete && r.Coverage.OmittedBoundaryPaths == 0
	gitlinks := makeModule(boundariesComplete)
	for _, item := range r.Gitlinks {
		gitlinks.add(item.Path, item, item.Path)
	}
	result["availability_gitlinks"] = gitlinks
	submodules := makeModule(boundariesComplete)
	for _, item := range r.Submodules {
		submodules.add(item.Path, item, item.Path, item.Evidence)
	}
	result["availability_submodules"] = submodules
	sparseComplete := r.Source.Mode == "directory" && r.Coverage.CheckoutMetadataInspected && r.Coverage.CheckoutMetadataComplete && r.Coverage.OmittedBoundaryPaths == 0 && r.Coverage.OmittedEvidence == 0
	sparse := makeModule(sparseComplete)
	if r.Source.Mode == "git" {
		sparse.status, sparse.unsupported = "unavailable", true
		sparse.reasons = append(sparse.reasons, "checkout_metadata_not_inspected_for_git_tree")
	}
	for _, item := range r.Sparse {
		sparse.add(key(item.Kind, item.Path, item.Evidence), item, item.Path, item.Evidence)
	}
	result["availability_sparse"] = sparse
	declarationsComplete := p.Declarations != nil && p.Declarations.Status == "complete" && p.Declarations.Coverage.OmittedFiles == 0 && p.Declarations.Coverage.OmittedDiagnostics == 0 && len(p.Declarations.Diagnostics) == 0
	referencePolicy := cloneMap(policy)
	referencePolicy["declaration_input"] = declarationInputPolicy(p)
	references := makeModule(boundariesComplete && r.Coverage.OmittedCorrelations == 0 && declarationsComplete)
	references.policy = referencePolicy
	if !declarationsComplete {
		references.reasons = append(references.reasons, "retained_declaration_coverage_required_for_reference_absence")
	}
	for _, item := range r.References {
		references.add(key(item.Project, item.Kind, item.Target, item.Evidence), item, item.Evidence, item.BoundaryPath)
	}
	result["availability_references"] = references
	diagnostics := makeModule(false)
	for _, item := range r.Diagnostics {
		diagnostics.add(key(item.Path, item.Code, item.Message), item, item.Path)
	}
	result["availability_diagnostics"] = diagnostics
}

func declarationInputPolicy(p profile.Report) map[string]any {
	if p.Declarations == nil {
		return map[string]any{"retained": false}
	}
	return map[string]any{"retained": true, "provider": p.Declarations.Provider, "provider_version": p.Declarations.ProviderVersion, "source": p.Declarations.Source, "selection": p.Declarations.Selection}
}

func zeroMap(m map[string]int64) bool {
	for _, n := range m {
		if n != 0 {
			return false
		}
	}
	return true
}

func key(fields ...string) string {
	// Length prefixes prevent separator text inside a declared ID from merging
	// two distinct identities. They do not infer renames or package equivalence.
	var result strings.Builder
	for _, field := range fields {
		fmt.Fprintf(&result, "%d:%s", len(field), field)
	}
	return result.String()
}
