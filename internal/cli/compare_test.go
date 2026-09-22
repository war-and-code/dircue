package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"dircue/pkg/reportdiff"
)

func compareReports(t *testing.T) (string, string) {
	t.Helper()
	root := t.TempDir()
	manifest := filepath.Join(root, "go.mod")
	if err := os.WriteFile(manifest, []byte("module example.org/app\ngo 1.24.0\n"), 0600); err != nil {
		t.Fatal(err)
	}
	base, _, err := invoke("analyze", "declarations", "--json", "--source", "directory", root)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manifest, []byte("module example.org/app\ngo 1.25.0\n"), 0600); err != nil {
		t.Fatal(err)
	}
	head, _, err := invoke("analyze", "declarations", "--json", "--source", "directory", root)
	if err != nil {
		t.Fatal(err)
	}
	output := t.TempDir()
	basePath, headPath := filepath.Join(output, "base.json"), filepath.Join(output, "head.json")
	if err := os.WriteFile(basePath, []byte(base), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(headPath, []byte(head), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(root); err != nil {
		t.Fatal(err)
	}
	return basePath, headPath
}

func TestCompareCLIOfflineChangesReturnSuccess(t *testing.T) {
	base, head := compareReports(t)
	for _, args := range [][]string{{"compare", base, head, "--json"}, {"--json", "compare", base, head}} {
		out, _, err := invoke(args...)
		if err != nil {
			t.Fatal(err)
		}
		var report reportdiff.Report
		if err := json.Unmarshal([]byte(out), &report); err != nil {
			t.Fatal(err)
		}
		found := false
		for _, module := range report.Modules {
			if module.Name == "declarations" {
				found = module.Status == "changed" && module.Counts.Changed == 1 && module.Compatibility == "compatible"
			}
		}
		if !found {
			t.Fatalf("declaration change absent: %s", out)
		}
	}
	out, _, err := invoke("compare", base, head)
	if err != nil || !strings.Contains(out, "declarations: changed (compatible)") || !strings.Contains(out, `"go.mod"`) {
		t.Fatalf("text: %s %v", out, err)
	}
}

func TestComparisonTextShowsCompositeIdentityWithoutChangingJSONID(t *testing.T) {
	const key = "4:Java8:core:api2:./8:detector"
	report := &reportdiff.Report{
		Status: "complete",
		Modules: []reportdiff.Module{{
			Name: "frameworks", Status: "changed", Compatibility: "observed_only",
			BaseStatus: "complete", HeadStatus: "complete",
			Changes: []reportdiff.Change{{ID: key, Status: "changed"}},
		}},
	}
	var out bytes.Buffer
	if err := writeComparison(&out, report); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `changed ("Java", "core:api", "./", "detector")`) || strings.Contains(out.String(), key) {
		t.Fatalf("composite key leaked into text: %s", out.String())
	}
	data, err := json.Marshal(report)
	if err != nil || !bytes.Contains(data, []byte(key)) {
		t.Fatalf("structured ID changed: %s: %v", data, err)
	}
	if got := comparisonTextID("languages", "4:Java"); got != `"4:Java"` {
		t.Fatalf("plain language ID was decoded: %s", got)
	}
	if got := comparisonTextID("registries", "2:é3:npm"); got != `("é", "npm")` {
		t.Fatalf("byte-length key was not rendered: %s", got)
	}
	if got := comparisonTextID("focused_metrics_related", "project:svc:language:2:Go0:"); got != `project "svc" language ("Go", "")` {
		t.Fatalf("related metrics identity was not rendered: %s", got)
	}
	if got := comparisonTextID("frameworks", "4:Java9:core:api2:./9:detector"); got != `"4:Java9:core:api2:./9:detector"` {
		t.Fatalf("malformed key was decoded: %s", got)
	}
}

func TestCompareCLIRejectsIrrelevantScanFlags(t *testing.T) {
	base, head := compareReports(t)
	for _, flag := range []string{"--source=directory", "--source=auto", "--rev=HEAD", "--tree-size=100000", "--workers=0", "--breakdown", "--strategies", "--max-file-bytes=0"} {
		if _, _, err := invoke("compare", base, head, flag); err == nil || !strings.Contains(err.Error(), "does not apply") {
			t.Fatalf("accepted flag %s: %v", flag, err)
		}
	}
	for _, args := range [][]string{{"compare"}, {"compare", base}, {"compare", base, head, head}} {
		if _, _, err := invoke(args...); err == nil {
			t.Fatalf("accepted args %v", args)
		}
	}
}

func TestCompareCLIFailsClosedWithoutPayloadDisclosure(t *testing.T) {
	base, head := compareReports(t)
	bad := filepath.Join(t.TempDir(), "bad.json")
	if err := os.WriteFile(bad, []byte(`{"SECRET_MARKER":`), 0600); err != nil {
		t.Fatal(err)
	}
	out, _, err := invoke("compare", bad, head, "--json")
	if err == nil || out != "" || strings.Contains(err.Error(), "SECRET_MARKER") {
		t.Fatalf("invalid report: %q %v", out, err)
	}
	for _, path := range []string{filepath.Dir(base), filepath.Join(t.TempDir(), "missing")} {
		if _, _, err := invoke("compare", path, head); err == nil {
			t.Fatalf("accepted nonreport: %s", path)
		}
	}
	link := filepath.Join(t.TempDir(), "link.json")
	if err := os.Symlink(base, link); err == nil {
		if _, _, err := invoke("compare", link, head); err == nil {
			t.Fatal("report symlink accepted")
		}
	}
}

type comparisonBrokenWriter struct{}

func (comparisonBrokenWriter) Write([]byte) (int, error) { return 0, errors.New("write failed") }

func TestCompareCLIPropagatesOutputAndCancellationErrors(t *testing.T) {
	base, head := compareReports(t)
	for _, flags := range [][]string{{}, {"--json"}} {
		args := append([]string{"compare", base, head}, flags...)
		if err := Execute(context.Background(), args, comparisonBrokenWriter{}, &bytes.Buffer{}); err == nil {
			t.Fatal("output error swallowed")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := Execute(ctx, []string{"compare", base, head}, &bytes.Buffer{}, &bytes.Buffer{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation: %v", err)
	}
}

func TestCompareAcceptsRealAggregateOptInReports(t *testing.T) {
	root := t.TempDir()
	for name, content := range map[string]string{"go.mod": "module example.org/app\ngo 1.24.0\n", "main.go": "package main\nfunc main() {}\n", ".npmrc": "registry=https://registry.npmjs.org\n", "README.md": "Example directory.\n"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	for _, args := range [][]string{
		{"analyze", "all", "--json", "--source", "directory", root},
		{"analyze", "all", "--metrics", "--json", "--source", "directory", root},
		{"analyze", "all", "--projects", "--metrics", "--json", "--source", "directory", root},
		{"analyze", "all", "--projects", "--metrics", "--discovery", "--registries", "--json", "--source", "directory", root},
		{"analyze", "all", "--projects", "--declarations", "--metrics", "--discovery", "--registries", "--json", "--source", "directory", root},
	} {
		out, _, err := invoke(args...)
		if err != nil {
			t.Fatal(err)
		}
		snapshot, err := reportdiff.Load(strings.NewReader(out))
		if err != nil {
			t.Fatalf("%v: %v", args, err)
		}
		compared, err := reportdiff.Compare(snapshot, snapshot)
		if err != nil {
			t.Fatal(err)
		}
		for _, module := range compared.Modules {
			if module.Counts.Added+module.Counts.Removed+module.Counts.Changed != 0 {
				t.Fatalf("self comparison changed %s", module.Name)
			}
		}
	}
}
