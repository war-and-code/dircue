package schema_test

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"dircue/internal/cli"
	"dircue/pkg/profile"
	"dircue/pkg/structure"
	"dircue/schema"
)

func TestFormatsSchemaBoundary(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "data.json"), []byte(`{"a":1}`), 0600); err != nil {
		t.Fatal(err)
	}
	var out, stderr bytes.Buffer
	if err := cli.Execute(context.Background(), []string{"analyze", "formats", "--json", root}, &out, &stderr); err != nil {
		t.Fatal(err)
	}
	var report map[string]any
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if err := schema.ValidateProfile(report); err != nil {
		t.Fatal(err)
	}
	for _, version := range []string{"1.0.0", "1.1.0", "1.2.0", "1.3.0", "1.4.0"} {
		report["schema_version"] = version
		if schema.ValidateProfile(report) == nil {
			t.Fatalf("formats accepted under %s", version)
		}
	}
	report["schema_version"] = "1.5.0"
	f := report["formats"].(map[string]any)
	observation := f["observations"].([]any)[0].(map[string]any)
	observation["read_scope"] = "entire_repository"
	if schema.ValidateProfile(report) == nil {
		t.Fatal("invalid read scope accepted")
	}
	observation["read_scope"] = "complete"
	f["scope"].(map[string]any)["archive_expansion"] = true
	if schema.ValidateProfile(report) == nil {
		t.Fatal("undeclared archive execution accepted")
	}
	delete(report, "formats")
	if schema.ValidateProfile(report) == nil {
		t.Fatal("schema1.5 accepted without selected module")
	}
}

func TestHotspotsSchemaBoundaryAndNullablePopulation(t *testing.T) {
	report := functionSchemaReport()
	report.SchemaVersion = profile.ContentSchemaVersion
	report.Structure.Functions = nil
	h := structure.NewHotspotReport()
	h.Status, h.FileCoverageStatus = "complete", "complete"
	h.AnalyzedFiles = 1
	h.Groups = []structure.HotspotGroup{{Language: "Java", Grammar: "tree-sitter-java", SyntaxCohort: "clean", AnalyzedFiles: 1, Metrics: []structure.HotspotDistribution{}}}
	for _, metric := range []string{"cyclomatic_sum", "span_lines"} {
		h.Groups[0].Metrics = append(h.Groups[0].Metrics, structure.HotspotDistribution{Metric: metric, Definition: "test definition", Unit: "count", MetricScope: "includes_nested_spaces", Histogram: make([]uint64, 65), Top: []structure.HotspotEvidence{}})
	}
	report.Structure.Hotspots = h
	raw, _ := json.Marshal(report)
	var value map[string]any
	if err := json.Unmarshal(raw, &value); err != nil {
		t.Fatal(err)
	}
	if err := schema.ValidateProfile(value); err != nil {
		t.Fatal(err)
	}
	for _, version := range []string{"1.0.0", "1.1.0", "1.2.0", "1.3.0", "1.4.0"} {
		value["schema_version"] = version
		if schema.ValidateProfile(value) == nil {
			t.Fatalf("hotspots accepted under %s", version)
		}
	}
	value["schema_version"] = "1.5.0"
	hotspot := value["structure"].(map[string]any)["hotspots"].(map[string]any)
	group := hotspot["groups"].([]any)[0].(map[string]any)
	metric := group["metrics"].([]any)[0].(map[string]any)
	metric["histogram"] = []any{0}
	if schema.ValidateProfile(value) == nil {
		t.Fatal("wrong histogram shape accepted")
	}
}
