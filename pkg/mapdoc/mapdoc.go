package mapdoc

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path"
	"slices"
	"strings"
)

var ErrInvalid = errors.New("invalid map document")

func New() Document {
	return Document{SchemaVersion: SchemaVersion, Kind: "map", Status: CoverageUnknown, Source: Source{Mode: "directory"}, Coverage: []QuestionCoverage{}, CoverageLedger: []CoverageLedgerEntry{}, Nodes: []Node{}, Edges: []Edge{}}
}

func NewNode(kind NodeKind, paths []string, discriminator string) Node {
	n := Node{Kind: kind, Paths: slices.Clone(paths), Discriminator: discriminator, Coverage: Coverage{Status: CoverageUnknown, Reasons: []string{}}, Evidence: []Evidence{}}
	n.ID = NodeID(n.Kind, n.Paths, n.Discriminator)
	return n
}

func NewEdge(kind EdgeType, from, to, discriminator string) Edge {
	e := Edge{Type: kind, From: from, To: to, Discriminator: discriminator, Coverage: Coverage{Status: CoverageUnknown, Reasons: []string{}}, Evidence: []Evidence{}}
	e.ID = EdgeID(e.Type, e.From, e.To, e.Discriminator)
	return e
}

func NodeID(kind NodeKind, paths []string, discriminator string) string {
	parts := slices.Clone(paths)
	for i := range parts {
		parts[i] = normalizePath(parts[i])
	}
	slices.Sort(parts)
	parts = slices.Compact(parts)
	return stableID(string(kind), append(parts, discriminator)...)
}

func EdgeID(kind EdgeType, from, to, discriminator string) string {
	return stableID("edge-"+string(kind), from, to, discriminator)
}

func stableID(prefix string, parts ...string) string {
	h := sha256.New()
	for _, value := range append([]string{prefix}, parts...) {
		fmt.Fprintf(h, "%d:", len(value))
		h.Write([]byte(value))
	}
	return prefix + ":" + hex.EncodeToString(h.Sum(nil)[:16])
}

// Normalize returns a detached canonical copy suitable for deterministic JSON.
func Normalize(input Document) (Document, error) {
	raw, err := json.Marshal(input)
	if err != nil {
		return Document{}, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	var d Document
	if err := json.Unmarshal(raw, &d); err != nil {
		return Document{}, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	if d.Coverage == nil {
		d.Coverage = []QuestionCoverage{}
	}
	if d.CoverageLedger == nil {
		d.CoverageLedger = []CoverageLedgerEntry{}
	}
	for i := range d.CoverageLedger {
		entry := &d.CoverageLedger[i]
		if entry.CoveredFiles == nil {
			entry.CoveredFiles = []string{}
		}
		slices.Sort(entry.CoveredFiles)
		entry.CoveredFiles = slices.Compact(entry.CoveredFiles)
	}
	if d.Nodes == nil {
		d.Nodes = []Node{}
	}
	if d.Edges == nil {
		d.Edges = []Edge{}
	}
	for i := range d.Coverage {
		normalizeCoverage(&d.Coverage[i].Coverage)
	}
	for i := range d.Nodes {
		n := &d.Nodes[i]
		for j := range n.Paths {
			n.Paths[j] = normalizePath(n.Paths[j])
		}
		slices.Sort(n.Paths)
		n.Paths = slices.Compact(n.Paths)
		normalizeCoverage(&n.Coverage)
		normalizeEvidence(n.Evidence)
		if n.Evidence == nil {
			n.Evidence = []Evidence{}
		}
		for j := range n.Facts {
			normalizeCoverage(&n.Facts[j].Coverage)
			normalizeEvidence(n.Facts[j].Evidence)
			if n.Facts[j].Evidence == nil {
				n.Facts[j].Evidence = []Evidence{}
			}
		}
		slices.SortFunc(n.Facts, func(a, b Fact) int { return strings.Compare(factKey(a), factKey(b)) })
		want := NodeID(n.Kind, n.Paths, n.Discriminator)
		if n.ID != "" && n.ID != want {
			return Document{}, fmt.Errorf("%w: node %q has noncanonical id", ErrInvalid, n.ID)
		}
		n.ID = want
	}
	for i := range d.Edges {
		e := &d.Edges[i]
		normalizeCoverage(&e.Coverage)
		normalizeEvidence(e.Evidence)
		if e.Evidence == nil {
			e.Evidence = []Evidence{}
		}
		want := EdgeID(e.Type, e.From, e.To, e.Discriminator)
		if e.ID != "" && e.ID != want {
			return Document{}, fmt.Errorf("%w: edge %q has noncanonical id", ErrInvalid, e.ID)
		}
		e.ID = want
	}
	slices.SortFunc(d.Coverage, func(a, b QuestionCoverage) int {
		return strings.Compare(a.Question+"\x00"+a.Scope, b.Question+"\x00"+b.Scope)
	})
	slices.SortFunc(d.CoverageLedger, func(a, b CoverageLedgerEntry) int {
		return strings.Compare(a.Tool+"\x00"+a.ReportKind+"\x00"+a.Scope+"\x00"+a.Binding+"\x00"+a.State,
			b.Tool+"\x00"+b.ReportKind+"\x00"+b.Scope+"\x00"+b.Binding+"\x00"+b.State)
	})
	slices.SortFunc(d.Nodes, func(a, b Node) int { return strings.Compare(a.ID, b.ID) })
	slices.SortFunc(d.Edges, func(a, b Edge) int { return strings.Compare(a.ID, b.ID) })
	if err := Validate(d); err != nil {
		return Document{}, err
	}
	return d, nil
}

func normalizeCoverage(c *Coverage) {
	if c.Reasons == nil {
		c.Reasons = []string{}
	}
	slices.Sort(c.Reasons)
	c.Reasons = slices.Compact(c.Reasons)
}
func normalizeEvidence(v []Evidence) {
	for i := range v {
		v[i].Path = normalizePath(v[i].Path)
	}
	slices.SortFunc(v, func(a, b Evidence) int { return strings.Compare(evidenceKey(a), evidenceKey(b)) })
}
func normalizePath(v string) string {
	v = strings.ReplaceAll(v, "\\", "/")
	if v == "" || v == "." {
		return "."
	}
	return strings.TrimPrefix(path.Clean(v), "./")
}
func factKey(v Fact) string         { b, _ := json.Marshal(v); return string(b) }
func evidenceKey(v Evidence) string { b, _ := json.Marshal(v); return string(b) }

func Marshal(d Document) ([]byte, error) {
	normalized, err := Normalize(d)
	if err != nil {
		return nil, err
	}
	b, err := json.MarshalIndent(normalized, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

func UnmarshalStrict(data []byte) (Document, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var d Document
	if err := dec.Decode(&d); err != nil {
		return Document{}, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	if err := ensureEOF(dec); err != nil {
		return Document{}, err
	}
	return Normalize(d)
}
func ensureEOF(dec *json.Decoder) error {
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		return fmt.Errorf("%w: trailing JSON value", ErrInvalid)
	}
	return nil
}
