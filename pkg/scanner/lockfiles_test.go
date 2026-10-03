package scanner

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/war-and-code/dircue/pkg/lockfiles"
	"github.com/war-and-code/dircue/pkg/profile"
)

const lockfilesNPMManifest = `{"name":"app","dependencies":{"left-pad":"1.0.0"}}`
const lockfilesNPMMatching = `{"lockfileVersion":3,"packages":{"":{"dependencies":{"left-pad":"1.0.0"}}}}`
const lockfilesNPMDifferent = `{"lockfileVersion":3,"packages":{"":{"dependencies":{"left-pad":"2.0.0"}}}}`

func requireLockfilesContext(t *testing.T, report *profile.Report, manifest string) lockfiles.Context {
	t.Helper()
	if report.Lockfiles == nil {
		t.Fatal("lockfile module was not requested")
	}
	for _, got := range report.Lockfiles.Contexts {
		if got.ManifestPath == manifest {
			return got
		}
	}
	t.Fatalf("lockfile context %q not found in %+v", manifest, report.Lockfiles.Contexts)
	return lockfiles.Context{}
}

func writeLockfileFixtures(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for name, content := range files {
		filename := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(filename), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filename, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
}

func TestLockfilesRespectGitSelectionAndPlainDirectoryState(t *testing.T) {
	root, _, selected := gitFixture(t, map[string]string{
		"app/package.json":      lockfilesNPMManifest,
		"app/package-lock.json": lockfilesNPMMatching,
	})
	// Dirty the selected project and add a second project after the selected
	// commit. Git analysis must use neither change; directory analysis must use
	// both.
	writeLockfileFixtures(t, root, map[string]string{
		"app/package.json":      `{"name":"app","dependencies":{"left-pad":"2.0.0"}}`,
		"new/package.json":      lockfilesNPMManifest,
		"new/package-lock.json": lockfilesNPMMatching,
	})
	gitReport, err := Scan(context.Background(), root, Options{Source: "git", Revision: selected.String(), Lockfiles: true, DeclarationsOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	gitContext := requireLockfilesContext(t, gitReport, "app/package.json")
	if gitContext.AssociationState != "observed" || len(gitContext.Checks) != 1 || gitContext.Checks[0].Status != "match" {
		t.Fatalf("Git result used dirty lockfile inputs: %+v", gitContext)
	}
	if len(gitReport.Lockfiles.Contexts) != 1 || gitReport.Lockfiles.Source != "git" || gitReport.Lockfiles.Tree == "" {
		t.Fatalf("Git module escaped the selected tree: %+v", gitReport.Lockfiles)
	}

	directoryReport, err := Scan(context.Background(), root, Options{Source: "directory", Lockfiles: true, DeclarationsOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	if got := requireLockfilesContext(t, directoryReport, "app/package.json"); got.Checks[0].Status != "different" {
		t.Fatalf("directory scan did not read dirty manifest: %+v", got)
	}
	if got := requireLockfilesContext(t, directoryReport, "new/package.json"); got.Checks[0].Status != "match" {
		t.Fatalf("directory scan omitted untracked project: %+v", got)
	}
	if directoryReport.Lockfiles.Source != "directory" || directoryReport.Lockfiles.Tree != "" {
		t.Fatalf("plain directory metadata: %+v", directoryReport.Lockfiles)
	}
}

func TestLockfilesInventoryIncludesNamedDependencyTreesWithoutTreatingThemAsProjects(t *testing.T) {
	root := fixtures(t, map[string]string{
		"app/package.json":                 lockfilesNPMManifest,
		"app/package-lock.json":            lockfilesNPMMatching,
		"node_modules/x/package-lock.json": `{"lockfileVersion":3,"packages":{}}`,
		"vendor/y/packages.lock.json":      `{"version":1,"dependencies":{}}`,
	})
	for _, tc := range []struct {
		summarize  bool
		candidates int
	}{{summarize: false, candidates: 3}, {summarize: true, candidates: 2}} {
		report, err := Scan(context.Background(), root, Options{Source: "directory", Lockfiles: true, DeclarationsOnly: true, SummarizeTrees: tc.summarize})
		if err != nil {
			t.Fatal(err)
		}
		if got := report.Lockfiles.Coverage.LockCandidates; got != tc.candidates {
			t.Errorf("SummarizeTrees=%v lock candidates=%d, want %d", tc.summarize, got, tc.candidates)
		}
		if len(report.Lockfiles.Contexts) != 1 || report.Lockfiles.Contexts[0].ManifestPath != "app/package.json" {
			t.Errorf("dependency-tree metadata became a project: %+v", report.Lockfiles.Contexts)
		}
	}
}

func TestLockfilesSymlinkCandidateIsNotOpened(t *testing.T) {
	root := fixtures(t, map[string]string{"app/package.json": lockfilesNPMManifest, "target.json": lockfilesNPMMatching})
	err := os.Symlink(filepath.Join(root, "target.json"), filepath.Join(root, "app", "package-lock.json"))
	if err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	report, err := Scan(context.Background(), root, Options{Source: "directory", Lockfiles: true, DeclarationsOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	got := requireLockfilesContext(t, report, "app/package.json")
	if got.AssociationState != "indeterminate" || len(got.Checks) != 1 || got.Checks[0].Status != "indeterminate" {
		t.Fatalf("symlink candidate was trusted as regular lockfile content: %+v", got)
	}
	if report.Lockfiles.Coverage.LockfilesRead != 0 {
		t.Fatalf("symlink target was read: %+v", report.Lockfiles.Coverage)
	}
}

func TestLockfilesWorkerAndReadBoundsAreDeterministic(t *testing.T) {
	root := fixtures(t, map[string]string{
		"app/package.json":      `{"dependencies":{"left-pad":"1"}}`,
		"app/package-lock.json": `{"lockfileVersion":3,"packages":{"":{"dependencies":{"left-pad":"1"}}},"padding":"` + string(make([]byte, 256)) + `"}`,
	})
	var reports []*profile.Report
	for _, workers := range []int{1, 8} {
		report, err := Scan(context.Background(), root, Options{Source: "directory", Lockfiles: true, DeclarationsOnly: true, Workers: workers, MaxFileBytes: 64})
		if err != nil {
			t.Fatal(err)
		}
		reports = append(reports, report)
		got := requireLockfilesContext(t, report, "app/package.json")
		if got.Checks[0].Status != "indeterminate" || report.Lockfiles.Coverage.LockfilesRead != 0 {
			t.Fatalf("max-file-bytes was not applied to the selected lockfile: %+v", report.Lockfiles)
		}
	}
	first, _ := json.Marshal(reports[0].Lockfiles)
	second, _ := json.Marshal(reports[1].Lockfiles)
	if string(first) != string(second) {
		t.Fatalf("lockfile report depends on worker count:\n%s\n%s", first, second)
	}

	limited, err := Scan(context.Background(), root, Options{Source: "directory", Lockfiles: true, DeclarationsOnly: true, MaxTreeSize: 1})
	if err != nil || limited.Lockfiles == nil || limited.Lockfiles.Status != "skipped" {
		t.Fatalf("tree-size limit report=%+v err=%v", limited, err)
	}
}

func TestLockfilesReportStaysPartialWhenDeclarationInventoryIsPartial(t *testing.T) {
	root := fixtures(t, map[string]string{
		"app/package.json":      lockfilesNPMManifest,
		"app/package-lock.json": lockfilesNPMMatching,
		"broken/package.json":   `{"dependencies":`,
	})
	report, err := Scan(context.Background(), root, Options{Source: "directory", Lockfiles: true, DeclarationsOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	got := requireLockfilesContext(t, report, "app/package.json")
	if got.Checks[0].Status != "match" || report.Declarations.Status != "partial" || report.Lockfiles.Status != "partial" {
		t.Fatalf("partial declaration inventory was hidden: lockfiles=%+v declarations=%+v", report.Lockfiles, report.Declarations)
	}
}

func TestLockfilesNPMShrinkwrapPrecedesPackageLock(t *testing.T) {
	root := fixtures(t, map[string]string{
		"app/package.json":        lockfilesNPMManifest,
		"app/package-lock.json":   lockfilesNPMDifferent,
		"app/npm-shrinkwrap.json": lockfilesNPMMatching,
	})
	report, err := Scan(context.Background(), root, Options{Source: "directory", Lockfiles: true, DeclarationsOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	got := requireLockfilesContext(t, report, "app/package.json")
	if got.AssociationState != "observed" || got.LockfilePath != "app/npm-shrinkwrap.json" || len(got.Checks) != 1 || got.Checks[0].Status != "match" {
		t.Fatalf("npm-shrinkwrap did not take precedence: %+v", got)
	}
}

func TestLockfilesReportIsStableAcrossRepeatedDirectoryScans(t *testing.T) {
	root := fixtures(t, map[string]string{"app/package.json": lockfilesNPMManifest, "app/package-lock.json": lockfilesNPMMatching})
	first, err := Scan(context.Background(), root, Options{Source: "directory", Lockfiles: true, DeclarationsOnly: true, Workers: 1})
	if err != nil {
		t.Fatal(err)
	}
	second, err := Scan(context.Background(), root, Options{Source: "directory", Lockfiles: true, DeclarationsOnly: true, Workers: 4})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first.Lockfiles, second.Lockfiles) {
		t.Fatalf("repeated reports differ:\n%+v\n%+v", first.Lockfiles, second.Lockfiles)
	}
}
