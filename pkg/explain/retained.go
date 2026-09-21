package explain

import (
	"errors"
	"fmt"
	"slices"

	"dircue/pkg/availability"
	"dircue/pkg/declarations"
	"dircue/pkg/focus"
)

type LanguageEvidence struct {
	Name  string
	Files []string
}

type RetainedEvidence struct {
	Source        Source
	Languages     []LanguageEvidence
	FilesRetained bool
	Declarations  *declarations.Report
	Focus         *focus.Report
	Availability  *availability.Report
	Explanation   *Report
	ReportSHA256  string
	EvidenceMode  string
}

// RetainedReport answers fixed queries from saved evidence only. A previously
// embedded explanation is reused only for the same query kind and path.
func RetainedReport(input RetainedEvidence, query Query) (*Report, error) {
	if input.EvidenceMode == "" {
		input.EvidenceMode = "retained"
	}
	if input.EvidenceMode != "fresh" && input.EvidenceMode != "retained" {
		return nil, errors.New("explanation evidence mode must be fresh or retained")
	}
	if input.ReportSHA256 != "" {
		input.Source.ReportSHA256 = input.ReportSHA256
	}
	if input.Explanation != nil && input.Explanation.Query == query {
		if err := ValidateReport(input.Explanation); err != nil {
			return nil, fmt.Errorf("embedded explanation is invalid: %w", err)
		}
		if !sameSourceIdentity(input.Explanation.Source, input.Source) {
			return nil, errors.New("embedded explanation source does not match retained report source")
		}
		return retainedExplanation(input.Explanation, input.Source), nil
	}
	switch query.Kind {
	case "language-path":
		return retainedLanguageReport(input, query.Path)
	case "project":
		return retainedProjectReport(input, query.Path)
	default:
		return nil, fmt.Errorf("unsupported explanation query %q", query.Kind)
	}
}

func retainedExplanation(original *Report, source Source) *Report {
	result := *original
	result.Source = source
	result.Scope.Evidence = "retained"
	result.Overrides = append([]Override{}, original.Overrides...)
	result.Facts = append([]Fact{}, original.Facts...)
	result.Steps = append([]Step{}, original.Steps...)
	result.Omissions = append([]Omission{}, original.Omissions...)
	result.ObservationID = observationID(&result)
	return &result
}

func sameSourceIdentity(a, b Source) bool {
	return a.Mode == b.Mode && a.Tree == b.Tree
}

func retainedLanguageReport(input RetainedEvidence, filename string) (*Report, error) {
	trace := LanguageTrace{
		Source: input.Source, Path: filename,
		Scope:     Scope{Module: "languages", Engine: "retained dircue report", Evidence: "retained", Inventory: "retained-report-only", Content: "not-retained"},
		Overrides: []Override{}, Facts: []Fact{}, Steps: []Step{}, Omissions: []Omission{},
	}
	addPathBoundaryFacts(&trace, input, filename)
	if !input.FilesRetained {
		trace.Decision = Decision{Status: "unavailable", Reason: "file_detail_not_retained"}
		trace.Omissions = append(trace.Omissions, Omission{Reason: "file_detail_not_retained", Count: 1})
		return LanguageReport(trace)
	}
	matchedLanguage := ""
	for _, language := range input.Languages {
		if slices.Contains(language.Files, filename) {
			if matchedLanguage != "" && matchedLanguage != language.Name {
				trace.Steps = append(trace.Steps, Step{Rule: "retained-language-file", Provider: "saved report", Outcome: "conflicting-language-membership", Evidence: filename})
				trace.Decision = Decision{Status: "unavailable", Reason: "conflicting_retained_language_evidence"}
				trace.Omissions = append(trace.Omissions, Omission{Reason: "conflicting_retained_language_evidence", Count: 1})
				return LanguageReport(trace)
			}
			matchedLanguage = language.Name
		}
	}
	if matchedLanguage != "" {
		trace.Steps = append(trace.Steps, Step{Rule: "retained-language-file", Provider: "saved report", Outcome: matchedLanguage, Evidence: filename})
		trace.Decision = Decision{Status: "included", Reason: "retained_language_file", ReportedLanguage: matchedLanguage}
		return LanguageReport(trace)
	}
	trace.Steps = append(trace.Steps, Step{Rule: "retained-language-file", Provider: "saved report", Outcome: "not-found-in-observed-inclusions", Evidence: "retained included-file lists cannot establish exclusion"})
	trace.Decision = Decision{Status: "unavailable", Reason: "language_inclusion_not_retained"}
	trace.Omissions = append(trace.Omissions, Omission{Reason: "exclusion_reason_not_retained", Count: 1})
	return LanguageReport(trace)
}

// DeclarationReport explains one project from the declaration facts already
// present in a fresh or retained report.
func DeclarationReport(report *declarations.Report, project, evidenceMode string) (*Report, error) {
	if report == nil {
		return nil, errors.New("declaration evidence is unavailable")
	}
	source := sourceFor(report.Source, report.Tree, evidenceMode, "")
	input := RetainedEvidence{Source: source, Declarations: report, EvidenceMode: evidenceMode}
	return retainedProjectReport(input, project)
}

func retainedProjectReport(input RetainedEvidence, project string) (*Report, error) {
	engine := "retained module facts"
	inventory := "retained-report-only"
	content := "manifest-facts-only"
	unavailableReason := "project_detail_not_retained"
	reportedReason := "retained_project_evidence"
	stepRule := "retained-project-facts"
	stepProvider := "dircue retained module reports"
	if input.EvidenceMode == "fresh" {
		engine = "dircue declarations"
		inventory = "selected-source-metadata-walk"
		content = "bounded-manifest-reads"
		unavailableReason = "project_not_reported"
		reportedReason = "fresh_project_evidence"
		stepRule = "project-facts"
		stepProvider = "dircue declarations"
	}
	trace := ProjectTrace{
		Source: input.Source, Project: project,
		Scope: Scope{Module: "project-evidence", Engine: engine, Evidence: input.EvidenceMode, Inventory: inventory, Content: content},
		Facts: []Fact{}, Steps: []Step{}, Omissions: []Omission{},
		Decision: Decision{Status: "unavailable", Reason: unavailableReason},
	}
	if input.Declarations != nil {
		if err := addDeclarationFacts(&trace, input.Declarations, project); err != nil {
			return nil, err
		}
	}
	if input.Focus != nil {
		addFocusFacts(&trace, input.Focus, project)
	}
	if input.Availability != nil {
		addAvailabilityFacts(&trace, input.Availability, project)
	}
	if len(trace.Facts) > 0 {
		trace.Decision = Decision{Status: "reported", Reason: reportedReason}
		trace.Steps = append(trace.Steps, Step{Rule: stepRule, Provider: stepProvider, Outcome: "reported", Evidence: fmt.Sprintf("%d bounded facts", len(trace.Facts))})
	} else {
		trace.Omissions = append(trace.Omissions, Omission{Reason: unavailableReason, Count: 1})
	}
	return ProjectReport(trace)
}

func addDeclarationFacts(trace *ProjectTrace, report *declarations.Report, project string) error {
	matches := 0
	for _, candidate := range report.Projects {
		if candidate.ID == project {
			matches++
		}
	}
	if matches > 1 {
		return errors.New("declaration evidence contains duplicate project candidates")
	}
	for _, candidate := range report.Projects {
		if candidate.ID != project {
			continue
		}
		appendProjectFact(trace, Fact{Kind: "project", State: "reported-candidate", Project: project, Evidence: project, Detail: candidate.Kind})
		for i, requirement := range candidate.Requirements {
			if len(trace.Facts) >= MaxFacts {
				trace.Omissions = appendOmission(trace.Omissions, "fact_limit", len(candidate.Requirements)-i+len(candidate.References)+len(candidate.Interfaces))
				return nil
			}
			appendProjectFact(trace, Fact{Kind: "requirement:" + requirement.Kind, State: requirement.State, Project: project, Target: requirement.Value, Evidence: requirement.Evidence, Condition: requirement.Condition})
		}
		for i, reference := range candidate.References {
			if len(trace.Facts) >= MaxFacts {
				trace.Omissions = appendOmission(trace.Omissions, "fact_limit", len(candidate.References)-i+len(candidate.Interfaces))
				return nil
			}
			target := reference.Target
			if target == "" {
				target = reference.Value
			}
			appendProjectFact(trace, Fact{Kind: "reference:" + reference.Kind, State: reference.State, Project: project, Target: target, Evidence: reference.Evidence, Condition: reference.Condition, Detail: reference.TargetStatus})
		}
		for i, iface := range candidate.Interfaces {
			if len(trace.Facts) >= MaxFacts {
				trace.Omissions = appendOmission(trace.Omissions, "fact_limit", len(candidate.Interfaces)-i)
				return nil
			}
			target := iface.Target
			if target == "" {
				target = iface.Name
			}
			appendProjectFact(trace, Fact{Kind: "interface:" + iface.Kind, State: iface.State, Project: project, Target: target, Evidence: iface.Evidence, Condition: iface.Condition})
		}
		break
	}
	if report.Status != "complete" || report.Coverage.OmittedFiles > 0 || report.Coverage.OmittedDiagnostics > 0 {
		trace.Omissions = append(trace.Omissions, Omission{Reason: "incomplete_declaration_evidence", Count: 1})
	}
	return nil
}

func addFocusFacts(trace *ProjectTrace, report *focus.Report, project string) {
	for i, context := range report.Context {
		if len(trace.Facts) >= MaxFacts {
			trace.Omissions = appendOmission(trace.Omissions, "fact_limit", len(report.Context)-i+len(report.Relations)+len(report.Boundaries))
			return
		}
		if context.ProjectID == project {
			appendProjectFact(trace, Fact{Kind: "focus-context:" + context.Kind, State: context.State, Project: project, Target: context.Path, Evidence: context.Evidence, Condition: context.Condition, Detail: context.Applicability})
		}
	}
	for i, relation := range report.Relations {
		if len(trace.Facts) >= MaxFacts {
			trace.Omissions = appendOmission(trace.Omissions, "fact_limit", len(report.Relations)-i+len(report.Boundaries))
			return
		}
		if relation.SourceProject == project {
			target := relation.Target
			if target == "" {
				target = relation.Value
			}
			appendProjectFact(trace, Fact{Kind: "focus-relation:" + relation.Kind, State: relation.State, Project: project, Target: target, Evidence: relation.Evidence, Condition: relation.Condition, Detail: relation.TargetStatus})
		}
	}
	for i, boundary := range report.Boundaries {
		if len(trace.Facts) >= MaxFacts {
			trace.Omissions = appendOmission(trace.Omissions, "fact_limit", len(report.Boundaries)-i)
			return
		}
		if boundary.ProjectID == project || boundary.Path == project {
			appendProjectFact(trace, Fact{Kind: "focus-boundary", State: "boundary", Project: project, Target: boundary.Path, Evidence: boundary.Evidence, Detail: boundary.Reason})
		}
	}
	if report.Status != "complete" {
		trace.Omissions = append(trace.Omissions, Omission{Reason: "incomplete_focus_evidence", Count: 1})
	}
}

func addAvailabilityFacts(trace *ProjectTrace, report *availability.Report, project string) {
	for i, reference := range report.References {
		if len(trace.Facts) >= MaxFacts {
			trace.Omissions = appendOmission(trace.Omissions, "fact_limit", len(report.References)-i)
			return
		}
		if reference.Project == project {
			detail := reference.BoundaryKind
			if reference.BoundaryPath != "" {
				detail += ":" + reference.BoundaryPath
			}
			appendProjectFact(trace, Fact{Kind: "availability-reference:" + reference.Kind, State: reference.Qualification, Project: project, Target: reference.Target, Evidence: reference.Evidence, Detail: detail})
		}
	}
	if report.Status != "complete" {
		trace.Omissions = append(trace.Omissions, Omission{Reason: "incomplete_availability_evidence", Count: 1})
	}
}

func appendProjectFact(trace *ProjectTrace, fact Fact) {
	if len(trace.Facts) < MaxFacts {
		trace.Facts = append(trace.Facts, fact)
		return
	}
	trace.Omissions = appendOmission(trace.Omissions, "fact_limit", 1)
}

func appendLanguageFact(trace *LanguageTrace, fact Fact) {
	if len(trace.Facts) < MaxFacts {
		trace.Facts = append(trace.Facts, fact)
		return
	}
	trace.Omissions = appendOmission(trace.Omissions, "fact_limit", 1)
}

func addPathBoundaryFacts(trace *LanguageTrace, input RetainedEvidence, filename string) {
	if input.Focus != nil {
		for i, boundary := range input.Focus.Boundaries {
			if len(trace.Facts) >= MaxFacts {
				trace.Omissions = appendOmission(trace.Omissions, "fact_limit", len(input.Focus.Boundaries)-i)
				return
			}
			if boundary.Path == filename {
				appendLanguageFact(trace, Fact{Kind: "focus-boundary", State: "boundary", Project: boundary.ProjectID, Target: filename, Evidence: boundary.Evidence, Detail: boundary.Reason})
			}
		}
	}
	if input.Availability == nil {
		return
	}
	for i, value := range input.Availability.LFS {
		if len(trace.Facts) >= MaxFacts {
			trace.Omissions = appendOmission(trace.Omissions, "fact_limit", len(input.Availability.LFS)-i+len(input.Availability.Gitlinks)+len(input.Availability.Sparse))
			return
		}
		if value.Path == filename {
			appendLanguageFact(trace, Fact{Kind: "availability-lfs:" + value.Kind, State: value.LFSAttribute, Target: filename, Evidence: filename, Detail: value.Reason})
		}
	}
	for i, value := range input.Availability.Gitlinks {
		if len(trace.Facts) >= MaxFacts {
			trace.Omissions = appendOmission(trace.Omissions, "fact_limit", len(input.Availability.Gitlinks)-i+len(input.Availability.Sparse))
			return
		}
		if value.Path == filename {
			appendLanguageFact(trace, Fact{Kind: "availability-gitlink", State: "boundary", Target: filename, Evidence: filename, Detail: value.Commit})
		}
	}
	for i, value := range input.Availability.Sparse {
		if len(trace.Facts) >= MaxFacts {
			trace.Omissions = appendOmission(trace.Omissions, "fact_limit", len(input.Availability.Sparse)-i)
			return
		}
		if value.Path == filename {
			appendLanguageFact(trace, Fact{Kind: "availability-sparse", State: "indicated", Target: filename, Evidence: value.Evidence, Detail: value.Kind})
		}
	}
}

func sourceFor(mode, tree, evidence, reportSHA string) Source {
	consistency := "live-directory-non-atomic"
	if mode == "git" {
		consistency = "immutable-selected-tree"
	}
	if evidence == "retained" {
		consistency = "retained-" + consistency
	}
	return Source{Mode: mode, Tree: tree, Consistency: consistency, ReportSHA256: reportSHA}
}
