package providerjoin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"reflect"
	"sort"
	"strings"

	"github.com/war-and-code/dircue/pkg/mapdoc"
)

const (
	defaultMaxBytes       = 32 << 20
	defaultMaxRecords     = 100_000
	defaultMaxAttachments = 16
)

type reportIdentity struct {
	Tree, Commit, Algorithm, Scope, Digest string
}

func join(ctx context.Context, in Input, attachments []Attachment, opts Options) (Result, error) {
	if len(attachments) > defaultMaxAttachments {
		return Result{}, fmt.Errorf("attachments exceed %d-report limit", defaultMaxAttachments)
	}
	if opts.MaxReportBytes <= 0 {
		opts.MaxReportBytes = defaultMaxBytes
	}
	if opts.MaxRecords <= 0 {
		opts.MaxRecords = defaultMaxRecords
	}
	var out Result
	for attachmentIndex, a := range attachments {
		if err := ctx.Err(); err != nil {
			return Result{}, err
		}
		data, err := readBounded(ctx, a.Path, opts.MaxReportBytes)
		if err != nil {
			return Result{}, fmt.Errorf("attach %s: %w", a.Kind, err)
		}
		switch strings.ToLower(a.Kind) {
		case "syft", "syft-json":
			r, err := ingestSyft(data, in, opts.MaxRecords, fmt.Sprintf("attachment:%d", attachmentIndex))
			if err != nil {
				return Result{}, fmt.Errorf("attach syft-json: %w", err)
			}
			merge(&out, r)
		case "sarif":
			r, err := ingestSARIF(data, in, opts.MaxRecords, fmt.Sprintf("attachment:%d", attachmentIndex))
			if err != nil {
				return Result{}, fmt.Errorf("attach sarif: %w", err)
			}
			merge(&out, r)
		case "noir", "noir-json":
			r, err := ingestNoir(data, in, opts.MaxRecords, fmt.Sprintf("attachment:%d", attachmentIndex))
			if err != nil {
				return Result{}, fmt.Errorf("attach noir-json: %w", err)
			}
			merge(&out, r)
		case "bifrost", "bifrost-json", "bifrost-code-query-json":
			r, err := ingestBifrost(data, in, opts.MaxRecords, fmt.Sprintf("attachment:%d", attachmentIndex))
			if err != nil {
				return Result{}, fmt.Errorf("attach bifrost-code-query-json: %w", err)
			}
			merge(&out, r)
		default:
			return Result{}, fmt.Errorf("unsupported attachment kind %q", a.Kind)
		}
	}
	out.Plans = Route(in)
	out.Coverage = coverageFor(out)
	if err := deduplicateResult(&out); err != nil {
		return Result{}, err
	}
	sortResult(&out)
	return out, nil
}

func deduplicateResult(r *Result) error {
	seenNodes := map[string]int{}
	nodes := make([]mapdoc.Node, 0, len(r.Nodes))
	for _, node := range r.Nodes {
		if index, ok := seenNodes[node.ID]; ok {
			previous := &nodes[index]
			if previous.Kind != node.Kind || previous.Name != node.Name || previous.Discriminator != node.Discriminator || !reflect.DeepEqual(previous.Paths, node.Paths) || !reflect.DeepEqual(previous.Properties, node.Properties) {
				return fmt.Errorf("provider reports produced conflicting node %q", node.ID)
			}
			previous.Evidence = append(previous.Evidence, node.Evidence...)
			previous.Facts = append(previous.Facts, node.Facts...)
			continue
		}
		seenNodes[node.ID] = len(nodes)
		nodes = append(nodes, node)
	}
	r.Nodes = nodes
	seenEdges := map[string]bool{}
	edges := r.Edges[:0]
	for _, edge := range r.Edges {
		if !seenEdges[edge.ID] {
			seenEdges[edge.ID] = true
			edges = append(edges, edge)
		}
	}
	// When a package has both a provider_location packaged_in edge and a richer
	// declared_requirement:* packaged_in edge to the same component, suppress
	// the provider_location edge: the declared_requirement edge carries
	// component-declaration evidence and is strictly more informative.
	type fromTo struct{ from, to string }
	declaredPairs := map[fromTo]bool{}
	for _, edge := range edges {
		if edge.Type == mapdoc.EdgePackagedIn && strings.HasPrefix(edge.Discriminator, "declared_requirement:") {
			declaredPairs[fromTo{edge.From, edge.To}] = true
		}
	}
	if len(declaredPairs) > 0 {
		filtered := edges[:0]
		for _, edge := range edges {
			if edge.Type == mapdoc.EdgePackagedIn && edge.Discriminator == "provider_location" {
				if declaredPairs[fromTo{edge.From, edge.To}] {
					continue // suppressed; the declared_requirement edge covers this
				}
			}
			filtered = append(filtered, edge)
		}
		edges = filtered
	}
	r.Edges = edges
	return nil
}

func readBounded(ctx context.Context, filename string, limit int64) ([]byte, error) {
	info, err := os.Lstat(filename)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("report must be a regular file")
	}
	f, err := openReportFile(filename)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err = f.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("report must be a regular file")
	}
	var b []byte
	buf := make([]byte, 32*1024)
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		remaining := limit + 1 - int64(len(b))
		if remaining <= 0 {
			break
		}
		n, readErr := f.Read(buf[:min(int64(len(buf)), remaining)])
		b = append(b, buf[:n]...)
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return nil, readErr
		}
	}
	if int64(len(b)) > limit {
		return nil, fmt.Errorf("report exceeds %d-byte limit", limit)
	}
	return b, nil
}

// stripBOM removes an optional leading UTF-8 byte-order mark (\xEF\xBB\xBF).
// Many Windows tools and some CI pipelines emit BOMs; stripping exactly one
// lets them round-trip without an obscure "invalid character '\uf'" error.
func stripBOM(data []byte) []byte {
	if len(data) >= 3 && data[0] == 0xEF && data[1] == 0xBB && data[2] == 0xBF {
		return data[3:]
	}
	return data
}

func decodeOne(data []byte, dst any) error {
	dec := json.NewDecoder(bytes.NewReader(stripBOM(data)))
	if err := dec.Decode(dst); err != nil {
		return err
	}
	var extra any
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		return fmt.Errorf("report must contain one JSON value")
	}
	return nil
}

func binding(in Snapshot, id reportIdentity) (Binding, string) {
	hasIdentity, allComparable := false, true
	unavailableReason := ""
	if id.Commit != "" {
		hasIdentity = true
		if in.Commit == "" {
			allComparable = false
			unavailableReason = "selected_snapshot_has_no_commit"
		} else {
			if !strings.EqualFold(id.Commit, in.Commit) {
				return BindingMismatch, "report_commit_mismatch"
			}
		}
	}
	if id.Tree != "" {
		hasIdentity = true
		if in.Mode != "git" || in.Tree == "" {
			allComparable = false
			if unavailableReason == "" {
				unavailableReason = "report_tree_cannot_bind_selected_snapshot"
			}
		} else {
			if id.Tree != in.Tree {
				return BindingMismatch, "report_tree_mismatch"
			}
		}
	}
	if id.Digest != "" {
		hasIdentity = true
		if !in.DigestComplete || in.Digest == nil || in.Digest.Value == "" {
			allComparable = false
			if unavailableReason == "" {
				unavailableReason = "selected_snapshot_digest_unavailable_or_incomplete"
			}
		} else {
			if !strings.EqualFold(id.Algorithm, in.Digest.Algorithm) || id.Scope != in.Digest.Scope || !strings.EqualFold(id.Digest, in.Digest.Value) {
				return BindingMismatch, "report_digest_mismatch"
			}
		}
	}
	if hasIdentity && allComparable {
		return BindingVerified, ""
	}
	if canCallerAssert(in) {
		return BindingCallerAsserted, "caller_asserted_report_binding"
	}
	if unavailableReason != "" {
		return BindingUnknown, unavailableReason
	}
	return BindingUnknown, "report_has_no_snapshot_identity"
}

func canCallerAssert(in Snapshot) bool {
	return in.CallerAsserted && in.Mode == "directory" && in.Digest != nil && in.Digest.Value != "" && in.DigestComplete
}

// qualifyUnbound downgrades every claim derived from a report unless its source
// binding is verified. A caller assertion records provenance without upgrading
// coverage to verified.
func qualifyUnbound(r *Result) {
	verified := len(r.Ledger) > 0
	for _, entry := range r.Ledger {
		verified = verified && entry.Binding == BindingVerified
	}
	if verified {
		return
	}
	for i := range r.Nodes {
		n := &r.Nodes[i]
		if n.Coverage.Status == mapdoc.CoverageComplete {
			n.Coverage = mapdoc.Coverage{Status: mapdoc.CoverageUnknown, Reasons: []string{"report_snapshot_not_verified"}}
		}
		for j := range n.Facts {
			if n.Facts[j].Coverage.Status == mapdoc.CoverageComplete {
				n.Facts[j].Coverage = mapdoc.Coverage{Status: mapdoc.CoverageUnknown, Reasons: []string{"report_snapshot_not_verified"}}
			}
		}
	}
	for i := range r.Edges {
		if r.Edges[i].Coverage.Status == mapdoc.CoverageComplete {
			r.Edges[i].Coverage = mapdoc.Coverage{Status: mapdoc.CoverageUnknown, Reasons: []string{"report_snapshot_not_verified"}}
		}
	}
}

func cleanReportPath(v string) (string, bool) {
	v = strings.ReplaceAll(strings.TrimSpace(v), "\\", "/")
	if v == "" || strings.HasPrefix(v, "/") || strings.Contains(v, "://") || (len(v) >= 2 && v[1] == ':') {
		return "", false
	}
	v = path.Clean(v)
	if v == ".." || strings.HasPrefix(v, "../") {
		return "", false
	}
	return v, true
}

func evidence(provider, version, p string, line int) mapdoc.Evidence {
	if p == "" {
		p = "."
	}
	e := mapdoc.Evidence{Basis: mapdoc.BasisProviderReported, Path: p, SourceKind: mapdoc.SourceFile, Provider: &mapdoc.Producer{ID: provider, Version: version}}
	if line > 0 {
		e.Span = &mapdoc.Span{StartLine: line, EndLine: line}
	}
	return e
}

func toolNode(tool, version, key string, b Binding, reason string, facts []mapdoc.Fact) mapdoc.Node {
	n := mapdoc.NewNode(mapdoc.NodeToolRun, []string{"."}, tool+":"+key)
	n.Name, n.Properties = tool, map[string]string{"tool": tool, "binding": string(b), "report_identity": key}
	if version != "" {
		n.Properties["version"] = version
	}
	if reason != "" {
		n.Properties["binding_reason"] = reason
	}
	n.Coverage = mapdoc.Coverage{Status: mapdoc.CoverageComplete}
	if b != BindingVerified {
		n.Coverage = mapdoc.Coverage{Status: mapdoc.CoverageUnknown, Reasons: []string{reason}}
	}
	n.Evidence = []mapdoc.Evidence{evidence(tool, fallbackVersion(version), ".", 0)}
	n.Facts = facts
	return n
}

func fallbackVersion(v string) string {
	if v == "" {
		return "unknown"
	}
	return v
}

func merge(dst *Result, src Result) {
	qualifyUnbound(&src)
	dst.Nodes = append(dst.Nodes, src.Nodes...)
	dst.Edges = append(dst.Edges, src.Edges...)
	dst.Diagnostics = append(dst.Diagnostics, src.Diagnostics...)
	dst.Ledger = append(dst.Ledger, src.Ledger...)
}

func coverageFor(r Result) []mapdoc.QuestionCoverage {
	packageStatus := mapdoc.CoverageUnknown
	packageReasons := []string{"no_syft_report_attached"}
	analyzerStatus := mapdoc.CoverageUnknown
	analyzerReasons := []string{"no_provider_reports_attached"}
	hasSyft := false
	if len(r.Ledger) > 0 {
		analyzerStatus = mapdoc.CoveragePartial
		analyzerReasons = []string{"provider_artifacts_do_not_prove_complete_coverage"}
	}
	for _, x := range r.Ledger {
		if x.ReportKind == "syft-json" {
			hasSyft = true
			// Syft's emitted package list is a bounded catalog snapshot; the
			// report does not establish that every relevant cataloger ran.
			packageStatus = mapdoc.CoveragePartial
			packageReasons = []string{"syft_cataloger_scope_not_proven_exhaustive"}
		}
		if x.Binding != BindingVerified {
			analyzerReasons = append(analyzerReasons, x.Reason)
			if x.ReportKind == "syft-json" {
				packageStatus = mapdoc.CoveragePartial
				packageReasons = append(packageReasons, x.Reason)
			}
		}
		if x.State == "unknown" || x.State == "tool_error" {
			analyzerReasons = append(analyzerReasons, x.State)
		}
		if x.Reason == "attachment_record_limit_reached" {
			analyzerReasons = append(analyzerReasons, "attachment_record_limit_reached")
			if x.ReportKind == "syft-json" {
				packageStatus = mapdoc.CoveragePartial
				packageReasons = append(packageReasons, "attachment_record_limit_reached")
			}
		}
	}
	if !hasSyft {
		packageStatus = mapdoc.CoverageUnknown
	}
	packageReasons = compact(packageReasons)
	analyzerReasons = compact(analyzerReasons)
	return []mapdoc.QuestionCoverage{
		{Question: "packages", Scope: ".", Coverage: mapdoc.Coverage{Status: packageStatus, Reasons: packageReasons}},
		{Question: "analyzer_coverage", Scope: ".", Coverage: mapdoc.Coverage{Status: analyzerStatus, Reasons: analyzerReasons}},
		{Question: "routing", Scope: ".", Coverage: mapdoc.Coverage{Status: mapdoc.CoverageUnknown, Reasons: []string{"routing_templates_do_not_prove_execution"}}},
	}
}

func compact(v []string) []string { sort.Strings(v); return slicesCompact(v) }
func slicesCompact(v []string) []string {
	if len(v) == 0 {
		return v
	}
	n := 1
	for _, s := range v[1:] {
		if s != v[n-1] {
			v[n] = s
			n++
		}
	}
	return v[:n]
}
func sortResult(r *Result) {
	sort.Slice(r.Nodes, func(i, j int) bool { return r.Nodes[i].ID < r.Nodes[j].ID })
	sort.Slice(r.Edges, func(i, j int) bool { return r.Edges[i].ID < r.Edges[j].ID })
	sort.Slice(r.Ledger, func(i, j int) bool { return r.Ledger[i].Tool+r.Ledger[i].Scope < r.Ledger[j].Tool+r.Ledger[j].Scope })
	sort.Slice(r.Plans, func(i, j int) bool {
		return r.Plans[i].Tool+r.Plans[i].ComponentID < r.Plans[j].Tool+r.Plans[j].ComponentID
	})
}
