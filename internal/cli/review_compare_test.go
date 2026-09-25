package cli

import (
	"bytes"
	"github.com/war-and-code/dircue/pkg/reportdiff"
	"strings"
	"testing"
)

func TestComparisonTextTruncatesOnlyWhenRowsHidden(t *testing.T) {
	for _, size := range []int{199, 200, 201} {
		module := reportdiff.Module{Name: "metrics_files", Counts: reportdiff.Counts{OmittedChanges: 7}}
		for i := 0; i < size; i++ {
			module.Changes = append(module.Changes, reportdiff.Change{ID: "f", Status: "changed"})
		}
		var out bytes.Buffer
		if err := writeComparison(&out, &reportdiff.Report{Modules: []reportdiff.Module{module}}); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(out.String(), "at most 200") != (size > 200) {
			t.Fatalf("size %d: incorrect truncation notice", size)
		}
		if !strings.Contains(out.String(), "7 omitted changes") {
			t.Fatal("omitted count missing")
		}
	}
}

func TestComparisonTextPreservesHeaderWithoutOmissions(t *testing.T) {
	module := reportdiff.Module{Name: "languages", Status: "unchanged", Compatibility: "compatible"}
	var out bytes.Buffer
	if err := writeComparison(&out, &reportdiff.Report{Modules: []reportdiff.Module{module}}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "omitted changes") {
		t.Fatalf("zero-valued review field changed ordinary text output: %s", out.String())
	}
	want := "languages: unchanged (compatible) — 0 added, 0 removed, 0 changed, 0 unchanged, 0 unavailable\n"
	if !strings.Contains(out.String(), want) {
		t.Fatalf("historical header changed: %s", out.String())
	}
}
