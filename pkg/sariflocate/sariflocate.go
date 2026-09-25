// Package sariflocate adds map ownership metadata to SARIF result locations.
// It treats both inputs as data: it performs no I/O, execution, or network access.
package sariflocate

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"path"
	"regexp"
	"slices"
	"strings"

	"github.com/war-and-code/dircue/pkg/mapdoc"
)

const PropertyName = "dircue.map"

var ErrLimit = errors.New("SARIF input limit exceeded")

type Limits struct {
	Bytes, Runs, Results, Locations int
}

func DefaultLimits() Limits {
	return Limits{Bytes: 64 << 20, Runs: 64, Results: 1_000_000, Locations: 4_000_000}
}

// Options supplies identity information that is deliberately absent from a
// portable map document. SourceURI is used only to relativize absolute SARIF
// URIs and is never copied into output. Digest binds directory maps when set.
type Options struct {
	Limits    Limits
	SourceURI string
	// URIBases defines uriBaseId values that a report uses without declaring
	// them in originalUriBaseIds, such as %SRCROOT%. Keys may be given with
	// or without the surrounding percent signs. Declared bases always win.
	URIBases      map[string]string
	Digest        *mapdoc.Digest
	CaseSensitive *bool
}

type Summary struct {
	Runs        []RunSummary   `json:"runs"`
	NodeCounts  []NodeCount    `json:"node_counts"`
	Resolutions map[string]int `json:"resolutions"`
}
type RunSummary struct {
	Index   int    `json:"index"`
	Tool    string `json:"tool,omitempty"`
	Version string `json:"version,omitempty"`
	Binding string `json:"binding"`
}
type NodeCount struct {
	NodeID  string `json:"node_id"`
	Kind    string `json:"kind"`
	Tool    string `json:"tool,omitempty"`
	Version string `json:"version,omitempty"`
	Count   int    `json:"count"`
}

type annotation struct {
	Resolution  string   `json:"resolution"`
	Components  []string `json:"components"`
	Deployables []string `json:"deployables"`
	Interfaces  []string `json:"interfaces"`
	ContentRole string   `json:"content_role,omitempty"`
}

type rawObject map[string]json.RawMessage

// Annotate returns a detached SARIF log with only location property bags
// extended. Unknown fields and existing property values are retained.
func Annotate(input []byte, doc mapdoc.Document, opts Options) ([]byte, Summary, error) {
	limits := opts.Limits
	if limits == (Limits{}) {
		limits = DefaultLimits()
	}
	if limits.Bytes <= 0 || len(input) > limits.Bytes {
		return nil, Summary{}, fmt.Errorf("%w: bytes", ErrLimit)
	}
	if err := mapdoc.Validate(doc); err != nil {
		return nil, Summary{}, fmt.Errorf("map: %w", err)
	}
	// Strip an optional UTF-8 BOM emitted by some Windows tools.
	if len(input) >= 3 && input[0] == 0xEF && input[1] == 0xBB && input[2] == 0xBF {
		input = input[3:]
	}
	var root rawObject
	dec := json.NewDecoder(bytes.NewReader(input))
	if err := dec.Decode(&root); err != nil {
		return nil, Summary{}, fmt.Errorf("SARIF: %w", err)
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		return nil, Summary{}, errors.New("SARIF: trailing JSON value")
	}
	if stringValue(root["version"]) != "2.1.0" {
		return nil, Summary{}, errors.New("SARIF: version must be 2.1.0")
	}
	var runs []rawObject
	if err := json.Unmarshal(root["runs"], &runs); err != nil {
		return nil, Summary{}, errors.New("SARIF: runs must be an array")
	}
	if limits.Runs <= 0 || len(runs) > limits.Runs {
		return nil, Summary{}, fmt.Errorf("%w: runs", ErrLimit)
	}
	idx := newIndex(doc, opts)
	summary := Summary{Resolutions: map[string]int{}}
	counts := map[string]int{}
	totalResults, totalLocations := 0, 0
	for ri := range runs {
		tool, version := toolIdentity(runs[ri])
		binding := sourceBinding(runs[ri], doc, opts)
		summary.Runs = append(summary.Runs, RunSummary{Index: ri, Tool: tool, Version: version, Binding: binding})
		bases := baseURIs(runs[ri], opts.URIBases)
		var results []rawObject
		if raw, ok := runs[ri]["results"]; ok {
			if err := json.Unmarshal(raw, &results); err != nil {
				return nil, Summary{}, errors.New("SARIF: results must be an array")
			}
		}
		totalResults += len(results)
		if limits.Results <= 0 || totalResults > limits.Results {
			return nil, Summary{}, fmt.Errorf("%w: results", ErrLimit)
		}
		for j := range results {
			resultNodes := map[string]bool{}
			for _, field := range []string{"locations", "relatedLocations"} {
				var locations []rawObject
				raw, ok := results[j][field]
				if !ok {
					continue
				}
				if err := json.Unmarshal(raw, &locations); err != nil {
					return nil, Summary{}, fmt.Errorf("SARIF: %s must be an array", field)
				}
				totalLocations += len(locations)
				if limits.Locations <= 0 || totalLocations > limits.Locations {
					return nil, Summary{}, fmt.Errorf("%w: locations", ErrLimit)
				}
				for k := range locations {
					a := idx.locate(locations[k], bases, binding)
					if err := setAnnotation(locations[k], a); err != nil {
						return nil, Summary{}, err
					}
					summary.Resolutions[a.Resolution]++
					for _, id := range append(append(slices.Clone(a.Components), a.Deployables...), a.Interfaces...) {
						resultNodes[id] = true
					}
				}
				results[j][field] = marshal(locations)
			}
			for id := range resultNodes {
				counts[id+"\x00"+tool+"\x00"+version]++
			}
		}
		runs[ri]["results"] = marshal(results)
	}
	root["runs"] = marshal(runs)
	for key, count := range counts {
		parts := strings.Split(key, "\x00")
		summary.NodeCounts = append(summary.NodeCounts, NodeCount{NodeID: parts[0], Kind: string(idx.nodes[parts[0]].Kind), Tool: parts[1], Version: parts[2], Count: count})
	}
	slices.SortFunc(summary.NodeCounts, func(a, b NodeCount) int {
		return strings.Compare(a.NodeID+"\x00"+a.Tool+"\x00"+a.Version, b.NodeID+"\x00"+b.Tool+"\x00"+b.Version)
	})
	out, err := json.Marshal(root)
	if err != nil {
		return nil, Summary{}, err
	}
	return append(out, '\n'), summary, nil
}

func setAnnotation(location rawObject, a annotation) error {
	var props rawObject
	if raw, ok := location["properties"]; ok {
		if err := json.Unmarshal(raw, &props); err != nil || props == nil {
			return errors.New("SARIF: location properties must be an object")
		}
	}
	if props == nil {
		props = rawObject{}
	}
	props[PropertyName] = marshal(a)
	location["properties"] = marshal(props)
	return nil
}

type index struct {
	doc       mapdoc.Document
	nodes     map[string]mapdoc.Node
	sourceURI string
	sensitive bool
}

func newIndex(doc mapdoc.Document, opts Options) index {
	sensitive := true
	if opts.CaseSensitive != nil {
		sensitive = *opts.CaseSensitive
	} else if windowsPath(opts.SourceURI) {
		sensitive = false
	}
	n := map[string]mapdoc.Node{}
	for _, node := range doc.Nodes {
		n[node.ID] = node
	}
	return index{doc: doc, nodes: n, sourceURI: opts.SourceURI, sensitive: sensitive}
}

func (i index) locate(loc rawObject, bases map[string]string, binding string) annotation {
	a := annotation{Resolution: "not_in_snapshot", Components: []string{}, Deployables: []string{}, Interfaces: []string{}}
	if binding == "mismatch" {
		return a
	}
	artifact, region, ok := physical(loc)
	if !ok {
		a.Resolution = "unresolvable_uri"
		return a
	}
	rel, status := resolveURI(stringValue(artifact["uri"]), stringValue(artifact["uriBaseId"]), bases, i.sourceURI, i.sensitive)
	if status != "" {
		a.Resolution = status
		return a
	}
	line := intValue(region["startLine"])
	for _, n := range i.doc.Nodes {
		matched := false
		for _, p := range ownershipPaths(n) {
			if containsPath(p, rel, i.sensitive) {
				matched = true
				break
			}
		}
		if !matched {
			continue
		}
		switch n.Kind {
		case mapdoc.NodeComponent:
			a.Components = append(a.Components, n.ID)
		case mapdoc.NodeDeployable:
			a.Deployables = append(a.Deployables, n.ID)
		case mapdoc.NodeInterface:
			if interfaceContains(n, rel, line, i.sensitive) {
				a.Interfaces = append(a.Interfaces, n.ID)
			}
		case mapdoc.NodeContent:
			if exactPath(n, rel, i.sensitive) && n.Properties["role"] != "" {
				a.ContentRole = n.Properties["role"]
			}
		}
	}
	slices.Sort(a.Components)
	slices.Sort(a.Deployables)
	slices.Sort(a.Interfaces)
	if len(a.Components) > 1 || len(a.Deployables) > 1 || len(a.Interfaces) > 1 {
		a.Resolution = "ambiguous"
	} else if len(a.Components) > 0 || len(a.Deployables) > 0 || a.ContentRole != "" || len(a.Interfaces) > 0 {
		a.Resolution = "resolved"
	}
	return a
}

func ownershipPaths(n mapdoc.Node) []string {
	if root := n.Properties["root"]; root != "" {
		return []string{root}
	}
	if n.Kind == mapdoc.NodeComponent {
		out := []string{}
		for _, p := range n.Paths {
			out = append(out, path.Dir(p))
		}
		return out
	}
	return n.Paths
}
func interfaceContains(n mapdoc.Node, rel string, line int, sensitive bool) bool {
	for _, e := range n.Evidence {
		if samePath(e.Path, rel, sensitive) && (e.Span == nil || line == 0 || (line >= e.Span.StartLine && line <= e.Span.EndLine)) {
			return true
		}
	}
	return false
}
func exactPath(n mapdoc.Node, rel string, sensitive bool) bool {
	for _, p := range n.Paths {
		if samePath(p, rel, sensitive) {
			return true
		}
	}
	return false
}

func physical(loc rawObject) (rawObject, rawObject, bool) {
	var p rawObject
	if json.Unmarshal(loc["physicalLocation"], &p) != nil {
		return nil, nil, false
	}
	var a rawObject
	if json.Unmarshal(p["artifactLocation"], &a) != nil || stringValue(a["uri"]) == "" {
		return nil, nil, false
	}
	var r rawObject
	_ = json.Unmarshal(p["region"], &r)
	return a, r, true
}

func baseURIs(run rawObject, supplied map[string]string) map[string]string {
	var raw map[string]rawObject
	_ = json.Unmarshal(run["originalUriBaseIds"], &raw)
	out := map[string]string{}
	for id, v := range supplied {
		name := strings.Trim(id, "%")
		out[name] = v + "\x00"
		out["%"+name+"%"] = v + "\x00"
	}
	for id, v := range raw {
		out[id] = stringValue(v["uri"]) + "\x00" + stringValue(v["uriBaseId"])
	}
	return out
}

func resolveURI(uri, baseID string, bases map[string]string, source string, sensitive bool) (string, string) {
	seen := map[string]bool{}
	// An absolute URI does not depend on a base (SARIF 2.1.0 section 3.4.4),
	// so a uriBaseId that some tools still attach to absolute paths is ignored.
	for baseID != "" && !absoluteURI(uri) {
		if seen[baseID] {
			return "", "unresolvable_uri"
		}
		seen[baseID] = true
		v, ok := bases[baseID]
		if !ok {
			return "", "unresolvable_uri"
		}
		parts := strings.SplitN(v, "\x00", 2)
		uri = joinURI(parts[0], uri)
		baseID = parts[1]
	}
	u, err := url.PathUnescape(uri)
	if err != nil || strings.ContainsRune(u, '\x00') {
		return "", "unresolvable_uri"
	}
	uri = u
	parsedURI, parseErr := url.Parse(uri)
	if parseErr != nil {
		return "", "unresolvable_uri"
	}
	if parsedURI.Scheme != "" && !strings.EqualFold(parsedURI.Scheme, "file") && !driveRE.MatchString(uri) {
		return "", "unresolvable_uri"
	}
	if strings.HasPrefix(strings.ToLower(uri), "file:") {
		parsed := parsedURI
		if parsed.RawQuery != "" || parsed.Fragment != "" || (parsed.Host != "" && parsed.Host != "localhost") {
			return "", "unresolvable_uri"
		}
		uri = parsed.Path
		if len(uri) >= 3 && uri[0] == '/' && uri[2] == ':' {
			uri = uri[1:]
		}
	}
	uri = strings.ReplaceAll(uri, "\\", "/")
	if absolutePath(uri) {
		if source == "" {
			return "", "unresolvable_uri"
		}
		s := strings.ReplaceAll(source, "\\", "/")
		if strings.HasPrefix(strings.ToLower(s), "file:") {
			p, e := url.Parse(s)
			if e != nil {
				return "", "unresolvable_uri"
			}
			s = p.Path
			if len(s) >= 3 && s[0] == '/' && s[2] == ':' {
				s = s[1:]
			}
		}
		if !containsPath(s, uri, sensitive) {
			return "", "outside_root"
		}
		uri = strings.TrimPrefix(matchCase(uri, s, sensitive), "/")
	}
	clean := path.Clean(uri)
	if clean == ".." || strings.HasPrefix(clean, "../") || strings.HasPrefix(clean, "/") {
		return "", "outside_root"
	}
	return strings.TrimPrefix(clean, "./"), ""
}
func joinURI(base, rel string) string {
	if absolutePath(rel) || strings.Contains(rel, "://") {
		return rel
	}
	return strings.TrimSuffix(base, "/") + "/" + rel
}
func containsPath(root, name string, sensitive bool) bool {
	root = strings.TrimSuffix(path.Clean(strings.ReplaceAll(root, "\\", "/")), "/")
	name = path.Clean(strings.ReplaceAll(name, "\\", "/"))
	if !sensitive {
		root = strings.ToLower(root)
		name = strings.ToLower(name)
	}
	return root == "." || name == root || strings.HasPrefix(name, root+"/")
}
func samePath(a, b string, sensitive bool) bool {
	if !sensitive {
		return strings.EqualFold(path.Clean(a), path.Clean(b))
	}
	return path.Clean(a) == path.Clean(b)
}
func matchCase(name, root string, sensitive bool) string {
	if sensitive {
		return strings.TrimPrefix(name, root)
	}
	return name[len(root):]
}

var driveRE = regexp.MustCompile(`^[A-Za-z]:[/\\]`)

func windowsPath(v string) bool {
	return driveRE.MatchString(v) || regexp.MustCompile(`^file:/+[A-Za-z]:`).MatchString(v)
}
func absolutePath(v string) bool { return strings.HasPrefix(v, "/") || driveRE.MatchString(v) }

func absoluteURI(v string) bool {
	return absolutePath(v) || strings.HasPrefix(strings.ToLower(v), "file:")
}

func sourceBinding(run rawObject, doc mapdoc.Document, opts Options) string {
	if doc.Source.Mode == "directory" {
		if doc.Source.Digest == nil || opts.Digest == nil {
			return "unknown"
		}
		if *doc.Source.Digest == *opts.Digest {
			return "matched"
		}
		return "mismatch"
	}
	var vcs []rawObject
	_ = json.Unmarshal(run["versionControlProvenance"], &vcs)
	if len(vcs) == 0 {
		return "unknown"
	}
	for _, v := range vcs {
		rev := stringValue(v["revisionId"])
		if rev != "" {
			commit := doc.Source.Commit
			// Older saved maps only carried the selector. A full object ID is
			// comparable, but a symbolic selector must stay unknown.
			if commit == "" {
				commit = doc.Source.Revision
			}
			if !gitObjectID(commit) || !gitObjectID(rev) {
				return "unknown"
			}
			if strings.EqualFold(rev, commit) {
				return "matched"
			}
			return "mismatch"
		}
	}
	return "unknown"
}

var gitObjectIDRE = regexp.MustCompile(`^(?:[0-9a-fA-F]{40}|[0-9a-fA-F]{64})$`)

func gitObjectID(value string) bool { return gitObjectIDRE.MatchString(value) }
func toolIdentity(run rawObject) (string, string) {
	var tool, driver rawObject
	_ = json.Unmarshal(run["tool"], &tool)
	_ = json.Unmarshal(tool["driver"], &driver)
	return stringValue(driver["name"]), stringValue(driver["version"])
}
func marshal(v any) json.RawMessage        { b, _ := json.Marshal(v); return b }
func stringValue(v json.RawMessage) string { var s string; _ = json.Unmarshal(v, &s); return s }
func intValue(v json.RawMessage) int       { var n int; _ = json.Unmarshal(v, &n); return n }
