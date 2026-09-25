package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestMapStatsJSON verifies that --stats-json writes a document that:
//   - has schema_version 1.0.0 and kind run-stats,
//   - has positive files_enumerated and files_content_read counters,
//   - has a positive bytes_requested counter,
//   - has non-negative limit_hits,
//   - has a positive wall_time_ns.
func TestMapStatsJSON(t *testing.T) {
	root := t.TempDir()
	for name, content := range map[string]string{
		"main.go":   "package main\nfunc main() {}\n",
		"README.md": "# hello\n",
	} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}

	statsFile := filepath.Join(t.TempDir(), "stats.json")
	var stdout, stderr bytes.Buffer
	err := Execute(t.Context(), []string{"map", "--source", "directory", "--json",
		"--stats-json", statsFile, root}, &stdout, &stderr)
	if err != nil {
		t.Fatalf("map --stats-json: %v (stderr: %s)", err, stderr.String())
	}

	// Verify the stats file was written.
	raw, err := os.ReadFile(statsFile)
	if err != nil {
		t.Fatalf("read stats-json: %v", err)
	}

	var doc map[string]any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&doc); err != nil {
		t.Fatalf("parse stats-json: %v", err)
	}

	if v, _ := doc["schema_version"].(string); v != "1.0.0" {
		t.Errorf("schema_version=%q, want 1.0.0", v)
	}
	if k, _ := doc["kind"].(string); k != "run-stats" {
		t.Errorf("kind=%q, want run-stats", k)
	}

	det, _ := doc["deterministic_costs"].(map[string]any)
	if det == nil {
		t.Fatal("deterministic_costs is missing or not an object")
	}
	filesEnum, _ := det["files_enumerated"].(json.Number)
	filesRead, _ := det["files_content_read"].(json.Number)
	bytesReq, _ := det["bytes_requested"].(json.Number)

	if n, _ := filesEnum.Int64(); n <= 0 {
		t.Errorf("files_enumerated=%d, want > 0", n)
	}
	if n, _ := filesRead.Int64(); n <= 0 {
		t.Errorf("files_content_read=%d, want > 0", n)
	}
	if n, _ := bytesReq.Int64(); n <= 0 {
		t.Errorf("bytes_requested=%d, want > 0", n)
	}

	hits, _ := det["limit_hits"].(map[string]any)
	if hits == nil {
		t.Fatal("limit_hits is missing or not an object")
	}
	if fb, _ := hits["file_bytes"].(json.Number); fb == "" {
		t.Error("limit_hits.file_bytes is missing")
	}
	if ts, _ := hits["tree_size"].(json.Number); ts == "" {
		t.Error("limit_hits.tree_size is missing")
	}

	meas, _ := doc["measurements"].(map[string]any)
	if meas == nil {
		t.Fatal("measurements is missing or not an object")
	}
	wallNs, _ := meas["wall_time_ns"].(json.Number)
	if n, _ := wallNs.Int64(); n <= 0 {
		t.Errorf("wall_time_ns=%d, want > 0", n)
	}

	// The map stdout must be a valid JSON object (not corrupted by stats).
	var mapDoc map[string]any
	mapDecoder := json.NewDecoder(&stdout)
	mapDecoder.UseNumber()
	if err := mapDecoder.Decode(&mapDoc); err != nil {
		t.Fatalf("map stdout is not valid JSON: %v", err)
	}
	if mapDoc["kind"] != "map" {
		t.Errorf("map stdout kind=%q, want map", mapDoc["kind"])
	}
}

// TestMapStatsJSONNotInMapPayload verifies that stats-json output is absent
// from the map JSON document on stdout (byte-determinism preserved).
func TestMapStatsJSONNotInMapPayload(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.go"), []byte("package a\n"), 0600); err != nil {
		t.Fatal(err)
	}

	run := func(withStats bool) []byte {
		statsFile := ""
		if withStats {
			statsFile = filepath.Join(t.TempDir(), "stats.json")
		}
		var stdout, stderr bytes.Buffer
		args := []string{"map", "--source", "directory", "--json", root}
		if withStats {
			args = append(args, "--stats-json", statsFile)
		}
		if err := Execute(t.Context(), args, &stdout, &stderr); err != nil {
			t.Fatalf("map: %v", err)
		}
		return stdout.Bytes()
	}

	without := run(false)
	with := run(true)
	if !bytes.Equal(without, with) {
		t.Error("--stats-json changed the map stdout output (map payload must be byte-deterministic)")
	}
}

// TestMapStatsJSONWorkerInvariant verifies that deterministic cost counters
// are identical across different worker counts.
func TestMapStatsJSONWorkerInvariant(t *testing.T) {
	root := t.TempDir()
	for name, content := range map[string]string{
		"a.go": "package a\n",
		"b.go": "package b\n",
		"c.go": "package c\n",
	} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}

	runStats := func(workers string) map[string]any {
		statsFile := filepath.Join(t.TempDir(), "stats.json")
		var stdout, stderr bytes.Buffer
		if err := Execute(t.Context(), []string{"map", "--source", "directory", "--json",
			"--workers", workers, "--stats-json", statsFile, root}, &stdout, &stderr); err != nil {
			t.Fatalf("map --workers=%s: %v", workers, err)
		}
		raw, err := os.ReadFile(statsFile)
		if err != nil {
			t.Fatalf("read stats: %v", err)
		}
		var doc map[string]any
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.UseNumber()
		if err := decoder.Decode(&doc); err != nil {
			t.Fatalf("parse stats: %v", err)
		}
		det, _ := doc["deterministic_costs"].(map[string]any)
		return det
	}

	baseline := runStats("1")
	for _, w := range []string{"2", "4"} {
		got := runStats(w)
		for _, field := range []string{"files_enumerated", "files_content_read", "bytes_requested"} {
			bv, _ := baseline[field].(json.Number)
			gv, _ := got[field].(json.Number)
			if bv.String() != gv.String() {
				t.Errorf("workers=%s: %s: got %s, want %s", w, field, gv, bv)
			}
		}
	}
}

// TestMapStatsJSONBudgetHitsLimit verifies that limit_hits.tree_size is set
// when the inventory budget is exhausted.
func TestMapStatsJSONBudgetHitsLimit(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"a.go", "b.go", "c.go"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("package x\n"), 0600); err != nil {
			t.Fatal(err)
		}
	}

	statsFile := filepath.Join(t.TempDir(), "stats.json")
	var stdout, stderr bytes.Buffer
	// budget-files=1 will hit the limit immediately.
	err := Execute(t.Context(), []string{"map", "--source", "directory", "--json",
		"--budget-files", "1", "--stats-json", statsFile, root}, &stdout, &stderr)
	if err != nil {
		t.Fatalf("map --budget-files=1: %v", err)
	}

	raw, err := os.ReadFile(statsFile)
	if err != nil {
		t.Fatalf("read stats: %v", err)
	}
	var doc map[string]any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&doc); err != nil {
		t.Fatalf("parse stats: %v", err)
	}
	det, _ := doc["deterministic_costs"].(map[string]any)
	hits, _ := det["limit_hits"].(map[string]any)
	ts, _ := hits["tree_size"].(json.Number)
	if n, _ := ts.Int64(); n != 1 {
		t.Errorf("limit_hits.tree_size=%d after budget exhaustion, want 1", n)
	}
}
