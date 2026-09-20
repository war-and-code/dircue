//go:build ignore

// This experiment feeds synthetic histogram summaries to the production Go
// aggregator. It does not run a parser or validate source-file discovery.
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"math/bits"
	"os"
	"slices"

	"dircue/pkg/structure"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	if len(os.Args) > 1 {
		if len(os.Args) != 2 || os.Args[1] != "--topk" {
			return fmt.Errorf("unsupported experiment mode")
		}
		return runTopK()
	}
	var values []uint64
	decoder := json.NewDecoder(io.LimitReader(os.Stdin, 1<<20))
	if err := decoder.Decode(&values); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return fmt.Errorf("expected one JSON array")
	}
	if len(values) > 4096 {
		return fmt.Errorf("experiment supports at most 4096 observations")
	}
	report := structure.NewHotspotReport()
	// Prime-sized partitions exercise the actual merge of file summaries.
	for start := 0; start < len(values); start += 17 {
		part := values[start:min(start+17, len(values))]
		metrics := []structure.HotspotMetric{}
		for _, name := range []string{"cyclomatic_sum", "span_lines"} {
			m := structure.HotspotMetric{Metric: name, Count: uint64(len(part)), Histogram: make([]uint64, 65), Top: []structure.HotspotEntry{}}
			for i, value := range part {
				if name == "span_lines" {
					value = 1
				}
				if m.Min == nil || value < *m.Min {
					x := value
					m.Min = &x
				}
				if m.Max == nil || value > *m.Max {
					x := value
					m.Max = &x
				}
				m.Histogram[bits.Len64(value)]++
				m.Top = append(m.Top, structure.HotspotEntry{Index: uint64(i + 1), NameStatus: "unavailable", StartLine: 1, EndLine: 1, Value: value})
			}
			slices.SortStableFunc(m.Top, func(a, b structure.HotspotEntry) int {
				if a.Value > b.Value {
					return -1
				}
				if a.Value < b.Value {
					return 1
				}
				return 0
			})
			m.Top = m.Top[:min(len(m.Top), structure.HotspotLimit)]
			metrics = append(metrics, m)
		}
		file := structure.File{Path: fmt.Sprintf("part-%04d.py", start/17), Language: "Python", SourceSHA256: "0000000000000000000000000000000000000000000000000000000000000000", Provenance: &structure.Provenance{Grammar: "python"}, Hotspots: &structure.FileHotspots{Provider: "big-code-analysis@2.2.0", Rule: "function-population", RuleVersion: "1.0.0", TotalSpaces: uint64(len(part)), Metrics: metrics}}
		if err := report.Add(file); err != nil {
			return err
		}
	}
	result := struct {
		Count     uint64   `json:"count"`
		Histogram []uint64 `json:"histogram"`
	}{Histogram: make([]uint64, 65)}
	if len(report.Groups) != 0 {
		result.Count = report.Groups[0].Metrics[0].Count
		result.Histogram = report.Groups[0].Metrics[0].Histogram
	}
	return json.NewEncoder(os.Stdout).Encode(result)
}

type rankedObservation struct {
	Value    uint64 `json:"value"`
	Identity uint64 `json:"identity"`
}

// Identity ranks become file paths and provider indices, preserving numeric
// identity order under Go's path/index tie-breakers. Each synthetic file supplies
// only its independently sorted top ten, while counts cover all its observations.
// This exercises Go's merge; production Rust selection is tested separately.
func runTopK() error {
	var entries []rankedObservation
	decoder := json.NewDecoder(io.LimitReader(os.Stdin, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&entries); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return fmt.Errorf("expected one JSON array")
	}
	if len(entries) > 4096 {
		return fmt.Errorf("too many experiment observations")
	}
	ordered := slices.Clone(entries)
	slices.SortFunc(ordered, func(a, b rankedObservation) int {
		if a.Identity < b.Identity {
			return -1
		}
		if a.Identity > b.Identity {
			return 1
		}
		return 0
	})
	ranks := make(map[uint64]int, len(entries))
	for rank, entry := range ordered {
		if _, exists := ranks[entry.Identity]; exists {
			return fmt.Errorf("duplicate experiment identity")
		}
		ranks[entry.Identity] = rank
	}
	const partitionSize = 17
	parts := make([][]rankedObservation, (len(entries)+partitionSize-1)/partitionSize)
	fileOrder := []int{}
	for _, entry := range entries {
		part := ranks[entry.Identity] / partitionSize
		if len(parts[part]) == 0 {
			fileOrder = append(fileOrder, part)
		}
		parts[part] = append(parts[part], entry)
	}

	type location struct {
		path  string
		index uint64
	}
	identities := make(map[location]uint64, len(entries))
	report := structure.NewHotspotReport()
	for _, part := range fileOrder {
		path := fmt.Sprintf("part-%020d.py", part)
		for _, entry := range parts[part] {
			index := uint64(ranks[entry.Identity]%partitionSize + 1)
			identities[location{path, index}] = entry.Identity
		}
		metrics := []structure.HotspotMetric{}
		for _, name := range []string{"cyclomatic_sum", "span_lines"} {
			m := structure.HotspotMetric{Metric: name, Count: uint64(len(parts[part])), Histogram: make([]uint64, 65), Top: []structure.HotspotEntry{}}
			for _, entry := range parts[part] {
				value := entry.Value
				if name == "span_lines" {
					value = 1
				}
				if m.Min == nil || value < *m.Min {
					x := value
					m.Min = &x
				}
				if m.Max == nil || value > *m.Max {
					x := value
					m.Max = &x
				}
				m.Histogram[bits.Len64(value)]++
				m.Top = append(m.Top, structure.HotspotEntry{Index: uint64(ranks[entry.Identity]%partitionSize + 1), NameStatus: "unavailable", StartLine: 1, EndLine: 1, Value: value})
			}
			slices.SortFunc(m.Top, func(a, b structure.HotspotEntry) int {
				if a.Value > b.Value {
					return -1
				}
				if a.Value < b.Value {
					return 1
				}
				if a.Index < b.Index {
					return -1
				}
				if a.Index > b.Index {
					return 1
				}
				return 0
			})
			m.Top = m.Top[:min(len(m.Top), structure.HotspotLimit)]
			metrics = append(metrics, m)
		}
		file := structure.File{Path: path, Language: "Python", SourceSHA256: "0000000000000000000000000000000000000000000000000000000000000000", Provenance: &structure.Provenance{Grammar: "python"}, Hotspots: &structure.FileHotspots{Provider: "big-code-analysis@2.2.0", Rule: "function-population", RuleVersion: "1.0.0", TotalSpaces: uint64(len(parts[part])), Metrics: metrics}}
		if err := report.Add(file); err != nil {
			return err
		}
	}
	result := struct {
		Top []rankedObservation `json:"top"`
	}{Top: []rankedObservation{}}
	if len(report.Groups) > 0 {
		for _, item := range report.Groups[0].Metrics[0].Top {
			identity, ok := identities[location{item.Path, item.Index}]
			if !ok {
				return fmt.Errorf("aggregation returned an unknown observation")
			}
			result.Top = append(result.Top, rankedObservation{Value: item.Value, Identity: identity})
		}
	}
	return json.NewEncoder(os.Stdout).Encode(result)
}
