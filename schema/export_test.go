package schema_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"dircue/internal/cli"
	"dircue/pkg/capabilities"
	"dircue/pkg/mapdoc"
	"dircue/pkg/planning"
	"dircue/pkg/profile"
	"dircue/pkg/mapdiff"
	"dircue/pkg/reportdiff"
	"dircue/pkg/structure"
	"dircue/schema"
	upstream "github.com/santhosh-tekuri/jsonschema/v5"
)

const exportResourceBase = "https://dircue.invalid/schema/"

func TestSchemaExportNamesAndIsolation(t *testing.T) {
	want := []string{"availability", "capabilities", "cli-capabilities", "comparison", "declarations", "environments", "explanation", "findings", "focus", "forest", "formats", "guide", "hotspots", "languages", "map", "map-compare", "planning", "profile", "stats"}
	if got := schema.Names(); !reflect.DeepEqual(got, want) {
		t.Fatalf("schema export allowlist: got %v, want %v", got, want)
	}
	names := schema.Names()
	names[0] = "caller mutation"
	if !reflect.DeepEqual(schema.Names(), want) {
		t.Fatal("caller changed the schema registry")
	}
	for _, invalid := range []string{"", "PROFILE", "profile.schema.json", "./profile", "../profile", "profile#/$defs/language", "profile?x=y", "https://dircue.invalid/schema/profile.schema.json", "file:///etc/passwd", "single-file", "profile\x00"} {
		if output, err := schema.Export(invalid); err == nil || len(output) != 0 {
			t.Fatalf("noncanonical schema name %q accepted", invalid)
		}
	}
	for _, name := range want {
		first, err := schema.Export(name)
		if err != nil {
			t.Fatal(err)
		}
		second, err := schema.Export(name)
		if err != nil || !bytes.Equal(first, second) {
			t.Fatalf("nondeterministic schema %s: %v", name, err)
		}
		first[0] = '!'
		third, err := schema.Export(name)
		if err != nil || !bytes.Equal(second, third) {
			t.Fatalf("returned schema %s aliases internal storage", name)
		}
	}
}

func compileExportPair(t *testing.T, name string) (*upstream.Schema, *upstream.Schema) {
	t.Helper()
	original := upstream.NewCompiler()
	exported := upstream.NewCompiler()
	denyLoad := func(location string) (io.ReadCloser, error) {
		t.Errorf("schema attempted external loading: %s", location)
		return nil, fmt.Errorf("external schema loading forbidden")
	}
	original.LoadURL, exported.LoadURL = denyLoad, denyLoad
	for _, resource := range schema.Names() {
		raw, err := os.ReadFile(resource + ".schema.json")
		if err != nil {
			t.Fatal(err)
		}
		if err := original.AddResource(exportResourceBase+resource+".schema.json", bytes.NewReader(raw)); err != nil {
			t.Fatal(err)
		}
	}
	before, err := original.Compile(exportResourceBase + name + ".schema.json")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := schema.Export(name)
	if err != nil {
		t.Fatal(err)
	}
	// A consumer can save the compound document anywhere. Its absolute IDs
	// must still resolve every resource, without the original files present.
	location := "https://consumer.invalid/saved/arbitrary-name.json"
	if err := exported.AddResource(location, bytes.NewReader(raw)); err != nil {
		t.Fatal(err)
	}
	after, err := exported.Compile(location)
	if err != nil {
		t.Fatal(err)
	}
	return before, after
}

func exportJSONValue(t *testing.T, value any) any {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var decoded any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&decoded); err != nil {
		t.Fatal(err)
	}
	return decoded
}

func exportCLIValue(t *testing.T, args ...string) any {
	t.Helper()
	var out, stderr bytes.Buffer
	if err := cli.Execute(context.Background(), args, &out, &stderr); err != nil {
		t.Fatalf("%v: %v (%s)", args, err, stderr.String())
	}
	var value any
	decoder := json.NewDecoder(&out)
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		t.Fatal(err)
	}
	return value
}

func TestExportedSchemasMatchOriginalResourcesOffline(t *testing.T) {
	root := writeContractFixture(t)
	for name, content := range map[string]string{
		"go.mod":       "module example.test/export\ngo 1.26.0\n",
		"package.json": `{"name":"export-fixture","dependencies":{"react":"1"}}`,
		"global.json":  `{"sdk":{"version":"8.0.100"}}`,
		"data.json":    `{"value":1}`,
	} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	base := targetedCLIReport(t, "analyze", "all", "--availability", "--declarations", "--environments", "--formats", "--json", "--source", "directory", root)
	focus := targetedCLIReport(t, "analyze", "focus", "--project", "App.csproj", "--json", "--source", "directory", root)
	explanation := targetedCLIReport(t, "analyze", "explain", "--file", "main.go", "--json", "--source", "directory", root)

	// Reuse the existing structural schema fixture, without executing a worker.
	withHotspots := functionSchemaReport()
	withHotspots.SchemaVersion = profile.ContentSchemaVersion
	withHotspots.Structure.Functions = nil
	hotspots := structure.NewHotspotReport()
	hotspots.Status, hotspots.FileCoverageStatus = "complete", "complete"
	hotspots.AnalyzedFiles = 1
	hotspots.Groups = []structure.HotspotGroup{{Language: "Java", Grammar: "tree-sitter-java", SyntaxCohort: "clean", AnalyzedFiles: 1, Metrics: []structure.HotspotDistribution{}}}
	for _, metric := range []string{"cyclomatic_sum", "span_lines"} {
		hotspots.Groups[0].Metrics = append(hotspots.Groups[0].Metrics, structure.HotspotDistribution{Metric: metric, Definition: "test definition", Unit: "count", MetricScope: "includes_nested_spaces", Histogram: make([]uint64, 65), Top: []structure.HotspotEvidence{}})
	}
	withHotspots.Structure.Hotspots = hotspots

	raw, err := json.Marshal(base)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := reportdiff.Load(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	comparison, err := reportdiff.Compare(snapshot, snapshot)
	if err != nil {
		t.Fatal(err)
	}
	var report profile.Report
	if err := json.Unmarshal(raw, &report); err != nil {
		t.Fatal(err)
	}
	plan, err := planning.Build(context.Background(), planning.Input{Profile: &report, ReportSHA256: strings.Repeat("a", 64), Capabilities: capabilities.Dircue("test"), Selection: planning.Selection{Modules: []string{"metrics"}}})
	if err != nil {
		t.Fatal(err)
	}
	values := map[string]any{
		"cli-capabilities": exportCLIValue(t, "capabilities", "--cli", "--json"),
		"guide":            exportCLIValue(t, "capabilities", "--guide", "--json"),
		"availability":     base["availability"], "capabilities": capabilities.Dircue("test"),
		"comparison": comparison, "declarations": base["declarations"],
		"environments": base["environments"], "explanation": explanation["explanation"],
		"findings": exportCLIValue(t, "analyze", "frameworks", "--json", "--source", "directory", root),
		"focus":    focus["focus"], "formats": base["formats"], "hotspots": hotspots,
		"languages": exportCLIValue(t, "--json", "--source", "directory", root),
		"map-compare": func() mapdiff.Report {
			doc := mapdoc.Document{SchemaVersion: mapdoc.SchemaVersion, Kind: "map", Status: mapdoc.CoverageComplete, Source: mapdoc.Source{Mode: "directory", Digest: &mapdoc.Digest{Algorithm: "sha256", Scope: "full_selected_tree", Value: strings.Repeat("a", 64)}}, Coverage: []mapdoc.QuestionCoverage{}, CoverageLedger: []mapdoc.CoverageLedgerEntry{}, AnalyzerCoverage: []mapdoc.AnalyzerCoverageEntry{}, AnalyzerBlindSpots: []mapdoc.AnalyzerBlindSpot{}, Nodes: []mapdoc.Node{}, Edges: []mapdoc.Edge{}}
			report, err := mapdiff.Compare(doc, doc)
			if err != nil {
				panic("map-compare fixture: " + err.Error())
			}
			return report
		}(),
		"map": func() mapdoc.Document {
			n := mapdoc.NewNode(mapdoc.NodeContent, []string{"main.go"}, "source")
			n.Properties = map[string]string{"role": "source"}
			n.Coverage = mapdoc.Coverage{Status: mapdoc.CoverageComplete, Reasons: []string{}}
			n.Evidence = []mapdoc.Evidence{{Basis: mapdoc.BasisFilenameHint, Path: "main.go", SourceKind: mapdoc.SourceFile, Rule: &mapdoc.Producer{ID: "fixture", Version: "1"}}}
			return mapdoc.Document{SchemaVersion: mapdoc.SchemaVersion, Kind: "map", Status: mapdoc.CoverageComplete, Source: mapdoc.Source{Mode: "directory", Digest: &mapdoc.Digest{Algorithm: "sha256", Scope: "full_selected_tree", Value: strings.Repeat("a", 64)}}, Coverage: []mapdoc.QuestionCoverage{}, CoverageLedger: []mapdoc.CoverageLedgerEntry{}, AnalyzerCoverage: []mapdoc.AnalyzerCoverageEntry{}, AnalyzerBlindSpots: []mapdoc.AnalyzerBlindSpot{}, Nodes: []mapdoc.Node{n}, Edges: []mapdoc.Edge{}}
		}(),
		"planning": plan, "profile": base,
		"stats": map[string]any{
			"schema_version": "1.0.0",
			"kind":           "run-stats",
			"command":        "map",
			"source_mode":    "directory",
			"deterministic_costs": map[string]any{
				"files_enumerated":   float64(10),
				"files_content_read": float64(8),
				"bytes_requested":    float64(4096),
				"limit_hits":         map[string]any{"file_bytes": float64(0), "tree_size": float64(0)},
			},
			"measurements": map[string]any{
				"wall_time_ns":           float64(1_000_000_000),
				"phases":                 map[string]any{"scan_ns": float64(800_000_000), "build_ns": float64(100_000_000)},
				"peak_heap_inuse_bytes":  float64(10_000_000),
				"gc_count":               float64(2),
			},
		},
		"forest": map[string]any{
			"schema_version": "1.0.0",
			"kind":           "forest",
			"status":         "complete",
			"source":         map[string]any{"mode": "directory", "path": "."},
			"coverage": []map[string]any{
				{"question": "roots", "scope": ".", "status": "complete", "reasons": []string{}},
				{"question": "residual", "scope": ".", "status": "complete", "reasons": []string{}},
				{"question": "environment_trees", "scope": ".", "status": "complete", "reasons": []string{}},
			},
			"roots": []any{},
			"residual": map[string]any{
				"schema_version":       "1.0.0",
				"kind":                 "map",
				"status":               "complete",
				"source":               map[string]any{"mode": "directory"},
				"coverage":             []any{},
				"coverage_ledger":      []any{},
				"analyzer_coverage":    []any{},
				"analyzer_blind_spots": []any{},
				"nodes":                []any{},
				"edges":                []any{},
			},
			"residual_totals": map[string]any{
				"files": float64(42),
				"bytes": float64(1024),
			},
		},
	}
	for _, name := range schema.Names() {
		t.Run(name, func(t *testing.T) {
			before, after := compileExportPair(t, name)
			check := func(label string, value any, valid bool) {
				t.Helper()
				a, b := before.Validate(value), after.Validate(value)
				if (a == nil) != valid || (b == nil) != valid {
					t.Fatalf("%s: original=%v exported=%v expected_valid=%t", label, a, b, valid)
				}
			}
			value := exportJSONValue(t, values[name])
			check("actual report", value, true)
			check("wrong root type", "invalid report", false)
			invalid := exportJSONValue(t, value)
			switch name {
			case "languages":
				for _, row := range invalid.(map[string]any) {
					row.(map[string]any)["unexpected_contract_field"] = true
				}
			case "findings":
				rows := invalid.([]any)
				if len(rows) == 0 {
					t.Fatal("framework fixture produced no finding")
				}
				rows[0].(map[string]any)["unexpected_contract_field"] = true
			default:
				invalid.(map[string]any)["unexpected_contract_field"] = true
			}
			check("unknown field", invalid, false)
			if name == "profile" {
				// Together these exercise every external resource used by the
				// profile, including their conflicting local $defs namespaces.
				for label, report := range map[string]any{"focus": focus, "explanation": explanation, "hotspots": withHotspots} {
					check(label+" profile", exportJSONValue(t, report), true)
				}
				for _, component := range []string{"availability", "declarations", "environments", "formats"} {
					bad := exportJSONValue(t, base).(map[string]any)
					bad[component].(map[string]any)["status"] = "not-a-status"
					check("invalid nested "+component, bad, false)
				}
				for label, report := range map[string]any{"focus": focus, "explanation": explanation} {
					bad := exportJSONValue(t, report).(map[string]any)
					bad[label].(map[string]any)["status"] = "not-a-status"
					check("invalid nested "+label, bad, false)
				}
				bad := exportJSONValue(t, withHotspots).(map[string]any)
				h := bad["structure"].(map[string]any)["hotspots"].(map[string]any)
				h["groups"].([]any)[0].(map[string]any)["metrics"].([]any)[0].(map[string]any)["histogram"] = []any{json.Number("0")}
				check("invalid nested hotspot histogram", bad, false)
			}
		})
	}
}

func TestLanguagesSchemaAcceptsExistingNaNStringOnly(t *testing.T) {
	before, after := compileExportPair(t, "languages")
	root := t.TempDir()
	for name, content := range map[string]string{".gitattributes": "*.py linguist-language=Python\n", "source.py": ""} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	value := exportCLIValue(t, "--json", "--breakdown", "--source", "directory", root)
	row := value.(map[string]any)["Python"].(map[string]any)
	if row["percentage"] != "NaN" || row["size"] != json.Number("0") {
		t.Fatalf("fixture no longer exercises existing zero-byte Linguist output: %v", row)
	}
	for _, s := range []*upstream.Schema{before, after} {
		if err := s.Validate(value); err != nil {
			t.Fatalf("existing NaN string output rejected: %v", err)
		}
		positive := exportJSONValue(t, value).(map[string]any)
		positive["Python"].(map[string]any)["size"] = json.Number("1")
		positive["Python"].(map[string]any)["percentage"] = "100.00"
		if err := s.Validate(positive); err != nil {
			t.Fatalf("positive-size numeric percentage rejected: %v", err)
		}
		impossible := exportJSONValue(t, positive).(map[string]any)
		impossible["Python"].(map[string]any)["percentage"] = "NaN"
		if s.Validate(impossible) == nil {
			t.Fatal("NaN percentage accepted for a nonzero language size")
		}
		for _, percentage := range []string{"0.00", "99.99", "100.00"} {
			normal := exportJSONValue(t, positive).(map[string]any)
			normal["Python"].(map[string]any)["percentage"] = percentage
			if err := s.Validate(normal); err != nil {
				t.Fatalf("normal percentage %q rejected: %v", percentage, err)
			}
		}
		for _, percentage := range []string{"100.01", "999.99"} {
			impossible := exportJSONValue(t, positive).(map[string]any)
			impossible["Python"].(map[string]any)["percentage"] = percentage
			if s.Validate(impossible) == nil {
				t.Fatalf("out-of-range percentage %q accepted", percentage)
			}
		}
		for _, percentage := range []any{"nan", "NAN", "Inf", "Infinity", "-NaN", "NaN%", "1.0", "1", "", 0.0, nil} {
			bad := exportJSONValue(t, value).(map[string]any)
			bad["Python"].(map[string]any)["percentage"] = percentage
			if s.Validate(bad) == nil {
				t.Fatalf("invalid percentage accepted: %#v", percentage)
			}
		}
	}
	// This correction accepts a JSON string, never nonstandard JSON NaN tokens.
	if json.Valid([]byte(`{"Python":{"size":0,"percentage":NaN}}`)) {
		t.Fatal("bare NaN unexpectedly became valid JSON")
	}
}
