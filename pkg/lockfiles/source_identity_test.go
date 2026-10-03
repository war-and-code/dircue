package lockfiles

import (
	"context"
	"strings"
	"testing"

	"github.com/war-and-code/dircue/pkg/declarations"
)

func TestAnalyzeValidatesSourceIdentityBeforeProducingReport(t *testing.T) {
	for _, tc := range []struct {
		name  string
		input Input
	}{
		{name: "directory", input: Input{Source: "directory", InventoryComplete: true}},
		{name: "git", input: Input{Source: "git", Tree: strings.Repeat("a", 40), InventoryComplete: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			report, err := Analyze(context.Background(), tc.input, Limits{})
			if err != nil {
				t.Fatal(err)
			}
			if err := ValidateReport(report); err != nil {
				t.Fatalf("valid source identity produced an invalid report: %v", err)
			}
		})
	}

	for _, tc := range []struct {
		name  string
		input Input
	}{
		{name: "unknown source", input: Input{Source: "other"}},
		{name: "git missing tree", input: Input{Source: "git"}},
		{name: "git wrong tree length", input: Input{Source: "git", Tree: strings.Repeat("a", 64)}},
		{name: "git non-hex tree", input: Input{Source: "git", Tree: strings.Repeat("g", 40)}},
		{name: "directory with tree", input: Input{Source: "directory", Tree: strings.Repeat("a", 40)}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			report, err := Analyze(context.Background(), tc.input, Limits{})
			if err == nil || report != nil {
				t.Fatalf("invalid source identity returned report=%+v err=%v", report, err)
			}
		})
	}
}

func TestAnalyzeRequiresPopulatedDeclarationProvenanceToMatchSelectedSource(t *testing.T) {
	gitTree := strings.Repeat("a", 40)
	for _, tc := range []struct {
		name, source, tree string
	}{
		{"directory provenance", "directory", ""},
		{"git provenance", "git", gitTree},
	} {
		t.Run("matching "+tc.name, func(t *testing.T) {
			in := Input{Source: tc.source, Tree: tc.tree, Declarations: declarations.Report{Status: "complete", Source: tc.source, Tree: tc.tree}}
			report, err := Analyze(context.Background(), in, Limits{})
			if err != nil {
				t.Fatal(err)
			}
			if err := ValidateReport(report); err != nil {
				t.Fatalf("matching provenance produced invalid report: %v", err)
			}
		})
	}

	// Empty provenance remains supported for callers that assemble records
	// directly rather than passing a selected declaration report.
	if _, err := Analyze(context.Background(), Input{Source: "directory"}, Limits{}); err != nil {
		t.Fatalf("empty manual declaration provenance was rejected: %v", err)
	}

	for _, tc := range []struct {
		name, inputSource, inputTree, declarationSource, declarationTree string
	}{
		{"mode mismatch", "directory", "", "git", gitTree},
		{"tree mismatch", "git", gitTree, "git", strings.Repeat("b", 40)},
		{"git provenance missing tree", "git", gitTree, "git", ""},
		{"tree without declaration source", "directory", "", "", gitTree},
	} {
		t.Run(tc.name, func(t *testing.T) {
			lock := `{"lockfileVersion":3,"packages":{"":{"dependencies":{"a":"1.0.0"}}}}`
			record := npmRecord("app", npmRef("a@1.0.0", "dependencies"))
			in := testInput([]declarations.ProjectRecord{record}, map[string]string{"app/package-lock.json": lock}, true)
			in.Source, in.Tree = tc.inputSource, tc.inputTree
			in.Declarations.Source, in.Declarations.Tree = tc.declarationSource, tc.declarationTree
			reads := 0
			in.ReadSelected = func(context.Context, string, int64) ([]byte, int64, error) {
				reads++
				return []byte(lock), int64(len(lock)), nil
			}
			if report, err := Analyze(context.Background(), in, Limits{}); err == nil || report != nil {
				t.Fatalf("conflicting declaration provenance returned report=%+v err=%v", report, err)
			}
			if reads != 0 {
				t.Fatalf("selected file was read before provenance validation: reads=%d", reads)
			}
		})
	}
}
