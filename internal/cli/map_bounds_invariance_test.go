package cli

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/war-and-code/dircue/pkg/formats"
	"github.com/war-and-code/dircue/pkg/mapdoc"
	"github.com/war-and-code/dircue/pkg/projects"
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
	for name, want := range map[string]int64{
		"formats.files":        formats.MaxFiles,
		"formats.file_bytes":   formats.MaxFileBytes,
		"formats.input_bytes":  formats.MaxInputBytes,
		"manifests.file_bytes": projects.MaxManifestBytes,
	} {
		s, ok := values[name]
		if !ok || s.Category != "fixed-safety-bound" || s.Minimum != s.Value || s.Maximum != s.Value {
			t.Fatalf("missing fixed bound: %s %+v", name, s)
		}
		parsed, err := strconv.ParseInt(s.Value, 10, 64)
		if err != nil || parsed != want {
			t.Fatalf("bound drift: %s %s", name, s.Value)
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
