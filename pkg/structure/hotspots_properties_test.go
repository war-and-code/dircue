package structure

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"math"
	"math/rand"
	"reflect"
	"sort"
	"strings"
	"testing"
	"unicode"
)

// These tests construct the complete population before retention. The reference
// sorts that population and uses division for buckets; it does not call the
// production comparator, bit-length helper, or bounded insertion algorithm.
type hotspotObservation struct {
	path, language, cohort string
	index, value, span     uint64
}

func referenceHotspotMetric(name string, entries []HotspotEntry) HotspotMetric {
	m := HotspotMetric{Metric: name, Count: uint64(len(entries)), Histogram: make([]uint64, 65), Top: []HotspotEntry{}}
	for _, e := range entries {
		bucket := 0
		for remaining := e.Value; remaining != 0; remaining /= 2 {
			bucket++
		}
		m.Histogram[bucket]++
		if m.Min == nil || e.Value < *m.Min {
			v := e.Value
			m.Min = &v
		}
		if m.Max == nil || e.Value > *m.Max {
			v := e.Value
			m.Max = &v
		}
	}
	m.Top = append(m.Top, entries...)
	sort.Slice(m.Top, func(i, j int) bool {
		if m.Top[i].Value != m.Top[j].Value {
			return m.Top[i].Value > m.Top[j].Value
		}
		return m.Top[i].Index < m.Top[j].Index
	})
	if len(m.Top) > 10 {
		m.Top = m.Top[:10]
	}
	return m
}

func generatedHotspotFiles(seed int64) ([]File, []hotspotObservation) {
	rng := rand.New(rand.NewSource(seed))
	var files []File
	var all []hotspotObservation
	for fileIndex := 0; fileIndex < 12; fileIndex++ {
		path := fmt.Sprintf("src/%02d.py", fileIndex)
		// Ties must use original paths even when display paths are omitted.
		if fileIndex == 1 || fileIndex == 2 {
			path = strings.Repeat("p", 1030) + path
		} else if fileIndex == 3 {
			path = "control\t/" + path
		}
		language, grammar := "Python", "python"
		if fileIndex%3 == 0 {
			language, grammar = "Java", "java"
		}
		cohort := "clean"
		if fileIndex%4 == 0 {
			cohort = "recovered"
		}
		population := rng.Intn(180)
		if seed < 22 {
			population = int(seed) // Include every empty/top-K transition.
		}
		f := File{Path: path, Language: language, Provenance: &Provenance{Grammar: grammar}, SourceSHA256: fmt.Sprintf("%x", sha256.Sum256([]byte(path)))}
		f.Hotspots = &FileHotspots{Provider: "big-code-analysis@2.2.0", Rule: "function-population", RuleVersion: "1.0.0", SyntaxErrors: cohort == "recovered", TotalSpaces: uint64(population)}
		entries := [2][]HotspotEntry{}
		for index := 1; index <= population; index++ {
			if index%17 == 0 {
				f.Hotspots.InvalidSpanSpaces++
				continue
			}
			value := rng.Uint64()
			switch index % 5 {
			case 0:
				value = math.MaxUint64 // Equal maxima in several files.
			case 1:
				value = 1 << uint(index%64)
			case 2:
				value = (1 << uint(index%64)) - 1
			case 3:
				value = uint64(index % 8)
			}
			span := uint64(rng.Intn(200) + 1)
			all = append(all, hotspotObservation{path, language, cohort, uint64(index), value, span})
			for metric, measured := range []uint64{value, span} {
				entries[metric] = append(entries[metric], HotspotEntry{Index: uint64(index), NameStatus: "unavailable", StartLine: 1, EndLine: span, Value: measured})
			}
		}
		f.Hotspots.Metrics = []HotspotMetric{referenceHotspotMetric("cyclomatic_sum", entries[0]), referenceHotspotMetric("span_lines", entries[1])}
		files = append(files, f)
	}
	return files, all
}

func assertHotspotPopulation(t *testing.T, report *HotspotReport, files []File, population []hotspotObservation) {
	t.Helper()
	var total, invalid, recovered uint64
	for _, f := range files {
		total += f.Hotspots.TotalSpaces
		invalid += f.Hotspots.InvalidSpanSpaces
		if f.Hotspots.SyntaxErrors {
			recovered++
		}
	}
	if report.TotalSpaces != total || report.InvalidSpanSpaces != invalid || report.AnalyzedFiles != uint64(len(files)) || report.RecoveredFiles != recovered {
		t.Fatalf("population counters differ: %+v", report)
	}
	if report.Status != "partial" || report.FileCoverageStatus != "complete" || len(report.Omissions) != 0 {
		t.Fatal("recovered population lost its coverage qualification")
	}
	if len(report.Groups) != 4 {
		t.Fatalf("expected four distinct language/syntax cohorts, got %d", len(report.Groups))
	}
	for _, group := range report.Groups {
		if len(group.Metrics) != 2 || group.Metrics[0].Metric != "cyclomatic_sum" || group.Metrics[1].Metric != "span_lines" {
			t.Fatal("metric identities changed")
		}
		var selected []hotspotObservation
		var groupTotal, groupInvalid, groupFiles uint64
		for _, f := range files {
			if f.Language == group.Language && f.Hotspots.SyntaxErrors == (group.SyntaxCohort == "recovered") {
				groupTotal += f.Hotspots.TotalSpaces
				groupInvalid += f.Hotspots.InvalidSpanSpaces
				groupFiles++
				if group.Grammar != f.Provenance.Grammar {
					t.Fatal("grammar lost")
				}
			}
		}
		if group.TotalSpaces != groupTotal || group.InvalidSpanSpaces != groupInvalid || group.AnalyzedFiles != groupFiles {
			t.Fatalf("wrong group counts: %+v", group)
		}
		for _, p := range population {
			if p.language == group.Language && p.cohort == group.SyntaxCohort {
				selected = append(selected, p)
			}
		}
		for metricIndex, metric := range group.Metrics {
			expected := append([]hotspotObservation(nil), selected...)
			if metricIndex == 1 {
				for i := range expected {
					expected[i].value = expected[i].span
				}
			}
			sort.Slice(expected, func(i, j int) bool {
				a, b := expected[i], expected[j]
				if a.value != b.value {
					return a.value > b.value
				}
				if a.path != b.path {
					return a.path < b.path
				}
				return a.index < b.index
			})
			histogram := make([]uint64, 65)
			for _, p := range expected {
				bucket := 0
				for value := p.value; value != 0; value /= 2 {
					bucket++
				}
				histogram[bucket]++
			}
			if metric.Count != uint64(len(expected)) || !reflect.DeepEqual(metric.Histogram, histogram) {
				t.Fatal("full population histogram/count differs")
			}
			if len(expected) == 0 {
				if metric.Min != nil || metric.Max != nil {
					t.Fatal("empty population assigned extrema")
				}
			} else if metric.Min == nil || metric.Max == nil || *metric.Max != expected[0].value || *metric.Min != expected[len(expected)-1].value {
				t.Fatal("full population extrema differ")
			}
			if len(expected) > 10 {
				expected = expected[:10]
			}
			if len(metric.Top) != len(expected) {
				t.Fatal("top-K length differs")
			}
			for i, e := range metric.Top {
				p := expected[i]
				status, display := "present", p.path
				if len(display) > 1024 || strings.ContainsFunc(display, unicode.IsControl) {
					status, display = "omitted", ""
				}
				if e.Value != p.value || e.Index != p.index || e.Path != display || e.PathStatus != status || e.PathSHA256 != fmt.Sprintf("%x", sha256.Sum256([]byte(p.path))) || e.StartLine != 1 || e.EndLine != p.span || e.SourceSHA256 != fmt.Sprintf("%x", sha256.Sum256([]byte(p.path))) {
					t.Fatalf("top-K entry %d differs: got %+v, full-sort oracle %+v", i, e, p)
				}
			}
		}
	}
}

func TestHotspotGeneratedMergeMatchesFullPopulation(t *testing.T) {
	for seed := int64(0); seed < 96; seed++ {
		t.Run(fmt.Sprint(seed), func(t *testing.T) {
			files, population := generatedHotspotFiles(seed)
			var first []byte
			for order := 0; order < 4; order++ {
				ordered := append([]File(nil), files...)
				if order > 0 {
					rng := rand.New(rand.NewSource(seed*13 + int64(order)))
					rng.Shuffle(len(ordered), func(i, j int) { ordered[i], ordered[j] = ordered[j], ordered[i] })
				}
				r := NewHotspotReport()
				for _, file := range ordered {
					if err := r.Add(file); err != nil {
						t.Fatal(err)
					}
				}
				r.Finish("complete", nil)
				assertHotspotPopulation(t, r, files, population)
				data, err := json.Marshal(r)
				if err != nil {
					t.Fatal(err)
				}
				if order == 0 {
					first = data
				} else if string(first) != string(data) {
					t.Fatal("file completion order changes serialized report")
				}
			}
		})
	}
}
