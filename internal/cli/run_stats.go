package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"runtime"
	"time"

	"github.com/war-and-code/dircue/pkg/profile"
	"github.com/war-and-code/dircue/pkg/scanner"
)

// statsDoc is the JSON document written to --stats-json PATH.
// It is intentionally separate from every deterministic output format:
// measurements vary with hardware and scheduling, so they must never enter
// the map document or any other content-addressed payload.
type statsDoc struct {
	SchemaVersion      string             `json:"schema_version"`
	Kind               string             `json:"kind"`
	Command            string             `json:"command,omitempty"`
	SourceMode         string             `json:"source_mode,omitempty"`
	DeterministicCosts statsDeterministic `json:"deterministic_costs"`
	Measurements       statsMeasurements  `json:"measurements"`
}

// statsDeterministic contains counters that are identical for the same input
// and settings regardless of worker count. These are safe to use as CI gates.
type statsDeterministic struct {
	FilesEnumerated  int64          `json:"files_enumerated"`
	FilesContentRead int64          `json:"files_content_read"`
	BytesRequested   int64          `json:"bytes_requested"`
	LimitHits        statsLimitHits `json:"limit_hits"`
}

type statsLimitHits struct {
	FileBytes int64 `json:"file_bytes"`
	TreeSize  int64 `json:"tree_size"`
}

// statsMeasurements contains observed, non-deterministic costs.
// Values vary with OS scheduler, CPU load, memory pressure, and hardware.
// Do not gate CI on these values.
type statsMeasurements struct {
	WallTimeNs         int64       `json:"wall_time_ns"`
	Phases             statsPhases `json:"phases,omitempty"`
	PeakHeapInuseBytes uint64      `json:"peak_heap_inuse_bytes"`
	GCCount            uint32      `json:"gc_count"`
}

type statsPhases struct {
	ScanNs  int64 `json:"scan_ns"`
	BuildNs int64 `json:"build_ns"`
}

// statsRecorder holds timing state across phases of the map command.
// It is only allocated when --stats-json is provided.
type statsRecorder struct {
	start    time.Time
	scanEnd  time.Time
	buildEnd time.Time
	// peak heap tracked across checkpoints
	peakHeap uint64
}

func newStatsRecorder() *statsRecorder {
	return &statsRecorder{start: time.Now()}
}

func (s *statsRecorder) endScan() {
	s.scanEnd = time.Now()
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	if ms.HeapInuse > s.peakHeap {
		s.peakHeap = ms.HeapInuse
	}
}

func (s *statsRecorder) endBuild() {
	s.buildEnd = time.Now()
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	if ms.HeapInuse > s.peakHeap {
		s.peakHeap = ms.HeapInuse
	}
}

// buildStatsDoc assembles the stats document from the report summary,
// run counters, and timing measurements.
func buildStatsDoc(
	cmd string,
	sourceMode string,
	report *profile.Report,
	counters *scanner.RunCounters,
	rec *statsRecorder,
) statsDoc {
	// Deterministic: from report summary (already deterministic) and counters.
	filesEnumerated := report.Summary.ScannedFiles
	var filesContentRead, bytesRequested int64
	if counters != nil {
		filesContentRead = counters.FilesContentRead.Load()
		bytesRequested = counters.BytesRequested.Load()
	}
	var fileBytesHits, treeSizeHits int64
	for _, w := range report.Warnings {
		switch w.Code {
		case "file_too_large":
			fileBytesHits++
		case "tree_size_limit":
			treeSizeHits++
		}
	}

	// Measurements: from timing and runtime.
	now := time.Now()
	wallNs := now.Sub(rec.start).Nanoseconds()
	scanNs := int64(0)
	if !rec.scanEnd.IsZero() {
		scanNs = rec.scanEnd.Sub(rec.start).Nanoseconds()
	}
	buildNs := int64(0)
	if !rec.scanEnd.IsZero() && !rec.buildEnd.IsZero() {
		buildNs = rec.buildEnd.Sub(rec.scanEnd).Nanoseconds()
	}
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	peakHeap := rec.peakHeap
	if ms.HeapInuse > peakHeap {
		peakHeap = ms.HeapInuse
	}

	return statsDoc{
		SchemaVersion: "1.0.0",
		Kind:          "run-stats",
		Command:       cmd,
		SourceMode:    sourceMode,
		DeterministicCosts: statsDeterministic{
			FilesEnumerated:  filesEnumerated,
			FilesContentRead: filesContentRead,
			BytesRequested:   bytesRequested,
			LimitHits: statsLimitHits{
				FileBytes: fileBytesHits,
				TreeSize:  treeSizeHits,
			},
		},
		Measurements: statsMeasurements{
			WallTimeNs: wallNs,
			Phases: statsPhases{
				ScanNs:  scanNs,
				BuildNs: buildNs,
			},
			PeakHeapInuseBytes: peakHeap,
			GCCount:            ms.NumGC,
		},
	}
}

// writeStatsJSON writes the stats document to the given file path.
// It creates or truncates the file. On error it returns a non-nil error
// that callers may log but must not treat as a scan failure.
func writeStatsJSON(path string, doc statsDoc) error {
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("stats-json: create %s: %w", path, err)
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	if err := enc.Encode(doc); err != nil {
		return fmt.Errorf("stats-json: write %s: %w", path, err)
	}
	return nil
}
