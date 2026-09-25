package providerjoin

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/war-and-code/dircue/pkg/mapdoc"
)

func FuzzProviderIngressDeterministicAndPortable(f *testing.F) {
	f.Add(byte(0), []byte(`{"descriptor":{"name":"syft","version":"1"},"artifacts":[]}`))
	f.Add(byte(1), []byte(`{"version":"2.1.0","runs":[]}`))
	f.Add(byte(2), []byte(`{"version":"1","endpoints":[]}`))
	f.Add(byte(3), []byte(`{"results":[],"truncated":false}`))
	f.Add(byte(0), []byte(`null`))
	f.Fuzz(func(t *testing.T, provider byte, data []byte) {
		if len(data) > 1<<20 {
			return
		}
		in := fuzzProviderInput()
		first, firstErr := fuzzIngest(provider, data, in)
		second, secondErr := fuzzIngest(provider, data, in)
		if (firstErr == nil) != (secondErr == nil) {
			t.Fatalf("provider ingress changed acceptance: %v then %v", firstErr, secondErr)
		}
		if firstErr != nil {
			return
		}
		if !reflect.DeepEqual(first, second) {
			left, _ := json.Marshal(first)
			right, _ := json.Marshal(second)
			t.Fatalf("provider ingress is nondeterministic:\n%s\n%s", left, right)
		}
		doc := mapdoc.Document{
			SchemaVersion: mapdoc.SchemaVersion,
			Kind:          "map",
			Status:        mapdoc.CoveragePartial,
			Source:        mapdoc.Source{Mode: "git", Revision: "fuzz", Tree: "fuzz-tree"},
			Coverage: []mapdoc.QuestionCoverage{{
				Question: mapdoc.QuestionSourceBinding,
				Scope:    ".",
				Coverage: mapdoc.Coverage{Status: mapdoc.CoverageComplete},
			}},
			Nodes: append([]mapdoc.Node{in.Nodes[0]}, first.Nodes...),
			Edges: first.Edges,
		}
		if _, err := mapdoc.Normalize(doc); err != nil {
			encoded, _ := json.Marshal(first)
			t.Fatalf("accepted provider result is not a portable map fragment: %v\n%s", err, encoded)
		}
	})
}

func fuzzIngest(provider byte, data []byte, in Input) (Result, error) {
	switch provider % 4 {
	case 0:
		return ingestSyft(data, in, 64, "fuzz")
	case 1:
		return ingestSARIF(data, in, 64, "fuzz")
	case 2:
		return ingestNoir(data, in, 64, "fuzz")
	default:
		return ingestBifrost(data, in, 64, "fuzz")
	}
}

func fuzzProviderInput() Input {
	node := mapdoc.NewNode(mapdoc.NodeComponent, []string{"services/api", "services/api/go.mod"}, "go")
	node.Name = "api"
	node.Properties = map[string]string{"language": "Go", "root": "services/api"}
	node.Coverage = mapdoc.Coverage{Status: mapdoc.CoverageComplete}
	node.Evidence = []mapdoc.Evidence{{
		Basis:      mapdoc.BasisDeclaredConfig,
		Path:       "services/api/go.mod",
		SourceKind: mapdoc.SourceConfiguration,
		Rule:       &mapdoc.Producer{ID: "fuzz-fixture", Version: "1"},
	}}
	return Input{Root: ".", Snapshot: Snapshot{Mode: "git", Tree: "fuzz-tree"}, Nodes: []mapdoc.Node{node}}
}
