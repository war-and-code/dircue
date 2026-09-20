package reportdiff

import "dircue/pkg/profile"

func contentModules(p profile.Report, result map[string]moduleData) {
	m := newModule()
	if r := p.Formats; r != nil {
		m.present, m.status = true, r.Status
		m.complete = r.Status == "complete" && r.Coverage.OmittedFiles == 0 && zeroMap(r.Omissions)
		if r.Coverage.SelectedFiles != r.Coverage.InspectedFiles || r.Coverage.InspectedFiles != r.Coverage.RetainedObservations || r.Coverage.RetainedObservations != int64(len(r.Observations)) || r.Coverage.CompleteReads > r.Coverage.InspectedFiles || r.Coverage.PrefixReads != r.Coverage.InspectedFiles-r.Coverage.CompleteReads {
			m.complete, m.observedOnly = false, true
			m.reasons = append(m.reasons, "format_population_or_evidence_incomplete")
		}
		m.policy = map[string]any{"provider": r.Provider, "provider_version": r.ProviderVersion, "source_mode": r.Source.Mode, "source_consistency": r.Source.Consistency, "scope": r.Scope, "limits": r.Limits}
		m.metadata = map[string]any{"tree": r.Source.Tree, "coverage": r.Coverage, "omissions": r.Omissions}
		if r.Provider == "" || r.ProviderVersion == "" || r.Scope.Population == "" || r.Scope.Selection == "" || r.Source.Consistency == "" || (r.Source.Mode == "git" && r.Source.Tree == "") {
			m.observedOnly, m.complete = true, false
			m.reasons = append(m.reasons, "format_provenance_incomplete")
		}
		for _, observation := range r.Observations {
			m.add(observation.Path, observation, observation.Path)
		}
	}
	result["formats"] = m

	m = newModule()
	if parent := p.Structure; parent != nil && parent.Hotspots != nil {
		r := parent.Hotspots
		m = observedModule(true, r.Status, "rankings_describe_measured_populations_not_function_identity_or_quality")
		// A missing language/metric cohort does not establish removal of its
		// functions, and top-N evidence cannot prove that a function disappeared.
		m.complete = false
		m.policy = map[string]any{"provider": r.Provider, "rule": r.Rule, "rule_version": r.RuleVersion, "scope": r.Scope, "population": r.Population, "histogram_rule": r.HistogramRule, "order": r.Order, "limit": r.Limit, "name_max_bytes": r.NameMaxBytes, "path_max_bytes": r.PathMaxBytes, "source": parent.Source, "max_file_bytes": parent.MaxFileBytes, "supported_languages": parent.SupportedLanguages}
		m.metadata = map[string]any{"tree": parent.Tree, "file_coverage_status": r.FileCoverageStatus, "analyzed_files": r.AnalyzedFiles, "recovered_files": r.RecoveredFiles, "total_spaces": r.TotalSpaces, "invalid_span_spaces": r.InvalidSpanSpaces, "omissions": r.Omissions}
		for _, group := range r.Groups {
			for _, metric := range group.Metrics {
				fields := object(metric)
				// Canonical comparison treats collections as multisets. Histogram
				// positions carry meaning, so preserve their indices explicitly.
				buckets := make([]map[string]any, len(metric.Histogram))
				for index, count := range metric.Histogram {
					buckets[index] = map[string]any{"bucket": index, "count": count}
				}
				fields["histogram"] = buckets
				fields["analyzed_files"] = group.AnalyzedFiles
				fields["total_spaces"] = group.TotalSpaces
				fields["invalid_span_spaces"] = group.InvalidSpanSpaces
				evidence := make([]string, 0, len(metric.Top))
				for _, entry := range metric.Top {
					if entry.Path != "" {
						evidence = append(evidence, entry.Path)
					}
				}
				m.add(key(group.Language, group.Grammar, group.SyntaxCohort, metric.Metric), fields, evidence...)
			}
		}
	}
	result["hotspots"] = m
}
