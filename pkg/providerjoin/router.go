package providerjoin

import (
	"sort"
	"strings"

	"dircue/pkg/mapdoc"
)

// Descriptors are versioned dircue data. They describe routing and expected
// applicability, not claims made by repository content.
func Descriptors() []Descriptor {
	return []Descriptor{
		{Tool: "bca", Version: "1", Languages: []string{"go", "java", "kotlin", "python", "javascript", "typescript", "rust", "c", "c++", "c#"}, ReportKind: "sarif"},
		{Tool: "bifrost", Version: "1", Languages: []string{"go", "java", "kotlin", "python", "javascript", "typescript", "rust", "c", "c++", "c#", "ruby", "php", "swift"}, ReportKind: "bifrost-json"},
		{Tool: "codeql", Version: "1", Languages: []string{"c", "c++", "c#", "go", "java", "kotlin", "javascript", "typescript", "python", "ruby", "swift"}, ReportKind: "sarif"},
		{Tool: "noir", Version: "1", RequiresFramework: true, ReportKind: "noir-json"},
		{Tool: "opengrep", Version: "1", Languages: []string{"generic"}, ReportKind: "sarif"},
		{Tool: "opentaint", Version: "1", Languages: []string{"java", "kotlin"}, ReportKind: "sarif"},
		{Tool: "sarif", Version: "2.1.0", Languages: []string{"unknown"}, ReportKind: "sarif"},
		{Tool: "scc", Version: "1", Languages: []string{"generic"}, ReportKind: "scc-json"},
		{Tool: "semgrep", Version: "1", Languages: []string{"generic"}, ReportKind: "sarif"},
		{Tool: "syft", Version: "1", Languages: []string{"generic"}, ReportKind: "syft-json"},
	}
}

// Route returns inert templates only. It neither consults PATH nor executes a
// tool. Repository facts can select applicability but never executable names,
// arguments, or budgets.
func Route(in Input) []Plan {
	components := []mapdoc.Node{}
	for _, n := range in.Nodes {
		if n.Kind == mapdoc.NodeComponent {
			components = append(components, n)
		}
	}
	if len(components) == 0 {
		components = []mapdoc.Node{{ID: "", Paths: []string{"."}, Properties: map[string]string{}}}
	}
	var out []Plan
	for _, c := range components {
		scope := "."
		if len(c.Paths) > 0 {
			scope = c.Paths[0]
		}
		langs := strings.ToLower(c.Properties["language"] + " " + c.Properties["ecosystem"] + " " + c.Discriminator)
		framework := strings.ToLower(c.Properties["framework"])
		out = append(out, Plan{Tool: "syft", ComponentID: c.ID, Applicable: true, Reason: "directory_source", Scope: scope, Argv: []string{"syft", "dir:{scope}", "-o", "syft-json={report}"}, ReportKind: "syft-json"})
		out = append(out,
			Plan{Tool: "scc", ComponentID: c.ID, Applicable: true, Reason: "source_population", Scope: scope, Argv: []string{"scc", "--format", "json", "{scope}"}, ReportKind: "scc-json"},
			Plan{Tool: "bca", ComponentID: c.ID, Applicable: true, Reason: "source_population", Scope: scope, Argv: []string{"bca", "{scope}", "--sarif", "{report}"}, ReportKind: "sarif"},
		)
		if framework != "" {
			out = append(out, Plan{Tool: "noir", ComponentID: c.ID, Applicable: true, Reason: "observed_framework:" + framework, Scope: scope, Argv: []string{"noir", "-b", "{scope}", "-f", "json", "-o", "{report}"}, ReportKind: "noir-json"})
		}
		if containsAny(langs, "java", "kotlin", "maven", "gradle") {
			out = append(out, Plan{Tool: "opentaint", ComponentID: c.ID, Applicable: true, Reason: "jvm_component", Prerequisites: []Prerequisite{{Name: "compiled_bytecode", Observed: false, Reason: "build_output_not_observed"}, {Name: "jdk_version", Observed: hasFact(c, "jdk") || hasFact(c, "java"), Reason: "requires_declared_or_caller_supplied_jdk"}}, Scope: scope, Argv: []string{"opentaint", "--input", "{compiled_classes}", "--sarif", "{report}"}, ReportKind: "sarif"})
		}
		if containsAny(langs, "go", "java", "kotlin", "python", "javascript", "typescript", "rust", "c#", "c++", "ruby", "php", "swift") {
			out = append(out, Plan{Tool: "bifrost", ComponentID: c.ID, Applicable: true, Reason: "supported_language", Prerequisites: []Prerequisite{{Name: "staged_regular_file_inventory", Observed: false, Reason: "caller_must_stage_confined_regular_files"}, {Name: "semantic_pack_download_disabled", Observed: false, Reason: "caller_policy_required"}, {Name: "private_cache", Observed: false, Reason: "caller_policy_required"}}, Scope: scope, Argv: []string{"bifrost", "--sources", "{staged_sources}", "--output", "{report}"}, ReportKind: "bifrost-json"})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Tool+out[i].ComponentID < out[j].Tool+out[j].ComponentID })
	return out
}

func containsAny(s string, values ...string) bool {
	for _, v := range values {
		if strings.Contains(s, v) {
			return true
		}
	}
	return false
}
func hasFact(n mapdoc.Node, needle string) bool {
	needle = strings.ToLower(needle)
	for _, f := range n.Facts {
		if strings.Contains(strings.ToLower(f.Kind+" "+f.Name), needle) {
			return true
		}
	}
	return false
}
