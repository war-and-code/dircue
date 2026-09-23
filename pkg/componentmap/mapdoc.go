package componentmap

import (
	"path/filepath"
	"strings"

	"dircue/pkg/mapdoc"
)

var componentRule = &mapdoc.Producer{ID: "dircue/component-declarations", Version: "1.0.0"}

// MapFacts converts a fragment into map document nodes and edges. Qualified
// references become attributable facts on their source component.
func MapFacts(fragment Fragment) ([]mapdoc.Node, []mapdoc.Edge) {
	nodes := make([]mapdoc.Node, 0, len(fragment.Components))
	edges := make([]mapdoc.Edge, 0, len(fragment.Relationships))
	ids := make(map[string]string, len(fragment.Components))
	indices := make(map[string]int, len(fragment.Components))
	for _, c := range fragment.Components {
		paths := []string{c.Manifest}
		if c.Root != "." && c.Root != c.Manifest {
			paths = append(paths, c.Root)
		}
		n := mapdoc.NewNode(mapdoc.NodeComponent, paths, c.Kind)
		n.Name = c.Name
		n.Coverage = mapCoverage(c.Coverage, "declaration input was incomplete")
		n.Evidence = []mapdoc.Evidence{evidence(mapdoc.BasisDeclaredConfig, c.Manifest)}
		n.Properties = map[string]string{"root": c.Root, "ecosystem": c.Ecosystem, "project_kind": c.Kind}
		if language := primaryProjectLanguage(c.Manifest); language != "" {
			n.Properties["language"] = language
			n.Properties["language_basis"] = "project_file_extension"
			n.Properties["language_scope"] = "declared_primary_project_language"
		}
		for _, requirement := range c.Requirements {
			coverage := mapdoc.Coverage{Status: mapdoc.CoverageComplete}
			if requirement.State == "conditional" || requirement.State == "unresolved" {
				coverage = mapdoc.Coverage{Status: mapdoc.CoveragePartial, Reasons: []string{"declaration_" + requirement.State}}
			}
			n.Facts = append(n.Facts, mapdoc.Fact{Kind: "declared_requirement", Name: requirement.Kind, Value: requirement.Value, State: requirement.State, Condition: requirement.Condition, Properties: map[string]string{"ecosystem": c.Ecosystem}, Coverage: coverage, Evidence: []mapdoc.Evidence{evidence(mapdoc.BasisDeclaredConfig, requirement.Evidence)}})
		}
		if c.Version != "" {
			n.Properties["version"] = c.Version
		}
		ids[c.Key] = n.ID
		indices[c.Key] = len(nodes)
		nodes = append(nodes, n)
	}
	for _, q := range fragment.QualifiedReferences {
		i, ok := indices[q.From]
		if !ok {
			continue
		}
		properties := map[string]string{"declaration_kind": q.DeclarationKind, "reason": q.Reason}
		if q.Target != "" {
			properties["target"] = q.Target
		}
		if q.TargetStatus != "" {
			properties["target_status"] = q.TargetStatus
		}
		nodes[i].Facts = append(nodes[i].Facts, mapdoc.Fact{Kind: "qualified_local_reference", Value: q.Value, State: q.State, Condition: q.Condition, Properties: properties, Coverage: mapdoc.Coverage{Status: mapdoc.CoverageUnknown, Reasons: []string{q.Reason}}, Evidence: []mapdoc.Evidence{evidence(mapdoc.BasisDeclaredConfig, q.Evidence)}})
	}
	for _, r := range fragment.Relationships {
		from, fromOK := ids[r.From]
		to, toOK := ids[r.To]
		if !fromOK || !toOK {
			continue
		}
		kind := mapdoc.EdgeType(r.Type)
		discriminator := strings.Join([]string{r.DeclarationKind, r.Evidence, r.State, r.Condition}, "\x00")
		e := mapdoc.NewEdge(kind, from, to, discriminator)
		e.Coverage = mapCoverage(r.Coverage, "relationship requires build evaluation")
		basis := mapdoc.BasisResolvedReference
		if r.DeclarationKind == "root-containment" {
			basis = mapdoc.BasisRuleInferred
		}
		e.Evidence = []mapdoc.Evidence{evidence(basis, r.Evidence)}
		e.Properties = map[string]string{"declaration_kind": r.DeclarationKind, "state": r.State}
		if r.Condition != "" {
			e.Properties["condition"] = r.Condition
		}
		edges = append(edges, e)
	}
	return nodes, edges
}

func primaryProjectLanguage(manifest string) string {
	switch strings.ToLower(filepath.Ext(manifest)) {
	case ".csproj":
		return "C#"
	case ".vbproj":
		return "Visual Basic .NET"
	case ".fsproj":
		return "F#"
	default:
		return ""
	}
}

func evidence(basis mapdoc.EvidenceBasis, filename string) mapdoc.Evidence {
	return mapdoc.Evidence{Basis: basis, Path: filename, SourceKind: mapdoc.SourceConfiguration, Rule: componentRule}
}

func mapCoverage(status, reason string) mapdoc.Coverage {
	if status == "complete" {
		return mapdoc.Coverage{Status: mapdoc.CoverageComplete, Reasons: []string{}}
	}
	if status == "partial" {
		return mapdoc.Coverage{Status: mapdoc.CoveragePartial, Reasons: []string{reason}}
	}
	return mapdoc.Coverage{Status: mapdoc.CoverageUnknown, Reasons: []string{reason}}
}
