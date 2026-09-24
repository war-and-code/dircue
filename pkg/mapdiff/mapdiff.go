// Package mapdiff compares two portable dircue map documents without reading
// either source tree.
package mapdiff

import (
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"strconv"
	"strings"

	"dircue/pkg/mapdoc"
)

const SchemaVersion = "1.0.0"

type Report struct {
	SchemaVersion         string           `json:"schema_version"`
	Kind                  string           `json:"kind"`
	Status                string           `json:"status"`
	SourceBinding         string           `json:"source_binding"`
	ObserverCompatibility string           `json:"observer_compatibility"`
	Base                  mapdoc.Source    `json:"base"`
	Head                  mapdoc.Source    `json:"head"`
	Counts                Counts           `json:"counts"`
	Changes               []Change         `json:"changes"`
	ProviderStatus        string           `json:"provider_status"`
	ProviderChanges       []Change         `json:"provider_changes"`
	CoverageChanges       []CoverageChange `json:"coverage_changes"`
	CoverageLedgerStatus  string           `json:"coverage_ledger_status"`
	CoverageLedgerChanges []LedgerChange   `json:"coverage_ledger_changes"`
	Caveats               []string         `json:"caveats"`
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
	Question string       `json:"question"`
	Scope    string       `json:"scope"`
	Base     CoverageView `json:"base"`
	Head     CoverageView `json:"head"`
}

type CoverageView struct {
	Present bool                  `json:"present"`
	Status  mapdoc.CoverageStatus `json:"status,omitempty"`
	Reasons []string              `json:"reasons"`
}

// LedgerChange describes a change in what an attached provider reported
// covering. It is deliberately separate from Changes: provider execution is
// evidence about an observation, not a source-material change.
type LedgerChange struct {
	Tool       string   `json:"tool"`
	ReportKind string   `json:"report_kind"`
	Scope      string   `json:"scope"`
	Status     string   `json:"status"`
	Fields     []string `json:"fields"`
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
		SchemaVersion:   SchemaVersion,
		Kind:            "map_comparison",
		Base:            base.Source,
		Head:            head.Source,
		Changes:         []Change{},
		ProviderStatus:  "same",
		ProviderChanges: []Change{},
		Caveats:         []string{"source ancestry and merge-base are not inferred; the caller selected this pairing"},
	}
	report.SourceBinding = compareSourceDocuments(base, head)
	report.ObserverCompatibility = "same"
	if !slices.Equal(producerSet(base), producerSet(head)) {
		report.ObserverCompatibility = "different"
		report.Caveats = append(report.Caveats, "observer or provider identities differ; map changes cannot be attributed solely to source changes")
	}
	if report.SourceBinding == "unknown" {
		report.Caveats = append(report.Caveats, "source identity is not fully bound; the caller selected this pairing")
	}
	report.CoverageChanges = coverageChanges(base.Coverage, head.Coverage)
	report.CoverageLedgerStatus = "same"
	report.CoverageLedgerChanges = coverageLedgerChanges(base.CoverageLedger, head.CoverageLedger)
	if len(report.CoverageLedgerChanges) > 0 {
		report.CoverageLedgerStatus = "changed"
		report.Caveats = append(report.Caveats, "provider run coverage changed separately from source material")
	}

	baseNodes := indexNodes(base.Nodes)
	headNodes := indexNodes(head.Nodes)
	for _, id := range unionKeys(baseNodes, headNodes) {
		before, inBase := baseNodes[id]
		after, inHead := headNodes[id]
		switch {
		case !inBase:
			change := Change{Entity: "node", ID: id, Status: "added", Material: true, Certainty: "observed"}
			if providerOnlyNode(after) {
				change.Material = false
				report.addProvider(change)
			} else {
				report.add(change)
			}
		case !inHead:
			if providerOnlyNode(before) {
				certainty, reason := providerRemovalCertainty(head, before.Evidence)
				report.addProvider(Change{Entity: "node", ID: id, Status: "removed", Material: false, Certainty: certainty, Reason: reason})
			} else {
				certainty, reason := removalCertainty(head, questionForNode(before.Kind))
				report.add(Change{Entity: "node", ID: id, Status: "removed", Material: true, Certainty: certainty, Reason: reason})
			}
		default:
			fields, material := nodeFields(before, after)
			if len(fields) == 0 {
				if !providerOnlyNode(before) && !providerOnlyNode(after) {
					report.Counts.Unchanged++
				}
				continue
			}
			change := Change{Entity: "node", ID: id, Status: "changed", Fields: fields, Material: material, Certainty: "observed"}
			if providerOnlyNode(before) || providerOnlyNode(after) {
				change.Material = false
				report.addProvider(change)
			} else {
				report.add(change)
			}
		}
	}

	baseEdges := indexEdges(base.Edges)
	headEdges := indexEdges(head.Edges)
	for _, id := range unionKeys(baseEdges, headEdges) {
		before, inBase := baseEdges[id]
		after, inHead := headEdges[id]
		switch {
		case !inBase:
			change := Change{Entity: "edge", ID: id, Status: "added", Material: true, Certainty: "observed"}
			if providerOnlyEdge(after) {
				change.Material = false
				report.addProvider(change)
			} else {
				report.add(change)
			}
		case !inHead:
			if providerOnlyEdge(before) {
				certainty, reason := providerRemovalCertainty(head, before.Evidence)
				report.addProvider(Change{Entity: "edge", ID: id, Status: "removed", Material: false, Certainty: certainty, Reason: reason})
			} else {
				certainty, reason := removalCertainty(head, questionForEdge(before.Type))
				report.add(Change{Entity: "edge", ID: id, Status: "removed", Material: true, Certainty: certainty, Reason: reason})
			}
		default:
			fields, material := edgeFields(before, after)
			if len(fields) == 0 {
				if !providerOnlyEdge(before) && !providerOnlyEdge(after) {
					report.Counts.Unchanged++
				}
				continue
			}
			change := Change{Entity: "edge", ID: id, Status: "changed", Fields: fields, Material: material, Certainty: "observed"}
			if providerOnlyEdge(before) || providerOnlyEdge(after) {
				change.Material = false
				report.addProvider(change)
			} else {
				report.add(change)
			}
		}
	}
	slices.SortFunc(report.Changes, func(a, b Change) int {
		return strings.Compare(a.Entity+"\x00"+a.ID, b.Entity+"\x00"+b.ID)
	})
	slices.SortFunc(report.CoverageChanges, func(a, b CoverageChange) int {
		return strings.Compare(a.Question+"\x00"+a.Scope, b.Question+"\x00"+b.Scope)
	})
	slices.SortFunc(report.ProviderChanges, func(a, b Change) int {
		return strings.Compare(a.Entity+"\x00"+a.ID, b.Entity+"\x00"+b.ID)
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

func (r *Report) addProvider(change Change) {
	r.ProviderChanges = append(r.ProviderChanges, change)
	if change.Certainty == "indeterminate" {
		r.ProviderStatus = "indeterminate"
		r.Caveats = append(r.Caveats, "provider observations disappeared without a comparable provider run in the head map")
	} else if r.ProviderStatus == "same" {
		r.ProviderStatus = "changed"
	}
}

func coverageLedgerChanges(base, head []mapdoc.CoverageLedgerEntry) []LedgerChange {
	type key struct{ tool, reportKind, scope string }
	a := make(map[key][]mapdoc.CoverageLedgerEntry)
	b := make(map[key][]mapdoc.CoverageLedgerEntry)
	for _, entry := range base {
		k := key{entry.Tool, entry.ReportKind, entry.Scope}
		a[k] = append(a[k], entry)
	}
	for _, entry := range head {
		k := key{entry.Tool, entry.ReportKind, entry.Scope}
		b[k] = append(b[k], entry)
	}
	keys := make(map[key]struct{}, len(a)+len(b))
	for k := range a {
		keys[k] = struct{}{}
	}
	for k := range b {
		keys[k] = struct{}{}
	}
	result := make([]LedgerChange, 0)
	for k := range keys {
		before, inBase := a[k]
		after, inHead := b[k]
		slices.SortFunc(before, func(a, b mapdoc.CoverageLedgerEntry) int {
			return strings.Compare(ledgerEntryKey(a), ledgerEntryKey(b))
		})
		slices.SortFunc(after, func(a, b mapdoc.CoverageLedgerEntry) int {
			return strings.Compare(ledgerEntryKey(a), ledgerEntryKey(b))
		})
		if reflect.DeepEqual(before, after) {
			continue
		}
		change := LedgerChange{Tool: k.tool, ReportKind: k.reportKind, Scope: k.scope, Fields: []string{}}
		switch {
		case !inBase:
			change.Status = "added"
		case !inHead:
			change.Status = "removed"
		default:
			change.Status = "changed"
			change.Fields = ledgerFields(before, after)
		}
		result = append(result, change)
	}
	slices.SortFunc(result, func(a, b LedgerChange) int {
		return strings.Compare(a.Tool+"\x00"+a.ReportKind+"\x00"+a.Scope, b.Tool+"\x00"+b.ReportKind+"\x00"+b.Scope)
	})
	return result
}

func ledgerEntryKey(value mapdoc.CoverageLedgerEntry) string {
	return value.Binding + "\x00" + strconv.FormatBool(value.Ran) + "\x00" + strings.Join(value.CoveredFiles, "\x00") + "\x00" + value.State + "\x00" + value.Reason
}

func ledgerFields(a, b []mapdoc.CoverageLedgerEntry) []string {
	fields := []string{}
	compare := func(name string, left, right any) {
		if !reflect.DeepEqual(left, right) {
			fields = append(fields, name)
		}
	}
	if len(a) != len(b) {
		fields = append(fields, "runs")
	}
	project := func(values []mapdoc.CoverageLedgerEntry, field func(mapdoc.CoverageLedgerEntry) any) []any {
		out := make([]any, 0, len(values))
		for _, value := range values {
			out = append(out, field(value))
		}
		return out
	}
	compare("binding", project(a, func(v mapdoc.CoverageLedgerEntry) any { return v.Binding }), project(b, func(v mapdoc.CoverageLedgerEntry) any { return v.Binding }))
	compare("ran", project(a, func(v mapdoc.CoverageLedgerEntry) any { return v.Ran }), project(b, func(v mapdoc.CoverageLedgerEntry) any { return v.Ran }))
	compare("covered_files", project(a, func(v mapdoc.CoverageLedgerEntry) any { return v.CoveredFiles }), project(b, func(v mapdoc.CoverageLedgerEntry) any { return v.CoveredFiles }))
	compare("state", project(a, func(v mapdoc.CoverageLedgerEntry) any { return v.State }), project(b, func(v mapdoc.CoverageLedgerEntry) any { return v.State }))
	compare("reason", project(a, func(v mapdoc.CoverageLedgerEntry) any { return v.Reason }), project(b, func(v mapdoc.CoverageLedgerEntry) any { return v.Reason }))
	return fields
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

// compareSourceDocuments extends compareSource across source modes: a
// directory digest in Git tree-ID form can be compared with a Git map's
// selected tree. Equal IDs prove equal content. Unequal IDs prove different
// content only when the directory digest was complete, because a qualified
// digest (for example with an unapplied filter driver) can differ from what
// Git records for identical files.
func compareSourceDocuments(base, head mapdoc.Document) string {
	a, b := base.Source, head.Source
	if a.Mode == b.Mode {
		return compareSource(a, b)
	}
	git, dir, dirDoc := a, b, head
	if a.Mode == "directory" {
		git, dir, dirDoc = b, a, base
	}
	if dir.Digest == nil || git.Tree == "" || !gitTreeDigest(dir.Digest, git.Tree) {
		return "different"
	}
	if strings.EqualFold(dir.Digest.Value, git.Tree) {
		return "same"
	}
	if sourceBindingStatus(dirDoc) == mapdoc.CoverageComplete {
		return "different"
	}
	return "unknown"
}

func gitTreeDigest(d *mapdoc.Digest, tree string) bool {
	// Accept both the split form (scope="gitignore_filtered", normalization="git_normalized")
	// and the legacy combined form (scope="gitignore_filtered+git_normalized") for
	// documents produced before the Normalization field was added.
	gitNorm := (d.Scope == "gitignore_filtered" && d.Normalization == "git_normalized") ||
		d.Scope == "gitignore_filtered+git_normalized"
	if !gitNorm {
		return false
	}
	switch d.Algorithm {
	case "git-sha1":
		return len(tree) == 40
	case "git-sha256":
		return len(tree) == 64
	}
	return false
}

func sourceBindingStatus(d mapdoc.Document) mapdoc.CoverageStatus {
	for _, q := range d.Coverage {
		if q.Question == mapdoc.QuestionSourceBinding && q.Scope == "." {
			return q.Status
		}
	}
	return mapdoc.CoverageUnknown
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

func providerRemovalCertainty(head mapdoc.Document, evidence []mapdoc.Evidence) (string, string) {
	providers := providerIDs(evidence)
	if len(providers) == 0 {
		return "indeterminate", "provider identity is absent from the removed observation"
	}
	for _, run := range head.CoverageLedger {
		if !slices.Contains(providers, run.Tool) || !run.Ran || run.Binding == "mismatch" {
			continue
		}
		return "indeterminate", "matching provider run does not declare exhaustive observation coverage"
	}
	return "indeterminate", "matching provider run is absent from the head map"
}

func providerOnlyNode(node mapdoc.Node) bool {
	if node.Kind == mapdoc.NodeToolRun {
		return true
	}
	evidence := slices.Clone(node.Evidence)
	for _, fact := range node.Facts {
		evidence = append(evidence, fact.Evidence...)
	}
	return providerOnlyEvidence(evidence)
}

func providerOnlyEdge(edge mapdoc.Edge) bool {
	return edge.Type == mapdoc.EdgeAnalyzedBy || providerOnlyEvidence(edge.Evidence)
}

func providerOnlyEvidence(evidence []mapdoc.Evidence) bool {
	if len(evidence) == 0 {
		return false
	}
	for _, item := range evidence {
		if item.Basis != mapdoc.BasisProviderReported || item.Provider == nil {
			return false
		}
	}
	return true
}

func providerIDs(evidence []mapdoc.Evidence) []string {
	ids := []string{}
	for _, item := range evidence {
		if item.Provider != nil && item.Provider.ID != "" {
			ids = append(ids, item.Provider.ID)
		}
	}
	slices.Sort(ids)
	return slices.Compact(ids)
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
	default:
		return "analyzer_coverage"
	}
}

func coverageChanges(base, head []mapdoc.QuestionCoverage) []CoverageChange {
	type key struct{ question, scope string }
	a := make(map[key]CoverageView, len(base))
	b := make(map[key]CoverageView, len(head))
	for _, value := range base {
		a[key{value.Question, value.Scope}] = CoverageView{Present: true, Status: value.Status, Reasons: value.Reasons}
	}
	for _, value := range head {
		b[key{value.Question, value.Scope}] = CoverageView{Present: true, Status: value.Status, Reasons: value.Reasons}
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

func producerSet(document mapdoc.Document) []string {
	result := []string{}
	add := func(values []mapdoc.Evidence) {
		for _, evidence := range values {
			if evidence.Rule != nil {
				result = append(result, "rule:"+evidence.Rule.ID+"@"+evidence.Rule.Version)
			}
			if evidence.Provider != nil {
				result = append(result, "provider:"+evidence.Provider.ID+"@"+evidence.Provider.Version)
			}
		}
	}
	for _, node := range document.Nodes {
		add(node.Evidence)
		for _, fact := range node.Facts {
			add(fact.Evidence)
		}
	}
	for _, edge := range document.Edges {
		add(edge.Evidence)
	}
	slices.Sort(result)
	return slices.Compact(result)
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
		// Provider annotations on a native node describe an analyzer run, not a
		// source-material mutation. The full Fact still participates in the
		// evidence-level field comparison above.
		if providerOnlyEvidence(value.Evidence) {
			continue
		}
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
