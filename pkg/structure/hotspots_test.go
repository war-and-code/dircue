package structure

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/bits"
	"strings"
	"testing"
	"time"
)

func validHotspots() map[string]any {
	metrics := []any{}
	for _, name := range []string{"cyclomatic_sum", "span_lines"} {
		histogram := make([]uint64, 65)
		histogram[2] = 1
		metrics = append(metrics, map[string]any{"metric": name, "count": 1, "min": 2, "max": 2, "histogram": histogram, "top": []any{map[string]any{"index": 1, "name": "f", "name_status": "present", "start_line": 1, "end_line": 2, "value": 2}}})
	}
	return map[string]any{"provider": "big-code-analysis@2.2.0", "rule": "function-population", "rule_version": "1.0.0", "syntax_errors": false, "total_spaces": 1, "invalid_span_spaces": 0, "metrics": metrics}
}

func TestHotspotResponseBindingAndNegotiation(t *testing.T) {
	content := []byte("class A {\n int f() {return 1;} }\n")
	response := validResponse("A.java", "Java", len(content))
	response["hotspots"] = validHotspots()
	response["source_sha256"] = "untrusted"
	raw, _ := json.Marshal(response)
	submitted := File{Path: "A.java", Language: "Java", SourceBytes: int64(len(content))}
	file, err := decodeEnrichedResponse(raw, submitted, content, false, true)
	if err != nil {
		t.Fatal(err)
	}
	if file.Hotspots == nil || file.Functions != nil || file.SourceSHA256 != fmt.Sprintf("%x", sha256.Sum256(content)) {
		t.Fatalf("wrong binding: %+v", file)
	}
	if _, err := decodeResponse(raw, submitted, content, false); err == nil {
		t.Fatal("unsolicited hotspots accepted")
	}
	delete(response, "hotspots")
	raw, _ = json.Marshal(response)
	if _, err := decodeEnrichedResponse(raw, submitted, content, false, true); err == nil {
		t.Fatal("missing requested hotspots accepted")
	}
	for _, field := range []string{"hotspots", "Hotspots", "HOTSPOTS"} {
		malformed := strings.TrimSuffix(string(raw), "}") + fmt.Sprintf(",%q:{}}", field)
		if _, err := decodeEnrichedResponse([]byte(malformed), submitted, content, false, true); err == nil {
			t.Fatalf("invalid field %s accepted", field)
		}
	}
	if !unsupportedHotspotRequest([]byte("{\"status\":\"error\",\"parse_count\":0,\"error\":{\"code\":\"invalid_request\",\"message\":\"unknown field `hotspots`, expected path\"}}")) {
		t.Fatal("old worker rejection not recognized")
	}
}

func TestHotspotRejectsFalsePopulationAndMalformedEvidence(t *testing.T) {
	metric := func(f map[string]any) map[string]any { return f["metrics"].([]any)[0].(map[string]any) }
	entry := func(f map[string]any) map[string]any { return metric(f)["top"].([]any)[0].(map[string]any) }
	cases := map[string]func(map[string]any){
		"provider":               func(f map[string]any) { f["provider"] = "other" },
		"missing counter":        func(f map[string]any) { delete(f, "invalid_span_spaces") },
		"null counter":           func(f map[string]any) { f["invalid_span_spaces"] = nil },
		"counter overflow":       func(f map[string]any) { f["total_spaces"] = uint64(math.MaxUint64) },
		"invalid exceeds total":  func(f map[string]any) { f["invalid_span_spaces"] = 2 },
		"cohort":                 func(f map[string]any) { f["syntax_errors"] = true },
		"wrong metric":           func(f map[string]any) { metric(f)["metric"] = "grade" },
		"null count":             func(f map[string]any) { metric(f)["count"] = nil },
		"missing population":     func(f map[string]any) { metric(f)["count"] = 0 },
		"null extrema":           func(f map[string]any) { metric(f)["min"] = nil },
		"wrong maximum":          func(f map[string]any) { metric(f)["max"] = 3 },
		"wrong minimum":          func(f map[string]any) { metric(f)["min"] = 1 },
		"missing bucket":         func(f map[string]any) { metric(f)["histogram"] = make([]uint64, 64) },
		"wrong bucket total":     func(f map[string]any) { metric(f)["histogram"].([]uint64)[2] = 2 },
		"overflow bucket":        func(f map[string]any) { metric(f)["histogram"].([]uint64)[2] = math.MaxUint64 },
		"outside extrema bucket": func(f map[string]any) { h := metric(f)["histogram"].([]uint64); h[2] = 0; h[64] = 1 },
		"null bucket": func(f map[string]any) {
			h := make([]any, 65)
			for i := range h {
				h[i] = 0
			}
			h[2] = 1
			h[0] = nil
			metric(f)["histogram"] = h
		},
		"missing top":    func(f map[string]any) { metric(f)["top"] = []any{} },
		"too many top":   func(f map[string]any) { metric(f)["top"] = make([]any, 11) },
		"invalid index":  func(f map[string]any) { entry(f)["index"] = 0 },
		"outside source": func(f map[string]any) { entry(f)["end_line"] = 3 },
		"null value":     func(f map[string]any) { entry(f)["value"] = nil },
		"wrong value":    func(f map[string]any) { entry(f)["value"] = 1 },
		"name control":   func(f map[string]any) { entry(f)["name"] = "f\n" },
		"name too long":  func(f map[string]any) { entry(f)["name"] = strings.Repeat("λ", 129) },
		"name hidden":    func(f map[string]any) { entry(f)["name_status"] = "omitted" },
		"unknown entry":  func(f map[string]any) { entry(f)["quality"] = "good" },
		"unknown metric": func(f map[string]any) { metric(f)["average"] = 2 },
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			f := validHotspots()
			change(f)
			raw, _ := json.Marshal(f)
			if _, ok := decodeHotspots(raw, []byte("line1\nline2\n"), false, 100); ok {
				t.Fatal("accepted malformed hotspot report")
			}
		})
	}
}

func TestHotspotEmptyPopulationHasNoInventedZero(t *testing.T) {
	f := validHotspots()
	f["total_spaces"] = 0
	for _, raw := range f["metrics"].([]any) {
		m := raw.(map[string]any)
		m["count"] = 0
		m["min"] = nil
		m["max"] = nil
		m["top"] = []any{}
		m["histogram"] = make([]uint64, 65)
	}
	raw, _ := json.Marshal(f)
	decoded, ok := decodeHotspots(raw, nil, false, 1)
	if !ok || decoded.Metrics[0].Min != nil {
		t.Fatal("empty population rejected or assigned zero")
	}
}

func hotspotFile(path, language string, recovered bool, values ...uint64) File {
	f := &FileHotspots{Provider: "big-code-analysis@2.2.0", Rule: "function-population", RuleVersion: "1.0.0", SyntaxErrors: recovered, TotalSpaces: uint64(len(values)), Metrics: []HotspotMetric{}}
	for _, name := range []string{"cyclomatic_sum", "span_lines"} {
		m := HotspotMetric{Metric: name, Count: uint64(len(values)), Histogram: make([]uint64, 65), Top: []HotspotEntry{}}
		for i, v := range values {
			if m.Min == nil || v < *m.Min {
				n := v
				m.Min = &n
			}
			if m.Max == nil || v > *m.Max {
				n := v
				m.Max = &n
			}
			m.Histogram[bits.Len64(v)]++
			m.Top = append(m.Top, HotspotEntry{Index: uint64(i + 1), NameStatus: "unavailable", StartLine: 1, EndLine: v, Value: v})
		}
		// Test values are supplied in descending order, matching worker retention.
		if len(m.Top) > HotspotLimit {
			m.Top = m.Top[:HotspotLimit]
		}
		f.Metrics = append(f.Metrics, m)
	}
	return File{Path: path, Language: language, Provenance: &Provenance{Grammar: "grammar"}, SourceSHA256: strings.Repeat("a", 64), Hotspots: f}
}

func TestHotspotMergeRanksFullPopulationAndSeparatesCohorts(t *testing.T) {
	files := []File{hotspotFile("z.py", "Python", false, 30, 20, 19, 18, 17, 16, 15, 14, 13, 12, 11, 10), hotspotFile("a.py", "Python", false, 30, 29, 28), hotspotFile("bad.py", "Python", true, 999), hotspotFile("C.java", "Java", false, 1000)}
	run := func(reverse bool) *HotspotReport {
		r := NewHotspotReport()
		for i := range files {
			j := i
			if reverse {
				j = len(files) - i - 1
			}
			if err := r.Add(files[j]); err != nil {
				t.Fatal(err)
			}
		}
		r.Finish("partial", map[string]int64{"unsupported_language": 2})
		return r
	}
	r := run(false)
	other := run(true)
	a, _ := json.Marshal(r)
	b, _ := json.Marshal(other)
	if string(a) != string(b) {
		t.Fatal("completion order changes aggregate")
	}
	if r.TotalSpaces != 17 || r.AnalyzedFiles != 4 || r.RecoveredFiles != 1 || len(r.Groups) != 3 || r.Status != "partial" || r.Omissions["unsupported_language"] != 2 {
		t.Fatalf("wrong coverage: %+v", r)
	}
	group := r.Groups[1]
	if group.Language != "Python" || group.SyntaxCohort != "clean" || group.TotalSpaces != 15 {
		t.Fatalf("wrong group: %+v", group)
	}
	metric := group.Metrics[0]
	if metric.Count != 15 || *metric.Min != 10 || *metric.Max != 30 || len(metric.Top) != 10 || metric.Top[0].Path != "a.py" || metric.Top[1].Path != "z.py" || metric.Top[2].Value != 29 {
		t.Fatalf("wrong ranking: %+v", metric)
	}
	var count uint64
	for _, n := range metric.Histogram {
		count += n
	}
	if count != 15 {
		t.Fatal("retention lost histogram population")
	}
}

func TestHotspotCounterOverflowLeavesReportUnchanged(t *testing.T) {
	r := NewHotspotReport()
	r.TotalSpaces = math.MaxUint64
	before, _ := json.Marshal(r)
	if r.Add(hotspotFile("a.py", "Python", false, 1)) == nil {
		t.Fatal("overflow accepted")
	}
	after, _ := json.Marshal(r)
	if string(before) != string(after) {
		t.Fatal("failed addition mutated report")
	}
}

func FuzzHotspotWireBoundary(f *testing.F) {
	raw, _ := json.Marshal(validHotspots())
	f.Add(raw)
	f.Add([]byte(`{}`))
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 1<<20 {
			return
		}
		decodeHotspots(data, []byte("line1\nline2\n"), false, 100)
	})
}

func TestHotspotBoundedPathsPreservePopulationAndTieOrder(t *testing.T) {
	r := NewHotspotReport()
	paths := []string{strings.Repeat("z", HotspotPathMaxBytes+1) + ".py", strings.Repeat("a", HotspotPathMaxBytes+1) + ".py", "b.py", "control\n.py"}
	for _, path := range paths {
		if err := r.Add(hotspotFile(path, "Python", false, 10)); err != nil {
			t.Fatal(err)
		}
	}
	r.Finish("complete", nil)
	top := r.Groups[0].Metrics[0].Top
	if len(top) != 4 || r.TotalSpaces != 4 || top[0].Path != "" || top[0].PathStatus != "omitted" || top[1].Path != "b.py" || top[2].PathStatus != "omitted" || top[3].PathStatus != "omitted" {
		t.Fatalf("bounded evidence lost original tie order: %+v", top)
	}
	for _, entry := range top {
		if entry.PathSHA256 != fmt.Sprintf("%x", sha256.Sum256([]byte(entry.sortPath))) {
			t.Fatal("wrong path digest")
		}
	}
	raw, _ := json.Marshal(r)
	if strings.Contains(string(raw), strings.Repeat("a", HotspotPathMaxBytes+1)) || strings.Contains(string(raw), "control") {
		t.Fatal("private sorting path escaped into report")
	}
	if r.PathMaxBytes != 1024 || len(raw) > 16<<10 {
		t.Fatalf("path contract not bounded: %d", len(raw))
	}
}

func TestHotspotCapabilitiesStrictIdentity(t *testing.T) {
	valid := `{"protocol":"dircue-structural-worker","version":1,"parse_count":0,"features":["functions","hotspots"]}`
	if !validHotspotCapabilities([]byte(valid)) {
		t.Fatal("valid capabilities rejected")
	}
	for _, data := range []string{
		strings.Replace(valid, `"version":1`, `"version":2`, 1),
		strings.Replace(valid, `"version":1`, `"version":null`, 1),
		strings.Replace(valid, `"parse_count":0`, `"parse_count":1`, 1),
		strings.Replace(valid, `"functions","hotspots"`, `"functions"`, 1),
		strings.Replace(valid, `"functions","hotspots"`, `"hotspots","hotspots"`, 1),
		strings.Replace(valid, `"protocol"`, `"Protocol"`, 1),
		strings.Replace(valid, `"version":1`, `"version":1,"version":1`, 1),
		valid + valid,
	} {
		if validHotspotCapabilities([]byte(data)) {
			t.Fatalf("invalid capabilities accepted: %s", data)
		}
	}
}

func TestHotspotCapabilityProcessBoundary(t *testing.T) {
	for _, mode := range []string{"capabilities", "failure", "invalid", "overflow", "timeout"} {
		t.Run(mode, func(t *testing.T) {
			timeout := 5 * time.Second
			if mode == "timeout" {
				timeout = 20 * time.Millisecond
			}
			c := helper(t, mode, timeout)
			if err := c.CheckCapabilities(context.Background()); err != nil {
				t.Fatalf("default feature unexpectedly probed worker: %v", err)
			}
			c.options.Hotspots = true
			err := c.CheckCapabilities(context.Background())
			if (mode == "capabilities") != (err == nil) {
				t.Fatalf("wrong capability result: %v", err)
			}
			if mode == "timeout" && !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("timeout not preserved: %v", err)
			}
		})
	}
	c := helper(t, "capabilities", time.Second)
	c.options.Hotspots = true
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := c.CheckCapabilities(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation not preserved: %v", err)
	}
}

func TestHotspotSerializedEvidenceBoundAcrossAllLanguageCohorts(t *testing.T) {
	r := NewHotspotReport()
	for language := range capabilities {
		for _, recovered := range []bool{false, true} {
			file := hotspotFile(strings.Repeat("<", HotspotPathMaxBytes), language, recovered, 10, 9, 8, 7, 6, 5, 4, 3, 2, 1)
			for i := range file.Hotspots.Metrics {
				for j := range file.Hotspots.Metrics[i].Top {
					file.Hotspots.Metrics[i].Top[j].Name = strings.Repeat("<", FunctionNameMaxBytes)
					file.Hotspots.Metrics[i].Top[j].NameStatus = "present"
				}
			}
			if err := r.Add(file); err != nil {
				t.Fatal(err)
			}
		}
	}
	r.Finish("partial", nil)
	raw, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) >= 16<<20 {
		t.Fatalf("maximally JSON-escaped bounded hotspot evidence grew beyond16MiB: %d", len(raw))
	}
	if len(r.Groups) != 2*len(capabilities) {
		t.Fatal("test failed to include every supported language/cohort")
	}
}

func TestHotspotAggregateRejectsMalformedFilesBeforeMutation(t *testing.T) {
	for _, change := range []func(*File){
		func(f *File) { f.Provenance = nil },
		func(f *File) { f.Hotspots.Metrics = nil },
		func(f *File) { f.Hotspots.Metrics[0].Histogram = nil },
		func(f *File) { f.Hotspots.Metrics[0].Metric = "other" },
		func(f *File) { f.Hotspots.Provider = "other" },
		func(f *File) { f.Hotspots.InvalidSpanSpaces = 2 },
		func(f *File) { f.Hotspots.Metrics[0].Count = math.MaxUint64 },
		func(f *File) { f.Hotspots.Metrics[0].Histogram[1] = math.MaxUint64 },
	} {
		r := NewHotspotReport()
		before, _ := json.Marshal(r)
		file := hotspotFile("a.py", "Python", false, 1)
		change(&file)
		if err := r.Add(file); err == nil {
			t.Fatal("malformed file accepted")
		}
		after, _ := json.Marshal(r)
		if string(before) != string(after) {
			t.Fatal("malformed addition mutated report")
		}
	}
}
