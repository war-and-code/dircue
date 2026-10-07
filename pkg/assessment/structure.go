package assessment

import (
	"fmt"
	"path"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/war-and-code/dircue/pkg/componentmap"
	"github.com/war-and-code/dircue/pkg/declarations"
	"github.com/war-and-code/dircue/pkg/pathrole"
)

const (
	StructureGroupLimit      = 256
	StructureMemberLimit     = 64
	StructureEdgeLimit       = 512
	StructureComponentLimit  = 256
	StructureProjectLimit    = 64
	StructureEntryPointLimit = 512
	StructureQualifiedLimit  = 512
)

// StructureReport describes explicit project populations, memberships, and
// local dependencies. Its graph is only the observed declaration graph.
type StructureReport struct {
	Populations                []StructurePopulation `json:"populations"`
	Coverage                   []StructureCoverage   `json:"coverage"`
	WorkspaceGroups            []StructureGroup      `json:"workspace_groups"`
	SolutionGroups             []StructureGroup      `json:"solution_groups"`
	WorkspaceGroupCount        int64                 `json:"workspace_group_count"`
	SolutionGroupCount         int64                 `json:"solution_group_count"`
	Dependencies               StructureDependencies `json:"dependencies"`
	EntryPoints                []StructureEntryPoint `json:"entry_points"`
	EntryPointCount            int64                 `json:"entry_point_count"`
	EntryPointRowCount         int64                 `json:"entry_point_row_count"`
	EntryPointAssociationCount int64                 `json:"entry_point_association_count"`
	OmittedWorkspaceGroups     int64                 `json:"omitted_workspace_groups"`
	OmittedSolutionGroups      int64                 `json:"omitted_solution_groups"`
	OmittedEntryPoints         int64                 `json:"omitted_entry_points"`
	ExcludedNonEntryKinds      map[string]int64      `json:"excluded_non_entry_kinds,omitempty"`
}

type StructurePopulation struct {
	Population string `json:"population"`
	Ecosystem  string `json:"ecosystem"`
	Role       string `json:"role"`
	Metric     Metric `json:"metric"`
}

// StructureGroup retains explicit workspace, module, and solution membership.
// Member arrays are bounded samples; counts remain exact for retained facts.
type StructureGroup struct {
	ID                       string                      `json:"id"`
	Ecosystem                string                      `json:"ecosystem"`
	Role                     string                      `json:"role"`
	Kind                     string                      `json:"kind"`
	MemberCount              int64                       `json:"member_count"`
	Members                  []string                    `json:"members"`
	OmittedMembers           int64                       `json:"omitted_members"`
	UnresolvedMemberCount    int64                       `json:"unresolved_member_count"`
	UnresolvedMembers        []StructureUnresolvedMember `json:"unresolved_members"`
	OmittedUnresolvedMembers int64                       `json:"omitted_unresolved_members"`
	MembershipCoverage       StructureCoverage           `json:"membership_coverage"`
}

type StructureCoverage struct {
	Scope     string   `json:"scope"`
	Ecosystem string   `json:"ecosystem"`
	Status    string   `json:"status"`
	Reasons   []string `json:"reasons"`
}

type StructureUnresolvedMember struct {
	Target     string `json:"target,omitempty"`
	Value      string `json:"value,omitempty"`
	State      string `json:"state"`
	Resolution string `json:"resolution"`
	Reason     string `json:"reason,omitempty"`
	Evidence   string `json:"evidence,omitempty"`
}

type StructureDependencies struct {
	Projects                       Metric                        `json:"projects"`
	DefiniteEdges                  Metric                        `json:"definite_edges"`
	ConnectedGroups                Metric                        `json:"connected_groups"`
	ConnectedGroupsWithQualified   Metric                        `json:"connected_groups_with_qualified"`
	QualifiedReferences            []StructureQualifiedCount     `json:"qualified_references"`
	QualifiedReferenceCount        int64                         `json:"qualified_reference_count"`
	OmittedQualifiedReferenceCount int64                         `json:"omitted_qualified_reference_count"`
	QualifiedGroupCount            int64                         `json:"qualified_reference_group_count"`
	Edges                          []StructureEdge               `json:"edges"`
	Components                     []StructureConnectedComponent `json:"components"`
	OmittedEdges                   int64                         `json:"omitted_edges"`
	OmittedComponents              int64                         `json:"omitted_components"`
	OmittedQualified               int64                         `json:"omitted_qualified_reference_groups"`
}

type StructureQualifiedCount struct {
	Ecosystem  string `json:"ecosystem"`
	Kind       string `json:"kind"`
	State      string `json:"state"`
	Resolution string `json:"resolution"`
	Count      int64  `json:"count"`
}

type StructureEdge struct {
	From      string `json:"from"`
	To        string `json:"to"`
	Ecosystem string `json:"ecosystem"`
	Kind      string `json:"kind"`
	Evidence  string `json:"evidence,omitempty"`
}

type StructureConnectedComponent struct {
	ID              string   `json:"id"`
	ProjectCount    int64    `json:"project_count"`
	EdgeCount       int64    `json:"edge_count"`
	Projects        []string `json:"projects"`
	OmittedProjects int64    `json:"omitted_projects"`
}

// StructureEntryPoint is a neutralized entry-point observation for map or
// deployable analyzers. ProjectID is empty when ownership is unknown.
type StructureEntryPoint struct {
	ProjectID    string `json:"project_id,omitempty"`
	EvidencePath string `json:"evidence_path"`
	Ecosystem    string `json:"ecosystem"`
	Role         string `json:"role"`
	Kind         string `json:"kind"`
	Name         string `json:"name,omitempty"`
	Target       string `json:"target,omitempty"`
	Basis        string `json:"basis"`
	Reason       string `json:"reason,omitempty"`
	State        string `json:"state"`
}

func structurePopulationKey(population, ecosystem, role string) string {
	return population + "\x00" + ecosystem + "\x00" + role
}

// diagnosticRelationshipScopeTable maps each known diagnostic code to the
// structural coverage scope it qualifies: "membership", "dependencies",
// "both", or "none" for facts that cannot change a project's memberships or
// local dependencies, such as build targets, bin entries, and names inferred
// from directories. New codes emitted by declaration parsers must be added
// here; TestDiagnosticRelationshipScopeIsExhaustive scans the parser sources
// and fails when one is missing.
var diagnosticRelationshipScopeTable = map[string]string{
	// Cargo workspace membership.
	"cargo-ambiguous-workspace":             "membership",
	"cargo-conflicting-workspace":           "membership",
	"cargo-member-not-package":              "membership",
	"cargo-missing-project":                 "membership",
	"cargo-nested-workspace":                "membership",
	"cargo-pattern-limit":                   "membership",
	"cargo-resolution-limit":                "membership",
	"cargo-unlisted-nested-package":         "membership",
	"cargo-unsupported-pattern":             "membership",
	"cargo-workspace-conflict":              "membership",
	"cargo-workspace-member-unavailable":    "membership",
	"cargo-workspace-membership-unresolved": "membership",
	"cargo-workspace-pattern-unresolved":    "membership",
	"cargo-workspace-selection-unresolved":  "membership",
	"cargo-default-not-member":              "membership",
	"cargo-workspace-pattern-unmatched":     "membership",
	// Cargo dependency resolution.
	"cargo-dependency-inheritance-unresolved": "dependencies",
	"cargo-dependency-limit":                  "dependencies",
	"cargo-dependency-name-mismatch":          "dependencies",
	"cargo-dependency-overrides-unevaluated":  "dependencies",
	"cargo-inheritance-unresolved":            "dependencies",
	// Cargo non-structural (build targets, features, scripts).
	"cargo-build-script-unavailable": "none",
	"cargo-external-build-script":    "none",
	"cargo-external-target":          "none",
	"cargo-nightly-features":         "none",
	"cargo-target-limit":             "none",
	"cargo-target-unavailable":       "none",
	// Go.
	"external-go-ignore":         "dependencies",
	"external-go-reference":      "dependencies",
	"unsupported-go-module-path": "dependencies",
	"unparsed-go-target":         "dependencies",
	// npm workspace membership.
	"duplicate-npm-workspace-name":       "membership",
	"npm-workspace-match-limit":          "membership",
	"unmatched-npm-workspace-pattern":    "membership",
	"unsupported-npm-workspace-field":    "membership",
	"unsupported-npm-workspace-identity": "membership",
	"unsupported-npm-workspace-pattern":  "membership",
	"unsupported-npm-workspaces":         "membership",
	// npm dependencies. Note: "unresolved/unsupported-npm-workspace-dependency"
	// qualifies dependency coverage, not membership; the substring heuristic
	// was previously routing these to membership incorrectly.
	"external-npm-dependency":              "dependencies",
	"npm-resolution-limit":                 "dependencies",
	"unparsed-npm-target":                  "dependencies",
	"unresolved-npm-workspace-dependency":  "dependencies",
	"unsupported-npm-dependency":           "dependencies",
	"unsupported-npm-workspace-dependency": "dependencies",
	// npm non-structural.
	"external-npm-bin":              "none",
	"invalid-npm-bin":               "none",
	"invalid-npm-dependency":        "both",
	"invalid-npm-field":             "both",
	"invalid-npm-manifest":          "both",
	"invalid-npm-script":            "none",
	"unnamed-npm-bin":               "none",
	"unsupported-npm-bin-directory": "none",
	"unsupported-npm-engine":        "none",
	"unsupported-npm-field":         "both",
	"unsupported-npm-name":          "both",
	"unsupported-npm-version":       "none",
	// Dart pub.
	"external-pub-path-dependency": "dependencies",
	"invalid-dart-pubspec":         "both",
	"missing-dart-package-name":    "both",
	// Python workspace membership.
	"ambiguous-python-workspace":           "membership",
	"duplicate-python-member-name":         "membership",
	"missing-python-workspace-member":      "membership",
	"nested-python-workspace":              "membership",
	"python-member-without-project":        "membership",
	"python-workspace-match-limit":         "membership",
	"python-workspace-pattern-limit":       "membership",
	"unmatched-python-workspace-pattern":   "membership",
	"unsupported-python-workspace-pattern": "membership",
	// Python dependencies.
	"dynamic-python-requirements":          "dependencies",
	"duplicate-uv-source-name":             "dependencies",
	"empty-uv-source":                      "dependencies",
	"external-python-backend":              "dependencies",
	"missing-static-python-requirements":   "dependencies",
	"python-requirement-source-withheld":   "dependencies",
	"python-requirements-constraint":       "dependencies",
	"python-requirements-line-limit":       "dependencies",
	"unsupported-python-group-include":     "dependencies",
	"unsupported-python-group-item":        "dependencies",
	"unsupported-python-marker":            "dependencies",
	"unsupported-python-requirement":       "dependencies",
	"unsupported-python-requirements-line": "dependencies",
	"unsupported-uv-archive-source":        "dependencies",
	"unsupported-uv-source":                "dependencies",
	// Python non-structural (names, metadata, setup).
	"ambiguous-python-setup":           "both",
	"conflicting-python-metadata":      "both",
	"dynamic-python-name":              "both",
	"dynamic-python-setup-metadata":    "both",
	"invalid-python-array":             "both",
	"invalid-python-array-item":        "both",
	"invalid-python-dynamic":           "both",
	"invalid-python-extra":             "both",
	"invalid-python-group":             "both",
	"invalid-python-manifest":          "both",
	"invalid-python-metadata":          "both",
	"invalid-python-project":           "both",
	"invalid-python-requirement":       "both",
	"invalid-python-requirements":      "both",
	"invalid-python-script-name":       "none",
	"invalid-python-setup":             "both",
	"invalid-python-setup-cfg":         "both",
	"invalid-python-table":             "both",
	"invalid-uv-managed":               "both",
	"invalid-uv-package":               "both",
	"invalid-uv-source":                "both",
	"invalid-uv-source-name":           "both",
	"missing-python-name":              "both",
	"missing-python-setup-cfg-name":    "both",
	"python-array-limit":               "both",
	"unsupported-python-backend":       "both",
	"unsupported-python-script-target": "none",
	"unsupported-python-setup":         "both",
	// Maven/Gradle (jvm.go diagnostics forwarded via collect.go).
	// Note: jvm.go uses underscores in its codes, not dashes.
	"declaration-budget-exceeded": "both",
	"invalid-module":              "membership",
	"invalid_manifest":            "both",
	"manifest-too-large":          "both",
	"unresolved_build_script":     "both",
	"unsupported_manifest":        "both",
	// .NET / solution (dotnet.go).
	"invalid-json":                  "both",
	"invalid-solution":              "both",
	"invalid-solution-filter":       "both",
	"invalid-solution-project":      "membership",
	"unsupported-solution-encoding": "both",
	// Reference graph (graph.go).
	"ambiguous-graph-project-id":      "both",
	"invalid-graph-project-id":        "both",
	"unattributed-project-references": "dependencies",
	// Shared / cross-ecosystem references.
	"invalid-reference-target":  "dependencies",
	"legacy-reference-withheld": "dependencies",
	"legacy-value-withheld":     "both",
	// Declaration infrastructure (declarations/collect.go).
	"declaration-limit": "both",
	"file-read-error":   "both",
	"read-error":        "both",
	"invalid-manifest":  "both",
	"invalid-xml":       "both",
	"unexpected-root":   "both",
	// Non-structural (build/toolchain only).
	"clojure-name-from-directory":           "none",
	"erlang-name-from-directory":            "none",
	"haskell-stack-name-from-directory":     "none",
	"invalid-autoconf":                      "both",
	"invalid-bazel-manifest":                "both",
	"invalid-cargo-declaration":             "both",
	"invalid-cargo-toml":                    "both",
	"invalid-clojure-deps":                  "both",
	"invalid-clojure-project":               "both",
	"invalid-cmake":                         "both",
	"invalid-deno-config":                   "both",
	"invalid-elixir-mix":                    "both",
	"invalid-erlang-rebar":                  "both",
	"invalid-go-manifest":                   "both",
	"invalid-haskell-cabal":                 "both",
	"invalid-haskell-stack":                 "both",
	"invalid-julia-project":                 "both",
	"invalid-kbuild-marker":                 "both",
	"invalid-meson":                         "both",
	"invalid-perl-cpanfile":                 "both",
	"invalid-perl-makefile-pl":              "both",
	"invalid-php-composer":                  "both",
	"invalid-r-description":                 "both",
	"invalid-ruby-gemfile":                  "both",
	"invalid-ruby-gemspec":                  "both",
	"invalid-scala-sbt":                     "both",
	"invalid-swift-package":                 "both",
	"invalid-zig-build":                     "both",
	"missing-autoconf-project-name":         "both",
	"missing-bazel-project-name":            "both",
	"missing-clojure-project-name":          "both",
	"missing-cmake-project-name":            "both",
	"missing-elixir-app-name":               "both",
	"missing-haskell-package-name":          "both",
	"missing-julia-project-name":            "both",
	"missing-meson-project-name":            "both",
	"missing-perl-dist-name":                "both",
	"missing-php-package-name":              "both",
	"missing-r-package-name":                "both",
	"missing-ruby-gem-name":                 "both",
	"missing-scala-project-name":            "both",
	"missing-swift-package-name":            "both",
	"perl-name-from-directory":              "none",
	"zig-name-from-directory":               "none",
	"unsupported-package-manager":           "both",
	"unsupported-package-manager-semantics": "both",
}

// structureEcosystemNames is the ecosystem vocabulary of the manifest table.
var structureEcosystemNames = func() map[string]bool {
	names := map[string]bool{}
	for _, class := range manifestClasses {
		names[class[0]] = true
	}
	return names
}()

func diagnosticRelationshipScope(code string) string {
	if scope, ok := diagnosticRelationshipScopeTable[code]; ok {
		return scope
	}
	// Unknown code: conservatively qualify both scopes.
	return "both"
}

func hasUnappliedSharedMSBuildReferences(report *declarations.Report) bool {
	if report == nil {
		return false
	}
	hasProjectReference := func(refs []declarations.Reference) bool {
		return slices.ContainsFunc(refs, func(ref declarations.Reference) bool { return ref.Kind == "project-reference" })
	}
	for _, project := range report.Projects {
		if strings.Contains(project.Kind, "configuration") && hasProjectReference(project.References) {
			return true
		}
	}
	return false
}

func buildStructure(records []declarations.ProjectRecord, counted []declarations.Project, report *declarations.Report, candidateRoles map[string]int64, inventoryReasons []string, unparsedEcosystems map[string]bool, unparsedWorkspaceReasons map[string]bool, globalCompletenessReasons []string) *StructureReport {
	result := &StructureReport{Populations: []StructurePopulation{}, Coverage: []StructureCoverage{}, WorkspaceGroups: []StructureGroup{}, SolutionGroups: []StructureGroup{}, EntryPoints: []StructureEntryPoint{}}
	rows := map[string]int64{}
	rowScope := map[string]string{}
	rowReasons := map[string]map[string]bool{}
	add := func(pop, eco, role, scope string, reasons []string) {
		key := structurePopulationKey(pop, eco, role)
		rows[key]++
		rowScope[key] = scope
		if len(reasons) > 0 {
			if rowReasons[key] == nil {
				rowReasons[key] = map[string]bool{}
			}
			for _, reason := range reasons {
				rowReasons[key][reason] = true
			}
		}
	}
	for key, n := range candidateRoles {
		parts := strings.Split(key, "\x00")
		if len(parts) == 3 {
			rows[key] += n
			rowScope[key] = "selected filename candidates classified by manifest filename and path role"
			if len(inventoryReasons) > 0 {
				rowReasons[key] = make(map[string]bool)
				for _, reason := range inventoryReasons {
					rowReasons[key][reason] = true
				}
			}
		}
	}
	projectByID := map[string]declarations.Project{}
	for _, p := range counted {
		projectByID[p.ID] = p
	}
	projectReasons := map[string][]string{}
	membershipReasons := map[string][]string{}
	dependencyReasons := map[string][]string{}
	globalReasons := slices.Clone(globalCompletenessReasons)
	for eco := range unparsedEcosystems {
		projectReasons[eco] = append(projectReasons[eco], "manifest_candidates_unparsed")
		membershipReasons[eco] = append(membershipReasons[eco], "manifest_candidates_unparsed")
		dependencyReasons[eco] = append(dependencyReasons[eco], "manifest_candidates_unparsed")
	}
	// f13: pnpm-workspace.yaml, lerna.json, and rush.json are recognized but
	// not parsed. Their presence means npm workspace membership may be
	// incomplete. Qualify npm membership coverage with an actionable reason
	// so the output states which file caused the lower bound.
	for reason := range unparsedWorkspaceReasons {
		membershipReasons["npm"] = append(membershipReasons["npm"], reason)
	}
	if hasUnappliedSharedMSBuildReferences(report) {
		dependencyReasons["nuget"] = append(dependencyReasons["nuget"], "shared_msbuild_project_references_not_applied")
	}
	for _, d := range report.Diagnostics {
		if d.Path == "" {
			globalReasons = append(globalReasons, "unattributed_declaration_diagnostic")
			continue
		}
		_, eco, _ := classifyManifest(d.Path)
		if eco == "other" {
			globalReasons = append(globalReasons, "unattributed_declaration_diagnostic")
		} else {
			switch diagnosticRelationshipScope(d.Code) {
			case "none":
			case "membership":
				membershipReasons[eco] = append(membershipReasons[eco], "declaration_diagnostics_present")
			case "dependencies":
				dependencyReasons[eco] = append(dependencyReasons[eco], "declaration_diagnostics_present")
			default:
				membershipReasons[eco] = append(membershipReasons[eco], "declaration_diagnostics_present")
				dependencyReasons[eco] = append(dependencyReasons[eco], "declaration_diagnostics_present")
			}
		}
	}
	seenProjectRow := map[string]bool{}
	for _, record := range records {
		if !record.Parsed || record.Project.ID == "" {
			continue
		}
		p := record.Project
		eco := projectEcosystem(p)
		if p.Kind == "gradle" && slices.ContainsFunc(p.Requirements, func(req declarations.Requirement) bool {
			return req.Kind == "build-evaluation" && req.State == "unresolved"
		}) {
			// Static Gradle declarations are retained, but project dependency
			// relationships require evaluating the build. Keep exact project
			// counts while qualifying graph scopes for this ecosystem only.
			// Membership is also affected: module membership edges reference
			// build.gradle paths, which require build evaluation to resolve to
			// project IDs, so a Gradle workspace with unresolved build evaluation
			// may have zero confirmed members.
			membershipReasons[eco] = append(membershipReasons[eco], "build_declarations_require_evaluation")
			dependencyReasons[eco] = append(dependencyReasons[eco], "build_declarations_require_evaluation")
		}
		if !record.Complete {
			projectReasons[eco] = append(projectReasons[eco], "parsed_project_observations_incomplete")
			membershipReasons[eco] = append(membershipReasons[eco], "parsed_project_observations_incomplete")
			dependencyReasons[eco] = append(dependencyReasons[eco], "parsed_project_observations_incomplete")
		}
		if _, counted := projectByID[p.ID]; !counted || seenProjectRow[p.ID] {
			continue
		}
		seenProjectRow[p.ID] = true
		reasons := slices.Clone(globalReasons)
		reasons = append(reasons, projectReasons[eco]...)
		add("parsed_projects", eco, pathrole.Of(p.ID), "parsed counted project records", uniqueSorted(reasons))
	}
	rootRoles := map[string]map[string][]string{}
	for _, p := range counted {
		eco := projectEcosystem(p)
		if rootRoles[eco] == nil {
			rootRoles[eco] = map[string][]string{}
		}
		rootRoles[eco][p.Root] = append(rootRoles[eco][p.Root], p.ID)
	}
	for eco, roots := range rootRoles {
		for _, ids := range roots {
			reasons := append(slices.Clone(globalReasons), projectReasons[eco]...)
			add("distinct_roots", eco, pathrole.Of(ids...), "distinct roots of counted projects within this ecosystem; a physical root may occur in multiple ecosystems", uniqueSorted(reasons))
		}
	}
	fragment := componentmap.Build(report)
	for eco, reason := range structuralDependencyQualifications(fragment, counted) {
		dependencyReasons[eco] = append(dependencyReasons[eco], reason...)
	}
	groupBuild := buildStructureGroups(fragment, report, records, globalReasons, membershipReasons)
	result.WorkspaceGroups, result.OmittedWorkspaceGroups = groupBuild.workspaces, groupBuild.omittedWorkspaces
	result.SolutionGroups, result.OmittedSolutionGroups = groupBuild.solutions, groupBuild.omittedSolutions
	result.WorkspaceGroupCount = groupBuild.workspaceCount
	result.SolutionGroupCount = groupBuild.solutionCount
	for _, group := range groupBuild.all {
		population := "workspace_groups"
		if group.Kind == "solution" {
			population = "solution_groups"
		}
		reasons := append(slices.Clone(globalReasons), projectReasons[group.Ecosystem]...)
		add(population, group.Ecosystem, group.Role, "explicitly declared workspace/module/solution group records retained by the declaration parser", uniqueSorted(reasons))
	}
	// F2: collect cross-ecosystem reasons for graph metric completeness.
	allProjectsReasons := slices.Clone(globalReasons)
	for _, reasons := range projectReasons {
		allProjectsReasons = append(allProjectsReasons, reasons...)
	}
	allProjectsReasons = uniqueSorted(allProjectsReasons)
	projectsCompleteness := "complete"
	if len(allProjectsReasons) > 0 {
		projectsCompleteness = "lower_bound"
	}
	allDepReasons := slices.Clone(globalReasons)
	for _, reasons := range dependencyReasons {
		for _, reason := range reasons {
			// A self-reference cannot hide an edge between two projects.
			if reason != "self_reference_qualified" {
				allDepReasons = append(allDepReasons, reason)
			}
		}
	}
	allDepReasons = uniqueSorted(allDepReasons)
	result.Dependencies = buildStructureDependencies(fragment, counted, projectsCompleteness, allProjectsReasons, allDepReasons)
	result.Coverage = structureCoverage(rows, membershipReasons, dependencyReasons, globalReasons)
	result.Coverage = append(result.Coverage, StructureCoverage{Scope: "entry_points", Ecosystem: "all", Status: "partial", Reasons: []string{"entry_point_observer_not_run"}})
	slices.SortFunc(result.Coverage, func(a, b StructureCoverage) int {
		return strings.Compare(a.Scope+"\x00"+a.Ecosystem, b.Scope+"\x00"+b.Ecosystem)
	})
	for _, key := range sortedKeys(rows) {
		parts := strings.Split(key, "\x00")
		reasons := sortedBoolKeys(rowReasons[key])
		result.Populations = append(result.Populations, StructurePopulation{Population: parts[0], Ecosystem: parts[1], Role: parts[2], Metric: metric(rows[key], rowScope[key], len(reasons) != 0, reasons)})
	}
	return result
}

type structureGroups struct {
	all, workspaces, solutions          []StructureGroup
	omittedWorkspaces, omittedSolutions int64
	workspaceCount, solutionCount       int64
}
type groupAccumulator struct {
	id, ecosystem, role, kind string
	members                   map[string]bool
	unresolved                []StructureUnresolvedMember
}

func buildStructureGroups(fragment componentmap.Fragment, report *declarations.Report, records []declarations.ProjectRecord, globalReasons []string, ecosystemReasons map[string][]string) structureGroups {
	components := map[string]componentmap.Component{}
	for _, c := range fragment.Components {
		components[c.Key] = c
	}
	groups := map[string]*groupAccumulator{}
	for _, rel := range fragment.Relationships {
		if rel.Type != "member_of" {
			continue
		}
		c, ok := components[rel.To]
		if !ok {
			continue
		}
		kind := "workspace"
		if rel.DeclarationKind == "solution-member" {
			kind = "solution"
		}
		key := kind + "\x00" + c.Key
		g := groups[key]
		if g == nil {
			g = &groupAccumulator{id: c.Key, ecosystem: canonicalEcosystem(c.Manifest, c.Ecosystem), role: pathrole.Of(c.Manifest), kind: kind, members: map[string]bool{}}
			groups[key] = g
		}
		if rel.Coverage != "complete" {
			state := rel.State
			resolution := "qualified_member"
			if rel.Condition != "" || state == "conditional" {
				state = "conditional"
				resolution = "conditional"
			} else if state == "" {
				state = "unresolved"
			}
			g.unresolved = append(g.unresolved, StructureUnresolvedMember{Target: rel.From, State: state, Resolution: resolution, Evidence: rel.Evidence})
			continue
		}
		g.members[rel.From] = true
	}
	// Empty, explicitly declared roots still describe groups. Membership edges
	// are not required for an empty npm/uv/Cargo/Go workspace or solution.
	for _, p := range report.Projects {
		kind, declared := declaredGroupKind(p)
		if !declared {
			continue
		}
		c, ok := components[p.ID]
		if !ok {
			continue
		}
		key := kind + "\x00" + p.ID
		if groups[key] == nil {
			groups[key] = &groupAccumulator{id: p.ID, ecosystem: canonicalEcosystem(c.Manifest, c.Ecosystem), role: pathrole.Of(c.Manifest), kind: kind, members: map[string]bool{}}
		}
	}
	// Maven aggregators can explicitly declare an empty <modules/> element;
	// no public reference exists in that case, so retain the parser-private bit
	// only when project records were requested by the assessment caller.
	for _, record := range records {
		if !record.Parsed || !record.WorkspaceDeclared || record.Project.Kind != "maven" {
			continue
		}
		id := record.Project.ID
		c, ok := components[id]
		if !ok {
			continue
		}
		key := "workspace\x00" + id
		if groups[key] == nil {
			groups[key] = &groupAccumulator{id: id, ecosystem: canonicalEcosystem(c.Manifest, c.Ecosystem), role: pathrole.Of(c.Manifest), kind: "workspace", members: map[string]bool{}}
		}
	}
	for _, q := range fragment.QualifiedReferences {
		if !isWorkspaceRelation(q.DeclarationKind) {
			continue
		}
		id := q.From
		c, ok := components[id]
		if !ok {
			continue
		}
		kind := "workspace"
		if q.DeclarationKind == "solution-member" {
			kind = "solution"
		}
		key := kind + "\x00" + id
		g := groups[key]
		if g == nil {
			g = &groupAccumulator{id: id, ecosystem: canonicalEcosystem(c.Manifest, c.Ecosystem), role: pathrole.Of(c.Manifest), kind: kind, members: map[string]bool{}}
			groups[key] = g
		}
		resolution := qualifiedResolution(q.Reason, q.TargetStatus)
		g.unresolved = append(g.unresolved, StructureUnresolvedMember{Target: q.Target, Value: q.Value, State: defaultString(q.State, "unresolved"), Resolution: resolution, Reason: q.Reason, Evidence: q.Evidence})
	}
	all := make([]StructureGroup, 0, len(groups))
	for _, g := range groups {
		members := sortedBoolKeys(g.members)
		unresolved := g.unresolved
		slices.SortFunc(unresolved, func(a, b StructureUnresolvedMember) int {
			return strings.Compare(unresolvedMemberKey(a), unresolvedMemberKey(b))
		})
		unresolved = slices.CompactFunc(unresolved, func(a, b StructureUnresolvedMember) bool {
			return unresolvedMemberKey(a) == unresolvedMemberKey(b)
		})
		ug := StructureGroup{ID: g.id, Ecosystem: g.ecosystem, Role: g.role, Kind: g.kind, MemberCount: int64(len(members)), Members: takeStrings(members, StructureMemberLimit), OmittedMembers: int64(max(0, len(members)-StructureMemberLimit)), UnresolvedMemberCount: int64(len(unresolved)), UnresolvedMembers: takeUnresolved(unresolved, StructureMemberLimit), OmittedUnresolvedMembers: int64(max(0, len(unresolved)-StructureMemberLimit))}
		scope := "workspace_membership"
		if g.kind == "solution" {
			scope = "solution_membership"
		}
		reasons := uniqueSorted(append(slices.Clone(globalReasons), ecosystemReasons[g.ecosystem]...))
		// f12: solution/workspace members whose project type is not parsed by the
		// declarations layer appear as qualified unresolved members with reason
		// "target_not_a_retained_component". Propagate this into the membership
		// coverage so the group shows "partial" with an actionable reason.
		for _, um := range unresolved {
			if um.Reason == "target_not_a_retained_component" {
				reasons = append(reasons, "member_project_type_not_retained")
				break
			}
		}
		reasons = uniqueSorted(reasons)
		ug.MembershipCoverage = structureCoverageEntry(scope, g.ecosystem, reasons)
		all = append(all, ug)
	}
	slices.SortFunc(all, func(a, b StructureGroup) int { return strings.Compare(groupKey(a), groupKey(b)) })
	result := structureGroups{all: all, workspaces: []StructureGroup{}, solutions: []StructureGroup{}}
	for _, g := range all {
		if g.Kind == "solution" {
			result.solutionCount++
			result.solutions = append(result.solutions, g)
		} else {
			result.workspaceCount++
			result.workspaces = append(result.workspaces, g)
		}
	}
	if len(result.workspaces) > StructureGroupLimit {
		result.omittedWorkspaces = int64(len(result.workspaces) - StructureGroupLimit)
		result.workspaces = result.workspaces[:StructureGroupLimit]
	}
	if len(result.solutions) > StructureGroupLimit {
		result.omittedSolutions = int64(len(result.solutions) - StructureGroupLimit)
		result.solutions = result.solutions[:StructureGroupLimit]
	}
	return result
}

func declaredGroupKind(p declarations.Project) (string, bool) {
	if p.Kind == "solution" || p.Kind == "go-workspace" || p.Kind == "cargo-workspace" || p.Kind == "python-workspace" {
		if p.Kind == "solution" {
			return "solution", true
		}
		return "workspace", true
	}
	if strings.HasPrefix(strings.ToLower(path.Base(p.ID)), "settings.gradle") {
		return "workspace", true
	}
	for _, req := range p.Requirements {
		if req.Kind == "npm-workspace-root" && req.State != "missing" {
			return "workspace", true
		}
	}
	return "", false
}

func buildStructureDependencies(fragment componentmap.Fragment, projects []declarations.Project, projectsCompleteness string, projectsReasons, allDependencyReasons []string) StructureDependencies {
	vertices := map[string]declarations.Project{}
	for _, p := range projects {
		vertices[p.ID] = p
	}
	mavenCoordinates := newMavenCoordinateIndex(projects)
	// F2: dependencies.projects carries the same completeness and reasons as
	// the top-level projects metric.
	projectsMetric := metricC(int64(len(vertices)), "counted parsed projects used as dependency graph vertices", projectsCompleteness, projectsReasons)
	deps := StructureDependencies{Projects: projectsMetric, QualifiedReferences: []StructureQualifiedCount{}, Edges: []StructureEdge{}, Components: []StructureConnectedComponent{}}
	edges := map[string]StructureEdge{}
	qualified := map[string]int64{}
	// qualifiedPairs joins two counted projects through a qualified local
	// reference whose target is one retained project.
	qualifiedPairs := map[[2]string]bool{}
	addPair := func(from, to string) {
		_, fok := vertices[from]
		_, tok := vertices[to]
		if from != to && fok && tok {
			qualifiedPairs[[2]string{from, to}] = true
		}
	}
	reactors := unconditionalMavenReactors(fragment)
	mavenRelationships := map[string]bool{}
	for _, rel := range fragment.Relationships {
		if rel.Type != "depends_on_local" {
			continue
		}
		if rel.DeclarationKind == "maven-sibling-dependency" {
			mavenRelationships[rel.From+"\x00"+rel.To] = true
		}
		from, fok := vertices[rel.From]
		_, tok := vertices[rel.To]
		if !fok || !tok {
			continue
		}
		eco := projectEcosystem(from)
		if rel.DeclarationKind == "maven-sibling-dependency" && mavenCoordinates.caseMismatch(rel.From, rel.To) {
			state := rel.State
			if rel.Condition != "" || state == "conditional" {
				state = "conditional"
			} else if state == "" {
				state = "declared"
			}
			qualified[qualifiedCountKey(eco, rel.DeclarationKind, state, "coordinate_case_mismatch")]++
			continue
		}
		if rel.DeclarationKind == "maven-sibling-dependency" && !sameMavenReactor(reactors, rel.From, rel.To) {
			state := rel.State
			if rel.Condition != "" || state == "conditional" {
				state = "conditional"
			} else if state == "" {
				state = "declared"
			}
			qualified[qualifiedCountKey(eco, rel.DeclarationKind, state, "coordinate_match_without_declared_reactor")]++
			addPair(rel.From, rel.To)
			continue
		}
		if rel.DeclarationKind == "maven-sibling-dependency" {
			state, resolution, _, unproven := qualifyMavenRelationship(mavenCoordinates, rel)
			if unproven {
				qualified[qualifiedCountKey(eco, rel.DeclarationKind, state, resolution)]++
				if !strings.Contains(resolution, "mismatch") {
					addPair(rel.From, rel.To)
				}
				continue
			}
		}
		// A Maven parent found by path counts only when the coordinates the
		// child declares match the parent POM. componentmap already turns a
		// proven mismatch into a qualified reference; an unconditional parent
		// whose coordinates are not literal is counted here with that reason.
		if rel.DeclarationKind == "parent" && rel.Condition == "" && rel.State != "conditional" {
			if reason := componentmap.MavenParentCheck(mavenCoordinates.byID[rel.From], mavenCoordinates.byID[rel.To]); reason != "" {
				qualified[qualifiedCountKey(eco, rel.DeclarationKind, defaultString(rel.State, "unresolved"), reason)]++
				continue
			}
		}
		if rel.Coverage == "complete" {
			e := StructureEdge{From: rel.From, To: rel.To, Ecosystem: eco, Kind: rel.DeclarationKind, Evidence: rel.Evidence}
			key := structureEdgeKey(e)
			if prior, ok := edges[key]; !ok || e.Kind+"\x00"+e.Evidence < prior.Kind+"\x00"+prior.Evidence {
				edges[key] = e
			}
		} else {
			state := rel.State
			if rel.Condition != "" || state == "conditional" {
				state = "conditional"
			} else if state == "" {
				state = "unresolved"
			}
			key := qualifiedCountKey(eco, rel.DeclarationKind, state, "qualified_target")
			qualified[key]++
			addPair(rel.From, rel.To)
		}
	}
	// componentmap's legacy Maven matcher lowercases only the declared
	// dependency GA while its target index preserves Maven's case-sensitive
	// coordinates. Recover exact-case matches here without changing that shared
	// map contract. Only an explicit unconditional reactor and matching known
	// versions establish a definite local edge.
	for _, project := range projects {
		if project.Kind != "maven" {
			continue
		}
		for _, req := range project.Requirements {
			if req.Kind != "maven-dependency" {
				continue
			}
			wanted := mavenDependencyGA(req.Value)
			matches := mavenCoordinates.exact[wanted]
			if len(matches) == 0 {
				folded := mavenCoordinates.folded[strings.ToLower(wanted)]
				if len(folded) == 0 {
					continue
				}
				if len(folded) > 1 {
					state := mavenRequirementState(req)
					qualified[qualifiedCountKey("maven", "maven-sibling-dependency", state, "ambiguous_coordinate_match")]++
					continue
				}
				to := folded[0]
				if mavenRelationships[project.ID+"\x00"+to] {
					continue
				}
				qualified[qualifiedCountKey("maven", "maven-sibling-dependency", mavenRequirementState(req), "coordinate_case_mismatch")]++
				continue
			}
			if len(matches) > 1 {
				state := mavenRequirementState(req)
				qualified[qualifiedCountKey("maven", "maven-sibling-dependency", state, "ambiguous_coordinate_match")]++
				continue
			}
			to := matches[0]
			if to == project.ID || mavenRelationships[project.ID+"\x00"+to] {
				continue
			}
			if !sameMavenReactor(reactors, project.ID, to) {
				qualified[qualifiedCountKey("maven", "maven-sibling-dependency", mavenRequirementState(req), "coordinate_match_without_declared_reactor")]++
				addPair(project.ID, to)
				continue
			}
			target := mavenCoordinates.byID[to]
			depVersion := mavenDependencyVersion(req.Value)
			targetVersion := mavenDeclaredVersion(target)
			if depVersion != "" && targetVersion != "" && depVersion != targetVersion {
				continue
			}
			if req.State != "declared" || req.Condition != "" || depVersion == "" || targetVersion == "" {
				state, resolution := mavenRequirementState(req), "qualified_target"
				if req.Condition != "" || req.State == "conditional" {
					state, resolution = "conditional", "conditional"
				}
				qualified[qualifiedCountKey("maven", "maven-sibling-dependency", state, resolution)]++
				addPair(project.ID, to)
				continue
			}
			e := StructureEdge{From: project.ID, To: to, Ecosystem: "maven", Kind: "maven-sibling-dependency", Evidence: req.Evidence}
			edges[structureEdgeKey(e)] = e
		}
	}
	// An assembly reference whose simple name is one in-repository project's
	// assembly name may be resolved to that project's output by custom build
	// logic. It stays qualified: the build may equally resolve a package, a
	// framework assembly, or a binary with the same name.
	assemblies := newAssemblyNameIndex(projects)
	for _, project := range projects {
		for _, req := range project.Requirements {
			if req.Kind != "assembly-reference" {
				continue
			}
			matches := assemblies.matches(project.ID, req.Value)
			state := mavenRequirementState(req)
			switch len(matches) {
			case 0:
			case 1:
				qualified[qualifiedCountKey(projectEcosystem(project), "assembly-reference", state, "assembly_name_match")]++
				addPair(project.ID, matches[0])
			default:
				qualified[qualifiedCountKey(projectEcosystem(project), "assembly-reference", state, "ambiguous_assembly_name_match")]++
			}
		}
	}
	// componentmap intentionally suppresses self edges. Preserve explicit
	// self-targeting local references in the structural view as qualified facts.
	for _, p := range projects {
		for _, ref := range p.References {
			if ref.Target != p.ID || !isStructureLocalRelation(ref.Kind) || ref.TargetStatus == "missing" || ref.TargetStatus == "external" {
				continue
			}
			state := ref.State
			resolution := "self_reference"
			if ref.Condition != "" || state == "conditional" {
				state = "conditional"
			} else if state == "" {
				state = "declared"
			}
			qualified[qualifiedCountKey(projectEcosystem(p), ref.Kind, state, resolution)]++
		}
	}
	for _, q := range fragment.QualifiedReferences {
		if !isStructureLocalRelation(q.DeclarationKind) {
			continue
		}
		from, ok := vertices[q.From]
		if !ok {
			continue
		}
		resolution := qualifiedResolution(q.Reason, q.TargetStatus)
		key := qualifiedCountKey(projectEcosystem(from), q.DeclarationKind, defaultString(q.State, "unresolved"), resolution)
		qualified[key]++
		if q.DeclarationKind == "go-local-replacement" && q.Reason == "go_replacement_activation_unresolved" {
			addPair(q.From, q.Target)
		}
	}
	edgeList := make([]StructureEdge, 0, len(edges))
	adjacency := map[string]map[string]bool{}
	for id := range vertices {
		adjacency[id] = map[string]bool{}
	}
	for _, e := range edges {
		edgeList = append(edgeList, e)
		adjacency[e.From][e.To] = true
		adjacency[e.To][e.From] = true
	}
	slices.SortFunc(edgeList, func(a, b StructureEdge) int { return strings.Compare(structureEdgeKey(a), structureEdgeKey(b)) })
	// F2: definite_edges is lower_bound whenever any dependency coverage reason
	// applies; definite edges are sound, so missing declarations can only add edges.
	if len(allDependencyReasons) > 0 {
		deps.DefiniteEdges = metricC(int64(len(edgeList)), "unique unconditional local dependency edges between counted parsed projects with present parsed targets; observed declarations only", "lower_bound", allDependencyReasons)
	} else {
		deps.DefiniteEdges = metric(int64(len(edgeList)), "unique unconditional local dependency edges between counted parsed projects with present parsed targets; observed declarations only", false, []string{})
	}
	for _, key := range sortedKeys(qualified) {
		p := strings.Split(key, "\x00")
		deps.QualifiedReferences = append(deps.QualifiedReferences, StructureQualifiedCount{Ecosystem: p[0], Kind: p[1], State: p[2], Resolution: p[3], Count: qualified[key]})
		deps.QualifiedReferenceCount += qualified[key]
	}
	deps.QualifiedGroupCount = int64(len(deps.QualifiedReferences))
	if len(deps.QualifiedReferences) > StructureQualifiedLimit {
		deps.OmittedQualified = int64(len(deps.QualifiedReferences) - StructureQualifiedLimit)
		for _, q := range deps.QualifiedReferences[StructureQualifiedLimit:] {
			deps.OmittedQualifiedReferenceCount += q.Count
		}
		deps.QualifiedReferences = deps.QualifiedReferences[:StructureQualifiedLimit]
	}
	deps.Edges = takeEdges(edgeList, StructureEdgeLimit)
	deps.OmittedEdges = int64(len(edgeList) - len(deps.Edges))
	components, componentCount := weakComponents(adjacency, edgeList)
	// F2: connected_groups completeness depends on vertex and edge completeness.
	//   upper_bound: vertex set is complete but edges may be missing; missing
	//     edges can only merge groups, so the true count is at most this value.
	//   observed_only: vertices are also incomplete; count may be higher or lower.
	//   complete: both vertex set and edge set are fully observed.
	switch {
	case projectsCompleteness != "complete":
		allGroupReasons := uniqueSorted(append(slices.Clone(projectsReasons), allDependencyReasons...))
		deps.ConnectedGroups = metricC(componentCount, "weakly connected groups in the retained observed dependency graph, including isolated counted projects", "observed_only", allGroupReasons)
	case len(allDependencyReasons) > 0:
		deps.ConnectedGroups = metricC(componentCount, "weakly connected groups in the retained observed dependency graph, including isolated counted projects", "upper_bound", allDependencyReasons)
	default:
		deps.ConnectedGroups = metric(componentCount, "weakly connected groups in the retained observed dependency graph, including isolated counted projects", false, []string{})
	}
	deps.Components = components
	deps.OmittedComponents = componentCount - int64(len(deps.Components))
	for pair := range qualifiedPairs {
		adjacency[pair[0]][pair[1]] = true
		adjacency[pair[1]][pair[0]] = true
	}
	_, withQualified := weakComponents(adjacency, nil)
	scope := "weakly connected groups when definite edges are joined by every qualified local reference whose target is one retained project; with connected_groups it brackets the true count when the only missing dependency evidence is those references"
	if len(allDependencyReasons) == 0 && projectsCompleteness == "complete" {
		deps.ConnectedGroupsWithQualified = metric(withQualified, scope, false, []string{})
	} else {
		deps.ConnectedGroupsWithQualified = metricC(withQualified, scope, "observed_only", uniqueSorted(append(slices.Clone(projectsReasons), allDependencyReasons...)))
	}
	return deps
}

// assemblyNameIndex maps the assembly names of parsed MSBuild projects to
// their project IDs. A project's assembly name is its one literal,
// unconditional AssemblyName property, or else its project file name; a
// conditional or expression-valued AssemblyName leaves it unindexed.
type assemblyNameIndex map[string][]string

func newAssemblyNameIndex(projects []declarations.Project) assemblyNameIndex {
	index := assemblyNameIndex{}
	for _, p := range projects {
		if p.Kind != "dotnet" {
			continue
		}
		ext := strings.ToLower(path.Ext(p.ID))
		if ext != ".csproj" && ext != ".vbproj" && ext != ".fsproj" {
			continue
		}
		name := strings.TrimSuffix(path.Base(p.ID), path.Ext(p.ID))
		var declared []declarations.Requirement
		for _, req := range p.Requirements {
			if req.Kind == "assembly-name" {
				declared = append(declared, req)
			}
		}
		if len(declared) > 0 {
			if len(declared) != 1 || declared[0].State != "declared" || declared[0].Condition != "" || strings.ContainsAny(declared[0].Value, "$@%") {
				continue
			}
			name = declared[0].Value
		}
		key := strings.ToLower(name)
		index[key] = append(index[key], p.ID)
	}
	return index
}

// matches returns the other projects whose assembly name equals name. .NET
// assembly names compare case-insensitively.
func (x assemblyNameIndex) matches(self, name string) []string {
	var out []string
	for _, id := range x[strings.ToLower(strings.TrimSpace(name))] {
		if id != self {
			out = append(out, id)
		}
	}
	return out
}

// A Maven reactor group exists only where an explicit module relationship is
// unconditional. The group includes its aggregator and explicitly named
// modules. Directory proximity and coordinates do not establish membership.
func unconditionalMavenReactors(fragment componentmap.Fragment) map[string]map[string]bool {
	groups := map[string]map[string]bool{}
	for _, rel := range fragment.Relationships {
		if rel.Type != "member_of" || rel.DeclarationKind != "module" || rel.Coverage != "complete" {
			continue
		}
		if groups[rel.To] == nil {
			groups[rel.To] = map[string]bool{rel.To: true}
		}
		groups[rel.To][rel.From] = true
	}
	// Compute transitive closure: a reactor is the root aggregator plus all
	// transitively declared unconditional modules, not only direct children.
	changed := true
	for changed {
		changed = false
		for _, rel := range fragment.Relationships {
			if rel.Type != "member_of" || rel.DeclarationKind != "module" || rel.Coverage != "complete" {
				continue
			}
			// If rel.From is already a member of some reactor group,
			// and rel.To is also in that group, this is already handled.
			// But if rel.From is itself the root of another reactor group,
			// merge that group's members into rel.To's group.
			if groups[rel.To] == nil {
				continue
			}
			if subGroup, ok := groups[rel.From]; ok {
				for member := range subGroup {
					if !groups[rel.To][member] {
						groups[rel.To][member] = true
						changed = true
					}
				}
			}
		}
	}
	return groups
}

func sameMavenReactor(groups map[string]map[string]bool, a, b string) bool {
	for _, members := range groups {
		if members[a] && members[b] {
			return true
		}
	}
	return false
}

func mavenDependencyGA(value string) string {
	first := strings.IndexByte(value, ':')
	if first < 1 {
		return ""
	}
	second := strings.IndexByte(value[first+1:], ':')
	if second < 0 {
		return value
	}
	return value[:first+1+second]
}

func mavenProjectCoordinate(project declarations.Project) string {
	group, groupOK := declaredLiteralMavenRequirement(project, "maven-groupId")
	if !groupOK && !hasMavenRequirement(project, "maven-groupId") {
		if parent, ok := declaredLiteralMavenRequirement(project, "maven-parent"); ok {
			parts := strings.Split(parent, ":")
			if len(parts) == 3 && validMavenCoordinatePart(parts[0]) && validMavenCoordinatePart(parts[1]) {
				group, groupOK = parts[0], true
			}
		}
	}
	artifact, artifactOK := declaredLiteralMavenRequirement(project, "maven-artifactId")
	if !groupOK || !artifactOK {
		return ""
	}
	return group + ":" + artifact
}

func declaredLiteralMavenRequirement(project declarations.Project, kind string) (string, bool) {
	value := ""
	found := false
	for _, req := range project.Requirements {
		if req.Kind != kind {
			continue
		}
		if req.State != "declared" || req.Condition != "" || !validLiteralMavenValue(req.Value) {
			return "", false
		}
		if found && value != req.Value {
			return "", false
		}
		value, found = req.Value, true
	}
	return value, found
}

func hasMavenRequirement(project declarations.Project, kind string) bool {
	return slices.ContainsFunc(project.Requirements, func(req declarations.Requirement) bool { return req.Kind == kind })
}

func validMavenCoordinatePart(value string) bool {
	return validLiteralMavenValue(value) && !strings.Contains(value, ":")
}

func validLiteralMavenValue(value string) bool {
	return value != "" && value == strings.TrimSpace(value) && !strings.ContainsAny(value, "$[](),*?")
}

type mavenCoordinateIndex struct {
	exact          map[string][]string
	folded         map[string][]string
	byID           map[string]declarations.Project
	coordinateByID map[string]string
	requested      map[string]map[string]map[string]bool
}

func newMavenCoordinateIndex(projects []declarations.Project) mavenCoordinateIndex {
	index := mavenCoordinateIndex{
		exact:          map[string][]string{},
		folded:         map[string][]string{},
		byID:           make(map[string]declarations.Project, len(projects)),
		coordinateByID: make(map[string]string, len(projects)),
		requested:      map[string]map[string]map[string]bool{},
	}
	for _, project := range projects {
		index.byID[project.ID] = project
		if project.Kind != "maven" {
			continue
		}
		coordinate := mavenProjectCoordinate(project)
		index.coordinateByID[project.ID] = coordinate
		if coordinate == "" {
			continue
		}
		index.exact[coordinate] = append(index.exact[coordinate], project.ID)
		folded := strings.ToLower(coordinate)
		index.folded[folded] = append(index.folded[folded], project.ID)
	}
	for _, project := range projects {
		if project.Kind != "maven" {
			continue
		}
		for _, req := range project.Requirements {
			if req.Kind != "maven-dependency" {
				continue
			}
			coordinate := mavenDependencyGA(req.Value)
			if coordinate == "" {
				continue
			}
			folded := strings.ToLower(coordinate)
			if index.requested[project.ID] == nil {
				index.requested[project.ID] = map[string]map[string]bool{}
			}
			if index.requested[project.ID][folded] == nil {
				index.requested[project.ID][folded] = map[string]bool{}
			}
			index.requested[project.ID][folded][coordinate] = true
		}
	}
	return index
}

func (index mavenCoordinateIndex) caseMismatch(from, to string) bool {
	target := index.coordinateByID[to]
	if target == "" {
		return false
	}
	for requested := range index.requested[from][strings.ToLower(target)] {
		if requested != target {
			return true
		}
	}
	return false
}

func mavenDependencyVersion(value string) string {
	parts := strings.Split(value, ":")
	if len(parts) != 3 || !validMavenCoordinatePart(parts[0]) || !validMavenCoordinatePart(parts[1]) || !validLiteralMavenValue(parts[2]) || strings.EqualFold(parts[2], "LATEST") || strings.EqualFold(parts[2], "RELEASE") {
		return ""
	}
	return parts[2]
}

func mavenDeclaredVersion(project declarations.Project) string {
	version, ok := declaredLiteralMavenRequirement(project, "maven-version")
	if !ok || strings.Contains(version, ":") || strings.EqualFold(version, "LATEST") || strings.EqualFold(version, "RELEASE") {
		return ""
	}
	return version
}

func mavenRequirementState(req declarations.Requirement) string {
	if req.Condition != "" || req.State == "conditional" {
		return "conditional"
	}
	return defaultString(req.State, "declared")
}

func qualifyMavenRelationship(index mavenCoordinateIndex, rel componentmap.Relationship) (state, resolution, reason string, unproven bool) {
	state, resolution = defaultString(rel.State, "unresolved"), "qualified_target"
	if rel.State != "declared" || rel.Condition != "" {
		if rel.State == "conditional" || rel.Condition != "" {
			state, resolution = "conditional", "conditional"
		}
		return state, resolution, "qualified_maven_dependency_reference", true
	}
	source, sourceOK := index.byID[rel.From]
	target, targetOK := index.byID[rel.To]
	if !sourceOK || !targetOK || index.coordinateByID[rel.To] == "" {
		return state, resolution, "maven_target_coordinate_unresolved", true
	}
	targetCoordinate := index.coordinateByID[rel.To]
	var matched *declarations.Requirement
	for i := range source.Requirements {
		req := &source.Requirements[i]
		if req.Kind != "maven-dependency" || mavenDependencyGA(req.Value) != targetCoordinate {
			continue
		}
		if rel.Evidence != "" && req.Evidence != rel.Evidence {
			continue
		}
		matched = req
		break
	}
	if matched == nil {
		return state, resolution, "maven_dependency_coordinate_unresolved", true
	}
	state = mavenRequirementState(*matched)
	if matched.State != "declared" || matched.Condition != "" {
		if matched.Condition != "" || matched.State == "conditional" {
			state, resolution = "conditional", "conditional"
		}
		return state, resolution, "qualified_maven_dependency_reference", true
	}
	dependencyVersion := mavenDependencyVersion(matched.Value)
	targetVersion := mavenDeclaredVersion(target)
	if dependencyVersion == "" || targetVersion == "" {
		return state, resolution, "maven_dependency_version_unresolved", true
	}
	if dependencyVersion != targetVersion {
		return state, resolution, "maven_dependency_version_mismatch", true
	}
	return state, "", "", false
}

func structuralDependencyQualifications(fragment componentmap.Fragment, projects []declarations.Project) map[string][]string {
	qualified := map[string][]string{}
	reactors := unconditionalMavenReactors(fragment)
	coordinates := newMavenCoordinateIndex(projects)
	counted := make(map[string]declarations.Project, len(projects))
	for _, project := range projects {
		counted[project.ID] = project
	}
	for _, rel := range fragment.Relationships {
		if rel.Type == "depends_on_local" && rel.Coverage != "complete" {
			if source, sourceOK := counted[rel.From]; sourceOK {
				if _, targetOK := counted[rel.To]; targetOK {
					reason := "qualified_local_dependency_reference"
					switch {
					case rel.DeclarationKind == "gradle-module":
						reason = "build_declarations_require_evaluation"
					case rel.DeclarationKind == "project-reference" && (rel.Condition != "" || rel.State == "conditional"):
						reason = "conditional_project_reference"
					case rel.Condition != "" || rel.State == "conditional":
						reason = "conditional_local_dependency_reference"
					}
					qualified[projectEcosystem(source)] = append(qualified[projectEcosystem(source)], reason)
				}
			}
		}
		if rel.Type == "depends_on_local" && rel.DeclarationKind == "maven-sibling-dependency" {
			if coordinates.caseMismatch(rel.From, rel.To) {
				qualified["maven"] = append(qualified["maven"], "coordinate_case_mismatch")
			} else if !sameMavenReactor(reactors, rel.From, rel.To) {
				qualified["maven"] = append(qualified["maven"], "coordinate_match_without_declared_reactor")
			} else if _, _, reason, unproven := qualifyMavenRelationship(coordinates, rel); unproven {
				qualified["maven"] = append(qualified["maven"], reason)
			}
		}
	}
	for _, q := range fragment.QualifiedReferences {
		if q.DeclarationKind == "go-local-replacement" && q.Reason == "go_replacement_activation_unresolved" {
			qualified["go"] = append(qualified["go"], "go_replacement_activation_unresolved")
		}
	}
	relationships := map[string]bool{}
	for _, rel := range fragment.Relationships {
		if rel.Type == "depends_on_local" && rel.DeclarationKind == "maven-sibling-dependency" {
			relationships[rel.From+"\x00"+rel.To] = true
		}
	}
	for _, p := range projects {
		if p.Kind != "maven" {
			continue
		}
		for _, req := range p.Requirements {
			if req.Kind != "maven-dependency" {
				continue
			}
			wanted := mavenDependencyGA(req.Value)
			matches := coordinates.exact[wanted]
			if len(matches) > 1 {
				qualified["maven"] = append(qualified["maven"], "ambiguous_coordinate_match")
				continue
			}
			if len(matches) == 0 {
				folded := coordinates.folded[strings.ToLower(wanted)]
				if len(folded) > 1 {
					qualified["maven"] = append(qualified["maven"], "ambiguous_coordinate_match")
				} else if len(folded) == 1 && !relationships[p.ID+"\x00"+folded[0]] {
					qualified["maven"] = append(qualified["maven"], "coordinate_case_mismatch")
				}
				continue
			}
			if len(matches) == 1 && !relationships[p.ID+"\x00"+matches[0]] {
				if !sameMavenReactor(reactors, p.ID, matches[0]) {
					qualified["maven"] = append(qualified["maven"], "coordinate_match_without_declared_reactor")
				} else if mavenDependencyVersion(req.Value) == "" || mavenDeclaredVersion(coordinates.byID[matches[0]]) == "" {
					qualified["maven"] = append(qualified["maven"], "maven_dependency_version_unresolved")
				} else if req.State != "declared" || req.Condition != "" {
					qualified["maven"] = append(qualified["maven"], "qualified_maven_dependency_reference")
				}
			}
		}
	}
	for _, p := range projects {
		for _, ref := range p.References {
			if ref.Target == p.ID && isStructureLocalRelation(ref.Kind) && ref.TargetStatus != "missing" && ref.TargetStatus != "external" {
				qualified[projectEcosystem(p)] = append(qualified[projectEcosystem(p)], "self_reference_qualified")
			}
		}
	}
	assemblies := newAssemblyNameIndex(projects)
	for _, p := range projects {
		for _, req := range p.Requirements {
			if req.Kind == "assembly-reference" && len(assemblies.matches(p.ID, req.Value)) > 0 {
				qualified[projectEcosystem(p)] = append(qualified[projectEcosystem(p)], "assembly_name_references_to_local_projects")
				break
			}
		}
	}
	for eco := range qualified {
		qualified[eco] = uniqueSorted(qualified[eco])
	}
	return qualified
}

func structureCoverage(rows map[string]int64, membershipReasons, dependencyReasons map[string][]string, globalReasons []string) []StructureCoverage {
	entries := map[string]StructureCoverage{}
	ecosystems := map[string]bool{}
	for key := range rows {
		p := strings.Split(key, "\x00")
		if len(p) == 3 {
			ecosystems[p[1]] = true
		}
	}
	for eco := range membershipReasons {
		ecosystems[eco] = true
	}
	for eco := range dependencyReasons {
		ecosystems[eco] = true
	}
	for eco := range ecosystems {
		for _, scope := range []string{"workspace_membership", "solution_membership"} {
			reasons := uniqueSorted(append(slices.Clone(globalReasons), membershipReasons[eco]...))
			key := scope + "\x00" + eco
			entries[key] = structureCoverageEntry(scope, eco, reasons)
		}
		for _, scope := range []string{"project_dependencies", "dependency_connectivity"} {
			reasons := uniqueSorted(append(slices.Clone(globalReasons), dependencyReasons[eco]...))
			key := scope + "\x00" + eco
			entries[key] = structureCoverageEntry(scope, eco, reasons)
		}
	}
	out := make([]StructureCoverage, 0, len(entries))
	for _, entry := range entries {
		out = append(out, entry)
	}
	slices.SortFunc(out, func(a, b StructureCoverage) int {
		return strings.Compare(a.Scope+"\x00"+a.Ecosystem, b.Scope+"\x00"+b.Ecosystem)
	})
	return out
}

func structureCoverageEntry(scope, ecosystem string, reasons []string) StructureCoverage {
	status := "complete"
	if len(reasons) > 0 {
		status = "partial"
	}
	return StructureCoverage{Scope: scope, Ecosystem: ecosystem, Status: status, Reasons: uniqueSorted(slices.Clone(reasons))}
}

func weakComponents(adjacency map[string]map[string]bool, edges []StructureEdge) ([]StructureConnectedComponent, int64) {
	seen := map[string]bool{}
	ids := sortedKeys(adjacency)
	componentOf := make(map[string]int, len(ids))
	result := []StructureConnectedComponent{}
	var componentCount int64
	for _, start := range ids {
		if seen[start] {
			continue
		}
		queue := []string{start}
		seen[start] = true
		keepMembers := componentCount < StructureComponentLimit
		members := []string{}
		var memberCount int64
		for len(queue) > 0 {
			n := queue[0]
			queue = queue[1:]
			memberCount++
			if keepMembers {
				members = append(members, n)
			}
			componentOf[n] = int(componentCount)
			for _, next := range sortedKeys(adjacency[n]) {
				if !seen[next] {
					seen[next] = true
					queue = append(queue, next)
				}
			}
		}
		if keepMembers {
			slices.Sort(members)
			result = append(result, StructureConnectedComponent{ID: members[0], ProjectCount: memberCount, Projects: takeStrings(members, StructureProjectLimit), OmittedProjects: int64(max(0, len(members)-StructureProjectLimit))})
		}
		componentCount++
	}
	for _, edge := range edges {
		idx, ok := componentOf[edge.From]
		if ok && componentOf[edge.To] == idx && idx < len(result) {
			result[idx].EdgeCount++
		}
	}
	return result, componentCount
}

// SetStructureEntryPoints replaces the report's entry-point observations with
// a deterministically sorted, bounded sample. Include unassociated declarations
// by leaving ProjectID empty; State and Reason preserve qualification.
func SetStructureEntryPoints(report *Report, entries []StructureEntryPoint) {
	if report == nil || report.Structure == nil {
		return
	}
	report.Structure.Coverage = slices.DeleteFunc(report.Structure.Coverage, func(row StructureCoverage) bool {
		return row.Scope == "entry_points" && row.Ecosystem == "all"
	})
	report.Structure.Coverage = append(report.Structure.Coverage, StructureCoverage{Scope: "entry_points", Ecosystem: "all", Status: "partial", Reasons: []string{"entry_point_scope_not_established"}})
	slices.SortFunc(report.Structure.Coverage, func(a, b StructureCoverage) int {
		return strings.Compare(a.Scope+"\x00"+a.Ecosystem, b.Scope+"\x00"+b.Ecosystem)
	})
	report.Structure.OmittedEntryPoints = 0
	items := slices.Clone(entries)
	if items == nil {
		items = []StructureEntryPoint{}
	}
	slices.SortFunc(items, func(a, b StructureEntryPoint) int { return strings.Compare(entryPointKey(a), entryPointKey(b)) })
	for i := 1; i < len(items); i++ {
		if entryPointKey(items[i-1]) == entryPointKey(items[i]) {
			items = append(items[:i], items[i+1:]...)
			i--
		}
	}
	rowTotal := int64(len(items))
	associationTotal := int64(0)
	distinctKeys := map[string]struct{}{}
	for _, e := range items {
		distinctKeys[entryPointDeclarationKey(e)] = struct{}{}
		if e.ProjectID != "" {
			associationTotal++
		}
	}
	distinctCount := int64(len(distinctKeys))
	if len(items) > StructureEntryPointLimit {
		report.Structure.OmittedEntryPoints = int64(len(items) - StructureEntryPointLimit)
		items = items[:StructureEntryPointLimit]
	}
	report.Structure.EntryPoints = items
	report.Structure.EntryPointRowCount = rowTotal
	report.Structure.EntryPointAssociationCount = associationTotal
	report.Structure.EntryPointCount = distinctCount
	boundStructureForReport(report)
}

func boundStructureForReport(report *Report) {
	if report == nil || report.Structure == nil {
		return
	}
	used := 0
	for _, v := range report.CandidateEvidence {
		used += encodedSize(v)
	}
	for _, v := range report.ProjectRootEvidence {
		used += encodedSize(v)
	}
	for _, v := range report.WorkspaceEvidence {
		used += encodedSize(v)
	}
	for _, v := range report.LocalDependencyEvidence {
		used += encodedSize(v)
	}
	remaining := MaxEvidenceJSONBytes - used
	if remaining < 0 {
		remaining = 0
	}
	boundStructureEvidence(report.Structure, &remaining)
}

func validateStructure(s *StructureReport, projects, manifestCandidates int64) error {
	if s == nil {
		return fmt.Errorf("missing structural summary")
	}
	if len(s.Populations) > 2048 || len(s.Coverage) > 2048 || len(s.WorkspaceGroups) > StructureGroupLimit || len(s.SolutionGroups) > StructureGroupLimit || len(s.EntryPoints) > StructureEntryPointLimit || len(s.Dependencies.Edges) > StructureEdgeLimit || len(s.Dependencies.Components) > StructureComponentLimit || len(s.Dependencies.QualifiedReferences) > StructureQualifiedLimit {
		return fmt.Errorf("structural summary exceeds a retained item limit")
	}
	last := ""
	for _, p := range s.Populations {
		if p.Population == "" || p.Ecosystem == "" || !projectRoleNames[p.Role] {
			return fmt.Errorf("invalid structural population row")
		}
		key := p.Population + "\x00" + p.Ecosystem + "\x00" + p.Role
		if last != "" && last >= key {
			return fmt.Errorf("structural population rows are not sorted and unique")
		}
		last = key
		if err := validateMetric(p.Metric); err != nil {
			return err
		}
	}
	populationTotals := map[string]int64{}
	for _, p := range s.Populations {
		var ok bool
		populationTotals[p.Population], ok = checkedAdd(populationTotals[p.Population], p.Metric.Count)
		if !ok {
			return fmt.Errorf("structural population totals overflow")
		}
	}
	if populationTotals["filename_candidates"] != manifestCandidates {
		return fmt.Errorf("filename candidate population rows do not reconcile")
	}
	if populationTotals["parsed_projects"] != projects {
		return fmt.Errorf("parsed project population rows do not reconcile")
	}
	if populationTotals["workspace_groups"] != s.WorkspaceGroupCount || populationTotals["solution_groups"] != s.SolutionGroupCount {
		return fmt.Errorf("declared group population rows do not reconcile")
	}
	if err := validateStructureGroups(s.WorkspaceGroups, "workspace", s.WorkspaceGroupCount, s.OmittedWorkspaceGroups); err != nil {
		return err
	}
	if err := validateStructureGroups(s.SolutionGroups, "solution", s.SolutionGroupCount, s.OmittedSolutionGroups); err != nil {
		return err
	}
	d := s.Dependencies
	if err := validateMetric(d.Projects); err != nil {
		return err
	}
	if err := validateMetric(d.DefiniteEdges); err != nil {
		return err
	}
	if err := validateMetric(d.ConnectedGroups); err != nil {
		return err
	}
	if err := validateMetric(d.ConnectedGroupsWithQualified); err != nil {
		return err
	}
	if d.ConnectedGroupsWithQualified.Count > d.ConnectedGroups.Count || d.Projects.Count > 0 && d.ConnectedGroupsWithQualified.Count < 1 {
		return fmt.Errorf("connected groups with qualified references must not exceed connected groups")
	}
	// F2: ConnectedGroups may be upper_bound or observed_only when coverage is
	// incomplete. The count remains exact for the observed graph in all cases.
	if d.Projects.Count != projects {
		return fmt.Errorf("dependency vertices do not match project population")
	}
	if d.OmittedEdges < 0 || d.OmittedComponents < 0 || d.OmittedQualified < 0 || d.QualifiedReferenceCount < 0 || d.OmittedQualifiedReferenceCount < 0 {
		return fmt.Errorf("dependency samples do not reconcile")
	}
	edgeSamples, ok := checkedAdd(int64(len(d.Edges)), d.OmittedEdges)
	componentSamples, componentCountOK := checkedAdd(int64(len(d.Components)), d.OmittedComponents)
	qualifiedGroups, qualifiedGroupsOK := checkedAdd(int64(len(d.QualifiedReferences)), d.OmittedQualified)
	if !ok || edgeSamples != d.DefiniteEdges.Count || !componentCountOK || componentSamples != d.ConnectedGroups.Count || !qualifiedGroupsOK || qualifiedGroups != d.QualifiedGroupCount {
		return fmt.Errorf("dependency samples do not reconcile")
	}
	for i, e := range d.Edges {
		if !validSelectedPath(e.From) || !validSelectedPath(e.To) || e.From == e.To || e.Ecosystem == "" || e.Kind == "" || len(e.From) > MaxEvidencePathBytes || len(e.To) > MaxEvidencePathBytes || len(e.Evidence) > MaxEvidencePathBytes || e.Evidence != "" && !validSelectedPath(e.Evidence) || i > 0 && structureEdgeKey(d.Edges[i-1]) >= structureEdgeKey(e) {
			return fmt.Errorf("invalid or unsorted dependency edge")
		}
	}
	var componentProjects, componentEdges int64
	componentOfSample := map[string]int{}
	componentMembersComplete := d.OmittedComponents == 0
	for i, c := range d.Components {
		projectSamples, projectSamplesOK := checkedAdd(int64(len(c.Projects)), c.OmittedProjects)
		if !validSelectedPath(c.ID) || len(c.ID) > MaxEvidencePathBytes || c.ProjectCount <= 0 || c.EdgeCount < 0 || c.OmittedProjects < 0 || !projectSamplesOK || projectSamples != c.ProjectCount || len(c.Projects) > StructureProjectLimit || i > 0 && d.Components[i-1].ID >= c.ID {
			return fmt.Errorf("invalid connected component")
		}
		if !slices.Contains(c.Projects, c.ID) {
			return fmt.Errorf("connected component ID is not in its retained project sample")
		}
		if c.OmittedProjects > 0 {
			componentMembersComplete = false
		}
		componentProjects, ok = checkedAdd(componentProjects, c.ProjectCount)
		if !ok {
			return fmt.Errorf("connected component project totals overflow")
		}
		componentEdges, ok = checkedAdd(componentEdges, c.EdgeCount)
		if !ok {
			return fmt.Errorf("connected component edge totals overflow")
		}
		for j, p := range c.Projects {
			if !validSelectedPath(p) || len(p) > MaxEvidencePathBytes || j > 0 && c.Projects[j-1] >= p {
				return fmt.Errorf("invalid connected-component members")
			}
			if _, duplicate := componentOfSample[p]; duplicate {
				return fmt.Errorf("connected-component project appears in multiple groups")
			}
			componentOfSample[p] = i
		}
	}
	if componentProjects > d.Projects.Count || componentEdges > d.DefiniteEdges.Count || componentSamples > d.Projects.Count || componentProjects > d.Projects.Count-d.OmittedComponents {
		return fmt.Errorf("connected components exceed observed graph vertices or edges")
	}
	if d.OmittedComponents == 0 && (componentProjects != d.Projects.Count || componentEdges != d.DefiniteEdges.Count) {
		return fmt.Errorf("fully retained connected components do not account for graph vertices or edges")
	}
	for _, edge := range d.Edges {
		from, fromFound := componentOfSample[edge.From]
		to, toFound := componentOfSample[edge.To]
		if fromFound && toFound && from != to {
			return fmt.Errorf("retained dependency edge crosses connected components")
		}
		if componentMembersComplete && (!fromFound || !toFound) {
			return fmt.Errorf("retained dependency edge endpoint is absent from fully retained components")
		}
	}
	if componentMembersComplete && d.OmittedEdges == 0 {
		perComponentEdges := make([]int64, len(d.Components))
		adjacency := make([]map[string][]string, len(d.Components))
		for i, c := range d.Components {
			adjacency[i] = make(map[string][]string, len(c.Projects))
		}
		for _, edge := range d.Edges {
			component := componentOfSample[edge.From]
			perComponentEdges[component]++
			adjacency[component][edge.From] = append(adjacency[component][edge.From], edge.To)
			adjacency[component][edge.To] = append(adjacency[component][edge.To], edge.From)
		}
		for i, c := range d.Components {
			if perComponentEdges[i] != c.EdgeCount {
				return fmt.Errorf("connected component edge count does not match retained graph")
			}
			seen := map[string]bool{c.ID: true}
			queue := []string{c.ID}
			for len(queue) > 0 {
				current := queue[0]
				queue = queue[1:]
				for _, next := range adjacency[i][current] {
					if !seen[next] {
						seen[next] = true
						queue = append(queue, next)
					}
				}
			}
			if len(seen) != len(c.Projects) {
				return fmt.Errorf("fully retained connected component is disconnected")
			}
		}
	}
	var qualifiedTotal int64
	for i, q := range d.QualifiedReferences {
		if q.Ecosystem == "" || q.Kind == "" || q.State == "" || q.Resolution == "" || q.Count <= 0 || i > 0 && qualifiedCountKey(d.QualifiedReferences[i-1].Ecosystem, d.QualifiedReferences[i-1].Kind, d.QualifiedReferences[i-1].State, d.QualifiedReferences[i-1].Resolution) >= qualifiedCountKey(q.Ecosystem, q.Kind, q.State, q.Resolution) {
			return fmt.Errorf("invalid qualified-reference tally")
		}
		var ok bool
		qualifiedTotal, ok = checkedAdd(qualifiedTotal, q.Count)
		if !ok {
			return fmt.Errorf("qualified-reference total overflow")
		}
	}
	qualifiedObserved, ok := checkedAdd(qualifiedTotal, d.OmittedQualifiedReferenceCount)
	if !ok || qualifiedObserved != d.QualifiedReferenceCount || d.OmittedQualified < 0 || d.QualifiedGroupCount < 0 {
		return fmt.Errorf("qualified-reference samples do not reconcile")
	}
	for i, e := range s.EntryPoints {
		if e.EvidencePath == "" || e.Kind == "" || e.Basis == "" || e.State == "" || e.Ecosystem == "" || !projectRoleNames[e.Role] || len(e.EvidencePath) > MaxEvidencePathBytes || !validSelectedPath(e.EvidencePath) || e.ProjectID != "" && (!validSelectedPath(e.ProjectID) || len(e.ProjectID) > MaxEvidencePathBytes) || i > 0 && entryPointKey(s.EntryPoints[i-1]) >= entryPointKey(e) {
			return fmt.Errorf("invalid or unsorted entry point")
		}
		switch e.State {
		case "declared", "associated":
			if e.Reason != "" {
				return fmt.Errorf("entry point state %q must not carry a reason", e.State)
			}
		case "qualified", "unassociated":
			if e.Reason == "" {
				return fmt.Errorf("entry point state %q requires a non-empty reason", e.State)
			}
		default:
			return fmt.Errorf("entry point state %q is not in the allowed vocabulary", e.State)
		}
	}
	entryPointSamples, entryPointSamplesOK := checkedAdd(int64(len(s.EntryPoints)), s.OmittedEntryPoints)
	if s.OmittedEntryPoints < 0 || s.OmittedWorkspaceGroups < 0 || s.OmittedSolutionGroups < 0 {
		return fmt.Errorf("negative structural omission count")
	}
	if !entryPointSamplesOK || entryPointSamples != s.EntryPointRowCount {
		return fmt.Errorf("entry-point row samples do not reconcile")
	}
	retainedAssociations := int64(0)
	retainedDeclarations := map[string]struct{}{}
	for _, e := range s.EntryPoints {
		if e.ProjectID != "" {
			retainedAssociations++
		}
		retainedDeclarations[entryPointDeclarationKey(e)] = struct{}{}
	}
	maxAssociations, associationsOK := checkedAdd(retainedAssociations, s.OmittedEntryPoints)
	maxDeclarations, declarationsOK := checkedAdd(int64(len(retainedDeclarations)), s.OmittedEntryPoints)
	if s.EntryPointCount < 0 || s.EntryPointAssociationCount < 0 || s.EntryPointRowCount < 0 || s.EntryPointCount > s.EntryPointRowCount || s.EntryPointAssociationCount > s.EntryPointRowCount || retainedAssociations > s.EntryPointAssociationCount || !associationsOK || s.EntryPointAssociationCount > maxAssociations || s.EntryPointCount < int64(len(retainedDeclarations)) || !declarationsOK || s.EntryPointCount > maxDeclarations || s.OmittedEntryPoints == 0 && (retainedAssociations != s.EntryPointAssociationCount || int64(len(retainedDeclarations)) != s.EntryPointCount) {
		return fmt.Errorf("entry-point totals do not reconcile")
	}
	if len(s.ExcludedNonEntryKinds) > 64 {
		return fmt.Errorf("too many excluded non-entry kinds: %d (max 64)", len(s.ExcludedNonEntryKinds))
	}
	for k, v := range s.ExcludedNonEntryKinds {
		if !validExcludedKindKey(k) || v < 1 {
			return fmt.Errorf("invalid excluded non-entry kind %q (count %d)", k, v)
		}
	}
	lastCoverage := ""
	entryPointCoverageFound := false
	for _, c := range s.Coverage {
		key := c.Scope + "\x00" + c.Ecosystem
		if c.Scope == "" || c.Ecosystem == "" || c.Status != "complete" && c.Status != "partial" || lastCoverage != "" && lastCoverage >= key {
			return fmt.Errorf("invalid or unsorted structural coverage row %q/%q (%q after %q)", c.Scope, c.Ecosystem, key, lastCoverage)
		}
		lastCoverage = key
		if c.Scope == "entry_points" && c.Ecosystem == "all" {
			entryPointCoverageFound = true
			if c.Status != "partial" || !hasEntryPointScopeQualification(c.Reasons) {
				return fmt.Errorf("entry-point coverage must disclose uninspected source entry declarations")
			}
		}
		for i, reason := range c.Reasons {
			if reason == "" || i > 0 && c.Reasons[i-1] >= reason {
				return fmt.Errorf("invalid structural coverage reasons")
			}
		}
		if c.Status == "complete" && len(c.Reasons) > 0 || c.Status == "partial" && len(c.Reasons) == 0 {
			return fmt.Errorf("structural coverage status disagrees with reasons")
		}
	}
	if !entryPointCoverageFound {
		return fmt.Errorf("entry-point source-inspection coverage is required")
	}
	// Groups and coverage rows use the manifest-table ecosystem vocabulary,
	// so they join with the project populations by name.
	for _, groups := range [][]StructureGroup{s.WorkspaceGroups, s.SolutionGroups} {
		for _, g := range groups {
			if !structureEcosystemNames[g.Ecosystem] {
				return fmt.Errorf("group %q uses ecosystem %q outside the manifest vocabulary", g.ID, g.Ecosystem)
			}
		}
	}
	for _, c := range s.Coverage {
		if c.Ecosystem != "all" && !structureEcosystemNames[c.Ecosystem] {
			return fmt.Errorf("coverage row %q uses ecosystem %q outside the manifest vocabulary", c.Scope, c.Ecosystem)
		}
	}
	return nil
}

func hasEntryPointScopeQualification(reasons []string) bool {
	for _, reason := range reasons {
		switch reason {
		case "entry_point_observer_not_run", "entry_point_scope_not_established", "source_entry_points_not_inspected", "entry_point_association_unavailable":
			return true
		}
	}
	return false
}

func validateStructureGroups(groups []StructureGroup, kind string, total, omitted int64) error {
	groupSamples, ok := checkedAdd(int64(len(groups)), omitted)
	if omitted < 0 || total < 0 || !ok || groupSamples != total {
		return fmt.Errorf("negative omitted group count")
	}
	last := ""
	for _, g := range groups {
		memberSamples, memberSamplesOK := checkedAdd(int64(len(g.Members)), g.OmittedMembers)
		unresolvedSamples, unresolvedSamplesOK := checkedAdd(int64(len(g.UnresolvedMembers)), g.OmittedUnresolvedMembers)
		if !validSelectedPath(g.ID) || len(g.ID) > MaxEvidencePathBytes || g.Ecosystem == "" || !projectRoleNames[g.Role] || g.Kind != kind || g.MemberCount < 0 || g.UnresolvedMemberCount < 0 || g.OmittedMembers < 0 || g.OmittedUnresolvedMembers < 0 || !memberSamplesOK || memberSamples != g.MemberCount || !unresolvedSamplesOK || unresolvedSamples != g.UnresolvedMemberCount || len(g.Members) > StructureMemberLimit || len(g.UnresolvedMembers) > StructureMemberLimit {
			return fmt.Errorf("invalid structure group")
		}
		coverage := g.MembershipCoverage
		if coverage.Ecosystem != g.Ecosystem || coverage.Scope != "workspace_membership" && coverage.Scope != "solution_membership" || coverage.Status != "complete" && coverage.Status != "partial" || coverage.Status == "complete" && len(coverage.Reasons) != 0 || coverage.Status == "partial" && len(coverage.Reasons) == 0 {
			return fmt.Errorf("invalid group membership coverage")
		}
		key := groupKey(g)
		if last != "" && last >= key {
			return fmt.Errorf("structure groups are not sorted and unique")
		}
		last = key
		for i, m := range g.Members {
			if !validSelectedPath(m) || len(m) > MaxEvidencePathBytes || i > 0 && g.Members[i-1] >= m {
				return fmt.Errorf("invalid group member")
			}
		}
		for i, m := range g.UnresolvedMembers {
			if !safeStructureText(m.State, MaxEvidencePathBytes) || !safeStructureText(m.Resolution, MaxEvidencePathBytes) || !safeStructureText(m.Value, MaxEvidencePathBytes) || !safeStructureText(m.Reason, MaxEvidencePathBytes) || m.Target != "" && (!(validSelectedPath(m.Target) || validRoot(m.Target)) || len(m.Target) > MaxEvidencePathBytes) || m.Evidence != "" && (!validSelectedPath(m.Evidence) || len(m.Evidence) > MaxEvidencePathBytes) {
				return fmt.Errorf("invalid unresolved group member")
			}
			if i > 0 && unresolvedMemberKey(g.UnresolvedMembers[i-1]) >= unresolvedMemberKey(m) {
				return fmt.Errorf("unresolved group members are not sorted and unique")
			}
		}
		coverageLast := ""
		for _, reason := range coverage.Reasons {
			if reason == "" || coverageLast != "" && coverageLast >= reason {
				return fmt.Errorf("group membership coverage reasons are invalid")
			}
			coverageLast = reason
		}
	}
	return nil
}

func boundStructureEvidence(s *StructureReport, remaining *int) {
	if s == nil {
		return
	}
	consume := func(value any) bool {
		n := encodedSize(value)
		if n > *remaining {
			return false
		}
		*remaining -= n
		return true
	}
	boundGroups := func(groups []StructureGroup, omitted *int64) []StructureGroup {
		out := groups[:0]
		for _, g := range groups {
			metadata := g
			metadata.Members = nil
			metadata.UnresolvedMembers = nil
			if len(g.ID) > MaxEvidencePathBytes || !validSelectedPath(g.ID) || !consume(metadata) {
				*omitted++
				continue
			}
			members := g.Members[:0]
			for _, m := range g.Members {
				if len(m) > MaxEvidencePathBytes || !validSelectedPath(m) || !consume(m) {
					g.OmittedMembers++
					continue
				}
				members = append(members, m)
			}
			g.Members = members
			unresolved := g.UnresolvedMembers[:0]
			for _, m := range g.UnresolvedMembers {
				if len(m.Target) > MaxEvidencePathBytes || m.Target != "" && !(validSelectedPath(m.Target) || validRoot(m.Target)) || !safeStructureText(m.State, MaxEvidencePathBytes) || !safeStructureText(m.Resolution, MaxEvidencePathBytes) || !safeStructureText(m.Value, MaxEvidencePathBytes) || !safeStructureText(m.Reason, MaxEvidencePathBytes) || len(m.Evidence) > MaxEvidencePathBytes || m.Evidence != "" && !validSelectedPath(m.Evidence) || !consume(m) {
					g.OmittedUnresolvedMembers++
					continue
				}
				unresolved = append(unresolved, m)
			}
			g.UnresolvedMembers = unresolved
			out = append(out, g)
		}
		return out
	}
	s.WorkspaceGroups = boundGroups(s.WorkspaceGroups, &s.OmittedWorkspaceGroups)
	s.SolutionGroups = boundGroups(s.SolutionGroups, &s.OmittedSolutionGroups)
	edges := s.Dependencies.Edges[:0]
	for _, e := range s.Dependencies.Edges {
		if len(e.From) > MaxEvidencePathBytes || !validSelectedPath(e.From) || len(e.To) > MaxEvidencePathBytes || !validSelectedPath(e.To) || len(e.Evidence) > MaxEvidencePathBytes || e.Evidence != "" && !validSelectedPath(e.Evidence) || !consume(e) {
			s.Dependencies.OmittedEdges++
			continue
		}
		edges = append(edges, e)
	}
	s.Dependencies.Edges = edges
	components := s.Dependencies.Components[:0]
	for _, c := range s.Dependencies.Components {
		metadata := c
		metadata.Projects = nil
		if len(c.ID) > MaxEvidencePathBytes || !validSelectedPath(c.ID) || !consume(metadata) {
			s.Dependencies.OmittedComponents++
			continue
		}
		members := c.Projects[:0]
		for _, p := range c.Projects {
			if len(p) > MaxEvidencePathBytes || !validSelectedPath(p) || !consume(p) {
				c.OmittedProjects++
				continue
			}
			members = append(members, p)
		}
		c.Projects = members
		components = append(components, c)
	}
	s.Dependencies.Components = components
	entries := s.EntryPoints[:0]
	for _, e := range s.EntryPoints {
		if len(e.ProjectID) > MaxEvidencePathBytes || e.ProjectID != "" && !validSelectedPath(e.ProjectID) || len(e.EvidencePath) > MaxEvidencePathBytes || !validSelectedPath(e.EvidencePath) || len(e.Target) > MaxEvidencePathBytes || !consume(e) {
			s.OmittedEntryPoints++
			continue
		}
		entries = append(entries, e)
	}
	s.EntryPoints = entries
}

func uniqueSorted(in []string) []string {
	if in == nil {
		return []string{}
	}
	slices.Sort(in)
	return slices.Compact(in)
}
func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}
func sortedBoolKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}

func takeStrings(in []string, n int) []string {
	if len(in) > n {
		in = in[:n]
	}
	return slices.Clone(in)
}
func takeEdges(in []StructureEdge, n int) []StructureEdge {
	if len(in) > n {
		in = in[:n]
	}
	return slices.Clone(in)
}
func takeUnresolved(in []StructureUnresolvedMember, n int) []StructureUnresolvedMember {
	if len(in) > n {
		in = in[:n]
	}
	return append([]StructureUnresolvedMember{}, in...)
}
func groupKey(g StructureGroup) string { return g.Kind + "\x00" + g.ID }
func unresolvedMemberKey(m StructureUnresolvedMember) string {
	return m.Target + "\x00" + m.Value + "\x00" + m.State + "\x00" + m.Resolution + "\x00" + m.Reason + "\x00" + m.Evidence
}

func safeStructureText(value string, limit int) bool {
	if len(value) > limit || !utf8.ValidString(value) {
		return false
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}
func structureEdgeKey(e StructureEdge) string {
	return e.From + "\x00" + e.To
}
func qualifiedCountKey(ecosystem, kind, state, resolution string) string {
	return ecosystem + "\x00" + kind + "\x00" + state + "\x00" + resolution
}
func entryPointKey(e StructureEntryPoint) string {
	return e.ProjectID + "\x00" + e.EvidencePath + "\x00" + e.Ecosystem + "\x00" + e.Role + "\x00" + e.Kind + "\x00" + e.Name + "\x00" + e.Target + "\x00" + e.Basis + "\x00" + e.Reason + "\x00" + e.State
}

// entryPointDeclarationKey identifies the physical deployable declaration,
// stripping the edge-type prefix ("builds:", "runs:") from Kind so that one
// Dockerfile associated with multiple projects still counts as one declaration.
func entryPointDeclarationKey(e StructureEntryPoint) string {
	kind := e.Kind
	for _, prefix := range []string{"builds:", "runs:"} {
		if strings.HasPrefix(kind, prefix) {
			kind = kind[len(prefix):]
			break
		}
	}
	return e.EvidencePath + "\x00" + kind + "\x00" + e.Name
}
func qualifiedResolution(reason, status string) string {
	if strings.HasPrefix(reason, "parent_") {
		// Maven parent coordinate checks keep their specific reason.
		return reason
	}
	if status == "external" || strings.Contains(reason, "external") {
		return "external"
	}
	if status == "missing" || strings.Contains(reason, "missing") {
		return "missing"
	}
	if strings.Contains(reason, "ambiguous") {
		return "ambiguous"
	}
	if status == "present" {
		return "present_qualified"
	}
	return "unresolved"
}
func defaultString(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

// validExcludedKindKey returns true when key matches [a-z0-9-]+:[a-z0-9_-]+.
// The format is "provider:kind" where provider uses hyphens and kind uses
// underscores or hyphens (consistent with pkg/deployables naming conventions).
func validExcludedKindKey(key string) bool {
	colon := strings.IndexByte(key, ':')
	if colon <= 0 || colon == len(key)-1 {
		return false
	}
	provider := key[:colon]
	kind := key[colon+1:]
	for _, c := range provider {
		if !((c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-') {
			return false
		}
	}
	for _, c := range kind {
		if !((c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '_' || c == '-') {
			return false
		}
	}
	return true
}
