package mapbuild

import (
	"testing"

	"dircue/pkg/discovery"
	"dircue/pkg/mapdoc"
	"dircue/pkg/profile"
)

func TestContentPopulationsHaveUserMeaningfulStableIdentity(t *testing.T) {
	makeReport := func(first, second string) *profile.Report {
		return &profile.Report{Discovery: &discovery.Report{
			Categories: []discovery.Group{
				{Name: "source_candidate", Basis: first, Counts: discovery.Counts{Files: 2, Bytes: 20}},
				{Name: "source_candidate", Basis: second, Counts: discovery.Counts{Files: 3, Bytes: 30}},
				{Name: "documentation_candidate", Basis: first, Counts: discovery.Counts{Files: 1, Bytes: 10}},
			},
			Roles: []discovery.Group{
				{Name: "documentation", Basis: second, Counts: discovery.Counts{Files: 1, Bytes: 10}},
			},
		}}
	}
	first := contentNodes(makeReport("enry_extension", "enry_filename"))
	second := contentNodes(makeReport("enry_path", "gitattributes"))
	if len(first) != 2 || len(second) != 2 {
		t.Fatalf("expected one source and one documentation population: %d, %d", len(first), len(second))
	}
	byRole := func(nodes []mapdoc.Node) map[string]mapdoc.Node {
		out := map[string]mapdoc.Node{}
		for _, node := range nodes {
			out[node.Properties["role"]] = node
		}
		return out
	}
	a, b := byRole(first), byRole(second)
	if a["source"].ID != b["source"].ID || a["documentation"].ID != b["documentation"].ID {
		t.Fatalf("Enry mechanism changed population identities: %+v vs %+v", a, b)
	}
	if a["source"].Properties["files"] != "5" || a["source"].Properties["bytes"] != "50" {
		t.Fatalf("source groups were not combined: %+v", a["source"].Properties)
	}
	if a["documentation"].Properties["files"] != "1" {
		t.Fatalf("documentation category and role were double-counted: %+v", a["documentation"].Properties)
	}
}

func TestSingleMapDoesNotClaimMaterialChangeQuestion(t *testing.T) {
	report := &profile.Report{Discovery: &discovery.Report{Status: "complete", Source: discovery.Source{Mode: "directory"}}}
	doc, err := Build(report, Options{})
	if err != nil {
		t.Fatal(err)
	}
	for _, question := range doc.Coverage {
		if question.Question == "material_change" {
			t.Fatal("a single directory map cannot answer a comparison question")
		}
	}
}
