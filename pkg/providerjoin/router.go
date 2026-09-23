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
	hasSource := false
	hasPackageMaterial := false
	for _, n := range in.Nodes {
		if n.Kind == mapdoc.NodeComponent {
			components = append(components, n)
		}
		if n.Kind == mapdoc.NodeContent {
			role := strings.ToLower(n.Properties["role"])
			hasSource = hasSource || role == "source"
			hasPackageMaterial = hasPackageMaterial || role == "archive"
		}
	}
	if len(components) == 0 && hasSource {
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
		componentSource := hasSource || hasKnownLanguage(langs)
		if c.ID != "" || hasPackageMaterial {
			out = append(out, Plan{Tool: "syft", ComponentID: c.ID, Applicable: true, Reason: "component_or_package_material_observed", Scope: scope, Argv: []string{"syft", "dir:{scope}", "-o", "syft-json={report}"}, ReportKind: "syft-json"})
		}
		if componentSource {
			out = append(out,
				unverifiedPlan("scc", c.ID, "source_population_observed", scope, "scc-json"),
				unverifiedPlan("bca", c.ID, "supported_source_population_observed", scope, "sarif"),
			)
		}
		if framework != "" {
			out = append(out, unverifiedPlan("noir", c.ID, "observed_framework:"+framework, scope, "noir-json"))
		}
		if containsAny(langs, "java", "kotlin", "maven", "gradle") {
			plan := unverifiedPlan("opentaint", c.ID, "jvm_component", scope, "sarif")
			plan.Prerequisites = append(plan.Prerequisites, Prerequisite{Name: "compiled_bytecode", Observed: false, Reason: "build_output_not_observed"}, Prerequisite{Name: "jdk_version", Observed: hasFact(c, "jdk") || hasFact(c, "java"), Reason: "requires_declared_or_caller_supplied_jdk"})
			out = append(out, plan)
		}
		if containsAny(langs, "go", "java", "kotlin", "python", "javascript", "typescript", "rust", "c#", "c++", "ruby", "php", "swift") {
			plan := unverifiedPlan("bifrost", c.ID, "supported_language_observed", scope, "bifrost-json")
			plan.Prerequisites = append(plan.Prerequisites, Prerequisite{Name: "staged_regular_file_inventory", Observed: false, Reason: "caller_must_stage_confined_regular_files"}, Prerequisite{Name: "semantic_pack_download_disabled", Observed: false, Reason: "caller_policy_required"}, Prerequisite{Name: "private_cache", Observed: false, Reason: "caller_policy_required"})
			out = append(out, plan)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Tool+out[i].ComponentID < out[j].Tool+out[j].ComponentID })
	return out
}

func unverifiedPlan(tool, componentID, reason, scope, reportKind string) Plan {
	return Plan{Tool: tool, ComponentID: componentID, Applicable: true, Reason: reason, Prerequisites: []Prerequisite{{Name: "exact_invocation", Observed: false, Reason: "invocation_not_verified_for_pinned_tool_version"}}, Scope: scope, Argv: []string{}, ReportKind: reportKind}
}

func hasKnownLanguage(s string) bool {
	return containsAny(s, "go", "java", "kotlin", "python", "javascript", "typescript", "rust", "c#", "c++", "ruby", "php", "swift", "maven", "gradle", "npm", "cargo")
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
