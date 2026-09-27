package mapbuild

// workflow_edges.go: associate GitHub Actions workflows with the components
// they build or test, using declared working-directory evidence (#48).
//
// This is a self-contained pass that runs after addDeployables has indexed
// component roots. It emits EdgeBuilds edges with coverage partial and reason
// workflow_working_directory_association between workflow deployable nodes and
// the component whose root contains (or is) the resolved working directory.
//
// Only local-qualified working_directory references are used; unresolved
// (expression-bearing) references are never promoted to component associations.
// The association is evidence-backed: the edge records the working-directory
// declaration's path and line as its evidence source.

import (
	"path"
	"strings"

	"github.com/war-and-code/dircue/pkg/deployables"
	"github.com/war-and-code/dircue/pkg/mapdoc"
)

// addWorkflowComponentEdges adds partial builds edges between github-actions
// workflow deployable nodes and the nearest containing component, using
// declared working-directory references. It runs after addDeployables has
// populated d.Nodes, so the component index can be rebuilt locally.
//
// The caller passes the same deployables report used by addDeployables.
func addWorkflowComponentEdges(d *mapdoc.Document, r *deployables.Report) {
	if r == nil {
		return
	}

	// Build component root → IDs index from the already-built document nodes.
	componentsByRoot := map[string][]string{}
	for _, n := range d.Nodes {
		if n.Kind == mapdoc.NodeComponent {
			root := n.Properties["root"]
			if root != "" {
				componentsByRoot[root] = append(componentsByRoot[root], n.ID)
			}
		}
	}
	if len(componentsByRoot) == 0 {
		return
	}

	// Build workflow deployable node ID → presence flag for quick lookup.
	deployableByID := map[string]mapdoc.Node{}
	for _, n := range d.Nodes {
		if n.Kind == mapdoc.NodeDeployable {
			deployableByID[n.ID] = n
		}
	}

	// Index existing edge IDs to avoid duplicates.
	seenEdges := map[string]bool{}
	for _, e := range d.Edges {
		seenEdges[e.ID] = true
	}

	for _, def := range r.Definitions {
		if def.Provider != "github-actions" || def.Kind != "workflow" {
			continue
		}

		// Find the deployable node for this definition.
		nodePaths := []string{def.Path}
		probe := mapdoc.NewNode(mapdoc.NodeDeployable, nodePaths, def.Provider+":"+def.Kind+":"+def.Name)
		if _, ok := deployableByID[probe.ID]; !ok {
			continue
		}
		workflowID := probe.ID

		for _, ref := range def.References {
			if ref.Kind != "working_directory" || ref.Qualification != "local" {
				continue
			}
			// Resolve the working-directory path relative to GITHUB_WORKSPACE
			// (i.e., the repository root). The workflow file is always under
			// .github/workflows/, so its directory is not the resolution base.
			resolved := path.Clean(ref.Value)

			// P2: Paths that escape the repository are never attributed.
			if resolved == ".." || strings.HasPrefix(resolved, "../") {
				continue
			}

			// Exact match at the resolved path.
			if owners := componentsByRoot[resolved]; len(owners) == 1 {
				addWorkflowEdge(d, seenEdges, workflowID, owners[0], resolved, def.Path, ref.Evidence)
				continue
			}
			// Walk up to find the nearest ancestor component root.
			if owners, _ := componentAncestorOwners(componentsByRoot, resolved); len(owners) == 1 {
				addWorkflowEdge(d, seenEdges, workflowID, owners[0], resolved, def.Path, ref.Evidence)
			}
		}
	}
}

// addWorkflowEdge emits a partial builds edge between a workflow deployable
// and a component, citing the working-directory declaration as evidence.
func addWorkflowEdge(d *mapdoc.Document, seen map[string]bool, workflowID, componentID, resolvedDir, defPath string, ev deployables.Evidence) {
	discriminator := "workflow-workdir:" + resolvedDir + ":" + componentID
	e := mapdoc.NewEdge(mapdoc.EdgeBuilds, workflowID, componentID, discriminator)
	if seen[e.ID] {
		return
	}
	seen[e.ID] = true
	e.Coverage = mapdoc.Coverage{Status: mapdoc.CoveragePartial, Reasons: []string{"workflow_working_directory_association"}}
	e.Evidence = []mapdoc.Evidence{deployableEvidence(defPath, ev)}
	d.Edges = append(d.Edges, e)
}
