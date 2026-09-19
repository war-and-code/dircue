package scanner

import (
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"strings"
	"testing"

	"dircue/pkg/profile"
	"dircue/pkg/structure"
)

func functionFile(path string, count int) structure.File {
	metrics := map[string]any{}
	for _, group := range structure.FunctionMetricGroups() {
		metrics[group] = map[string]int{"value": 1}
	}
	raw, _ := json.Marshal(metrics)
	f := structure.File{Path: path, Language: "Java", SourceBytes: 4096, SourceSHA256: strings.Repeat("a", 64), Status: "complete", ParseCount: 1, Functions: &structure.FunctionSpaces{Status: "complete", TotalSpaces: uint64(count), Entries: []structure.FunctionEntry{}}}
	for i := 0; i < count; i++ {
		f.Functions.Entries = append(f.Functions.Entries, structure.FunctionEntry{Index: uint64(i + 1), NameStatus: "unavailable", StartLine: 1, EndLine: 1, Metrics: raw})
	}
	return f
}

func functionParent() *profile.StructureReport {
	files := []structure.File{}
	return &profile.StructureReport{Status: "complete", Functions: newFunctionReport(), Omissions: map[string]int64{}, Observations: map[string]uint64{}, ObservationFiles: map[string]int64{}, Files: &files}
}

func TestFunctionsDeterministicGlobalCap(t *testing.T) {
	run := func(reverse bool) *profile.StructureReport {
		parent := functionParent()
		for i := 0; i < 10; i++ {
			index := i
			if reverse {
				index = 9 - i
			}
			f := functionFile(fmt.Sprintf("p%02d.java", index), 128)
			addStructure(parent, result{path: f.Path, structural: &f})
		}
		finishStructure(parent)
		return parent
	}
	first, second := run(false), run(true)
	if !reflect.DeepEqual(first, second) {
		t.Fatal("arrival order changed retained function evidence")
	}
	f := first.Functions
	if len(f.Entries) != 1024 || f.TotalSpaces != 1280 || f.OmittedSpaces != 256 || f.ReportOmittedSpaces != 256 || f.InvalidSpanSpaces != 0 || f.Status != "partial" || f.ParentStatus != "complete" || first.Status != "partial" {
		t.Fatalf("wrong cap: %+v", f)
	}
	if f.Entries[0].Path != "p00.java" || f.Entries[1023].Path != "p07.java" || f.Entries[1023].Index != 128 {
		t.Fatal("wrong deterministic retained prefix")
	}
	for _, file := range *first.Files {
		if file.Functions != nil || file.SourceSHA256 != "" {
			t.Fatal("function evidence duplicated into file detail")
		}
	}
}

func TestFunctionsCoverageAndPerFileOmissions(t *testing.T) {
	parent := functionParent()
	f := functionFile("A.java", 128)
	f.Functions.TotalSpaces = 132
	f.Functions.OmittedSpaces = 2
	f.Functions.InvalidSpanSpaces = 2
	f.Functions.Status = "partial"
	addStructure(parent, result{path: f.Path, structural: &f})
	skipped := structure.File{Path: "large.java", Status: "skipped", Reason: "file_too_large"}
	addStructure(parent, result{path: skipped.Path, structural: &skipped})
	finishStructure(parent)
	got := parent.Functions
	if got.TotalSpaces != uint64(len(got.Entries))+got.OmittedSpaces+got.InvalidSpanSpaces || got.PerFileOmittedSpaces != 2 || got.ReportOmittedSpaces != 0 || got.PartialFiles != 1 || got.ParentStatus != "partial" || got.Omissions["file_too_large"] != 1 || got.Status != "partial" {
		t.Fatalf("lost coverage: %+v", got)
	}
}

func TestFunctionsSkippedCoverage(t *testing.T) {
	for _, reason := range []string{"tree_size_limit", "outside_scope"} {
		parent := functionParent()
		parent.Omissions[reason] = 1
		if reason == "tree_size_limit" {
			parent.Status = "skipped"
		}
		finishStructure(parent)
		if parent.Functions.Status != "skipped" || parent.Functions.ParentStatus != "skipped" || parent.Functions.Entries == nil || parent.Functions.Omissions[reason] != 1 {
			t.Fatalf("lost skip: %+v", parent.Functions)
		}
	}
}

func TestFunctionCounterOverflowRejectsBeforeMutation(t *testing.T) {
	aggregate := newFunctionReport()
	first := structure.File{Path: "first.java", Functions: &structure.FunctionSpaces{Status: "partial", TotalSpaces: math.MaxUint64, InvalidSpanSpaces: math.MaxUint64}}
	if err := addFunctions(aggregate, first); err != nil {
		t.Fatal(err)
	}
	before := *aggregate
	next := functionFile("next.java", 1)
	if err := addFunctions(aggregate, next); err == nil || !strings.Contains(err.Error(), "counter overflow") {
		t.Fatalf("aggregate overflow accepted: %v", err)
	}
	if !reflect.DeepEqual(before, *aggregate) {
		t.Fatal("rejected addition mutated counters or retained evidence")
	}
}
