package scanner

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/war-and-code/dircue/pkg/environments"
	"github.com/war-and-code/dircue/pkg/lockfiles"
	"github.com/war-and-code/dircue/pkg/profile"
)

// Exhaust the selected-path budgets without creating hundreds of thousands
// of files. The scanner must retain bounded state and disclose skipped work,
// rather than aborting unrelated modules or asserting that a lockfile is absent.
func TestContextInventoryLimitsDiscloseSkippedWork(t *testing.T) {
	t.Run("lockfiles", func(t *testing.T) {
		a := newLockfileAccumulator(Options{Lockfiles: true})
		for i := 0; i <= lockfiles.DefaultMaxInventoryPaths; i++ {
			if err := a.add(result{path: fmt.Sprintf("p%d/package-lock.json", i)}); err != nil {
				t.Fatalf("inventory overflow aborted scan: %v", err)
			}
		}
		if len(a.files) != lockfiles.DefaultMaxInventoryPaths || len(a.jobs) != 0 {
			t.Fatalf("unbounded retained inventory: files=%d jobs=%d", len(a.files), len(a.jobs))
		}
		r := &profile.Report{}
		if err := a.finish(context.Background(), nil, nil, r); err != nil {
			t.Fatal(err)
		}
		if r.Lockfiles.Status != "skipped" || len(r.Lockfiles.Contexts) != 0 || len(r.Lockfiles.Diagnostics) != 1 || r.Lockfiles.Diagnostics[0].Code != "inventory_path_limit" {
			t.Fatalf("inventory omission was hidden: %+v", r.Lockfiles)
		}
		if err := lockfiles.ValidateReport(r.Lockfiles); err != nil {
			t.Fatalf("invalid skipped report: %v", err)
		}
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if err := a.finish(ctx, nil, nil, &profile.Report{}); !errors.Is(err, context.Canceled) {
			t.Fatalf("skipped work swallowed cancellation: %v", err)
		}
	})
	t.Run("environments", func(t *testing.T) {
		a := newEnvironmentAccumulator(Options{Environments: true})
		for i := 0; i <= environments.DefaultMaxInventoryPaths; i++ {
			if err := a.add(result{path: fmt.Sprintf("p%d/.node-version", i)}); err != nil {
				t.Fatalf("inventory overflow aborted scan: %v", err)
			}
		}
		if len(a.files) != environments.DefaultMaxInventoryPaths || len(a.jobs) != 0 {
			t.Fatalf("unbounded retained inventory: files=%d jobs=%d", len(a.files), len(a.jobs))
		}
		r := &profile.Report{}
		if err := a.finish(context.Background(), nil, nil, r); err != nil {
			t.Fatal(err)
		}
		if r.Environments.Status != "skipped" || len(r.Environments.ToolchainDeclarations) != 0 || len(r.Environments.Boundaries) != 1 || r.Environments.Boundaries[0].Reason != "inventory_path_limit" {
			t.Fatalf("inventory omission was hidden: %+v", r.Environments)
		}
		if err := environments.ValidateReport(r.Environments); err != nil {
			t.Fatalf("invalid skipped report: %v", err)
		}
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if err := a.finish(ctx, nil, nil, &profile.Report{}); !errors.Is(err, context.Canceled) {
			t.Fatalf("skipped work swallowed cancellation: %v", err)
		}
	})
}

func TestNuGetCaseVariantLockfileRemainsVisibleAndIndeterminate(t *testing.T) {
	root := fixtures(t, map[string]string{
		"App.csproj":         `<Project><ItemGroup><PackageReference Include="Declared" Version="1.0.0"/></ItemGroup></Project>`,
		"Packages.lock.json": `{"version":1,"dependencies":{"net8.0":{"Declared":{"type":"Direct"}}}}`,
	})
	r, err := Scan(context.Background(), root, Options{Source: "directory", Lockfiles: true, DeclarationsOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	c := requireLockfilesContext(t, r, "App.csproj")
	if r.Lockfiles.Coverage.LockCandidates != 1 || c.AssociationState != "indeterminate" || r.Lockfiles.Status != "partial" {
		t.Fatalf("case variant became absent or certainly selected: %+v", r.Lockfiles)
	}
}
