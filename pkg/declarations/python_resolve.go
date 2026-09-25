package declarations

import (
	"path"
	"slices"
	"strings"
)

// auxiliaryPriority returns a sort key for breaking ties when multiple auxiliary
// Python manifests exist at the same root. Lower value = higher priority (wins).
func auxiliaryPriority(d *Document) int {
	switch path.Base(d.Project.ID) {
	case "setup.py":
		return 0
	case "setup.cfg":
		return 1
	case "Pipfile":
		return 2
	}
	return 3 // requirements*.txt and other auxiliary files
}

// resolveRequirementsIncludes copies requirements from -r included files into the
// including document. Resolution is transitive up to maxIncludeDepth levels.
// Only files already parsed and present in byPath are followed. Cycles are
// detected via a visited set. The included files themselves are not modified.
func resolveRequirementsIncludes(docs []*Document, byPath map[string]*Document) {
	const maxIncludeDepth = 5

	for _, root := range docs {
		if root == nil || root.Project == nil {
			continue
		}
		data, ok := root.Data.(*pythonData)
		if !ok || data.includeOnly {
			continue
		}
		// Check if this doc has any -r references at all.
		hasIncludes := false
		for _, ref := range root.Project.References {
			if ref.Kind == "python-requirements-include" {
				hasIncludes = true
				break
			}
		}
		if !hasIncludes {
			continue
		}

		type workItem struct {
			doc   *Document
			depth int
		}
		visited := map[string]bool{root.Project.ID: true}
		queue := []workItem{}

		// Seed the queue from direct -r references on root.
		for i := range root.Project.References {
			ref := &root.Project.References[i]
			if ref.Kind != "python-requirements-include" || ref.Target == "" {
				continue
			}
			inc := byPath[ref.Target]
			if inc == nil || inc.Project == nil {
				// Not in inventory as a parsed manifest: leave state as declared/unresolved.
				continue
			}
			ref.TargetStatus = "present"
			ref.State = "resolved"
			if !visited[ref.Target] {
				visited[ref.Target] = true
				queue = append(queue, workItem{inc, 1})
			}
		}

		for len(queue) > 0 {
			curr := queue[0]
			queue = queue[1:]
			// Merge requirements from curr.doc into root (preserve original evidence paths).
			for _, req := range curr.doc.Project.Requirements {
				if req.Kind == "declaration-semantics" || req.Kind == "python-requires-python" {
					continue
				}
				AddRequirement(root, req)
			}
			if curr.depth >= maxIncludeDepth {
				continue
			}
			// Follow transitive -r references inside the included file.
			for _, ref := range curr.doc.Project.References {
				if ref.Kind != "python-requirements-include" || ref.Target == "" {
					continue
				}
				if visited[ref.Target] {
					continue
				}
				inc := byPath[ref.Target]
				if inc == nil || inc.Project == nil {
					continue
				}
				visited[ref.Target] = true
				queue = append(queue, workItem{inc, curr.depth + 1})
			}
		}
	}
}

type pythonWorkspace struct {
	doc        *Document
	members    map[string]*Document
	byName     map[string][]*Document
	incomplete bool
}

// ResolvePython only consults the selected inventory. A matched manifest proves
// a declared relationship, not installability or the active uv environment.
func ResolvePython(docs []*Document, files map[string]bool) {
	pyprojectRoots := map[string]bool{}
	for _, d := range docs {
		if d != nil && d.Project != nil && path.Base(d.Project.ID) == "pyproject.toml" {
			pyprojectRoots[d.Project.Root] = true
		}
	}

	// Step 1: resolve -r includes BEFORE filtering, while all parsed docs are available.
	byPath := map[string]*Document{}
	for _, d := range docs {
		if d != nil && d.Project != nil {
			byPath[d.Project.ID] = d
		}
	}
	resolveRequirementsIncludes(docs, byPath)

	for _, d := range docs {
		if d == nil || d.Project == nil {
			continue
		}
		data, ok := d.Data.(*pythonData)
		if !ok || !data.auxiliary {
			continue
		}
		// Include-only files (requirements/ subdirectory targets) never become
		// standalone components regardless of what else is in the tree.
		if data.includeOnly {
			d.Project = nil
			continue
		}
		// Drop auxiliary Python documents that are shadowed by a pyproject.toml in
		// the same root (the primary manifest wins) or have no Python evidence in
		// their tree. A requirements.txt with no .py siblings is still kept: it is
		// the only manifest file in that directory and therefore valid evidence of a
		// Python dependency set, even when the application code isn't in the tree
		// (pre-built images, Docker contexts, separate code repos, etc.).
		if pyprojectRoots[d.Project.Root] {
			d.Project = nil
			continue
		}
		if !pythonSourceInRoot(files, d.Project.Root) && !pythonReqsOnlyRoot(files, d.Project.Root) {
			d.Project = nil
		}
	}

	// Step 2: deduplicate — when multiple auxiliary manifests share the same root,
	// keep the highest-priority one and merge the others' requirements into it.
	// Priority: setup.py > setup.cfg > Pipfile > requirements*.txt.
	// This prevents duplicate (root) components when both Pipfile and requirements.txt
	// coexist, while preserving evidence paths from all contributing files.
	byRootAux := map[string][]*Document{}
	for _, d := range docs {
		if d == nil || d.Project == nil {
			continue
		}
		data, ok := d.Data.(*pythonData)
		if !ok || !data.auxiliary {
			continue
		}
		byRootAux[d.Project.Root] = append(byRootAux[d.Project.Root], d)
	}
	for _, group := range byRootAux {
		if len(group) <= 1 {
			continue
		}
		slices.SortFunc(group, func(a, b *Document) int {
			if pa, pb := auxiliaryPriority(a), auxiliaryPriority(b); pa != pb {
				return pa - pb
			}
			return strings.Compare(a.Project.ID, b.Project.ID)
		})
		winner := group[0]
		for _, loser := range group[1:] {
			// Merge loser's requirements into winner (preserving original evidence paths).
			for _, req := range loser.Project.Requirements {
				if req.Kind == "declaration-semantics" {
					continue
				}
				AddRequirement(winner, req)
			}
			loser.Project = nil
		}
	}
	pythonDocs := []*Document{}
	byID := map[string]*Document{}
	workspaces := map[string]*pythonWorkspace{}
	for _, d := range docs {
		if d == nil || d.Project == nil {
			continue
		}
		data, ok := d.Data.(*pythonData)
		if !ok {
			continue
		}
		pythonDocs = append(pythonDocs, d)
		byID[d.Project.ID] = d
		if data.workspace && data.managed {
			workspaces[d.Project.ID] = &pythonWorkspace{doc: d, members: map[string]*Document{}, byName: map[string][]*Document{}}
		}
	}
	if len(pythonDocs) == 0 {
		return
	}
	slices.SortFunc(pythonDocs, func(a, b *Document) int { return strings.Compare(a.Project.ID, b.Project.ID) })
	directories := map[string]bool{}
	needDirectories := len(workspaces) > 0
	for _, d := range pythonDocs {
		for _, ref := range d.Project.References {
			if ref.Kind == "python-backend-path" && ref.Target != "" {
				needDirectories = true
			}
		}
	}
	if needDirectories {
		for filename := range files {
			for dir := path.Dir(filename); !directories[dir]; dir = path.Dir(dir) {
				directories[dir] = true
				if dir == "." || dir == "/" || path.Dir(dir) == dir {
					break
				}
			}
		}
	}
	dirs := make([]string, 0, len(directories))
	for dir := range directories {
		dirs = append(dirs, dir)
	}
	slices.Sort(dirs)
	parents := map[string][]*pythonWorkspace{}
	checks := 0
	for _, d := range pythonDocs {
		ws := workspaces[d.Project.ID]
		if ws == nil {
			continue
		}
		data := d.Data.(*pythonData)
		ws.incomplete = data.invalidPatterns || data.invalidMembers || d.limited
		if data.project {
			pythonIncludeMember(ws, d, d.Project.ID, files, parents)
		}
		if data.invalidPatterns {
			continue
		}
		for _, pattern := range data.patterns {
			if d.limited {
				ws.incomplete = true
				break
			}
			matched := false
			for _, dir := range dirs {
				if checks >= pythonMatchBudget {
					ws.incomplete = true
					AddDiagnostic(d, "python-workspace-match-limit", "Workspace matching exceeded the comparison limit; membership is incomplete.")
					break
				}
				checks++
				relative, inside := pythonRelative(d.Project.Root, dir)
				if !inside || relative == "." {
					continue
				}
				match, err := MatchPattern(pattern, relative)
				if err != nil {
					ws.incomplete = true
					AddDiagnostic(d, "python-workspace-match-limit", "A selected directory exceeds the supported matching limits.")
					continue
				}
				if !match {
					continue
				}
				matched = true
				excluded := false
				for _, exclude := range data.excludes {
					if checks >= pythonMatchBudget {
						ws.incomplete = true
						excluded = true
						AddDiagnostic(d, "python-workspace-match-limit", "Workspace matching exceeded the comparison limit; membership is incomplete.")
						break
					}
					checks++
					excludeMatch, err := MatchPattern(exclude, relative)
					if err != nil {
						ws.incomplete = true
						excluded = true
						break
					}
					if excludeMatch {
						excluded = true
						break
					}
				}
				if excluded {
					continue
				}
				target := path.Join(dir, "pyproject.toml")
				pythonIncludeMember(ws, byID[target], target, files, parents)
				if d.limited {
					ws.incomplete = true
					break
				}
			}
			if !matched && checks < pythonMatchBudget {
				if !strings.ContainsAny(pattern, "*?[") {
					excluded := false
					for _, exclude := range data.excludes {
						match, _ := MatchPattern(exclude, pattern)
						if match {
							excluded = true
							break
						}
					}
					if excluded {
						continue
					}
					target, ok := LocalTarget(d.Project.ID, pattern, "pyproject.toml")
					if ok {
						pythonIncludeMember(ws, byID[target], target, files, parents)
					}
				} else {
					AddDiagnostic(d, "unmatched-python-workspace-pattern", "A workspace member pattern matched no directory in the selected inventory.")
				}
			}
			if checks >= pythonMatchBudget {
				break
			}
		}
	}
	for _, d := range pythonDocs {
		// Backend paths point to directories rather than package manifests.
		for i := range d.Project.References {
			ref := &d.Project.References[i]
			if ref.Kind == "python-backend-path" && ref.Target != "" {
				ref.TargetStatus = "missing"
				if directories[ref.Target] {
					ref.TargetStatus = "present"
				}
				if ref.TargetStatus == "present" {
					ref.State = "resolved"
				} else {
					ref.State = "missing"
				}
			}
		}
		data := d.Data.(*pythonData)
		if !data.managed {
			continue
		}
		own := workspaces[d.Project.ID]
		scopes := parents[d.Project.ID]
		if own != nil && len(scopes) == 0 {
			scopes = []*pythonWorkspace{own}
		}
		if len(scopes) > 1 {
			AddDiagnostic(d, "ambiguous-python-workspace", "The project is declared as a member of more than one workspace.")
		}
		var scope *pythonWorkspace
		if len(scopes) == 1 {
			scope = scopes[0]
		}
		lockRoot := d.Project.Root
		if scope != nil {
			lockRoot = scope.doc.Project.Root
		}
		lock := path.Join(lockRoot, "uv.lock")
		if files[lock] {
			AddReference(d, Reference{Kind: "uv-lockfile", Value: "presence-only", Target: lock, State: "declared", TargetStatus: "present", Evidence: d.Project.ID})
		}
		// Retain local and remote source declarations even if no dependency uses them.
		for _, name := range pythonSourceNames(data.sources) {
			for _, source := range data.sources[name] {
				ref := pythonResolveSource(name, source, d, scope, workspaces, files, byID)
				ref.Kind = "uv-source-" + source.kind
				AddReference(d, ref)
			}
		}
		for _, dep := range data.dependencies {
			if d.limited {
				break
			}
			declaring := d
			sources, exists := data.sources[dep.name]
			if !exists && scope != nil && scope.doc != d {
				sources, exists = scope.doc.Data.(*pythonData).sources[dep.name]
				declaring = scope.doc
			}
			if !exists {
				continue
			}
			for _, source := range sources {
				if source.kind == "remote" {
					continue
				}
				ref := pythonResolveSource(dep.name, source, declaring, scope, workspaces, files, byID)
				ref.Kind = "uv-local-dependency"
				ref.Condition = pythonCondition(dep.condition, ref.Condition)
				if ref.Condition != "" && ref.State == "resolved" {
					ref.State = "conditional"
				}
				if declaring != d {
					ref.Condition = pythonCondition(ref.Condition, "source-inherited-from:"+declaring.Project.ID)
				}
				AddReference(d, ref)
			}
		}

	}
}

// pythonReqsOnlyRoot returns true when the selected inventory contains at
// least one requirements*.txt file or a Pipfile directly in root and no
// competing primary Python manifest (setup.py) at the same level. pyproject.toml
// is handled before this call by the caller. This supports repos that declare a
// Python dependency set via requirements.txt or Pipfile only — no source files,
// no setup.py — such as a service whose code lives elsewhere or is shipped as a
// pre-built container image.
func pythonReqsOnlyRoot(files map[string]bool, root string) bool {
	hasReqs := false
	rootDir := root
	if rootDir == "" {
		rootDir = "."
	}
	for filename := range files {
		dir := path.Dir(filename)
		if dir != rootDir {
			continue
		}
		base := strings.ToLower(path.Base(filename))
		if base == "setup.py" {
			return false // setup.py in same dir takes precedence; drop auxiliary
		}
		if base == "pipfile" {
			hasReqs = true
		}
		if strings.HasPrefix(base, "requirements") && strings.HasSuffix(base, ".txt") {
			hasReqs = true
		}
	}
	return hasReqs
}

func pythonSourceInRoot(files map[string]bool, root string) bool {
	for filename := range files {
		if path.Ext(filename) != ".py" || path.Base(filename) == "setup.py" {
			continue
		}
		relative, ok := pythonRelative(root, path.Dir(filename))
		if ok && relative != "" {
			return true
		}
	}
	return false
}

func pythonRelative(root, dir string) (string, bool) {
	if root == "." {
		return dir, true
	}
	if root == dir {
		return ".", true
	}
	prefix := root + "/"
	if !strings.HasPrefix(dir, prefix) {
		return "", false
	}
	return strings.TrimPrefix(dir, prefix), true
}
func pythonIncludeMember(ws *pythonWorkspace, member *Document, target string, files map[string]bool, parents map[string][]*pythonWorkspace) {
	if _, exists := ws.members[target]; exists {
		return
	}
	// Track missing declarations too, so overlapping patterns do not multiply output.
	ws.members[target] = member
	ref := Reference{Kind: "uv-workspace-member", Value: "declared-member", Target: target, State: "missing", TargetStatus: "missing", Evidence: ws.doc.Project.ID}
	if files[target] {
		ref.TargetStatus = "unparsed"
		ref.State = "unresolved"
	}
	if member != nil {
		data := member.Data.(*pythonData)
		if !data.managed {
			return
		}
		if member != ws.doc && data.workspace {
			ref.TargetStatus = "unsupported"
			ref.State = "unresolved"
			ws.incomplete = true
			AddDiagnostic(ws.doc, "nested-python-workspace", "A matched member declares another workspace; nested membership is not established.")
		} else if !data.project || member.Project.Name == "" {
			ref.TargetStatus = "unsupported"
			ref.State = "unresolved"
			ws.incomplete = true
			AddDiagnostic(ws.doc, "python-member-without-project", "A selected member has no supported project table with a static project name.")
		} else {
			ref.TargetStatus = "present"
			ref.State = "resolved"
			parents[target] = append(parents[target], ws)
			if member.Project.Name != "" {
				canonical := pythonCanonical(member.Project.Name)
				ws.byName[canonical] = append(ws.byName[canonical], member)
				if len(ws.byName[canonical]) > 1 {
					AddDiagnostic(ws.doc, "duplicate-python-member-name", "Workspace members share a normalized project name.")
				}
			}
		}
	} else {
		ws.incomplete = true
		AddDiagnostic(ws.doc, "missing-python-workspace-member", "A declared member has no parsed project in the selected inventory.")
	}
	if !AddReference(ws.doc, ref) {
		ws.incomplete = true
	}
}
func pythonSourceNames(m map[string][]pythonSource) []string {
	out := make([]string, 0, len(m))
	for key := range m {
		out = append(out, key)
	}
	slices.Sort(out)
	return out
}
func pythonResolveSource(name string, source pythonSource, declaring *Document, scope *pythonWorkspace, workspaces map[string]*pythonWorkspace, files map[string]bool, byID map[string]*Document) Reference {
	ref := Reference{Value: name, State: "unresolved", TargetStatus: "unresolved", Evidence: declaring.Project.ID, Condition: source.marker}
	if source.invalid || source.kind == "remote" {
		return ref
	}
	target := source.target
	if source.kind == "workspace" {
		workspace := scope
		if source.target != "" {
			workspace = workspaces[source.target]
		}
		if workspace == nil {
			return ref
		}
		candidates := workspace.byName[name]
		if len(candidates) > 1 {
			ref.TargetStatus = "ambiguous"
			return ref
		}
		if len(candidates) == 0 {
			ref.TargetStatus = "missing"
			if workspace.incomplete {
				ref.TargetStatus = "unresolved"
			}
			return ref
		}
		if workspace.incomplete {
			ref.TargetStatus = "unresolved"
			return ref
		}
		target = candidates[0].Project.ID
	}
	ref.Target = target
	if target == "" {
		return ref
	}
	ref.TargetStatus = "missing"
	ref.State = "missing"
	if files[target] {
		ref.TargetStatus = "unparsed"
		if member := byID[target]; member != nil && member.Project.Name != "" {
			if pythonCanonical(member.Project.Name) == name {
				ref.TargetStatus = "present"
				ref.State = "resolved"
			} else {
				ref.TargetStatus = "name-mismatch"
				ref.State = "unresolved"
			}
		} else {
			ref.State = "unresolved"
		}
	}
	if ref.Condition != "" {
		ref.State = "conditional"
	}
	return ref
}
