package reportdiff

import (
	"testing"

	"github.com/war-and-code/dircue/pkg/availability"
	"github.com/war-and-code/dircue/pkg/declarations"
	"github.com/war-and-code/dircue/pkg/environments"
	"github.com/war-and-code/dircue/pkg/profile"
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

func TestUnresolvedToolchainRowsCannotProveEnvironmentAbsence(t *testing.T) {
	base := &environments.Report{Provider: environments.Provider, ProviderVersion: environments.LegacyProviderVersion, Status: "complete", Source: "directory", Requirements: []environments.Requirement{{ProjectID: "app/package.json", ContextID: "app/package.json", Dimension: "runtime-constraint", Kind: "npm-engine", Value: ">=20", State: "declared", Evidence: "app/package.json", Applicability: "project declaration"}}}
	head := &environments.Report{Provider: environments.Provider, ProviderVersion: environments.ProviderVersion, Status: "complete", Source: "directory", ToolchainDeclarations: []environments.ToolchainDeclaration{{SourcePath: ".nvmrc", Tool: "node", Kind: "nvmrc", State: "unsupported", ScopeDirectory: ".", Applicability: "literal declaration scope"}}}
	a := moduleInputs(profile.Report{Environments: base})["environments"]
	b := moduleInputs(profile.Report{Environments: head})["environments"]
	if b.complete {
		t.Fatal("unsupported toolchain declaration established complete coverage")
	}
	// Compare's provider-version guard also downgrades the old 1.0 population;
	// mirror it here so the test isolates the unavailable-deletion rule.
	a.complete = false
	remaining, budget := 10, 1<<20
	comparison := compareModule("environments", a, b, &remaining, &budget)
	if comparison.Compatibility != "observed_only" || comparison.Counts.Added != 0 || comparison.Counts.Unavailable != 2 {
		t.Fatalf("unresolved declaration promoted absence to a known addition: %+v", comparison)
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
