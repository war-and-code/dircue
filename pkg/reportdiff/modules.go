package reportdiff

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"dircue/pkg/profile"
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
	// These original fields do not identify the classifier or complete source
	// selection policy. Numeric differences are report observations only.
	legacyComplete := len(p.Warnings) == 0
	languages := observedModule(true, "complete", "language_provider_and_selection_provenance_unavailable")
	languages.complete = legacyComplete
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
		m.complete = legacyComplete
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
			m.status, m.unsupported = "unavailable", true
			m.reasons = append(m.reasons, "detailed_comparison_not_supported_for_this_module")
		}
		result[name] = m
	}
	return result
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
