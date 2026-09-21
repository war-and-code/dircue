package schema_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"dircue/schema"
)

func cloneProfile(t *testing.T, value map[string]any) map[string]any {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func rejectProfileMutations(t *testing.T, base map[string]any, object func(map[string]any) map[string]any, required string) {
	t.Helper()
	if err := schema.ValidateProfile(base); err != nil {
		t.Fatalf("actual output rejected before mutation: %v", err)
	}
	unknown := cloneProfile(t, base)
	object(unknown)["unexpected_contract_field"] = true
	if schema.ValidateProfile(unknown) == nil {
		t.Fatal("unknown nested field accepted")
	}
	missing := cloneProfile(t, base)
	delete(object(missing), required)
	if schema.ValidateProfile(missing) == nil {
		t.Fatalf("missing required nested field %q accepted", required)
	}
}

func writeContractFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	files := map[string]string{
		"main.go":               "package main\nfunc main() {}\n",
		"App.csproj":            `<Project Sdk="Microsoft.NET.Sdk"/>`,
		"Directory.Build.props": `<Project/>`,
		"asset.bin":             "version https://git-lfs.github.com/spec/v1\noid sha256:" + strings.Repeat("a", 64) + "\nsize 12\n",
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestActualNestedReportsRejectUnknownAndMissingFields(t *testing.T) {
	root := writeContractFixture(t)
	t.Run("profile language", func(t *testing.T) {
		v := targetedCLIReport(t, "analyze", "all", "--json", "--breakdown", root)
		rejectProfileMutations(t, v, func(v map[string]any) map[string]any {
			return v["languages"].([]any)[0].(map[string]any)
		}, "name")
	})
	t.Run("projects project", func(t *testing.T) {
		v := targetedCLIReport(t, "analyze", "projects", "--json", root)
		rejectProfileMutations(t, v, func(v map[string]any) map[string]any {
			return v["projects"].(map[string]any)["projects"].([]any)[0].(map[string]any)
		}, "id")
	})
	t.Run("availability lfs", func(t *testing.T) {
		v := targetedCLIReport(t, "analyze", "availability", "--json", root)
		rejectProfileMutations(t, v, func(v map[string]any) map[string]any {
			return v["availability"].(map[string]any)["lfs"].([]any)[0].(map[string]any)
		}, "path")
	})
	t.Run("focus context", func(t *testing.T) {
		v := targetedCLIReport(t, "analyze", "focus", "--project", "App.csproj", "--json", root)
		rejectProfileMutations(t, v, func(v map[string]any) map[string]any {
			return v["focus"].(map[string]any)["context"].([]any)[0].(map[string]any)
		}, "path")
	})
	t.Run("explanation step", func(t *testing.T) {
		v := targetedCLIReport(t, "analyze", "explain", "--file", "main.go", "--json", root)
		rejectProfileMutations(t, v, func(v map[string]any) map[string]any {
			return v["explanation"].(map[string]any)["steps"].([]any)[0].(map[string]any)
		}, "rule")
	})
}
