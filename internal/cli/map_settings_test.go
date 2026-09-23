package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strings"
	"testing"
)

func TestMapSettingsResolvesPresetAndOverride(t *testing.T) {
	out, stderr, err := invoke("map", "settings", "--preset", "balanced", "--set", "workers=8", "--set", "git.object_cache_bytes=128MiB", "--json")
	if err != nil || stderr != "" {
		t.Fatalf("stderr=%q err=%v", stderr, err)
	}
	var report mapSettingsReport
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatal(err)
	}
	if report.Kind != "map_settings" || report.Preset != "balanced" || len(report.Settings) != 7 {
		t.Fatalf("unexpected report: %+v", report)
	}
	byName := map[string]mapEffectiveSetting{}
	for _, setting := range report.Settings {
		byName[setting.Name] = setting
	}
	if got := byName["workers"]; got.Value != "8" || got.Category != "performance-only" || got.Origin != "--set" {
		t.Fatalf("workers = %+v", got)
	}
	if got := byName["inventory.files"]; got.Value != "100000" || got.Category != "coverage-affecting" {
		t.Fatalf("inventory.files = %+v", got)
	}
	if got := byName["git.object_cache_bytes"]; got.Value != "134217728" || got.Category != "performance-only" || got.Origin != "--set" {
		t.Fatalf("custom Git cache = %+v", got)
	}
	if got := byName["classification.prefix_bytes"]; got.Value != "131072" || got.Category != "conformance-locked" || got.Origin != "fixed:linguist-parity" {
		t.Fatalf("classifier window = %+v", got)
	}
	if !strings.Contains(strings.Join(report.Notes, " "), "not an RSS guarantee") {
		t.Fatalf("resource qualification missing: %+v", report.Notes)
	}
}

func TestMapSettingsPresetValuesAndValidation(t *testing.T) {
	for preset, workers := range map[string]string{"balanced": "0", "low-memory": "2", "thorough": "0"} {
		out, _, err := invoke("map", "settings", "--preset", preset, "--json")
		if err != nil {
			t.Fatalf("%s: %v", preset, err)
		}
		var report mapSettingsReport
		if err := json.Unmarshal([]byte(out), &report); err != nil {
			t.Fatal(err)
		}
		if report.Settings[0].Name != "workers" || report.Settings[0].Value != workers {
			t.Fatalf("%s: %+v", preset, report.Settings)
		}
		if preset == "low-memory" && (report.Settings[1].Name != "git.object_cache_bytes" || report.Settings[1].Value != "8388608") {
			t.Fatalf("low-memory retained Git cache is not reduced: %+v", report.Settings)
		}
	}
	for _, args := range [][]string{
		{"map", "settings", "--preset", "turbo"},
		{"map", "settings", "--preset", "fast"},
		{"map", "settings", "--set", "workers=-1"},
		{"map", "settings", "--set", "invented=1"},
		{"map", "settings", "--set", "classification.prefix_bytes=65536"},
		{"map", "settings", "--set", "git.object_cache_bytes=0"},
		{"map", "settings", "--set", "git.object_cache_bytes=3GiB"},
		{"map", "settings", "--cpu-limit", "-1"},
		{"map", "settings", "--memory-limit", "nope"},
		{"map", "settings", "--memory-limit", "1"},
		{"map", "settings", "--set", "runtime.memory_bytes=-1"},
		{"map", "settings", "--source", "directory"},
	} {
		if _, _, err := invoke(args...); err == nil {
			t.Fatalf("invalid settings accepted: %v", args)
		}
	}
}

func TestMapPresetAndWorkerOverridePreserveAnswers(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\nfunc main() {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	baseline, stderr, err := invoke("map", "--source", "directory", "--preset", "balanced", "--json", root)
	if err != nil || stderr != "" {
		t.Fatalf("baseline stderr=%q err=%v", stderr, err)
	}
	for _, flags := range [][]string{
		{"--preset", "low-memory"},
		{"--preset", "balanced", "--set", "workers=3"},
		{"--set", "workers=16", "--set", "git.object_cache_bytes=128MiB", "--workers", "1"},
		{"--set", "runtime.cpu=2", "--set", "runtime.memory_bytes=64MiB"},
		{"--cpu-limit", "1", "--memory-limit", "67108864"},
	} {
		args := append([]string{"map", "--source", "directory"}, flags...)
		args = append(args, "--json", root)
		got, gotStderr, gotErr := invoke(args...)
		if gotErr != nil || gotStderr != "" || !sameMapAnswer(t, baseline, got) {
			t.Fatalf("%v changed answer: stderr=%q err=%v\nbase=%s\ngot=%s", flags, gotStderr, gotErr, baseline, got)
		}
	}
}

func TestMapRuntimeLimitsPrecedenceProvenanceAndRestoration(t *testing.T) {
	out, stderr, err := invoke("map", "settings",
		"--set", "runtime.cpu=3", "--set", "runtime.memory_bytes=32MiB",
		"--cpu-limit", "2", "--memory-limit", "64MiB", "--json")
	if err != nil || stderr != "" {
		t.Fatalf("stderr=%q err=%v", stderr, err)
	}
	var report mapSettingsReport
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatal(err)
	}
	byName := map[string]mapEffectiveSetting{}
	for _, setting := range report.Settings {
		byName[setting.Name] = setting
	}
	if got := byName["runtime.cpu"]; got.Value != "2" || got.Origin != "--cpu-limit" || got.Minimum != "0" || got.Maximum != "1024" {
		t.Fatalf("CPU setting = %+v", got)
	}
	if got := byName["runtime.memory_bytes"]; got.Value != "67108864" || got.Origin != "--memory-limit" || !strings.Contains(got.Description, "Cooperative") {
		t.Fatalf("memory setting = %+v", got)
	}

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	beforeCPU := runtime.GOMAXPROCS(0)
	beforeMemory := debug.SetMemoryLimit(-1)
	restore := applyMapRuntimeSettings(resolvedMapSettings{CPULimit: 1, MemoryLimit: 64 << 20})
	if got := runtime.GOMAXPROCS(0); got != 1 {
		restore()
		t.Fatalf("CPU limit was not applied: %d", got)
	}
	if got := debug.SetMemoryLimit(-1); got != 64<<20 {
		restore()
		t.Fatalf("memory limit was not applied: %d", got)
	}
	restore()
	if got := runtime.GOMAXPROCS(0); got != beforeCPU {
		t.Fatalf("direct GOMAXPROCS restore failed: %d", got)
	}
	if got := debug.SetMemoryLimit(-1); got != beforeMemory {
		t.Fatalf("direct memory restore failed: %d", got)
	}
	mapJSON, mapStderr, err := invoke("map", "--source", "directory", "--cpu-limit", "1", "--memory-limit", "64MiB", "--json", root)
	if err != nil || mapStderr != "" {
		t.Fatalf("stderr=%q err=%v", mapStderr, err)
	}
	if after := runtime.GOMAXPROCS(0); after != beforeCPU {
		t.Fatalf("GOMAXPROCS leaked: before=%d after=%d", beforeCPU, after)
	}
	if after := debug.SetMemoryLimit(-1); after != beforeMemory {
		t.Fatalf("memory limit leaked: before=%d after=%d", beforeMemory, after)
	}
	var document map[string]json.RawMessage
	if err := json.Unmarshal([]byte(mapJSON), &document); err != nil {
		t.Fatal(err)
	}
	if _, exists := document["execution"]; exists {
		t.Fatal("execution settings must not alter the portable map document; inspect map settings instead")
	}
}

func sameMapAnswer(t *testing.T, left, right string) bool {
	t.Helper()
	return left == right
}
