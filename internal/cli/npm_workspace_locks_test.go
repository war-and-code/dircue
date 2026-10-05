package cli

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/war-and-code/dircue/pkg/lockfiles"
	"github.com/war-and-code/dircue/pkg/profile"
)

func TestNPMWorkspaceLockFlagIsExplicitAndMatchesAssessment(t *testing.T) {
	root := t.TempDir()
	writeCLILockfileFixture(t, root, map[string]string{
		"package.json":              `{"name":"root","private":true,"workspaces":["packages/app"]}`,
		"package-lock.json":         `{"lockfileVersion":3,"packages":{"":{},"packages/app":{"dependencies":{"left-pad":"1.0.0"}}}}`,
		"packages/app/package.json": cliLockfileManifest,
	})
	t.Setenv("PATH", t.TempDir())
	run := func(args ...string) *profile.Report {
		t.Helper()
		args = append(args, "--source", "directory", "--json", root)
		output, stderr, err := invoke(args...)
		if err != nil || stderr != "" {
			t.Fatalf("%v: err=%v stderr=%q", args, err, stderr)
		}
		decodeLockfilesProfile(t, output)
		var report profile.Report
		if err := json.Unmarshal([]byte(output), &report); err != nil {
			t.Fatal(err)
		}
		return &report
	}
	member := func(report *profile.Report) lockfiles.Context {
		t.Helper()
		if report.Lockfiles == nil {
			t.Fatal("workspace flag did not enable lockfile analysis")
		}
		for _, context := range report.Lockfiles.Contexts {
			if context.ManifestPath == "packages/app/package.json" {
				return context
			}
		}
		t.Fatal("workspace member context missing")
		return lockfiles.Context{}
	}
	for _, args := range [][]string{
		{"analyze", "lockfiles"},
		{"analyze", "lockfiles", "--npm-workspace-locks=false"},
	} {
		if got := member(run(args...)); got.AssociationState != "indeterminate" || len(got.Checks) != 0 {
			t.Fatalf("default semantics unexpectedly selected a workspace lock: %+v", got)
		}
	}
	explicit := run("analyze", "lockfiles", "--npm-workspace-locks")
	got := member(explicit)
	if got.AssociationState != "observed" || got.LockfilePath != "package-lock.json" || len(got.Checks) != 1 || got.Checks[0].Status != "match" {
		t.Fatalf("explicit flag failed to compare member declarations against its root entry: %+v", got)
	}
	combined := run("analyze", "all", "--npm-workspace-locks")
	if combined.Assessment != nil || !reflect.DeepEqual(explicit.Lockfiles, combined.Lockfiles) {
		t.Fatal("workspace flag must imply lockfiles without enabling assessment")
	}
	for _, args := range [][]string{
		{"analyze", "assessment"},
		{"analyze", "all", "--assessment", "--npm-workspace-locks=false"},
	} {
		assessed := run(args...)
		if assessed.Assessment == nil || !reflect.DeepEqual(explicit.Lockfiles, assessed.Lockfiles) {
			t.Fatalf("assessment must apply explicit workspace semantics: %v", args)
		}
	}
}
