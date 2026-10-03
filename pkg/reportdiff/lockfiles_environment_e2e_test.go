package reportdiff_test

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/war-and-code/dircue/internal/cli"
	"github.com/war-and-code/dircue/pkg/profile"
	"github.com/war-and-code/dircue/pkg/reportdiff"
)

func analyzeSavedProfile(t *testing.T, root string) []byte {
	t.Helper()
	var out, diagnostics bytes.Buffer
	err := cli.Execute(context.Background(), []string{"analyze", "all", "--source", "directory", "--discovery", "--environments", "--lockfiles", "--json", root}, &out, &diagnostics)
	if err != nil {
		t.Fatalf("analyze all failed: %v; stderr=%s", err, diagnostics.String())
	}
	return out.Bytes()
}

func TestCLIProfilesLoadAndCompareEnvironmentAndLockfileEvidence(t *testing.T) {
	root := t.TempDir()
	write := func(name, contents string) {
		t.Helper()
		full := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(contents), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("package.json", `{"dependencies":{"left-pad":"1.0.0"}}`)
	write("package-lock.json", `{"name":"fixture","lockfileVersion":3,"packages":{"":{"dependencies":{"left-pad":"1.0.0"}},"node_modules/left-pad":{"version":"1.0.0"}}}`)
	write(".python-version", "3.11.9\n")
	baseBytes := analyzeSavedProfile(t, root)
	base, err := reportdiff.Load(bytes.NewReader(baseBytes))
	if err != nil {
		t.Fatalf("load CLI profile: %v", err)
	}
	write("package-lock.json", `{"name":"fixture","lockfileVersion":3,"packages":{"":{"dependencies":{"left-pad":"2.0.0"}},"node_modules/left-pad":{"version":"2.0.0"}}}`)
	write(".python-version", "3.12.1\n")
	headBytes := analyzeSavedProfile(t, root)
	head, err := reportdiff.Load(bytes.NewReader(headBytes))
	if err != nil {
		t.Fatalf("load second CLI profile: %v", err)
	}
	comparison, err := reportdiff.Compare(base, head)
	if err != nil {
		t.Fatalf("compare saved CLI profiles: %v", err)
	}
	var environmentsChanged, lockfilesChanged bool
	for _, module := range comparison.Modules {
		switch module.Name {
		case "environments":
			environmentsChanged = module.Counts.Changed > 0
		case "lockfiles":
			lockfilesChanged = module.Counts.Changed > 0
		}
	}
	if !environmentsChanged || !lockfilesChanged {
		t.Fatalf("CLI reports lost real changes: env=%t lockfiles=%t modules=%+v", environmentsChanged, lockfilesChanged, comparison.Modules)
	}
}

func TestCLILockfileReportRetainsShrinkwrapPrecedenceWithSiblingLock(t *testing.T) {
	root := t.TempDir()
	for name, contents := range map[string]string{
		"package.json":        `{"dependencies":{"left-pad":"1.0.0"}}`,
		"npm-shrinkwrap.json": `{"name":"fixture","lockfileVersion":3,"packages":{"":{"dependencies":{"left-pad":"1.0.0"}},"node_modules/left-pad":{"version":"1.0.0"}}}`,
		"package-lock.json":   `not-json`,
	} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(contents), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	var output, diagnostics bytes.Buffer
	err := cli.Execute(context.Background(), []string{"analyze", "all", "--source", "directory", "--lockfiles", "--json", root}, &output, &diagnostics)
	if err != nil {
		t.Fatalf("analyze all failed: %v; stderr=%s", err, diagnostics.String())
	}
	var saved profile.Report
	if err := json.Unmarshal(output.Bytes(), &saved); err != nil {
		t.Fatal(err)
	}
	if saved.Lockfiles == nil || saved.Lockfiles.Status != "complete" || saved.Lockfiles.Coverage.LockCandidates != 2 || saved.Lockfiles.Coverage.LockfilesRead != 1 || len(saved.Lockfiles.Contexts) != 1 || saved.Lockfiles.Contexts[0].LockfilePath != "npm-shrinkwrap.json" {
		t.Fatalf("CLI did not preserve selected shrinkwrap scope: %+v", saved.Lockfiles)
	}
	if _, err := reportdiff.Load(bytes.NewReader(output.Bytes())); err != nil {
		t.Fatalf("reportdiff rejected valid scoped lockfile report: %v", err)
	}
}
