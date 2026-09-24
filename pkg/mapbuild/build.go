// Package mapbuild joins bounded scanner observations into a portable map.
package mapbuild

import (
	"fmt"
	"path"
	"slices"
	"strconv"
	"strings"

	"dircue/pkg/componentmap"
	"dircue/pkg/coverageledger"
	"dircue/pkg/deployables"
	"dircue/pkg/discovery"
	"dircue/pkg/formats"
	"dircue/pkg/intentmap"
	"dircue/pkg/mapdoc"
	"dircue/pkg/profile"
)

const ruleVersion = "1.0.0"

type Options struct {
	Revision string
	Commit   string
	// SourceDigest and SourceBinding describe a directory source's content
	// identity. A nil SourceBinding keeps the unqualified default.
	SourceDigest  *mapdoc.Digest
	SourceBinding *mapdoc.Coverage
	Deployables   *deployables.Report
	Intent        *intentmap.Report
	ExtraNodes    []mapdoc.Node
	ExtraEdges    []mapdoc.Edge
}

func Build(r *profile.Report, opts Options) (mapdoc.Document, error) {
	if r == nil || r.Discovery == nil {
		return mapdoc.Document{}, fmt.Errorf("map requires a discovery inventory")
	}
	d := mapdoc.New()
	mode := r.Discovery.Source.Mode
	d.Source.Mode = mode
	if mode == "git" {
		d.Source.Tree = r.Discovery.Source.Tree
		d.Source.Revision = opts.Revision
		d.Source.Commit = opts.Commit
		if d.Source.Revision == "" {
			d.Source.Revision = "HEAD"
		}
	}
	if mode == "directory" {
		d.Source.Digest = opts.SourceDigest
		switch {
		case opts.SourceBinding != nil:
			d.Coverage = append(d.Coverage, question("source_binding", opts.SourceBinding.Status, opts.SourceBinding.Reasons...))
		default:
			d.Coverage = append(d.Coverage, question("source_binding", mapdoc.CoverageUnknown, "live_directory_has_no_full_content_digest"))
		}
	} else {
		d.Coverage = append(d.Coverage, question("source_binding", mapdoc.CoverageComplete))
	}

	contentStatus := status(r.Discovery.Status)
	contentReasons := reasons(r.Discovery.Omissions, r.Discovery.OmittedCandidates)
	for _, warning := range r.Warnings {
		switch warning.Code {
		case "file_too_large", "file_read_error", "unsupported_gitattributes", "tree_size_limit", "permission_denied", "walk_error":
			contentStatus = mapdoc.CoveragePartial
			contentReasons = append(contentReasons, warning.Code)
		}
	}
	if r.Formats != nil && r.Formats.Status != "complete" {
		contentStatus = mapdoc.CoveragePartial
		contentReasons = append(contentReasons, "format_observations_incomplete")
	}
	slices.Sort(contentReasons)
	contentReasons = slices.Compact(contentReasons)
	if contentStatus != mapdoc.CoverageComplete && len(contentReasons) == 0 {
		contentReasons = []string{"content_inventory_incomplete"}
	}
	d.Coverage = append(d.Coverage, mapdoc.QuestionCoverage{Question: "content", Scope: ".", Coverage: mapdoc.Coverage{Status: contentStatus, Reasons: contentReasons}})
	d.Nodes = append(d.Nodes, contentNodes(r)...)
	d.Nodes = append(d.Nodes, languageNodes(r.Languages, mapdoc.Coverage{Status: contentStatus, Reasons: contentReasons})...)
	d.Nodes = append(d.Nodes, summarizedTreeNodes(r)...)

	fragment := componentmap.Build(r.Declarations)
	components, relationships := componentmap.MapFacts(fragment)
	// Build a lookup from app-root → name extracted from config/application.rb
	// so that unnamed Ruby components at the same root can use the app name.
	railsAppNames := map[string]string{}
	if r.Declarations != nil {
		for _, p := range r.Declarations.Projects {
			if p.Kind == "ruby-rails-app" && p.Name != "" {
				// p.ID is "…/config/application.rb"; app root is two levels up.
				appRoot := path.Dir(path.Dir(p.ID))
				if appRoot == "" {
					appRoot = "."
				}
				railsAppNames[appRoot] = p.Name
			}
		}
	}
	for i := range components {
		if strings.TrimSpace(components[i].Name) == "" {
			root := components[i].Properties["root"]
			if root == "" {
				root = "."
			}
			if root == "." {
				// At the repository root: use the Rails app module name if
				// available for Ruby components, otherwise use the stable
				// label "(root)". Never expose the host directory name.
				if components[i].Properties["ecosystem"] == "ruby" {
					if railsName, ok := railsAppNames["."]; ok {
						components[i].Name = railsName
					}
				}
				if components[i].Name == "" {
					components[i].Name = "(root)"
				}
			} else {
				// Nested root: check for a Rails app name at this root, then
				// fall back to the last path segment of the root directory.
				if components[i].Properties["ecosystem"] == "ruby" {
					if railsName, ok := railsAppNames[path.Clean(root)]; ok {
						components[i].Name = railsName
					}
				}
				if components[i].Name == "" {
					components[i].Name = path.Base(root)
				}
			}
		}
		role := mapPathRole(components[i].Paths...)
		roleBasis := "path_name"
		if role == "" {
			// Signal 2: placeholder declared names indicate generated or template
			// entries that are not primary service or library components.
			if r := placeholderNameRole(components[i].Name); r != "" {
				role = r
				roleBasis = "declared_name"
			} else {
				role = "primary"
			}
		}
		components[i].Properties["role"] = role
		components[i].Properties["role_basis"] = roleBasis
		// For Go modules: prefer the last path segment as display name to avoid
		// exposing full module paths (e.g. "github.com/grafana/loki" → "loki").
		// The full module path is preserved in the "go_module" property.
		if components[i].Properties["ecosystem"] == "go" && components[i].Name != "" {
			modulePath := components[i].Name
			if lastSeg := path.Base(modulePath); lastSeg != "" && lastSeg != "." && strings.ContainsRune(modulePath, '/') {
				components[i].Properties["go_module"] = modulePath
				components[i].Name = lastSeg
			}
		}
	}
	// Signal 3: repeated sibling templates — when 3 or more primary components
	// share an identical declared name under the same grandparent directory they
	// are template instances (test cases, golden fixtures) rather than primary
	// products. Promote them to fixture.
	type gpName struct{ gp, name string }
	siblingCounts := map[gpName]int{}
	for i := range components {
		if components[i].Properties["role"] == "primary" {
			root := components[i].Properties["root"]
			gp := path.Dir(path.Dir(path.Clean(root)))
			key := gpName{gp, strings.ToLower(components[i].Name)}
			siblingCounts[key]++
		}
	}
	for i := range components {
		if components[i].Properties["role"] == "primary" {
			root := components[i].Properties["root"]
			gp := path.Dir(path.Dir(path.Clean(root)))
			key := gpName{gp, strings.ToLower(components[i].Name)}
			if siblingCounts[key] >= 3 {
				components[i].Properties["role"] = "fixture"
				components[i].Properties["role_basis"] = "repeated_sibling_name"
			}
		}
	}
	if len(r.Languages) == 1 {
		for i := range components {
			if components[i].Properties["root"] == "." {
				components[i].Properties["language"] = r.Languages[0].Name
				components[i].Properties["language_basis"] = "repository_population"
			}
		}
	}
	d.Nodes = append(d.Nodes, components...)
	d.Edges = append(d.Edges, relationships...)
	// The declaration parser covers a bounded set of ecosystems and forms. A
	// successful pass is not proof that every project in the tree was found.
	componentStatus := mapdoc.CoveragePartial
	componentReasons := []string{"bounded_declaration_catalog"}
	if status(fragment.Coverage.Status) != mapdoc.CoverageComplete {
		componentReasons = append(componentReasons, "declarations_incomplete_or_local_references_unresolved")
	}
	d.Coverage = append(d.Coverage, mapdoc.QuestionCoverage{Question: "components", Scope: ".", Coverage: mapdoc.Coverage{Status: componentStatus, Reasons: componentReasons}})

	for _, q := range []string{"deployables", "interfaces", "capabilities"} {
		d.Coverage = append(d.Coverage, question(q, mapdoc.CoverageUnknown, "native_observation_unavailable"))
	}
	d.Coverage = append(d.Coverage,
		question("packages", mapdoc.CoverageUnknown, "no_package_report_attached"),
		question("routing", mapdoc.CoverageUnknown, "no_route_plan_evaluated"),
		question("analyzer_coverage", mapdoc.CoverageUnknown, "no_analyzer_report_attached"),
	)
	if opts.Deployables != nil {
		addDeployables(&d, opts.Deployables)
	}
	if opts.Intent != nil {
		addIntent(&d, opts.Intent)
	}
	d.Nodes = append(d.Nodes, opts.ExtraNodes...)
	d.Edges = append(d.Edges, opts.ExtraEdges...)
	coverageledger.Reconcile(&d)
	d.Status = mapdoc.CoverageComplete
	for _, q := range d.Coverage {
		if q.Status != mapdoc.CoverageComplete {
			d.Status = mapdoc.CoveragePartial
			break
		}
	}
	return mapdoc.Normalize(d)
}

func question(name string, status mapdoc.CoverageStatus, reasons ...string) mapdoc.QuestionCoverage {
	return mapdoc.QuestionCoverage{Question: name, Scope: ".", Coverage: mapdoc.Coverage{Status: status, Reasons: reasons}}
}

func status(v string) mapdoc.CoverageStatus {
	switch v {
	case "complete":
		return mapdoc.CoverageComplete
	case "partial":
		return mapdoc.CoveragePartial
	default:
		return mapdoc.CoverageUnknown
	}
}

func reasons(maps ...map[string]int64) []string {
	var out []string
	for _, m := range maps {
		for reason, count := range m {
			if count > 0 {
				out = append(out, reason)
			}
		}
	}
	slices.Sort(out)
	return slices.Compact(out)
}

func contentNodes(r *profile.Report) []mapdoc.Node {
	var nodes []mapdoc.Node
	roleCounts := map[string]discovery.Counts{}
	for _, group := range r.Discovery.Roles {
		role := group.Name
		if role == "test_candidate" {
			role = "test"
		}
		counts := roleCounts[role]
		counts.Files += group.Files
		counts.Bytes += group.Bytes
		roleCounts[role] = counts
	}
	for _, group := range r.Discovery.Categories {
		var role string
		switch group.Name {
		case "source_candidate":
			role = "source"
		case "data_candidate":
			role = "data"
		case "documentation_candidate":
			role = "documentation"
		}
		if role != "" && role != "documentation" {
			counts := roleCounts[role]
			counts.Files += group.Files
			counts.Bytes += group.Bytes
			roleCounts[role] = counts
		}
	}
	roles := make([]string, 0, len(roleCounts))
	for role := range roleCounts {
		roles = append(roles, role)
	}
	slices.Sort(roles)
	for _, role := range roles {
		nodes = append(nodes, population(role, roleCounts[role]))
	}
	for _, c := range r.Discovery.Candidates {
		role := ""
		if c.Kind == "artifact" {
			role = "binary"
			if strings.Contains(c.Format, "archive") || strings.Contains(c.Format, "package") || c.Format == "python_wheel" {
				role = "archive"
			}
		}
		if role == "" {
			continue
		}
		nodes = append(nodes, fileNode(c.Path, role, c.Format, c.Basis, c.Bytes))
	}
	if r.Formats != nil {
		for _, o := range r.Formats.Observations {
			role, format, basis := formatRole(o)
			if role != "" {
				nodes = append(nodes, fileNode(o.Path, role, format, basis, o.Bytes))
			}
		}
	}
	return uniqueNodes(nodes)
}

func languageNodes(languages []profile.Language, coverage mapdoc.Coverage) []mapdoc.Node {
	nodes := make([]mapdoc.Node, 0, len(languages))
	for _, language := range languages {
		n := mapdoc.NewNode(mapdoc.NodeContent, []string{"."}, "language:"+language.Name)
		n.Name = language.Name
		n.Properties = map[string]string{
			"role": "language_population", "language": language.Name,
			"bytes":      strconv.FormatInt(language.Bytes, 10),
			"files":      strconv.FormatInt(language.FileCount, 10),
			"percentage": strconv.FormatFloat(language.Percentage, 'f', 4, 64),
		}
		n.Coverage = coverage
		n.Evidence = []mapdoc.Evidence{{Basis: mapdoc.BasisRuleInferred, Path: ".", SourceKind: mapdoc.SourceDirectory,
			Rule: &mapdoc.Producer{ID: "dircue/linguist-language-population", Version: ruleVersion}}}
		nodes = append(nodes, n)
	}
	return nodes
}

func population(role string, counts discovery.Counts) mapdoc.Node {
	n := mapdoc.NewNode(mapdoc.NodeContent, []string{"."}, "population:"+role)
	n.Name = role + " population"
	n.Properties = map[string]string{"role": role, "files": strconv.FormatInt(counts.Files, 10), "bytes": strconv.FormatInt(counts.Bytes, 10), "scope": "inventory_population"}
	n.Coverage = mapdoc.Coverage{Status: mapdoc.CoverageComplete}
	n.Evidence = []mapdoc.Evidence{{Basis: mapdoc.BasisRuleInferred, Path: ".", SourceKind: mapdoc.SourceDirectory, Rule: &mapdoc.Producer{ID: "dircue/discovery-population", Version: discovery.RuleVersion}}}
	return n
}

func fileNode(filename, role, format, basis string, size int64) mapdoc.Node {
	n := mapdoc.NewNode(mapdoc.NodeContent, []string{filename}, "role:"+role)
	n.Name = path.Base(filename)
	n.Properties = map[string]string{"role": role, "format": format, "bytes": strconv.FormatInt(size, 10)}
	n.Coverage = mapdoc.Coverage{Status: mapdoc.CoverageComplete}
	evidenceBasis := mapdoc.BasisFilenameHint
	if basis == "signature_match" || basis == "complete_validation" || basis == "parsed_prefix" {
		evidenceBasis = mapdoc.BasisRuleInferred
	}
	n.Evidence = []mapdoc.Evidence{{Basis: evidenceBasis, Path: filename, SourceKind: mapdoc.SourceFile, Rule: &mapdoc.Producer{ID: "dircue/content-role", Version: ruleVersion}}}
	if evidenceBasis == mapdoc.BasisFilenameHint {
		n.Coverage = mapdoc.Coverage{Status: mapdoc.CoveragePartial, Reasons: []string{"filename_hint_unverified"}}
	} else if basis == "parsed_prefix" || basis == "signature_match" {
		n.Coverage = mapdoc.Coverage{Status: mapdoc.CoveragePartial, Reasons: []string{"prefix_or_header_only"}}
	}
	return n
}

func formatRole(o formats.Observation) (role, format, basis string) {
	filename := strings.ToLower(o.Path)
	for _, e := range o.Evidence {
		if e.Basis == "signature_match" {
			switch e.Format {
			case "elf", "pe", "dos-executable", "mach_o", "mach_o_fat", "java_class", "wasm":
				return "binary", e.Format, e.Basis
			case "zip", "gzip", "7z", "tar":
				return "archive", e.Format, e.Basis
			case "sqlite":
				return "data", e.Format, e.Basis
			}
		}
	}
	switch {
	case strings.HasSuffix(filename, ".crt"), strings.HasSuffix(filename, ".cer"), strings.HasSuffix(filename, ".pem"):
		return "certificate", "certificate_candidate", "extension_hint"
	case strings.HasSuffix(filename, ".key"):
		return "key_material", "key_candidate", "extension_hint"
	case strings.HasSuffix(filename, ".csv"), strings.HasSuffix(filename, ".tsv"), strings.HasSuffix(filename, ".sqlite"), strings.HasSuffix(filename, ".sqlite3"), strings.HasSuffix(filename, ".log"):
		return "data", "data_candidate", "extension_hint"
	}
	return "", "", ""
}

func uniqueNodes(nodes []mapdoc.Node) []mapdoc.Node {
	seen := make(map[string]bool, len(nodes))
	out := make([]mapdoc.Node, 0, len(nodes))
	for _, n := range nodes {
		if !seen[n.ID] {
			seen[n.ID] = true
			out = append(out, n)
		}
	}
	return out
}

// placeholderNameRole returns the role for a component whose declared name is a
// generic placeholder that indicates a template, test case, or generated entry.
// Returns "" if the name is not a recognised placeholder.
func placeholderNameRole(name string) string {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "test", "tests":
		return "test"
	case "example", "examples", "sample", "samples":
		return "example"
	case "fixture", "fixtures", "dummy", "foo", "my-project", "my_project", "demo":
		return "fixture"
	}
	return ""
}

// mapPathRole is a conservative path-name hint. It marks auxiliary material
// without discarding any node from the machine-readable map.
// Returns one of "vendored", "fixture", "test", "example", "docs", "tooling", or
// "" (no role inferred — the caller may assign "primary").
func mapPathRole(paths ...string) string {
	role, priority := "", 0
	choose := func(candidate string, rank int) {
		if rank > priority {
			role, priority = candidate, rank
		}
	}
	for _, filename := range paths {
		lower := strings.ToLower(strings.ReplaceAll(filename, "\\", "/"))
		segments := strings.Split(lower, "/")
		for _, segment := range segments {
			switch segment {
			// vendored external code
			case "vendor", "node_modules", "third_party", ".bingo", "bingo":
				choose("vendored", 6)
			// test fixtures and evaluation truth data
			case "fixtures", "testdata", "__fixtures__", "truth", "cases", "snapshots", "corpus", "golden":
				choose("fixture", 5)
			// examples and demos
			case "examples", "samples", "demo", "demos":
				choose("example", 4)
			// test code
			case "test", "tests", "__tests__", "spec", "specs":
				choose("test", 3)
			// docs/release tooling
			case "docs", "doc", "documentation", "releasing", "translations", "i18n", "locale", "locales":
				choose("docs", 2)
			// other tooling
			case "tools", "tooling", "scripts", "hack", "ci", "infra", "benchmarks", "bench":
				choose("tooling", 1)
			default:
				// Directory segments that end with _test, _tests, test, tests
				// identify test modules or crates (e.g. ruff_mdtest, ty_test).
				if strings.HasSuffix(segment, "_test") || strings.HasSuffix(segment, "_tests") ||
					strings.HasSuffix(segment, "-test") || strings.HasSuffix(segment, "-tests") {
					choose("test", 3)
				}
			}
		}
		base := path.Base(lower)
		// .NET test projects by conventional suffixes
		if strings.Contains(base, ".tests.") || strings.HasSuffix(base, "test.csproj") || strings.HasSuffix(base, "tests.csproj") ||
			strings.HasSuffix(base, "test.vbproj") || strings.HasSuffix(base, "tests.vbproj") ||
			strings.HasSuffix(base, "test.fsproj") || strings.HasSuffix(base, "tests.fsproj") ||
			strings.HasSuffix(base, ".unittests.csproj") || strings.HasSuffix(base, ".functionaltests.csproj") ||
			strings.HasSuffix(base, ".integrationtests.csproj") {
			choose("test", 3)
		}
		// project names ending in Tests/UnitTests/FunctionalTests (without extension)
		nameNoExt := strings.TrimSuffix(base, path.Ext(base))
		if strings.HasSuffix(nameNoExt, "tests") || strings.HasSuffix(nameNoExt, "test") ||
			strings.HasSuffix(nameNoExt, "unittests") || strings.HasSuffix(nameNoExt, "functionaltests") ||
			strings.HasSuffix(nameNoExt, "integrationtests") {
			choose("test", 3)
		}
		// .bingo directory as tooling
		if strings.Contains(lower, "/.bingo/") {
			choose("tooling", 1)
		}
	}
	return role
}

// summarizedTreeNodes creates content nodes for summarized environment and
// build-output trees. These nodes carry the counted metadata rather than
// language-classified content, and are marked partial coverage because the
// tree was not walked in detail.
func summarizedTreeNodes(r *profile.Report) []mapdoc.Node {
	if len(r.SummarizedTrees) == 0 {
		return nil
	}
	nodes := make([]mapdoc.Node, 0, len(r.SummarizedTrees))
	for _, st := range r.SummarizedTrees {
		n := mapdoc.NewNode(mapdoc.NodeContent, []string{st.Path}, "env_tree:"+st.Ecosystem)
		n.Name = path.Base(st.Path)
		coverageStatus := mapdoc.CoverageComplete
		coverageReasons := []string{}
		if !st.Bounded {
			coverageStatus = mapdoc.CoveragePartial
			coverageReasons = []string{"entry_cap_reached"}
		}
		n.Coverage = mapdoc.Coverage{Status: coverageStatus, Reasons: coverageReasons}
		n.Properties = map[string]string{
			"role":      st.Kind,
			"ecosystem": st.Ecosystem,
			"entries":   strconv.FormatInt(st.Entries, 10),
			"bytes":     strconv.FormatInt(st.Bytes, 10),
		}
		evidenceBasis := mapdoc.BasisFilenameHint
		if st.Basis == "rule_inferred" {
			evidenceBasis = mapdoc.BasisRuleInferred
		}
		n.Evidence = []mapdoc.Evidence{{
			Basis:      evidenceBasis,
			Path:       st.Marker,
			SourceKind: mapdoc.SourceDirectory,
			Rule:       &mapdoc.Producer{ID: "dircue/env-tree-recognizer", Version: ruleVersion},
		}}
		nodes = append(nodes, n)
	}
	return nodes
}
