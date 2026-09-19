package structure

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

func validFunctions() map[string]any {
	metrics := map[string]any{}
	for _, group := range FunctionMetricGroups() {
		metrics[group] = map[string]any{"value": 0}
	}
	return map[string]any{"provider": "big-code-analysis@2.2.0", "rule": "space-kind-function", "rule_version": "1.0.0", "scope": "file", "status": "complete", "syntax_errors": false, "metric_scope": "includes_nested_spaces", "order": "provider_preorder", "limit": 128, "name_max_bytes": 256, "total_spaces": 1, "omitted_spaces": 0, "invalid_span_spaces": 0, "entries": []any{map[string]any{"index": 1, "name": "f", "name_status": "present", "start_line": 1, "end_line": 2, "metrics": metrics}}}
}

func TestFunctionResponseAndSourceBinding(t *testing.T) {
	content := []byte("class A {\n int f() { return 1; } }\n")
	response := validResponse("A.java", "Java", len(content))
	response["functions"] = validFunctions()
	response["source_sha256"] = "not-trusted"
	raw, _ := json.Marshal(response)
	submitted := File{Path: "A.java", Language: "Java", SourceBytes: int64(len(content))}
	observed, err := decodeResponse(raw, submitted, content, true)
	if err != nil {
		t.Fatal(err)
	}
	if observed.SourceSHA256 != fmt.Sprintf("%x", sha256.Sum256(content)) || observed.Functions == nil || len(observed.Functions.Entries) != 1 {
		t.Fatalf("wrong binding: %+v", observed)
	}
	if _, err := decode(raw, submitted); err == nil {
		t.Fatal("unsolicited function output accepted")
	}
	delete(response, "functions")
	raw, _ = json.Marshal(response)
	if _, err := decodeResponse(raw, submitted, content, true); err == nil {
		t.Fatal("missing requested functions accepted")
	}
	observed, err = decode(raw, submitted)
	if err != nil || observed.SourceSHA256 != "" || observed.Functions != nil {
		t.Fatalf("default response changed: %+v %v", observed, err)
	}
}

func TestFunctionContractRejections(t *testing.T) {
	changes := map[string]func(map[string]any){
		"provider": func(f map[string]any) { f["provider"] = "other" },
		"order":    func(f map[string]any) { f["order"] = "sorted" },
		"count":    func(f map[string]any) { f["total_spaces"] = 2 },
		"huge count": func(f map[string]any) {
			f["total_spaces"] = uint64(1) << 63
			f["omitted_spaces"] = (uint64(1) << 63) - 1
		},
		"missing zero field":  func(f map[string]any) { delete(f, "invalid_span_spaces") },
		"unknown":             func(f map[string]any) { f["extra"] = true },
		"status":              func(f map[string]any) { f["status"] = "partial" },
		"null zero field":     func(f map[string]any) { f["invalid_span_spaces"] = nil },
		"null syntax":         func(f map[string]any) { f["syntax_errors"] = nil },
		"invented cap":        func(f map[string]any) { f["total_spaces"] = 2; f["omitted_spaces"] = 1; f["status"] = "partial" },
		"null entries":        func(f map[string]any) { f["entries"] = nil; f["total_spaces"] = 0 },
		"zero line":           func(f map[string]any) { f["entries"].([]any)[0].(map[string]any)["start_line"] = 0 },
		"outside source":      func(f map[string]any) { f["entries"].([]any)[0].(map[string]any)["end_line"] = 4 },
		"reversed span":       func(f map[string]any) { f["entries"].([]any)[0].(map[string]any)["start_line"] = 3 },
		"wrong index":         func(f map[string]any) { f["entries"].([]any)[0].(map[string]any)["index"] = 2 },
		"unknown entry key":   func(f map[string]any) { f["entries"].([]any)[0].(map[string]any)["class"] = true },
		"control name":        func(f map[string]any) { f["entries"].([]any)[0].(map[string]any)["name"] = "f\n" },
		"large name":          func(f map[string]any) { f["entries"].([]any)[0].(map[string]any)["name"] = strings.Repeat("é", 129) },
		"absent present name": func(f map[string]any) { delete(f["entries"].([]any)[0].(map[string]any), "name") },
		"wrong metric groups": func(f map[string]any) {
			f["entries"].([]any)[0].(map[string]any)["metrics"].(map[string]any)["npa"] = map[string]int{"value": 1}
		},
	}
	for name, change := range changes {
		t.Run(name, func(t *testing.T) {
			f := validFunctions()
			change(f)
			raw, _ := json.Marshal(f)
			if _, ok := decodeFunctions(raw, []byte("function() {\nreturn;\n}"), false, 1024); ok {
				t.Fatal("invalid function result accepted")
			}
		})
	}
}

func TestFunctionPartialAndEmptySpaces(t *testing.T) {
	for _, reason := range []string{"cap", "invalid span", "name", "syntax", "empty"} {
		f := validFunctions()
		syntax := false
		switch reason {
		case "cap":
			entries := []any{}
			for i := 1; i <= 128; i++ {
				entry := map[string]any{}
				for k, v := range f["entries"].([]any)[0].(map[string]any) {
					entry[k] = v
				}
				entry["index"] = i
				entries = append(entries, entry)
			}
			f["entries"] = entries
			f["total_spaces"] = 129
			f["omitted_spaces"] = 1
		case "invalid span":
			f["total_spaces"] = 2
			f["invalid_span_spaces"] = 1
		case "name":
			entry := f["entries"].([]any)[0].(map[string]any)
			delete(entry, "name")
			entry["name_status"] = "omitted"
		case "syntax":
			syntax = true
			f["syntax_errors"] = true
		case "empty":
			f["entries"] = []any{}
			f["total_spaces"] = 0
		}
		if reason != "empty" {
			f["status"] = "partial"
		}
		raw, _ := json.Marshal(f)
		if _, ok := decodeFunctions(raw, []byte("function() {\nreturn;\n}"+strings.Repeat(" ", 200)), syntax, 1024); !ok {
			t.Fatalf("valid %s rejected", reason)
		}
	}
}

func TestDuplicateFunctionKeys(t *testing.T) {
	raw, _ := json.Marshal(validFunctions())
	raw = []byte(strings.Replace(string(raw), `"limit":128`, `"limit":2,"limit":128`, 1))
	if _, ok := decodeFunctions(raw, []byte("function() {\nreturn;\n}"), false, 1024); ok {
		t.Fatal("duplicate accepted")
	}
}

func TestFunctionOptionProcess(t *testing.T) {
	client := helper(t, "default-request", 5*time.Second)
	if client.FunctionMetricsEnabled() {
		t.Fatal("functions enabled by default")
	}
	if _, err := client.Analyze(context.Background(), "A.java", "Java", []byte("class A {}")); err != nil {
		t.Fatal(err)
	}
	client.options.Functions = true
	// The helper checks the default request body only in the previous call.
	t.Setenv("DIRCUE_STRUCTURE_TEST_HELPER", "success")
	file, err := client.Analyze(context.Background(), "A.java", "Java", []byte("class A { int f() { return 1; } }"))
	if err != nil || file.Functions == nil || file.SourceSHA256 == "" {
		t.Fatalf("opt-in failed: %+v %v", file, err)
	}
}

func TestFunctionOuterResponseAmbiguity(t *testing.T) {
	response := validResponse("A.java", "Java", 2)
	response["functions"] = validFunctions()
	raw, _ := json.Marshal(response)
	for _, altered := range []string{strings.Replace(string(raw), `"path":"A.java"`, `"path":"A.java","PATH":"A.java"`, 1), strings.Replace(string(raw), `"path":"A.java"`, `"path":"A.java","path":"A.java"`, 1), strings.Replace(string(raw), `"bca":`, `"BCA":`, 1)} {
		if strictFunctionResponse([]byte(altered)) {
			t.Fatal("ambiguous typed response accepted")
		}
	}
	if !strictFunctionResponse(raw) {
		t.Fatal("valid outer response rejected")
	}
}

func TestFunctionRetainedPrefixAndPhysicalLines(t *testing.T) {
	f := validFunctions()
	entry := f["entries"].([]any)[0].(map[string]any)
	entry["start_line"] = 1
	entry["end_line"] = 1
	for _, source := range []string{"x", "x\n", "x\r\n", "λ\n"} {
		raw, _ := json.Marshal(f)
		if _, ok := decodeFunctions(raw, []byte(source), false, 1); !ok {
			t.Fatalf("physical line rejected: %q", source)
		}
		entry["end_line"] = 2
		raw, _ = json.Marshal(f)
		if _, ok := decodeFunctions(raw, []byte(source), false, 1); ok {
			t.Fatalf("imaginary trailing line accepted: %q", source)
		}
		entry["end_line"] = 1
	}
	if raw, _ := json.Marshal(f); func() bool { _, ok := decodeFunctions(raw, nil, false, 1); return ok }() {
		t.Fatal("function span in empty source accepted")
	}
	entries := []any{}
	for i := 2; i <= 129; i++ {
		copy := map[string]any{}
		for k, v := range entry {
			copy[k] = v
		}
		copy["index"] = i
		entries = append(entries, copy)
	}
	f["entries"] = entries
	f["total_spaces"] = 129
	f["omitted_spaces"] = 1
	f["status"] = "partial"
	raw, _ := json.Marshal(f)
	if _, ok := decodeFunctions(raw, []byte("x"), false, 129); ok {
		t.Fatal("valid retained-prefix gap accepted")
	}
}

func TestFunctionArrayBoundBeforeTypedAllocation(t *testing.T) {
	if boundedFunctionEntries([]byte("[" + strings.Repeat("{},", 128) + "{}]")) {
		t.Fatal("array exceeded128entry bound")
	}
	if boundedFunctionEntries([]byte("null")) {
		t.Fatal("null array accepted")
	}
	if !boundedFunctionEntries([]byte("[]")) {
		t.Fatal("empty array rejected")
	}
}

func TestOldWorkerFunctionRequestError(t *testing.T) {
	client := helper(t, "unsupported-functions", 5*time.Second)
	client.options.Functions = true
	_, err := client.Analyze(context.Background(), "A.java", "Java", []byte("class A {}"))
	if err == nil || !strings.Contains(err.Error(), "does not support function-space metrics; use an updated worker") {
		t.Fatalf("unclear old-worker failure: %v", err)
	}
	client.options.Functions = false
	_, err = client.Analyze(context.Background(), "A.java", "Java", []byte("class A {}"))
	if err == nil || strings.Contains(err.Error(), "function-space") {
		t.Fatalf("changed default worker failure: %v", err)
	}
}
