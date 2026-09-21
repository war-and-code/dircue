package explain

import (
	"strings"
	"testing"

	"dircue/pkg/availability"
	"dircue/pkg/declarations"
	"dircue/pkg/focus"
)

func retainedSource() Source {
	return Source{Mode: "unknown", Consistency: "not_retained", ReportSHA256: strings.Repeat("b", 64)}
}

func TestRetainedLanguageEvidenceDoesNotInferExclusion(t *testing.T) {
	base := RetainedEvidence{Source: retainedSource(), EvidenceMode: "retained", Languages: []LanguageEvidence{{Name: "Go", Files: []string{"main.go"}}}}
	withoutFiles, err := RetainedReport(base, Query{Kind: "language-path", Path: "main.go"})
	if err != nil {
		t.Fatal(err)
	}
	if withoutFiles.Decision.Status != "unavailable" || withoutFiles.Decision.Reason != "file_detail_not_retained" {
		t.Fatalf("unexpected absent-detail decision: %+v", withoutFiles.Decision)
	}
	base.FilesRetained = true
	included, err := RetainedReport(base, Query{Kind: "language-path", Path: "main.go"})
	if err != nil {
		t.Fatal(err)
	}
	if included.Decision.Status != "included" || included.Decision.ReportedLanguage != "Go" {
		t.Fatalf("retained observed inclusion lost: %+v", included.Decision)
	}
	unknown, err := RetainedReport(base, Query{Kind: "language-path", Path: "missing.go"})
	if err != nil {
		t.Fatal(err)
	}
	if unknown.Decision.Status != "unavailable" || unknown.Decision.Reason != "language_inclusion_not_retained" {
		t.Fatalf("absence was treated as exclusion: %+v", unknown.Decision)
	}
}

func TestRetainedLanguageEvidenceQualifiesConflictingMembership(t *testing.T) {
	result, err := RetainedReport(RetainedEvidence{
		Source: retainedSource(), EvidenceMode: "retained", FilesRetained: true,
		Languages: []LanguageEvidence{{Name: "Go", Files: []string{"main.go"}}, {Name: "Python", Files: []string{"main.go"}}},
	}, Query{Kind: "language-path", Path: "main.go"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Decision.Status != "unavailable" || result.Decision.Reason != "conflicting_retained_language_evidence" || result.Decision.ReportedLanguage != "" {
		t.Fatalf("conflicting membership chosen arbitrarily: %+v", result.Decision)
	}
}

func TestRetainedLanguageAddsMatchingBoundaryFactsWhenDecisionUnavailable(t *testing.T) {
	input := RetainedEvidence{
		Source: retainedSource(), EvidenceMode: "retained",
		Focus: &focus.Report{Status: "partial", Boundaries: []focus.Boundary{{Path: "src/missing.go", ProjectID: "go.mod", Reason: "ownership_unknown", Evidence: "go.mod"}}},
		Availability: &availability.Report{
			Status:   "partial",
			LFS:      []availability.LFSObservation{{Path: "src/missing.go", Kind: "pointer_like", LFSAttribute: "unknown", Reason: "metadata_unavailable"}},
			Gitlinks: []availability.Gitlink{{Path: "src/missing.go", Commit: strings.Repeat("c", 40)}},
			Sparse:   []availability.SparseIndication{{Path: "src/missing.go", Kind: "skip_worktree", Evidence: "src/missing.go"}},
		},
	}
	report, err := RetainedReport(input, Query{Kind: "language-path", Path: "src/missing.go"})
	if err != nil {
		t.Fatal(err)
	}
	if report.Decision.Status != "unavailable" || len(report.Facts) != 4 {
		t.Fatalf("boundary facts missing: decision=%+v facts=%+v", report.Decision, report.Facts)
	}
}

func TestEmbeddedExplanationRequiresSameQueryAndSource(t *testing.T) {
	embedded, err := LanguageReport(completeLanguageTrace())
	if err != nil {
		t.Fatal(err)
	}
	source := embedded.Source
	source.ReportSHA256 = strings.Repeat("d", 64)
	reused, err := RetainedReport(RetainedEvidence{Source: source, Explanation: embedded, EvidenceMode: "retained"}, embedded.Query)
	if err != nil {
		t.Fatal(err)
	}
	if reused.Scope.Evidence != "retained" || reused.Source.ReportSHA256 == "" {
		t.Fatalf("embedded explanation was not qualified: %+v", reused)
	}
	conflicting := source
	conflicting.Tree = strings.Repeat("e", 40)
	if _, err := RetainedReport(RetainedEvidence{Source: conflicting, Explanation: embedded, EvidenceMode: "retained"}, embedded.Query); err == nil {
		t.Fatal("accepted embedded explanation from a different tree")
	}
	different, err := RetainedReport(RetainedEvidence{Source: source, Explanation: embedded, FilesRetained: true, EvidenceMode: "retained"}, Query{Kind: "language-path", Path: "other.go"})
	if err != nil {
		t.Fatal(err)
	}
	if different.Decision.ReportedLanguage != "" || different.Decision.Status != "unavailable" {
		t.Fatalf("reused trace for different query: %+v", different.Decision)
	}
	malformed := *embedded
	malformed.Decision.Status = "reported"
	if _, err := RetainedReport(RetainedEvidence{Source: source, Explanation: &malformed, EvidenceMode: "retained"}, malformed.Query); err == nil {
		t.Fatal("reused malformed embedded explanation")
	}
}

func TestRepeatedRetainedExplanationBindsCurrentReportBytes(t *testing.T) {
	embedded, err := LanguageReport(completeLanguageTrace())
	if err != nil {
		t.Fatal(err)
	}
	firstDigest := strings.Repeat("1", 64)
	first, err := RetainedReport(RetainedEvidence{Source: embedded.Source, Explanation: embedded, EvidenceMode: "retained", ReportSHA256: firstDigest}, embedded.Query)
	if err != nil {
		t.Fatal(err)
	}
	if first.Source.ReportSHA256 != firstDigest {
		t.Fatalf("first retained digest=%q", first.Source.ReportSHA256)
	}
	secondDigest := strings.Repeat("2", 64)
	second, err := RetainedReport(RetainedEvidence{Source: first.Source, Explanation: first, EvidenceMode: "retained", ReportSHA256: secondDigest}, first.Query)
	if err != nil {
		t.Fatal(err)
	}
	if second.Source.ReportSHA256 != secondDigest {
		t.Fatalf("re-explanation retained stale digest=%q", second.Source.ReportSHA256)
	}
}

func TestDeclarationReportRetainsCandidateAndQualifiedFactsWithoutClaimingParse(t *testing.T) {
	report := &declarations.Report{
		Status: "partial", Source: "directory",
		Coverage: declarations.Coverage{OmittedFiles: 1},
		Projects: []declarations.Project{{
			ID: "app/app.csproj", Kind: "dotnet",
			Requirements: []declarations.Requirement{{Kind: "package", Value: "Example", State: "conditional", Evidence: "app/app.csproj", Condition: "Debug"}},
			References:   []declarations.Reference{{Kind: "project", Value: "../missing.csproj", Target: "missing.csproj", State: "declared", TargetStatus: "missing", Evidence: "app/app.csproj"}},
		}},
	}
	explanation, err := DeclarationReport(report, "app/app.csproj", "fresh")
	if err != nil {
		t.Fatal(err)
	}
	if explanation.Scope.Engine != "dircue declarations" || explanation.Scope.Inventory != "selected-source-metadata-walk" || explanation.Scope.Content != "bounded-manifest-reads" {
		t.Fatalf("fresh scope under-disclosed: %+v", explanation.Scope)
	}
	if len(explanation.Facts) != 3 || explanation.Facts[0].State == "parsed" {
		t.Fatalf("candidate eligibility invented: %+v", explanation.Facts)
	}
	if explanation.Status != "partial" {
		t.Fatalf("partial declaration evidence lost: %+v", explanation)
	}
	text, err := Text(explanation)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "extent: not retained for project facts") {
		t.Fatalf("project text claimed byte extent: %s", text)
	}
}

func TestRetainedProjectFactsAreCappedDuringCollection(t *testing.T) {
	project := declarations.Project{ID: "go.mod", Kind: "go"}
	for i := 0; i < MaxFacts*2; i++ {
		project.Requirements = append(project.Requirements, declarations.Requirement{Kind: "module", Value: "example", State: "declared", Evidence: "go.mod"})
	}
	result, err := RetainedReport(RetainedEvidence{Source: retainedSource(), EvidenceMode: "retained", Declarations: &declarations.Report{Status: "complete", Projects: []declarations.Project{project}}}, Query{Kind: "project", Path: "go.mod"})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Facts) > MaxFacts {
		t.Fatalf("facts exceed cap: %d", len(result.Facts))
	}
	foundLimit := false
	for _, omission := range result.Omissions {
		foundLimit = foundLimit || omission.Reason == "fact_limit"
	}
	if !foundLimit {
		t.Fatalf("fact cap not disclosed: %+v", result.Omissions)
	}
}

func TestRetainedAvailabilityFactUsesRecordedEvidence(t *testing.T) {
	result, err := RetainedReport(RetainedEvidence{
		Source: retainedSource(), EvidenceMode: "retained",
		Availability: &availability.Report{Status: "complete", References: []availability.ReferenceCorrelation{{
			MissingReference: availability.MissingReference{Project: "app.csproj", Kind: "project", Target: "missing.csproj", Evidence: "app.csproj"},
			Qualification:    "established_boundary", BoundaryKind: "gitlink", BoundaryPath: "missing.csproj",
		}}},
	}, Query{Kind: "project", Path: "app.csproj"})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Facts) != 1 || result.Facts[0].Evidence != "app.csproj" || result.Facts[0].Detail != "gitlink:missing.csproj" {
		t.Fatalf("availability provenance changed: %+v", result.Facts)
	}
}

func TestRetainedReportRejectsMalformedProjectCandidates(t *testing.T) {
	_, err := RetainedReport(RetainedEvidence{Source: retainedSource(), EvidenceMode: "retained", Declarations: &declarations.Report{Status: "complete", Projects: []declarations.Project{{ID: "go.mod", Kind: "go", Requirements: []declarations.Requirement{{Kind: "module", Value: "x", State: "declared", Evidence: "../outside"}}}}}}, Query{Kind: "project", Path: "go.mod"})
	if err == nil {
		t.Fatal("accepted malformed retained evidence path")
	}
}

func TestRetainedReportRejectsDuplicateProjectCandidates(t *testing.T) {
	project := declarations.Project{ID: "go.mod", Kind: "go"}
	_, err := RetainedReport(RetainedEvidence{Source: retainedSource(), EvidenceMode: "retained", Declarations: &declarations.Report{Status: "complete", Projects: []declarations.Project{project, project}}}, Query{Kind: "project", Path: "go.mod"})
	if err == nil {
		t.Fatal("accepted duplicate retained project candidates")
	}
}
