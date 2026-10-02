package mapbuild

import (
	"path"
	"strings"

	"github.com/war-and-code/dircue/pkg/declarations"
	"github.com/war-and-code/dircue/pkg/mapdoc"
)

func artifactReferenceNodes(report *declarations.Report) []mapdoc.Node {
	if report == nil {
		return nil
	}
	var nodes []mapdoc.Node
	seen := map[string]bool{}
	for _, project := range report.Projects {
		for _, ref := range project.References {
			if ref.Kind != "local-artifact" || ref.TargetStatus != "present" || ref.Target == "" || seen[ref.Target] {
				continue
			}
			role, format := artifactContentKind(ref.Target)
			if role == "" {
				continue
			}
			seen[ref.Target] = true
			n := fileNode(ref.Target, role, format, "filename_hint", ref.TargetBytes)
			nodes = append(nodes, n)
		}
	}
	return nodes
}

func artifactContentKind(filename string) (role, format string) {
	switch strings.ToLower(path.Ext(filename)) {
	case ".jar":
		return "archive", "java_archive"
	case ".dll":
		return "binary", "dotnet_or_native_library"
	default:
		return "", ""
	}
}

func addArtifactReferenceEdges(d *mapdoc.Document, report *declarations.Report) {
	if report == nil {
		return
	}
	componentsByRoot := map[string][]int{}
	contentByPath := map[string]string{}
	seenEdges := map[string]bool{}
	for i, node := range d.Nodes {
		if node.Kind == mapdoc.NodeComponent {
			root := node.Properties["root"]
			componentsByRoot[root] = append(componentsByRoot[root], i)
		}
		if node.Kind == mapdoc.NodeContent && len(node.Paths) == 1 {
			contentByPath[node.Paths[0]] = node.ID
		}
	}
	for _, edge := range d.Edges {
		seenEdges[edge.ID] = true
	}
	for _, project := range report.Projects {
		owners := []int{}
		for _, index := range componentsByRoot[project.Root] {
			if d.Nodes[index].Properties["ecosystem"] == project.Kind {
				owners = append(owners, index)
			}
		}
		if len(owners) > 1 {
			// Several manifests can share a component root. Prefer the component
			// whose retained paths include this exact declaration manifest.
			exact := []int{}
			for _, index := range owners {
				if containsString(d.Nodes[index].Paths, project.ID) {
					exact = append(exact, index)
				}
			}
			if len(exact) == 1 {
				owners = exact
			}
		}
		if len(owners) != 1 {
			continue
		}
		for _, ref := range project.References {
			if ref.Kind != "local-artifact" {
				continue
			}
			owner := &d.Nodes[owners[0]]
			coverage := mapdoc.Coverage{Status: mapdoc.CoverageUnknown, Reasons: []string{"artifact_target_unresolved"}}
			switch ref.TargetStatus {
			case "present":
				coverage = mapdoc.Coverage{Status: mapdoc.CoveragePartial, Reasons: []string{"artifact_path_selected_type_unverified"}}
			case "missing":
				coverage = mapdoc.Coverage{Status: mapdoc.CoveragePartial, Reasons: []string{"artifact_target_missing"}}
			case "unknown":
				coverage = mapdoc.Coverage{Status: mapdoc.CoverageUnknown, Reasons: []string{"artifact_inventory_incomplete"}}
			}
			if ref.Condition != "" || ref.State == "conditional" {
				coverage.Status = mapdoc.CoveragePartial
				coverage.Reasons = append(coverage.Reasons, "conditional_artifact_reference")
			}
			value := ""
			if ref.TargetStatus == "present" && ref.Target != "" {
				value = ref.Target
			}
			owner.Facts = append(owner.Facts, mapdoc.Fact{
				Kind: "artifact_reference", Name: ref.Kind, Value: value, State: ref.TargetStatus,
				Condition: ref.Condition, Coverage: coverage,
				Evidence: artifactEvidence(ref, coverage.Status == mapdoc.CoveragePartial && ref.TargetStatus == "present"),
			})
			if ref.TargetStatus != "present" || ref.Target == "" {
				continue
			}
			targetID := contentByPath[ref.Target]
			if targetID == "" {
				continue
			}
			e := mapdoc.NewEdge(mapdoc.EdgeReferencesArtifact, owner.ID, targetID, ref.Kind+":"+ref.Evidence+":"+ref.Target)
			if seenEdges[e.ID] {
				continue
			}
			seenEdges[e.ID] = true
			e.Coverage = coverage
			e.Evidence = artifactEvidence(ref, true)
			d.Edges = append(d.Edges, e)
		}
	}
}

func artifactEvidence(ref declarations.Reference, selected bool) []mapdoc.Evidence {
	evidence := []mapdoc.Evidence{{Basis: mapdoc.BasisDeclaredConfig, Path: ref.Evidence, SourceKind: mapdoc.SourceConfiguration, Rule: &mapdoc.Producer{ID: "dircue/local-artifact-declaration", Version: "1.1.0"}}}
	if selected && ref.Target != "" {
		evidence = append(evidence, mapdoc.Evidence{Basis: mapdoc.BasisResolvedReference, Path: ref.Target, SourceKind: mapdoc.SourceFile, Rule: &mapdoc.Producer{ID: "dircue/selected-inventory", Version: "1.0.0"}})
	}
	return evidence
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
