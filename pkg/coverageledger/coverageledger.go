// Package coverageledger derives conservative analyzer run accounting from a
// dircue map. It does not execute tools or interpret their findings.
package coverageledger

import (
	"path"
	"slices"
	"sort"
	"strings"

	"github.com/war-and-code/dircue/pkg/mapdoc"
)

const DescriptorVersion = "1.0.0"

type Descriptor struct {
	Tool              string
	Languages         []string
	RequiresFramework bool
	Prerequisites     []string
	Source            string
	GenericFallback   bool
}

func Descriptors() []Descriptor {
	return []Descriptor{
		{Tool: "bca", Languages: []string{"c", "c++", "c#", "go", "java", "javascript", "kotlin", "python", "rust", "typescript"}, Source: "https://dekobon.github.io/big-code-analysis/"},
		{Tool: "bifrost", Languages: []string{"c", "c++", "c#", "go", "java", "javascript", "kotlin", "php", "python", "ruby", "rust", "swift", "typescript"}, Source: "https://bifrost.brokk.ai/capabilities/"},
		{Tool: "noir", Languages: []string{"generic"}, RequiresFramework: true, Source: "https://github.com/owasp-noir/noir"},
		{Tool: "opentaint", Languages: []string{"java", "kotlin"}, Prerequisites: []string{"compiled_bytecode"}, Source: "https://github.com/seqra/opentaint"},
		{Tool: "scc", Languages: []string{"generic"}, Source: "https://github.com/boyter/scc"},
		{Tool: "syft", Languages: []string{"generic"}, Source: "https://github.com/anchore/syft"},
		{Tool: "sarif", Languages: []string{"generic"}, Source: "https://docs.oasis-open.org/sarif/sarif/v2.1.0/", GenericFallback: true},
	}
}

type population struct {
	id, root      string
	languages     []string
	languageBasis string
	framework     string
	node          mapdoc.Node
}

// Reconcile replaces derived analyzer accounting with deterministic entries.
func Reconcile(d *mapdoc.Document) {
	pops := populations(*d)
	descriptors := Descriptors()
	var entries []mapdoc.AnalyzerCoverageEntry
	for _, pop := range pops {
		for _, language := range pop.languages {
			for _, descriptor := range descriptors[:len(descriptors)-1] {
				entries = append(entries, account(pop, language, descriptor, d.CoverageLedger))
			}
		}
	}
	// A SARIF producer not represented by a built-in descriptor retains an
	// explicit generic entry; SARIF metadata does not establish language support.
	known := map[string]bool{}
	for _, descriptor := range descriptors[:len(descriptors)-1] {
		known[descriptor.Tool] = true
	}
	for _, run := range d.CoverageLedger {
		tool := strings.ToLower(run.Tool)
		if run.ReportKind != "sarif" || known[tool] {
			continue
		}
		for _, pop := range pops {
			for _, language := range pop.languages {
				descriptor := descriptors[len(descriptors)-1]
				descriptor.Tool = tool
				entries = append(entries, account(pop, language, descriptor, []mapdoc.CoverageLedgerEntry{run}))
			}
		}
	}
	d.AnalyzerCoverage = entries
	counts := map[string]int{}
	for _, entry := range entries {
		counts[entry.NotCovered]++
	}
	d.AnalyzerBlindSpots = nil
	for reason, count := range counts {
		d.AnalyzerBlindSpots = append(d.AnalyzerBlindSpots, mapdoc.AnalyzerBlindSpot{Reason: reason, Entries: count})
	}
	setCoverageQuestion(d)
}

func setCoverageQuestion(d *mapdoc.Document) {
	coverage := mapdoc.Coverage{Status: mapdoc.CoveragePartial, Reasons: []string{"provider_file_lists_are_not_exhaustive_proof"}}
	if len(d.CoverageLedger) == 0 {
		coverage = mapdoc.Coverage{Status: mapdoc.CoverageUnknown, Reasons: []string{"no_analyzer_report_attached"}}
	}
	if len(d.AnalyzerCoverage) == 0 {
		coverage = mapdoc.Coverage{Status: mapdoc.CoverageUnknown, Reasons: []string{"no_source_population_observed"}}
		for _, question := range d.Coverage {
			if question.Question == "content" && question.Scope == "." && question.Status != mapdoc.CoverageComplete {
				coverage.Reasons = append(coverage.Reasons, "source_population_inventory_incomplete")
				break
			}
		}
	}
	for _, entry := range d.AnalyzerCoverage {
		if len(d.CoverageLedger) > 0 && entry.LanguageBasis == "unattributed" {
			coverage = mapdoc.Coverage{Status: mapdoc.CoverageUnknown, Reasons: []string{"component_language_not_exactly_attributed"}}
			break
		}
	}
	for i := range d.Coverage {
		if d.Coverage[i].Question == "analyzer_coverage" && d.Coverage[i].Scope == "." {
			d.Coverage[i].Coverage = coverage
			return
		}
	}
	d.Coverage = append(d.Coverage, mapdoc.QuestionCoverage{Question: "analyzer_coverage", Scope: ".", Coverage: coverage})
}

func populations(d mapdoc.Document) []population {
	var out []population
	for _, node := range d.Nodes {
		if node.Kind != mapdoc.NodeComponent {
			continue
		}
		root := node.Properties["root"]
		if root == "" {
			root = firstPath(node.Paths)
		}
		language := normalizeLanguage(node.Properties["language"])
		basis := "component_property"
		if node.Properties["language_basis"] == "repository_population" {
			basis = "repository_population"
		}
		if language == "" {
			language = "unknown"
			basis = "unattributed"
		}
		out = append(out, population{id: node.ID, root: clean(root), languages: []string{language}, languageBasis: basis, framework: strings.ToLower(node.Properties["framework"]), node: node})
	}
	if len(out) == 0 {
		languages := []string{}
		for _, node := range d.Nodes {
			if node.Kind == mapdoc.NodeContent && node.Properties["role"] == "language_population" {
				languages = append(languages, normalizeLanguage(node.Properties["language"]))
			}
		}
		languages = compact(languages)
		if len(languages) == 0 {
			return nil
		}
		out = append(out, population{id: "repository", root: ".", languages: languages, languageBasis: "repository_population"})
	}
	return out
}

func account(pop population, language string, descriptor Descriptor, runs []mapdoc.CoverageLedgerEntry) mapdoc.AnalyzerCoverageEntry {
	entry := mapdoc.AnalyzerCoverageEntry{ComponentID: pop.id, Language: language, LanguageBasis: pop.languageBasis, Tool: descriptor.Tool, DescriptorVersion: DescriptorVersion, DescriptorSource: descriptor.Source, NotCovered: "unknown", Reason: "coverage_not_reported_by_provider", CoveredFiles: []string{}}
	matching := matchingRuns(runs, descriptor.Tool, pop.root)
	if len(matching) > 0 {
		entry.Ran = true
		toolError, bindingQualified := "", ""
		for _, run := range matching {
			if run.Binding != "verified" {
				bindingQualified = "provider_snapshot_binding_" + run.Binding
			}
			if run.State == "tool_error" {
				toolError = first(run.Reason, "provider_reported_tool_error")
			}
			for _, filename := range run.CoveredFiles {
				if run.Binding == "verified" && within(pop.root, filename) {
					entry.CoveredFiles = append(entry.CoveredFiles, filename)
				}
			}
		}
		entry.CoveredFiles = compact(entry.CoveredFiles)
		if bindingQualified != "" {
			entry.NotCovered = "unknown"
			entry.Reason = bindingQualified
			return entry
		}
		if toolError != "" {
			entry.NotCovered = "tool_error"
			entry.Reason = toolError
			return entry
		}
	}
	if language == "unknown" {
		entry.Reason = "component_language_not_exactly_attributed"
		return entry
	}
	if !supports(descriptor.Languages, language) {
		entry.NotCovered = "unsupported_language"
		entry.Reason = "descriptor_does_not_list_language"
		return entry
	}
	entry.ExpectedApplicable = true
	if descriptor.GenericFallback {
		entry.ExpectedApplicable = false
	}
	if descriptor.RequiresFramework && pop.framework == "" {
		entry.NotCovered = "unsupported_framework"
		entry.Reason = "supported_framework_not_observed"
		return entry
	}
	for _, prerequisite := range descriptor.Prerequisites {
		if !hasFact(pop.node, prerequisite) {
			entry.NotCovered = "prerequisite_unmet"
			entry.Reason = prerequisite + "_not_observed"
			return entry
		}
	}
	if len(matching) == 0 {
		if hasToolRun(runs, descriptor.Tool) {
			entry.NotCovered, entry.Reason = "unknown", "provider_run_scope_not_attributable_to_component"
		} else {
			entry.NotCovered, entry.Reason = "not_run", "no_provider_report_attached"
		}
		return entry
	}
	if len(entry.CoveredFiles) == 0 {
		entry.Reason = "provider_did_not_report_covered_files"
	} else {
		entry.Reason = "provider_file_list_is_not_exhaustive_proof"
	}
	return entry
}

func matchingRuns(runs []mapdoc.CoverageLedgerEntry, tool, root string) []mapdoc.CoverageLedgerEntry {
	var out []mapdoc.CoverageLedgerEntry
	for _, run := range runs {
		if strings.EqualFold(run.Tool, tool) && scopesOverlap(root, run.Scope) {
			out = append(out, run)
		}
	}
	return out
}
func hasToolRun(runs []mapdoc.CoverageLedgerEntry, tool string) bool {
	for _, run := range runs {
		if strings.EqualFold(run.Tool, tool) && run.Ran {
			return true
		}
	}
	return false
}
func scopesOverlap(a, b string) bool { return within(a, b) || within(b, a) }
func within(root, filename string) bool {
	root, filename = clean(root), clean(filename)
	return root == "." || filename == root || strings.HasPrefix(filename, root+"/")
}
func clean(v string) string {
	v = strings.ReplaceAll(v, "\\", "/")
	v = path.Clean(v)
	if v == "" {
		return "."
	}
	return v
}
func supports(values []string, language string) bool {
	return slices.Contains(values, "generic") || slices.Contains(values, language)
}
func normalizeLanguage(v string) string {
	v = strings.ToLower(strings.TrimSpace(v))
	aliases := map[string]string{"golang": "go", "csharp": "c#", "c-sharp": "c#", "javascript": "javascript", "typescript": "typescript"}
	if x := aliases[v]; x != "" {
		return x
	}
	return v
}
func hasFact(n mapdoc.Node, value string) bool {
	value = strings.ReplaceAll(strings.ToLower(value), "_", " ")
	for _, f := range n.Facts {
		hay := strings.ReplaceAll(strings.ToLower(f.Kind+" "+f.Name+" "+f.Value), "_", " ")
		if strings.Contains(hay, value) {
			return true
		}
	}
	return false
}
func firstPath(v []string) string {
	if len(v) == 0 {
		return "."
	}
	return v[0]
}
func first(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
func compact(v []string) []string { sort.Strings(v); return slices.Compact(v) }
