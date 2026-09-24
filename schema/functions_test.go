package schema_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v5"
	"github.com/war-and-code/dircue/pkg/profile"
	"github.com/war-and-code/dircue/pkg/structure"
)

func functionSchemaReport() *profile.Report {
	return &profile.Report{SchemaVersion: profile.EnhancedSchemaVersion, Root: ".", Languages: []profile.Language{}, Ecosystems: []profile.Finding{}, Frameworks: []profile.Finding{}, Layouts: []profile.Finding{}, Warnings: []profile.Warning{}, Structure: &profile.StructureReport{SupportedLanguages: structure.Capabilities(), ObservationFiles: map[string]int64{}, Engine: "big-code-analysis", EngineVersion: "2.2.0", Status: "skipped", Scope: "source", Source: "directory", MaxFileBytes: structure.MaxSourceBytes, Omissions: map[string]int64{}, Observations: map[string]uint64{}, Functions: &profile.FunctionReport{Provider: "big-code-analysis@2.2.0", Rule: "space-kind-function", RuleVersion: "1.0.0", Scope: "selected-source-files", Status: "skipped", ParentStatus: "skipped", MetricScope: "includes_nested_spaces", MetricGroups: structure.FunctionMetricGroups(), Order: "path_then_provider_preorder", Limit: 1024, PerFileLimit: 128, NameMaxBytes: 256, Omissions: map[string]int64{}, Entries: []profile.FunctionEvidence{}}}}
}

func TestFunctionSummarySchemaVersionBoundary(t *testing.T) {
	compiled, err := jsonschema.Compile("profile.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	for _, populated := range []bool{false, true} {
		report := functionSchemaReport()
		if populated {
			metrics := map[string]any{}
			for _, group := range structure.FunctionMetricGroups() {
				metrics[group] = map[string]any{"sum": 1, "undefined": nil}
			}
			raw, _ := json.Marshal(metrics)
			f := report.Structure.Functions
			f.Status = "complete"
			f.ParentStatus = "complete"
			f.TotalSpaces = 1
			f.AnalyzedFiles = 1
			f.Entries = append(f.Entries, profile.FunctionEvidence{Path: "A.java", Language: "Java", SourceSHA256: strings.Repeat("a", 64), FunctionEntry: structure.FunctionEntry{Index: 1, Name: "f", NameStatus: "present", StartLine: 1, EndLine: 2, Metrics: raw}})
			report.Structure.Status = "complete"
			report.Structure.AnalyzedFiles = 1
			report.Structure.ParseCount = 1
		}
		raw, _ := json.Marshal(report)
		var value map[string]any
		if err := json.Unmarshal(raw, &value); err != nil {
			t.Fatal(err)
		}
		if err := compiled.Validate(value); err != nil {
			t.Fatalf("valid functions: %v\n%s", err, raw)
		}
		for _, version := range []string{"1.0.0", "1.1.0", "1.2.0"} {
			value["schema_version"] = version
			if compiled.Validate(value) == nil {
				t.Fatalf("%s accepted functions", version)
			}
		}
		value["schema_version"] = "1.3.0"
		if populated {
			functions := value["structure"].(map[string]any)["functions"].(map[string]any)
			entry := functions["entries"].([]any)[0].(map[string]any)
			entry["source_sha256"] = "short"
			if compiled.Validate(value) == nil {
				t.Fatal("invalid digest accepted")
			}
			entry["source_sha256"] = strings.Repeat("a", 64)
			entry["metrics"].(map[string]any)["npa"] = map[string]any{"sum": 1}
			if compiled.Validate(value) == nil {
				t.Fatal("file-only function metrics accepted")
			}
			delete(entry["metrics"].(map[string]any), "npa")
			entry["name_status"] = "unavailable"
			if compiled.Validate(value) == nil {
				t.Fatal("unavailable name value accepted")
			}
			delete(entry, "name")
			if err := compiled.Validate(value); err != nil {
				t.Fatal(err)
			}
		}
		delete(value["structure"].(map[string]any), "functions")
		if compiled.Validate(value) == nil {
			t.Fatal("1.3 requires an explicitly enabled enhanced module")
		}
		value["schema_version"] = "1.2.0"
		if err := compiled.Validate(value); err != nil {
			t.Fatal(err)
		}
	}
}
