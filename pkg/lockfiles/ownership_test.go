package lockfiles

import (
	"context"
	"strings"
	"testing"

	"github.com/war-and-code/dircue/pkg/declarations"
)

func workspaceRecord(root string, targets ...string) declarations.ProjectRecord {
	r := npmRecord(root)
	r.Project.Requirements = []declarations.Requirement{{Kind: "npm-workspace-root", Value: "true", State: "declared"}}
	for _, target := range targets {
		r.Project.References = append(r.Project.References, declarations.Reference{Kind: "npm-workspace-member", Target: target, State: "resolved", Evidence: r.Project.ID})
	}
	return r
}

func analyzeContext(t *testing.T, in Input, manifest string) Context {
	t.Helper()
	r, err := Analyze(context.Background(), in, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateReport(r); err != nil {
		t.Fatalf("invalid report: %v", err)
	}
	c := contextByManifest(r, manifest)
	if c.ManifestPath == "" {
		t.Fatalf("no context for %s: %+v", manifest, r.Contexts)
	}
	return c
}

func reasons(c Context) []string {
	out := make([]string, 0, len(c.Boundaries))
	for _, b := range c.Boundaries {
		out = append(out, b.Reason)
	}
	return out
}

// An independent project with its own lockfile below a workspace root that
// does not list it is npm's own local prefix (playwright's
// tests/playwright-test/stable-test-runner is a public example).
func TestNPMWorkspaceModeKeepsIndependentNestedLock(t *testing.T) {
	root := workspaceRecord(".", "packages/app/package.json")
	member := npmRecord("packages/app", npmRef("alpha@1.0.0", "dependencies"))
	nested := npmRecord("tests/runner", npmRef("beta@2.0.0", "dependencies"))
	files := map[string]string{
		"package-lock.json":              `{"lockfileVersion":3,"packages":{"":{},"packages/app":{"dependencies":{"alpha":"1.0.0"}}}}`,
		"tests/runner/package-lock.json": `{"lockfileVersion":3,"packages":{"":{"dependencies":{"beta":"2.0.0"}}}}`,
	}
	in := testInput([]declarations.ProjectRecord{root, member, nested}, files, true)
	in.WorkspaceLocks = true
	c := analyzeContext(t, in, nested.Project.ID)
	if c.AssociationState != "observed" || c.LockfilePath != "tests/runner/package-lock.json" || c.Checks[0].Status != "match" || len(c.Boundaries) != 0 {
		t.Fatalf("independent nested lock was not used: %+v", c)
	}
	in.WorkspaceLocks = false
	if legacy := analyzeContext(t, in, nested.Project.ID); legacy.AssociationState != "observed" {
		t.Fatalf("default mode changed for an own lockfile: %+v", legacy)
	}
}

// Declaration omissions block only ownership decisions they could change.
func TestNPMWorkspaceOwnershipIgnoresUnrelatedDeclarationOmissions(t *testing.T) {
	root := workspaceRecord(".", "packages/app/package.json")
	member := npmRecord("packages/app", npmRef("alpha@1.0.0", "dependencies"))
	files := map[string]string{"package-lock.json": `{"lockfileVersion":3,"packages":{"":{},"packages/app":{"dependencies":{"alpha":"1.0.0"}}}}`}
	for _, tc := range []struct {
		name       string
		omitted    []string
		trees      []string
		attributed bool
		want       string
	}{
		{"unrelated oversized pom.xml", []string{"java/pom.xml"}, nil, true, "observed"},
		{"omitted files that are not manifests", nil, nil, true, "observed"},
		{"unconfined omitted path is unattributed", []string{"../x/package.json"}, nil, true, "indeterminate"},
		{"unattributed omission", nil, nil, false, "indeterminate"},
		{"omitted ancestor package.json", []string{"packages/package.json"}, nil, true, "indeterminate"},
		{"omitted manifest beneath the owner", []string{"packages/other/package.json"}, nil, true, "indeterminate"},
		{"unreadable directory beneath the owner", nil, []string{"packages/other"}, true, "indeterminate"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			in := testInput([]declarations.ProjectRecord{root, member}, files, true)
			in.WorkspaceLocks = true
			in.Declarations.Status = "partial"
			in.Declarations.Coverage.OmittedFiles = 1
			in.DeclarationOmissions, in.DeclarationOmittedTrees, in.DeclarationOmissionsAttributed = tc.omitted, tc.trees, tc.attributed
			c := analyzeContext(t, in, member.Project.ID)
			if c.AssociationState != tc.want {
				t.Fatalf("association=%q want %q: %+v", c.AssociationState, tc.want, c)
			}
		})
	}
}

func TestNuGetOwnershipIgnoresUnrelatedDeclarationOmissions(t *testing.T) {
	record := nugetRecord("src/App", "src/App/App.csproj", declarations.Requirement{Kind: "package-reference", Value: "A@1.0", State: "declared"})
	files := map[string]string{"src/App/packages.lock.json": `{"version":1,"dependencies":{"net8.0":{"A":{"type":"Direct"}}}}`}
	for _, tc := range []struct {
		name    string
		omitted []string
		want    string
	}{
		{"unrelated omission", []string{"web/package.json"}, "observed"},
		{"omitted project elsewhere", []string{"src/Other/Other.csproj"}, "observed"},
		{"omitted project in the same directory", []string{"src/App/Second.vbproj"}, "indeterminate"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			in := testInput([]declarations.ProjectRecord{record}, files, true)
			in.Declarations.Status = "partial"
			in.Declarations.Coverage.OmittedFiles = int64(len(tc.omitted))
			in.DeclarationOmissions, in.DeclarationOmissionsAttributed = tc.omitted, true
			if c := analyzeContext(t, in, record.Project.ID); c.AssociationState != tc.want {
				t.Fatalf("association=%q want %q: %+v", c.AssociationState, tc.want, c)
			}
		})
	}
}

func TestNuGetLockfilesApplyToFSharpAndVisualBasicProjects(t *testing.T) {
	for _, id := range []string{"src/App/App.fsproj", "src/App/App.vbproj"} {
		record := nugetRecord("src/App", id, declarations.Requirement{Kind: "package-reference", Value: "A@1.0", State: "declared"})
		in := testInput([]declarations.ProjectRecord{record}, map[string]string{"src/App/packages.lock.json": `{"version":1,"dependencies":{"net8.0":{"A":{"type":"Direct"},"FSharp.Core":{"type":"Direct"}}}}`}, true)
		if c := analyzeContext(t, in, id); c.AssociationState != "observed" || c.Checks[0].Status != "match" {
			t.Fatalf("%s lock was not associated: %+v", id, c)
		}
		project := "src/App/packages." + id[len("src/App/"):len(id)-len(".fsproj")] + ".lock.json"
		in = testInput([]declarations.ProjectRecord{record}, map[string]string{project: `{"version":2,"dependencies":{"net8.0":{"A":{"type":"Direct"}}}}`}, true)
		if c := analyzeContext(t, in, id); c.AssociationState != "observed" || c.LockfilePath != project {
			t.Fatalf("%s project-named lock was not associated: %+v", id, c)
		}
	}
}

// Yarn, pnpm, and Bun projects are outside npm's lockfile semantics; they
// must not be reported as missing an npm lockfile.
func TestNPMAlternativePackageManagerEvidence(t *testing.T) {
	app := npmRecord("app", npmRef("alpha@1.0.0", "dependencies"))
	npmLock := `{"lockfileVersion":3,"packages":{"":{"dependencies":{"alpha":"1.0.0"}}}}`
	withManager := func(rec declarations.ProjectRecord, value string) declarations.ProjectRecord {
		rec.Project.Requirements = append(rec.Project.Requirements, declarations.Requirement{Kind: "package-manager", Value: value, State: "declared"})
		return rec
	}
	for _, tc := range []struct {
		name    string
		records []declarations.ProjectRecord
		files   map[string]string
		state   string
		lock    string
		reason  string
	}{
		{"own Yarn lock", []declarations.ProjectRecord{app}, map[string]string{"app/yarn.lock": "x"}, "unsupported", "app/yarn.lock", "npm-alternative-lockfile-format"},
		{"own pnpm lock", []declarations.ProjectRecord{app}, map[string]string{"app/pnpm-lock.yaml": "x"}, "unsupported", "app/pnpm-lock.yaml", "npm-alternative-lockfile-format"},
		{"own Bun binary lock", []declarations.ProjectRecord{app}, map[string]string{"app/bun.lockb": "x"}, "unsupported", "app/bun.lockb", "npm-alternative-lockfile-format"},
		{"own Bun text lock", []declarations.ProjectRecord{app}, map[string]string{"app/bun.lock": "x"}, "unsupported", "app/bun.lock", "npm-alternative-lockfile-format"},
		{"own pnpm workspace file", []declarations.ProjectRecord{app}, map[string]string{"app/pnpm-workspace.yaml": "x"}, "unsupported", "", "npm-alternative-package-manager"},
		{"npm lock wins over a stray Yarn lock", []declarations.ProjectRecord{app}, map[string]string{"app/package-lock.json": npmLock, "app/yarn.lock": "x"}, "observed", "app/package-lock.json", ""},
		{"ancestor Yarn lock", []declarations.ProjectRecord{app}, map[string]string{"yarn.lock": "x"}, "indeterminate", "", "npm-ancestor-alternative-package-manager"},
		{"ancestor Yarn workspace that does not list the project", []declarations.ProjectRecord{npmRecord("."), app}, map[string]string{"yarn.lock": "x"}, "indeterminate", "", "npm-ancestor-alternative-package-manager"},
		{"ancestor Rush configuration", []declarations.ProjectRecord{app}, map[string]string{"rush.json": "x"}, "indeterminate", "", "npm-ancestor-alternative-package-manager"},
		{"ancestor pnpm packageManager", []declarations.ProjectRecord{withManager(npmRecord("."), "pnpm@9.0.0"), app}, nil, "indeterminate", "", "npm-ancestor-alternative-package-manager"},
		{"nearer npm packageManager wins", []declarations.ProjectRecord{withManager(npmRecord("."), "pnpm@9.0.0"), withManager(app, "npm@10.0.0")}, nil, "missing", "", "lockfile-not-present"},
		{"no alternative evidence", []declarations.ProjectRecord{app}, nil, "missing", "", "lockfile-not-present"},
	} {
		for _, workspace := range []bool{false, true} {
			t.Run(tc.name, func(t *testing.T) {
				in := testInput(tc.records, tc.files, true)
				in.WorkspaceLocks = workspace
				c := analyzeContext(t, in, app.Project.ID)
				got := ""
				if len(c.Boundaries) > 0 {
					got = c.Boundaries[0].Reason
				}
				state, reason := tc.state, tc.reason
				// Workspace mode has evaluated the package.json workspaces that Yarn
				// shares with npm: a Yarn lock above a project that no workspace
				// lists is not that project's lockfile.
				if workspace && strings.HasPrefix(tc.name, "ancestor Yarn") {
					state, reason = "missing", "lockfile-not-present"
				}
				if c.AssociationState != state || c.LockfilePath != tc.lock || got != reason {
					t.Fatalf("workspace=%v got %+v", workspace, c)
				}
			})
		}
	}
}

// A project that declares nothing to lock is not_applicable whatever manager
// governs the tree above it; evidence at its own root still names the manager.
func TestNPMProjectWithoutDeclarationsIgnoresAncestorManager(t *testing.T) {
	empty := npmRecord("app")
	for _, tc := range []struct {
		name   string
		files  map[string]string
		state  string
		reason string
	}{
		{"ancestor Yarn lock", map[string]string{"yarn.lock": "x"}, "not_applicable", ""},
		{"ancestor pnpm workspace", map[string]string{"pnpm-workspace.yaml": "x"}, "not_applicable", ""},
		{"own Yarn lock", map[string]string{"app/yarn.lock": "x"}, "unsupported", "npm-alternative-lockfile-format"},
	} {
		for _, workspace := range []bool{false, true} {
			t.Run(tc.name, func(t *testing.T) {
				in := testInput([]declarations.ProjectRecord{empty}, tc.files, true)
				in.WorkspaceLocks = workspace
				c := analyzeContext(t, in, empty.Project.ID)
				got := ""
				if len(c.Boundaries) > 0 {
					got = c.Boundaries[0].Reason
				}
				if c.AssociationState != tc.state || got != tc.reason {
					t.Fatalf("workspace=%v got %+v", workspace, c)
				}
			})
		}
	}
}

// Yarn and Bun read the same package.json workspaces field as npm, so in
// workspace mode their members resolve to the owner's alternative lockfile.
func TestNPMWorkspaceMemberOfAlternativeManagerIsUnsupported(t *testing.T) {
	member := npmRecord("packages/app", npmRef("alpha@1.0.0", "dependencies"))
	for _, tc := range []struct {
		name   string
		files  map[string]string
		reason string
	}{
		{"Yarn 4 workspace", map[string]string{"yarn.lock": "x", ".yarnrc.yml": "x"}, "npm-alternative-lockfile-format"},
		{"Bun workspace", map[string]string{"bun.lock": "x"}, "npm-alternative-lockfile-format"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			in := testInput([]declarations.ProjectRecord{workspaceRecord(".", member.Project.ID), member}, tc.files, true)
			in.WorkspaceLocks = true
			if c := analyzeContext(t, in, member.Project.ID); c.AssociationState != "unsupported" || reasons(c)[0] != tc.reason {
				t.Fatalf("got %+v", c)
			}
			in.WorkspaceLocks = false
			if c := analyzeContext(t, in, member.Project.ID); c.AssociationState != "indeterminate" || reasons(c)[0] != "npm-ancestor-alternative-package-manager" {
				t.Fatalf("default mode got %+v", c)
			}
		})
	}
	// pnpm reads pnpm-workspace.yaml, which npm does not, so membership of a
	// package that npm's workspaces do not list remains unknown.
	in := testInput([]declarations.ProjectRecord{npmRecord("."), member}, map[string]string{"pnpm-workspace.yaml": "x", "pnpm-lock.yaml": "x"}, true)
	in.WorkspaceLocks = true
	if c := analyzeContext(t, in, member.Project.ID); c.AssociationState != "indeterminate" || reasons(c)[0] != "npm-ancestor-alternative-package-manager" || c.Boundaries[0].Path != "pnpm-workspace.yaml" {
		t.Fatalf("pnpm member got %+v", c)
	}
}

// An invalid packageManager value carries no manager name. npm ignores the
// field, so it does not block an npm association.
func TestInvalidPackageManagerValueDoesNotBlockNPM(t *testing.T) {
	member := npmRecord("packages/app", npmRef("alpha@1.0.0", "dependencies"))
	in := testInput([]declarations.ProjectRecord{workspaceRecord(".", member.Project.ID), member}, map[string]string{"package-lock.json": `{"lockfileVersion":3,"packages":{"":{},"packages/app":{"dependencies":{"alpha":"1.0.0"}}}}`}, true)
	in.WorkspaceLocks = true
	in.Declarations.Diagnostics = []declarations.Diagnostic{{Path: "package.json", Code: "unsupported-package-manager"}, {Path: member.Project.ID, Code: "unsupported-package-manager"}}
	if c := analyzeContext(t, in, member.Project.ID); c.AssociationState != "observed" || c.Checks[0].Status != "match" {
		t.Fatalf("got %+v", c)
	}
}

// Yarn's nohoist is outside npm's workspace semantics; npm ignores it.
func TestIgnoredWorkspaceFieldDoesNotBlockOwnership(t *testing.T) {
	member := npmRecord("packages/app", npmRef("alpha@1.0.0", "dependencies"))
	in := testInput([]declarations.ProjectRecord{workspaceRecord(".", member.Project.ID), member}, map[string]string{"package-lock.json": `{"lockfileVersion":3,"packages":{"":{},"packages/app":{"dependencies":{"alpha":"1.0.0"}}}}`}, true)
	in.WorkspaceLocks = true
	in.Declarations.Diagnostics = []declarations.Diagnostic{{Path: "package.json", Code: "unsupported-npm-workspace-field"}}
	if c := analyzeContext(t, in, member.Project.ID); c.AssociationState != "observed" {
		t.Fatalf("got %+v", c)
	}
}

// Only compared names are charged to the package-name budget, so a large
// workspace lock does not exhaust it for later projects.
func TestWorkspaceLockChargesOnlyComparedNames(t *testing.T) {
	member := npmRecord("packages/app", npmRef("alpha@1.0.0", "dependencies"))
	lock := `{"lockfileVersion":3,"packages":{"":{"dependencies":{"x":"1","y":"1"}},"packages/app":{"dependencies":{"alpha":"1.0.0"}},"node_modules/x":{"dependencies":{"z":"1"}},"node_modules/y":{}}}`
	in := testInput([]declarations.ProjectRecord{workspaceRecord(".", member.Project.ID), member}, map[string]string{"package-lock.json": lock}, true)
	in.WorkspaceLocks = true
	r, err := Analyze(context.Background(), in, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	// Root: 2 unexpected names. Member: 1 compared name.
	if r.Coverage.PackageNames != 3 {
		t.Fatalf("package names=%d want 3", r.Coverage.PackageNames)
	}
}

// Corepack applies the nearest packageManager declaration. Inside the
// workspace it names the workspace's manager; above the owner it leaves the
// manager, and so the lockfile, uncertain.
func TestNPMWorkspaceMemberUsesNearestPackageManager(t *testing.T) {
	withManager := func(rec declarations.ProjectRecord, value string) declarations.ProjectRecord {
		rec.Project.Requirements = append(rec.Project.Requirements, declarations.Requirement{Kind: "package-manager", Value: value, State: "declared"})
		return rec
	}
	member := npmRecord("fixtures/yarn/nested", npmRef("alpha@1.0.0", "dependencies"))
	owner := workspaceRecord("fixtures/yarn", member.Project.ID)
	files := map[string]string{"fixtures/yarn/package-lock.json": `{"lockfileVersion":3,"packages":{"":{},"nested":{"dependencies":{"alpha":"1.0.0"}}}}`}
	for _, tc := range []struct {
		name    string
		records []declarations.ProjectRecord
		state   string
		path    string
	}{
		{"pnpm above the owner", []declarations.ProjectRecord{withManager(npmRecord("."), "pnpm@9.0.0"), owner, member}, "indeterminate", "package.json"},
		{"Yarn at the owner", []declarations.ProjectRecord{withManager(owner, "yarn@4.0.0"), member}, "unsupported", "fixtures/yarn/package.json"},
		{"npm at the owner shadows pnpm above", []declarations.ProjectRecord{withManager(npmRecord("."), "pnpm@9.0.0"), withManager(owner, "npm@10.0.0"), member}, "observed", "fixtures/yarn/package-lock.json"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			in := testInput(tc.records, files, true)
			in.WorkspaceLocks = true
			if c := analyzeContext(t, in, member.Project.ID); c.AssociationState != tc.state || c.Boundaries[0].Path != tc.path {
				t.Fatalf("got %+v", c)
			}
		})
	}
}

func TestOutcomeReasonSkipsTheShrinkwrapNote(t *testing.T) {
	app := npmRecord("app", npmRef("alpha@1.0.0", "dependencies"))
	in := testInput([]declarations.ProjectRecord{app}, map[string]string{"app/npm-shrinkwrap.json": `{"lockfileVersion":1}`}, true)
	c := analyzeContext(t, in, app.Project.ID)
	if reasons(c)[0] != "npm-v11-shrinkwrap-selection" || c.OutcomeReason() != "unsupported-npm-lockfile-version" {
		t.Fatalf("got %+v", c)
	}
	if (Context{}).OutcomeReason() != "" {
		t.Fatal("empty context has a reason")
	}
}

// An unreadable directory outside a nested owner cannot hide its members.
func TestNPMWorkspaceOwnershipScopesUnreadableDirectories(t *testing.T) {
	owner := workspaceRecord("web", "web/app/package.json")
	member := npmRecord("web/app", npmRef("alpha@1.0.0", "dependencies"))
	files := map[string]string{"web/package-lock.json": `{"lockfileVersion":3,"packages":{"":{},"app":{"dependencies":{"alpha":"1.0.0"}}}}`}
	for tree, want := range map[string]string{"docs": "observed", "web/lib": "indeterminate"} {
		in := testInput([]declarations.ProjectRecord{owner, member}, files, true)
		in.WorkspaceLocks = true
		in.Declarations.Status = "partial"
		in.Declarations.Coverage.OmittedFiles = 1
		in.DeclarationOmittedTrees, in.DeclarationOmissionsAttributed = []string{tree}, true
		if c := analyzeContext(t, in, member.Project.ID); c.AssociationState != want {
			t.Fatalf("unreadable %s: association=%q want %q: %+v", tree, c.AssociationState, want, c)
		}
	}
}

// A project at the selected root has no ancestor that an omission could hide,
// so its own lockfile stays observed even when omissions are unattributed.
func TestNPMRootProjectIgnoresUnattributedOmissions(t *testing.T) {
	root := npmRecord(".", npmRef("alpha@1.0.0", "dependencies"))
	nested := npmRecord("tools/cli", npmRef("alpha@1.0.0", "dependencies"))
	lock := `{"lockfileVersion":3,"packages":{"":{"dependencies":{"alpha":"1.0.0"}}}}`
	in := testInput([]declarations.ProjectRecord{root, nested}, map[string]string{"package-lock.json": lock, "tools/cli/package-lock.json": lock}, true)
	in.WorkspaceLocks = true
	in.Declarations.Status = "partial"
	in.Declarations.Coverage.OmittedFiles = 1
	if c := analyzeContext(t, in, root.Project.ID); c.AssociationState != "observed" {
		t.Fatalf("root association=%q: %+v", c.AssociationState, c)
	}
	if c := analyzeContext(t, in, nested.Project.ID); c.AssociationState != "indeterminate" || !hasBoundaryReason(c, "npm-workspace-lock-owner-incomplete") {
		t.Fatalf("nested association=%q: %+v", c.AssociationState, c)
	}
}

// npm skips a listed workspace path without a package.json, as npm 11.12.1
// does when installing and when resolving `npm prefix` for the other members.
func TestNPMWorkspaceMemberIgnoresAbsentListedPaths(t *testing.T) {
	root := workspaceRecord(".", "packages/a/package.json")
	root.Project.References = append(root.Project.References, declarations.Reference{Kind: "npm-workspace-member", Target: "docs/package.json", State: "missing", Evidence: root.Project.ID})
	member := npmRecord("packages/a", npmRef("alpha@1.0.0", "dependencies"))
	files := map[string]string{"package-lock.json": `{"lockfileVersion":3,"packages":{"":{},"packages/a":{"dependencies":{"alpha":"1.0.0"}}}}`}
	in := testInput([]declarations.ProjectRecord{root, member}, files, true)
	in.WorkspaceLocks = true
	if c := analyzeContext(t, in, member.Project.ID); c.AssociationState != "observed" {
		t.Fatalf("association=%q: %+v", c.AssociationState, c)
	}
	root.Project.References[1].State = "unresolved"
	in = testInput([]declarations.ProjectRecord{root, member}, files, true)
	in.WorkspaceLocks = true
	if c := analyzeContext(t, in, member.Project.ID); c.AssociationState != "indeterminate" || !hasBoundaryReason(c, "npm-workspace-lock-membership-unresolved") {
		t.Fatalf("unresolved listed path: association=%q: %+v", c.AssociationState, c)
	}
}

// A dependency field the parser could not read leaves the absence of
// declarations unknown, so such a project is not not_applicable.
func TestNPMUnreadableDependencyFieldsAreNotNotApplicable(t *testing.T) {
	for _, workspace := range []bool{false, true} {
		for _, code := range []string{"invalid-npm-dependency", "invalid-npm-field", "omitted"} {
			standalone := npmRecord("app")
			owner := workspaceRecord("ws", "ws/member/package.json")
			member := npmRecord("ws/member")
			in := testInput([]declarations.ProjectRecord{standalone, owner, member}, nil, true)
			in.WorkspaceLocks = workspace
			if code == "omitted" {
				in.Declarations.Coverage.OmittedDiagnostics = 1
			} else {
				for _, rec := range []declarations.ProjectRecord{standalone, member} {
					in.Declarations.Diagnostics = append(in.Declarations.Diagnostics, declarations.Diagnostic{Path: rec.Project.ID, Code: code})
				}
			}
			for _, rec := range []declarations.ProjectRecord{standalone, member} {
				if !workspace && rec.Project.Root == "ws/member" {
					continue
				}
				want := "npm-manifest-declarations-unresolved"
				if code == "omitted" && rec.Project.Root == "ws/member" {
					// Omitted diagnostics already leave the owner unknown.
					want = "npm-workspace-lock-owner-incomplete"
				}
				c := analyzeContext(t, in, rec.Project.ID)
				if c.AssociationState != "indeterminate" || !hasBoundaryReason(c, want) {
					t.Fatalf("workspace=%v %s %s: association=%q: %+v", workspace, code, rec.Project.ID, c.AssociationState, c)
				}
			}
		}
	}
	in := testInput([]declarations.ProjectRecord{npmRecord("app")}, nil, true)
	in.Declarations.Diagnostics = []declarations.Diagnostic{{Path: "other/package.json", Code: "invalid-npm-dependency"}}
	if c := analyzeContext(t, in, "app/package.json"); c.AssociationState != "not_applicable" {
		t.Fatalf("another manifest's diagnostic changed the association: %+v", c)
	}
}
