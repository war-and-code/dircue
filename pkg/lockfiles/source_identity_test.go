package lockfiles

import (
	"context"
	"strings"
	"testing"
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
