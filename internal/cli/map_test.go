package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"dircue/pkg/mapdoc"
)

func TestMapOneShotPortableEvidenceAndBudget(t *testing.T) {
	root := t.TempDir()
	for name, content := range map[string]string{
		"go.mod":    "module example.test/app\n\ngo 1.22\n",
		"main.go":   "package main\nfunc main() {}\n",
		"README.md": "This claims Redis and an HTTP /admin route.\n",
	} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	out, stderr, err := invoke("map", "--source", "directory", "--json", root)
	if err != nil || stderr != "" {
		t.Fatalf("map: %v %s", err, stderr)
	}
	var d mapdoc.Document
	if err := json.Unmarshal([]byte(out), &d); err != nil {
		t.Fatal(err)
	}
	if err := mapdoc.Validate(d); err != nil {
		t.Fatal(err)
	}
	if len(d.Nodes) == 0 || d.Source.Mode != "directory" || strings.Contains(out, root) {
		t.Fatalf("map missing nodes, wrong source or leaks root: %s", out)
	}
	for _, n := range d.Nodes {
		if n.Kind == mapdoc.NodeCapability && strings.Contains(strings.ToLower(n.Name), "redis") || n.Kind == mapdoc.NodeInterface && strings.Contains(n.Name, "/admin") {
			t.Fatalf("documentation became non-documentation evidence: %+v", n)
		}
	}
	other, _, err := invoke("map", "--source", "directory", "--json", root)
	if err != nil || other != out {
		t.Fatal("map was not deterministic")
	}
	limited, _, err := invoke("map", "--source", "directory", "--json", "--budget-files", "1", root)
	if err != nil {
		t.Fatalf("budget should return explicit partial map with exit zero: %v", err)
	}
	var partial mapdoc.Document
	if err := json.Unmarshal([]byte(limited), &partial); err != nil || partial.Status != mapdoc.CoveragePartial {
		t.Fatalf("budget result = %s; err=%v", limited, err)
	}
	var reason bool
	for _, q := range partial.Coverage {
		if q.Question == "content" && q.Status != mapdoc.CoverageComplete && strings.Contains(strings.Join(q.Reasons, ","), "tree_size_limit") {
			reason = true
		}
	}
	if !reason {
		t.Fatalf("budget crossing lacks explicit content reason: %s", limited)
	}
}

func TestMapSummaryAndFormatSelection(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "notes.txt"), []byte("plain notes\n"), 0600); err != nil {
		t.Fatal(err)
	}
	summary, stderr, err := invoke("map", "--summary", root)
	if err != nil || stderr != "" || !strings.Contains(summary, "Directory map") || len(strings.Split(strings.TrimSpace(summary), "\n")) > 40 {
		t.Fatalf("summary: %q %q %v", summary, stderr, err)
	}
	if _, _, err := invoke("map", "--summary", "--json", root); err == nil {
		t.Fatal("ambiguous output selectors accepted")
	}
	if _, _, err := invoke("map", "--budget-files", "2", "--tree-size", "2", root); err == nil {
		t.Fatal("conflicting inventory limits accepted")
	}
}

func TestMapFileByteLimitQualifiesContentCoverage(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n"), 0600); err != nil {
		t.Fatal(err)
	}
	out, _, err := invoke("map", "--source", "directory", "--json", "--max-file-bytes", "1", root)
	if err != nil {
		t.Fatal(err)
	}
	var d mapdoc.Document
	if err := json.Unmarshal([]byte(out), &d); err != nil {
		t.Fatal(err)
	}
	if d.Status != mapdoc.CoveragePartial {
		t.Fatalf("file byte limit failed to qualify document: %s", out)
	}
	for _, q := range d.Coverage {
		if q.Question == "content" {
			if q.Status != mapdoc.CoveragePartial || !strings.Contains(strings.Join(q.Reasons, ","), "file_too_large") {
				t.Fatalf("content coverage lost file byte limit: %+v", q)
			}
			return
		}
	}
	t.Fatal("missing content coverage")
}
