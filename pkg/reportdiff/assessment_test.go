package reportdiff_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	assessmentpkg "github.com/war-and-code/dircue/pkg/assessment"
	"github.com/war-and-code/dircue/pkg/declarations"
	"github.com/war-and-code/dircue/pkg/discovery"
	"github.com/war-and-code/dircue/pkg/profile"
	"github.com/war-and-code/dircue/pkg/reportdiff"
	"github.com/war-and-code/dircue/pkg/scanner"
)

func assessmentProfile(t *testing.T, files map[string]string) profile.Report {
	t.Helper()
	root := t.TempDir()
	for name, content := range files {
		filename := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(filename), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filename, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	p, err := scanner.Scan(context.Background(), root, scanner.Options{Source: "directory", Assessment: true})
	if err != nil {
		t.Fatal(err)
	}
	return *p
}

func TestAssessmentComparisonUsesAggregateCountsNotSamples(t *testing.T) {
	a := assessmentProfile(t, map[string]string{"app/package.json": `{"name":"a"}`, "data.txt": "a"})
	b := assessmentProfile(t, map[string]string{"app/package.json": `{"name":"a"}`, "data.txt": "ab"})
	m := reviewModule(t, reviewCompare(t, a, b), "assessment")
	if m.Counts.Changed != 1 || m.Counts.Added != 0 || m.Counts.Removed != 0 || m.Status != "changed" {
		t.Fatalf("aggregate byte change: %+v", m)
	}
	// Dropping retained examples while increasing their explicit omission count
	// must not become removed content in an aggregate comparison.
	b = a
	raw, _ := json.Marshal(a.Assessment)
	b.Assessment = nil
	if err := json.Unmarshal(raw, &b.Assessment); err != nil {
		t.Fatal(err)
	}
	b.Assessment.ProjectRootEvidence = []string{}
	b.Assessment.OmittedProjectRootEvidence = b.Assessment.ProjectRoots.Count
	b.Assessment.CandidateEvidence = b.Assessment.CandidateEvidence[:0]
	for _, c := range b.Assessment.FilenameCandidates {
		b.Assessment.OmittedCandidateEvidence[c.Kind] = c.Files
	}
	m = reviewModule(t, reviewCompare(t, a, b), "assessment")
	if m.Status != "unchanged" || len(m.Changes) != 0 {
		t.Fatalf("samples masqueraded as aggregates: %+v", m)
	}
}

func TestAssessmentComparisonIncludesStructuralEntryTotalsWithoutProjectChanges(t *testing.T) {
	a := assessmentProfile(t, map[string]string{"app/package.json": `{"name":"a"}`})
	b := cloneAssessmentProfile(t, a)
	if a.Assessment.Projects.Count != b.Assessment.Projects.Count {
		t.Fatal("test setup changed the legacy project count")
	}
	entry := assessmentpkg.StructureEntryPoint{ProjectID: "app/package.json", EvidencePath: "app/server.js", Ecosystem: "npm", Role: "primary", Kind: "node-entry", Name: "server", Basis: "declaration", State: "declared"}
	assessmentpkg.SetStructureEntryPoints(b.Assessment, []assessmentpkg.StructureEntryPoint{entry})
	if err := assessmentpkg.ValidateReport(b.Assessment); err != nil {
		t.Fatalf("mutated structural report invalid: %v", err)
	}
	m := reviewModule(t, reviewCompare(t, a, b), "assessment")
	if m.Status != "changed" || m.Counts.Changed == 0 {
		t.Fatalf("new interface did not appear in the structural assessment diff: %+v", m)
	}
	if got := changeByID(m, "structure:entry_point_total"); got == nil || got.Status != "changed" {
		t.Fatalf("entry-point aggregate change missing: %+v", m.Changes)
	}
	if changeByID(m, "structure:population:parsed_projects:npm:primary") != nil {
		t.Fatalf("same project population changed when an interface was added: %+v", m.Changes)
	}
}

func TestAssessmentComparisonCapturesQualifiedReferenceAggregateChanges(t *testing.T) {
	a := assessmentProfile(t, map[string]string{"app/package.json": `{"name":"a"}`})
	b := cloneAssessmentProfile(t, a)
	d := &b.Assessment.Structure.Dependencies
	d.QualifiedReferences = []assessmentpkg.StructureQualifiedCount{{Ecosystem: "npm", Kind: "npm-local-dependency", State: "missing", Resolution: "missing", Count: 1}}
	d.QualifiedReferenceCount = 1
	d.QualifiedGroupCount = 1
	d.OmittedQualified = 0
	d.OmittedQualifiedReferenceCount = 0
	m := reviewModule(t, reviewCompare(t, a, b), "assessment")
	if got := changeByID(m, "structure:qualified_reference_total"); got == nil || got.Status != "changed" {
		t.Fatalf("qualified-reference total change missing: %+v", m.Changes)
	}
}

func TestAssessmentComparisonReportsScopedCoverageChanges(t *testing.T) {
	a := assessmentProfile(t, map[string]string{"app/package.json": `{"name":"a"}`})
	b := cloneAssessmentProfile(t, a)
	for i := range b.Assessment.Structure.Coverage {
		row := &b.Assessment.Structure.Coverage[i]
		if row.Scope == "project_dependencies" && row.Ecosystem == "npm" {
			row.Status = "partial"
			row.Reasons = []string{"selected_dependency_observations_omitted"}
		}
	}
	m := reviewModule(t, reviewCompare(t, a, b), "assessment")
	if got := changeByID(m, "structure:coverage:20:project_dependencies3:npm"); got == nil || got.Status != "changed" {
		t.Fatalf("scoped coverage qualification change missing: %+v", m.Changes)
	}
	if m.Compatibility == "incomparable" {
		t.Fatalf("partial structural coverage made the whole assessment incomparable: %+v", m)
	}
}

func TestAssessmentSampleCapsRemainMetadataAndDoNotCreateRemovals(t *testing.T) {
	a := assessmentProfile(t, map[string]string{"app/package.json": `{"name":"a"}`})
	b := cloneAssessmentProfile(t, a)
	makeEntries := func(prefix string) []assessmentpkg.StructureEntryPoint {
		entries := make([]assessmentpkg.StructureEntryPoint, 513)
		for i := range entries {
			name := fmt.Sprintf("%s-%03d", prefix, i)
			entries[i] = assessmentpkg.StructureEntryPoint{ProjectID: "app/package.json", EvidencePath: "app/server.js", Ecosystem: "npm", Role: "primary", Kind: "node-entry", Name: name, Basis: "declaration", State: "declared"}
		}
		return entries
	}
	assessmentpkg.SetStructureEntryPoints(a.Assessment, makeEntries("base"))
	assessmentpkg.SetStructureEntryPoints(b.Assessment, makeEntries("head"))
	if err := assessmentpkg.ValidateReport(a.Assessment); err != nil {
		t.Fatalf("base capped structural report invalid: %v", err)
	}
	if err := assessmentpkg.ValidateReport(b.Assessment); err != nil {
		t.Fatalf("head capped structural report invalid: %v", err)
	}
	if a.Assessment.Structure.EntryPointCount != b.Assessment.Structure.EntryPointCount || a.Assessment.Structure.OmittedEntryPoints == 0 || b.Assessment.Structure.OmittedEntryPoints == 0 {
		t.Fatal("test setup did not hold totals equal across bounded samples")
	}
	m := reviewModule(t, reviewCompare(t, a, b), "assessment")
	if m.Status != "unchanged" || m.Counts.Removed != 0 || m.Counts.Added != 0 || m.Counts.Changed != 0 {
		t.Fatalf("bounded sample changes were treated as aggregate facts: %+v", m)
	}
	if len(m.Metadata) == 0 {
		t.Fatal("bounded structural samples were not retained as comparison metadata")
	}
}

func TestAssessmentLegacyVersionStructuralComparabilityIsQualified(t *testing.T) {
	legacy := assessmentProfile(t, map[string]string{"app/package.json": `{"name":"a"}`})
	legacy.Assessment.Version = assessmentpkg.LegacyVersion
	legacy.Assessment.Structure = nil
	legacy.SchemaVersion = "1.9.0"
	if err := assessmentpkg.ValidateReport(legacy.Assessment); err != nil {
		t.Fatalf("legacy assessment report invalid: %v", err)
	}
	current := assessmentProfile(t, map[string]string{"app/package.json": `{"name":"a"}`})
	m := reviewModule(t, reviewCompare(t, legacy, current), "assessment")
	if m.Compatibility != "incomparable" || m.Counts.Removed != 0 {
		t.Fatalf("assessment policy version change was treated as comparable: %+v", m)
	}
	if !strings.Contains(strings.Join(m.Reasons, " "), "structural_assessment_not_measured") {
		t.Fatalf("legacy structural measurement limitation missing: %+v", m.Reasons)
	}
}

func cloneAssessmentProfile(t *testing.T, in profile.Report) profile.Report {
	t.Helper()
	raw, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	var out profile.Report
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func changeByID(m reportdiff.Module, id string) *reportdiff.Change {
	for i := range m.Changes {
		if m.Changes[i].ID == id {
			return &m.Changes[i]
		}
	}
	return nil
}

func TestAssessmentComparisonDoesNotClaimRemovedFromMissingModule(t *testing.T) {
	a := assessmentProfile(t, map[string]string{"package.json": `{"name":"a"}`})
	b := a
	b.SchemaVersion = profile.LockfilesSchemaVersion
	b.Assessment = nil
	m := reviewModule(t, reviewCompare(t, a, b), "assessment")
	if m.Counts.Removed != 0 || m.Status != "unavailable" || m.Compatibility != "unavailable" {
		t.Fatalf("missing assessment claimed removal: %+v", m)
	}
}

func TestAssessmentLoadRejectsContradictoryNativeMeasurements(t *testing.T) {
	for name, mutate := range map[string]func(*profile.Report){
		"wrong source":                     func(p *profile.Report) { p.Assessment.Source.Mode = "git" },
		"invented files":                   func(p *profile.Report) { p.Assessment.Inventory.Files.Count++ },
		"invented bytes":                   func(p *profile.Report) { p.Assessment.Inventory.Bytes.Count++ },
		"bad state partition":              func(p *profile.Report) { p.Assessment.LockfilesOverall.Unknown.Count++ },
		"false completeness":               func(p *profile.Report) { p.Assessment.Inventory.Files.Reasons = []string{"source_skipped"} },
		"different declaration population": func(p *profile.Report) { p.Declarations.Coverage.ManifestCandidates++ },
	} {
		t.Run(name, func(t *testing.T) {
			p := assessmentProfile(t, map[string]string{"package.json": `{"name":"a"}`})
			mutate(&p)
			raw, err := json.Marshal(p)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := reportdiff.Load(bytes.NewReader(raw)); err == nil {
				t.Fatal("contradictory native measurement accepted")
			}
		})
	}
}

func TestAssessmentLoadRejectsFictitiousStructuralComponentAgainstFullDeclarations(t *testing.T) {
	p := assessmentProfile(t, map[string]string{"app/package.json": `{"name":"a"}`})
	if p.Declarations.Status != "complete" || p.Declarations.Coverage.OmittedFiles != 0 || len(p.Declarations.Diagnostics) != 0 || len(p.Declarations.Projects) != 1 {
		t.Fatalf("test requires a fully retained declaration population: %+v", p.Declarations.Coverage)
	}
	c := &p.Assessment.Structure.Dependencies.Components[0]
	c.ID = "package.json"
	c.Projects = []string{"package.json"}
	if err := assessmentpkg.ValidateReport(p.Assessment); err != nil {
		t.Fatalf("fictitious component should pass native aggregate-only validator before companion reconciliation: %v", err)
	}
	raw, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reportdiff.Load(bytes.NewReader(raw)); err == nil {
		t.Fatal("component ID absent from fully retained declarations was accepted")
	}
}

func TestAssessmentStructureIDJoinExcludesSolutionRecords(t *testing.T) {
	p := assessmentProfile(t, map[string]string{
		"app/App.csproj": "<Project><PropertyGroup><TargetFramework>net8.0</TargetFramework></PropertyGroup></Project>\n",
		"Product.sln":    "Microsoft Visual Studio Solution File, Format Version 12.00\n# Visual Studio Version 17\n",
	})
	if p.Assessment.Structure.Dependencies.Projects.Count != 1 || len(p.Declarations.Projects) != 2 {
		t.Fatalf("fixture must retain one project vertex and one solution record: vertices=%+v records=%+v",
			p.Assessment.Structure.Dependencies.Projects, p.Declarations.Projects)
	}
	loadAssessmentProfile(t, p)
}

func TestAssessmentLoadReconcilesLongManifestPathOmission(t *testing.T) {
	// Git tree paths can exceed the declaration collector's string bound even
	// though each path component is small enough for a normal filesystem.
	name := strings.Repeat("d/", 4100) + "package.json"
	p := collectorAssessmentProfile(t, name)
	if p.Assessment.ManifestCandidatePopulation.Count != 1 || p.Assessment.ManifestCandidatePopulation.Completeness != "complete" {
		t.Fatalf("assessment did not retain its exact filename aggregate: %+v", p.Assessment.ManifestCandidatePopulation)
	}
	if p.Declarations.Coverage.ManifestCandidates != 0 || p.Declarations.Coverage.OmittedFiles == 0 {
		t.Fatalf("declaration omission boundary changed: %+v", p.Declarations.Coverage)
	}
	loadAssessmentProfile(t, p)
}

func TestAssessmentLoadReconcilesInvalidUTF8ManifestPathOmission(t *testing.T) {
	name := "bad\xff/package.json"
	p := collectorAssessmentProfile(t, name)
	if p.Assessment.ManifestCandidatePopulation.Count != 0 || p.Assessment.ManifestCandidatePopulation.Completeness != "lower_bound" {
		t.Fatalf("invalid UTF-8 path did not lower-bound assessment population: %+v", p.Assessment.ManifestCandidatePopulation)
	}
	if p.Declarations.Coverage.ManifestCandidates != 1 || p.Declarations.Coverage.OmittedFiles == 0 {
		t.Fatalf("declaration omission boundary changed: %+v", p.Declarations.Coverage)
	}
	loadAssessmentProfile(t, p)

	// A lower-bound metric caused by another omission does not explain why
	// declarations counted one more manifest than assessment did.
	p.Assessment.ManifestCandidatePopulation.Reasons = []string{"invalid_selected_path"}
	data, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reportdiff.Load(bytes.NewReader(data)); err == nil {
		t.Fatal("reverse discrepancy without invalid_utf8_path support accepted")
	}
}

func TestAssessmentLoadRejectsManifestPopulationMismatchWithoutOmissionSupport(t *testing.T) {
	p := assessmentProfile(t, map[string]string{"package.json": `{"name":"a"}`})
	// A claimed declaration omission cannot explain an unbounded discrepancy.
	p.Declarations.Coverage.ManifestCandidates = 3
	p.Declarations.Coverage.OmittedFiles = 1
	data, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reportdiff.Load(bytes.NewReader(data)); err == nil {
		t.Fatal("unsupported manifest population mismatch accepted")
	}
}

func collectorAssessmentProfile(t *testing.T, name string) profile.Report {
	t.Helper()
	p := assessmentProfile(t, map[string]string{})
	file := discovery.File{Path: name, Size: 1}

	discoveryCollector := discovery.New("directory", "", 100000)
	discoveryCollector.Add(file)
	p.Discovery = discoveryCollector.Finish()

	declarationCollector := declarations.New("directory", "", 0)
	declarationCollector.Add(name, &declarations.Candidate{Path: name, Size: 1})
	declarationReport, err := declarationCollector.Finish(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	p.Declarations = declarationReport

	assessmentCollector := assessmentpkg.New("directory", "")
	assessmentCollector.Add(file)
	assessmentReport, err := assessmentCollector.Finish(assessmentpkg.Evidence{Declarations: declarationReport, Lockfiles: p.Lockfiles})
	if err != nil {
		t.Fatal(err)
	}
	p.Assessment = assessmentReport
	return p
}

func loadAssessmentProfile(t *testing.T, p profile.Report) {
	t.Helper()
	data, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reportdiff.Load(bytes.NewReader(data)); err != nil {
		t.Fatalf("collector-produced population discrepancy rejected: %v", err)
	}
}
