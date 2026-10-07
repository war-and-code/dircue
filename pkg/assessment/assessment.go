// Package assessment combines bounded inventory, declaration, and lockfile
// evidence without interpreting build behavior or dependency resolution.
package assessment

import (
	"container/heap"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"math"
	"path"
	"reflect"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/war-and-code/dircue/pkg/declarations"
	"github.com/war-and-code/dircue/pkg/discovery"
	"github.com/war-and-code/dircue/pkg/lockfiles"
	"github.com/war-and-code/dircue/pkg/pathrole"
	"github.com/war-and-code/dircue/pkg/projects"
)

const (
	Version                   = "1.1.0"
	LegacyVersion             = "1.0.0"
	EvidenceLimitPerKind      = 256
	ProjectRootEvidenceLimit  = 256
	RelationshipEvidenceLimit = 64
	MaxEvidencePathBytes      = 3072
	MaxEvidenceJSONBytes      = 7 << 20
	MaxAssessmentJSONBytes    = 8 << 20
)

// Metric reports a value over an explicit population. Lower-bound metrics
// count observed evidence and carry reasons for evidence known to be omitted.
type Metric struct {
	Count        int64    `json:"count"`
	Scope        string   `json:"scope"`
	Completeness string   `json:"completeness"` // complete, lower_bound, upper_bound, or observed_only
	Reasons      []string `json:"reasons"`
}

// CandidateCount groups selected files with one recognized manifest filename.
// Kind is the file's role in its ecosystem: manifest, workspace, solution,
// configuration, or toolchain.
type CandidateCount struct {
	Filename  string `json:"filename"`
	Kind      string `json:"kind"`
	Ecosystem string `json:"ecosystem"`
	Files     int64  `json:"files"`
	Bytes     int64  `json:"bytes"`
}

type CandidateKindCount struct {
	Kind  string `json:"kind"`
	Files int64  `json:"files"`
	Bytes int64  `json:"bytes"`
}

// InventoryMetrics counts selected regular files. The vendored subset uses
// Linguist's vendor path rules and linguist-vendored attributes, the rule
// that excludes files from language statistics.
type InventoryMetrics struct {
	Files         Metric `json:"files"`
	Bytes         Metric `json:"bytes"`
	VendoredFiles Metric `json:"vendored_files"`
	VendoredBytes Metric `json:"vendored_bytes"`
}

// RoleCount partitions a count by conventional path role (see pathrole).
type RoleCount struct {
	Role  string `json:"role"`
	Count int64  `json:"count"`
}

type RelationshipCount struct {
	Kind  string `json:"kind"`
	Count int64  `json:"count"`
}

// RelationshipEvidence contains only selected project roots. Source and
// target roots are omitted if a reference cannot be tied to parsed projects.
type RelationshipEvidence struct {
	Kind          string `json:"kind"`
	SourceProject string `json:"source_project,omitempty"`
	SourceRoot    string `json:"source_root,omitempty"`
	TargetPath    string `json:"target_path,omitempty"`
	TargetRoot    string `json:"target_root,omitempty"`
	State         string `json:"state"`
}

// LockfileRole partitions one lockfile row by project role. Its counts share
// the completeness of the row's metrics.
type LockfileRole struct {
	Role          string `json:"role"`
	Projects      int64  `json:"projects"`
	Eligible      int64  `json:"eligible"`
	Covered       int64  `json:"covered"`
	Missing       int64  `json:"missing"`
	NotApplicable int64  `json:"not_applicable"`
	Unsupported   int64  `json:"unsupported"`
	Unknown       int64  `json:"unknown"`
}

// OutcomeReason counts the projects in one non-covered state by the reason
// that explains it, usually a lockfile boundary reason.
type OutcomeReason struct {
	State  string `json:"state"`
	Reason string `json:"reason"`
	Count  int64  `json:"count"`
}

// LockfileChecks partitions covered projects by the status of their named
// lockfile check. IndeterminateReasons counts each reason once per project,
// so a project can appear under several reasons.
type LockfileChecks struct {
	Match                int64         `json:"match"`
	Different            int64         `json:"different"`
	Indeterminate        int64         `json:"indeterminate"`
	NotApplicable        int64         `json:"not_applicable"`
	IndeterminateReasons []ReasonCount `json:"indeterminate_reasons"`
}

// LockfilePresence partitions NuGet projects by whether a lockfile exists at
// a path they can use, independently of ownership. UnknownReasons counts each
// reason once per project.
type LockfilePresence struct {
	Observed       int64         `json:"observed"`
	NotObserved    int64         `json:"not_observed"`
	Unknown        int64         `json:"unknown"`
	UnknownReasons []ReasonCount `json:"unknown_reasons"`
}

// ReasonCount counts projects that carry one reason.
type ReasonCount struct {
	Reason string `json:"reason"`
	Count  int64  `json:"count"`
}

// LockfileCause counts the projects whose uncertain outcome or check names
// one selected file as its cause.
type LockfileCause struct {
	Reason string `json:"reason"`
	Path   string `json:"path"`
	Count  int64  `json:"count"`
}

// LockfileCauseLimit bounds the causes listed in one lockfile row.
const LockfileCauseLimit = 32

type LockfileEcosystem struct {
	Ecosystem      string          `json:"ecosystem"`
	Projects       Metric          `json:"projects"`
	Eligible       Metric          `json:"eligible"`
	Covered        Metric          `json:"covered"`
	Missing        Metric          `json:"missing"`
	NotApplicable  Metric          `json:"not_applicable"`
	Unsupported    Metric          `json:"unsupported"`
	Unknown        Metric          `json:"unknown"`
	ByRole         []LockfileRole  `json:"by_role"`
	OutcomeReasons []OutcomeReason `json:"outcome_reasons"`
	// Checks, NuGetPresence, and Causes are absent from assessment 1.0.0.
	// NuGetPresence is set on the nuget and all rows when NuGet projects
	// exist; Causes is omitted when no file is named.
	Checks        *LockfileChecks   `json:"checks,omitempty"`
	NuGetPresence *LockfilePresence `json:"nuget_presence,omitempty"`
	Causes        []LockfileCause   `json:"causes,omitempty"`
	OmittedCauses int64             `json:"omitted_causes,omitempty"`
}

type Definition struct {
	ID          string `json:"id"`
	Description string `json:"description"`
}

type Report struct {
	Version                      string                 `json:"version"`
	Source                       discovery.Source       `json:"source"`
	Inventory                    InventoryMetrics       `json:"inventory"`
	ManifestCandidates           []CandidateCount       `json:"manifest_candidates"`
	ManifestCandidatePopulation  Metric                 `json:"manifest_candidate_population"`
	UnparsedManifestCandidates   Metric                 `json:"unparsed_manifest_candidates"`
	FilenameCandidates           []CandidateKindCount   `json:"filename_candidates"`
	FilenameCandidatePopulation  Metric                 `json:"filename_candidate_population"`
	CandidateEvidence            []discovery.Candidate  `json:"candidate_evidence"`
	OmittedCandidateEvidence     map[string]int64       `json:"omitted_candidate_evidence"`
	ProjectRoots                 Metric                 `json:"project_roots"`
	ProjectRootsByRole           []RoleCount            `json:"project_roots_by_role"`
	Projects                     Metric                 `json:"projects"`
	ProjectsByRole               []RoleCount            `json:"projects_by_role"`
	ProjectRootEvidence          []string               `json:"project_root_evidence"`
	OmittedProjectRootEvidence   int64                  `json:"omitted_project_root_evidence"`
	WorkspaceMembership          Metric                 `json:"workspace_membership"`
	WorkspaceByKind              []RelationshipCount    `json:"workspace_by_kind"`
	WorkspaceEvidence            []RelationshipEvidence `json:"workspace_evidence"`
	OmittedWorkspaceEvidence     int64                  `json:"omitted_workspace_evidence"`
	LocalDependencies            Metric                 `json:"local_dependencies"`
	LocalDependencyByKind        []RelationshipCount    `json:"local_dependency_by_kind"`
	LocalDependencyEvidence      []RelationshipEvidence `json:"local_dependency_evidence"`
	OmittedLocalEvidence         int64                  `json:"omitted_local_dependency_evidence"`
	Lockfiles                    []LockfileEcosystem    `json:"lockfiles"`
	LockfilesOverall             LockfileEcosystem      `json:"lockfiles_overall"`
	UnsupportedEcosystemProjects Metric                 `json:"unsupported_ecosystem_projects"`
	Definitions                  []Definition           `json:"definitions"`
	Structure                    *StructureReport       `json:"structure,omitempty"`
}

// NamedMetric is one aggregate metric with a stable name. Lockfile row
// metrics also name their ecosystem row ("all" for the overall row).
type NamedMetric struct {
	Name      string
	Ecosystem string
	Metric    Metric
}

// Metrics lists every aggregate metric in a stable order, so validation,
// comparison, and planning cannot drift from the report's fields.
func (r *Report) Metrics() []NamedMetric {
	out := []NamedMetric{
		{Name: "inventory:files", Metric: r.Inventory.Files},
		{Name: "inventory:bytes", Metric: r.Inventory.Bytes},
		{Name: "inventory:vendored_files", Metric: r.Inventory.VendoredFiles},
		{Name: "inventory:vendored_bytes", Metric: r.Inventory.VendoredBytes},
		{Name: "manifest_candidate_population", Metric: r.ManifestCandidatePopulation},
		{Name: "unparsed_manifest_candidates", Metric: r.UnparsedManifestCandidates},
		{Name: "filename_candidate_population", Metric: r.FilenameCandidatePopulation},
		{Name: "projects", Metric: r.Projects},
		{Name: "project_roots", Metric: r.ProjectRoots},
		{Name: "workspace_membership", Metric: r.WorkspaceMembership},
		{Name: "local_dependencies", Metric: r.LocalDependencies},
		{Name: "unsupported_ecosystem_projects", Metric: r.UnsupportedEcosystemProjects},
	}
	lockfileRows := make([]LockfileEcosystem, 0, len(r.Lockfiles)+1)
	lockfileRows = append(lockfileRows, r.LockfilesOverall)
	lockfileRows = append(lockfileRows, r.Lockfiles...)
	for _, row := range lockfileRows {
		for _, m := range []NamedMetric{{Name: "projects", Metric: row.Projects}, {Name: "eligible", Metric: row.Eligible}, {Name: "covered", Metric: row.Covered}, {Name: "missing", Metric: row.Missing}, {Name: "not_applicable", Metric: row.NotApplicable}, {Name: "unsupported", Metric: row.Unsupported}, {Name: "unknown", Metric: row.Unknown}} {
			m.Ecosystem = row.Ecosystem
			out = append(out, m)
		}
	}
	if r.Structure != nil {
		for _, population := range r.Structure.Populations {
			name := "structure:population:" + population.Population + ":" + population.Ecosystem + ":" + population.Role
			out = append(out, NamedMetric{Name: name, Metric: population.Metric})
		}
		out = append(out, []NamedMetric{
			{Name: "structure:dependency_projects", Metric: r.Structure.Dependencies.Projects},
			{Name: "structure:definite_edges", Metric: r.Structure.Dependencies.DefiniteEdges},
			{Name: "structure:connected_groups", Metric: r.Structure.Dependencies.ConnectedGroups},
			{Name: "structure:connected_groups_with_qualified", Metric: r.Structure.Dependencies.ConnectedGroupsWithQualified},
		}...)
	}
	return out
}

// Evidence is the declaration and lockfile evidence for the collector's
// source snapshot.
type Evidence struct {
	Declarations *declarations.Report
	Lockfiles    *lockfiles.Report
	// Records are the declaration collector's project records; they are
	// required whenever the declaration report contains projects.
	Records []declarations.ProjectRecord
	// InterpretedManifests lists manifests the declaration parser read in full
	// and either parsed or found to hold no declarations. Selected manifests
	// outside this list are counted as unparsed.
	InterpretedManifests []string
	// OmittedDeclarationPaths lists omitted selected manifests and
	// OmittedDeclarationTrees lists selected directories that could not be
	// read. When attributed is false, any omission may hide a manifest;
	// otherwise every other omission is a file that is not a manifest.
	OmittedDeclarationPaths      []string
	OmittedDeclarationTrees      []string
	OmittedDeclarationAttributed bool
}

type Collector struct {
	mode, tree          string
	files               int64
	bytes               int64
	vendoredFiles       int64
	vendoredBytes       int64
	invalid             map[string]int64
	candidateCounts     map[string]*CandidateCount
	candidateRoleCounts map[string]int64
	kindCounts          map[string]*CandidateKindCount
	candidates          map[string]*candidateHeap
	// manifests holds every selected manifest candidate path, so unparsed
	// candidates are identified by path rather than by subtracting counts.
	manifests          map[string]bool
	pythonLockDirs     map[string]bool
	unparsedWorkspaces map[string]bool
	status             string
	omissions          map[string]int64
}

// New creates an assessment collector for one selected source snapshot.
func New(mode, tree string) *Collector {
	return &Collector{mode: mode, tree: tree, status: "complete", invalid: map[string]int64{}, candidateCounts: map[string]*CandidateCount{}, candidateRoleCounts: map[string]int64{}, kindCounts: map[string]*CandidateKindCount{}, candidates: map[string]*candidateHeap{}, manifests: map[string]bool{}, pythonLockDirs: map[string]bool{}, unparsedWorkspaces: map[string]bool{}, omissions: map[string]int64{}}
}

// Add adds metadata for one selected regular file. It does not retain every
// path: exact counts are maintained separately from bounded candidate samples.
func (c *Collector) Add(file discovery.File) {
	c.files++
	sized := false
	if file.Size < 0 {
		c.invalid["invalid_file_size"]++
		c.Partial("invalid_file_size")
	} else if file.Size > math.MaxInt64-c.bytes {
		c.invalid["inventory_bytes_overflow"]++
		c.Partial("inventory_bytes_overflow")
	} else {
		c.bytes += file.Size
		sized = true
	}
	if !utf8.ValidString(file.Path) {
		c.invalid["invalid_utf8_path"]++
		c.Partial("invalid_utf8_path")
		return
	}
	if !validSelectedPath(file.Path) {
		c.invalid["invalid_selected_path"]++
		c.Partial("invalid_selected_path")
		return
	}
	if file.LinguistVendored() {
		c.vendoredFiles++
		if sized {
			c.vendoredBytes += file.Size
		}
	}
	base := path.Base(file.Path)
	if !strings.Contains("/"+file.Path+"/", "/node_modules/") {
		if reason := unparsedWorkspaceReason(base); reason != "" {
			c.unparsedWorkspaces[reason] = true
		}
		if isPythonLock(base) {
			c.pythonLockDirs[path.Dir(file.Path)] = true
		}
	}
	if declarations.IsManifest(file.Path) {
		c.manifests[file.Path] = true
		filename, ecosystem, kind := classifyManifest(file.Path)
		key := kind + "\x00" + ecosystem + "\x00" + filename
		count := c.candidateCounts[key]
		if count == nil {
			count = &CandidateCount{Filename: filename, Kind: kind, Ecosystem: ecosystem}
			c.candidateCounts[key] = count
		}
		count.Files++
		c.candidateRoleCounts[structurePopulationKey("filename_candidates", ecosystem, pathrole.Of(file.Path))]++
		if file.Size >= 0 && file.Size <= math.MaxInt64-count.Bytes {
			count.Bytes += file.Size
		} else if file.Size >= 0 {
			c.Partial("manifest_candidate_bytes_overflow")
		}
	}
	candidate := discovery.ClassifyCandidate(file.Path)
	if candidate == nil {
		return
	}
	if candidate.Kind == "manifest" && (strings.HasSuffix(candidate.Format, "_lock") || strings.HasSuffix(candidate.Format, "_checksums")) {
		// Lockfiles and checksum files record resolutions rather than
		// declarations, so they are not counted with manifests.
		candidate.Kind = "lockfile"
	}
	kindCount := c.kindCounts[candidate.Kind]
	if kindCount == nil {
		kindCount = &CandidateKindCount{Kind: candidate.Kind}
		c.kindCounts[candidate.Kind] = kindCount
	}
	kindCount.Files++
	if file.Size >= 0 && file.Size <= math.MaxInt64-kindCount.Bytes {
		kindCount.Bytes += file.Size
	} else if file.Size >= 0 {
		c.Partial("filename_candidate_bytes_overflow")
	}
	if file.Size >= 0 {
		candidate.Bytes = file.Size
	}
	h := c.candidates[candidate.Kind]
	if h == nil {
		h = &candidateHeap{}
		c.candidates[candidate.Kind] = h
	}
	if len(candidate.Path) > MaxEvidencePathBytes {
		c.omissions["candidate_evidence:"+candidate.Kind]++
		return
	}
	if len(*h) < EvidenceLimitPerKind {
		heap.Push(h, *candidate)
		return
	}
	c.omissions["candidate_evidence:"+candidate.Kind]++
	if candidate.Path < (*h)[0].Path {
		(*h)[0] = *candidate
		heap.Fix(h, 0)
	}
}

// unparsedWorkspaceReason names workspace definition files that dircue
// recognizes but does not parse; their members are not counted.
func unparsedWorkspaceReason(base string) string {
	switch base {
	case "pnpm-workspace.yaml":
		return "pnpm_workspace_unparsed"
	case "lerna.json":
		return "lerna_workspace_unparsed"
	case "rush.json":
		return "rush_workspace_unparsed"
	}
	return ""
}

func isPythonLock(base string) bool {
	switch base {
	case "poetry.lock", "pdm.lock", "uv.lock", "pylock.toml":
		return true
	}
	return strings.HasPrefix(base, "pylock.") && strings.HasSuffix(base, ".toml")
}

// Partial records a real source omission or invalid selected metadata.
func (c *Collector) Partial(reason string) {
	if reason == "" {
		reason = "unspecified_omission"
	}
	if c.status != "skipped" {
		c.status = "partial"
	}
	c.omissions[reason]++
}

// Skip marks the source traversal as skipped. Finish still returns exact
// metadata already supplied to Add, with lower-bound scope.
func (c *Collector) Skip(reason string) {
	if reason == "" {
		reason = "source_skipped"
	}
	c.status = "skipped"
	c.omissions[reason]++
}

const manifestPopulationScope = "selected files with a recognized manifest filename, excluding installed node_modules"

// Finish combines exact scanner metadata with bounded parsed-project and
// lockfile observations. Inputs must describe the same source snapshot.
func (c *Collector) Finish(in Evidence) (*Report, error) {
	decls, locks := in.Declarations, in.Lockfiles
	if decls == nil {
		return nil, errors.New("assessment requires a declaration report")
	}
	if !validSource(c.mode, c.tree) {
		return nil, errors.New("assessment source identity is invalid")
	}
	if decls.Source != c.mode || decls.Tree != c.tree {
		return nil, errors.New("assessment and declaration source identities do not match")
	}
	if locks != nil && (locks.Source != c.mode || locks.Tree != c.tree) {
		return nil, errors.New("assessment and lockfile source identities do not match")
	}
	if err := validateDeclarationPaths(decls.Projects); err != nil {
		return nil, err
	}
	if len(in.Records) == 0 && len(decls.Projects) > 0 {
		return nil, errors.New("assessment requires declaration project records to distinguish parsed projects from configuration and invalid documents")
	}
	recordProjects := make([]declarations.Project, 0, len(in.Records))
	for _, record := range in.Records {
		recordProjects = append(recordProjects, record.Project)
	}
	if err := validateDeclarationPaths(recordProjects); err != nil {
		return nil, err
	}
	sel := selectProjects(in.Records, decls.Diagnostics, c.pythonLockDirs)
	consistency := "live_directory_metadata"
	if c.mode == "git" {
		consistency = "selected_git_tree"
	}
	inventoryLower, inventoryReasons := c.status != "complete", mapKeys(c.omissions)
	out := &Report{Version: Version, Source: discovery.Source{Mode: c.mode, Tree: c.tree, Consistency: consistency},
		Inventory: InventoryMetrics{
			Files:         metric(c.files, "selected regular files", inventoryLower, inventoryReasons),
			Bytes:         metric(c.bytes, "logical bytes in selected regular files", inventoryLower, inventoryReasons),
			VendoredFiles: metric(c.vendoredFiles, "selected regular files that Linguist vendor path rules or a linguist-vendored attribute mark as vendored", inventoryLower, inventoryReasons),
			VendoredBytes: metric(c.vendoredBytes, "logical bytes in vendored selected regular files", inventoryLower, inventoryReasons),
		},
		ManifestCandidates: sortedCandidateCounts(c.candidateCounts), CandidateEvidence: []discovery.Candidate{}, OmittedCandidateEvidence: map[string]int64{},
		FilenameCandidates:  sortedKindCounts(c.kindCounts),
		ProjectRootEvidence: []string{}, WorkspaceByKind: []RelationshipCount{}, WorkspaceEvidence: []RelationshipEvidence{},
		LocalDependencyByKind: []RelationshipCount{}, LocalDependencyEvidence: []RelationshipEvidence{}, Lockfiles: []LockfileEcosystem{}, Definitions: definitions()}
	var manifestCount int64
	var manifestBytes int64
	manifestBytesOverflow := false
	for _, count := range out.ManifestCandidates {
		manifestCount += count.Files
		var ok bool
		manifestBytes, ok = checkedAdd(manifestBytes, count.Bytes)
		manifestBytesOverflow = manifestBytesOverflow || !ok
	}
	if manifestBytesOverflow && !metricHasReason(out.Inventory.Bytes, "inventory_bytes_overflow") {
		return nil, errors.New("manifest candidate bytes overflow without matching inventory overflow")
	}
	if !manifestBytesOverflow && manifestBytes > out.Inventory.Bytes.Count {
		return nil, errors.New("manifest candidate bytes exceed inventory bytes")
	}
	out.ManifestCandidatePopulation = metric(manifestCount, manifestPopulationScope, inventoryLower, inventoryReasons)
	interpreted := make(map[string]bool, len(in.InterpretedManifests))
	for _, name := range in.InterpretedManifests {
		interpreted[name] = true
	}
	var unparsed int64
	for name := range c.manifests {
		if !sel.parsedIDs[name] && !interpreted[name] {
			unparsed++
		}
	}
	out.UnparsedManifestCandidates = metric(unparsed, "manifest candidates the declaration parser did not interpret because they were unreadable, invalid, unsupported, or over a declaration input limit", inventoryLower, inventoryReasons)
	var filenameCandidateCount int64
	var filenameCandidateBytes int64
	filenameCandidateBytesOverflow := false
	for _, item := range out.FilenameCandidates {
		filenameCandidateCount += item.Files
		var ok bool
		filenameCandidateBytes, ok = checkedAdd(filenameCandidateBytes, item.Bytes)
		filenameCandidateBytesOverflow = filenameCandidateBytesOverflow || !ok
	}
	if filenameCandidateBytesOverflow && !metricHasReason(out.Inventory.Bytes, "inventory_bytes_overflow") {
		return nil, errors.New("filename candidate bytes overflow without matching inventory overflow")
	}
	if !filenameCandidateBytesOverflow && filenameCandidateBytes > out.Inventory.Bytes.Count {
		return nil, errors.New("filename candidate bytes exceed inventory bytes")
	}
	out.FilenameCandidatePopulation = metric(filenameCandidateCount, "selected files with discovery filename classification evidence", inventoryLower, inventoryReasons)
	for kind, h := range c.candidates {
		out.CandidateEvidence = append(out.CandidateEvidence, (*h)...)
		if omitted := c.omissions["candidate_evidence:"+kind]; omitted > 0 {
			out.OmittedCandidateEvidence[kind] = omitted
		}
	}
	slices.SortFunc(out.CandidateEvidence, func(a, b discovery.Candidate) int {
		if n := strings.Compare(a.Path, b.Path); n != 0 {
			return n
		}
		return strings.Compare(a.Kind, b.Kind)
	})
	projectsComplete, projectReasons := projectCoverage(decls, sel, unparsed > 0, manifestOmitted(decls, in))
	rootSet, rootRoles := projectRoots(sel.projects)
	out.ProjectRoots = metric(int64(len(rootSet)), "distinct roots of counted projects", !projectsComplete, projectReasons)
	out.ProjectRootsByRole = rootRoles
	out.Projects = metric(int64(len(sel.projects)), "parsed package and project records; virtual workspace roots and configuration, solution, annotation, and tool-settings records are excluded", !projectsComplete, projectReasons)
	out.ProjectsByRole = projectRoles(sel.projects)
	unparsedEcosystems := map[string]bool{}
	for name := range c.manifests {
		if sel.parsedIDs[name] || interpreted[name] {
			continue
		}
		_, ecosystem, _ := classifyManifest(name)
		unparsedEcosystems[ecosystem] = true
	}
	structureGlobalReasons := []string{}
	if decls.Status == "skipped" {
		structureGlobalReasons = append(structureGlobalReasons, "declarations_skipped")
	}
	if decls.Coverage.OmittedDiagnostics > 0 {
		structureGlobalReasons = append(structureGlobalReasons, "declaration_diagnostics_omitted")
	}
	if manifestOmitted(decls, in) {
		structureGlobalReasons = append(structureGlobalReasons, "declaration_files_omitted")
	}
	if sel.unclassified {
		structureGlobalReasons = append(structureGlobalReasons, "unclassified_declaration_kind")
	}
	out.Structure = buildStructure(in.Records, sel.projects, in.Declarations, c.candidateRoleCounts, inventoryReasons, unparsedEcosystems, c.unparsedWorkspaces, structureGlobalReasons)
	projectByID := map[string]string{}
	for _, p := range sel.graph {
		if p.ID != "" && p.Root != "" {
			projectByID[p.ID] = p.Root
		}
	}
	allRoots := mapKeysBool(rootSet)
	rootEvidence := make([]string, 0, min(len(allRoots), ProjectRootEvidenceLimit))
	for _, root := range allRoots {
		if len(root) > MaxEvidencePathBytes || len(rootEvidence) >= ProjectRootEvidenceLimit {
			continue
		}
		rootEvidence = append(rootEvidence, root)
	}
	out.ProjectRootEvidence = rootEvidence
	out.OmittedProjectRootEvidence = int64(len(allRoots) - len(rootEvidence))
	relationComplete, relationReasons := relationshipCoverage(decls, projectsComplete, projectReasons, c.unparsedWorkspaces)
	out.WorkspaceMembership, out.WorkspaceByKind, out.WorkspaceEvidence, out.OmittedWorkspaceEvidence = relationshipMetric(sel.graph, projectByID, "workspace", relationComplete, relationReasons)
	out.LocalDependencies, out.LocalDependencyByKind, out.LocalDependencyEvidence, out.OmittedLocalEvidence = relationshipMetric(sel.graph, projectByID, "local", relationComplete, relationReasons)
	out.Lockfiles, out.LockfilesOverall = lockfileMetrics(sel.projects, locks, projectsComplete, projectReasons)
	var unsupportedProjects int64
	for _, row := range out.Lockfiles {
		if !associationScope(row.Ecosystem) {
			unsupportedProjects += row.Projects.Count
		}
	}
	out.UnsupportedEcosystemProjects = metric(unsupportedProjects, "parsed projects outside npm/NuGet static lockfile association", !projectsComplete, projectReasons)
	boundEvidence(out)
	if err := ValidateReport(out); err != nil {
		return nil, err
	}
	return out, nil
}

// boundEvidence limits the serialized size of retained sample evidence. Exact
// aggregate metrics are computed before this pass and are never reduced.
func boundEvidence(r *Report) {
	remaining := MaxEvidenceJSONBytes
	keepCandidates := r.CandidateEvidence[:0]
	for _, candidate := range r.CandidateEvidence {
		n := encodedSize(candidate)
		if n <= remaining {
			keepCandidates = append(keepCandidates, candidate)
			remaining -= n
		} else {
			r.OmittedCandidateEvidence[candidate.Kind]++
		}
	}
	r.CandidateEvidence = keepCandidates
	keepRoots := r.ProjectRootEvidence[:0]
	for _, root := range r.ProjectRootEvidence {
		n := encodedSize(root)
		if n <= remaining {
			keepRoots = append(keepRoots, root)
			remaining -= n
		} else {
			r.OmittedProjectRootEvidence++
		}
	}
	r.ProjectRootEvidence = keepRoots
	keepWorkspace := r.WorkspaceEvidence[:0]
	for _, evidence := range r.WorkspaceEvidence {
		n := encodedSize(evidence)
		if n <= remaining {
			keepWorkspace = append(keepWorkspace, evidence)
			remaining -= n
		} else {
			r.OmittedWorkspaceEvidence++
		}
	}
	r.WorkspaceEvidence = keepWorkspace
	keepLocal := r.LocalDependencyEvidence[:0]
	for _, evidence := range r.LocalDependencyEvidence {
		n := encodedSize(evidence)
		if n <= remaining {
			keepLocal = append(keepLocal, evidence)
			remaining -= n
		} else {
			r.OmittedLocalEvidence++
		}
	}
	r.LocalDependencyEvidence = keepLocal
	boundStructureEvidence(r.Structure, &remaining)
}

func encodedSize(v any) int {
	b, _ := json.Marshal(v)
	return len(b)
}

func validSource(mode, tree string) bool {
	if mode == "directory" {
		return tree == ""
	}
	return mode == "git" && validHexHash(tree)
}

func validHexHash(hash string) bool {
	if len(hash) != 40 {
		return false
	}
	for _, r := range hash {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f') {
			return false
		}
	}
	return true
}

func validSelectedPath(p string) bool {
	// Scanner paths use POSIX separators. A literal backslash is legal in a
	// POSIX filename, so only slash-delimited traversal and absolute paths are
	// rejected here.
	return p != "" && utf8.ValidString(p) && !strings.ContainsRune(p, '\x00') && !path.IsAbs(p) && path.Clean(p) == p && p != "." && p != ".." && !strings.HasPrefix(p, "../")
}

func validateDeclarationPaths(projects []declarations.Project) error {
	for _, p := range projects {
		if !validSelectedPath(p.ID) || len(p.ID) > declarations.MaxStringBytes || !validRoot(p.Root) || len(p.Root) > declarations.MaxStringBytes || path.Dir(p.ID) != p.Root {
			return errors.New("declaration report contains a project path outside the selected root")
		}
		for _, ref := range p.References {
			if ref.Target != "" && (!validSelectedPath(ref.Target) || len(ref.Target) > declarations.MaxStringBytes) {
				return errors.New("declaration report contains a reference target outside the selected root")
			}
		}
	}
	return nil
}

func metric(count int64, scope string, lower bool, reasons []string) Metric {
	completeness := "complete"
	if lower {
		completeness = "lower_bound"
	}
	return Metric{Count: count, Scope: scope, Completeness: completeness, Reasons: slices.Clone(reasons)}
}

func projectCoverage(r *declarations.Report, sel selection, unparsed, omitted bool) (bool, []string) {
	reasons := []string{}
	if r.Status == "skipped" {
		reasons = append(reasons, "declarations_skipped")
	}
	if omitted {
		reasons = append(reasons, "declaration_files_omitted")
	}
	if r.Coverage.OmittedDiagnostics > 0 {
		reasons = append(reasons, "declaration_diagnostics_omitted")
	}
	if unparsed {
		reasons = append(reasons, "manifest_candidates_unparsed")
	}
	if hasDotnetSelectedPathIdentityIssue(r) {
		reasons = append(reasons, "dotnet_selected_path_identity_ambiguous")
	}
	if sel.incomplete {
		reasons = append(reasons, "parsed_project_observations_incomplete")
	}
	if sel.unclassified {
		reasons = append(reasons, "unclassified_declaration_kind")
	}
	slices.Sort(reasons)
	return len(reasons) == 0, reasons
}

// manifestOmitted reports whether a declaration omission may hide a project.
// An omitted path that is not a manifest, such as a symbolic link to a
// directory, cannot. Unattributed omissions are assumed to hide one.
func manifestOmitted(r *declarations.Report, in Evidence) bool {
	if r.Coverage.OmittedFiles == 0 {
		return false
	}
	if !in.OmittedDeclarationAttributed || int64(len(in.OmittedDeclarationPaths)+len(in.OmittedDeclarationTrees)) > r.Coverage.OmittedFiles {
		return true
	}
	return len(in.OmittedDeclarationTrees) > 0 || slices.ContainsFunc(in.OmittedDeclarationPaths, declarations.IsManifest)
}

func hasDotnetSelectedPathIdentityIssue(r *declarations.Report) bool {
	if r == nil {
		return false
	}
	for _, p := range r.Projects {
		if strings.ContainsRune(p.ID, '\\') && projects.IsDotnet(p.ID) {
			// The .NET adapter normalizes backslashes before parsing. On a POSIX
			// source, however, a backslash is part of the selected filename. The
			// adapter may then fail to retain the selected identity or its
			// declarations while still producing a configuration-shaped record.
			return true
		}
	}
	return false
}

func relationshipCoverage(r *declarations.Report, complete bool, reasons []string, unparsedWorkspaces map[string]bool) (bool, []string) {
	result := slices.Clone(reasons)
	if len(r.Diagnostics) > 0 {
		result = append(result, "declaration_diagnostics_present")
	}
	if r.Coverage.RetainedObservations >= r.Limits.TotalObservations && r.Limits.TotalObservations > 0 {
		result = append(result, "declaration_observation_limit_reached")
	}
	for reason := range unparsedWorkspaces {
		result = append(result, reason)
	}
	slices.Sort(result)
	result = slices.Compact(result)
	return complete && len(result) == 0, result
}

// projectKinds classifies every declaration record kind. True kinds are
// packages and projects. False kinds are virtual workspace roots (go.work, a
// Cargo manifest with only [workspace], a uv workspace without [project]),
// configuration, solutions, and records that only annotate another manifest,
// such as a Rails config/application.rb name hint. Workspace roots still
// contribute membership edges. An unlisted kind is never counted silently:
// it makes the project metrics lower bounds.
var projectKinds = map[string]bool{
	"autoconf": true, "bazel-module": true, "bazel-workspace": true, "cargo": true,
	"clojure-deps": true, "clojure-leiningen": true, "cmake": true, "dart-pub": true, "deno": true,
	"dotnet": true, "elixir-mix": true, "erlang-rebar": true, "go": true, "gradle": true,
	"haskell-cabal": true, "haskell-stack": true, "julia-project": true, "kbuild-kconfig": true, "maven": true,
	"meson": true, "npm": true, "perl-cpanfile": true, "perl-extutils": true, "php-composer": true,
	"python": true, "python-uv": true, "r-package": true, "ruby-bundler": true,
	"ruby-gem": true, "scala-sbt": true, "swift-package": true, "zig-build": true,
	"cargo-workspace": false, "go-workspace": false, "python-workspace": false,
	"configuration": false, "dotnet-configuration": false, "jvm-configuration": false,
	"kbuild-kconfig-marker": false, "ruby-rails-app": false, "solution": false,
}

type selection struct {
	graph        []declarations.Project // every parsed record, for relationship edges
	projects     []declarations.Project // counted projects
	parsedIDs    map[string]bool
	incomplete   bool
	unclassified bool
}

func selectProjects(records []declarations.ProjectRecord, diagnostics []declarations.Diagnostic, pythonLockDirs map[string]bool) selection {
	sel := selection{graph: []declarations.Project{}, projects: []declarations.Project{}, parsedIDs: map[string]bool{}}
	cargoMissing := map[string]bool{}
	for _, d := range diagnostics {
		if d.Code == "cargo-missing-project" {
			cargoMissing[d.Path] = true
		}
	}
	for _, record := range records {
		if !record.Parsed {
			continue
		}
		p := record.Project
		if sel.parsedIDs[p.ID] {
			sel.incomplete = true
			continue
		}
		sel.parsedIDs[p.ID] = true
		sel.graph = append(sel.graph, p)
		if !record.Complete {
			sel.incomplete = true
		}
		counted, known := projectKinds[p.Kind]
		if !known {
			sel.unclassified = true
			continue
		}
		// Cargo rejects a manifest with neither a package nor a workspace table.
		if !counted || p.Kind == "cargo" && cargoMissing[p.ID] || toolSettingsPyproject(record, pythonLockDirs) {
			continue
		}
		sel.projects = append(sel.projects, p)
	}
	return sel
}

// toolSettingsPyproject reports a pyproject.toml with neither a project nor a
// build-system table and no Python lockfile beside it. Such a file holds tool
// settings, such as [tool.ruff], rather than a package.

func toolSettingsPyproject(record declarations.ProjectRecord, pythonLockDirs map[string]bool) bool {
	p := record.Project
	if p.Kind != "python" || path.Base(p.ID) != "pyproject.toml" || pythonLockDirs[p.Root] {
		return false
	}
	if record.PythonBuildSystemSeen {
		return false
	}
	projectTable := true
	for _, req := range p.Requirements {
		switch req.Kind {
		case "python-project-table":
			projectTable = req.State != "missing"
		case "python-build-backend", "python-build-requirement":
			return false
		}
	}
	return !projectTable
}

func projectRoles(projectList []declarations.Project) []RoleCount {
	counts := map[string]int64{}
	for _, p := range projectList {
		counts[pathrole.Of(p.ID)]++
	}
	return sortedRoleCounts(counts)
}

// projectRoots returns the distinct roots of counted projects and their roles.
// A root takes the highest-priority role among its projects.
func projectRoots(projectList []declarations.Project) (map[string]bool, []RoleCount) {
	byRoot := map[string][]string{}
	for _, p := range projectList {
		if p.Root != "" {
			byRoot[p.Root] = append(byRoot[p.Root], p.ID)
		}
	}
	roots := make(map[string]bool, len(byRoot))
	counts := map[string]int64{}
	for root, ids := range byRoot {
		roots[root] = true
		counts[pathrole.Of(ids...)]++
	}
	return roots, sortedRoleCounts(counts)
}

func sortedRoleCounts(counts map[string]int64) []RoleCount {
	out := make([]RoleCount, 0, len(counts))
	for role, n := range counts {
		out = append(out, RoleCount{Role: role, Count: n})
	}
	slices.SortFunc(out, func(a, b RoleCount) int { return strings.Compare(a.Role, b.Role) })
	return out
}

func relationshipMetric(projects []declarations.Project, byID map[string]string, relation string, projectsComplete bool, reasons []string) (Metric, []RelationshipCount, []RelationshipEvidence, int64) {
	totals := map[string]int64{}
	samples := &relationshipHeap{}
	seenEdges := map[string]bool{}
	var total int64
	for _, p := range projects {
		for _, ref := range p.References {
			kind := ref.Kind
			isWorkspace := isWorkspaceRelation(kind)
			isLocal := isLocalRelation(kind)
			if relation == "workspace" && !isWorkspace || relation == "local" && !isLocal {
				continue
			}
			if ref.Target == "" || ref.TargetStatus == "external" {
				continue
			}
			edgeKey := kind + "\x00" + p.ID + "\x00" + ref.Target + "\x00" + ref.State
			if seenEdges[edgeKey] {
				continue
			}
			seenEdges[edgeKey] = true
			total++
			totals[kind]++
			targetRoot := byID[ref.Target]
			e := RelationshipEvidence{Kind: kind, SourceProject: p.ID, SourceRoot: p.Root, TargetPath: ref.Target, State: ref.State}
			if targetRoot != "" {
				e.TargetRoot = targetRoot
			}
			if len(e.SourceProject) > MaxEvidencePathBytes || len(e.SourceRoot) > MaxEvidencePathBytes || len(e.TargetPath) > MaxEvidencePathBytes || len(e.TargetRoot) > MaxEvidencePathBytes {
				continue
			}
			if len(*samples) < RelationshipEvidenceLimit {
				heap.Push(samples, e)
				continue
			}
			if relationshipKey(e) < relationshipKey((*samples)[0]) {
				(*samples)[0] = e
				heap.Fix(samples, 0)
			}
		}
	}
	counts := make([]RelationshipCount, 0, len(totals))
	for kind, n := range totals {
		counts = append(counts, RelationshipCount{Kind: kind, Count: n})
	}
	slices.SortFunc(counts, func(a, b RelationshipCount) int { return strings.Compare(a.Kind, b.Kind) })
	evidence := []RelationshipEvidence{}
	evidence = append(evidence, (*samples)...)
	omitted := total - int64(len(evidence))
	slices.SortFunc(evidence, func(a, b RelationshipEvidence) int { return strings.Compare(relationshipKey(a), relationshipKey(b)) })
	scope := "supported explicit workspace/module declarations with a confined target; conditional declarations are counted without evaluating build logic"
	if relation == "local" {
		scope = "supported explicit local package/project references with a confined target; conditional declarations are counted without evaluating build logic"
	}
	return metric(total, scope, !projectsComplete, reasons), counts, evidence, omitted
}

func isWorkspaceRelation(kind string) bool {
	switch kind {
	case "solution-member", "module", "gradle-module", "npm-workspace-member", "uv-workspace-member", "go-workspace-member", "cargo-workspace-member", "cargo-workspace-path-member":
		return true
	default:
		return false
	}
}

func isLocalRelation(kind string) bool {
	switch kind {
	case "project-reference", "npm-local-dependency", "npm-workspace-dependency", "uv-local-dependency", "pub-path-dependency", "cargo-path-dependency", "cargo-workspace-path-dependency", "go-local-replacement":
		return true
	default:
		return false
	}
}

// isStructureLocalRelation extends isLocalRelation with "parent" for structural
// qualified-reference tallying only. It must NOT be used for the top-level
// local_dependencies metric (released in 1.4.0) because that metric predates
// parent-edge verification and because directory-default parent paths can
// silently count unverified edges.
func isStructureLocalRelation(kind string) bool {
	return isLocalRelation(kind) || kind == "parent"
}

func relationshipKey(e RelationshipEvidence) string {
	return e.Kind + "\x00" + e.SourceProject + "\x00" + e.TargetPath + "\x00" + e.SourceRoot + "\x00" + e.TargetRoot + "\x00" + e.State
}

type relationshipHeap []RelationshipEvidence

func (h relationshipHeap) Len() int           { return len(h) }
func (h relationshipHeap) Less(i, j int) bool { return relationshipKey(h[i]) > relationshipKey(h[j]) }
func (h relationshipHeap) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }
func (h *relationshipHeap) Push(v any)        { *h = append(*h, v.(RelationshipEvidence)) }
func (h *relationshipHeap) Pop() any {
	old := *h
	n := len(old)
	v := old[n-1]
	*h = old[:n-1]
	return v
}

// associationScope reports whether dircue's static lockfile association
// covers an ecosystem.
func associationScope(ecosystem string) bool {
	return ecosystem == "npm" || ecosystem == "nuget"
}

type lockfileTally struct {
	counts       map[string]int64
	roles        map[string]*LockfileRole
	reasons      map[[2]string]int64
	checks       LockfileChecks
	checkReasons map[string]int64
	presence     *LockfilePresence
	unknownWhy   map[string]int64
	causes       map[[2]string]int64
}

// addDetail tallies the check, presence, and cause evidence of one project's
// lockfile context.
func (t *lockfileTally) addDetail(ctx *lockfiles.Context, ecosystem, state, noContext string) {
	if ecosystem == "nuget" {
		if t.presence == nil {
			t.presence = &LockfilePresence{}
		}
		switch {
		case ctx == nil || ctx.NuGetEvidence == nil:
			t.presence.Unknown++
			t.unknownWhy[noContext]++
		case ctx.NuGetEvidence.PresenceState == "observed":
			t.presence.Observed++
		case ctx.NuGetEvidence.PresenceState == "not_observed":
			t.presence.NotObserved++
		default:
			t.presence.Unknown++
			for _, reason := range ctx.NuGetEvidence.PresenceReasons {
				t.unknownWhy[reason]++
			}
		}
	}
	if ctx == nil {
		return
	}
	checkStatus := ""
	if state == "covered" {
		// A valid lockfile report has exactly one check for an observed
		// association; anything else counts as indeterminate.
		checkStatus = "indeterminate"
		if len(ctx.Checks) == 1 {
			checkStatus = ctx.Checks[0].Status
		}
		switch checkStatus {
		case "match":
			t.checks.Match++
		case "different":
			t.checks.Different++
		case "not_applicable":
			t.checks.NotApplicable++
		default:
			t.checks.Indeterminate++
			reasons := map[string]bool{}
			if ctx.NuGetEvidence != nil {
				for _, reason := range ctx.NuGetEvidence.CheckReasons {
					reasons[reason] = true
				}
			} else {
				for _, b := range ctx.Boundaries {
					reasons[b.Reason] = true
				}
			}
			if len(reasons) == 0 {
				reasons["unspecified"] = true
			}
			for reason := range reasons {
				t.checkReasons[reason]++
			}
		}
	}
	// Causes explain an uncertain or missing outcome, or an indeterminate
	// check; they are not listed for settled results.
	if state == "covered" && checkStatus != "indeterminate" || state == "not_applicable" {
		return
	}
	seen := map[[2]string]bool{}
	if ctx.NuGetEvidence != nil {
		for _, cause := range ctx.NuGetEvidence.Causes {
			seen[[2]string{cause.Reason, cause.Path}] = true
		}
	} else if reason := ctx.OutcomeReason(); reason != "" && state != "covered" {
		for _, b := range ctx.Boundaries {
			if b.Reason == reason && b.Path != "" {
				seen[[2]string{b.Reason, b.Path}] = true
			}
		}
	}
	for key := range seen {
		t.causes[key]++
	}
}

func (t *lockfileTally) add(role, state, reason string, eligible bool) {
	t.counts["projects"]++
	t.counts[state]++
	r := t.roles[role]
	if r == nil {
		r = &LockfileRole{Role: role}
		t.roles[role] = r
	}
	r.Projects++
	switch state {
	case "covered":
		r.Covered++
	case "missing":
		r.Missing++
	case "not_applicable":
		r.NotApplicable++
	case "unsupported":
		r.Unsupported++
	default:
		r.Unknown++
	}
	if eligible {
		t.counts["eligible"]++
		r.Eligible++
	}
	if state != "covered" {
		t.reasons[[2]string{state, reason}]++
	}
}

func (t *lockfileTally) row(ecosystem string, complete bool, reasons []string) LockfileEcosystem {
	population := "parsed projects in this ecosystem"
	stateScope := "parsed projects in this ecosystem with this lockfile outcome"
	eligibleScope := "npm/NuGet projects whose outcome is covered, missing, or unknown; descriptive subset, not a policy requirement"
	switch {
	case ecosystem == "all":
		population = "parsed projects across ecosystems"
		stateScope = "parsed projects across ecosystems with this lockfile outcome"
	case !associationScope(ecosystem):
		eligibleScope = "not in npm/NuGet static association scope"
	}
	m := func(name, scope string) Metric { return metric(t.counts[name], scope, !complete, reasons) }
	out := LockfileEcosystem{Ecosystem: ecosystem, Projects: m("projects", population), Eligible: m("eligible", eligibleScope),
		Covered: m("covered", stateScope), Missing: m("missing", stateScope), NotApplicable: m("not_applicable", stateScope), Unsupported: m("unsupported", stateScope), Unknown: m("unknown", stateScope),
		ByRole: make([]LockfileRole, 0, len(t.roles)), OutcomeReasons: make([]OutcomeReason, 0, len(t.reasons))}
	checks := t.checks
	checks.IndeterminateReasons = sortedReasonCounts(t.checkReasons)
	out.Checks = &checks
	if t.presence != nil {
		presence := *t.presence
		presence.UnknownReasons = sortedReasonCounts(t.unknownWhy)
		out.NuGetPresence = &presence
	}
	for key, n := range t.causes {
		out.Causes = append(out.Causes, LockfileCause{Reason: key[0], Path: key[1], Count: n})
	}
	slices.SortFunc(out.Causes, compareLockfileCauses)
	if len(out.Causes) > LockfileCauseLimit {
		out.OmittedCauses = int64(len(out.Causes) - LockfileCauseLimit)
		out.Causes = out.Causes[:LockfileCauseLimit]
	}
	for _, role := range t.roles {
		out.ByRole = append(out.ByRole, *role)
	}
	slices.SortFunc(out.ByRole, func(a, b LockfileRole) int { return strings.Compare(a.Role, b.Role) })
	for key, n := range t.reasons {
		out.OutcomeReasons = append(out.OutcomeReasons, OutcomeReason{State: key[0], Reason: key[1], Count: n})
	}
	slices.SortFunc(out.OutcomeReasons, func(a, b OutcomeReason) int { return strings.Compare(outcomeReasonKey(a), outcomeReasonKey(b)) })
	return out
}

func outcomeReasonKey(o OutcomeReason) string { return o.State + "\x00" + o.Reason }

func sortedReasonCounts(m map[string]int64) []ReasonCount {
	out := make([]ReasonCount, 0, len(m))
	for reason, n := range m {
		out = append(out, ReasonCount{Reason: reason, Count: n})
	}
	slices.SortFunc(out, func(a, b ReasonCount) int { return strings.Compare(a.Reason, b.Reason) })
	return out
}

// compareLockfileCauses orders causes by descending count, then reason and
// path, so the bounded list keeps the files that explain the most projects.
func compareLockfileCauses(a, b LockfileCause) int {
	if a.Count != b.Count {
		if a.Count > b.Count {
			return -1
		}
		return 1
	}
	if n := strings.Compare(a.Reason, b.Reason); n != 0 {
		return n
	}
	return strings.Compare(a.Path, b.Path)
}

func lockfileMetrics(projectList []declarations.Project, locks *lockfiles.Report, complete bool, reasons []string) ([]LockfileEcosystem, LockfileEcosystem) {
	contexts := map[string]lockfiles.Context{}
	noContext := "lockfiles-not-run"
	if locks != nil {
		noContext = "no-lockfile-context"
		if locks.Status == "skipped" {
			noContext = "lockfiles-skipped"
		}
		for _, ctx := range locks.Contexts {
			key := ctx.Ecosystem + "\x00" + ctx.ProjectID
			if _, seen := contexts[key]; !seen {
				contexts[key] = ctx
			}
		}
	}
	newTally := func() *lockfileTally {
		return &lockfileTally{counts: map[string]int64{}, roles: map[string]*LockfileRole{}, reasons: map[[2]string]int64{}, checkReasons: map[string]int64{}, unknownWhy: map[string]int64{}, causes: map[[2]string]int64{}}
	}
	rows := map[string]*lockfileTally{}
	overall := newTally()
	for _, p := range projectList {
		ecosystem := projectEcosystem(p)
		state, reason := lockfileOutcome(ecosystem, p.ID, contexts, noContext)
		eligible := associationScope(ecosystem) && (state == "covered" || state == "missing" || state == "unknown")
		role := pathrole.Of(p.ID)
		if rows[ecosystem] == nil {
			rows[ecosystem] = newTally()
		}
		var ctx *lockfiles.Context
		if found, ok := contexts[ecosystem+"\x00"+p.ID]; ok && associationScope(ecosystem) {
			ctx = &found
		}
		for _, t := range []*lockfileTally{rows[ecosystem], overall} {
			t.add(role, state, reason, eligible)
			if associationScope(ecosystem) {
				t.addDetail(ctx, ecosystem, state, noContext)
			}
		}
	}
	ecosystems := make([]string, 0, len(rows))
	for ecosystem := range rows {
		ecosystems = append(ecosystems, ecosystem)
	}
	slices.Sort(ecosystems)
	out := make([]LockfileEcosystem, 0, len(ecosystems))
	for _, ecosystem := range ecosystems {
		out = append(out, rows[ecosystem].row(ecosystem, complete, reasons))
	}
	return out, overall.row("all", complete, reasons)
}

// lockfileOutcome maps a project's lockfile context to a report state and
// the reason that explains a non-covered state.
func lockfileOutcome(ecosystem, id string, contexts map[string]lockfiles.Context, noContext string) (string, string) {
	if !associationScope(ecosystem) {
		return "unsupported", "ecosystem-outside-association-scope"
	}
	ctx, ok := contexts[ecosystem+"\x00"+id]
	if !ok {
		return "unknown", noContext
	}
	state := "unknown"
	switch ctx.AssociationState {
	case "observed":
		return "covered", ""
	case "missing", "not_applicable", "unsupported":
		state = ctx.AssociationState
	}
	// A not_applicable project has nothing to lock; any boundary it carries,
	// such as an unshared ancestor lockfile, does not explain that state.
	if state == "not_applicable" {
		return state, "no-direct-declarations"
	}
	reason := ctx.OutcomeReason()
	if reason == "" {
		reason = "unspecified"
	}
	return state, reason
}

// projectEcosystem uses the manifest table, so a project and its manifest
// candidate always name the same ecosystem.
func projectEcosystem(p declarations.Project) string {
	if _, ecosystem, _ := classifyManifest(p.ID); ecosystem != "other" {
		return ecosystem
	}
	return p.Kind
}

// canonicalEcosystem normalizes a componentmap.Component's Ecosystem field to
// the assessment-layer canonical name, which matches the vocabulary produced by
// classifyManifest. The two vocabularies diverge on python-uv projects
// (componentmap: "python-uv"; assessment: "python") and on dotnet-kind projects
// (componentmap: "dotnet"; assessment: "nuget"). Normalizing at the point where
// components enter the assessment layer keeps populations, groups, coverage, and
// membership-reason maps consistent with one another.
func canonicalEcosystem(manifest, componentEcosystem string) string {
	if _, eco, _ := classifyManifest(manifest); eco != "other" {
		return eco
	}
	return componentEcosystem
}

func sortedCandidateCounts(m map[string]*CandidateCount) []CandidateCount {
	out := make([]CandidateCount, 0, len(m))
	for _, v := range m {
		out = append(out, *v)
	}
	slices.SortFunc(out, func(a, b CandidateCount) int {
		if n := strings.Compare(a.Filename, b.Filename); n != 0 {
			return n
		}
		if n := strings.Compare(a.Kind, b.Kind); n != 0 {
			return n
		}
		return strings.Compare(a.Ecosystem, b.Ecosystem)
	})
	return out
}

func sortedKindCounts(m map[string]*CandidateKindCount) []CandidateKindCount {
	out := make([]CandidateKindCount, 0, len(m))
	for _, v := range m {
		out = append(out, *v)
	}
	slices.SortFunc(out, func(a, b CandidateKindCount) int { return strings.Compare(a.Kind, b.Kind) })
	return out
}

// manifestClasses names the ecosystem and kind of every manifest filename
// group that declarations.IsManifest selects. Ecosystem names follow the
// declarations module, except that SDK-style .NET projects and NuGet files
// are nuget, the ecosystem of their lockfiles.
var manifestClasses = map[string][2]string{
	"package.json": {"npm", "manifest"},
	"go.mod":       {"go", "manifest"}, "go.work": {"go", "workspace"},
	"cargo.toml":     {"cargo", "manifest"},
	"pyproject.toml": {"python", "manifest"}, "setup.py": {"python", "manifest"}, "setup.cfg": {"python", "manifest"}, "pipfile": {"python", "manifest"},
	"requirements*.txt": {"python", "manifest"}, "requirements/*.txt": {"python", "manifest"},
	"gemfile": {"ruby-bundler", "manifest"}, "*.gemspec": {"ruby-gem", "manifest"}, "application.rb": {"ruby-bundler", "configuration"},
	"composer.json": {"php-composer", "manifest"},
	"package.swift": {"swift-package", "manifest"},
	"pubspec.yaml":  {"dart-pub", "manifest"},
	"mix.exs":       {"elixir-mix", "manifest"},
	"rebar.config":  {"erlang-rebar", "manifest"},
	"build.sbt":     {"scala-sbt", "manifest"},
	"stack.yaml":    {"haskell-stack", "manifest"}, "*.cabal": {"haskell-cabal", "manifest"},
	"cmakelists.txt": {"cmake", "manifest"}, "meson.build": {"meson", "manifest"}, "configure.ac": {"autoconf", "manifest"},
	"deno.json": {"deno", "manifest"}, "deno.jsonc": {"deno", "manifest"},
	"module.bazel": {"bazel-module", "manifest"}, "workspace": {"bazel-workspace", "workspace"},
	"build.zig":    {"zig-build", "manifest"},
	"project.toml": {"julia-project", "manifest"},
	"description":  {"r-package", "manifest"},
	"deps.edn":     {"clojure-deps", "manifest"}, "project.clj": {"clojure-leiningen", "manifest"},
	"cpanfile": {"perl-cpanfile", "manifest"}, "makefile.pl": {"perl-extutils", "manifest"},
	"kbuild": {"kbuild-kconfig", "manifest"}, "kconfig": {"kbuild-kconfig", "configuration"},
	"*.csproj": {"nuget", "manifest"}, "*.fsproj": {"nuget", "manifest"}, "*.vbproj": {"nuget", "manifest"}, "packages.config": {"nuget", "manifest"},
	"directory.packages.props": {"nuget", "configuration"}, "nuget.config": {"nuget", "configuration"},
	"*.vcxproj": {"dotnet", "manifest"}, "*.sqlproj": {"dotnet", "manifest"}, "*.wixproj": {"dotnet", "manifest"}, "*.shproj": {"dotnet", "manifest"}, "*.proj": {"dotnet", "manifest"},
	"*.sln": {"dotnet", "solution"}, "*.slnx": {"dotnet", "solution"}, "*.slnf": {"dotnet", "solution"},
	"directory.build.props": {"dotnet", "configuration"}, "directory.build.targets": {"dotnet", "configuration"},
	"global.json": {"dotnet", "toolchain"},
	"pom.xml":     {"maven", "manifest"}, "toolchains.xml": {"maven", "toolchain"},
	"build.gradle": {"gradle", "manifest"}, "build.gradle.kts": {"gradle", "manifest"},
	"settings.gradle": {"gradle", "workspace"}, "settings.gradle.kts": {"gradle", "workspace"},
	"gradle.properties": {"gradle", "configuration"}, "gradle-wrapper.properties": {"gradle", "toolchain"},
}

// classifyManifest returns a selected manifest's filename group, ecosystem,
// and kind. A name outside the table is ecosystem "other".
func classifyManifest(selectedPath string) (filename, ecosystem, kind string) {
	filename = manifestFilenameType(selectedPath)
	if class, ok := manifestClasses[strings.ToLower(filename)]; ok {
		return filename, class[0], class[1]
	}
	return filename, "other", "manifest"
}

func manifestFilenameType(selectedPath string) string {
	// Dotnet's selected parser accepts both POSIX separators and normalized
	// MSBuild-style backslashes. Match its basename for bounded grouping while
	// retaining the untouched selected path in evidence.
	base := path.Base(strings.ReplaceAll(selectedPath, `\`, "/"))
	lower := strings.ToLower(base)
	if strings.HasPrefix(lower, "requirements") && strings.HasSuffix(lower, ".txt") {
		return "requirements*.txt"
	}
	if path.Base(path.Dir(selectedPath)) == "requirements" && strings.HasSuffix(lower, ".txt") {
		return "requirements/*.txt"
	}
	for _, ext := range []string{".csproj", ".fsproj", ".vbproj", ".vcxproj", ".sqlproj", ".wixproj", ".shproj", ".proj", ".sln", ".slnx", ".slnf", ".cabal", ".gemspec"} {
		if strings.HasSuffix(lower, ext) {
			return "*" + ext
		}
	}
	switch lower {
	case "global.json", "directory.build.props", "directory.build.targets", "directory.packages.props", "nuget.config", "packages.config":
		return lower
	}
	return base
}

func mapKeys(m map[string]int64) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		if !strings.HasPrefix(k, "candidate_evidence:") {
			out = append(out, k)
		}
	}
	slices.Sort(out)
	return out
}

func mapKeysBool(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}

func definitions() []Definition {
	return []Definition{
		{ID: "inventory_bytes", Description: "Logical file bytes count selected regular-file sizes, including lockfiles and data, without archive expansion."},
		{ID: "inventory_vendored", Description: "Vendored files match Linguist's vendor path rules (for example node_modules/, vendor/, dist/, test/fixtures/, and .github/) or a linguist-vendored attribute. Installed environments outside those rules, such as .venv, stay in the unvendored remainder."},
		{ID: "manifest_candidate_population", Description: "Manifest candidates are selected files with a filename the declarations parser reads, excluding installed node_modules. Each group names an ecosystem and a kind: manifest, workspace, solution, configuration, or toolchain. Projects use the same ecosystem table."},
		{ID: "unparsed_manifest_candidates", Description: "Unparsed candidates are selected manifests that the declaration parser did not read in full and interpret, including manifests it reported as invalid or unsupported. A manifest merged into another project record, read in full and found to hold no declarations, or parsed and then dropped at the declaration report size limit is not counted; the last makes project counts lower bounds instead."},
		{ID: "filename_candidate_population", Description: "Filename candidate counts are exact across selected files; bounded candidate evidence does not reduce these totals. Lockfiles and checksum files form the lockfile kind."},
		{ID: "projects", Description: "Projects are parsed package and project records. Virtual workspace roots (go.work, a Cargo manifest with only [workspace], a uv workspace without [project]) are excluded but still contribute workspace membership. Configuration, solution, and annotation records (such as config/application.rb), Cargo manifests without a package or workspace table, and pyproject.toml files with neither a project nor a build-system table and no Python lockfile beside them are also excluded."},
		{ID: "project_roles", Description: "Roles come from conventional path names, such as test/, fixtures/, examples/, vendor/, docs/, tools/, and .NET *.Tests projects; other paths are primary. A root takes the highest-priority role among its projects, in the order vendored, fixture, example, test, docs, tooling."},
		{ID: "project_roots", Description: "Distinct roots are counted only from counted projects; declaration parser caps can make this a lower bound."},
		{ID: "workspace_membership", Description: "Only explicit supported workspace or module references are counted; Cargo default-member selection remains a distinct declaration and is not counted as general workspace membership. Selected pnpm-workspace.yaml, lerna.json, and rush.json files are not parsed and make this and local_dependencies lower bounds."},
		{ID: "local_dependencies", Description: "Only explicit supported local dependency or project references with a confined target are counted."},
		{ID: "lockfile_association", Description: "Covered means a static project-to-lockfile association was observed; it does not establish consistency or a successful locked restore. npm associations follow npm's workspace-root selection: the nearest ancestor whose workspaces list a project owns its lockfile."},
		{ID: "lockfile_eligible", Description: "Eligible counts npm and NuGet projects whose outcome is covered, missing, or unknown. Not_applicable projects declare no direct dependencies; unsupported projects use another package manager or an unsupported lockfile. Eligibility is descriptive, not a policy requirement."},
		{ID: "lockfile_states", Description: "Covered, missing, not_applicable, unsupported, and unknown partition the project population. Outcome reasons name the lockfile boundary reason behind each non-covered project, and no-direct-declarations for each not_applicable project; by_role partitions each row by project role."},
		{ID: "source_consistency", Description: "Directory mode uses live metadata; Git mode refers to the selected Git tree."},
		{ID: "unsupported_ecosystems", Description: "Parsed projects outside npm and NuGet static lockfile association are counted as unsupported; this is not a health judgment."},
	}
}

// ValidateReport checks the native aggregate contract. It deliberately checks
// aggregate totals independently from bounded evidence samples.
func ValidateReport(r *Report) error {
	if r == nil {
		return errors.New("assessment report is nil")
	}
	if r.Version != Version && r.Version != LegacyVersion {
		return fmt.Errorf("unsupported assessment version %q", r.Version)
	}
	if r.Version == LegacyVersion && r.Structure != nil {
		return errors.New("legacy assessment version cannot contain structural summary")
	}
	if r.Version == Version && r.Structure == nil {
		return errors.New("assessment structural summary is required")
	}
	if !validSource(r.Source.Mode, r.Source.Tree) {
		return errors.New("assessment source identity is invalid")
	}
	if (r.Source.Mode == "directory" && r.Source.Commit != "") || (r.Source.Commit != "" && !validHexHash(r.Source.Commit)) {
		return errors.New("assessment source commit is invalid for its source mode")
	}
	wantConsistency := "live_directory_metadata"
	if r.Source.Mode == "git" {
		wantConsistency = "selected_git_tree"
	}
	if r.Source.Consistency != wantConsistency {
		return errors.New("assessment source consistency does not match source mode")
	}
	for _, m := range r.Metrics() {
		if err := validateMetric(m.Metric); err != nil {
			return fmt.Errorf("%s: %w", m.Name, err)
		}
		// connected_groups cannot be lower_bound: missing edges can only merge
		// components (reducing the count), so an incomplete edge set is an upper
		// bound, not a lower bound.
		if m.Name == "structure:connected_groups" && m.Metric.Completeness == "lower_bound" {
			return errors.New("structure:connected_groups: lower_bound completeness is not valid; use upper_bound or observed_only")
		}
	}
	if r.Inventory.VendoredFiles.Count > r.Inventory.Files.Count || r.Inventory.VendoredBytes.Count > r.Inventory.Bytes.Count {
		return errors.New("vendored inventory exceeds the selected inventory")
	}
	if r.Projects.Count != r.LockfilesOverall.Projects.Count {
		return errors.New("project total does not match overall lockfile project population")
	}
	var manifestFiles int64
	if len(r.ManifestCandidates) > 128 {
		return errors.New("manifest candidate groups exceed the schema limit")
	}
	var manifestBytes int64
	manifestBytesOverflow := false
	for i, c := range r.ManifestCandidates {
		if c.Filename == "" || !manifestKinds[c.Kind] || c.Files < 0 || c.Bytes < 0 || c.Ecosystem == "" {
			return errors.New("manifest candidate group is invalid")
		}
		if i > 0 && candidateCountKey(r.ManifestCandidates[i-1]) >= candidateCountKey(c) {
			return errors.New("manifest candidate groups are not strictly sorted")
		}
		var ok bool
		manifestFiles, ok = checkedAdd(manifestFiles, c.Files)
		if !ok {
			return errors.New("manifest candidate file totals overflow")
		}
		manifestBytes, ok = checkedAdd(manifestBytes, c.Bytes)
		manifestBytesOverflow = manifestBytesOverflow || !ok
	}
	if manifestBytesOverflow && !metricHasReason(r.Inventory.Bytes, "inventory_bytes_overflow") {
		return errors.New("manifest candidate bytes overflow without matching inventory overflow")
	}
	if manifestFiles != r.ManifestCandidatePopulation.Count || manifestFiles > r.Inventory.Files.Count || !manifestBytesOverflow && manifestBytes > r.Inventory.Bytes.Count {
		return errors.New("manifest candidate groups do not reconcile with their population metric")
	}
	if r.UnparsedManifestCandidates.Count > r.ManifestCandidatePopulation.Count {
		return errors.New("unparsed manifest candidates exceed the manifest candidate population")
	}
	var filenameFiles int64
	var filenameBytes int64
	filenameBytesOverflow := false
	for i, c := range r.FilenameCandidates {
		if c.Kind == "" || c.Files < 0 || c.Bytes < 0 {
			return errors.New("filename candidate group is invalid")
		}
		if i > 0 && r.FilenameCandidates[i-1].Kind >= c.Kind {
			return errors.New("filename candidate groups are not strictly sorted")
		}
		var ok bool
		filenameFiles, ok = checkedAdd(filenameFiles, c.Files)
		if !ok {
			return errors.New("filename candidate file totals overflow")
		}
		filenameBytes, ok = checkedAdd(filenameBytes, c.Bytes)
		filenameBytesOverflow = filenameBytesOverflow || !ok
	}
	if filenameBytesOverflow && !metricHasReason(r.Inventory.Bytes, "inventory_bytes_overflow") {
		return errors.New("filename candidate bytes overflow without matching inventory overflow")
	}
	if filenameFiles != r.FilenameCandidatePopulation.Count || filenameFiles > r.Inventory.Files.Count || !filenameBytesOverflow && filenameBytes > r.Inventory.Bytes.Count {
		return errors.New("filename candidate groups do not reconcile with their population metric")
	}
	if err := validateCandidateEvidence(r); err != nil {
		return err
	}
	rootEvidenceTotal, ok := checkedSum(int64(len(r.ProjectRootEvidence)), r.OmittedProjectRootEvidence)
	if len(r.ProjectRootEvidence) > ProjectRootEvidenceLimit || !ok || rootEvidenceTotal != r.ProjectRoots.Count || r.OmittedProjectRootEvidence < 0 || r.ProjectRoots.Count > r.Projects.Count {
		return errors.New("project root evidence does not reconcile with the root metric")
	}
	for i, root := range r.ProjectRootEvidence {
		if !validRoot(root) {
			return errors.New("project root evidence contains an invalid root")
		}
		if i > 0 && r.ProjectRootEvidence[i-1] >= root {
			return errors.New("project root evidence is not strictly sorted")
		}
	}
	if err := validateRoleCounts(r.ProjectsByRole, r.Projects.Count); err != nil {
		return fmt.Errorf("projects by role: %w", err)
	}
	if err := validateRoleCounts(r.ProjectRootsByRole, r.ProjectRoots.Count); err != nil {
		return fmt.Errorf("project roots by role: %w", err)
	}
	if err := validateRelationships(r.WorkspaceMembership, r.WorkspaceByKind, r.WorkspaceEvidence, r.OmittedWorkspaceEvidence); err != nil {
		return fmt.Errorf("workspace membership: %w", err)
	}
	if err := validateRelationships(r.LocalDependencies, r.LocalDependencyByKind, r.LocalDependencyEvidence, r.OmittedLocalEvidence); err != nil {
		return fmt.Errorf("local dependencies: %w", err)
	}
	if err := validateLockfileRows(r); err != nil {
		return err
	}
	if r.Structure != nil {
		if err := validateStructure(r.Structure, r.Projects.Count, r.ManifestCandidatePopulation.Count); err != nil {
			return fmt.Errorf("structure: %w", err)
		}
	}
	encoded, err := json.Marshal(r)
	if err != nil || len(encoded) > MaxAssessmentJSONBytes {
		return errors.New("assessment JSON exceeds its serialized size limit")
	}
	return nil
}

var manifestKinds = map[string]bool{"manifest": true, "workspace": true, "solution": true, "configuration": true, "toolchain": true}

var projectRoleNames = map[string]bool{pathrole.Primary: true, pathrole.Vendored: true, pathrole.Fixture: true, pathrole.Example: true, pathrole.Test: true, pathrole.Docs: true, pathrole.Tooling: true}

func validateRoleCounts(counts []RoleCount, total int64) error {
	var sum int64
	for i, c := range counts {
		if !projectRoleNames[c.Role] || c.Count <= 0 || i > 0 && counts[i-1].Role >= c.Role {
			return errors.New("role groups are invalid or unsorted")
		}
		var ok bool
		if sum, ok = checkedAdd(sum, c.Count); !ok {
			return errors.New("role totals overflow")
		}
	}
	if sum != total {
		return errors.New("role groups do not partition their metric")
	}
	return nil
}

func validateMetric(m Metric) error {
	if m.Count < 0 || strings.TrimSpace(m.Scope) == "" {
		return errors.New("metric has a negative count or empty scope")
	}
	switch m.Completeness {
	case "complete", "lower_bound", "upper_bound", "observed_only":
	default:
		return errors.New("metric completeness must be complete, lower_bound, upper_bound, or observed_only")
	}
	if (m.Completeness == "lower_bound" || m.Completeness == "upper_bound" || m.Completeness == "observed_only") && len(m.Reasons) == 0 {
		return errors.New("non-complete metric has no reason")
	}
	if m.Completeness == "complete" && len(m.Reasons) != 0 {
		return errors.New("complete metric cannot carry omission reasons")
	}
	for i, reason := range m.Reasons {
		if reason == "" || i > 0 && m.Reasons[i-1] >= reason {
			return errors.New("metric reasons must be non-empty, sorted, and unique")
		}
	}
	return nil
}

// metricC creates a Metric with an explicit completeness string and reasons.
// Use "complete", "lower_bound", "upper_bound", or "observed_only".
func metricC(count int64, scope, completeness string, reasons []string) Metric {
	return Metric{Count: count, Scope: scope, Completeness: completeness, Reasons: slices.Clone(reasons)}
}

func validateCandidateEvidence(r *Report) error {
	counts := map[string]int64{}
	lastPath := ""
	lastKind := ""
	for _, c := range r.CandidateEvidence {
		if !validSelectedPath(c.Path) || len(c.Path) > MaxEvidencePathBytes || !validRoot(c.Root) || len(c.Root) > MaxEvidencePathBytes || path.Dir(c.Path) != c.Root || c.Kind == "" || c.Format == "" || c.Basis == "" || c.Bytes < 0 {
			return errors.New("candidate evidence entry is invalid")
		}
		if lastPath != "" && (lastPath > c.Path || lastPath == c.Path && lastKind >= c.Kind) {
			return errors.New("candidate evidence is not strictly sorted")
		}
		lastPath, lastKind = c.Path, c.Kind
		if path.Dir(c.Path) != c.Root {
			return errors.New("candidate evidence root does not match its selected path")
		}
		counts[c.Kind]++
		if counts[c.Kind] > EvidenceLimitPerKind {
			return errors.New("candidate evidence exceeds its per-kind limit")
		}
	}
	kindTotals := map[string]int64{}
	for _, c := range r.FilenameCandidates {
		kindTotals[c.Kind] = c.Files
	}
	for kind, n := range r.OmittedCandidateEvidence {
		if kind == "" || n <= 0 {
			return errors.New("omitted candidate evidence count is invalid")
		}
		if _, ok := kindTotals[kind]; !ok {
			return errors.New("omitted evidence names an unknown candidate kind")
		}
	}
	for kind, total := range kindTotals {
		got, ok := checkedAdd(counts[kind], r.OmittedCandidateEvidence[kind])
		if !ok || got != total {
			return fmt.Errorf("candidate evidence for %q does not reconcile", kind)
		}
	}
	for kind := range counts {
		if _, ok := kindTotals[kind]; !ok {
			return errors.New("candidate evidence names an unknown candidate kind")
		}
	}
	return nil
}

func validateRelationships(m Metric, byKind []RelationshipCount, evidence []RelationshipEvidence, omitted int64) error {
	if len(evidence) > RelationshipEvidenceLimit {
		return errors.New("relationship evidence exceeds the aggregate sample limit")
	}
	var total int64
	kindTotals := map[string]int64{}
	for i, group := range byKind {
		if group.Kind == "" || group.Count <= 0 || i > 0 && byKind[i-1].Kind >= group.Kind {
			return errors.New("relationship kind groups are invalid or unsorted")
		}
		var ok bool
		total, ok = checkedAdd(total, group.Count)
		if !ok {
			return errors.New("relationship group totals overflow")
		}
		kindTotals[group.Kind] = group.Count
	}
	if total != m.Count || omitted < 0 || int64(len(evidence))+omitted != m.Count {
		return errors.New("relationship counts or evidence do not reconcile")
	}
	last := ""
	kindEvidenceCounts := map[string]int{}
	for _, e := range evidence {
		if e.Kind == "" || e.State == "" || !validSelectedPath(e.SourceProject) || len(e.SourceProject) > MaxEvidencePathBytes || !validRoot(e.SourceRoot) || len(e.SourceRoot) > MaxEvidencePathBytes || path.Dir(e.SourceProject) != e.SourceRoot || !validSelectedPath(e.TargetPath) || len(e.TargetPath) > MaxEvidencePathBytes || e.TargetRoot != "" && (!validRoot(e.TargetRoot) || len(e.TargetRoot) > MaxEvidencePathBytes || path.Dir(e.TargetPath) != e.TargetRoot) {
			return errors.New("relationship evidence is invalid")
		}
		key := relationshipKey(e)
		if last != "" && last >= key {
			return errors.New("relationship evidence is duplicate or unsorted")
		}
		last = key
		kindEvidenceCounts[e.Kind]++
		if _, ok := kindTotals[e.Kind]; !ok || int64(kindEvidenceCounts[e.Kind]) > kindTotals[e.Kind] {
			return errors.New("relationship evidence kind does not reconcile with its count group")
		}
		if kindEvidenceCounts[e.Kind] > RelationshipEvidenceLimit {
			return errors.New("relationship evidence exceeds its per-kind limit")
		}
	}
	return nil
}

func validateLockfileRows(r *Report) error {
	if len(r.Lockfiles) > 128 {
		return errors.New("lockfile ecosystem rows exceed schema limit")
	}
	sum := lockfileTotals{roles: map[string]LockfileRole{}, reasons: map[string]int64{}}
	var unsupportedEcosystemTotal int64
	for i, row := range r.Lockfiles {
		if row.Ecosystem == "" || row.Ecosystem == "all" || i > 0 && r.Lockfiles[i-1].Ecosystem >= row.Ecosystem {
			return errors.New("lockfile ecosystem rows are invalid or unsorted")
		}
		if err := validateLockfileRow(row, r.Version == Version); err != nil {
			return fmt.Errorf("lockfile row %q: %w", row.Ecosystem, err)
		}
		if associationScope(row.Ecosystem) {
			if eligible, ok := checkedSum(row.Covered.Count, row.Missing.Count, row.Unknown.Count); !ok || row.Eligible.Count != eligible {
				return fmt.Errorf("lockfile row %q: eligible is not covered + missing + unknown", row.Ecosystem)
			}
		} else if row.Eligible.Count != 0 || row.Unsupported.Count != row.Projects.Count {
			return fmt.Errorf("lockfile row %q: an ecosystem outside association scope must be wholly unsupported", row.Ecosystem)
		} else {
			var ok bool
			if unsupportedEcosystemTotal, ok = checkedAdd(unsupportedEcosystemTotal, row.Projects.Count); !ok {
				return errors.New("unsupported ecosystem project totals overflow")
			}
		}
		if err := sum.add(row); err != nil {
			return err
		}
	}
	o := r.LockfilesOverall
	if o.Ecosystem != "all" {
		return errors.New("overall lockfile row is not named all")
	}
	if err := validateLockfileRow(o, r.Version == Version); err != nil {
		return fmt.Errorf("overall lockfile row: %w", err)
	}
	overall := lockfileTotals{roles: map[string]LockfileRole{}, reasons: map[string]int64{}}
	if err := overall.add(o); err != nil {
		return err
	}
	if overall.counts != sum.counts || !maps.Equal(overall.roles, sum.roles) || !maps.Equal(overall.reasons, sum.reasons) || overall.checks != sum.checks || !maps.Equal(overall.checkReasons, sum.checkReasons) {
		return errors.New("overall lockfile totals do not reconcile with ecosystem rows")
	}
	var nugetPresence *LockfilePresence
	for _, row := range r.Lockfiles {
		if row.Ecosystem == "nuget" {
			nugetPresence = row.NuGetPresence
		}
	}
	if (nugetPresence == nil) != (o.NuGetPresence == nil) || nugetPresence != nil && !reflect.DeepEqual(*nugetPresence, *o.NuGetPresence) {
		return errors.New("overall NuGet presence does not match the nuget row")
	}
	if o.Projects.Count != r.Projects.Count {
		return errors.New("lockfile overall projects do not match assessment projects")
	}
	if r.UnsupportedEcosystemProjects.Count != unsupportedEcosystemTotal {
		return errors.New("unsupported ecosystem metric does not reconcile with ecosystem rows")
	}
	return nil
}

var outcomeStates = map[string]bool{"missing": true, "not_applicable": true, "unsupported": true, "unknown": true}

// validateLockfileRow checks that one row's states partition its projects
// and that its role and reason breakdowns reconcile with its metrics.
func validateLockfileRow(row LockfileEcosystem, current bool) error {
	states, ok := checkedSum(row.Covered.Count, row.Missing.Count, row.NotApplicable.Count, row.Unsupported.Count, row.Unknown.Count)
	if !ok || states != row.Projects.Count {
		return errors.New("lockfile states do not partition project totals")
	}
	if eligible, ok := checkedSum(row.Covered.Count, row.Missing.Count, row.Unknown.Count); !ok || row.Eligible.Count > eligible {
		return errors.New("lockfile eligibility exceeds covered + missing + unknown")
	}
	var roles LockfileRole
	for i, role := range row.ByRole {
		if !projectRoleNames[role.Role] || role.Projects <= 0 || i > 0 && row.ByRole[i-1].Role >= role.Role {
			return errors.New("lockfile role groups are invalid or unsorted")
		}
		roleStates, ok := checkedSum(role.Covered, role.Missing, role.NotApplicable, role.Unsupported, role.Unknown)
		if !ok || roleStates != role.Projects || role.Eligible < 0 || role.Eligible > role.Covered+role.Missing+role.Unknown {
			return errors.New("lockfile role states do not partition role projects")
		}
		for target, value := range map[*int64]int64{&roles.Projects: role.Projects, &roles.Eligible: role.Eligible, &roles.Covered: role.Covered, &roles.Missing: role.Missing, &roles.NotApplicable: role.NotApplicable, &roles.Unsupported: role.Unsupported, &roles.Unknown: role.Unknown} {
			if *target, ok = checkedAdd(*target, value); !ok {
				return errors.New("lockfile role totals overflow")
			}
		}
	}
	if roles != (LockfileRole{Projects: row.Projects.Count, Eligible: row.Eligible.Count, Covered: row.Covered.Count, Missing: row.Missing.Count, NotApplicable: row.NotApplicable.Count, Unsupported: row.Unsupported.Count, Unknown: row.Unknown.Count}) {
		return errors.New("lockfile role groups do not reconcile with row metrics")
	}
	byState := map[string]int64{}
	for i, reason := range row.OutcomeReasons {
		if !outcomeStates[reason.State] || !validOutcomeReason(reason.Reason) || reason.Count <= 0 || i > 0 && outcomeReasonKey(row.OutcomeReasons[i-1]) >= outcomeReasonKey(reason) {
			return errors.New("lockfile outcome reasons are invalid or unsorted")
		}
		if byState[reason.State], ok = checkedAdd(byState[reason.State], reason.Count); !ok {
			return errors.New("lockfile outcome reason totals overflow")
		}
	}
	if byState["missing"] != row.Missing.Count || byState["not_applicable"] != row.NotApplicable.Count || byState["unsupported"] != row.Unsupported.Count || byState["unknown"] != row.Unknown.Count {
		return errors.New("lockfile outcome reasons do not reconcile with row states")
	}
	if !current {
		if row.Checks != nil || row.NuGetPresence != nil || row.Causes != nil || row.OmittedCauses != 0 {
			return errors.New("legacy lockfile row contains newer evidence")
		}
		return nil
	}
	c := row.Checks
	if c == nil {
		return errors.New("lockfile row lacks check statuses")
	}
	if checks, ok := checkedSum(c.Match, c.Different, c.Indeterminate, c.NotApplicable); !ok || checks != row.Covered.Count {
		return errors.New("lockfile check statuses do not partition covered projects")
	}
	if err := validateReasonCounts(c.IndeterminateReasons, c.Indeterminate); err != nil {
		return fmt.Errorf("lockfile check reasons: %w", err)
	}
	if p := row.NuGetPresence; p != nil {
		if row.Ecosystem != "nuget" && row.Ecosystem != "all" {
			return errors.New("NuGet presence appears outside the nuget and all rows")
		}
		total, ok := checkedSum(p.Observed, p.NotObserved, p.Unknown)
		if !ok || row.Ecosystem == "nuget" && total != row.Projects.Count || total > row.Projects.Count {
			return errors.New("NuGet presence does not partition NuGet projects")
		}
		if err := validateReasonCounts(p.UnknownReasons, p.Unknown); err != nil {
			return fmt.Errorf("NuGet presence reasons: %w", err)
		}
	} else if row.Ecosystem == "nuget" {
		return errors.New("nuget row lacks NuGet presence")
	}
	if len(row.Causes) > LockfileCauseLimit || row.OmittedCauses < 0 || row.OmittedCauses > 0 && len(row.Causes) != LockfileCauseLimit {
		return errors.New("lockfile causes exceed their bound")
	}
	seenCauses := make(map[[2]string]bool, len(row.Causes))
	for i, cause := range row.Causes {
		key := [2]string{cause.Reason, cause.Path}
		if !validOutcomeReason(cause.Reason) || !validSelectedPath(cause.Path) || cause.Count <= 0 || cause.Count > row.Projects.Count || seenCauses[key] || i > 0 && compareLockfileCauses(row.Causes[i-1], cause) >= 0 {
			return errors.New("lockfile causes are invalid, duplicated, or unsorted")
		}
		seenCauses[key] = true
	}
	return nil
}

// validateReasonCounts checks reasons that can each count a project once:
// every counted project has at least one reason, and no reason exceeds the
// population.
func validateReasonCounts(reasons []ReasonCount, population int64) error {
	if reasons == nil {
		return errors.New("reasons are absent")
	}
	var total int64
	for i, reason := range reasons {
		var ok bool
		if !validOutcomeReason(reason.Reason) || reason.Count <= 0 || reason.Count > population || i > 0 && reasons[i-1].Reason >= reason.Reason {
			return errors.New("reasons are invalid or unsorted")
		}
		if total, ok = checkedAdd(total, reason.Count); !ok {
			return errors.New("reason totals overflow")
		}
	}
	if total < population {
		return errors.New("some counted projects have no reason")
	}
	return nil
}

func validOutcomeReason(reason string) bool {
	if reason == "" || len(reason) > 256 || reason[0] == '-' || reason[len(reason)-1] == '-' {
		return false
	}
	for _, r := range reason {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-') {
			return false
		}
	}
	return true
}

// lockfileTotals sums rows for the overall reconciliation.
type lockfileTotals struct {
	counts       LockfileRole
	roles        map[string]LockfileRole
	reasons      map[string]int64
	checks       [4]int64
	checkReasons map[string]int64
}

func (t *lockfileTotals) add(row LockfileEcosystem) error {
	values := LockfileRole{Projects: row.Projects.Count, Eligible: row.Eligible.Count, Covered: row.Covered.Count, Missing: row.Missing.Count, NotApplicable: row.NotApplicable.Count, Unsupported: row.Unsupported.Count, Unknown: row.Unknown.Count}
	next, ok := addLockfileRole(t.counts, values)
	if !ok {
		return errors.New("lockfile totals overflow")
	}
	t.counts = next
	for _, role := range row.ByRole {
		name := role.Role
		role.Role = ""
		next, ok := addLockfileRole(t.roles[name], role)
		if !ok {
			return errors.New("lockfile role totals overflow")
		}
		t.roles[name] = next
	}
	for _, reason := range row.OutcomeReasons {
		key := outcomeReasonKey(reason)
		if t.reasons[key], ok = checkedAdd(t.reasons[key], reason.Count); !ok {
			return errors.New("lockfile reason totals overflow")
		}
	}
	if row.Checks == nil {
		return nil
	}
	for i, n := range []int64{row.Checks.Match, row.Checks.Different, row.Checks.Indeterminate, row.Checks.NotApplicable} {
		if t.checks[i], ok = checkedAdd(t.checks[i], n); !ok {
			return errors.New("lockfile check totals overflow")
		}
	}
	if t.checkReasons == nil {
		t.checkReasons = map[string]int64{}
	}
	for _, reason := range row.Checks.IndeterminateReasons {
		if t.checkReasons[reason.Reason], ok = checkedAdd(t.checkReasons[reason.Reason], reason.Count); !ok {
			return errors.New("lockfile check reason totals overflow")
		}
	}
	return nil
}

func addLockfileRole(a, b LockfileRole) (LockfileRole, bool) {
	out := a
	var ok bool
	for _, pair := range [][3]*int64{{&out.Projects, &a.Projects, &b.Projects}, {&out.Eligible, &a.Eligible, &b.Eligible}, {&out.Covered, &a.Covered, &b.Covered}, {&out.Missing, &a.Missing, &b.Missing}, {&out.NotApplicable, &a.NotApplicable, &b.NotApplicable}, {&out.Unsupported, &a.Unsupported, &b.Unsupported}, {&out.Unknown, &a.Unknown, &b.Unknown}} {
		if *pair[0], ok = checkedAdd(*pair[1], *pair[2]); !ok {
			return LockfileRole{}, false
		}
	}
	return out, true
}

func checkedAdd(a, b int64) (int64, bool) {
	if a < 0 || b < 0 || b > math.MaxInt64-a {
		return 0, false
	}
	return a + b, true
}

func checkedSum(values ...int64) (int64, bool) {
	var total int64
	for _, value := range values {
		var ok bool
		total, ok = checkedAdd(total, value)
		if !ok {
			return 0, false
		}
	}
	return total, true
}

func metricHasReason(m Metric, want string) bool {
	if m.Completeness == "complete" {
		return false
	}
	for _, reason := range m.Reasons {
		if reason == want {
			return true
		}
	}
	return false
}

func candidateCountKey(c CandidateCount) string {
	return c.Filename + "\x00" + c.Kind + "\x00" + c.Ecosystem
}
func validRoot(root string) bool { return root == "." || validSelectedPath(root) }

type candidateHeap []discovery.Candidate

func (h candidateHeap) Len() int           { return len(h) }
func (h candidateHeap) Less(i, j int) bool { return h[i].Path > h[j].Path }
func (h candidateHeap) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }
func (h *candidateHeap) Push(v any)        { *h = append(*h, v.(discovery.Candidate)) }
func (h *candidateHeap) Pop() any          { old := *h; n := len(old); v := old[n-1]; *h = old[:n-1]; return v }
