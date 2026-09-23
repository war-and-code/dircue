package mapbuild

import (
	"testing"

	"dircue/pkg/deployables"
	"dircue/pkg/discovery"
	"dircue/pkg/mapdoc"
	"dircue/pkg/profile"
)

func TestFilenameHintDoesNotClaimValidatedBinary(t *testing.T) {
	n := fileNode("opaque.lib", "binary", "static_library", "extension", 20)
	if n.Coverage.Status != mapdoc.CoveragePartial || n.Evidence[0].Basis != mapdoc.BasisFilenameHint {
		t.Fatalf("unverified suffix claimed a complete binary: %+v", n)
	}
}

func TestDeclaredBuildAndRunLinksHaveEvidence(t *testing.T) {
	doc := mapdoc.New()
	component := mapdoc.NewNode(mapdoc.NodeComponent, []string{"services/api", "services/api/go.mod"}, "go")
	component.Properties = map[string]string{"root": "services/api"}
	doc.Nodes = append(doc.Nodes, component)
	evidence := deployables.Evidence{Field: "declaration", Line: 1, Basis: "static-field"}
	definitions := []deployables.Definition{
		{Provider: "compose", Kind: "service", Name: "api", Path: "compose.yml", Coverage: "complete", Evidence: []deployables.Evidence{evidence}, References: []deployables.Reference{
			{Kind: "build_context", Value: "services/api", Qualification: "local", Evidence: evidence},
			{Kind: "image", Value: "example/api:v1", Qualification: "external", Evidence: evidence},
		}},
		{Provider: "dockerfile", Kind: "container_build", Name: "default", Path: "services/api/Dockerfile", Coverage: "complete", Evidence: []deployables.Evidence{evidence}},
		{Provider: "kubernetes", Kind: "workload", Name: "api", Path: "k8s/api.yaml", Coverage: "qualified", Evidence: []deployables.Evidence{evidence}, References: []deployables.Reference{{Kind: "image", Value: "example/api:v1", Qualification: "external", Evidence: evidence}}},
		{Provider: "cloudformation", Kind: "infrastructure", Name: "function", Path: "template.yaml", Coverage: "qualified", Evidence: []deployables.Evidence{evidence}, References: []deployables.Reference{{Kind: "code_uri", Value: "services/api", Qualification: "local", Evidence: evidence}}},
	}
	addDeployables(&doc, &deployables.Report{Status: "complete", Definitions: definitions})
	builds, runs := 0, 0
	for _, edge := range doc.Edges {
		if edge.To != component.ID {
			t.Fatalf("edge points to wrong component: %+v", edge)
		}
		if edge.Coverage.Status != mapdoc.CoveragePartial || len(edge.Evidence) == 0 {
			t.Fatalf("declared link lacks qualification or evidence: %+v", edge)
		}
		switch edge.Type {
		case mapdoc.EdgeBuilds:
			builds++
		case mapdoc.EdgeRuns:
			runs++
		}
	}
	if builds != 3 || runs != 2 {
		t.Fatalf("want Dockerfile, Compose and SAM builds plus Compose and Kubernetes runs; got builds=%d runs=%d", builds, runs)
	}
	for _, question := range doc.Coverage {
		if question.Question == "deployables" && question.Status != mapdoc.CoveragePartial {
			t.Fatalf("bounded parser claimed complete deployable catalog: %+v", question)
		}
	}
}

func TestDuplicateObserverIdentityDoesNotDiscardWholeMap(t *testing.T) {
	evidence := deployables.Evidence{Field: "provider", Line: 1, Basis: "static-field"}
	definition := deployables.Definition{Provider: "terraform", Kind: "infrastructure", Name: "provider:aws", Path: "main.tf", Coverage: "qualified", Evidence: []deployables.Evidence{evidence}}
	report := &profile.Report{Discovery: &discovery.Report{Status: "complete", Source: discovery.Source{Mode: "directory"}}}
	doc, err := Build(report, Options{Deployables: &deployables.Report{Status: "complete", Definitions: []deployables.Definition{definition, definition}}})
	if err != nil {
		t.Fatalf("duplicate observer identity discarded the map: %v", err)
	}
	count := 0
	for _, n := range doc.Nodes {
		if n.Kind == mapdoc.NodeDeployable {
			count++
			if n.Coverage.Status != mapdoc.CoveragePartial {
				t.Fatalf("duplicate was not qualified: %+v", n)
			}
		}
	}
	if count != 1 {
		t.Fatalf("retained deployable count=%d", count)
	}
	for _, q := range doc.Coverage {
		if q.Question == "deployables" && q.Status == mapdoc.CoveragePartial && len(q.Reasons) == 2 {
			return
		}
	}
	t.Fatal("duplicate omission was not disclosed in deployable coverage")
}

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

func TestAuxiliaryPathRolesAreHintsNotDeletedFacts(t *testing.T) {
	for _, test := range []struct{ filename, role string }{
		{"services/api/App.csproj", ""},
		{"tests/map_corpus/fixtures/app/package.json", "fixture"},
		{"src/Tests/Compiler.Tests.csproj", "test"},
		{"examples/demo/Dockerfile", "example"},
		{"vendor/lib/Cargo.toml", "vendored"},
	} {
		if got := mapPathRole(test.filename); got != test.role {
			t.Errorf("%s: role %q, want %q", test.filename, got, test.role)
		}
	}
}
