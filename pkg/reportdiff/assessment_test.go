package reportdiff_test

import (
	"bytes"
	"context"
	"encoding/json"
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
