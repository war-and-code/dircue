package mapdiff

import (
	"testing"

	"github.com/war-and-code/dircue/pkg/mapdoc"
)

func TestCompatibleProducersIgnoresRulesThatFiredOnOneSide(t *testing.T) {
	base := []string{"rule:dircue/component-declarations@1.0.0"}
	head := []string{"rule:dircue/component-declarations@1.0.0", "rule:dircue/deployables/dockerfile-instruction@1.0.0"}
	if !compatibleProducers(base, head) {
		t.Fatal("a rule that fired only in head must not make observers incompatible")
	}
	if compatibleProducers(base, []string{"rule:dircue/component-declarations@1.1.0"}) {
		t.Fatal("a shared rule at a different version must make observers incompatible")
	}
}

func TestLabelChangesNamesNodesAndEdges(t *testing.T) {
	nodes := map[string]mapdoc.Node{
		"component:a":  {ID: "component:a", Kind: mapdoc.NodeComponent, Name: "api", Paths: []string{"svc/api"}},
		"deployable:b": {ID: "deployable:b", Kind: mapdoc.NodeDeployable, Name: "Dockerfile", Paths: []string{"svc/api/Dockerfile"}},
	}
	edges := map[string]mapdoc.Edge{"edge:c": {ID: "edge:c", Type: mapdoc.EdgeBuilds, From: "deployable:b", To: "component:a"}}
	changes := []Change{{Entity: "node", ID: "component:a"}, {Entity: "edge", ID: "edge:c"}}
	labelChanges(changes, nil, nodes, nil, edges)
	if changes[0].Kind != "component" || changes[0].Label != "api (svc/api)" {
		t.Fatalf("node label = %q/%q", changes[0].Kind, changes[0].Label)
	}
	if changes[1].Kind != "builds" || changes[1].Label != "Dockerfile (svc/api/Dockerfile) builds api (svc/api)" {
		t.Fatalf("edge label = %q/%q", changes[1].Kind, changes[1].Label)
	}
}
