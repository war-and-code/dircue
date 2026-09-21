package focus

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"slices"
	"strings"
	"unicode/utf8"

	"dircue/pkg/declarations"
)

const ownershipRule = "nearest-project-manifest-directory-v1"

type candidate struct {
	id       string
	root     string
	kind     string
	parsed   bool
	eligible bool
	record   *ProjectRecord
}

// Build creates a focus plan from already selected and parsed evidence.
func Build(ctx context.Context, in Input, request Request, limits Limits) (*Result, error) {
	limits, err := normalizeLimits(limits)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(in.Inventory) > DefaultMaxInventoryPaths {
		return nil, fmt.Errorf("focus inventory exceeds hard limit %d", DefaultMaxInventoryPaths)
	}
	if len(in.OwnershipBarriers) > DefaultMaxInventoryPaths {
		return nil, fmt.Errorf("focus ownership barriers exceed hard limit %d", DefaultMaxInventoryPaths)
	}
	if len(in.Projects) > DefaultMaxProjectRecords {
		return nil, fmt.Errorf("focus project records exceed hard limit %d", DefaultMaxProjectRecords)
	}
	if in.Source != "directory" && in.Source != "git" {
		return nil, errors.New("focus source must be directory or git")
	}
	if in.Source == "git" && in.Tree == "" {
		return nil, errors.New("git focus source requires a selected tree")
	}
	projectMode := request.Project != ""
	affectedMode := request.AffectedBy != ""
	if projectMode == affectedMode {
		return nil, errors.New("focus requires exactly one of project or affected-by")
	}
	if affectedMode && len(request.Related) > 0 {
		return nil, errors.New("affected-by focus cannot select related projects")
	}
	if affectedMode && !validPath(request.AffectedBy) {
		return nil, errors.New("affected-by path must be a clean root-relative path")
	}

	r := &Report{Provider: Provider, ProviderVersion: ProviderVersion, Status: "complete", Source: in.Source, Tree: in.Tree, Limits: limits, Primary: []PopulationFile{}, Related: []RelatedPopulation{}, Context: []Context{}, Relations: []Relation{}, Boundaries: []Boundary{}, Omissions: []Omission{}}
	result := &Result{Report: r, PrimaryPaths: map[string]struct{}{}, RelatedPaths: map[string]map[string]struct{}{}, SelectionComplete: true, contextsByProject: map[string][]Context{}, projectsByContext: map[string][]ProjectImpact{}}

	files, omittedInventory := boundedInventory(in.Inventory, limits.InventoryPaths)
	barriers, omittedBarriers := boundedBarriers(in.OwnershipBarriers, limits.InventoryPaths)
	r.Coverage.OwnershipBarriers = len(barriers)
	r.Coverage.InventoryFiles = len(files)
	r.Coverage.OmittedFiles = in.OmittedFiles + int64(omittedInventory+omittedBarriers)
	if !in.InventoryComplete || r.Coverage.OmittedFiles > 0 {
		result.SelectionComplete = false
		partial(r)
		count := r.Coverage.OmittedFiles
		if !in.InventoryComplete && count == 0 {
			count = 1
		}
		r.Omissions = append(r.Omissions, Omission{Reason: "incomplete-inventory", Count: count})
	}
	r.Coverage.OmittedProjects = in.OmittedProjects
	if !in.DeclarationsComplete || in.OmittedProjects > 0 {
		result.SelectionComplete = false
		partial(r)
		count := in.OmittedProjects
		if !in.DeclarationsComplete && count == 0 {
			count = 1
		}
		r.Omissions = append(r.Omissions, Omission{Reason: "incomplete-declarations", Count: count})
	}
	for _, f := range files {
		r.Coverage.InventoryBytes += f.Size
	}

	records := append([]ProjectRecord(nil), in.Projects...)
	slices.SortFunc(records, compareRecord)
	if len(records) > limits.ProjectRecords {
		result.SelectionComplete = false
		r.Coverage.OmittedRecords += len(records) - limits.ProjectRecords
		records = records[:limits.ProjectRecords]
		partial(r)
		r.Omissions = append(r.Omissions, Omission{Reason: "project-record-limit", Count: int64(r.Coverage.OmittedRecords)})
	}
	r.Coverage.ProjectRecords = len(records)
	for _, record := range records {
		if record.Parsed {
			r.Coverage.ParsedProjectRecords++
		}
	}

	byID := map[string][]*ProjectRecord{}
	for i := range records {
		record := &records[i]
		if validPath(record.Project.ID) {
			byID[record.Project.ID] = append(byID[record.Project.ID], record)
		}
	}
	evidence, evidenceComplete, err := buildEvidenceIndex(ctx, files, records, &r.Coverage, limits.Work)
	if err != nil {
		return nil, err
	}
	if !evidenceComplete {
		result.SelectionComplete = false
		markWorkLimit(r)
	}
	if affectedMode {
		r.Scope = Scope{Algorithm: "sha256", Rule: "direct-context-impact-v1", Role: "affected-by", AffectedBy: request.AffectedBy, RelatedProjects: []string{}}
		r.Scope.ID = scopeID(in.Source, in.Tree, r.Scope)
		for i := range records {
			if !supported(&records[i]) {
				continue
			}
			if err := addProjectEvidence(ctx, result, &records[i], evidence, limits); err != nil {
				return nil, err
			}
		}
		if err := indexQueries(ctx, result, limits); err != nil {
			return nil, err
		}
		query, err := result.AffectedProjects(ctx, request.AffectedBy, QueryOptions{Limit: limits.Relations})
		if err != nil {
			return nil, err
		}
		r.AffectedProjects = &query
		sortReport(r)
		if err := boundOutput(ctx, r); err != nil {
			return nil, err
		}
		return result, nil
	}

	primaryRecord, err := selectedRecord(request.Project, byID)
	if err != nil {
		return nil, err
	}
	if !supported(primaryRecord) {
		return nil, fmt.Errorf("project %q is not an eligible parsed .NET or Python/uv project", request.Project)
	}
	if slices.IndexFunc(files, func(f File) bool { return f.Path == request.Project }) < 0 {
		return nil, fmt.Errorf("selected project %q is outside the retained inventory", request.Project)
	}
	relatedIDs := append([]string{}, request.Related...)
	slices.Sort(relatedIDs)
	relatedIDs = slices.Compact(relatedIDs)
	if slices.Contains(relatedIDs, request.Project) {
		return nil, errors.New("primary project must not also be requested as related")
	}
	selected := map[string]*ProjectRecord{request.Project: primaryRecord}
	for _, id := range relatedIDs {
		record, err := selectedRecord(id, byID)
		if err != nil {
			return nil, fmt.Errorf("related %w", err)
		}
		if !supported(record) {
			return nil, fmt.Errorf("related project %q is not an eligible parsed .NET or Python/uv project", id)
		}
		selected[id] = record
	}

	primaryView := projectView(primaryRecord)
	r.PrimaryProject = &primaryView
	r.Scope = Scope{Algorithm: "sha256", Rule: ownershipRule, Role: "project", PrimaryProject: request.Project, RelatedProjects: relatedIDs}
	r.Scope.ID = scopeID(in.Source, in.Tree, r.Scope)
	for _, id := range relatedIDs {
		r.Related = append(r.Related, RelatedPopulation{Project: projectView(selected[id]), Files: []PopulationFile{}})
		result.RelatedPaths[id] = map[string]struct{}{}
	}

	roots, ownershipIndexComplete, err := candidateRoots(ctx, files, barriers, byID, &r.Coverage, limits.Work)
	if err != nil {
		return nil, err
	}
	cache := map[string][]candidate{}
	workExhausted := !ownershipIndexComplete
	for _, file := range files {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if !ownershipIndexComplete {
			r.Coverage.UnresolvedFiles++
			continue
		}
		owners, exhausted := nearestCandidates(ctx, path.Dir(file.Path), roots, cache, &r.Coverage.Work, limits.Work)
		if exhausted && ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if exhausted {
			workExhausted = true
			r.Coverage.UnresolvedFiles++
			continue
		}
		if len(owners) == 0 {
			continue
		}
		ids := candidateIDs(owners)
		if len(owners) != 1 || !owners[0].eligible {
			reason := "ambiguous-project-ownership"
			if len(owners) == 1 {
				reason = "ineligible-project-boundary"
				r.Coverage.UnresolvedFiles++
			} else {
				r.Coverage.AmbiguousFiles++
			}
			if containsSelected(ids, selected) || withinSelectedRoot(file.Path, selected) {
				result.SelectionComplete = false
				r.Boundaries = appendBoundedBoundary(r.Boundaries, Boundary{Path: file.Path, Reason: reason, Candidates: ids}, limits.Relations, &r.Coverage.OmittedRelations)
				partial(r)
			}
			continue
		}
		owner := owners[0]
		if _, ok := selected[owner.id]; !ok {
			continue
		}
		population := PopulationFile{Path: file.Path, Bytes: file.Size, ProjectID: owner.id, Ownership: "qualified", Basis: ownershipRule}
		if owner.id == request.Project {
			r.Primary = append(r.Primary, population)
			result.PrimaryPaths[file.Path] = struct{}{}
			r.Coverage.PrimaryFiles++
			r.Coverage.PrimaryBytes += file.Size
		} else {
			index := slices.IndexFunc(r.Related, func(v RelatedPopulation) bool { return v.Project.ID == owner.id })
			r.Related[index].Files = append(r.Related[index].Files, population)
			result.RelatedPaths[owner.id][file.Path] = struct{}{}
			r.Coverage.RelatedFiles++
			r.Coverage.RelatedBytes += file.Size
		}
	}
	if workExhausted {
		result.SelectionComplete = false
		markWorkLimit(r)
	}

	for _, id := range append([]string{request.Project}, relatedIDs...) {
		if err := addProjectEvidence(ctx, result, selected[id], evidence, limits); err != nil {
			return nil, err
		}
	}
	if err := indexQueries(ctx, result, limits); err != nil {
		return nil, err
	}
	sortReport(r)
	if err := boundOutput(ctx, r); err != nil {
		return nil, err
	}
	return result, nil
}

func normalizeLimits(v Limits) (Limits, error) {
	if v.InventoryPaths == 0 {
		v.InventoryPaths = DefaultMaxInventoryPaths
	}
	if v.ProjectRecords == 0 {
		v.ProjectRecords = DefaultMaxProjectRecords
	}
	if v.Relations == 0 {
		v.Relations = DefaultMaxRelations
	}
	if v.Work == 0 {
		v.Work = DefaultMaxWork
	}
	if v.OutputBytes == 0 {
		v.OutputBytes = DefaultMaxOutputBytes
	}
	if v.InventoryPaths < 1 || v.InventoryPaths > DefaultMaxInventoryPaths || v.ProjectRecords < 1 || v.ProjectRecords > DefaultMaxProjectRecords || v.Relations < 1 || v.Relations > DefaultMaxRelations || v.Work < 1 || v.Work > DefaultMaxWork || v.OutputBytes < 4096 || v.OutputBytes > DefaultMaxOutputBytes {
		return v, errors.New("focus limits are outside supported bounds")
	}
	return v, nil
}

func boundedInventory(values []File, limit int) ([]File, int) {
	out := make([]File, 0, min(len(values), limit))
	for _, f := range values {
		if f.Size < 0 || !validPath(f.Path) {
			continue
		}
		out = append(out, f)
	}
	slices.SortFunc(out, func(a, b File) int { return strings.Compare(a.Path, b.Path) })
	out = slices.CompactFunc(out, func(a, b File) bool { return a.Path == b.Path })
	omitted := len(values) - len(out)
	if len(out) > limit {
		omitted += len(out) - limit
		out = out[:limit]
	}
	return out, omitted
}

func validPath(v string) bool {
	return v != "" && len(v) <= 8192 && utf8.ValidString(v) && v == path.Clean(v) && v != "." && v != ".." && !strings.HasPrefix(v, "../") && !strings.HasPrefix(v, "/") && !strings.ContainsAny(v, "\\\x00\r\n")
}

func compareRecord(a, b ProjectRecord) int {
	if n := strings.Compare(a.Project.ID, b.Project.ID); n != 0 {
		return n
	}
	if n := strings.Compare(a.Project.Kind, b.Project.Kind); n != 0 {
		return n
	}
	if a.Parsed == b.Parsed {
		return 0
	}
	if a.Parsed {
		return -1
	}
	return 1
}

func selectedRecord(id string, byID map[string][]*ProjectRecord) (*ProjectRecord, error) {
	values := byID[id]
	if len(values) == 0 {
		return nil, fmt.Errorf("project %q was not retained from the selected declarations", id)
	}
	if len(values) != 1 {
		return nil, fmt.Errorf("project %q is ambiguous across %d declaration records", id, len(values))
	}
	return values[0], nil
}

func supported(r *ProjectRecord) bool {
	if r == nil || !r.Parsed {
		return false
	}
	switch r.Project.Kind {
	case "dotnet":
		return dotnetProjectCandidate(r.Project.ID)
	case "python":
		return path.Base(r.Project.ID) == "pyproject.toml" && r.Project.Name != ""
	case "python-workspace":
		return path.Base(r.Project.ID) == "pyproject.toml"
	default:
		return false
	}
}

func projectView(r *ProjectRecord) Project {
	return Project{ID: r.Project.ID, Root: r.Project.Root, Kind: r.Project.Kind, Parsed: r.Parsed}
}

func scopeID(source, tree string, scope Scope) string {
	data, _ := json.Marshal(struct {
		Version, Source, Tree, Rule, Role, Primary, AffectedBy string
		Related                                                []string
	}{ProviderVersion, source, tree, scope.Rule, scope.Role, scope.PrimaryProject, scope.AffectedBy, scope.RelatedProjects})
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func dotnetProjectCandidate(name string) bool {
	switch strings.ToLower(path.Ext(name)) {
	case ".csproj", ".fsproj", ".vbproj":
		return true
	}
	return false
}

func projectCandidate(name string) bool {
	base := path.Base(name)
	if dotnetProjectCandidate(name) {
		return true
	}
	switch base {
	case "package.json", "pyproject.toml", "go.mod", "Cargo.toml", "pom.xml", "build.gradle", "build.gradle.kts":
		return true
	}
	return false
}

// IsProjectManifestCandidate reports filenames that establish a conservative
// ownership boundary even when their contents are unavailable or unsupported.
func IsProjectManifestCandidate(name string) bool {
	return validPath(name) && projectCandidate(name)
}

func configurationCandidate(name string) bool {
	switch strings.ToLower(path.Base(name)) {
	case "global.json", "directory.build.props", "directory.build.targets", "directory.packages.props", "nuget.config", "packages.config":
		return true
	}
	return false
}

func candidateRoots(ctx context.Context, files []File, barriers []string, recordByID map[string][]*ProjectRecord, coverage *Coverage, limit int) (map[string][]candidate, bool, error) {
	roots := map[string][]candidate{}
	seen := map[string]bool{}
	for _, f := range files {
		ok, err := spend(ctx, coverage, limit)
		if err != nil {
			return nil, false, err
		}
		if !ok {
			return roots, false, nil
		}
		if !projectCandidate(f.Path) {
			continue
		}
		key := f.Path + "\x00inventory"
		if seen[key] {
			continue
		}
		seen[key] = true
		matches := recordByID[f.Path]
		if len(matches) == 0 {
			roots[path.Dir(f.Path)] = append(roots[path.Dir(f.Path)], candidate{id: f.Path, root: path.Dir(f.Path), kind: "unparsed-candidate"})
			continue
		}
		for _, record := range matches {
			roots[path.Dir(f.Path)] = append(roots[path.Dir(f.Path)], candidate{id: record.Project.ID, root: path.Dir(f.Path), kind: record.Project.Kind, parsed: record.Parsed, eligible: supported(record), record: record})
		}
	}
	for _, name := range barriers {
		ok, err := spend(ctx, coverage, limit)
		if err != nil {
			return nil, false, err
		}
		if !ok {
			return roots, false, nil
		}
		roots[path.Dir(name)] = append(roots[path.Dir(name)], candidate{id: name, root: path.Dir(name), kind: "nonregular-candidate"})
	}
	for root := range roots {
		slices.SortFunc(roots[root], func(a, b candidate) int {
			if n := strings.Compare(a.id, b.id); n != 0 {
				return n
			}
			if n := strings.Compare(a.kind, b.kind); n != 0 {
				return n
			}
			if a.parsed == b.parsed {
				return 0
			}
			if a.parsed {
				return -1
			}
			return 1
		})
	}
	return roots, true, nil
}

func boundedBarriers(values []string, limit int) ([]string, int) {
	out := make([]string, 0, min(len(values), limit))
	for _, name := range values {
		if IsProjectManifestCandidate(name) {
			out = append(out, name)
		}
	}
	slices.Sort(out)
	out = slices.Compact(out)
	omitted := len(values) - len(out)
	if len(out) > limit {
		omitted += len(out) - limit
		out = out[:limit]
	}
	return out, omitted
}

func nearestCandidates(ctx context.Context, dir string, roots map[string][]candidate, cache map[string][]candidate, work *int, limit int) ([]candidate, bool) {
	start := dir
	visited := []string{}
	for {
		if values, ok := cache[dir]; ok {
			for _, v := range visited {
				cache[v] = values
			}
			return values, false
		}
		if ctx.Err() != nil || *work >= limit {
			return nil, true
		}
		*work++
		visited = append(visited, dir)
		if values := roots[dir]; len(values) > 0 {
			for _, v := range visited {
				cache[v] = values
			}
			return values, false
		}
		if dir == "." || path.Dir(dir) == dir {
			break
		}
		dir = path.Dir(dir)
	}
	for _, v := range visited {
		cache[v] = nil
	}
	cache[start] = nil
	return nil, false
}

func candidateIDs(values []candidate) []string {
	out := make([]string, len(values))
	for i := range values {
		out[i] = values[i].id
	}
	return out
}
func containsSelected(ids []string, selected map[string]*ProjectRecord) bool {
	for _, id := range ids {
		if selected[id] != nil {
			return true
		}
	}
	return false
}

func withinSelectedRoot(filename string, selected map[string]*ProjectRecord) bool {
	for _, record := range selected {
		root := record.Project.Root
		if root == "." || filename == root || strings.HasPrefix(filename, root+"/") {
			return true
		}
	}
	return false
}

func appendBoundedBoundary(values []Boundary, v Boundary, limit int, omitted *int) []Boundary {
	if len(values) >= limit {
		*omitted++
		return values
	}
	return append(values, v)
}

func partial(r *Report) {
	if r.Status != "skipped" {
		r.Status = "partial"
	}
}

type evidenceIndex struct {
	parsed           map[string]bool
	configurations   map[string][]Context
	workspaceParents map[string][]workspaceParent
}

type workspaceParent struct {
	project   *ProjectRecord
	reference declarations.Reference
}

func buildEvidenceIndex(ctx context.Context, files []File, records []ProjectRecord, coverage *Coverage, limit int) (evidenceIndex, bool, error) {
	index := evidenceIndex{parsed: map[string]bool{}, configurations: map[string][]Context{}, workspaceParents: map[string][]workspaceParent{}}
	for i := range records {
		ok, err := spend(ctx, coverage, limit)
		if err != nil {
			return index, false, err
		}
		if !ok {
			return index, false, nil
		}
		if records[i].Parsed {
			index.parsed[records[i].Project.ID] = true
		}
		if records[i].Parsed && records[i].Project.Kind == "python-workspace" {
			for _, ref := range records[i].Project.References {
				ok, err := spend(ctx, coverage, limit)
				if err != nil {
					return index, false, err
				}
				if !ok {
					return index, false, nil
				}
				if ref.Kind == "uv-workspace-member" && ref.Target != "" {
					index.workspaceParents[ref.Target] = append(index.workspaceParents[ref.Target], workspaceParent{project: &records[i], reference: ref})
				}
			}
		}
	}
	for _, file := range files {
		ok, err := spend(ctx, coverage, limit)
		if err != nil {
			return index, false, err
		}
		if !ok {
			return index, false, nil
		}
		if !configurationCandidate(file.Path) {
			continue
		}
		parsed := index.parsed[file.Path]
		state := "unparsed"
		if parsed {
			state = "parsed"
		}
		index.configurations[path.Dir(file.Path)] = append(index.configurations[path.Dir(file.Path)], Context{Path: file.Path, Kind: "dotnet-configuration", Basis: "supported-ancestor-configuration-name", Applicability: "candidate", Evidence: file.Path, State: state, Parsed: parsed})
	}
	for dir := range index.configurations {
		slices.SortFunc(index.configurations[dir], compareContext)
	}
	return index, true, nil
}

func spend(ctx context.Context, coverage *Coverage, limit int) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if coverage.Work >= limit {
		return false, nil
	}
	coverage.Work++
	return true, nil
}

func markWorkLimit(r *Report) {
	partial(r)
	for _, omission := range r.Omissions {
		if omission.Reason == "work-limit" {
			return
		}
	}
	r.Omissions = append(r.Omissions, Omission{Reason: "work-limit", Count: 1})
}

func addProjectEvidence(ctx context.Context, result *Result, selected *ProjectRecord, evidence evidenceIndex, limits Limits) error {
	r := result.Report
	id := selected.Project.ID
	addContext := func(v Context) {
		if len(r.Context) >= limits.Relations {
			r.Coverage.OmittedRelations++
			partial(r)
			return
		}
		r.Context = append(r.Context, v)
		result.contextsByProject[id] = append(result.contextsByProject[id], v)
	}
	addContext(Context{ProjectID: id, Path: id, Kind: "project-manifest", Basis: "selected-project", Applicability: "selected", Evidence: id, State: "declared", Parsed: true})
	if selected.Project.Kind == "dotnet" {
		for dir := selected.Project.Root; ; dir = path.Dir(dir) {
			ok, err := spend(ctx, &r.Coverage, limits.Work)
			if err != nil {
				return err
			}
			if !ok {
				markWorkLimit(r)
				break
			}
			for _, candidate := range evidence.configurations[dir] {
				ok, err := spend(ctx, &r.Coverage, limits.Work)
				if err != nil {
					return err
				}
				if !ok {
					markWorkLimit(r)
					r.Coverage.ContextInputs = len(r.Context)
					return nil
				}
				if strings.EqualFold(path.Base(candidate.Path), "packages.config") && dir != selected.Project.Root {
					continue
				}
				candidate.ProjectID = id
				if strings.EqualFold(path.Base(candidate.Path), "packages.config") {
					candidate.Basis = "supported-project-local-configuration-name"
				}
				addContext(candidate)
			}
			if dir == "." || path.Dir(dir) == dir {
				break
			}
		}
	}
	if selected.Project.Kind == "python" {
		for _, parent := range evidence.workspaceParents[id] {
			ok, err := spend(ctx, &r.Coverage, limits.Work)
			if err != nil {
				return err
			}
			if !ok {
				markWorkLimit(r)
				break
			}
			ref := parent.reference
			if r.Coverage.Relations >= limits.Relations {
				r.Coverage.OmittedRelations++
				partial(r)
				continue
			}
			r.Relations = append(r.Relations, Relation{SourceProject: parent.project.Project.ID, Target: ref.Target, Kind: ref.Kind, Class: "declaration", Value: ref.Value, State: ref.State, TargetStatus: ref.TargetStatus, Evidence: ref.Evidence, Condition: ref.Condition})
			r.Coverage.Relations++
			applicability := "declared"
			if ref.Condition != "" || ref.State == "conditional" {
				applicability = "conditional"
			}
			addContext(Context{ProjectID: id, Path: parent.project.Project.ID, Kind: "workspace-declaration", Basis: "uv-workspace-member", Applicability: applicability, Evidence: ref.Evidence, State: ref.TargetStatus, Condition: ref.Condition, Parsed: parent.project.Parsed})
		}
	}
	for _, ref := range selected.Project.References {
		ok, err := spend(ctx, &r.Coverage, limits.Work)
		if err != nil {
			return err
		}
		if !ok {
			markWorkLimit(r)
			break
		}
		if r.Coverage.Relations >= limits.Relations {
			r.Coverage.OmittedRelations++
			partial(r)
			continue
		}
		class, include := relationClass(selected.Project.Kind, ref.Kind)
		if !include {
			continue
		}
		relation := Relation{SourceProject: id, Target: ref.Target, Kind: ref.Kind, Class: class, Value: ref.Value, State: ref.State, TargetStatus: ref.TargetStatus, Evidence: ref.Evidence, Condition: ref.Condition}
		r.Relations = append(r.Relations, relation)
		r.Coverage.Relations++
		if ref.Target == "" {
			continue
		}
		kind, applicability := "declared-project", "declared"
		if class == "configuration" {
			kind, applicability = "declared-configuration", "declared"
		}
		if ref.Condition != "" || ref.State == "conditional" {
			applicability = "conditional"
		}
		parsed := evidence.parsed[ref.Target]
		addContext(Context{ProjectID: id, Path: ref.Target, Kind: kind, Basis: ref.Kind, Applicability: applicability, Evidence: ref.Evidence, State: ref.TargetStatus, Condition: ref.Condition, Parsed: parsed})
	}
	r.Coverage.ContextInputs = len(r.Context)
	return nil
}

func ancestor(parent, child string) bool {
	return parent == "." || child == parent || strings.HasPrefix(child, parent+"/")
}

func relationClass(projectKind, kind string) (string, bool) {
	switch kind {
	case "import":
		return "configuration", projectKind == "dotnet"
	case "project-reference":
		return "declaration", projectKind == "dotnet"
	case "uv-lockfile":
		return "configuration", projectKind == "python" || projectKind == "python-workspace"
	case "uv-workspace-member", "uv-local-dependency":
		return "declaration", projectKind == "python" || projectKind == "python-workspace"
	default:
		if strings.HasPrefix(kind, "uv-source-") {
			return "declaration", projectKind == "python" || projectKind == "python-workspace"
		}
		return "", false
	}
}

func indexQueries(ctx context.Context, result *Result, limits Limits) error {
	for _, c := range result.Report.Context {
		ok, err := spend(ctx, &result.Report.Coverage, limits.Work)
		if err != nil {
			return err
		}
		if !ok {
			markWorkLimit(result.Report)
			break
		}
		impact := ProjectImpact{ProjectID: c.ProjectID, Basis: c.Basis, Applicability: c.Applicability, Evidence: c.Evidence}
		result.projectsByContext[c.Path] = append(result.projectsByContext[c.Path], impact)
	}
	for project := range result.contextsByProject {
		if err := ctx.Err(); err != nil {
			return err
		}
		slices.SortFunc(result.contextsByProject[project], compareContext)
	}
	for p := range result.projectsByContext {
		if err := ctx.Err(); err != nil {
			return err
		}
		slices.SortFunc(result.projectsByContext[p], func(a, b ProjectImpact) int {
			if n := strings.Compare(a.ProjectID, b.ProjectID); n != 0 {
				return n
			}
			if n := strings.Compare(a.Basis, b.Basis); n != 0 {
				return n
			}
			return strings.Compare(a.Evidence, b.Evidence)
		})
		result.projectsByContext[p] = slices.Compact(result.projectsByContext[p])
	}
	return nil
}

func compareContext(a, b Context) int {
	if n := strings.Compare(a.ProjectID, b.ProjectID); n != 0 {
		return n
	}
	if n := strings.Compare(a.Path, b.Path); n != 0 {
		return n
	}
	if n := strings.Compare(a.Kind, b.Kind); n != 0 {
		return n
	}
	return strings.Compare(a.Evidence, b.Evidence)
}

func sortReport(r *Report) {
	slices.SortFunc(r.Primary, func(a, b PopulationFile) int { return strings.Compare(a.Path, b.Path) })
	for i := range r.Related {
		slices.SortFunc(r.Related[i].Files, func(a, b PopulationFile) int { return strings.Compare(a.Path, b.Path) })
	}
	slices.SortFunc(r.Context, compareContext)
	slices.SortFunc(r.Relations, func(a, b Relation) int {
		av, _ := json.Marshal(a)
		bv, _ := json.Marshal(b)
		return strings.Compare(string(av), string(bv))
	})
	slices.SortFunc(r.Boundaries, func(a, b Boundary) int {
		if n := strings.Compare(a.Path, b.Path); n != 0 {
			return n
		}
		return strings.Compare(a.Reason, b.Reason)
	})
	slices.SortFunc(r.Omissions, func(a, b Omission) int { return strings.Compare(a.Reason, b.Reason) })
}

func boundOutput(ctx context.Context, r *Report) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		data, _ := json.Marshal(r)
		if len(data) <= r.Limits.OutputBytes {
			return nil
		}
		partial(r)
		kind, index, size := largestOutputCollection(r)
		if size == 0 {
			return errors.New("focus report envelope exceeds output limit")
		}
		remove := max(1, size/2)
		r.Coverage.OmittedOutputRecords += remove
		switch kind {
		case "boundaries":
			r.Boundaries = r.Boundaries[:size-remove]
		case "context":
			r.Context = r.Context[:size-remove]
		case "relations":
			r.Relations = r.Relations[:size-remove]
		case "primary":
			r.Primary = r.Primary[:size-remove]
		case "affected":
			r.AffectedProjects.Projects = r.AffectedProjects.Projects[:size-remove]
			r.AffectedProjects.Omitted += remove
			r.AffectedProjects.Status = "partial"
		case "related":
			r.Related[index].Files = r.Related[index].Files[:size-remove]
		}
	}
}

func largestOutputCollection(r *Report) (kind string, index, size int) {
	consider := func(candidate string, candidateIndex, candidateSize int) {
		if candidateSize > size {
			kind, index, size = candidate, candidateIndex, candidateSize
		}
	}
	consider("boundaries", 0, len(r.Boundaries))
	consider("context", 0, len(r.Context))
	consider("relations", 0, len(r.Relations))
	consider("primary", 0, len(r.Primary))
	if r.AffectedProjects != nil {
		consider("affected", 0, len(r.AffectedProjects.Projects))
	}
	for i := range r.Related {
		consider("related", i, len(r.Related[i].Files))
	}
	return kind, index, size
}
