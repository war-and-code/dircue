package mapdiff_test

import (
	"bytes"
	"testing"

	"dircue/pkg/mapdiff"
	"dircue/pkg/mapdoc"
)

func FuzzCompareInputOrderInvariant(f *testing.F) {
	f.Add("api-before", "api-after", "worker", true)
	f.Add("same", "same", "other", false)
	f.Fuzz(func(t *testing.T, baseName, headName, secondName string, partial bool) {
		if len(baseName)+len(headName)+len(secondName) > 4096 {
			return
		}
		status := mapdoc.CoverageComplete
		if partial {
			status = mapdoc.CoveragePartial
		}
		baseFirst := component(baseName)
		baseSecond := component(secondName)
		baseSecond.Paths = []string{"worker/go.mod"}
		baseSecond.ID = mapdoc.NodeID(baseSecond.Kind, baseSecond.Paths, baseSecond.Discriminator)
		baseSecond.Evidence[0].Path = "worker/go.mod"
		headFirst := component(headName)
		headSecond := baseSecond

		base := testDocument("base-tree", baseFirst, status)
		base.Nodes = append(base.Nodes, baseSecond)
		head := testDocument("head-tree", headFirst, status)
		head.Nodes = append(head.Nodes, headSecond)

		want, err := mapdiff.Compare(base, head)
		if err != nil {
			t.Fatal(err)
		}
		base.Nodes[0], base.Nodes[1] = base.Nodes[1], base.Nodes[0]
		head.Nodes[0], head.Nodes[1] = head.Nodes[1], head.Nodes[0]
		base.Coverage[0], base.Coverage[1] = base.Coverage[1], base.Coverage[0]
		head.Coverage[0], head.Coverage[1] = head.Coverage[1], head.Coverage[0]
		got, err := mapdiff.Compare(base, head)
		if err != nil {
			t.Fatal(err)
		}
		wantJSON, err := mapdiff.Marshal(want)
		if err != nil {
			t.Fatal(err)
		}
		gotJSON, err := mapdiff.Marshal(got)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(wantJSON, gotJSON) {
			t.Fatalf("input ordering changed comparison:\n%s\n%s", wantJSON, gotJSON)
		}
	})
}
