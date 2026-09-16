package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func metricsFixture(t *testing.T) string {
	t.Helper()
	root := fixture(t)
	for name, content := range map[string]string{
		"go.mod":     "module example.com/demo\n\ngo 1.26\n",
		"events.xml": "<events>\n<event/>\n</events>\n",
	} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestMetricsRequiresOptIn(t *testing.T) {
	root := metricsFixture(t)
	for _, args := range [][]string{{"--json", root}, {"analyze", "languages", "--json", root}, {"analyze", "all", "--json", root}} {
		out, stderr, err := invoke(args...)
		if err != nil || stderr != "" {
			t.Fatalf("%v: %q %v", args, stderr, err)
		}
		if strings.Contains(out, `"metrics"`) {
			t.Fatalf("unexpected metrics: %s", out)
		}
		if args[0] == "analyze" && args[1] == "all" && !strings.Contains(out, `"schema_version": "1.0.0"`) {
			t.Fatalf("changed legacy schema: %s", out)
		}
	}
}

func TestMetricsCommandEnvelopeAndHooks(t *testing.T) {
	root := metricsFixture(t)
	for _, mode := range []string{"metrics", "all"} {
		args := []string{"analyze", mode, "--json", root}
		if mode == "all" {
			args = append(args, "--metrics")
		}
		out, _, err := invoke(args...)
		if err != nil {
			t.Fatal(err)
		}
		var report map[string]any
		if err := json.Unmarshal([]byte(out), &report); err != nil {
			t.Fatal(err)
		}
		if report["schema_version"] != "1.1.0" || report["metrics"] == nil {
			t.Fatalf("missing metrics envelope: %s", out)
		}
		ecosystems := report["ecosystems"].([]any)
		if (mode == "metrics" && len(ecosystems) != 0) || (mode == "all" && len(ecosystems) == 0) {
			t.Fatalf("wrong hooks for %s: %s", mode, out)
		}
	}
}

func TestMetricsFlagValidation(t *testing.T) {
	root := fixture(t)
	for _, args := range [][]string{
		{"--metrics"}, {"--files"}, {"--metrics-scope", "text"},
		{"analyze", "languages", "--metrics"}, {"analyze", "languages", "--files"},
		{"analyze", "frameworks", "--metrics-max-file-bytes", "64"},
		{"analyze", "metrics", "--metrics"},
		{"analyze", "all", "--files"}, {"analyze", "all", "--files=false"},
		{"analyze", "all", "--metrics-scope", "source"},
		{"analyze", "all", "--metrics-max-file-bytes", "16"},
		{"analyze", "all", "--metrics=false", "--files"},
		{"analyze", "metrics", "--metrics-scope", "other"},
		{"analyze", "metrics", "--metrics-max-file-bytes", "0"},
		{"analyze", "metrics", "--metrics-max-file-bytes", "-1"},
		{"analyze", "metrics", "--metrics-max-file-bytes", "268435457"},
	} {
		args = append(args, root)
		out, stderr, err := invoke(args...)
		if err == nil || out != "" || stderr != "" {
			t.Fatalf("%v: out=%q stderr=%q err=%v", args, out, stderr, err)
		}
	}
}

func TestMetricsHelpScope(t *testing.T) {
	for _, mode := range []string{"languages", "frameworks", "ecosystems", "metrics", "all"} {
		out, stderr, err := invoke("analyze", mode, "--help")
		if err != nil || stderr != "" {
			t.Fatal(err)
		}
		wantsMetrics := mode == "metrics" || mode == "all"
		if strings.Contains(out, "--metrics-scope") != wantsMetrics || strings.Contains(out, "--files") != wantsMetrics {
			t.Fatalf("wrong flags for %s: %s", mode, out)
		}
	}
}

func TestMetricsScopeFilesAndTextOutput(t *testing.T) {
	root := metricsFixture(t)
	for _, scope := range []string{"source", "text"} {
		out, _, err := invoke("analyze", "metrics", "--json", "--files", "--metrics-scope", scope, root)
		if err != nil {
			t.Fatal(err)
		}
		var report struct {
			Metrics struct {
				Scope string `json:"scope"`
				Files []struct {
					Path   string `json:"path"`
					Status string `json:"status"`
					Reason string `json:"reason"`
					Counts *struct {
						Lines int `json:"lines"`
						Code  int `json:"code"`
						Blank int `json:"blank"`
					} `json:"counts"`
				} `json:"files"`
			} `json:"metrics"`
		}
		if err := json.Unmarshal([]byte(out), &report); err != nil {
			t.Fatal(err)
		}
		if report.Metrics.Scope != scope {
			t.Fatalf("wrong scope: %s", out)
		}
		foundGo, foundXML := false, false
		for _, file := range report.Metrics.Files {
			switch file.Path {
			case "hello.go":
				foundGo = true
				if file.Status != "counted" || file.Counts == nil || file.Counts.Lines != 3 || file.Counts.Code != 2 || file.Counts.Blank != 1 {
					t.Fatalf("incorrect complete Go counts: %s", out)
				}
			case "events.xml":
				foundXML = true
				if scope == "source" && (file.Status != "skipped" || file.Reason != "outside_scope" || file.Counts != nil) {
					t.Fatalf("XML must be excluded in source scope: %s", out)
				}
				if scope == "text" && (file.Status != "counted" || file.Counts == nil || file.Counts.Lines != 3) {
					t.Fatalf("XML must be counted in text scope: %s", out)
				}
			}
		}
		if !foundGo || !foundXML {
			t.Fatalf("missing file rows: %s", out)
		}
	}
	out, _, err := invoke("analyze", "metrics", "--files", root)
	if err != nil || !strings.Contains(out, "scope: source; status: complete") || !strings.Contains(out, "Complexity") || !strings.Contains(out, `"hello.go"`) || strings.Contains(out, "Ecosystems:") {
		t.Fatalf("metrics text: %q %v", out, err)
	}
	out, _, err = invoke("analyze", "all", "--metrics", root)
	if err != nil || !strings.Contains(out, "Languages:") || !strings.Contains(out, "Ecosystems:") || !strings.Contains(out, "Metrics:") {
		t.Fatalf("all text: %q %v", out, err)
	}
}

func TestMetricsTreeLimitHasUnknownSkipCount(t *testing.T) {
	root := fixture(t)
	out, _, err := invoke("analyze", "metrics", "--tree-size", "1", root)
	if err != nil || !strings.Contains(out, "Skipped: unknown (tree_size_limit)") {
		t.Fatalf("tree limit text: %q %v", out, err)
	}
	out, _, err = invoke("analyze", "metrics", "--tree-size", "1", "--json", root)
	if err != nil {
		t.Fatal(err)
	}
	var report struct {
		Metrics struct {
			Skipped []map[string]json.RawMessage `json:"skipped"`
		} `json:"metrics"`
	}
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatal(err)
	}
	if len(report.Metrics.Skipped) != 1 || report.Metrics.Skipped[0]["files"] != nil {
		t.Fatalf("tree limit must not invent a file count: %s", out)
	}
}
