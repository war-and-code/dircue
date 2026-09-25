package schema_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v5"
	"github.com/war-and-code/dircue/internal/cli"
	"github.com/war-and-code/dircue/pkg/reportdiff"
)

func TestNativeHotspotCLIAndSchemaAcrossLanguages(t *testing.T) {
	worker := os.Getenv("DIRCUE_STRUCTURAL_WORKER")
	if worker == "" {
		t.Skip("native structural worker not supplied")
	}
	compiled, err := jsonschema.Compile("profile.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	fixtureRoot := filepath.Join("..", "tests", "structural_breadth")
	raw, err := os.ReadFile(filepath.Join(fixtureRoot, "fixtures.json"))
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []struct{ Path, Language string }
	if err := json.Unmarshal(raw, &fixtures); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	sources := map[string][]byte{}
	var attributes strings.Builder
	for _, fixture := range fixtures {
		source, err := os.ReadFile(filepath.Join(fixtureRoot, "testdata", fixture.Path))
		if err != nil {
			t.Fatal(err)
		}
		sources[fixture.Path] = source
		if err := os.WriteFile(filepath.Join(root, fixture.Path), source, 0600); err != nil {
			t.Fatal(err)
		}
		fmt.Fprintf(&attributes, "%s linguist-language=%s linguist-generated=false linguist-vendored=false linguist-detectable=true\n", fixture.Path, fixture.Language)
	}
	if err := os.WriteFile(filepath.Join(root, ".gitattributes"), []byte(attributes.String()), 0600); err != nil {
		t.Fatal(err)
	}
	invoke := func(args ...string) ([]byte, map[string]any) {
		t.Helper()
		args = append(args, "--json", "--source", "directory", "--structural-worker", worker, root)
		var out, stderr bytes.Buffer
		if err := cli.Execute(context.Background(), args, &out, &stderr); err != nil {
			t.Fatalf("%v: %v (%s)", args, err, stderr.String())
		}
		if stderr.Len() != 0 && !strings.Contains(strings.Join(args, " "), "--tree-size 1") {
			t.Fatal(stderr.String())
		}
		var value map[string]any
		if err := json.Unmarshal(out.Bytes(), &value); err != nil {
			t.Fatal(err)
		}
		if err := compiled.Validate(value); err != nil {
			t.Fatalf("%v: untouched upstream schema validator: %v", args, err)
		}
		snapshot, err := reportdiff.Load(bytes.NewReader(out.Bytes()))
		if err != nil {
			if directory := os.Getenv("DIRCUE_NATIVE_FAILURE_DIR"); directory != "" {
				_ = os.WriteFile(filepath.Join(directory, "hotspots-profile.json"), out.Bytes(), 0600)
			}
			t.Fatalf("%v: actual report import: %v", args, err)
		}
		compared, err := reportdiff.Compare(snapshot, snapshot)
		if err != nil {
			t.Fatal(err)
		}
		for _, module := range compared.Modules {
			if module.Name == "hotspots" {
				expected := "unchanged"
				if module.BaseStatus == "skipped" {
					expected = "unavailable"
				}
				if module.Status != expected {
					t.Fatalf("actual report self-comparison changed: %+v", module)
				}
			}
		}
		return append([]byte(nil), out.Bytes()...), value
	}
	first, report := invoke("analyze", "structure", "--hotspots", "--files", "--workers", "1")
	second, _ := invoke("analyze", "structure", "--hotspots", "--files", "--workers", "8")
	if !bytes.Equal(first, second) {
		t.Fatal("worker scheduling changed hotspot output")
	}
	parent := report["structure"].(map[string]any)
	hotspots := parent["hotspots"].(map[string]any)
	if report["schema_version"] != "1.5.0" || hotspots["analyzed_files"] != float64(len(fixtures)) || parent["parse_count"] != float64(len(fixtures)) {
		t.Fatal("hotspot version, measured files or single-parse contract changed")
	}
	if _, found := parent["functions"]; found {
		t.Fatal("hotspots implied function list retention")
	}
	languages := map[string]bool{}
	for _, rawGroup := range hotspots["groups"].([]any) {
		group := rawGroup.(map[string]any)
		languages[group["language"].(string)] = true
		for _, rawMetric := range group["metrics"].([]any) {
			metric := rawMetric.(map[string]any)
			count := metric["count"].(float64)
			var total float64
			for _, value := range metric["histogram"].([]any) {
				total += value.(float64)
			}
			if total != count || count != group["total_spaces"].(float64)-group["invalid_span_spaces"].(float64) {
				t.Fatal("native metric population differs from histogram")
			}
			if count == 0 && (metric["min"] != nil || metric["max"] != nil || len(metric["top"].([]any)) != 0) {
				t.Fatal("empty native population acquired non-null extrema")
			}
			for _, rawEntry := range metric["top"].([]any) {
				entry := rawEntry.(map[string]any)
				path := entry["path"].(string)
				if entry["source_sha256"] != fmt.Sprintf("%x", sha256.Sum256(sources[path])) || entry["path_sha256"] != fmt.Sprintf("%x", sha256.Sum256([]byte(path))) {
					t.Fatal("native top evidence lost source/path identity")
				}
			}
		}
	}
	if len(languages) != 20 {
		t.Fatalf("hotspot aggregation covered %d languages, want 20", len(languages))
	}
	_, together := invoke("analyze", "all", "--structure", "--hotspots", "--functions", "--files", "--formats")
	if !reflect.DeepEqual(hotspots, together["structure"].(map[string]any)["hotspots"]) {
		t.Fatal("function list or format inspection altered hotspot population")
	}
	_, baseline := invoke("analyze", "structure", "--files")
	delete(parent, "hotspots")
	report["schema_version"] = baseline["schema_version"]
	if !reflect.DeepEqual(report, baseline) {
		t.Fatal("hotspot option changed default report fields")
	}
	_, skipped := invoke("analyze", "structure", "--hotspots", "--tree-size", "1")
	h := skipped["structure"].(map[string]any)["hotspots"].(map[string]any)
	if h["status"] != "skipped" || len(h["groups"].([]any)) != 0 {
		t.Fatal("skipped tree acquired measured populations")
	}
	root = t.TempDir()
	for name, source := range map[string]string{"Empty.java": "class Empty {}\n", "Empty.cs": "class Empty {}\n", "empty.py": "VALUE = 1\n"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(source), 0600); err != nil {
			t.Fatal(err)
		}
	}
	_, empty := invoke("analyze", "structure", "--hotspots", "--functions", "--files")
	emptyHotspots := empty["structure"].(map[string]any)["hotspots"].(map[string]any)
	if emptyHotspots["total_spaces"] != float64(0) || len(emptyHotspots["groups"].([]any)) != 3 {
		t.Fatal("expected three measured languages with no function spaces")
	}
	for _, item := range emptyHotspots["groups"].([]any) {
		for _, entry := range item.(map[string]any)["metrics"].([]any) {
			metric := entry.(map[string]any)
			if metric["min"] != nil || metric["max"] != nil || metric["count"] != float64(0) || len(metric["top"].([]any)) != 0 {
				t.Fatal("empty measured language population fabricated metric values")
			}
		}
	}

}
