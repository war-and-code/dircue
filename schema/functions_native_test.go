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
)

func TestNativeFunctionCLIAndSchema(t *testing.T) {
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
		var stdout, stderr bytes.Buffer
		if err := cli.Execute(context.Background(), args, &stdout, &stderr); err != nil {
			t.Fatalf("%v: %v (%s)", args, err, stderr.String())
		}
		var value map[string]any
		if err := json.Unmarshal(stdout.Bytes(), &value); err != nil {
			t.Fatal(err)
		}
		if err := compiled.Validate(value); err != nil {
			t.Fatalf("%v: schema: %v", args, err)
		}
		return append([]byte(nil), stdout.Bytes()...), value
	}
	first, report := invoke("analyze", "structure", "--functions", "--files", "--workers", "1")
	second, _ := invoke("analyze", "structure", "--functions", "--files", "--workers", "8")
	if !bytes.Equal(first, second) {
		t.Fatal("worker count changed function output")
	}
	structure := report["structure"].(map[string]any)
	functions := structure["functions"].(map[string]any)
	if report["schema_version"] != "1.3.0" || functions["analyzed_files"] != float64(len(fixtures)) {
		t.Fatalf("wrong version or coverage: %+v", functions)
	}
	groups := functions["metric_groups"].([]any)
	if len(groups) != 10 {
		t.Fatal("wrong function metric groups")
	}
	languages := map[string]bool{}
	for _, item := range structure["files"].([]any) {
		file := item.(map[string]any)
		if _, found := file["functions"]; found {
			t.Fatal("function list duplicated into --files")
		}
		if _, found := file["source_sha256"]; found {
			t.Fatal("function hash leaked into legacy file detail")
		}
		if file["status"] != "skipped" {
			languages[file["language"].(string)] = true
		}
	}
	if len(languages) != 20 {
		t.Fatalf("analyzed %d languages, expected20", len(languages))
	}
	entries := functions["entries"].([]any)
	for _, item := range entries {
		entry := item.(map[string]any)
		source := sources[entry["path"].(string)]
		if entry["source_sha256"] != fmt.Sprintf("%x", sha256.Sum256(source)) {
			t.Fatal("source hash mismatch")
		}
	}
	if functions["total_spaces"].(float64) != float64(len(entries))+functions["omitted_spaces"].(float64)+functions["invalid_span_spaces"].(float64) {
		t.Fatal("function total lost coverage")
	}
	_, baseline := invoke("analyze", "structure", "--files")
	delete(structure, "functions")
	report["schema_version"] = "1.2.0"
	// Some grammars yield function-name/span omissions in otherwise complete files;
	// only the opt-in function coverage can make the parent status more cautious.
	structure["status"] = baseline["structure"].(map[string]any)["status"]
	if !reflect.DeepEqual(report, baseline) {
		t.Fatal("function opt-in changed preexisting report content")
	}
	_, combined := invoke("analyze", "all", "--structure", "--functions", "--files", "--metrics", "--projects", "--discovery", "--graph")
	for _, field := range []string{"structure", "metrics", "projects", "discovery", "graph"} {
		if _, ok := combined[field]; !ok {
			t.Fatalf("combined report missing %s", field)
		}
	}
	_, skip := invoke("analyze", "structure", "--functions", "--tree-size", "1")
	skipped := skip["structure"].(map[string]any)["functions"].(map[string]any)
	if skip["schema_version"] != "1.3.0" || skipped["status"] != "skipped" || skipped["parent_status"] != "skipped" || len(skipped["entries"].([]any)) != 0 {
		t.Fatalf("wrong tree skip: %+v", skipped)
	}
}
