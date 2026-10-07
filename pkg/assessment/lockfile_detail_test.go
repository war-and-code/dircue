package assessment

import (
	"slices"
	"testing"

	"github.com/war-and-code/dircue/pkg/declarations"
	"github.com/war-and-code/dircue/pkg/discovery"
	"github.com/war-and-code/dircue/pkg/lockfiles"
)

func TestLockfileRowsSeparatePresenceOwnershipChecksAndCauses(t *testing.T) {
	ids := []string{"a/A.csproj", "b/B.csproj", "c/C.csproj", "d/D.csproj", "e/E.csproj", "web/package.json"}
	var projects []declarations.Project
	collector := New("directory", "")
	for _, id := range ids {
		kind := "dotnet"
		if id == "web/package.json" {
			kind = "npm"
		}
		projects = append(projects, declarations.Project{ID: id, Root: id[:1], Kind: kind})
		collector.Add(discovery.File{Path: id, Size: 4})
	}
	projects[5].Root = "web"
	evidence := func(presence string, check string, checkReasons []string, causes ...lockfiles.NuGetCause) *lockfiles.NuGetEvidence {
		if causes == nil {
			causes = []lockfiles.NuGetCause{}
		}
		return &lockfiles.NuGetEvidence{PresenceState: presence, CandidatePaths: []string{}, PresenceReasons: []string{}, OwnershipReasons: []string{}, CheckState: check, CheckReasons: checkReasons, Causes: causes}
	}
	unmodeled := lockfiles.NuGetCause{Reason: "nuget-msbuild-input-unmodeled", Path: "Directory.Build.props", Detail: "import Sdk.props from SDK Arcade"}
	conditional := lockfiles.NuGetCause{Reason: "nuget-package-reference-conditional", Path: "b/B.csproj"}
	contexts := []lockfiles.Context{
		{ProjectID: "a/A.csproj", Ecosystem: "nuget", AssociationState: "observed", Checks: []lockfiles.Check{{Status: "match"}}, NuGetEvidence: evidence("observed", "match", []string{})},
		{ProjectID: "b/B.csproj", Ecosystem: "nuget", AssociationState: "observed", Checks: []lockfiles.Check{{Status: "indeterminate"}}, NuGetEvidence: evidence("observed", "indeterminate", []string{"nuget-package-reference-conditional"}, conditional)},
		{ProjectID: "c/C.csproj", Ecosystem: "nuget", AssociationState: "indeterminate", Boundaries: []lockfiles.Boundary{{Path: "Directory.Build.props", Reason: unmodeled.Reason}}, NuGetEvidence: evidence("observed", "not_compared", []string{unmodeled.Reason}, unmodeled)},
		{ProjectID: "d/D.csproj", Ecosystem: "nuget", AssociationState: "indeterminate", Boundaries: []lockfiles.Boundary{{Path: "Directory.Build.props", Reason: unmodeled.Reason}}, NuGetEvidence: evidence("unknown", "not_compared", []string{unmodeled.Reason}, unmodeled)},
		{ProjectID: "e/E.csproj", Ecosystem: "nuget", AssociationState: "not_applicable", NuGetEvidence: evidence("not_observed", "not_compared", []string{"no-direct-declarations"})},
		{ProjectID: "web/package.json", Ecosystem: "npm", AssociationState: "missing", Boundaries: []lockfiles.Boundary{{Reason: "lockfile-not-present"}}},
	}
	contexts[3].NuGetEvidence.PresenceReasons = []string{unmodeled.Reason}
	decls := &declarations.Report{Source: "directory", Status: "complete", Coverage: declarations.Coverage{ManifestCandidates: int64(len(projects)), ParsedManifests: len(projects)}, Projects: projects}
	r, err := collector.Finish(Evidence{Declarations: decls, Lockfiles: &lockfiles.Report{Source: "directory", Status: "partial", Contexts: contexts}, Records: projectRecords(projects)})
	if err != nil {
		t.Fatal(err)
	}
	var nuget LockfileEcosystem
	for _, row := range r.Lockfiles {
		if row.Ecosystem == "nuget" {
			nuget = row
		}
	}
	if nuget.Covered.Count != 2 || nuget.Unknown.Count != 2 || nuget.NotApplicable.Count != 1 {
		t.Fatalf("states: %+v", nuget)
	}
	if c := nuget.Checks; c.Match != 1 || c.Indeterminate != 1 || !slices.Equal(c.IndeterminateReasons, []ReasonCount{{Reason: "nuget-package-reference-conditional", Count: 1}}) {
		t.Fatalf("checks: %+v", c)
	}
	if p := nuget.NuGetPresence; p == nil || p.Observed != 3 || p.NotObserved != 1 || p.Unknown != 1 || !slices.Equal(p.UnknownReasons, []ReasonCount{{Reason: unmodeled.Reason, Count: 1}}) {
		t.Fatalf("presence: %+v", nuget.NuGetPresence)
	}
	want := []LockfileCause{{Reason: unmodeled.Reason, Path: "Directory.Build.props", Count: 2}, {Reason: conditional.Reason, Path: "b/B.csproj", Count: 1}}
	if !slices.Equal(nuget.Causes, want) {
		t.Fatalf("causes: %+v", nuget.Causes)
	}
	if o := r.LockfilesOverall; o.NuGetPresence == nil || o.NuGetPresence.Observed != 3 || o.Checks.Match != 1 {
		t.Fatalf("overall: %+v", o)
	}
	if err := ValidateReport(r); err != nil {
		t.Fatal(err)
	}
	tampered := *r
	tampered.Lockfiles = slices.Clone(r.Lockfiles)
	for i := range tampered.Lockfiles {
		if tampered.Lockfiles[i].Ecosystem == "nuget" {
			checks := *tampered.Lockfiles[i].Checks
			checks.Match++
			tampered.Lockfiles[i].Checks = &checks
		}
	}
	if err := ValidateReport(&tampered); err == nil {
		t.Fatal("check counts that do not partition covered projects were accepted")
	}
}

func TestLockfileRowsRejectRepeatedCauseIdentityWithDifferentCounts(t *testing.T) {
	collector := New("directory", "")
	projects := []declarations.Project{{ID: "A.csproj", Root: ".", Kind: "dotnet"}, {ID: "B.csproj", Root: ".", Kind: "dotnet"}}
	var contexts []lockfiles.Context
	for _, p := range projects {
		collector.Add(discovery.File{Path: p.ID, Size: 1})
		contexts = append(contexts, lockfiles.Context{ProjectID: p.ID, Ecosystem: "nuget", AssociationState: "indeterminate",
			Boundaries: []lockfiles.Boundary{{Reason: "nuget-msbuild-input-unmodeled"}},
			NuGetEvidence: &lockfiles.NuGetEvidence{PresenceState: "unknown", PresenceReasons: []string{"nuget-msbuild-input-unmodeled"},
				Causes: []lockfiles.NuGetCause{{Reason: "nuget-msbuild-input-unmodeled", Path: "Directory.Build.props"}}}})
	}
	decls := &declarations.Report{Source: "directory", Status: "complete", Coverage: declarations.Coverage{ManifestCandidates: 2, ParsedManifests: 2}, Projects: projects}
	r, err := collector.Finish(Evidence{Declarations: decls, Records: projectRecords(projects), Lockfiles: &lockfiles.Report{Source: "directory", Status: "partial", Contexts: contexts}})
	if err != nil {
		t.Fatal(err)
	}
	row := r.Lockfiles[0]
	if len(row.Causes) != 1 || row.Causes[0].Count != 2 {
		t.Fatalf("test requires one shared cause affecting both projects: %+v", row.Causes)
	}
	duplicate := row.Causes[0]
	duplicate.Count = 1
	row.Causes = append(slices.Clone(row.Causes), duplicate)
	// The count-ranked order is valid; identity uniqueness must be checked
	// independently of ordering so one cause cannot be counted twice.
	if err := validateLockfileRow(row, true); err == nil {
		t.Fatal("duplicate reason/path cause with a different count was accepted")
	}
}
