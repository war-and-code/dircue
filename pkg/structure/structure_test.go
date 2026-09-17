package structure

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
	"time"
)

func validResponse(path, language string, n int) map[string]any {
	observations := map[string]uint64{}
	for _, key := range capabilities[language].Observations {
		observations[key] = 0
	}
	observations["syntax_nodes"] = 1
	grammar := capabilities[language].Grammar
	metrics := map[string]any{}
	for _, group := range capabilities[language].MetricGroups {
		metrics[group] = map[string]any{"value": 0}
	}
	return map[string]any{"path": path, "language": language, "status": "complete", "source_bytes": n, "parse_count": 1, "syntax_errors": false, "observations": observations, "metrics": metrics, "provenance": map[string]string{"bca": "big-code-analysis@2.2.0", "tree_sitter": "0.26.12", "grammar": grammar}, "timings_ns": map[string]int{"parse": 123}}
}

func TestMain(m *testing.M) {
	if mode := os.Getenv("DIRCUE_STRUCTURE_TEST_HELPER"); mode != "" {
		switch mode {
		case "timeout":
			time.Sleep(time.Minute)
		case "overflow":
			fmt.Print(strings.Repeat("x", 17<<20))
		case "failure":
			fmt.Fprint(os.Stderr, "intentional worker failure")
			os.Exit(3)
		case "invalid":
			fmt.Print("[]")
		default:
			var req struct{ Path, Language, Source string }
			body, _ := io.ReadAll(os.Stdin)
			if json.Unmarshal(body, &req) != nil {
				os.Exit(4)
			}
			json.NewEncoder(os.Stdout).Encode(validResponse(req.Path, req.Language, len(req.Source)))
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func TestContractRejections(t *testing.T) {
	changes := map[string]func(map[string]any){
		"path":                  func(v map[string]any) { v["path"] = "other.java" },
		"language":              func(v map[string]any) { v["language"] = "C#" },
		"bytes":                 func(v map[string]any) { v["source_bytes"] = 7 },
		"missing bytes":         func(v map[string]any) { delete(v, "source_bytes") },
		"parse":                 func(v map[string]any) { v["parse_count"] = 2 },
		"version":               func(v map[string]any) { v["provenance"].(map[string]string)["bca"] = "big-code-analysis@3" },
		"grammar":               func(v map[string]any) { v["provenance"].(map[string]string)["grammar"] = "other" },
		"runtime":               func(v map[string]any) { v["provenance"].(map[string]string)["tree_sitter"] = "other" },
		"observations":          func(v map[string]any) { v["observations"] = nil },
		"metrics":               func(v map[string]any) { v["metrics"] = []int{} },
		"metrics missing group": func(v map[string]any) { delete(v["metrics"].(map[string]any), "abc") },
		"metrics extra group":   func(v map[string]any) { v["metrics"].(map[string]any)["extra"] = map[string]int{"value": 1} },
		"metrics string":        func(v map[string]any) { v["metrics"].(map[string]any)["abc"] = map[string]string{"value": "wrong"} },
		"metrics boolean":       func(v map[string]any) { v["metrics"].(map[string]any)["abc"] = map[string]bool{"value": true} },
		"metrics array":         func(v map[string]any) { v["metrics"].(map[string]any)["abc"] = []int{1} },
		"metrics empty group":   func(v map[string]any) { v["metrics"].(map[string]any)["abc"] = map[string]int{} },

		"partial contradiction": func(v map[string]any) { v["status"] = "partial" },
		"syntax contradiction":  func(v map[string]any) { v["observations"].(map[string]uint64)["error_nodes"] = 1 },
	}
	for name, change := range changes {
		t.Run(name, func(t *testing.T) {
			v := validResponse("a.java", "Java", 0)
			change(v)
			data, _ := json.Marshal(v)
			if _, err := decode(data, File{Path: "a.java", Language: "Java"}); err == nil {
				t.Fatal("accepted invalid response")
			}
		})
	}
}

func helper(t *testing.T, mode string, timeout time.Duration) *Client {
	t.Helper()
	t.Setenv("DIRCUE_STRUCTURE_TEST_HELPER", mode)
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	c, err := New(Options{Worker: executable, Timeout: timeout})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestProcessBoundaries(t *testing.T) {
	for _, mode := range []string{"success", "failure", "invalid", "timeout", "overflow"} {
		t.Run(mode, func(t *testing.T) {
			timeout := 5 * time.Second
			if mode == "timeout" {
				timeout = 50 * time.Millisecond
			}
			c := helper(t, mode, timeout)
			f, err := c.Analyze(context.Background(), "a.java", "Java", []byte("class A {}"))
			if mode == "success" {
				if err != nil || f.ParseCount != 1 {
					t.Fatalf("%+v %v", f, err)
				}
				data, _ := json.Marshal(f)
				if strings.Contains(string(data), "timings") {
					t.Fatal("nondeterministic timings escaped")
				}
			} else if err == nil {
				t.Fatal("worker failure was hidden")
			}
			if mode == "timeout" && !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("wrong timeout: %v", err)
			}
		})
	}
}

func TestInputSelection(t *testing.T) {
	c := helper(t, "failure", time.Second)
	for _, tc := range []struct {
		name, language string
		content        []byte
		reason         string
	}{
		{"a.xml", "XML", []byte("<log/>"), "unsupported_language"},
		{"a.java", "Java", []byte{0xff}, "invalid_utf8"},
		{"a.cs", "C#", []byte{0}, "binary_source"},
		{strings.Repeat("a", 16<<10+1), "Java", nil, "invalid_path"},
		{"a.java", "Java", make([]byte, MaxSourceBytes+1), "file_too_large"},
	} {
		f, err := c.Analyze(context.Background(), tc.name, tc.language, tc.content)
		if err != nil || f.Reason != tc.reason || f.ParseCount != 0 {
			t.Fatalf("%s: %+v %v", tc.reason, f, err)
		}
	}
}

func TestCanceledAdmission(t *testing.T) {
	c := helper(t, "success", 5*time.Second)
	c.admission <- struct{}{}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, err := c.Analyze(ctx, "a.java", "Java", nil)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("blocked admission ignored cancellation: %v", err)
	}
	<-c.admission
	if _, err := c.Analyze(context.Background(), "a.java", "Java", nil); err != nil {
		t.Fatal(err)
	}
}

func TestOptions(t *testing.T) {
	executable, _ := os.Executable()
	for _, opts := range []Options{{}, {Worker: t.TempDir()}, {Worker: "/path/that/does/not/exist"}, {Worker: executable, MaxFileBytes: MaxSourceBytes + 1}, {Worker: executable, MaxFileBytes: -1}, {Worker: executable, Timeout: -1}} {
		if _, err := New(opts); err == nil {
			t.Fatalf("accepted %+v", opts)
		}
	}
}

// Set DIRCUE_STRUCTURAL_WORKER to exercise the pinned native parser, including
// syntax recovery. CI sets this after building the worker on each platform.
func TestNativeWorker(t *testing.T) {
	path := os.Getenv("DIRCUE_STRUCTURAL_WORKER")
	if path == "" {
		t.Skip("native structural worker not supplied")
	}
	c, err := New(Options{Worker: path})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		language, source string
		partial          bool
	}{
		{"Java", "class A { int f() { return 1; } }", false},
		{"C#", "class A { int F() { return 1; } }", false},
		{"Java", "class {", true},
		{"C#", "class {", true},
	} {
		f, err := c.Analyze(context.Background(), "fixture", tc.language, []byte(tc.source))
		if err != nil {
			t.Fatal(err)
		}
		if f.ParseCount != 1 || f.SyntaxErrors != tc.partial {
			t.Fatalf("unexpected parse: %+v", f)
		}
		if !tc.partial && (f.Observations["classes"] != 1 || f.Observations["methods"] != 1) {
			t.Fatalf("unexpected observations: %+v", f)
		}
		again, err := c.Analyze(context.Background(), "fixture", tc.language, []byte(tc.source))
		if err != nil {
			t.Fatal(err)
		}
		a, _ := json.Marshal(f)
		b, _ := json.Marshal(again)
		if string(a) != string(b) {
			t.Fatal("repeated analysis not deterministic")
		}
	}
}

func TestNumericMetricsBoundary(t *testing.T) {
	valid := validResponse("a.java", "Java", 0)
	valid["metrics"].(map[string]any)["abc"] = map[string]any{"undefined": nil, "negative": -3.5, "nested": map[string]any{"value": 2}}
	data, _ := json.Marshal(valid)
	if _, err := decode(data, File{Path: "a.java", Language: "Java"}); err != nil {
		t.Fatal(err)
	}
	if numericMetric(json.Number("1e100000"), 0) {
		t.Fatal("nonfinite metric accepted")
	}
	var deep any = json.Number("1")
	for i := 0; i < 34; i++ {
		deep = map[string]any{"nested": deep}
	}
	if numericMetric(deep, 0) {
		t.Fatal("excessive metric nesting accepted")
	}
}

func TestCapabilityContract(t *testing.T) {
	listed := Capabilities()
	if len(listed) != 20 {
		t.Fatalf("supported language count: %d", len(listed))
	}
	for i, c := range listed {
		if !Supports(c.Language) || (i > 0 && listed[i-1].Language >= c.Language) {
			t.Fatalf("unstable or unsupported capability: %+v", c)
		}
		response := validResponse("fixture", c.Language, 0)
		data, _ := json.Marshal(response)
		file, err := decode(data, File{Path: "fixture", Language: c.Language})
		if err != nil || file.Provenance.Grammar != c.Grammar {
			t.Fatalf("%s: %+v %v", c.Language, file, err)
		}
	}
	for _, name := range []string{"Bash", "JSX", "Objective-C++", "F5 iRule", "COBOL", "Swift", "XML", ""} {
		if Supports(name) {
			t.Fatalf("unsupported or noncanonical language accepted: %s", name)
		}
	}
	listed[0].Observations[0] = "modified"
	if Capabilities()[0].Observations[0] == "modified" {
		t.Fatal("caller mutated capabilities")
	}
}

func TestExpandedObservationBoundary(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(map[string]any)
	}{
		{"invented declaration", func(v map[string]any) { v["observations"].(map[string]uint64)["classes"] = 0 }},
		{"missing health", func(v map[string]any) { delete(v["observations"].(map[string]uint64), "error_nodes") }},
		{"wrong parser", func(v map[string]any) { v["provenance"].(map[string]string)["grammar"] = "tree-sitter-java@0.23.5" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v := validResponse("main.py", "Python", 0)
			tc.change(v)
			data, _ := json.Marshal(v)
			if _, err := decode(data, File{Path: "main.py", Language: "Python"}); err == nil {
				t.Fatal("invalid expanded-language contract accepted")
			}
		})
	}
	v := validResponse("main.py", "Python", 0)
	v["status"] = "partial"
	v["syntax_errors"] = true
	v["observations"].(map[string]uint64)["error_nodes"] = 1
	data, _ := json.Marshal(v)
	file, err := decode(data, File{Path: "main.py", Language: "Python"})
	if err != nil || file.Reason != "syntax_errors" || len(file.Observations) != 3 {
		t.Fatalf("partial observation lost: %+v %v", file, err)
	}
}

func TestRecoveredRootWithoutVisibleErrorNodes(t *testing.T) {
	v := validResponse("Choice.kt", "Kotlin", 0)
	v["status"] = "partial"
	v["syntax_errors"] = true
	data, _ := json.Marshal(v)
	file, err := decode(data, File{Path: "Choice.kt", Language: "Kotlin"})
	if err != nil || !file.SyntaxErrors || file.Reason != "syntax_errors" {
		t.Fatalf("hidden recovery: %+v %v", file, err)
	}
}

func TestLanguageMetricAvailability(t *testing.T) {
	for _, tc := range []struct {
		language string
		missing  string
		invented string
	}{
		{"Shell", "tokens", "npa"},
		{"C", "cyclomatic", "wmc"},
		{"Go", "npa", "wmc"},
	} {
		t.Run(tc.language, func(t *testing.T) {
			v := validResponse("fixture", tc.language, 0)
			metrics := v["metrics"].(map[string]any)
			metrics[tc.invented] = map[string]any{"value": 0}
			data, _ := json.Marshal(v)
			if _, err := decode(data, File{Path: "fixture", Language: tc.language}); err == nil {
				t.Fatal("unsupported metric synthesized as zero accepted")
			}
			delete(metrics, tc.invented)
			delete(metrics, tc.missing)
			data, _ = json.Marshal(v)
			if _, err := decode(data, File{Path: "fixture", Language: tc.language}); err == nil {
				t.Fatal("available metric missing accepted")
			}
		})
	}
}
