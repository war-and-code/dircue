package providerjoin

import (
	"path"
	"sort"
	"strings"

	"dircue/pkg/mapdoc"
)

// Descriptors are versioned dircue data. They describe routing and expected
// applicability, not claims made by repository content.
func Descriptors() []Descriptor {
	return []Descriptor{
		{Tool: "bca", Version: "1", Languages: []string{"go", "java", "kotlin", "python", "javascript", "typescript", "rust", "c", "c++", "c#"}, ReportKind: "sarif"},
		{Tool: "bifrost", Version: "1", Languages: []string{"go", "java", "kotlin", "python", "javascript", "typescript", "rust", "c", "c++", "c#", "ruby", "php", "swift"}, ReportKind: "bifrost-code-query-json"},
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
	repositoryLanguages := []string{}
	for _, n := range in.Nodes {
		if n.Kind == mapdoc.NodeComponent {
			if routeAuxiliaryComponent(n) {
				continue
			}
			components = append(components, n)
		}
		if n.Kind == mapdoc.NodeContent {
			role := strings.ToLower(n.Properties["role"])
			hasSource = hasSource || role == "source"
			hasPackageMaterial = hasPackageMaterial || role == "archive"
			if role == "language_population" {
				repositoryLanguages = append(repositoryLanguages, normalizeRouterLanguage(n.Properties["language"]))
			}
		}
	}
	if len(components) == 0 && hasSource {
		components = []mapdoc.Node{{ID: "", Paths: []string{"."}, Properties: map[string]string{"language": strings.Join(compact(repositoryLanguages), ",")}}}
	}
	sort.Slice(components, func(i, j int) bool { return components[i].ID < components[j].ID })
	out := make([]Plan, 0)
	for _, c := range components {
		scope := "."
		if root, ok := cleanReportPath(c.Properties["root"]); ok {
			scope = root
		} else if len(c.Paths) > 0 {
			scope = c.Paths[0]
		}
		if strings.HasPrefix(scope, "-") {
			scope = "./" + scope
		}
		languages := routerLanguages(c.Properties["language"])
		ecosystem := strings.ToLower(strings.TrimSpace(c.Properties["ecosystem"]))
		framework := strings.ToLower(c.Properties["framework"])
		if c.ID != "" || hasPackageMaterial {
			out = append(out, Plan{Tool: "syft", ComponentID: c.ID, Applicable: true, Reason: "component_or_package_material_observed", Scope: scope, Argv: []string{"syft", "dir:{scope}", "-o", "syft-json={report}"}, ReportKind: "syft-json"})
		}
		if len(languages) > 0 || c.ID == "" && hasSource {
			out = append(out, unverifiedPlan("scc", c.ID, "source_population_observed", scope, "scc-json"))
		}
		if routerSupports("bca", languages) {
			out = append(out, unverifiedPlan("bca", c.ID, "supported_source_population_observed", scope, "sarif"))
		}
		if framework != "" {
			out = append(out, unverifiedPlan("noir", c.ID, "observed_framework:"+framework, scope, "noir-json"))
		}
		if routerSupports("opentaint", languages) || ecosystem == "maven" || ecosystem == "gradle" {
			plan := unverifiedPlan("opentaint", c.ID, "jvm_component", scope, "sarif")
			plan.Prerequisites = append(plan.Prerequisites, Prerequisite{Name: "compiled_bytecode", Observed: false, Reason: "build_output_not_observed"}, Prerequisite{Name: "jdk_version", Observed: hasFact(c, "jdk") || hasFact(c, "java"), Reason: "requires_declared_or_caller_supplied_jdk"})
			out = append(out, plan)
		}
		if routerSupports("bifrost", languages) {
			plan := unverifiedPlan("bifrost", c.ID, "supported_language_observed", scope, "bifrost-code-query-json")
			plan.Prerequisites = append(plan.Prerequisites, Prerequisite{Name: "staged_regular_file_inventory", Observed: false, Reason: "caller_must_stage_confined_regular_files"}, Prerequisite{Name: "semantic_pack_download_disabled", Observed: false, Reason: "caller_policy_required"}, Prerequisite{Name: "private_cache", Observed: false, Reason: "caller_policy_required"})
			out = append(out, plan)
		}
	}
	return dedupePlans(out)
}

func routeAuxiliaryComponent(n mapdoc.Node) bool {
	declared := strings.ToLower(strings.TrimSpace(n.Properties["role"]))
	if declared == "" || n.Properties["role_basis"] != "path_name" {
		return false
	}
	for _, value := range n.Paths {
		clean := strings.ToLower(strings.ReplaceAll(value, "\\", "/"))
		for _, segment := range strings.Split(clean, "/") {
			var derived string
			switch segment {
			case "vendor", "node_modules", "third_party":
				derived = "vendored"
			case "fixtures", "testdata", "__fixtures__":
				derived = "fixture"
			case "examples", "samples":
				derived = "example"
			case "test", "tests", "__tests__":
				derived = "test"
			}
			if derived != "" {
				return derived == declared
			}
		}
		base := path.Base(clean)
		if strings.Contains(base, ".tests.") || strings.HasSuffix(base, "test.csproj") || strings.HasSuffix(base, "tests.csproj") || strings.HasSuffix(base, "test.vbproj") || strings.HasSuffix(base, "tests.vbproj") {
			return declared == "test"
		}
	}
	return false
}

func unverifiedPlan(tool, componentID, reason, scope, reportKind string) Plan {
	return Plan{Tool: tool, ComponentID: componentID, Applicable: true, Reason: reason, Prerequisites: []Prerequisite{{Name: "exact_invocation", Observed: false, Reason: "invocation_not_verified_for_pinned_tool_version"}}, Scope: scope, Argv: []string{}, ReportKind: reportKind}
}

func routerLanguages(value string) []string {
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if language := normalizeRouterLanguage(part); language != "" {
			out = append(out, language)
		}
	}
	return compact(out)
}

func normalizeRouterLanguage(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	switch value {
	case "golang":
		return "go"
	case "csharp", "c-sharp":
		return "c#"
	default:
		return value
	}
}

func routerSupports(tool string, languages []string) bool {
	for _, descriptor := range Descriptors() {
		if descriptor.Tool != tool {
			continue
		}
		for _, language := range languages {
			for _, supported := range descriptor.Languages {
				if language == supported || supported == "generic" {
					return true
				}
			}
		}
		return false
	}
	return false
}

func dedupePlans(plans []Plan) []Plan {
	byKey := map[string]int{}
	out := make([]Plan, 0, len(plans))
	for _, plan := range plans {
		key := plan.Tool + "\x00" + plan.Scope + "\x00" + plan.ReportKind
		if index, ok := byKey[key]; ok {
			if out[index].ComponentID != plan.ComponentID {
				out[index].ComponentID = ""
			}
			out[index].Prerequisites = mergePrerequisites(out[index].Prerequisites, plan.Prerequisites)
			continue
		}
		byKey[key] = len(out)
		out = append(out, plan)
	}
	sort.Slice(out, func(i, j int) bool {
		left := out[i].Tool + "\x00" + out[i].Scope + "\x00" + out[i].ReportKind + "\x00" + out[i].ComponentID
		right := out[j].Tool + "\x00" + out[j].Scope + "\x00" + out[j].ReportKind + "\x00" + out[j].ComponentID
		return left < right
	})
	return out
}

func mergePrerequisites(left, right []Prerequisite) []Prerequisite {
	byName := make(map[string]Prerequisite, len(left)+len(right))
	for _, prerequisite := range append(append([]Prerequisite{}, left...), right...) {
		if existing, ok := byName[prerequisite.Name]; ok {
			existing.Observed = existing.Observed && prerequisite.Observed
			if existing.Reason == "" || !prerequisite.Observed && prerequisite.Reason != "" {
				existing.Reason = prerequisite.Reason
			}
			byName[prerequisite.Name] = existing
			continue
		}
		byName[prerequisite.Name] = prerequisite
	}
	out := make([]Prerequisite, 0, len(byName))
	for _, prerequisite := range byName {
		out = append(out, prerequisite)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
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
