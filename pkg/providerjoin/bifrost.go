package providerjoin

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"dircue/pkg/mapdoc"
)

// bifrostCodeQueryReport is the ordinary CodeQuery response documented by
// Bifrost. Explain, profile, policy, and finding reports intentionally do not
// match this envelope.
type bifrostCodeQueryReport struct {
	Results     []json.RawMessage `json:"results"`
	Truncated   *bool             `json:"truncated"`
	Diagnostics []json.RawMessage `json:"diagnostics"`
}

type bifrostRange struct {
	StartLine int `json:"start_line"`
	EndLine   int `json:"end_line"`
}

type bifrostResult struct {
	ResultType          string       `json:"result_type"`
	Path                string       `json:"path"`
	Language            string       `json:"language"`
	Kind                string       `json:"kind"`
	ProcedureKind       string       `json:"procedure_kind"`
	CallKind            string       `json:"call_kind"`
	UsageKind           string       `json:"usage_kind"`
	ID                  string       `json:"id"`
	FQName              string       `json:"fq_name"`
	EnclosingSymbol     string       `json:"enclosing_symbol"`
	StartLine           int          `json:"start_line"`
	EndLine             int          `json:"end_line"`
	Range               bifrostRange `json:"range"`
	ProvenanceTruncated bool         `json:"provenance_truncated"`
	Caller              struct {
		ID     string `json:"id"`
		FQName string `json:"fq_name"`
	} `json:"caller"`
	Callee struct {
		ID     string `json:"id"`
		FQName string `json:"fq_name"`
	} `json:"callee"`
	Target struct {
		ID     string `json:"id"`
		FQName string `json:"fq_name"`
	} `json:"target"`
}

var bifrostStructuralResultTypes = map[string]bool{
	"structural_match": true,
	"declaration":      true,
	"procedure":        true,
	"reference_site":   true,
	"call_site":        true,
	"file":             true,
}

func ingestBifrost(data []byte, in Input, limit int, key string) (Result, error) {
	var doc bifrostCodeQueryReport
	if err := decodeOne(data, &doc); err != nil {
		return Result{}, err
	}
	if doc.Results == nil || doc.Truncated == nil {
		return Result{}, fmt.Errorf("not a Bifrost ordinary CodeQuery result")
	}
	if len(doc.Results) > limit {
		return Result{}, fmt.Errorf("report exceeds %d-record limit", limit)
	}

	bindingState, bindingReason := binding(in.Snapshot, reportIdentity{})
	tool := toolNode("bifrost", "unknown", key, bindingState, bindingReason, nil)
	covered := make([]string, 0)
	partialReasons := []string{"selected_structural_query_not_comprehensive"}
	if *doc.Truncated {
		partialReasons = append(partialReasons, "provider_results_truncated")
	}
	if len(doc.Diagnostics) > 0 {
		partialReasons = append(partialReasons, "provider_diagnostics_reported")
	}

	for index, raw := range doc.Results {
		var row bifrostResult
		if err := json.Unmarshal(raw, &row); err != nil {
			return Result{}, fmt.Errorf("result %d: %w", index, err)
		}
		if !bifrostStructuralResultTypes[row.ResultType] {
			continue // Findings, witnesses, conflicts, and unknown domains stay with Bifrost.
		}
		clean, ok := cleanReportPath(row.Path)
		if !ok {
			continue
		}
		covered = append(covered, clean)
		start, end := row.StartLine, row.EndLine
		if start == 0 {
			start, end = row.Range.StartLine, row.Range.EndLine
		}
		if end == 0 {
			end = start
		}
		e := evidence("bifrost", "unknown", clean, start)
		if e.Span != nil && end >= start {
			e.Span.EndLine = end
		}
		properties := compactProperties(map[string]string{
			"language":        row.Language,
			"structural_kind": firstNonEmpty(row.Kind, row.ProcedureKind, row.CallKind, row.UsageKind),
			"result_id":       row.ID,
			"caller_id":       row.Caller.ID,
			"caller":          row.Caller.FQName,
			"callee_id":       row.Callee.ID,
			"callee":          row.Callee.FQName,
			"target_id":       row.Target.ID,
			"target":          row.Target.FQName,
		})
		coverage := mapdoc.Coverage{Status: mapdoc.CoveragePartial, Reasons: append([]string(nil), partialReasons...)}
		if row.ProvenanceTruncated {
			coverage.Reasons = append(coverage.Reasons, "provider_provenance_truncated")
		}
		tool.Facts = append(tool.Facts, mapdoc.Fact{
			Kind:       "structural_query_result",
			Name:       firstNonEmpty(row.FQName, row.EnclosingSymbol, row.Callee.FQName, row.Target.FQName, row.ID, row.ResultType),
			Value:      row.ResultType,
			State:      "provider_reported",
			Properties: properties,
			Coverage:   coverage,
			Evidence:   []mapdoc.Evidence{e},
		})
	}

	covered = compact(covered)
	sort.Slice(tool.Facts, func(i, j int) bool {
		left, right := tool.Facts[i], tool.Facts[j]
		return left.Value+left.Name+left.Evidence[0].Path < right.Value+right.Name+right.Evidence[0].Path
	})
	state := "selected_query_reported"
	if *doc.Truncated || len(doc.Diagnostics) > 0 {
		state = "partial_query_reported"
	}
	return Result{
		Nodes:  []mapdoc.Node{tool},
		Ledger: []CoverageEntry{{Tool: "bifrost", ReportKind: "bifrost-code-query-json", Scope: ".", Binding: bindingState, Ran: true, CoveredFiles: covered, State: state, Reason: bindingReason}},
	}, nil
}

func compactProperties(values map[string]string) map[string]string {
	for key, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			delete(values, key)
		} else {
			values[key] = value
		}
	}
	if len(values) == 0 {
		return nil
	}
	return values
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}
