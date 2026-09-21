package reportdiff

import (
	"dircue/pkg/profile"
	"fmt"
	"testing"
)

func TestFairQuotasPreserveSmallLateModules(t *testing.T) {
	got := fairQuotas([]int{5000, 2, 5000, 1}, 4096)
	if got[1] != 2 || got[3] != 1 || got[0] < 2000 || got[2] < 2000 {
		t.Fatalf("unfair retention: %v", got)
	}
	total := 0
	for _, n := range got {
		total += n
	}
	if total != 4096 {
		t.Fatal(total)
	}
}

func TestMissingModuleStatusIsExplicit(t *testing.T) {
	budget, bytes := 100, 10000
	m := compareModule("formats", moduleData{}, moduleData{present: true, status: "complete"}, &budget, &bytes)
	if m.BaseStatus != "unavailable" || m.HeadStatus != "complete" || m.Compatibility != "unavailable" {
		t.Fatalf("%+v", m)
	}
}

func TestLateModuleEvidenceSurvivesLargeLanguageDiff(t *testing.T) {
	base, head := emptyProfile(), emptyProfile()
	for i := 0; i < MaxChanges+10; i++ {
		head.Languages = append(head.Languages, profile.Language{Name: fmt.Sprintf("lang-%d", i), Bytes: 1, Percentage: 1, FileCount: 1})
	}
	head.Layouts = append(head.Layouts, profile.Finding{Kind: "layout", Name: "test-layout", Root: ".", Evidence: []string{"config"}, Detector: "fixture"})
	result := comparison(t, base, head)
	early, late := moduleNamed(t, result, "languages"), moduleNamed(t, result, "layouts")
	if len(late.Changes) != 1 || late.Counts.OmittedChanges != 0 {
		t.Fatalf("late evidence starved: %+v", late)
	}
	if len(early.Changes) == 0 || len(early.Changes) >= MaxChanges || early.Counts.OmittedChanges+len(early.Changes) != MaxChanges+10 {
		t.Fatalf("early retention incorrect: %+v", early.Counts)
	}
	if result.Status != "partial" {
		t.Fatal(result.Status)
	}
}
