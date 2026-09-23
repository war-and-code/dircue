package providerjoin_test

import (
	"testing"

	"dircue/pkg/mapdoc"
	"dircue/pkg/providerjoin"
)

func routingComponent(root, language string) mapdoc.Node {
	n := mapdoc.NewNode(mapdoc.NodeComponent, []string{root}, "project:"+language)
	n.Properties = map[string]string{"root": root, "language": language}
	return n
}

func TestRouteReturnsNonNilEmptySlice(t *testing.T) {
	plans := providerjoin.Route(providerjoin.Input{})
	if plans == nil || len(plans) != 0 {
		t.Fatalf("plans=%#v; want non-nil empty slice", plans)
	}
}

func TestRouteDeduplicatesSharedExecutionScopes(t *testing.T) {
	first := routingComponent("src", "C#")
	second := routingComponent("src", "C#")
	second.Discriminator = "another-project"
	second.ID = mapdoc.NodeID(second.Kind, second.Paths, second.Discriminator)
	plans := providerjoin.Route(providerjoin.Input{Nodes: []mapdoc.Node{second, first}})
	seen := map[string]bool{}
	for _, plan := range plans {
		key := plan.Tool + "\x00" + plan.Scope + "\x00" + plan.ReportKind
		if seen[key] {
			t.Fatalf("duplicate execution plan %q: %+v", key, plans)
		}
		seen[key] = true
		if plan.ComponentID != "" {
			t.Fatalf("shared-scope plan retains misleading component ID: %+v", plan)
		}
	}
	if len(plans) != 4 { // Syft, scc, BCA, and Bifrost.
		t.Fatalf("plans=%+v", plans)
	}
}

func TestRouteMergesSharedScopePrerequisitesConservatively(t *testing.T) {
	withJDK := routingComponent("src", "Java")
	withJDK.Facts = []mapdoc.Fact{{Kind: "java_toolchain", Name: "jdk"}}
	withoutJDK := routingComponent("src", "Java")
	withoutJDK.Discriminator = "second"
	withoutJDK.ID = mapdoc.NodeID(withoutJDK.Kind, withoutJDK.Paths, withoutJDK.Discriminator)
	for _, plan := range providerjoin.Route(providerjoin.Input{Nodes: []mapdoc.Node{withJDK, withoutJDK}}) {
		if plan.Tool != "opentaint" {
			continue
		}
		for _, prerequisite := range plan.Prerequisites {
			if prerequisite.Name == "jdk_version" && prerequisite.Observed {
				t.Fatalf("shared-scope prerequisite overclaimed: %+v", plan)
			}
		}
		return
	}
	t.Fatal("OpenTaint route missing")
}

func TestRouteUsesComponentLocalLanguageApplicability(t *testing.T) {
	vb := routingComponent("vb", "Visual Basic .NET")
	unknown := routingComponent("unknown", "")
	source := mapdoc.NewNode(mapdoc.NodeContent, []string{"somewhere/main.cs"}, "role:source")
	source.Properties = map[string]string{"role": "source"}
	plans := providerjoin.Route(providerjoin.Input{Nodes: []mapdoc.Node{vb, unknown, source}})
	counts := map[string]int{}
	for _, plan := range plans {
		counts[plan.Tool]++
		if plan.Tool == "bca" || plan.Tool == "bifrost" || plan.Tool == "opentaint" || plan.Scope == "unknown" && plan.Tool != "syft" {
			t.Fatalf("repository-global source leaked into unsupported or unattributed component: %+v", plan)
		}
	}
	if len(plans) != 3 || counts["syft"] != 2 || counts["scc"] != 1 {
		t.Fatalf("plans=%+v; want Syft per component and generic scc for attributed VB", plans)
	}
}

func TestRouteUsesRepositoryPopulationOnlyForSyntheticRoot(t *testing.T) {
	source := mapdoc.NewNode(mapdoc.NodeContent, []string{"main.go"}, "role:source")
	source.Properties = map[string]string{"role": "source"}
	population := mapdoc.NewNode(mapdoc.NodeContent, []string{"."}, "language:Go")
	population.Properties = map[string]string{"role": "language_population", "language": "Go"}
	plans := providerjoin.Route(providerjoin.Input{Nodes: []mapdoc.Node{source, population}})
	want := map[string]bool{"scc": true, "bca": true, "bifrost": true}
	for _, plan := range plans {
		if !want[plan.Tool] || plan.Scope != "." || plan.ComponentID != "" {
			t.Fatalf("unexpected synthetic-root plan: %+v", plan)
		}
		delete(want, plan.Tool)
	}
	if len(want) != 0 {
		t.Fatalf("missing synthetic-root plans: %v; got %+v", want, plans)
	}

	withoutPopulation := providerjoin.Route(providerjoin.Input{Nodes: []mapdoc.Node{source}})
	if len(withoutPopulation) != 1 || withoutPopulation[0].Tool != "scc" {
		t.Fatalf("unknown-language root plans=%+v; want generic scc only", withoutPopulation)
	}
}
