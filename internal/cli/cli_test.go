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

	"github.com/war-and-code/dircue/pkg/profile"
)

const goSource = "package sample\n\nfunc Hello() string { return \"hello\" }\n"

func fixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "hello.go"), []byte(goSource), 0600); err != nil {
		t.Fatal(err)
	}
	return root
}

func invoke(args ...string) (string, string, error) {
	var out, errOut bytes.Buffer
	err := Execute(context.Background(), args, &out, &errOut)
	return out.String(), errOut.String(), err
}

func TestLegacyAndStructuredLanguageRouting(t *testing.T) {
	root := fixture(t)
	for _, args := range [][]string{
		{root, "--json"}, {"--json", root}, {"-j", "-b", root}, {root, "-bj"},
		{"analyze", "languages", root, "--json"},
		{"--json", "analyze", "languages", root},
		{"--workers", "1", root, "--json"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			out, stderr, err := invoke(args...)
			if err != nil {
				t.Fatal(err)
			}
			if stderr != "" {
				t.Fatalf("unexpected stderr: %s", stderr)
			}
			var result map[string]struct {
				Size       int64    `json:"size"`
				Percentage string   `json:"percentage"`
				Files      []string `json:"files"`
			}
			if err := json.Unmarshal([]byte(out), &result); err != nil {
				t.Fatalf("%v: %s", err, out)
			}
			if strings.Count(out, "\n") != 1 {
				t.Fatalf("legacy JSON must be a single line: %q", out)
			}
			if len(result) != 1 || result["Go"].Size != int64(len(goSource)) || result["Go"].Percentage != "100.00" {
				t.Fatalf("unexpected legacy result: %s", out)
			}
			breakdown := strings.Contains(strings.Join(args, " "), "-b")
			if breakdown && (len(result["Go"].Files) != 1 || result["Go"].Files[0] != "hello.go") {
				t.Fatalf("missing breakdown: %s", out)
			}
			if !breakdown && strings.Contains(out, "files") {
				t.Fatalf("unexpected files: %s", out)
			}
		})
	}
}

func TestDefaultDirectoryAndReservedDirectoryNames(t *testing.T) {
	root := fixture(t)
	t.Chdir(root)
	out, stderr, err := invoke()
	if err != nil || stderr != "" || !strings.Contains(out, "100.00%") {
		t.Fatalf("no arguments: %q %q %v", out, stderr, err)
	}
	out, _, err = invoke("--json")
	if err != nil || !strings.Contains(out, `"Go"`) {
		t.Fatalf("default path: %s %v", out, err)
	}
	for _, name := range []string{"analyze", "help", "-odd", "some checkout"} {
		if err := os.Mkdir(name, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(name, "hello.go"), []byte(goSource), 0600); err != nil {
			t.Fatal(err)
		}
		out, _, err := invoke("--json", "--", name)
		if err != nil || !strings.Contains(out, `"Go"`) {
			t.Fatalf("directory %q: %s %v", name, out, err)
		}
	}
	out, _, err = invoke("some checkout", "--json")
	if err != nil || !strings.Contains(out, `"Go"`) {
		t.Fatalf("relative path: %s %v", out, err)
	}
}

func TestPlainAndBreakdownOutput(t *testing.T) {
	out, _, err := invoke(fixture(t), "--breakdown")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(out, "100.00% ") || !strings.Contains(out, " Go\n\nGo:\n  hello.go\n\n") {
		t.Fatalf("unexpected breakdown: %q", out)
	}
}

func TestStructuredProfileAndFilteredOutputs(t *testing.T) {
	root := fixture(t)
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.org/sample\n\ngo 1.24\n"), 0600); err != nil {
		t.Fatal(err)
	}
	out, stderr, err := invoke("analyze", "all", "--json", root)
	if err != nil {
		t.Fatal(err)
	}
	if stderr != "" {
		t.Fatalf("unexpected stderr: %s", stderr)
	}
	var result profile.Report
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "\n  \"schema_version\"") {
		t.Fatalf("structured profile JSON should be indented: %q", out)
	}
	if result.SchemaVersion != profile.SchemaVersion || len(result.Languages) == 0 || len(result.Ecosystems) == 0 {
		t.Fatalf("incomplete profile: %s", out)
	}
	for _, mode := range []string{"ecosystems", "frameworks"} {
		out, _, err := invoke("analyze", mode, root, "--json")
		if err != nil {
			t.Fatal(err)
		}
		var findings []profile.Finding
		if err := json.Unmarshal([]byte(out), &findings); err != nil || findings == nil {
			t.Fatalf("expected array, got %q: %v", out, err)
		}
	}
}

func TestEmptyDirectory(t *testing.T) {
	root := t.TempDir()
	out, stderr, err := invoke(root, "--json")
	if err != nil || stderr != "" || strings.TrimSpace(out) != "{}" {
		t.Fatalf("empty legacy output %q %q %v", out, stderr, err)
	}
	out, _, err = invoke(root)
	if err != nil || out != "" {
		t.Fatalf("empty text output %q %v", out, err)
	}
	out, _, err = invoke(root, "--breakdown")
	if err != nil || out != "\n" {
		t.Fatalf("empty breakdown output %q %v", out, err)
	}
	out, _, err = invoke("analyze", "all", root, "--json")
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(out), &raw); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"languages", "ecosystems", "frameworks", "layouts", "warnings"} {
		if string(raw[key]) != "[]" {
			t.Errorf("%s must be empty array, got %s", key, raw[key])
		}
	}
}

func TestInvalidArguments(t *testing.T) {
	for _, args := range [][]string{
		{"one", "two"}, {"analyze", "languages", "one", "two"},
		{"analyze", "langauges"}, {"analyze"}, {"--source", "unknown"},
		{"--source", "directory", "--rev", "HEAD"},
		{"--workers=-1"}, {"--max-file-bytes=-1"}, {"--not-a-flag"},
	} {
		out, stderr, err := invoke(args...)
		if err == nil {
			t.Errorf("%v should fail", args)
		}
		if out != "" || stderr != "" {
			t.Errorf("errors should be returned, not printed for %v: %q %q", args, out, stderr)
		}
	}
	root := t.TempDir()
	_, _, err := invoke(filepath.Join(root, "missing"))
	if err == nil {
		t.Fatal("missing path should fail")
	}
}

func TestHelp(t *testing.T) {
	for _, args := range [][]string{{"--help"}, {"analyze", "--help"}, {"analyze", "all", "--help"}} {
		out, stderr, err := invoke(args...)
		if err != nil || stderr != "" || !strings.Contains(out, "Usage:") {
			t.Fatalf("help %v: %q %q %v", args, out, stderr, err)
		}
	}
}

func TestCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var out, errOut bytes.Buffer
	err := Execute(ctx, []string{fixture(t)}, &out, &errOut)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected cancellation, got %v", err)
	}
	if out.Len() != 0 {
		t.Fatalf("unexpected partial stdout: %s", out.String())
	}
}

func TestVersion(t *testing.T) {
	out, stderr, err := invoke("--version")
	if err != nil || stderr != "" || out != "dircue "+Version+"\n" {
		t.Fatalf("version: %q %q %v", out, stderr, err)
	}
}

func TestContentSourceAndRevisionValidation(t *testing.T) {
	root := fixture(t)
	for _, args := range [][]string{
		{"--source", "directory", "--max-file-bytes", "0", "--json", root},
		{"--source", "auto", "--json", root},
	} {
		out, stderr, err := invoke(args...)
		if err != nil || stderr != "" || !strings.Contains(out, `"Go"`) {
			t.Fatalf("source %v: %q %q %v", args, out, stderr, err)
		}
	}
	for _, args := range [][]string{
		{"--rev", "HEAD", root},
		{"--source", "git", root},
		{"--rev", "HEAD", "--source", "directory", root},
		{"--source", "directory", "--rev", "", root},
	} {
		out, _, err := invoke(args...)
		if err == nil || out != "" {
			t.Fatalf("invalid source %v: %q %v", args, out, err)
		}
	}
}

func TestSingleFileOutput(t *testing.T) {
	root := fixture(t)
	filename := filepath.Join(root, "hello.go")
	out, stderr, err := invoke("-js", filename)
	if err != nil || stderr != "" {
		t.Fatalf("file: %q %q %v", out, stderr, err)
	}
	var result map[string]fileOutput
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatal(err)
	}
	got := result[filename]
	if got.Lines != 3 || got.SLOC != 2 || got.Type != "Text" || got.MIME != "text/plain" || got.Language == nil || *got.Language != "Go" {
		t.Fatalf("file result: %s", out)
	}
	out, _, err = invoke("-s", filename)
	if err != nil || !strings.Contains(out, "  strategy:  Extension\n") {
		t.Fatalf("file strategy: %q %v", out, err)
	}
	out, _, err = invoke("analyze", "all", filename)
	if err == nil || out != "" {
		t.Fatalf("structured file: %q %v", out, err)
	}
}

func TestStrategiesTextAndJSON(t *testing.T) {
	root := fixture(t)
	out, _, err := invoke("-s", root)
	if err != nil || !strings.Contains(out, "  hello.go [Extension]\n") {
		t.Fatalf("strategies: %q %v", out, err)
	}
	out, _, err = invoke("-js", root)
	if err != nil || strings.Contains(out, "files") || strings.Contains(out, "Extension") {
		t.Fatalf("strategy JSON must stay language stats: %q %v", out, err)
	}
	out, _, err = invoke("-jbs", root)
	if err != nil || !strings.Contains(out, `"files":["hello.go"]`) || strings.Contains(out, "Extension") {
		t.Fatalf("strategy breakdown JSON: %q %v", out, err)
	}
}

func TestLinguistSummaryTieAndBreakdownOrders(t *testing.T) {
	root := t.TempDir()
	for filename, content := range map[string]string{"a.py": "print(1)\n", "b.go": "package\n\n"} {
		if err := os.WriteFile(filepath.Join(root, filename), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	out, _, err := invoke("-b", root)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(out, "\n")
	if !strings.HasSuffix(lines[0], " Go") || !strings.HasSuffix(lines[1], " Python") || strings.Index(out, "Python:\n") > strings.Index(out, "Go:\n") {
		t.Fatalf("expected reverse encounter order for equal-size summary and encounter order for breakdown: %q", out)
	}
}
