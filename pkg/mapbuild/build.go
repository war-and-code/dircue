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
	Revision    string
	Deployables *deployables.Report
	Intent      *intentmap.Report
	ExtraNodes  []mapdoc.Node
	ExtraEdges  []mapdoc.Edge
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
		if d.Source.Revision == "" {
			d.Source.Revision = "HEAD"
		}
	}
	if mode == "directory" {
		d.Coverage = append(d.Coverage, question("source_binding", mapdoc.CoverageUnknown, "live_directory_has_no_full_content_digest"))
	} else {
		d.Coverage = append(d.Coverage, question("source_binding", mapdoc.CoverageComplete))
	}

	contentStatus := status(r.Discovery.Status)
	contentReasons := reasons(r.Discovery.Omissions, r.Discovery.OmittedCandidates)
	for _, warning := range r.Warnings {
		switch warning.Code {
		case "file_too_large", "file_read_error", "unsupported_gitattributes", "tree_size_limit":
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

	fragment := componentmap.Build(r.Declarations)
	components, relationships := componentmap.MapFacts(fragment)
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
	componentStatus := status(fragment.Coverage.Status)
	componentReasons := []string{}
	if componentStatus != mapdoc.CoverageComplete {
		componentReasons = []string{"declarations_incomplete_or_local_references_unresolved"}
	}
	d.Coverage = append(d.Coverage, mapdoc.QuestionCoverage{Question: "components", Scope: ".", Coverage: mapdoc.Coverage{Status: componentStatus, Reasons: componentReasons}})

	for _, q := range []string{"deployables", "interfaces", "capabilities", "packages", "routing", "analyzer_coverage", "material_change"} {
		d.Coverage = append(d.Coverage, question(q, mapdoc.CoverageUnknown, "observer_not_yet_bound_to_map"))
	}
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
	for _, group := range r.Discovery.Roles {
		role := group.Name
		if role == "test_candidate" {
			role = "test"
		}
		nodes = append(nodes, population(role, group.Name+":"+group.Basis, group.Counts))
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
		if role != "" {
			nodes = append(nodes, population(role, group.Name+":"+group.Basis, group.Counts))
		}
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

func population(role, discriminator string, counts discovery.Counts) mapdoc.Node {
	n := mapdoc.NewNode(mapdoc.NodeContent, []string{"."}, "population:"+discriminator)
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
	if basis == "parsed_prefix" || basis == "signature_match" {
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
