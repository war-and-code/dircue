package scanner

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"dircue/pkg/profile"
	"dircue/pkg/structure"
)

func TestHotspotsCoverageIndependentOfFunctionRetention(t *testing.T) {
	parent := functionParent()
	parent.Hotspots = structure.NewHotspotReport()
	file := functionFile("a.py", 128)
	file.Functions.TotalSpaces = 129
	file.Functions.OmittedSpaces = 1
	file.Functions.Status = "partial"
	file.Provenance = &structure.Provenance{Grammar: "tree-sitter-python@0.25.0"}
	file.Hotspots = scannerHotspotPopulation(129, 0)
	if err := addStructure(parent, result{path: file.Path, structural: &file}); err != nil {
		t.Fatal(err)
	}
	finishStructure(parent)
	if parent.Status != "partial" || parent.Functions.Status != "partial" || parent.Hotspots.Status != "complete" || parent.Hotspots.FileCoverageStatus != "complete" || parent.Hotspots.TotalSpaces != 129 {
		t.Fatalf("function retention incorrectly tainted population coverage: %+v", parent.Hotspots)
	}
	if (*parent.Files)[0].Hotspots != nil || (*parent.Files)[0].SourceSHA256 != "" {
		t.Fatal("hotspot evidence duplicated into file output")
	}
}

func TestHotspotsOmittedAndEmptyCoverage(t *testing.T) {
	for _, reason := range []string{"tree_size_limit", "file_too_large", "unsupported_language", "outside_scope"} {
		parent := &profile.StructureReport{Status: "complete", Hotspots: structure.NewHotspotReport(), Omissions: map[string]int64{reason: 1}}
		if reason == "tree_size_limit" {
			parent.Status = "skipped"
		}
		finishStructure(parent)
		if parent.Hotspots.Omissions[reason] != 1 || parent.Hotspots.Status != parent.Status || parent.Hotspots.AnalyzedFiles != 0 || parent.Hotspots.Groups == nil {
			t.Fatalf("lost omission %s: %+v", reason, parent.Hotspots)
		}
	}
}

func TestHotspotsNativeLatePopulationAndWorkerOrder(t *testing.T) {
	worker := os.Getenv("DIRCUE_STRUCTURAL_WORKER")
	if worker == "" {
		t.Skip("native structural worker not supplied")
	}
	client, err := structure.New(structure.Options{Worker: worker, Hotspots: true, Functions: true})
	if err != nil {
		t.Fatal(err)
	}
	files := map[string]string{}
	for file := 0; file < 9; file++ {
		var b strings.Builder
		for i := 0; i < 140; i++ {
			fmt.Fprintf(&b, "def f_%d():\n    return 0\n", i)
		}
		files[fmt.Sprintf("small%02d.py", file)] = b.String()
	}
	files["z.py"] = "def late(x):\n" + strings.Repeat("    if x:\n        return x\n", 30)
	files["broken.py"] = "def broken(:\n return 0\n"
	files["notes.xml"] = "<log/>\n"
	root := t.TempDir()
	for path, content := range files {
		if err := os.WriteFile(filepath.Join(root, path), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	var reference []byte
	for _, workers := range []int{1, 8} {
		result, err := Scan(context.Background(), root, Options{Source: "directory", Workers: workers, Structure: client, StructureFiles: true})
		if err != nil {
			t.Fatal(err)
		}
		h := result.Structure.Hotspots
		if h == nil || h.AnalyzedFiles != 11 || h.RecoveredFiles != 1 || len(h.Groups) != 2 || h.TotalSpaces < 1261 {
			t.Fatalf("wrong population: %+v", h)
		}
		clean := h.Groups[0]
		if clean.SyntaxCohort != "clean" || clean.TotalSpaces != 1261 {
			t.Fatalf("wrong clean population: %+v", clean)
		}
		if clean.Metrics[0].Top[0].Path != "z.py" || clean.Metrics[0].Top[0].Value != 31 || clean.Metrics[0].Count != 1261 {
			t.Fatalf("ranking selected old retained sample: %+v", clean.Metrics[0])
		}
		if len(result.Structure.Functions.Entries) != 1024 || result.Structure.Functions.OmittedSpaces == 0 {
			t.Fatal("fixture did not exceed existing function caps")
		}
		raw, _ := json.Marshal(h)
		if reference == nil {
			reference = raw
		} else if string(reference) != string(raw) {
			t.Fatal("worker count changes measured population")
		}
		for _, f := range *result.Structure.Files {
			if f.Hotspots != nil || f.Functions != nil {
				t.Fatal("duplicated opt-in evidence")
			}
		}
	}
}

func TestHotspotInvalidSpanQualifiesParentWithoutRewritingFileCoverage(t *testing.T) {
	parent := functionParent()
	parent.Functions = nil
	parent.Hotspots = structure.NewHotspotReport()
	file := functionFile("a.py", 0)
	file.Functions = nil
	file.Provenance = &structure.Provenance{Grammar: "grammar"}
	file.Hotspots = scannerHotspotPopulation(1, 1)
	if err := addStructure(parent, result{path: file.Path, structural: &file}); err != nil {
		t.Fatal(err)
	}
	finishStructure(parent)
	if parent.Status != "partial" || parent.Hotspots.Status != "partial" || parent.Hotspots.FileCoverageStatus != "complete" {
		t.Fatalf("incorrect status qualification: %+v", parent.Hotspots)
	}
}

func scannerHotspotPopulation(total, invalid uint64) *structure.FileHotspots {
	result := &structure.FileHotspots{Provider: "big-code-analysis@2.2.0", Rule: "function-population", RuleVersion: "1.0.0", TotalSpaces: total, InvalidSpanSpaces: invalid, Metrics: []structure.HotspotMetric{}}
	for _, name := range []string{"cyclomatic_sum", "span_lines"} {
		histogram := make([]uint64, 65)
		histogram[1] = total - invalid
		result.Metrics = append(result.Metrics, structure.HotspotMetric{Metric: name, Count: total - invalid, Histogram: histogram, Top: []structure.HotspotEntry{}})
	}
	return result
}
