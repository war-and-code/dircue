package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMapSettingsResolvesPresetAndOverride(t *testing.T) {
	out, stderr, err := invoke("map", "settings", "--preset", "fast", "--set", "workers=8", "--json")
	if err != nil || stderr != "" {
		t.Fatalf("stderr=%q err=%v", stderr, err)
	}
	var report mapSettingsReport
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatal(err)
	}
	if report.Kind != "map_settings" || report.Preset != "fast" || len(report.Settings) != 3 {
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
	if !strings.Contains(strings.Join(report.Notes, " "), "not a measured RSS guarantee") {
		t.Fatalf("resource qualification missing: %+v", report.Notes)
	}
}

func TestMapSettingsPresetValuesAndValidation(t *testing.T) {
	for preset, workers := range map[string]string{"balanced": "0", "fast": "16", "low-memory": "2", "thorough": "0"} {
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
	}
	for _, args := range [][]string{
		{"map", "settings", "--preset", "turbo"},
		{"map", "settings", "--set", "workers=-1"},
		{"map", "settings", "--set", "invented=1"},
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
		{"--preset", "fast"},
		{"--preset", "low-memory"},
		{"--preset", "balanced", "--set", "workers=3"},
		{"--preset", "fast", "--set", "workers=7", "--workers", "1"},
	} {
		args := append([]string{"map", "--source", "directory"}, flags...)
		args = append(args, "--json", root)
		got, gotStderr, gotErr := invoke(args...)
		if gotErr != nil || gotStderr != "" || got != baseline {
			t.Fatalf("%v changed answer: stderr=%q err=%v\nbase=%s\ngot=%s", flags, gotStderr, gotErr, baseline, got)
		}
	}
}
