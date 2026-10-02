package cli

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/war-and-code/dircue/pkg/environments"
	"github.com/war-and-code/dircue/pkg/formats"
	"github.com/war-and-code/dircue/pkg/intentmap"
	"github.com/war-and-code/dircue/pkg/mapdoc"
	"github.com/war-and-code/dircue/pkg/projects"
	"github.com/war-and-code/dircue/pkg/registries"
	"github.com/war-and-code/dircue/pkg/scanner"
	"github.com/war-and-code/dircue/pkg/structure"
)

func TestFixedMapBoundsAreDiscoverableAndReadOnly(t *testing.T) {
	out, _, err := invoke("map", "settings", "--json")
	if err != nil {
		t.Fatal(err)
	}
	var report mapSettingsReport
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatal(err)
	}
	values := map[string]mapEffectiveSetting{}
	for _, s := range report.Settings {
		if _, exists := values[s.Name]; exists {
			t.Fatalf("duplicate setting: %s", s.Name)
		}
		values[s.Name] = s
	}
	lanes, singleLaneReaders, concurrentLaneReaders := scanner.GitStorageBounds()
	want := map[string]struct {
		value int64
		unit  string
	}{
		"git.object_lanes":                         {int64(lanes), "lanes"},
		"git.retained_readers_single_lane":         {int64(singleLaneReaders), "readers"},
		"git.retained_readers_per_concurrent_lane": {int64(concurrentLaneReaders), "readers"},
		"formats.files":                            {int64(formats.MaxFiles), "files"},
		"formats.file_bytes":                       {formats.MaxFileBytes, "bytes"},
		"formats.input_bytes":                      {formats.MaxInputBytes, "bytes"},
		"formats.output_bytes":                     {formats.MaxOutputBytes, "bytes"},
		"formats.parser_depth":                     {int64(formats.MaxDepth), "levels"},
		"formats.parser_tokens":                    {int64(formats.MaxTokens), "tokens"},
		"manifests.file_bytes":                     {projects.MaxManifestBytes, "bytes"},
		"registries.file_bytes":                    {registries.MaxFileBytes, "bytes"},
		"environments.inventory_paths":             {int64(environments.DefaultMaxInventoryPaths), "paths"},
		"environments.file_bytes":                  {environments.DefaultMaxGlobalJSONBytes, "bytes"},
		"environments.input_bytes":                 {environments.DefaultMaxInputBytes, "bytes"},
		"environments.requirements":                {int64(environments.DefaultMaxRequirements), "requirements"},
		"environments.contexts":                    {int64(environments.DefaultMaxContexts), "contexts"},
		"environments.output_bytes":                {int64(environments.DefaultMaxOutputBytes), "bytes"},
		"structure.report_functions":               {int64(scanner.FunctionReportLimit()), "functions"},
		"structure.file_functions":                 {int64(structure.FunctionLimit), "functions"},
		"intent.import_tokens_per_file":            {int64(intentmap.DefaultMaxLexicalTokensPerFile), "tokens"},
	}
	fixed := map[string]mapEffectiveSetting{}
	for _, s := range report.Settings {
		if s.Category == "fixed-safety-bound" {
			fixed[s.Name] = s
		}
	}
	if len(fixed) != len(want) {
		t.Fatalf("fixed bound inventory has %d entries, want %d: %+v", len(fixed), len(want), fixed)
	}
	for name, expected := range want {
		s, ok := fixed[name]
		if !ok || s.Category != "fixed-safety-bound" || s.Minimum != s.Value || s.Maximum != s.Value {
			t.Fatalf("missing fixed bound: %s %+v", name, s)
		}
		if s.Origin != "fixed" || s.Unit != expected.unit || s.Description == "" {
			t.Fatalf("fixed bound metadata mismatch: %s %+v", name, s)
		}
		parsed, err := strconv.ParseInt(s.Value, 10, 64)
		if err != nil || parsed != expected.value {
			t.Fatalf("bound drift: %s got %s want %d", name, s.Value, expected.value)
		}
		out, _, err := invoke("map", "settings", "--set", name+"="+s.Value)
		if err == nil || !strings.Contains(err.Error(), "fixed bound") || out != "" {
			t.Fatalf("fixed bound accepted: %s %v", name, err)
		}
	}
}

func TestMapPerformanceSettingsPreserveAnswers(t *testing.T) {
	root := t.TempDir()
	for name, data := range map[string]string{
		"go.mod":     "module example.invalid/app\n\ngo 1.24\n",
		"main.go":    "package main\nimport \"net/http\"\nfunc main() { http.Get(\"https://example.invalid\") }\n",
		"Dockerfile": "FROM scratch\nCOPY main.go /main.go\nEXPOSE 8080\n",
	} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
	assertMapPerformanceSettings(t, root, "directory")
	git, err := exec.LookPath("git")
	if err != nil {
		t.Skip("Git oracle unavailable; directory invariance tested")
	}
	for _, args := range [][]string{
		{"init", "-q"}, {"add", "-A"},
		{"-c", "user.name=dircue-test", "-c", "user.email=test@example.invalid", "-c", "commit.gpgsign=false", "-c", "core.hooksPath=" + t.TempDir(), "commit", "-qm", "fixture"},
		{"repack", "-ad"},
	} {
		cmd := exec.Command(git, args...)
		cmd.Dir = root
		cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL="+os.DevNull)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("Git fixture %v: %v %s", args, err, out)
		}
	}
	assertMapPerformanceSettings(t, root, "git")
	base, _, err := invoke("map", "--source", "directory", "--json", root)
	if err != nil {
		t.Fatal(err)
	}
	limited, _, err := invoke("map", "--source", "directory", "--json", "--budget-files", "1", root)
	if err != nil {
		t.Fatal(err)
	}
	var doc mapdoc.Document
	if err := json.Unmarshal([]byte(limited), &doc); err != nil {
		t.Fatal(err)
	}
	var disclosed bool
	for _, c := range doc.Coverage {
		if c.Question == "content" && c.Status == mapdoc.CoveragePartial {
			for _, reason := range c.Reasons {
				if reason == "tree_size_limit" {
					disclosed = true
				}
			}
		}
	}
	if !disclosed || limited == base {
		t.Fatalf("coverage-changing limit not disclosed: %s", limited)
	}
}

func assertMapPerformanceSettings(t *testing.T, root, source string) {
	t.Helper()
	base, _, err := invoke("map", "--source", source, "--json", root)
	if err != nil {
		t.Fatal(err)
	}
	for _, settings := range [][]string{
		{"--workers", "1"}, {"--workers", "4"},
		{"--set", "git.object_cache_bytes=1MiB"},
		{"--cpu-limit", "1"}, {"--memory-limit", "64MiB"},
	} {
		args := append([]string{"map", "--source", source, "--json"}, settings...)
		args = append(args, root)
		got, _, err := invoke(args...)
		if err != nil || got != base {
			t.Fatalf("performance preference changed answers: %v err=%v", settings, err)
		}
	}
}
