package lockfiles

import (
	"testing"

	"github.com/war-and-code/dircue/pkg/declarations"
)

// hasBoundaryReason reports whether any boundary has the given reason.
func hasBoundaryReason(c Context, code string) bool {
	for _, b := range c.Boundaries {
		if b.Reason == code {
			return true
		}
	}
	return false
}

// TestProjectDeclarationsIncomplete covers project-declarations-incomplete for
// both npm and nuget paths.
func TestProjectDeclarationsIncomplete(t *testing.T) {
	// npm
	app := npmRecord("app", npmRef("alpha@1.0.0", "dependencies"))
	app.Complete = false
	in := testInput([]declarations.ProjectRecord{app}, map[string]string{"app/package-lock.json": `{"lockfileVersion":3,"packages":{"":{"dependencies":{"alpha":"1.0.0"}}}}`}, true)
	c := analyzeContext(t, in, app.Project.ID)
	if c.AssociationState != "indeterminate" || !hasBoundaryReason(c, "project-declarations-incomplete") {
		t.Fatalf("npm incomplete: %+v", c)
	}
	// NuGet
	rec := nugetRecord("src/App", "src/App/App.csproj", declarations.Requirement{Kind: "package-reference", Value: "A@1.0", State: "declared"})
	rec.Parsed = false
	in2 := testInput([]declarations.ProjectRecord{rec}, map[string]string{"src/App/packages.lock.json": `{"version":1,"dependencies":{"net8.0":{"A":{"type":"Direct"}}}}`}, true)
	c2 := analyzeContext(t, in2, rec.Project.ID)
	if c2.AssociationState != "indeterminate" || !hasBoundaryReason(c2, "project-declarations-incomplete") {
		t.Fatalf("nuget incomplete: %+v", c2)
	}
}

// TestWorkspaceLockfileOwnerUnresolved covers workspace-lockfile-owner-unresolved
// for an ancestor lockfile without npm workspace semantics.
func TestWorkspaceLockfileOwnerUnresolved(t *testing.T) {
	app := npmRecord("packages/app", npmRef("alpha@1.0.0", "dependencies"))
	// Ancestor lock at repo root; no own lock for packages/app.
	files := map[string]string{
		"package-lock.json": `{"lockfileVersion":3,"packages":{"":{"dependencies":{"alpha":"1.0.0"}}}}`,
	}
	in := testInput([]declarations.ProjectRecord{app}, files, true)
	// WorkspaceLocks = false (default)
	c := analyzeContext(t, in, app.Project.ID)
	if c.AssociationState != "indeterminate" || !hasBoundaryReason(c, "workspace-lockfile-owner-unresolved") {
		t.Fatalf("non-workspace mode: %+v", c)
	}
	// npm's workspace-root selection settles ownership: no ancestor lists the
	// project, so the ancestor lockfile is not its lockfile.
	in.WorkspaceLocks = true
	cw := analyzeContext(t, in, app.Project.ID)
	if cw.AssociationState != "missing" || hasBoundaryReason(cw, "workspace-lockfile-owner-unresolved") || !hasBoundaryReason(cw, "npm-ancestor-lock-not-shared") {
		t.Fatalf("workspace mode: %+v", cw)
	}
}

// npm prefix from a listed member selects the workspace root. A member-local
// lockfile is ignored for association and comparison, just as npm install does.
func TestNPMWorkspaceUsesRootLockDespiteMemberLocalLock(t *testing.T) {
	owner := workspaceRecord(".", "packages/app/package.json")
	member := npmRecord("packages/app", npmRef("alpha@1.0.0", "dependencies"))
	childLock := `{"lockfileVersion":3,"packages":{"":{"dependencies":{"wrong":"9.9.9"}}}}`
	t.Run("root lock owns listed member", func(t *testing.T) {
		files := map[string]string{
			"package-lock.json":              `{"lockfileVersion":3,"packages":{"":{},"packages/app":{"dependencies":{"alpha":"1.0.0"}}}}`,
			"packages/app/package-lock.json": childLock,
		}
		in := testInput([]declarations.ProjectRecord{owner, member}, files, true)
		in.WorkspaceLocks = true
		c := analyzeContext(t, in, member.Project.ID)
		if c.AssociationState != "observed" || c.LockfilePath != "package-lock.json" || len(c.Checks) != 1 || c.Checks[0].Status != "match" {
			t.Fatalf("workspace root lock was not selected: %+v", c)
		}
	})
	t.Run("root shrinkwrap takes precedence over package lock", func(t *testing.T) {
		files := map[string]string{
			"package-lock.json":                `{"lockfileVersion":3,"packages":{"":{},"packages/app":{"dependencies":{"wrong":"9.9.9"}}}}`,
			"npm-shrinkwrap.json":              `{"lockfileVersion":3,"packages":{"":{},"packages/app":{"dependencies":{"alpha":"1.0.0"}}}}`,
			"packages/app/package-lock.json":   childLock,
			"packages/app/npm-shrinkwrap.json": childLock,
		}
		in := testInput([]declarations.ProjectRecord{owner, member}, files, true)
		in.WorkspaceLocks = true
		c := analyzeContext(t, in, member.Project.ID)
		if c.AssociationState != "observed" || c.LockfilePath != "npm-shrinkwrap.json" || len(c.Checks) != 1 || c.Checks[0].Status != "match" {
			t.Fatalf("root shrinkwrap did not take precedence: %+v", c)
		}
	})
	t.Run("member lock does not substitute for missing root lock", func(t *testing.T) {
		in := testInput([]declarations.ProjectRecord{owner, member}, map[string]string{"packages/app/package-lock.json": childLock}, true)
		in.WorkspaceLocks = true
		c := analyzeContext(t, in, member.Project.ID)
		if c.AssociationState != "missing" {
			t.Fatalf("member-local lock was treated as the workspace lock: %+v", c)
		}
	})
	t.Run("member shrinkwrap does not substitute for missing root lock", func(t *testing.T) {
		in := testInput([]declarations.ProjectRecord{owner, member}, map[string]string{"packages/app/npm-shrinkwrap.json": childLock}, true)
		in.WorkspaceLocks = true
		c := analyzeContext(t, in, member.Project.ID)
		if c.AssociationState != "missing" {
			t.Fatalf("member shrinkwrap was treated as the workspace lock: %+v", c)
		}
	})
}

// TestNestedNPMWorkspaceOwnerUnresolved covers nested-npm-workspace-owner-unresolved
// emitted by associateMember() when the member itself declares workspaces.
func TestNestedNPMWorkspaceOwnerUnresolved(t *testing.T) {
	outer := workspaceRecord(".", "packages/app/package.json")
	member := npmRecord("packages/app", npmRef("alpha@1.0.0", "dependencies"))
	// Give the member a workspace-root requirement so declaresNPMWorkspace() returns true.
	member.Project.Requirements = append(member.Project.Requirements,
		declarations.Requirement{Kind: "npm-workspace-root", Value: "true", State: "declared"})
	files := map[string]string{
		"package-lock.json": `{"lockfileVersion":3,"packages":{"":{},"packages/app":{"dependencies":{"alpha":"1.0.0"}}}}`,
	}
	in := testInput([]declarations.ProjectRecord{outer, member}, files, true)
	in.WorkspaceLocks = true
	c := analyzeContext(t, in, member.Project.ID)
	if c.AssociationState != "indeterminate" || !hasBoundaryReason(c, "nested-npm-workspace-owner-unresolved") {
		t.Fatalf("got %+v", c)
	}
}

// TestNPMWorkspaceLockMembershipUnresolved covers npm-workspace-lock-membership-unresolved
// emitted by workspaceOwner() when a workspace-member reference has State != "resolved",
// or when the same member is listed more than once.
func TestNPMWorkspaceLockMembershipUnresolved(t *testing.T) {
	member := npmRecord("packages/app", npmRef("alpha@1.0.0", "dependencies"))
	for _, tc := range []struct {
		name string
		root declarations.ProjectRecord
	}{
		{
			"unresolved member reference",
			declarations.ProjectRecord{
				Parsed: true, Complete: true,
				Project: declarations.Project{
					ID: "package.json", Root: ".", Kind: "npm",
					Requirements: []declarations.Requirement{{Kind: "npm-workspace-root", Value: "true", State: "declared"}},
					References:   []declarations.Reference{{Kind: "npm-workspace-member", Target: "packages/app/package.json", State: "unresolved", Evidence: "package.json"}},
				},
			},
		},
		{
			"duplicate member reference",
			declarations.ProjectRecord{
				Parsed: true, Complete: true,
				Project: declarations.Project{
					ID: "package.json", Root: ".", Kind: "npm",
					Requirements: []declarations.Requirement{{Kind: "npm-workspace-root", Value: "true", State: "declared"}},
					References: []declarations.Reference{
						{Kind: "npm-workspace-member", Target: "packages/app/package.json", State: "resolved", Evidence: "package.json"},
						{Kind: "npm-workspace-member", Target: "packages/app/package.json", State: "resolved", Evidence: "package.json"},
					},
				},
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			in := testInput([]declarations.ProjectRecord{tc.root, member}, map[string]string{"package-lock.json": `{"lockfileVersion":3,"packages":{"":{},"packages/app":{}}}`}, true)
			in.WorkspaceLocks = true
			c := analyzeContext(t, in, member.Project.ID)
			if c.AssociationState != "indeterminate" || !hasBoundaryReason(c, "npm-workspace-lock-membership-unresolved") {
				t.Fatalf("%s: got %+v", tc.name, c)
			}
		})
	}
}

// TestNPMReasonCodesLockfileParsing covers diagnostic codes that are emitted
// by parseLock / npmDirectTables as parsedLock.reason, which Analyze then
// surfaces as a Boundary.Reason on the context.
func TestNPMReasonCodesLockfileParsing(t *testing.T) {
	app := npmRecord("app", npmRef("alpha@1.0.0", "dependencies"))
	for _, tc := range []struct {
		code  string
		lock  string
		state string
	}{
		{
			code:  "npm-packages-table-missing",
			lock:  `{"lockfileVersion":3}`,
			state: "unsupported",
		},
		{
			code:  "npm-root-package-entry-missing",
			lock:  `{"lockfileVersion":3,"packages":{"node_modules/a":{}}}`,
			state: "unsupported",
		},
		{
			code:  "invalid-npm-direct-entry",
			lock:  `{"lockfileVersion":3,"packages":{"":{"dependencies":{"alpha":123}}}}`,
			state: "unsupported",
		},
	} {
		t.Run(tc.code, func(t *testing.T) {
			in := testInput([]declarations.ProjectRecord{app}, map[string]string{"app/package-lock.json": tc.lock}, true)
			c := analyzeContext(t, in, app.Project.ID)
			if c.AssociationState != tc.state {
				t.Fatalf("code=%s: got state=%q want %q; ctx=%+v", tc.code, c.AssociationState, tc.state, c)
			}
			var found bool
			for _, b := range c.Boundaries {
				if b.Reason == tc.code {
					found = true
				}
			}
			if !found {
				t.Fatalf("code=%s: reason not found in boundaries %+v", tc.code, c.Boundaries)
			}
		})
	}
}

// TestNPMDeclarationDiagnosticsQualifyComparisonAndOwnership covers every
// declaration diagnostic code the npm association consumes as input.
func TestNPMDeclarationDiagnosticsQualifyComparisonAndOwnership(t *testing.T) {
	lock := `{"lockfileVersion":3,"packages":{"":{"dependencies":{"alpha":"1.0.0"}},"packages/app":{"dependencies":{"alpha":"1.0.0"}}}}`
	for _, code := range npmComparisonDiagnostics {
		t.Run("comparison/"+code, func(t *testing.T) {
			app := npmRecord(".", npmRef("alpha@1.0.0", "dependencies"))
			in := testInput([]declarations.ProjectRecord{app}, map[string]string{"package-lock.json": lock}, true)
			in.Declarations.Diagnostics = []declarations.Diagnostic{{Path: app.Project.ID, Code: code}}
			c := analyzeContext(t, in, app.Project.ID)
			if c.AssociationState != "observed" || !hasBoundaryReason(c, "npm-manifest-declarations-unresolved") || len(c.Checks) != 1 || c.Checks[0].Status != "indeterminate" {
				t.Fatalf("%s: %+v", code, c)
			}
		})
	}
	for _, code := range npmMembershipDiagnostics {
		t.Run("membership/"+code, func(t *testing.T) {
			owner := workspaceRecord(".", "packages/app/package.json")
			member := npmRecord("packages/app", npmRef("alpha@1.0.0", "dependencies"))
			in := testInput([]declarations.ProjectRecord{owner, member}, map[string]string{"package-lock.json": lock}, true)
			in.WorkspaceLocks = true
			in.Declarations.Diagnostics = []declarations.Diagnostic{{Path: owner.Project.ID, Code: code}}
			c := analyzeContext(t, in, member.Project.ID)
			if c.AssociationState != "indeterminate" || !hasBoundaryReason(c, "npm-workspace-lock-owner-incomplete") {
				t.Fatalf("%s: %+v", code, c)
			}
		})
	}
	for _, kind := range []string{"npm-local-dependency", "npm-workspace-dependency"} {
		t.Run("reference/"+kind, func(t *testing.T) {
			ref := npmRef("alpha@1.0.0", "dependencies")
			ref.Kind = kind
			app := npmRecord(".", ref)
			in := testInput([]declarations.ProjectRecord{app}, map[string]string{"package-lock.json": lock}, true)
			c := analyzeContext(t, in, app.Project.ID)
			if c.AssociationState != "observed" || len(c.Checks) != 1 || c.Checks[0].Status != "indeterminate" {
				t.Fatalf("%s: %+v", kind, c)
			}
		})
	}
}
