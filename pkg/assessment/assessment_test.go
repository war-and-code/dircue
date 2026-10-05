package assessment

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/war-and-code/dircue/pkg/declarations"
	"github.com/war-and-code/dircue/pkg/discovery"
	"github.com/war-and-code/dircue/pkg/lockfiles"
)

func TestDotnetBackslashSelectedPathBoundsProjectMetrics(t *testing.T) {
	name := `dir\p.csproj`
	files := map[string]string{name: `<Project Sdk="Microsoft.NET.Sdk"><PropertyGroup><TargetFramework>net8.0</TargetFramework></PropertyGroup></Project>`}
	decls, records := parseAssessmentFixture(t, files)
	if len(decls.Projects) != 1 || decls.Projects[0].Kind != "dotnet-configuration" || !records[0].Parsed {
		t.Fatalf("expected the adapter's configuration-shaped mismatched-path record, projects=%+v records=%+v", decls.Projects, records)
	}
	collector := New("directory", "")
	collector.Add(discovery.File{Path: name, Size: int64(len(files[name]))})
	locks := &lockfiles.Report{Source: "directory", Status: "complete", Contexts: []lockfiles.Context{}}
	report, err := collector.Finish(Evidence{Declarations: decls, Lockfiles: locks, Records: records})
	if err != nil {
		t.Fatal(err)
	}
	if report.ManifestCandidatePopulation.Completeness != "complete" || report.ManifestCandidatePopulation.Count != 1 {
		t.Fatalf("selected-file aggregate changed: %+v", report.ManifestCandidatePopulation)
	}
	for name, metric := range map[string]Metric{
		"projects":            report.Projects,
		"roots":               report.ProjectRoots,
		"workspace relations": report.WorkspaceMembership,
		"local relations":     report.LocalDependencies,
		"lockfile projects":   report.LockfilesOverall.Projects,
		"lockfile unknown":    report.LockfilesOverall.Unknown,
	} {
		if metric.Completeness != "lower_bound" || !slices.Contains(metric.Reasons, "dotnet_selected_path_identity_ambiguous") {
			t.Fatalf("%s falsely claims complete coverage: %+v", name, metric)
		}
	}
	if err := ValidateReport(report); err != nil {
		t.Fatalf("ValidateReport: %v", err)
	}
}

func TestGoWorkIsAWorkspaceRecordNotPackageProject(t *testing.T) {
	files := map[string]string{
		"go.work":      "go 1.24.0\nuse (\n ./app\n ./tools\n)\n",
		"app/go.mod":   "module example.com/app\ngo 1.24.0\n",
		"tools/go.mod": "module example.com/tools\ngo 1.24.0\n",
	}
	decls, records := parseAssessmentFixture(t, files)
	collector := New("directory", "")
	for name, content := range files {
		collector.Add(discovery.File{Path: name, Size: int64(len(content))})
	}
	report, err := collector.Finish(Evidence{Declarations: decls, Lockfiles: &lockfiles.Report{Source: "directory", Status: "complete", Contexts: []lockfiles.Context{}}, Records: records})
	if err != nil {
		t.Fatal(err)
	}
	if report.Projects.Count != 2 || report.ProjectRoots.Count != 2 {
		t.Fatalf("go.work inflated project totals: projects=%+v roots=%+v", report.Projects, report.ProjectRoots)
	}
	if report.WorkspaceMembership.Count != 2 {
		t.Fatalf("go.work members were not retained: %+v", report.WorkspaceMembership)
	}
	foundGoWorkSource := false
	for _, evidence := range report.WorkspaceEvidence {
		if evidence.SourceProject == "go.work" && evidence.Kind == "go-workspace-member" {
			foundGoWorkSource = true
		}
	}
	if !foundGoWorkSource {
		t.Fatalf("go.work source missing from membership evidence: %+v", report.WorkspaceEvidence)
	}
}

func TestDuplicateProjectRecordsLowerBoundAllProjectPopulationMetrics(t *testing.T) {
	project := declarations.Project{ID: "package.json", Root: ".", Kind: "npm"}
	decls := &declarations.Report{Source: "directory", Status: "complete", Coverage: declarations.Coverage{ManifestCandidates: 1, ParsedManifests: 1}, Projects: []declarations.Project{project, project}}
	records := []declarations.ProjectRecord{{Project: project, Parsed: true, Complete: true}, {Project: project, Parsed: true, Complete: true}}
	collector := New("directory", "")
	collector.Add(discovery.File{Path: project.ID, Size: 1})
	locks := &lockfiles.Report{Source: "directory", Status: "complete", Contexts: []lockfiles.Context{}}
	report, err := collector.Finish(Evidence{Declarations: decls, Lockfiles: locks, Records: records})
	if err != nil {
		t.Fatal(err)
	}
	for name, metric := range map[string]Metric{
		"projects":           report.Projects,
		"roots":              report.ProjectRoots,
		"workspace":          report.WorkspaceMembership,
		"local dependencies": report.LocalDependencies,
		"lockfile projects":  report.LockfilesOverall.Projects,
		"lockfile unknown":   report.LockfilesOverall.Unknown,
	} {
		if metric.Completeness != "lower_bound" || !slices.Contains(metric.Reasons, "parsed_project_observations_incomplete") {
			t.Fatalf("%s falsely claims complete coverage: %+v", name, metric)
		}
	}
	if err := ValidateReport(report); err != nil {
		t.Fatalf("ValidateReport: %v", err)
	}
}

func parseAssessmentFixture(t *testing.T, files map[string]string) (*declarations.Report, []declarations.ProjectRecord) {
	t.Helper()
	collector := declarations.New("directory", "", 0)
	collector.EnableProjectRecords()
	for name, content := range files {
		data := []byte(content)
		collector.Add(name, &declarations.Candidate{Path: name, Size: int64(len(data)), Read: func(context.Context, int64) ([]byte, int64, error) {
			return data, int64(len(data)), nil
		}})
	}
	report, err := collector.Finish(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return report, collector.ProjectRecords()
}

func TestFinishSeparatesExactCountsFromBoundedEvidence(t *testing.T) {
	c := New("directory", "")
	for i := 0; i < 300; i++ {
		c.Add(discovery.File{Path: fmt.Sprintf("packages/p%03d/package.json", i), Size: int64(i + 1)})
	}
	decls := &declarations.Report{Source: "directory", Status: "complete", Coverage: declarations.Coverage{ManifestCandidates: 300, ParsedManifests: 300}}
	locks := &lockfiles.Report{Source: "directory", Status: "complete", Contexts: []lockfiles.Context{}}
	report, err := c.Finish(Evidence{Declarations: decls, Lockfiles: locks, Records: projectRecords(decls.Projects)})
	if err != nil {
		t.Fatal(err)
	}
	if got := report.ManifestCandidatePopulation.Count; got != 300 {
		t.Fatalf("manifest population = %d, want 300", got)
	}
	if got := report.ManifestCandidates[0].Files; got != 300 {
		t.Fatalf("exact filename count = %d, want 300", got)
	}
	if len(report.CandidateEvidence) != EvidenceLimitPerKind {
		t.Fatalf("evidence size = %d, want %d", len(report.CandidateEvidence), EvidenceLimitPerKind)
	}
	if got := report.OmittedCandidateEvidence["manifest"]; got != 44 {
		t.Fatalf("omitted evidence = %d, want 44", got)
	}
	if report.ManifestCandidatePopulation.Completeness != "complete" {
		t.Fatalf("sample cap changed aggregate completeness: %+v", report.ManifestCandidatePopulation)
	}
	if err := ValidateReport(report); err != nil {
		t.Fatalf("ValidateReport: %v", err)
	}
}

func TestUnsupportedProjectFormsPartitionPopulationOutsideEligibility(t *testing.T) {
	c := New("directory", "")
	projects := []declarations.Project{
		{ID: "py/pyproject.toml", Root: "py", Kind: "python"},
		{ID: "native/lib.vcxproj", Root: "native", Kind: "dotnet"},
		{ID: "web/package.json", Root: "web", Kind: "npm"},
	}
	for _, p := range projects {
		c.Add(discovery.File{Path: p.ID, Size: 10})
	}
	decls := &declarations.Report{Source: "directory", Status: "complete", Coverage: declarations.Coverage{ManifestCandidates: 3, ParsedManifests: 3}, Projects: projects}
	locks := &lockfiles.Report{Source: "directory", Status: "complete", Contexts: []lockfiles.Context{{ProjectID: "web/package.json", Ecosystem: "npm", AssociationState: "missing"}}}
	report, err := c.Finish(Evidence{Declarations: decls, Lockfiles: locks, Records: projectRecords(projects)})
	if err != nil {
		t.Fatal(err)
	}
	o := report.LockfilesOverall
	if o.Projects.Count != 3 || o.Eligible.Count != 1 || o.Missing.Count != 1 || o.Unsupported.Count != 2 {
		t.Fatalf("project partition incorrect: %+v", o)
	}
	if len(o.OutcomeReasons) != 2 || o.OutcomeReasons[1] != (OutcomeReason{State: "unsupported", Reason: "ecosystem-outside-association-scope", Count: 2}) {
		t.Fatalf("outcome reasons: %+v", o.OutcomeReasons)
	}
	if err := ValidateReport(report); err != nil {
		t.Fatalf("ValidateReport: %v", err)
	}
}

func TestProjectTotalsExcludeDotnetConfigurationsAndSolutions(t *testing.T) {
	c := New("directory", "")
	projects := make([]declarations.Project, 0, 16)
	records := make([]declarations.ProjectRecord, 0, 16)
	for i := 0; i < 14; i++ {
		p := declarations.Project{ID: fmt.Sprintf("src/P%02d.csproj", i), Root: "src", Kind: "dotnet"}
		projects = append(projects, p)
		records = append(records, declarations.ProjectRecord{Project: p, Parsed: true, Complete: true})
		c.Add(discovery.File{Path: p.ID, Size: 4})
	}
	props := declarations.Project{ID: "Directory.Build.props", Root: ".", Kind: "dotnet-configuration"}
	sln := declarations.Project{ID: "App.sln", Root: ".", Kind: "solution", References: []declarations.Reference{{Kind: "solution-member", Target: "src/P00.csproj", TargetStatus: "present", State: "declared"}}}
	projects = append(projects, props, sln)
	records = append(records, declarations.ProjectRecord{Project: props, Parsed: true, Complete: true}, declarations.ProjectRecord{Project: sln, Parsed: true, Complete: true})
	decls := &declarations.Report{Source: "directory", Status: "complete", Coverage: declarations.Coverage{ManifestCandidates: 16, ParsedManifests: 16}, Projects: projects}
	report, err := c.Finish(Evidence{Declarations: decls, Lockfiles: &lockfiles.Report{Source: "directory", Status: "complete"}, Records: records})
	if err != nil {
		t.Fatal(err)
	}
	if report.Projects.Count != 14 || report.ProjectRoots.Count != 1 || report.WorkspaceMembership.Count != 1 {
		t.Fatalf("configuration/solution inflated package totals: projects=%d roots=%d workspace=%d", report.Projects.Count, report.ProjectRoots.Count, report.WorkspaceMembership.Count)
	}
	if report.LockfilesOverall.Projects.Count != 14 || report.LockfilesOverall.Unknown.Count != 14 {
		t.Fatalf("unsupported association contexts were not explicit: %+v", report.LockfilesOverall)
	}
	if err := ValidateReport(report); err != nil {
		t.Fatalf("ValidateReport: %v", err)
	}
}

func TestManifestGroupNormalizesProjectFilenames(t *testing.T) {
	c := New("directory", "")
	for i := 0; i < 300; i++ {
		c.Add(discovery.File{Path: fmt.Sprintf("projects/p%d.csproj", i), Size: 2})
	}
	decls := &declarations.Report{Source: "directory", Status: "complete", Coverage: declarations.Coverage{ManifestCandidates: 300, ParsedManifests: 300}}
	r, err := c.Finish(Evidence{Declarations: decls})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.ManifestCandidates) != 1 || r.ManifestCandidates[0].Filename != "*.csproj" || r.ManifestCandidates[0].Files != 300 {
		t.Fatalf("manifest grouping: %+v", r.ManifestCandidates)
	}
	if err := ValidateReport(r); err != nil {
		t.Fatalf("ValidateReport: %v", err)
	}
}

func TestFinishBoundsEscapedEvidenceBytesWithoutChangingTotals(t *testing.T) {
	c := New("directory", "")
	projects := make([]declarations.Project, 0, ProjectRootEvidenceLimit)
	for i := 0; i < ProjectRootEvidenceLimit; i++ {
		root := "odd/" + strings.Repeat("\x01", 2880) + fmt.Sprintf("%03d", i)
		p := declarations.Project{ID: root + "/package.json", Root: root, Kind: "npm"}
		projects = append(projects, p)
		c.Add(discovery.File{Path: p.ID, Size: 8})
	}
	decls := &declarations.Report{Source: "directory", Status: "complete", Coverage: declarations.Coverage{ManifestCandidates: int64(len(projects)), ParsedManifests: len(projects)}, Projects: projects}
	r, err := c.Finish(Evidence{Declarations: decls, Lockfiles: &lockfiles.Report{Source: "directory", Status: "complete"}, Records: projectRecords(projects)})
	if err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	if len(b) > MaxAssessmentJSONBytes {
		t.Fatalf("serialized report size = %d, limit = %d", len(b), MaxAssessmentJSONBytes)
	}
	if r.Projects.Count != ProjectRootEvidenceLimit || r.ManifestCandidatePopulation.Count != ProjectRootEvidenceLimit {
		t.Fatalf("evidence trimming changed aggregate totals: projects=%d manifests=%d", r.Projects.Count, r.ManifestCandidatePopulation.Count)
	}
	if len(r.ProjectRootEvidence) == ProjectRootEvidenceLimit && len(r.CandidateEvidence) == EvidenceLimitPerKind {
		t.Fatal("expected escaped-path evidence to be trimmed")
	}
	if err := ValidateReport(r); err != nil {
		t.Fatalf("ValidateReport: %v", err)
	}
}

func TestFinishCountsParsedRootsAndSeparateRelationships(t *testing.T) {
	c := New("directory", "")
	c.Add(discovery.File{Path: "repo/package.json", Size: 3})
	decls := &declarations.Report{Source: "directory", Status: "complete", Coverage: declarations.Coverage{ManifestCandidates: 1, ParsedManifests: 1}, Projects: []declarations.Project{
		{ID: "repo/package.json", Root: "repo", Kind: "npm", References: []declarations.Reference{
			{Kind: "npm-workspace-member", Target: "repo/packages/lib/package.json", TargetStatus: "present", State: "declared"},
			{Kind: "npm-local-dependency", Target: "repo/packages/lib/package.json", TargetStatus: "present", State: "declared"},
			{Kind: "npm-dependency", Value: "external", TargetStatus: "external", State: "declared"},
		}},
		{ID: "repo/packages/lib/package.json", Root: "repo/packages/lib", Kind: "npm"},
	}}
	locks := &lockfiles.Report{Source: "directory", Status: "complete", Contexts: []lockfiles.Context{
		{ProjectID: "repo/package.json", Ecosystem: "npm", AssociationState: "observed"},
		{ProjectID: "repo/packages/lib/package.json", Ecosystem: "npm", AssociationState: "indeterminate"},
	}}
	report, err := c.Finish(Evidence{Declarations: decls, Lockfiles: locks, Records: projectRecords(decls.Projects)})
	if err != nil {
		t.Fatal(err)
	}
	if report.Projects.Count != 2 || report.ProjectRoots.Count != 2 {
		t.Fatalf("project totals: projects=%d roots=%d", report.Projects.Count, report.ProjectRoots.Count)
	}
	if report.WorkspaceMembership.Count != 1 || report.LocalDependencies.Count != 1 {
		t.Fatalf("relationship totals: workspace=%d local=%d", report.WorkspaceMembership.Count, report.LocalDependencies.Count)
	}
	if report.LockfilesOverall.Eligible.Count != 2 || report.LockfilesOverall.Covered.Count != 1 || report.LockfilesOverall.Missing.Count != 0 || report.LockfilesOverall.Unknown.Count != 1 {
		t.Fatalf("lock totals do not reconcile: %+v", report.LockfilesOverall)
	}
	if err := ValidateReport(report); err != nil {
		t.Fatalf("ValidateReport: %v", err)
	}
}

func TestFinishQualifiesInvalidPathAndSourceMismatch(t *testing.T) {
	c := New("directory", "")
	c.Add(discovery.File{Path: "../../escape/package.json", Size: 7})
	decls := &declarations.Report{Source: "directory", Status: "complete"}
	report, err := c.Finish(Evidence{Declarations: decls})
	if err != nil {
		t.Fatal(err)
	}
	if report.Inventory.Files.Completeness != "lower_bound" || report.ManifestCandidatePopulation.Completeness != "lower_bound" {
		t.Fatalf("invalid path did not qualify metrics: %+v", report)
	}
	if err := ValidateReport(report); err != nil {
		t.Fatalf("ValidateReport: %v", err)
	}
	if _, err := New("git", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa").Finish(Evidence{Declarations: decls}); err == nil {
		t.Fatal("expected source mismatch error")
	}
}

func TestFinishAcceptsLiteralBackslashInPOSIXFilename(t *testing.T) {
	c := New("directory", "")
	c.Add(discovery.File{Path: "odd\\dir/package.json", Size: 4})
	decls := &declarations.Report{Source: "directory", Status: "complete", Coverage: declarations.Coverage{ManifestCandidates: 1, ParsedManifests: 1}, Projects: []declarations.Project{{ID: "odd\\dir/package.json", Root: "odd\\dir", Kind: "npm"}}}
	report, err := c.Finish(Evidence{Declarations: decls, Lockfiles: &lockfiles.Report{Source: "directory", Status: "complete"}, Records: projectRecords(decls.Projects)})
	if err != nil {
		t.Fatal(err)
	}
	if report.ManifestCandidatePopulation.Count != 1 {
		t.Fatalf("literal backslash path dropped: %+v", report.ManifestCandidates)
	}
	if err := ValidateReport(report); err != nil {
		t.Fatalf("ValidateReport: %v", err)
	}
}

func TestValidateReportRejectsCounterfeitAggregateAfterSampling(t *testing.T) {
	c := New("directory", "")
	for i := 0; i < EvidenceLimitPerKind+1; i++ {
		c.Add(discovery.File{Path: fmt.Sprintf("%03d/package.json", i), Size: 1})
	}
	decls := &declarations.Report{Source: "directory", Status: "complete"}
	report, err := c.Finish(Evidence{Declarations: decls, Lockfiles: &lockfiles.Report{Source: "directory", Status: "complete"}, Records: projectRecords(decls.Projects)})
	if err != nil {
		t.Fatal(err)
	}
	report.ManifestCandidatePopulation.Count--
	if err := ValidateReport(report); err == nil {
		t.Fatal("expected aggregate mismatch rejection")
	}
}

func BenchmarkCollectorAddAndFinish50K(b *testing.B) {
	decls := &declarations.Report{Source: "directory", Status: "complete"}
	locks := &lockfiles.Report{Source: "directory", Status: "complete"}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		c := New("directory", "")
		for n := 0; n < 50_000; n++ {
			name := fmt.Sprintf("src/pkg%05d/file.go", n)
			if n%100 == 0 {
				name = fmt.Sprintf("modules/m%05d/package.json", n)
			}
			c.Add(discovery.File{Path: name, Size: 256})
		}
		if _, err := c.Finish(Evidence{Declarations: decls, Lockfiles: locks}); err != nil {
			b.Fatal(err)
		}
	}
}

func projectRecords(projects []declarations.Project) []declarations.ProjectRecord {
	out := make([]declarations.ProjectRecord, 0, len(projects))
	for _, p := range projects {
		out = append(out, declarations.ProjectRecord{Project: p, Parsed: true, Complete: true})
	}
	return out
}
