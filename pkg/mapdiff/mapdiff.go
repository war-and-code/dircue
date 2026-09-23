// Package mapdiff compares two portable dircue map documents without reading
// either source tree.
package mapdiff

import (
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"strings"

	"dircue/pkg/mapdoc"
)

const SchemaVersion = "1.0.0"

type Report struct {
	SchemaVersion   string           `json:"schema_version"`
	Kind            string           `json:"kind"`
	Status          string           `json:"status"`
	SourceBinding   string           `json:"source_binding"`
	Base            mapdoc.Source    `json:"base"`
	Head            mapdoc.Source    `json:"head"`
	Counts          Counts           `json:"counts"`
	Changes         []Change         `json:"changes"`
	CoverageChanges []CoverageChange `json:"coverage_changes"`
	Caveats         []string         `json:"caveats"`
}

type Counts struct {
	Added                int `json:"added"`
	Removed              int `json:"removed"`
	Changed              int `json:"changed"`
	Unchanged            int `json:"unchanged"`
	Material             int `json:"material"`
	IndeterminateRemoval int `json:"indeterminate_removal"`
}

type Change struct {
	Entity    string   `json:"entity"`
	ID        string   `json:"id"`
	Status    string   `json:"status"`
	Fields    []string `json:"fields"`
	Material  bool     `json:"material"`
	Certainty string   `json:"certainty"`
	Reason    string   `json:"reason,omitempty"`
}

type CoverageChange struct {
	Question string          `json:"question"`
	Scope    string          `json:"scope"`
	Base     mapdoc.Coverage `json:"base"`
	Head     mapdoc.Coverage `json:"head"`
}

// Compare reports observable map changes. It does not infer Git ancestry,
// renames, or source equivalence when a directory map lacks a full digest.
func Compare(baseInput, headInput mapdoc.Document) (Report, error) {
	base, err := mapdoc.Normalize(baseInput)
	if err != nil {
		return Report{}, fmt.Errorf("base map: %w", err)
	}
	head, err := mapdoc.Normalize(headInput)
	if err != nil {
		return Report{}, fmt.Errorf("head map: %w", err)
	}
	report := Report{
		SchemaVersion: SchemaVersion,
		Kind:          "map_comparison",
		Base:          base.Source,
		Head:          head.Source,
		Changes:       []Change{},
		Caveats:       []string{"source ancestry and merge-base are not inferred; the caller selected this pairing"},
	}
	report.SourceBinding = compareSource(base.Source, head.Source)
	if report.SourceBinding == "unknown" {
		report.Caveats = append(report.Caveats, "source identity is not fully bound; the caller selected this pairing")
	}
	report.CoverageChanges = coverageChanges(base.Coverage, head.Coverage)

	baseNodes := indexNodes(base.Nodes)
	headNodes := indexNodes(head.Nodes)
	for _, id := range unionKeys(baseNodes, headNodes) {
		before, inBase := baseNodes[id]
		after, inHead := headNodes[id]
		switch {
		case !inBase:
			report.add(Change{Entity: "node", ID: id, Status: "added", Material: true, Certainty: "observed"})
		case !inHead:
			certainty, reason := removalCertainty(head, questionForNode(before.Kind))
			report.add(Change{Entity: "node", ID: id, Status: "removed", Material: true, Certainty: certainty, Reason: reason})
		default:
			fields, material := nodeFields(before, after)
			if len(fields) == 0 {
				report.Counts.Unchanged++
				continue
			}
			report.add(Change{Entity: "node", ID: id, Status: "changed", Fields: fields, Material: material, Certainty: "observed"})
		}
	}

	baseEdges := indexEdges(base.Edges)
	headEdges := indexEdges(head.Edges)
	for _, id := range unionKeys(baseEdges, headEdges) {
		before, inBase := baseEdges[id]
		after, inHead := headEdges[id]
		switch {
		case !inBase:
			report.add(Change{Entity: "edge", ID: id, Status: "added", Material: true, Certainty: "observed"})
		case !inHead:
			certainty, reason := removalCertainty(head, questionForEdge(before.Type))
			report.add(Change{Entity: "edge", ID: id, Status: "removed", Material: true, Certainty: certainty, Reason: reason})
		default:
			fields, material := edgeFields(before, after)
			if len(fields) == 0 {
				report.Counts.Unchanged++
				continue
			}
			report.add(Change{Entity: "edge", ID: id, Status: "changed", Fields: fields, Material: material, Certainty: "observed"})
		}
	}
	slices.SortFunc(report.Changes, func(a, b Change) int {
		return strings.Compare(a.Entity+"\x00"+a.ID, b.Entity+"\x00"+b.ID)
	})
	slices.SortFunc(report.CoverageChanges, func(a, b CoverageChange) int {
		return strings.Compare(a.Question+"\x00"+a.Scope, b.Question+"\x00"+b.Scope)
	})
	slices.Sort(report.Caveats)
	report.Caveats = slices.Compact(report.Caveats)
	switch {
	case report.Counts.Material > 0:
		report.Status = "changed"
	case report.Counts.IndeterminateRemoval > 0:
		report.Status = "indeterminate"
	default:
		report.Status = "unchanged"
	}
	return report, nil
}

func (r *Report) add(change Change) {
	r.Changes = append(r.Changes, change)
	switch change.Status {
	case "added":
		r.Counts.Added++
	case "removed":
		r.Counts.Removed++
	case "changed":
		r.Counts.Changed++
	}
	if change.Material && change.Certainty != "indeterminate" {
		r.Counts.Material++
	}
	if change.Status == "removed" && change.Certainty == "indeterminate" {
		r.Counts.IndeterminateRemoval++
		r.Caveats = append(r.Caveats, "incomplete head coverage prevents one or more absences from being confirmed as removals")
	}
}

func compareSource(a, b mapdoc.Source) string {
	if a.Mode != b.Mode {
		return "different"
	}
	if a.Mode == "git" {
		if a.Tree == b.Tree {
			return "same"
		}
		return "different"
	}
	if a.Digest == nil || b.Digest == nil {
		return "unknown"
	}
	if reflect.DeepEqual(a.Digest, b.Digest) {
		return "same"
	}
	return "different"
}

func removalCertainty(head mapdoc.Document, question string) (string, string) {
	for _, coverage := range head.Coverage {
		if coverage.Question == question && coverage.Scope == "." {
			if coverage.Status == mapdoc.CoverageComplete {
				return "confirmed", ""
			}
			return "indeterminate", question + " coverage in the head map is " + string(coverage.Status)
		}
	}
	return "indeterminate", question + " coverage is absent from the head map"
}

func questionForNode(kind mapdoc.NodeKind) string {
	switch kind {
	case mapdoc.NodeContent:
		return "content"
	case mapdoc.NodeComponent:
		return "components"
	case mapdoc.NodeDeployable:
		return "deployables"
	case mapdoc.NodeInterface:
		return "interfaces"
	case mapdoc.NodeCapability:
		return "capabilities"
	case mapdoc.NodePackage:
		return "packages"
	default:
		return "analyzer_coverage"
	}
}

func questionForEdge(kind mapdoc.EdgeType) string {
	switch kind {
	case mapdoc.EdgeContains, mapdoc.EdgeMemberOf, mapdoc.EdgeDependsOnLocal:
		return "components"
	case mapdoc.EdgeBuilds, mapdoc.EdgeRuns:
		return "deployables"
	case mapdoc.EdgeExposes, mapdoc.EdgeDeclares:
		return "interfaces"
	case mapdoc.EdgeUsesCapability:
		return "capabilities"
	case mapdoc.EdgePackagedIn:
		return "packages"
	case mapdoc.EdgeConflictsWith:
		return "routing"
	default:
		return "analyzer_coverage"
	}
}

func coverageChanges(base, head []mapdoc.QuestionCoverage) []CoverageChange {
	type key struct{ question, scope string }
	a := make(map[key]mapdoc.Coverage, len(base))
	b := make(map[key]mapdoc.Coverage, len(head))
	for _, value := range base {
		a[key{value.Question, value.Scope}] = value.Coverage
	}
	for _, value := range head {
		b[key{value.Question, value.Scope}] = value.Coverage
	}
	keys := make(map[key]struct{}, len(a)+len(b))
	for value := range a {
		keys[value] = struct{}{}
	}
	for value := range b {
		keys[value] = struct{}{}
	}
	result := []CoverageChange{}
	for value := range keys {
		if !reflect.DeepEqual(a[value], b[value]) {
			result = append(result, CoverageChange{Question: value.question, Scope: value.scope, Base: a[value], Head: b[value]})
		}
	}
	return result
}

func nodeFields(a, b mapdoc.Node) ([]string, bool) {
	fields := []string{}
	material := false
	compare := func(name string, left, right any, isMaterial bool) {
		if !reflect.DeepEqual(left, right) {
			fields = append(fields, name)
			material = material || isMaterial
		}
	}
	compare("name", a.Name, b.Name, true)
	compare("properties", a.Properties, b.Properties, true)
	if !reflect.DeepEqual(a.Facts, b.Facts) {
		fields = append(fields, "facts")
		material = material || !reflect.DeepEqual(semanticFacts(a.Facts), semanticFacts(b.Facts))
	}
	compare("coverage", a.Coverage, b.Coverage, false)
	compare("evidence", a.Evidence, b.Evidence, false)
	return fields, material
}

type semanticFact struct {
	Kind       string
	Name       string
	Value      string
	State      string
	Condition  string
	Properties map[string]string
}

func semanticFacts(values []mapdoc.Fact) []semanticFact {
	result := make([]semanticFact, 0, len(values))
	for _, value := range values {
		result = append(result, semanticFact{
			Kind: value.Kind, Name: value.Name, Value: value.Value, State: value.State,
			Condition: value.Condition, Properties: value.Properties,
		})
	}
	return result
}

func edgeFields(a, b mapdoc.Edge) ([]string, bool) {
	fields := []string{}
	material := false
	if !reflect.DeepEqual(a.Properties, b.Properties) {
		fields = append(fields, "properties")
		material = true
	}
	if !reflect.DeepEqual(a.Coverage, b.Coverage) {
		fields = append(fields, "coverage")
	}
	if !reflect.DeepEqual(a.Evidence, b.Evidence) {
		fields = append(fields, "evidence")
	}
	return fields, material
}

func indexNodes(values []mapdoc.Node) map[string]mapdoc.Node {
	result := make(map[string]mapdoc.Node, len(values))
	for _, value := range values {
		result[value.ID] = value
	}
	return result
}

func indexEdges(values []mapdoc.Edge) map[string]mapdoc.Edge {
	result := make(map[string]mapdoc.Edge, len(values))
	for _, value := range values {
		result[value.ID] = value
	}
	return result
}

func unionKeys[T any](a, b map[string]T) []string {
	keys := make([]string, 0, len(a)+len(b))
	seen := make(map[string]struct{}, len(a)+len(b))
	for key := range a {
		seen[key] = struct{}{}
		keys = append(keys, key)
	}
	for key := range b {
		if _, ok := seen[key]; !ok {
			keys = append(keys, key)
		}
	}
	slices.Sort(keys)
	return keys
}

func Marshal(report Report) ([]byte, error) {
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}
