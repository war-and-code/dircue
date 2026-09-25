package schema_test

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/war-and-code/dircue/internal/cli"
	"github.com/war-and-code/dircue/pkg/reportdiff"
	"github.com/war-and-code/dircue/schema"
)

func TestPartialFocusedMetricsRoundTripThroughSavedExplanation(t *testing.T) {
	tests := []struct {
		name    string
		project string
		files   map[string]string
	}{
		{
			name:    "dotnet",
			project: "app/App.csproj",
			files: map[string]string{
				"app/App.csproj":           `<Project Sdk="Microsoft.NET.Sdk" />`,
				"app/Own.cs":               "class Own {}\n",
				"app/broken/Broken.csproj": "<Project><ItemGroup>\n",
				"app/broken/NotOwned.cs":   "class NotOwned {}\n",
			},
		},
		{
			name:    "python",
			project: "app/pyproject.toml",
			files: map[string]string{
				"app/pyproject.toml":         "[project]\nname = 'app'\nversion = '1'\n",
				"app/main.py":                "VALUE = 1\n",
				"app/generated/Cargo.toml":   "[package]\nname = 'nested'\nversion = '1.0.0'\n",
				"app/generated/not_owned.py": "HIDDEN = 1\n",
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			for name, content := range test.files {
				path := filepath.Join(root, filepath.FromSlash(name))
				if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(content), 0600); err != nil {
					t.Fatal(err)
				}
			}

			var produced, stderr bytes.Buffer
			args := []string{"analyze", "focus", "--project", test.project, "--metrics", "--files", "--source", "directory", "--json", root}
			if err := cli.Execute(context.Background(), args, &produced, &stderr); err != nil {
				t.Fatalf("produce partial focus report: %v (%s)", err, stderr.String())
			}
			value := decodeProfile(t, produced.Bytes())
			marker := partialFocusMarker(t, value)
			if _, found := marker["files"]; found {
				t.Fatal("focus-wide partial marker must not invent a skipped-file count")
			}
			if err := schema.ValidateProfile(value); err != nil {
				t.Fatalf("producer emitted invalid partial focus report: %v", err)
			}
			if _, _, err := reportdiff.ReadEvidence(bytes.NewReader(produced.Bytes())); err != nil {
				t.Fatalf("strict saved-report reader rejected producer output: %v", err)
			}

			saved := filepath.Join(t.TempDir(), "partial-focus.json")
			if err := os.WriteFile(saved, produced.Bytes(), 0600); err != nil {
				t.Fatal(err)
			}
			var explained bytes.Buffer
			stderr.Reset()
			if err := cli.Execute(context.Background(), []string{"analyze", "explain", "--report", saved, "--project", test.project, "--json"}, &explained, &stderr); err != nil {
				t.Fatalf("explain producer's saved report: %v (%s)", err, stderr.String())
			}
			explanation := decodeProfile(t, explained.Bytes())
			if err := schema.ValidateProfile(explanation); err != nil {
				t.Fatalf("saved explanation is invalid: %v", err)
			}
			query := explanation["explanation"].(map[string]any)["query"].(map[string]any)
			if query["kind"] != "project" || query["path"] != test.project {
				t.Fatalf("saved explanation query = %#v", query)
			}

			for _, mutation := range []struct {
				name   string
				mutate func(map[string]any)
			}{
				{"unknown-reason", func(value map[string]any) { partialFocusMarker(t, value)["reason"] = "unknown_focus_scope" }},
				{"invented-file-count", func(value map[string]any) { partialFocusMarker(t, value)["files"] = float64(0) }},
				{"complete-metrics", func(value map[string]any) {
					value["focused_metrics"].(map[string]any)["primary"].(map[string]any)["status"] = "complete"
				}},
				{"complete-focus", func(value map[string]any) { value["focus"].(map[string]any)["status"] = "complete" }},
			} {
				t.Run(mutation.name, func(t *testing.T) {
					forged := decodeProfile(t, produced.Bytes())
					mutation.mutate(forged)
					rejectProfile(t, forged)
				})
			}
		})
	}
}

func TestFocusScopePartialMarkerIsRejectedFromAggregateMetrics(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n"), 0600); err != nil {
		t.Fatal(err)
	}
	value := targetedCLIReport(t, "analyze", "metrics", "--source", "directory", "--json", root)
	metrics := value["metrics"].(map[string]any)
	metrics["status"] = "partial"
	metrics["skipped"] = append(metrics["skipped"].([]any), map[string]any{"reason": "focus_scope_partial"})
	rejectProfile(t, value)
}

func decodeProfile(t *testing.T, data []byte) map[string]any {
	t.Helper()
	var value map[string]any
	if err := json.Unmarshal(data, &value); err != nil {
		t.Fatal(err)
	}
	return value
}

func partialFocusMarker(t *testing.T, value map[string]any) map[string]any {
	t.Helper()
	focused, ok := value["focused_metrics"].(map[string]any)
	if !ok {
		t.Fatal("missing focused_metrics")
	}
	primary, ok := focused["primary"].(map[string]any)
	if !ok || primary["status"] != "partial" {
		t.Fatalf("primary focused metrics are not partial: %#v", primary)
	}
	skipped, ok := primary["skipped"].([]any)
	if !ok {
		t.Fatal("missing primary focused-metrics skips")
	}
	for _, item := range skipped {
		skip, ok := item.(map[string]any)
		if ok && skip["reason"] == "focus_scope_partial" {
			return skip
		}
	}
	t.Fatal("missing focus_scope_partial marker")
	return nil
}

func rejectProfile(t *testing.T, value map[string]any) {
	t.Helper()
	if schema.ValidateProfile(value) == nil {
		t.Fatal("schema accepted malformed focus_scope_partial marker")
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := reportdiff.ReadEvidence(bytes.NewReader(encoded)); err == nil {
		t.Fatal("strict saved-report reader accepted malformed focus_scope_partial marker")
	}
}
