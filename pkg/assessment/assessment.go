// Package assessment combines bounded inventory, declaration, and lockfile
// evidence without interpreting build behavior or dependency resolution.
package assessment

import (
	"container/heap"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"path"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/war-and-code/dircue/pkg/declarations"
	"github.com/war-and-code/dircue/pkg/discovery"
	"github.com/war-and-code/dircue/pkg/lockfiles"
	"github.com/war-and-code/dircue/pkg/projects"
)

const (
	Version                   = "1.0.0"
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
	Completeness string   `json:"completeness"` // complete or lower_bound
	Reasons      []string `json:"reasons"`
}

type CandidateCount struct {
	Filename  string `json:"filename"`
	Kind      string `json:"kind"`
	Ecosystem string `json:"ecosystem,omitempty"`
	Files     int64  `json:"files"`
	Bytes     int64  `json:"bytes"`
}

type CandidateKindCount struct {
	Kind  string `json:"kind"`
	Files int64  `json:"files"`
	Bytes int64  `json:"bytes"`
}

type InventoryMetrics struct {
	Files Metric `json:"files"`
	Bytes Metric `json:"bytes"`
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

type LockfileEcosystem struct {
	Ecosystem     string `json:"ecosystem"`
	Projects      Metric `json:"projects"`
	Eligible      Metric `json:"eligible"`
	Covered       Metric `json:"covered"`
	Missing       Metric `json:"missing"`
	NotApplicable Metric `json:"not_applicable"`
	Unsupported   Metric `json:"unsupported"`
	Unknown       Metric `json:"unknown"`
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
	Projects                     Metric                 `json:"projects"`
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
}

type Collector struct {
	mode, tree      string
	files           int64
	bytes           int64
	invalid         map[string]int64
	candidateCounts map[string]*CandidateCount
	kindCounts      map[string]*CandidateKindCount
	candidates      map[string]*candidateHeap
	status          string
	omissions       map[string]int64
}

// New creates an assessment collector for one selected source snapshot.
func New(mode, tree string) *Collector {
	return &Collector{mode: mode, tree: tree, status: "complete", invalid: map[string]int64{}, candidateCounts: map[string]*CandidateCount{}, kindCounts: map[string]*CandidateKindCount{}, candidates: map[string]*candidateHeap{}, omissions: map[string]int64{}}
}

// Add adds metadata for one selected regular file. It does not retain every
// path: exact counts are maintained separately from bounded candidate samples.
func (c *Collector) Add(file discovery.File) {
	c.files++
	if file.Size < 0 {
		c.invalid["invalid_file_size"]++
		c.Partial("invalid_file_size")
	} else if file.Size > math.MaxInt64-c.bytes {
		c.invalid["inventory_bytes_overflow"]++
		c.Partial("inventory_bytes_overflow")
	} else {
		c.bytes += file.Size
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
	if declarations.IsManifest(file.Path) {
		filename := manifestFilenameType(file.Path)
		ecosystem := manifestEcosystem(file.Path)
		key := "manifest\x00" + ecosystem + "\x00" + filename
		count := c.candidateCounts[key]
		if count == nil {
			count = &CandidateCount{Filename: filename, Kind: "manifest", Ecosystem: ecosystem}
			c.candidateCounts[key] = count
		}
		count.Files++
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

// Finish combines exact scanner metadata with bounded parsed-project and
// lockfile observations. Inputs must describe the same source snapshot.
func (c *Collector) Finish(decls *declarations.Report, locks *lockfiles.Report, projectRecords ...[]declarations.ProjectRecord) (*Report, error) {
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
	if (len(projectRecords) == 0 || projectRecords[0] == nil || len(projectRecords[0]) == 0) && len(decls.Projects) > 0 {
		return nil, errors.New("assessment requires declaration project records to distinguish parsed projects from configuration and invalid documents")
	}
	if len(projectRecords) > 0 {
		recordProjects := make([]declarations.Project, 0, len(projectRecords[0]))
		for _, record := range projectRecords[0] {
			recordProjects = append(recordProjects, record.Project)
		}
		if err := validateDeclarationPaths(recordProjects); err != nil {
			return nil, err
		}
	}
	graphProjects, parsedProjects, recordsIncomplete := selectProjects(decls.Projects, projectRecords)
	consistency := "live_directory_metadata"
	if c.mode == "git" {
		consistency = "selected_git_tree"
	}
	out := &Report{Version: Version, Source: discovery.Source{Mode: c.mode, Tree: c.tree, Consistency: consistency},
		Inventory:          InventoryMetrics{Files: metric(c.files, "selected regular files", c.status != "complete", mapKeys(c.omissions)), Bytes: metric(c.bytes, "logical bytes in selected regular files", c.status != "complete", mapKeys(c.omissions))},
		ManifestCandidates: []CandidateCount{}, ManifestCandidatePopulation: metric(0, "selected files recognized by declarations.IsManifest, excluding installed node_modules", c.status != "complete", mapKeys(c.omissions)), CandidateEvidence: []discovery.Candidate{}, OmittedCandidateEvidence: map[string]int64{},
		FilenameCandidates:  sortedKindCounts(c.kindCounts),
		ProjectRootEvidence: []string{}, WorkspaceByKind: []RelationshipCount{}, WorkspaceEvidence: []RelationshipEvidence{},
		LocalDependencyByKind: []RelationshipCount{}, LocalDependencyEvidence: []RelationshipEvidence{}, Lockfiles: []LockfileEcosystem{}, Definitions: definitions()}
	out.ManifestCandidates = sortedCandidateCounts(c.candidateCounts)
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
	out.ManifestCandidatePopulation = metric(manifestCount, "selected files recognized by declarations.IsManifest, excluding installed node_modules", c.status != "complete", mapKeys(c.omissions))
	unparsedCandidates := max(int64(0), manifestCount-int64(decls.Coverage.ParsedManifests))
	out.UnparsedManifestCandidates = metric(unparsedCandidates, "recognized manifest candidates not represented by a successfully parsed document; ecosystem ownership is not inferred", c.status != "complete", mapKeys(c.omissions))
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
	out.FilenameCandidatePopulation = metric(filenameCandidateCount, "selected files with discovery filename classification evidence", c.status != "complete", mapKeys(c.omissions))
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
	projectsComplete, projectReasons := projectCoverage(decls, recordsIncomplete)
	rootSet, projectByID := projectRoots(parsedProjects, graphProjects)
	out.ProjectRoots = metric(int64(len(rootSet)), "distinct roots among projects in the declaration report", !projectsComplete, projectReasons)
	out.Projects = metric(int64(len(parsedProjects)), "parsed package, project, or virtual-workspace records; configuration and solution records excluded", !projectsComplete, projectReasons)
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
	relationComplete, relationReasons := relationshipCoverage(decls, projectsComplete, projectReasons)
	out.WorkspaceMembership, out.WorkspaceByKind, out.WorkspaceEvidence, out.OmittedWorkspaceEvidence = relationshipMetric(graphProjects, projectByID, "workspace", relationComplete, relationReasons)
	out.LocalDependencies, out.LocalDependencyByKind, out.LocalDependencyEvidence, out.OmittedLocalEvidence = relationshipMetric(graphProjects, projectByID, "local", relationComplete, relationReasons)
	out.Lockfiles, out.LockfilesOverall = lockfileMetrics(parsedProjects, locks, projectsComplete, projectReasons)
	var unsupportedProjects int64
	for _, row := range out.Lockfiles {
		if row.Ecosystem != "npm" && row.Ecosystem != "nuget" {
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

func projectCoverage(r *declarations.Report, recordsIncomplete bool) (bool, []string) {
	reasons := []string{}
	if r.Status == "skipped" {
		reasons = append(reasons, "declarations_skipped")
	}
	if r.Coverage.OmittedFiles > 0 {
		reasons = append(reasons, "declaration_files_omitted")
	}
	if r.Coverage.OmittedDiagnostics > 0 {
		reasons = append(reasons, "declaration_diagnostics_omitted")
	}
	if r.Coverage.ManifestCandidates > int64(r.Coverage.ParsedManifests) {
		reasons = append(reasons, "manifest_candidates_unparsed")
	}
	if hasDotnetSelectedPathIdentityIssue(r) {
		reasons = append(reasons, "dotnet_selected_path_identity_ambiguous")
	}
	if recordsIncomplete {
		reasons = append(reasons, "parsed_project_observations_incomplete")
	}
	slices.Sort(reasons)
	return len(reasons) == 0, reasons
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

func relationshipCoverage(r *declarations.Report, complete bool, reasons []string) (bool, []string) {
	result := slices.Clone(reasons)
	if len(r.Diagnostics) > 0 {
		result = append(result, "declaration_diagnostics_present")
	}
	if r.Coverage.RetainedObservations >= r.Limits.TotalObservations && r.Limits.TotalObservations > 0 {
		result = append(result, "declaration_observation_limit_reached")
	}
	slices.Sort(result)
	result = slices.Compact(result)
	return complete && len(result) == 0, result
}

func selectProjects(projects []declarations.Project, recordsArg [][]declarations.ProjectRecord) (graphProjects, parsedProjects []declarations.Project, incomplete bool) {
	if len(recordsArg) == 0 {
		// Empty project collections need no eligibility metadata.
		return []declarations.Project{}, []declarations.Project{}, false
	}
	seen := map[string]bool{}
	for _, record := range recordsArg[0] {
		if !record.Parsed {
			continue
		}
		p := record.Project
		if seen[p.ID] {
			incomplete = true
			continue
		}
		seen[p.ID] = true
		graphProjects = append(graphProjects, p)
		if !record.Complete {
			incomplete = true
		}
		if isPackageProject(p) {
			parsedProjects = append(parsedProjects, p)
		}
	}
	return graphProjects, parsedProjects, incomplete
}

func isPackageProject(p declarations.Project) bool {
	kind := strings.ToLower(p.Kind)
	if kind == "solution" || kind == "configuration" || strings.HasSuffix(kind, "-configuration") || kind == "kbuild-kconfig-marker" || kind == "go-workspace" {
		return false
	}
	return true
}

func projectRoots(projects, graphProjects []declarations.Project) (map[string]bool, map[string]string) {
	roots := map[string]bool{}
	byID := map[string]string{}
	for _, p := range projects {
		root := p.Root
		if root == "" {
			continue
		}
		roots[root] = true
	}
	for _, p := range graphProjects {
		if p.ID != "" && p.Root != "" {
			byID[p.ID] = p.Root
		}
	}
	return roots, byID
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

func lockfileMetrics(projects []declarations.Project, locks *lockfiles.Report, projectsComplete bool, reasons []string) ([]LockfileEcosystem, LockfileEcosystem) {
	byEco := map[string]map[string]int64{}
	contextIDs := map[string]map[string]bool{}
	projectIDs := map[string]map[string]bool{}
	// Parsed projects define the full population. Eligibility is a subset for
	// which a supported static association checker exists.
	for _, p := range projects {
		eco := projectEcosystem(p)
		if eco == "" {
			continue
		}
		if byEco[eco] == nil {
			byEco[eco] = map[string]int64{}
		}
		if projectIDs[eco] == nil {
			projectIDs[eco] = map[string]bool{}
		}
		projectIDs[eco][p.ID] = true
		byEco[eco]["projects"]++
		if eco == "npm" || eco == "nuget" {
			byEco[eco]["eligible"]++
		} else {
			byEco[eco]["unsupported"]++
		}
	}
	if locks != nil {
		for _, ctx := range locks.Contexts {
			counts := byEco[ctx.Ecosystem]
			if counts == nil || counts["eligible"] == 0 || !projectIDs[ctx.Ecosystem][ctx.ProjectID] {
				continue
			}
			if contextIDs[ctx.Ecosystem] == nil {
				contextIDs[ctx.Ecosystem] = map[string]bool{}
			}
			if contextIDs[ctx.Ecosystem][ctx.ProjectID] {
				continue
			}
			contextIDs[ctx.Ecosystem][ctx.ProjectID] = true
			state := ctx.AssociationState
			if state != "observed" && state != "missing" && state != "not_applicable" && state != "unsupported" {
				state = "unknown"
			}
			counts[state]++
		}
	}
	// No context, including one omitted under a context limit, means that the
	// association is unknown. Missing is reserved for an explicit missing state.
	for eco, counts := range byEco {
		if eco != "npm" && eco != "nuget" {
			continue
		}
		known := counts["observed"] + counts["missing"] + counts["not_applicable"] + counts["unsupported"] + counts["unknown"]
		if known < counts["eligible"] {
			counts["unknown"] += counts["eligible"] - known
		}
	}
	ecologies := mapKeysNested(byEco)
	out := make([]LockfileEcosystem, 0, len(ecologies))
	for _, eco := range ecologies {
		counts := byEco[eco]
		projectMetric := metric(counts["projects"], "parsed projects in this ecosystem", !projectsComplete, reasons)
		eligibleComplete := projectsComplete
		eligibleReasons := []string{}
		if !projectsComplete {
			eligibleReasons = append(eligibleReasons, reasons...)
		}
		stateMetrics := func(state string) Metric {
			return metric(counts[state], "reported parsed projects in this ecosystem with this association state", !eligibleComplete, eligibleReasons)
		}
		eligibleScope := "parsed npm/NuGet projects supported by the static association checker; descriptive subset, not a policy requirement"
		if eco != "npm" && eco != "nuget" {
			eligibleScope = "not in npm/nuget static association scope"
		}
		eligibleCount := counts["eligible"]
		out = append(out, LockfileEcosystem{Ecosystem: eco, Projects: projectMetric,
			Eligible: metric(eligibleCount, eligibleScope, !eligibleComplete, eligibleReasons),
			Covered:  stateMetrics("observed"), Missing: stateMetrics("missing"), NotApplicable: stateMetrics("not_applicable"), Unsupported: stateMetrics("unsupported"), Unknown: stateMetrics("unknown")})
	}
	all := map[string]int64{}
	for _, groups := range byEco {
		for state, count := range groups {
			all[state] += count
		}
	}
	allComplete := projectsComplete
	allReasons := slices.Clone(reasons)
	slices.Sort(allReasons)
	stateMetric := func(state string) Metric {
		return metric(all[state], "reported parsed projects across ecosystems with this association state", !allComplete, allReasons)
	}
	overall := LockfileEcosystem{Ecosystem: "all", Projects: metric(all["projects"], "parsed projects in ecosystems represented by this report", !projectsComplete, reasons),
		Eligible: metric(all["eligible"], "parsed npm/NuGet projects supported by the static association checker; descriptive subset, not a policy requirement", !allComplete, allReasons),
		Covered:  stateMetric("observed"), Missing: stateMetric("missing"), NotApplicable: stateMetric("not_applicable"), Unsupported: stateMetric("unsupported"), Unknown: stateMetric("unknown")}
	return out, overall
}

func projectEcosystem(p declarations.Project) string {
	switch p.Kind {
	case "npm":
		return "npm"
	case "dotnet":
		if strings.EqualFold(path.Ext(p.ID), ".csproj") {
			return "nuget"
		}
		return "dotnet"
	case "cargo":
		return "cargo"
	case "go":
		return "go"
	case "python", "python-uv":
		return "python"
	case "ruby", "ruby-bundler":
		return "ruby-bundler"
	case "ruby-gem":
		return "ruby-gem"
	case "erlang-rebar":
		return "erlang-rebar"
	case "maven":
		return "maven"
	case "gradle":
		return "gradle"
	case "dart", "dart-pub":
		return "dart-pub"
	case "swift", "swift-package":
		return "swift-package"
	case "elixir", "elixir-mix":
		return "elixir-mix"
	case "sbt", "scala-sbt":
		return "scala-sbt"
	case "composer", "php-composer":
		return "php-composer"
	case "haskell", "haskell-cabal", "haskell-stack":
		if path.Ext(p.ID) == ".cabal" {
			return "haskell-cabal"
		}
		return "haskell-stack"
	default:
		return p.Kind
	}
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

func manifestEcosystem(selectedPath string) string {
	filename := manifestFilenameType(selectedPath)
	lower := strings.ToLower(filename)
	if strings.HasPrefix(lower, "requirements") && strings.HasSuffix(lower, ".txt") || strings.Contains(lower, "requirements/") && strings.HasSuffix(lower, ".txt") {
		return "python"
	}
	switch lower {
	case "package.json":
		return "npm"
	case "go.mod":
		return "go"
	case "go.work":
		return "go-workspace"
	case "cargo.toml":
		return "cargo"
	case "pyproject.toml", "requirements.txt", "setup.py", "setup.cfg", "pipfile":
		return "python"
	case "gemfile":
		return "ruby-bundler"
	case "application.rb":
		return "ruby-rails-app"
	case "composer.json":
		return "php-composer"
	case "packages.config", "packages.lock.json":
		return "nuget"
	case "global.json", "directory.build.props", "directory.build.targets", "directory.packages.props", "nuget.config":
		return "dotnet-configuration"
	case "pubspec.yaml":
		return "dart-pub"
	case "package.swift":
		return "swift-package"
	case "mix.exs":
		return "elixir-mix"
	case "build.sbt":
		return "scala-sbt"
	case "pom.xml":
		return "maven"
	case "build.gradle", "build.gradle.kts":
		return "gradle"
	case "module.bazel":
		return "bazel-module"
	case "workspace":
		return "bazel-workspace"
	case "cmakelists.txt":
		return "cmake"
	case "deno.json", "deno.jsonc":
		return "deno"
	case "stack.yaml":
		return "haskell-stack"
	case "project.toml":
		return "julia-project"
	case "description":
		return "r-package"
	case "deps.edn":
		return "clojure-deps"
	case "project.clj":
		return "clojure-leiningen"
	case "cpanfile":
		return "perl-cpanfile"
	case "makefile.pl":
		return "perl-extutils"
	case "rebar.config":
		return "erlang-rebar"
	case "kbuild", "kconfig":
		return "kbuild-kconfig-marker"
	case "configure.ac":
		return "autoconf"
	case "meson.build":
		return "meson"
	case "build.zig":
		return "zig-build"
	default:
		ext := strings.ToLower(path.Ext(filename))
		switch ext {
		case ".csproj", ".fsproj", ".vbproj", "*.csproj", "*.fsproj", "*.vbproj":
			return "nuget"
		case ".vcxproj", ".sqlproj", ".wixproj", ".shproj", ".proj", "*.vcxproj", "*.sqlproj", "*.wixproj", "*.shproj", "*.proj":
			return "dotnet"
		case ".sln", ".slnx", ".slnf", "*.sln", "*.slnx", "*.slnf":
			return "dotnet-solution"
		case ".cabal", "*.cabal":
			return "haskell-cabal"
		case ".gemspec", "*.gemspec":
			return "ruby-gem"
		case ".c":
			return "cmake"
		}
		return "other"
	}
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
func mapKeysNested(m map[string]map[string]int64) []string {
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
		{ID: "manifest_candidate_population", Description: "Manifest candidate counts use the declarations parser's filename selection, including its installed node_modules exclusion; their bounded evidence sample does not reduce aggregate counts."},
		{ID: "unparsed_manifest_candidates", Description: "This counts filename-selected manifest candidates without a successfully parsed document; it does not assign failed manifests to a package ecosystem."},
		{ID: "filename_candidate_population", Description: "Filename candidate counts are exact across selected files; bounded candidate evidence does not reduce these totals."},
		{ID: "project_roots", Description: "Distinct roots are counted only from parsed declaration projects; declaration parser caps can make this a lower bound."},
		{ID: "workspace_membership", Description: "Only explicit supported workspace or module references are counted; Cargo default-member selection remains a distinct declaration and is not counted as general workspace membership."},
		{ID: "local_dependencies", Description: "Only explicit supported local dependency or project references with a confined target are counted."},
		{ID: "lockfile_association", Description: "Covered means a static project-to-lockfile association was observed; it does not establish consistency or a successful locked restore."},
		{ID: "lockfile_eligible", Description: "Eligible means a parsed npm or NuGet project was in the static association scope; eligibility is descriptive and does not express a policy requirement."},
		{ID: "lockfile_states", Description: "Covered, missing, not_applicable, unsupported, and unknown partition the reported project population; Eligible identifies the npm/NuGet subset, while unsupported ecosystems and checker-ineligible project forms have an unsupported state."},
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
	if r.Version != Version {
		return fmt.Errorf("unsupported assessment version %q", r.Version)
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
	metrics := []Metric{r.Inventory.Files, r.Inventory.Bytes, r.ManifestCandidatePopulation, r.UnparsedManifestCandidates, r.FilenameCandidatePopulation, r.ProjectRoots, r.Projects, r.WorkspaceMembership, r.LocalDependencies, r.UnsupportedEcosystemProjects,
		r.LockfilesOverall.Projects, r.LockfilesOverall.Eligible, r.LockfilesOverall.Covered, r.LockfilesOverall.Missing, r.LockfilesOverall.NotApplicable, r.LockfilesOverall.Unsupported, r.LockfilesOverall.Unknown}
	for i := range r.Lockfiles {
		row := r.Lockfiles[i]
		metrics = append(metrics, row.Projects, row.Eligible, row.Covered, row.Missing, row.NotApplicable, row.Unsupported, row.Unknown)
	}
	for _, m := range metrics {
		if err := validateMetric(m); err != nil {
			return err
		}
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
		if c.Filename == "" || c.Kind != "manifest" || c.Files < 0 || c.Bytes < 0 || c.Ecosystem == "" {
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
	if err := validateRelationships(r.WorkspaceMembership, r.WorkspaceByKind, r.WorkspaceEvidence, r.OmittedWorkspaceEvidence); err != nil {
		return fmt.Errorf("workspace membership: %w", err)
	}
	if err := validateRelationships(r.LocalDependencies, r.LocalDependencyByKind, r.LocalDependencyEvidence, r.OmittedLocalEvidence); err != nil {
		return fmt.Errorf("local dependencies: %w", err)
	}
	if err := validateLockfileRows(r); err != nil {
		return err
	}
	encoded, err := json.Marshal(r)
	if err != nil || len(encoded) > MaxAssessmentJSONBytes {
		return errors.New("assessment JSON exceeds its serialized size limit")
	}
	return nil
}

func validateMetric(m Metric) error {
	if m.Count < 0 || strings.TrimSpace(m.Scope) == "" {
		return errors.New("metric has a negative count or empty scope")
	}
	if m.Completeness != "complete" && m.Completeness != "lower_bound" {
		return errors.New("metric completeness must be complete or lower_bound")
	}
	if m.Completeness == "lower_bound" && len(m.Reasons) == 0 {
		return errors.New("lower-bound metric has no reason")
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
	var projectTotal, eligibleTotal, coveredTotal, missingTotal, naTotal, unsupportedTotal, unknownTotal, unsupportedEcosystemTotal int64
	if len(r.Lockfiles) > 128 {
		return errors.New("lockfile ecosystem rows exceed schema limit")
	}
	for i, row := range r.Lockfiles {
		if row.Ecosystem == "" || row.Ecosystem == "all" || i > 0 && r.Lockfiles[i-1].Ecosystem >= row.Ecosystem {
			return errors.New("lockfile ecosystem rows are invalid or unsorted")
		}
		var ok bool
		for target, value := range map[*int64]int64{&projectTotal: row.Projects.Count, &eligibleTotal: row.Eligible.Count, &coveredTotal: row.Covered.Count, &missingTotal: row.Missing.Count, &naTotal: row.NotApplicable.Count, &unsupportedTotal: row.Unsupported.Count, &unknownTotal: row.Unknown.Count} {
			*target, ok = checkedAdd(*target, value)
			if !ok {
				return errors.New("lockfile totals overflow")
			}
		}
		if row.Ecosystem != "npm" && row.Ecosystem != "nuget" {
			unsupportedEcosystemTotal, ok = checkedAdd(unsupportedEcosystemTotal, row.Projects.Count)
			if !ok {
				return errors.New("unsupported ecosystem project totals overflow")
			}
		}
		states, ok := checkedSum(row.Covered.Count, row.Missing.Count, row.NotApplicable.Count, row.Unsupported.Count, row.Unknown.Count)
		if !ok || states != row.Projects.Count {
			return fmt.Errorf("lockfile states for %q do not partition project totals", row.Ecosystem)
		}
		if row.Eligible.Count > row.Projects.Count {
			return errors.New("lockfile eligibility exceeds the project population")
		}
		if (row.Ecosystem == "npm" || row.Ecosystem == "nuget") && row.Eligible.Count != row.Projects.Count {
			return errors.New("supported ecosystem project total is inconsistent")
		}
		if row.Ecosystem != "npm" && row.Ecosystem != "nuget" && row.Eligible.Count != 0 {
			return errors.New("unsupported ecosystem was counted as lockfile eligible")
		}
		if row.Ecosystem != "npm" && row.Ecosystem != "nuget" && row.Unsupported.Count != row.Projects.Count {
			return errors.New("unsupported ecosystem projects do not have unsupported outcomes")
		}
	}
	o := r.LockfilesOverall
	if o.Ecosystem != "all" || o.Projects.Count != projectTotal || o.Eligible.Count != eligibleTotal || o.Covered.Count != coveredTotal || o.Missing.Count != missingTotal || o.NotApplicable.Count != naTotal || o.Unsupported.Count != unsupportedTotal || o.Unknown.Count != unknownTotal {
		return errors.New("overall lockfile totals do not reconcile with ecosystem rows")
	}
	states, ok := checkedSum(o.Covered.Count, o.Missing.Count, o.NotApplicable.Count, o.Unsupported.Count, o.Unknown.Count)
	if !ok || states != o.Projects.Count || o.Eligible.Count > o.Projects.Count {
		return errors.New("overall lockfile states do not partition project totals")
	}
	if o.Projects.Count != r.Projects.Count {
		return errors.New("lockfile overall projects do not match assessment projects")
	}
	if r.UnsupportedEcosystemProjects.Count != unsupportedEcosystemTotal {
		return errors.New("unsupported ecosystem metric does not reconcile with ecosystem rows")
	}
	return nil
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
	if m.Completeness != "lower_bound" {
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
