package lockfiles

import (
	"context"
	"strings"
	"testing"

	"github.com/war-and-code/dircue/pkg/declarations"
)

func validNPMReport(t *testing.T) *Report {
	t.Helper()
	manifest := `{"dependencies":{"left-pad":"1.0.0"}}`
	lock := `{"name":"fixture","lockfileVersion":3,"packages":{"":{"dependencies":{"left-pad":"1.0.0"}},"node_modules/left-pad":{"version":"1.0.0"}}}`
	files := map[string][]byte{
		"package.json":      []byte(manifest),
		"package-lock.json": []byte(lock),
	}
	project := declarations.Project{ID: "package.json", Root: ".", Kind: "npm"}
	r, err := Analyze(context.Background(), Input{
		Source: "directory", InventoryComplete: true,
		Inventory:      []File{{Path: "package.json", Size: int64(len(manifest))}, {Path: "package-lock.json", Size: int64(len(lock))}},
		ProjectRecords: []declarations.ProjectRecord{{Project: project, Parsed: true, Complete: true}},
		ReadSelected: func(_ context.Context, name string, _ int64) ([]byte, int64, error) {
			content := files[name]
			return content, int64(len(content)), nil
		},
	}, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	if r.Status != "complete" || len(r.Contexts) != 1 || len(r.Contexts[0].Checks) != 1 {
		t.Fatalf("fixture did not produce complete lockfile evidence: %+v", r)
	}
	return r
}

func TestValidateReportAcceptsAnalyzerOutput(t *testing.T) {
	r := validNPMReport(t)
	if err := ValidateReport(r); err != nil {
		t.Fatalf("valid analyzer output rejected: %v", err)
	}
}

func TestValidateReportRejectsInconsistentCompleteCoverage(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Report)
	}{
		{"indeterminate check", func(r *Report) { r.Contexts[0].Checks[0].Status = "indeterminate" }},
		{"indeterminate association", func(r *Report) { r.Contexts[0].AssociationState = "indeterminate" }},
		{"unsupported association", func(r *Report) { r.Contexts[0].AssociationState = "unsupported" }},
		{"omitted contexts", func(r *Report) { r.Coverage.OmittedContexts = 1 }},
		{"diagnostic", func(r *Report) {
			r.Diagnostics = append(r.Diagnostics, Diagnostic{Path: "package.json", Code: "unresolved", Message: "incomplete"})
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := validNPMReport(t)
			tt.mutate(r)
			if err := ValidateReport(r); err == nil {
				t.Fatal("inconsistent complete report was accepted")
			}
		})
	}
}

func TestValidateReportRejectsUnconfinedPathsAndImpossibleCounters(t *testing.T) {
	for _, mutate := range []func(*Report){
		func(r *Report) { r.Contexts[0].LockfilePath = "../package-lock.json" },
		func(r *Report) { r.Contexts[0].ManifestPath = "/package.json" },
		func(r *Report) { r.Coverage.LockfilesRead = r.Coverage.LockCandidates + 1 },
		func(r *Report) { r.Coverage.LockfilesRead = 0 },
		func(r *Report) { r.Coverage.InputBytes = r.Limits.InputBytes + 1 },
		func(r *Report) { r.Limits.Contexts = DefaultMaxContexts + 1 },
		func(r *Report) { r.Coverage.ProjectRecords = 0 },
		func(r *Report) {
			r.Contexts[0].Checks[0].Missing = []string{"not-a-difference"}
			r.Contexts[0].Checks[0].Status = "match"
		},
	} {
		r := validNPMReport(t)
		mutate(r)
		if err := ValidateReport(r); err == nil {
			t.Fatalf("invalid report accepted: %+v", r)
		}
	}
}

func TestValidateReportAcceptsPartialDiagnosticsWithoutPromotingCoverage(t *testing.T) {
	r := validNPMReport(t)
	r.Status = "partial"
	r.Coverage.OmittedFiles++
	r.Diagnostics = append(r.Diagnostics, Diagnostic{Path: "package.json", Code: "bounded-read", Message: strings.Repeat("x", 20)})
	if err := ValidateReport(r); err != nil {
		t.Fatalf("partial report should remain loadable: %v", err)
	}
}
