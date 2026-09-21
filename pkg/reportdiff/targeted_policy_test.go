package reportdiff

import (
	"testing"

	"dircue/pkg/availability"
	"dircue/pkg/declarations"
	"dircue/pkg/environments"
	"dircue/pkg/profile"
)

func TestAvailabilityDeclarationSourceMustMatchWithinReport(t *testing.T) {
	p := profile.Report{Availability: &availability.Report{Provider: "dircue", ProviderVersion: "1.0.0", Source: availability.Source{Mode: "git", Tree: "tree-a", Consistency: "selected_git_tree", CheckoutMetadata: "not_inspected_for_git_tree"}}, Declarations: &declarations.Report{Source: "git", Tree: "tree-b"}}
	if validTargetedStates(p) {
		t.Fatal("accepted availability correlations backed by another declaration tree")
	}
	p.Declarations.Tree = "tree-a"
	if !validTargetedStates(p) {
		t.Fatal("rejected matching selected-source identities")
	}
}

func TestEnvironmentQualificationPreservesMeasuredLegacyPopulation(t *testing.T) {
	env := &environments.Report{}
	nonempty := moduleInputs(profile.Report{Environments: env, Languages: []profile.Language{{Name: "Go", Bytes: 10, FileCount: 1}}, Summary: profile.Summary{ScannedFiles: 1, AnalyzedFiles: 1, LanguageBytes: 10}})
	if nonempty["languages"].unsupported || nonempty["summary"].unsupported {
		t.Fatal("all --environments population was downgraded")
	}
	empty := moduleInputs(profile.Report{Environments: env})
	if !empty["languages"].unsupported || !empty["summary"].unsupported {
		t.Fatal("standalone environment report implied repository population coverage")
	}
}

func TestAvailabilityReferencesRemainComparableAcrossGitRevisions(t *testing.T) {
	makeProfile := func(tree string) profile.Report {
		return profile.Report{Declarations: &declarations.Report{Provider: "dircue", ProviderVersion: "1.0.0", Status: "complete", Source: "git", Tree: tree}, Availability: &availability.Report{Provider: "dircue", ProviderVersion: "1.0.0", Status: "complete", Source: availability.Source{Mode: "git", Tree: tree, Consistency: "selected_git_tree", CheckoutMetadata: "not_inspected_for_git_tree"}, Coverage: availability.Coverage{SelectedInventoryComplete: true}}}
	}
	a, b := moduleInputs(makeProfile("tree-a"))["availability_references"], moduleInputs(makeProfile("tree-b"))["availability_references"]
	remaining, budget := 10, 1<<20
	m := compareModule("availability_references", a, b, &remaining, &budget)
	if m.Compatibility == "incomparable" {
		t.Fatalf("natural revision delta became policy change: %+v", m)
	}
}
