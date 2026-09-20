package structure

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestHotspotReviewRejectsRepairedUnicodeEvidence(t *testing.T) {
	content := []byte("class A {\n int f() {return 1;} }\n")
	for _, tc := range []struct {
		name, path string
		replace    func([]byte) []byte
	}{
		{"unpaired high name", "A.java", func(data []byte) []byte {
			return bytes.ReplaceAll(data, []byte(`"name":"f"`), []byte(`"name":"\ud800"`))
		}},
		{"unpaired low name", "A.java", func(data []byte) []byte {
			return bytes.ReplaceAll(data, []byte(`"name":"f"`), []byte(`"name":"\udc00"`))
		}},
		{"surrogate path aliases literal replacement", "\ufffd.java", func(data []byte) []byte {
			return bytes.ReplaceAll(data, []byte(`"path":"`+"\ufffd.java"+`"`), []byte(`"path":"\ud800.java"`))
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			response := validResponse(tc.path, "Java", len(content))
			response["hotspots"] = validHotspots()
			raw, err := json.Marshal(response)
			if err != nil {
				t.Fatal(err)
			}
			bad := tc.replace(raw)
			if bytes.Equal(raw, bad) {
				t.Fatal("test did not mutate expected payload")
			}
			if _, err := decodeEnrichedResponse(bad, File{Path: tc.path, Language: "Java", SourceBytes: int64(len(content))}, content, false, true); err == nil {
				t.Fatal("repaired Unicode admitted as faithful worker evidence")
			}
		})
	}
	response := validResponse("A.java", "Java", len(content))
	response["hotspots"] = validHotspots()
	raw, _ := json.Marshal(response)
	paired := bytes.ReplaceAll(raw, []byte(`"name":"f"`), []byte(`"name":"\ud834\udd1e"`))
	f, err := decodeEnrichedResponse(paired, File{Path: "A.java", Language: "Java", SourceBytes: int64(len(content))}, content, false, true)
	if err != nil || f.Hotspots.Metrics[0].Top[0].Name != "𝄞" {
		t.Fatalf("valid surrogate pair rejected or changed: %v", err)
	}
}

func TestHotspotReviewCapabilitiesUnicodeIsNotRepaired(t *testing.T) {
	for _, feature := range []string{`\ud800`, `\udc00`, `x\ud800y`} {
		raw := []byte(`{"protocol":"dircue-structural-worker","version":1,"parse_count":0,"features":["hotspots","` + feature + `"]}`)
		if validHotspotCapabilities(raw) {
			t.Fatalf("invalid escaped Unicode feature accepted: %s", feature)
		}
	}
	valid := []byte(`{"protocol":"dircue-structural-worker","version":1,"parse_count":0,"features":["hotspots","\ud834\udd1e"]}`)
	if !validHotspotCapabilities(valid) {
		t.Fatal("valid supplementary Unicode feature rejected")
	}
}

func TestHotspotReviewMetricIntersectionIdentifiesOneSpace(t *testing.T) {
	for _, field := range []string{"name", "name_status", "start_line", "end_line"} {
		t.Run(field, func(t *testing.T) {
			f := validHotspots()
			metrics := f["metrics"].([]any)
			first := metrics[0].(map[string]any)
			e := first["top"].([]any)[0].(map[string]any)
			switch field {
			case "name":
				e["name"] = "other"
			case "name_status":
				delete(e, "name")
				e["name_status"] = "omitted"
			case "start_line":
				e["start_line"] = 2
			case "end_line":
				e["end_line"] = 1
			}
			raw, _ := json.Marshal(f)
			if _, ok := decodeHotspots(raw, []byte("line1\nline2\n"), false, 100); ok {
				t.Fatalf("same provider index described different %s across metrics", field)
			}
		})
	}
}

func TestHotspotReviewTopListCannotOmitKnownHigherBuckets(t *testing.T) {
	f := validHotspots()
	f["total_spaces"] = 11
	for _, item := range f["metrics"].([]any) {
		m := item.(map[string]any)
		m["count"] = 11
		m["min"] = 1
		hist := make([]uint64, 65)
		top := []any{}
		if m["metric"] == "cyclomatic_sum" {
			m["max"] = 8
			hist[4] = 10
			hist[1] = 1
		} else {
			m["max"] = 1
			hist[1] = 11
		}
		for i := 1; i <= 10; i++ {
			value := 1
			if m["metric"] == "cyclomatic_sum" && i < 10 {
				value = 8
			}
			top = append(top, map[string]any{"index": i, "name": "f", "name_status": "present", "start_line": 1, "end_line": 1, "value": value})
		}
		m["histogram"] = hist
		m["top"] = top
	}
	raw, _ := json.Marshal(f)
	if _, ok := decodeHotspots(raw, []byte("source\n"), false, 100); ok {
		t.Fatal("top list omitted a known larger-bucket value while retaining a lower value")
	}
}

func TestHotspotReviewUniformValuesPreserveEarliestValidIndices(t *testing.T) {
	f := validHotspots()
	f["total_spaces"] = 11
	for _, item := range f["metrics"].([]any) {
		m := item.(map[string]any)
		m["count"] = 11
		m["min"] = 1
		m["max"] = 1
		hist := make([]uint64, 65)
		hist[1] = 11
		m["histogram"] = hist
		top := []any{}
		for i := 2; i <= 11; i++ {
			top = append(top, map[string]any{"index": i, "name": "f", "name_status": "present", "start_line": 1, "end_line": 1, "value": 1})
		}
		m["top"] = top
	}
	raw, _ := json.Marshal(f)
	if _, ok := decodeHotspots(raw, []byte("source\n"), false, 100); ok {
		t.Fatal("uniform population omitted earlier valid tie")
	}
	// Index 1 may be absent when it was explicitly accounted as an invalid span.
	f["total_spaces"] = 12
	f["invalid_span_spaces"] = 1
	raw, _ = json.Marshal(f)
	if _, ok := decodeHotspots(raw, []byte("source\n"), false, 100); !ok {
		t.Fatal("accounted invalid earlier span incorrectly rejected")
	}
}
