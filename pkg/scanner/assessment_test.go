package scanner

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/war-and-code/dircue/pkg/assessment"
	"github.com/war-and-code/dircue/pkg/lockfiles"
	"github.com/war-and-code/dircue/pkg/profile"
	"github.com/war-and-code/dircue/schema"
)

func scanAssessment(t *testing.T, root string, opts Options) *profile.Report {
	t.Helper()
	opts.Assessment = true
	report, err := Scan(context.Background(), root, opts)
	if err != nil {
		t.Fatal(err)
	}
	if report.Assessment == nil || report.SchemaVersion != profile.AssessmentSchemaVersion {
		t.Fatalf("assessment output missing or wrong schema version: version=%s report=%+v", report.SchemaVersion, report.Assessment)
	}
	if err := assessment.ValidateReport(report.Assessment); err != nil {
		t.Fatalf("native assessment validation: %v", err)
	}
	encoded, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	var value any
	if err := json.Unmarshal(encoded, &value); err != nil {
		t.Fatal(err)
	}
	if err := schema.ValidateProfile(value); err != nil {
		t.Fatalf("bundled profile/assessment schema validation: %v", err)
	}
	return report
}

func assessmentLockState(t *testing.T, report *profile.Report, manifest string) string {
	t.Helper()
	if report.Lockfiles == nil {
		t.Fatal("assessment did not include lockfile observations")
	}
	for _, c := range report.Lockfiles.Contexts {
		if c.ManifestPath == manifest {
			return c.AssociationState
		}
	}
	t.Fatalf("lockfile context %q absent: %+v", manifest, report.Lockfiles.Contexts)
	return ""
}

func metricReason(metric assessment.Metric, reason string) bool {
	return metric.Completeness == "lower_bound" && slicesContains(metric.Reasons, reason)
}

func TestAssessmentExactInventoryLanguagesAndSharedWorkspaceLocks(t *testing.T) {
	files := map[string]string{
		"package.json":              `{"name":"repo","workspaces":["packages/*"]}`,
		"package-lock.json":         `{"lockfileVersion":3,"packages":{"":{"dependencies":{}},"packages/app":{"dependencies":{"left-pad":"1.0.0"}}}}`,
		"packages/app/package.json": `{"name":"app","dependencies":{"left-pad":"1.0.0"}}`,
		"main.go":                   goSource,
	}
	root := fixtures(t, files)
	report := scanAssessment(t, root, Options{Source: "directory", Workers: 2})
	var expectedBytes int64
	for _, content := range files {
		expectedBytes += int64(len(content))
	}
	a := report.Assessment
	if a.Source.Mode != "directory" || a.Source.Tree != "" || a.Source.Consistency != "live_directory_metadata" {
		t.Fatalf("directory identity: %+v", a.Source)
	}
	if a.Inventory.Files.Count != int64(len(files)) || a.Inventory.Bytes.Count != expectedBytes || a.Inventory.Files.Completeness != "complete" || a.Inventory.Bytes.Completeness != "complete" {
		t.Fatalf("inventory is not exact: %+v", a.Inventory)
	}
	if a.Projects.Count != 2 || a.ProjectRoots.Count != 2 || a.WorkspaceMembership.Count != 1 {
		t.Fatalf("project/workspace counts: projects=%+v roots=%+v workspace=%+v", a.Projects, a.ProjectRoots, a.WorkspaceMembership)
	}
	if state := assessmentLockState(t, report, "packages/app/package.json"); state != "observed" {
		t.Fatalf("assessment did not associate the selected shared lock: %s", state)
	}
	if report.Lockfiles == nil {
		t.Fatal("lockfile report missing")
	}
	for _, c := range report.Lockfiles.Contexts {
		if c.ManifestPath == "packages/app/package.json" && (len(c.Checks) != 1 || c.Checks[0].Status != "match" || len(c.Boundaries) != 1 || c.Boundaries[0].Reason != "npm-workspace-lock-ownership-observed") {
			t.Fatalf("member lock check did not use and disclose its own descriptor: %+v", c)
		}
	}
	legacyLanguages, err := Scan(context.Background(), root, Options{Source: "directory", Workers: 2})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(report.Languages, legacyLanguages.Languages) || len(report.Languages) != 1 || report.Languages[0].Name != "Go" || report.Languages[0].Bytes != int64(len(goSource)) {
		t.Fatalf("assessment changed language analysis: %+v", report.Languages)
	}

	legacy, err := Scan(context.Background(), root, Options{Source: "directory", Lockfiles: true, DeclarationsOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	if legacy.Assessment != nil || assessmentLockState(t, legacy, "packages/app/package.json") != "indeterminate" {
		t.Fatalf("legacy lockfiles mode inherited the new opt-in association: %+v", legacy)
	}
}

func TestAssessmentPreservesLiteralPOSIXBackslashProjectPath(t *testing.T) {
	manifest := `a\b/package.json`
	root := fixtures(t, map[string]string{
		manifest:                `{"name":"backslash-app","dependencies":{"left-pad":"1.0.0"}}`,
		`a\b/package-lock.json`: `{"lockfileVersion":3,"packages":{"":{"dependencies":{"left-pad":"1.0.0"}}}}`,
		"a/b/package.json":      `{"name":"slash-app","dependencies":{"other":"2.0.0"}}`,
		"a/b/package-lock.json": `{"lockfileVersion":3,"packages":{"":{"dependencies":{"other":"2.0.0"}}}}`,
	})
	report := scanAssessment(t, root, Options{Source: "directory"})
	if got := assessmentLockState(t, report, manifest); got != "observed" {
		t.Fatalf("selected literal-backslash project lost its own lock association: %s", got)
	}
	if got := assessmentLockState(t, report, "a/b/package.json"); got != "observed" {
		t.Fatalf("slash-separated sibling was poisoned by literal-backslash path: %s", got)
	}
	for _, expected := range []struct{ manifest, lock string }{{manifest, `a\b/package-lock.json`}, {"a/b/package.json", "a/b/package-lock.json"}} {
		found := false
		for _, ctx := range report.Lockfiles.Contexts {
			if ctx.ManifestPath == expected.manifest {
				found = ctx.ProjectID == expected.manifest && ctx.LockfilePath == expected.lock
				break
			}
		}
		if !found {
			t.Fatalf("selected path identity lost: manifest=%q lock=%q contexts=%+v", expected.manifest, expected.lock, report.Lockfiles.Contexts)
		}
	}
	mutated := *report
	mutatedLockfiles := *report.Lockfiles
	mutatedLockfiles.Semantics = append([]string(nil), mutatedLockfiles.Semantics[:2]...)
	mutated.Lockfiles = &mutatedLockfiles
	if err := lockfiles.ValidateReport(&mutatedLockfiles); err == nil {
		t.Fatal("native lockfile validator accepted literal POSIX paths without the opt-in semantics marker")
	}
	encoded, err := json.Marshal(&mutated)
	if err != nil {
		t.Fatal(err)
	}
	var value any
	if err := json.Unmarshal(encoded, &value); err != nil {
		t.Fatal(err)
	}
	if err := schema.ValidateProfile(value); err == nil {
		t.Fatal("JSON Schema accepted literal POSIX paths without the opt-in semantics marker")
	}
}

func TestAssessmentCountsResolvedNPMWorkspaceDependencySeparatelyFromMembership(t *testing.T) {
	root := fixtures(t, map[string]string{
		"package.json":              `{"name":"repo","workspaces":["packages/*"]}`,
		"packages/app/package.json": `{"name":"app","dependencies":{"lib":"workspace:*"}}`,
		"packages/lib/package.json": `{"name":"lib"}`,
	})
	report := scanAssessment(t, root, Options{Source: "directory"})
	if report.Assessment.WorkspaceMembership.Count != 2 {
		t.Fatalf("workspace members were not counted: %+v", report.Assessment.WorkspaceMembership)
	}
	if report.Assessment.LocalDependencies.Count != 1 {
		t.Fatalf("resolved workspace dependency was not counted as a local dependency: %+v", report.Assessment.LocalDependencies)
	}
}

func TestAssessmentGitUsesSelectedTreeAndDirectoryUsesLiveFiles(t *testing.T) {
	committed := map[string]string{
		"main.go":                   goSource,
		"package.json":              `{"name":"repo","workspaces":["packages/*"]}`,
		"package-lock.json":         `{"lockfileVersion":3,"packages":{"":{"dependencies":{}},"packages/app":{"dependencies":{"left-pad":"1.0.0"}}}}`,
		"packages/app/package.json": `{"name":"app","dependencies":{"left-pad":"1.0.0"}}`,
	}
	root, _, selected := gitFixture(t, committed)
	writeLockfileFixtures(t, root, map[string]string{
		"packages/app/package.json": `{"name":"app","dependencies":{"left-pad":"2.0.0"}}`,
		"untracked/package.json":    `{"name":"new"}`,
		"untracked.txt":             "untracked\n",
	})
	gitReport := scanAssessment(t, root, Options{Source: "git", Revision: selected.String()})
	if gitReport.Assessment.Source.Mode != "git" || len(gitReport.Assessment.Source.Tree) != 40 || gitReport.Assessment.Inventory.Files.Count != 4 {
		t.Fatalf("Git assessment did not describe committed tree only: %+v", gitReport.Assessment)
	}
	if assessmentLockState(t, gitReport, "packages/app/package.json") != "observed" {
		t.Fatal("selected tree lost the shared workspace lock association")
	}
	directoryReport := scanAssessment(t, root, Options{Source: "directory"})
	if directoryReport.Assessment.Source.Mode != "directory" || directoryReport.Assessment.Inventory.Files.Count != 6 || directoryReport.Assessment.Projects.Count != 3 {
		t.Fatalf("directory assessment did not include dirty and untracked files: %+v", directoryReport.Assessment)
	}
	var gitBytes int64
	for _, value := range committed {
		gitBytes += int64(len(value))
	}
	if gitReport.Assessment.Inventory.Bytes.Count != gitBytes || directoryReport.Assessment.Inventory.Bytes.Count <= gitBytes {
		t.Fatalf("logical byte totals did not follow source selection: git=%d directory=%d committed=%d", gitReport.Assessment.Inventory.Bytes.Count, directoryReport.Assessment.Inventory.Bytes.Count, gitBytes)
	}
}

func TestAssessmentExactCountsSurviveIndependentEvidenceSampleCap(t *testing.T) {
	files := make(map[string]string, 300)
	for i := 0; i < 300; i++ {
		files[fmt.Sprintf("packages/p%03d/package.json", i)] = `{"name":"p"}`
	}
	root := fixtures(t, files)
	report := scanAssessment(t, root, Options{Source: "directory", Workers: 4})
	a := report.Assessment
	if a.Inventory.Files.Count != 300 || a.Projects.Count != 300 || a.ProjectRoots.Count != 300 || a.ManifestCandidatePopulation.Count != 300 {
		t.Fatalf("sample cap changed exact counts: inventory=%+v projects=%+v roots=%+v manifests=%+v", a.Inventory.Files, a.Projects, a.ProjectRoots, a.ManifestCandidatePopulation)
	}
	if a.Inventory.Files.Completeness != "complete" || a.Projects.Completeness != "complete" || len(a.CandidateEvidence) != assessment.EvidenceLimitPerKind || a.OmittedCandidateEvidence["manifest"] != 300-assessment.EvidenceLimitPerKind {
		t.Fatalf("sample cap incorrectly changed completeness or omission counts: evidence=%d omitted=%v", len(a.CandidateEvidence), a.OmittedCandidateEvidence)
	}
}

func TestAssessmentPerMetricLowerBoundsForContentSummaryAndTreeLimits(t *testing.T) {
	t.Run("content cap", func(t *testing.T) {
		root := fixtures(t, map[string]string{
			"package.json":      `{"name":"a","description":"` + strings.Repeat("x", 100) + `"}`,
			"large.go":          strings.Repeat("package main\n", 20),
			"package-lock.json": `{"lockfileVersion":3,"packages":{"":{"dependencies":{}}}}`,
		})
		report := scanAssessment(t, root, Options{Source: "directory", MaxFileBytes: 32})
		a := report.Assessment
		if a.Inventory.Files.Completeness != "complete" || a.Inventory.Bytes.Completeness != "complete" || a.ManifestCandidatePopulation.Completeness != "complete" {
			t.Fatalf("content cap hid known filesystem metadata: inventory=%+v manifests=%+v", a.Inventory, a.ManifestCandidatePopulation)
		}
		if !metricReason(a.Projects, "manifest_candidates_unparsed") || !metricReason(a.ProjectRoots, "manifest_candidates_unparsed") {
			t.Fatalf("skipped declaration content did not qualify project metrics: projects=%+v roots=%+v", a.Projects, a.ProjectRoots)
		}
	})
	t.Run("summarized tree", func(t *testing.T) {
		root := fixtures(t, map[string]string{"main.go": goSource, "node_modules/pkg/index.js": "module.exports = 1\n"})
		report := scanAssessment(t, root, Options{Source: "directory", SummarizeTrees: true})
		a := report.Assessment
		if !metricReason(a.Inventory.Files, "summarized_tree") || !metricReason(a.Inventory.Bytes, "summarized_tree") {
			t.Fatalf("summarized subtree did not lower-bound inventory: %+v", a.Inventory)
		}
		if a.Inventory.Files.Count != 1 || a.Inventory.Bytes.Count != int64(len(goSource)) {
			t.Fatalf("tree summary admitted unknown inner files into exact counters: %+v", a.Inventory)
		}
	})
	t.Run("tree size limit", func(t *testing.T) {
		root := fixtures(t, map[string]string{"main.go": goSource, "package.json": `{"name":"a"}`})
		report := scanAssessment(t, root, Options{Source: "directory", MaxTreeSize: 1})
		a := report.Assessment
		if !metricReason(a.Inventory.Files, "tree_size_limit") || !metricReason(a.Inventory.Bytes, "tree_size_limit") || !metricReason(a.Projects, "declarations_skipped") || !metricReason(a.LockfilesOverall.Unknown, "declarations_skipped") {
			t.Fatalf("tree-size omissions were not qualified per metric: inventory=%+v projects=%+v locks=%+v", a.Inventory, a.Projects, a.LockfilesOverall)
		}
	})
}

func TestAssessmentEmptyDirectoryAndSymlinkLockDoNotInventEvidence(t *testing.T) {
	t.Run("empty directory", func(t *testing.T) {
		report := scanAssessment(t, t.TempDir(), Options{Source: "directory"})
		if report.Assessment.Inventory.Files.Count != 0 || report.Assessment.Inventory.Bytes.Count != 0 || report.Assessment.Inventory.Files.Completeness != "complete" || report.SchemaVersion != "1.9.0" {
			t.Fatalf("empty directory contract: %+v", report.Assessment)
		}
	})
	t.Run("symlink lock", func(t *testing.T) {
		root := fixtures(t, map[string]string{"app/package.json": lockfilesNPMManifest, "target.json": lockfilesNPMMatching})
		if err := os.Symlink(filepath.Join(root, "target.json"), filepath.Join(root, "app", "package-lock.json")); err != nil {
			t.Skipf("symlink unavailable: %v", err)
		}
		report := scanAssessment(t, root, Options{Source: "directory"})
		if got := assessmentLockState(t, report, "app/package.json"); got != "indeterminate" || report.Lockfiles.Coverage.LockfilesRead != 0 {
			t.Fatalf("assessment read or trusted symlink lock: state=%s coverage=%+v", got, report.Lockfiles.Coverage)
		}
	})
}

func slicesContains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
