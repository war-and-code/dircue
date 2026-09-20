package reportdiff_test

import (
	"context"
	"crypto/sha256"
	"fmt"
	"strings"
	"testing"

	"dircue/pkg/formats"
	"dircue/pkg/profile"
	"dircue/pkg/structure"
)

func contentProfile(t *testing.T, paths ...string) profile.Report {
	t.Helper()
	p := reviewProfile(t, 0)
	p.SchemaVersion = profile.ContentSchemaVersion
	c := formats.New("directory", "", 0)
	for _, path := range paths {
		c.Add(formats.Candidate{Path: path, Size: 2, Read: func(context.Context, int64) ([]byte, int64, error) { return []byte("{}"), 2, nil }})
	}
	var err error
	p.Formats, err = c.Finish(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return p
}
func TestFormatComparisonQualifiesAbsence(t *testing.T) {
	a, b := contentProfile(t, "a.json"), contentProfile(t)
	m := reviewModule(t, reviewCompare(t, a, b), "formats")
	if m.Counts.Removed != 1 {
		t.Fatalf("complete absence: %+v", m)
	}
	b.Formats.Coverage.SelectedFiles = 1
	m = reviewModule(t, reviewCompare(t, a, b), "formats")
	if m.Counts.Removed != 0 || m.Counts.Unavailable != 1 || m.Compatibility != "observed_only" {
		t.Fatalf("inconsistent population removal: %+v", m)
	}
	b = contentProfile(t)
	b.Formats.Status = "partial"
	b.Formats.Omissions["input_byte_limit"] = 1
	m = reviewModule(t, reviewCompare(t, a, b), "formats")
	if m.Counts.Removed != 0 || m.Counts.Unavailable != 1 {
		t.Fatalf("capped absence: %+v", m)
	}
	b = contentProfile(t)
	b.Formats.ProviderVersion = "2.0.0"
	m = reviewModule(t, reviewCompare(t, a, b), "formats")
	if m.Compatibility != "incomparable" || len(m.Changes) != 0 {
		t.Fatalf("provider mismatch: %+v", m)
	}
}
func hotspotProfile(t *testing.T, path string) profile.Report {
	p := reviewProfile(t, 0)
	p.SchemaVersion = profile.ContentSchemaVersion
	h := structure.NewHotspotReport()
	h.AnalyzedFiles = 1
	metrics := []structure.HotspotDistribution{}
	for _, name := range []string{"cyclomatic_sum", "span_lines"} {
		m := structure.HotspotDistribution{Metric: name, Definition: "test definition", Unit: "count", MetricScope: "includes_nested_spaces", Histogram: make([]uint64, 65), Top: []structure.HotspotEvidence{}}
		if path != "" {
			n := uint64(1)
			m.Count = 1
			m.Min = &n
			m.Max = &n
			m.Histogram[1] = 1
			m.Top = append(m.Top, structure.HotspotEvidence{Path: path, PathStatus: "present", PathSHA256: fmt.Sprintf("%x", sha256.Sum256([]byte(path))), SourceSHA256: strings.Repeat("a", 64), HotspotEntry: structure.HotspotEntry{Index: 1, Name: "f", NameStatus: "present", StartLine: 1, EndLine: 1, Value: 1}})
		}
		metrics = append(metrics, m)
	}
	if path != "" {
		h.TotalSpaces = 1
	}
	h.Groups = []structure.HotspotGroup{{Language: "Java", Grammar: "tree-sitter-java", SyntaxCohort: "clean", AnalyzedFiles: 1, TotalSpaces: h.TotalSpaces, Metrics: metrics}}
	p.Structure = &profile.StructureReport{Hotspots: h, SupportedLanguages: structure.Capabilities(), ObservationFiles: map[string]int64{}, Engine: "big-code-analysis", EngineVersion: "2.2.0", Status: "complete", Scope: "source", Source: "directory", MaxFileBytes: structure.MaxSourceBytes, AnalyzedFiles: 1, ParseCount: 1, Omissions: map[string]int64{}, Observations: map[string]uint64{}}
	return p
}
func TestHotspotComparisonRanksAreNotFunctionIdentity(t *testing.T) {
	a, b := hotspotProfile(t, "a.java"), hotspotProfile(t, "b.java")
	m := reviewModule(t, reviewCompare(t, a, b), "hotspots")
	if m.Counts.Removed != 0 || m.Counts.Added != 0 || m.Counts.Changed != 2 || m.Compatibility != "observed_only" {
		t.Fatalf("ranking treated as function identities: %+v", m)
	}
	empty := hotspotProfile(t, "")
	m = reviewModule(t, reviewCompare(t, empty, empty), "hotspots")
	if m.Status != "unchanged" {
		t.Fatalf("nullable extrema did not roundtrip: %+v", m)
	}
	b.Structure.Hotspots.Groups = []structure.HotspotGroup{}
	m = reviewModule(t, reviewCompare(t, a, b), "hotspots")
	if m.Counts.Removed != 0 || m.Counts.Unavailable != 2 {
		t.Fatalf("missing cohort proved removal: %+v", m)
	}
	b = hotspotProfile(t, "b.java")
	b.Structure.Hotspots.RuleVersion = "2.0.0"
	m = reviewModule(t, reviewCompare(t, a, b), "hotspots")
	if m.Compatibility != "incomparable" {
		t.Fatalf("rule mismatch comparable: %+v", m)
	}
}

func TestHotspotComparisonPreservesHistogramBinIdentity(t *testing.T) {
	a, b := hotspotProfile(t, "a.java"), hotspotProfile(t, "a.java")
	for _, p := range []*profile.Report{&a, &b} {
		h := p.Structure.Hotspots
		h.TotalSpaces = 15
		h.Groups[0].TotalSpaces = 15
		for i := range h.Groups[0].Metrics {
			m := &h.Groups[0].Metrics[i]
			m.Count = 15
			lo, hi := uint64(1), uint64(64)
			m.Min = &lo
			m.Max = &hi
			m.Histogram = make([]uint64, 65)
			m.Histogram[1] = 1
			m.Histogram[2] = 1
			m.Histogram[3] = 2
			m.Histogram[4] = 1
			m.Histogram[7] = 10
			// Ten identical high values keep the retained ranking unchanged; only
			// the lower population distribution changes between the snapshots.
			first := m.Top[0]
			m.Top = nil
			for j := uint64(1); j <= 10; j++ {
				entry := first
				entry.Index = j
				entry.Value = 64
				entry.EndLine = 64
				m.Top = append(m.Top, entry)
			}
		}
	}
	for i := range b.Structure.Hotspots.Groups[0].Metrics {
		m := &b.Structure.Hotspots.Groups[0].Metrics[i]
		m.Histogram[2], m.Histogram[3] = 2, 1
	}
	m := reviewModule(t, reviewCompare(t, a, b), "hotspots")
	if m.Counts.Changed != 2 {
		t.Fatalf("histogram bins compared as an unordered bag: %+v", m)
	}
	for _, change := range m.Changes {
		if len(change.Fields) != 1 || change.Fields[0].Field != "histogram" {
			t.Fatalf("unexpected field change: %+v", change)
		}
	}
}
