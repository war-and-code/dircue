package mapbuild

import (
	"path"
	"slices"
	"strings"

	"dircue/pkg/deployables"
	"dircue/pkg/intentmap"
	"dircue/pkg/mapdoc"
)

func setQuestion(d *mapdoc.Document, name string, coverage mapdoc.Coverage) {
	for i := range d.Coverage {
		if d.Coverage[i].Question == name && d.Coverage[i].Scope == "." {
			d.Coverage[i].Coverage = coverage
			return
		}
	}
	d.Coverage = append(d.Coverage, mapdoc.QuestionCoverage{Question: name, Scope: ".", Coverage: coverage})
}

func observerCoverage(value string, fallback string) mapdoc.Coverage {
	result := mapdoc.Coverage{Status: status(value)}
	if result.Status != mapdoc.CoverageComplete {
		result.Reasons = []string{fallback}
	}
	return result
}

func addDeployables(d *mapdoc.Document, r *deployables.Report) {
	setQuestion(d, "deployables", observerCoverage(r.Status, "deployable_observations_incomplete"))
	componentsByRoot := map[string][]string{}
	for _, n := range d.Nodes {
		if n.Kind == mapdoc.NodeComponent {
			root := n.Properties["root"]
			componentsByRoot[root] = append(componentsByRoot[root], n.ID)
		}
	}
	for _, def := range r.Definitions {
		n := mapdoc.NewNode(mapdoc.NodeDeployable, []string{def.Path}, def.Provider+":"+def.Kind+":"+def.Name)
		n.Name = def.Name
		n.Properties = map[string]string{"kind": def.Kind, "provider": def.Provider, "source_sha256": def.SourceSHA256}
		n.Coverage = mapdoc.Coverage{Status: mapdoc.CoverageComplete}
		if def.Coverage != "complete" {
			n.Coverage = mapdoc.Coverage{Status: mapdoc.CoveragePartial, Reasons: []string{"static_declaration_not_evaluated"}}
		}
		for _, item := range def.Evidence {
			n.Evidence = append(n.Evidence, deployableEvidence(def.Path, item))
		}
		if len(n.Evidence) == 0 {
			continue
		}
		for _, ref := range def.References {
			n.Facts = append(n.Facts, mapdoc.Fact{Kind: "deployable_reference", Name: ref.Kind, Value: ref.Value, State: ref.Qualification, Coverage: referenceCoverage(ref.Qualification), Evidence: []mapdoc.Evidence{deployableEvidence(def.Path, ref.Evidence)}})
			if ref.Kind == "build_context" && ref.Qualification == "local" {
				root := path.Clean(path.Join(path.Dir(def.Path), ref.Value))
				owners := componentsByRoot[root]
				if len(owners) == 1 {
					e := mapdoc.NewEdge(mapdoc.EdgeBuilds, n.ID, owners[0], ref.Kind+":"+root)
					e.Coverage = mapdoc.Coverage{Status: mapdoc.CoverageComplete}
					e.Evidence = []mapdoc.Evidence{deployableEvidence(def.Path, ref.Evidence)}
					d.Edges = append(d.Edges, e)
				}
			}
		}
		d.Nodes = append(d.Nodes, n)
	}
}

func referenceCoverage(qualification string) mapdoc.Coverage {
	if qualification == "local" || qualification == "declared" {
		return mapdoc.Coverage{Status: mapdoc.CoverageComplete}
	}
	return mapdoc.Coverage{Status: mapdoc.CoverageUnknown, Reasons: []string{"reference_" + qualification}}
}

func deployableEvidence(filename string, e deployables.Evidence) mapdoc.Evidence {
	item := mapdoc.Evidence{Basis: mapdoc.BasisDeclaredConfig, Path: filename, SourceKind: mapdoc.SourceConfiguration, Rule: &mapdoc.Producer{ID: "dircue/deployables/" + e.Basis, Version: deployables.ProviderVersion}}
	if e.Line > 0 {
		item.Span = &mapdoc.Span{StartLine: e.Line, EndLine: e.Line}
	}
	return item
}

func addIntent(d *mapdoc.Document, r *intentmap.Report) {
	coverage := observerCoverage(r.Coverage.Status, "interface_or_capability_observations_incomplete")
	setQuestion(d, "interfaces", coverage)
	setQuestion(d, "capabilities", coverage)
	componentsByManifest := map[string]string{}
	for _, n := range d.Nodes {
		if n.Kind == mapdoc.NodeComponent {
			for _, p := range n.Paths {
				if p == n.Properties["root"] {
					continue
				}
				componentsByManifest[p] = n.ID
			}
		}
	}
	contentByPath := map[string]string{}
	for _, n := range d.Nodes {
		if n.Kind == mapdoc.NodeContent && len(n.Paths) == 1 && n.Paths[0] != "." {
			contentByPath[n.Paths[0]] = n.ID
		}
	}
	seen := map[string]bool{}
	for _, o := range r.Observations {
		if o.Kind == intentmap.KindImport {
			continue
		}
		kind := mapdoc.NodeInterface
		if o.Kind == intentmap.KindCapability {
			kind = mapdoc.NodeCapability
		}
		key := string(o.Kind) + ":" + o.Path + ":" + o.Name + ":" + o.ProjectID
		if seen[key] {
			continue
		}
		seen[key] = true
		n := mapdoc.NewNode(kind, []string{o.Path}, string(o.Kind)+":"+o.Name+":"+o.ProjectID)
		n.Name = o.Name
		n.Properties = map[string]string{"observation_kind": string(o.Kind), "state": o.State, "basis": o.Basis}
		for k, v := range o.Properties {
			n.Properties[k] = v
		}
		n.Coverage = mapdoc.Coverage{Status: mapdoc.CoverageComplete}
		if o.State == "partial" || o.State == "unresolved" {
			n.Coverage = mapdoc.Coverage{Status: mapdoc.CoveragePartial, Reasons: []string{"observation_" + o.State}}
		}
		n.Evidence = []mapdoc.Evidence{intentEvidence(o)}
		if o.ProjectID != "" {
			if owner := componentsByManifest[o.ProjectID]; owner != "" {
				kind := mapdoc.EdgeDeclares
				if n.Kind == mapdoc.NodeCapability {
					kind = mapdoc.EdgeUsesCapability
				}
				e := mapdoc.NewEdge(kind, owner, n.ID, string(o.Kind)+":"+o.Path)
				e.Coverage = n.Coverage
				e.Evidence = slices.Clone(n.Evidence)
				d.Edges = append(d.Edges, e)
			}
		} else if n.Kind == mapdoc.NodeInterface && strings.HasSuffix(strings.ToLower(o.Path), ".proto") {
			owner := contentByPath[o.Path]
			if owner == "" {
				content := fileNode(o.Path, "declared_contract", "protobuf", "parsed_prefix", 0)
				content.Coverage = mapdoc.Coverage{Status: mapdoc.CoverageComplete}
				owner = content.ID
				contentByPath[o.Path] = owner
				d.Nodes = append(d.Nodes, content)
			}
			e := mapdoc.NewEdge(mapdoc.EdgeDeclares, owner, n.ID, o.Name)
			e.Coverage = n.Coverage
			e.Evidence = slices.Clone(n.Evidence)
			d.Edges = append(d.Edges, e)
		}
		d.Nodes = append(d.Nodes, n)
	}
}

func intentEvidence(o intentmap.Observation) mapdoc.Evidence {
	basis := mapdoc.BasisDeclaredConfig
	source := mapdoc.SourceConfiguration
	if o.Basis == "code_syntax" || o.Basis == "declared_contract" {
		basis, source = mapdoc.BasisCodeSyntax, mapdoc.SourceCode
	}
	if o.Basis == "imported" {
		basis, source = mapdoc.BasisRuleInferred, mapdoc.SourceCode
	}
	item := mapdoc.Evidence{Basis: basis, Path: o.Path, SourceKind: source, Rule: &mapdoc.Producer{ID: "dircue/intent/" + o.Basis, Version: intentmap.DetectorVersion}}
	if o.StartLine > 0 {
		item.Span = &mapdoc.Span{StartLine: o.StartLine, EndLine: max(o.StartLine, o.EndLine)}
	}
	return item
}
