package providerjoin

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"sort"
	"strings"

	"dircue/pkg/mapdoc"
)

const (
	defaultMaxBytes   = 32 << 20
	defaultMaxRecords = 100_000
)

type reportIdentity struct {
	Tree, Algorithm, Scope, Digest string
}

func join(ctx context.Context, in Input, attachments []Attachment, opts Options) (Result, error) {
	if opts.MaxReportBytes <= 0 {
		opts.MaxReportBytes = defaultMaxBytes
	}
	if opts.MaxRecords <= 0 {
		opts.MaxRecords = defaultMaxRecords
	}
	var out Result
	for _, a := range attachments {
		if err := ctx.Err(); err != nil {
			return Result{}, err
		}
		data, err := readBounded(a.Path, opts.MaxReportBytes)
		if err != nil {
			return Result{}, fmt.Errorf("attach %s: %w", a.Kind, err)
		}
		switch strings.ToLower(a.Kind) {
		case "syft", "syft-json":
			r, err := ingestSyft(data, in, opts.MaxRecords)
			if err != nil {
				return Result{}, fmt.Errorf("attach syft-json: %w", err)
			}
			merge(&out, r)
		case "sarif":
			r, err := ingestSARIF(data, in, opts.MaxRecords)
			if err != nil {
				return Result{}, fmt.Errorf("attach sarif: %w", err)
			}
			merge(&out, r)
		case "noir", "noir-json":
			r, err := ingestNoir(data, in, opts.MaxRecords)
			if err != nil {
				return Result{}, fmt.Errorf("attach noir-json: %w", err)
			}
			merge(&out, r)
		default:
			return Result{}, fmt.Errorf("unsupported attachment kind %q", a.Kind)
		}
	}
	out.Plans = Route(in)
	out.Coverage = coverageFor(out)
	sortResult(&out)
	return out, nil
}

func readBounded(filename string, limit int64) ([]byte, error) {
	f, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	r := io.LimitReader(f, limit+1)
	b, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > limit {
		return nil, fmt.Errorf("report exceeds %d-byte limit", limit)
	}
	return b, nil
}

func decodeOne(data []byte, dst any) error {
	dec := json.NewDecoder(bytes.NewReader(data))
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
	if id.Tree != "" {
		if in.Mode != "git" || in.Tree == "" {
			return BindingUnknown, "report_tree_cannot_bind_selected_snapshot"
		}
		if id.Tree != in.Tree {
			return BindingMismatch, "report_tree_mismatch"
		}
		return BindingVerified, ""
	}
	if id.Digest != "" {
		if in.Digest == nil || in.Digest.Value == "" {
			return BindingUnknown, "selected_snapshot_has_no_digest"
		}
		if !strings.EqualFold(id.Algorithm, in.Digest.Algorithm) || id.Scope != in.Digest.Scope || !strings.EqualFold(id.Digest, in.Digest.Value) {
			return BindingMismatch, "report_digest_mismatch"
		}
		return BindingVerified, ""
	}
	return BindingUnknown, "report_has_no_snapshot_identity"
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

func reportKey(data []byte) string { s := sha256.Sum256(data); return hex.EncodeToString(s[:8]) }

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
	dst.Nodes = append(dst.Nodes, src.Nodes...)
	dst.Edges = append(dst.Edges, src.Edges...)
	dst.Diagnostics = append(dst.Diagnostics, src.Diagnostics...)
	dst.Ledger = append(dst.Ledger, src.Ledger...)
}

func coverageFor(r Result) []mapdoc.QuestionCoverage {
	providerStatus := mapdoc.CoverageComplete
	providerReasons := []string{}
	for _, x := range r.Ledger {
		if x.Binding != BindingVerified {
			providerStatus = mapdoc.CoveragePartial
			providerReasons = append(providerReasons, x.Reason)
		}
	}
	if len(r.Ledger) == 0 {
		providerStatus = mapdoc.CoverageUnknown
		providerReasons = []string{"no_provider_reports_attached"}
	}
	providerReasons = compact(providerReasons)
	return []mapdoc.QuestionCoverage{
		{Question: "packages", Scope: ".", Coverage: mapdoc.Coverage{Status: providerStatus, Reasons: providerReasons}},
		{Question: "analyzer_coverage", Scope: ".", Coverage: mapdoc.Coverage{Status: providerStatus, Reasons: providerReasons}},
		{Question: "routing", Scope: ".", Coverage: mapdoc.Coverage{Status: mapdoc.CoverageComplete, Reasons: []string{}}},
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
