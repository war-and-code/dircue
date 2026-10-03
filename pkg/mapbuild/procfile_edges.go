package mapbuild

import (
	"path"
	"strings"

	"github.com/war-and-code/dircue/pkg/deployables"
	"github.com/war-and-code/dircue/pkg/mapdoc"
)

// addProcfileEdges links a process to the unique selected component that owns
// its statically matched entry file. It runs after addDeployables has emitted
// the process node and component nodes.
func addProcfileEdges(d *mapdoc.Document, report *deployables.Report) {
	if report == nil {
		return
	}
	componentsByRoot := map[string][]mapdoc.Node{}
	seenEdges := map[string]bool{}
	for _, node := range d.Nodes {
		if node.Kind != mapdoc.NodeComponent {
			continue
		}
		root := node.Properties["root"]
		if root == "" {
			root = "."
		}
		componentsByRoot[path.Clean(root)] = append(componentsByRoot[path.Clean(root)], node)
	}
	for _, edge := range d.Edges {
		seenEdges[edge.ID] = true
	}

	for _, definition := range report.Definitions {
		if definition.Provider != "procfile" || definition.Kind != "process" {
			continue
		}
		probe := mapdoc.NewNode(mapdoc.NodeDeployable, []string{definition.Path}, definition.Provider+":"+definition.Kind+":"+definition.Name)
		deployableIndex := -1
		for i := range d.Nodes {
			if d.Nodes[i].ID == probe.ID && d.Nodes[i].Kind == mapdoc.NodeDeployable {
				deployableIndex = i
				break
			}
		}
		if deployableIndex < 0 {
			continue
		}
		for _, ref := range definition.References {
			if ref.Kind != "process_target" {
				continue
			}
			if ref.Qualification != "local" || ref.SourcePath == "" {
				markProcfileTargetPartial(&d.Nodes[deployableIndex], "procfile_target_unresolved")
				continue
			}
			ecosystem := procfileTargetEcosystem(ref.SourcePath)
			if ecosystem == "" {
				markProcfileTargetPartial(&d.Nodes[deployableIndex], "procfile_target_unsupported")
				continue
			}
			owner, reason := procfileComponentOwner(ref.SourcePath, ecosystem, componentsByRoot)
			if owner.ID == "" {
				markProcfileTargetPartial(&d.Nodes[deployableIndex], reason)
				continue
			}
			e := mapdoc.NewEdge(mapdoc.EdgeRuns, d.Nodes[deployableIndex].ID, owner.ID, "procfile-process:"+definition.Name+":"+ref.SourcePath)
			if seenEdges[e.ID] {
				continue
			}
			seenEdges[e.ID] = true
			e.Coverage = mapdoc.Coverage{Status: mapdoc.CoveragePartial, Reasons: []string{"procfile_process_target_static"}}
			e.Evidence = []mapdoc.Evidence{deployableEvidence(definition.Path, ref.Evidence)}
			d.Edges = append(d.Edges, e)
		}
	}
}

func procfileTargetEcosystem(source string) string {
	switch strings.ToLower(path.Ext(source)) {
	case ".py":
		return "python"
	case ".js", ".mjs", ".cjs":
		return "npm"
	default:
		return ""
	}
}

func procfileComponentOwner(source, ecosystem string, componentsByRoot map[string][]mapdoc.Node) (mapdoc.Node, string) {
	dir := path.Clean(path.Dir(source))
	for {
		components := componentsByRoot[dir]
		if len(components) > 1 {
			return mapdoc.Node{}, "ambiguous_procfile_component_owner"
		}
		if len(components) == 1 {
			component := components[0]
			compatible := component.Properties["ecosystem"] == ecosystem || ecosystem == "python" && component.Properties["ecosystem"] == "python-uv"
			if !compatible {
				return mapdoc.Node{}, "procfile_component_owner_mismatch"
			}
			return component, ""
		}
		if dir == "." {
			break
		}
		dir = path.Dir(dir)
	}
	return mapdoc.Node{}, "procfile_component_owner_missing"
}

func markProcfileTargetPartial(node *mapdoc.Node, reason string) {
	for i := range node.Facts {
		fact := &node.Facts[i]
		if fact.Kind != "deployable_reference" || fact.Name != "process_target" {
			continue
		}
		fact.Coverage = mapdoc.Coverage{Status: mapdoc.CoveragePartial, Reasons: []string{reason}}
		return
	}
}
