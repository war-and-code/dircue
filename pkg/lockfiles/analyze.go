package lockfiles

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/war-and-code/dircue/pkg/declarations"
)

const (
	npmSemantics   = "npm-package-lock-direct-tables-v1"
	nugetSemantics = "nuget-packages-lock-direct-presence-v1"
)

var ErrOutputLimit = errors.New("lockfile report exceeds output limit")

func Skip(source, tree, reason string) *Report {
	l := defaults(Limits{})
	return &Report{Provider: Provider, ProviderVersion: ProviderVersion, Status: "skipped", Source: source, Tree: tree, Semantics: []string{npmSemantics, nugetSemantics}, Limits: l, Contexts: []Context{}, Diagnostics: []Diagnostic{{Path: ".", Code: reason, Message: "Lockfile observations were omitted because the selected source traversal did not complete."}}}
}

func Analyze(ctx context.Context, in Input, limits Limits) (*Report, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	limits = defaults(limits)
	r := &Report{Provider: Provider, ProviderVersion: ProviderVersion, Status: "complete", Source: in.Source, Tree: in.Tree, Semantics: []string{npmSemantics, nugetSemantics}, Limits: limits, Contexts: []Context{}, Diagnostics: []Diagnostic{}}
	r.Coverage.OmittedFiles = in.OmittedFiles
	if !in.InventoryComplete || in.OmittedFiles > 0 {
		r.Status = "partial"
	}

	files := make(map[string]File)
	inventoryComplete := in.InventoryComplete && in.OmittedFiles == 0
	paths := make([]string, 0, min(len(in.Inventory), limits.InventoryPaths))
	ordered := slices.Clone(in.Inventory)
	slices.SortFunc(ordered, func(a, b File) int { return strings.Compare(a.Path, b.Path) })
	for _, f := range ordered {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if r.Coverage.InventoryPaths >= limits.InventoryPaths {
			r.Status = "partial"
			r.Coverage.OmittedFiles++
			inventoryComplete = false
			continue
		}
		clean, ok := cleanRelative(f.Path)
		if !ok {
			r.Status = "partial"
			inventoryComplete = false
			r.Diagnostics = append(r.Diagnostics, Diagnostic{Path: f.Path, Code: "invalid-inventory-path", Message: "Inventory path is not a confined root-relative path."})
			continue
		}
		if _, exists := files[clean]; exists {
			continue
		}
		f.Path = clean
		files[clean] = f
		paths = append(paths, clean)
		r.Coverage.InventoryPaths++
	}

	locksByDir := map[string][]string{}
	lockAllowed := map[string]bool{}
	for _, p := range paths {
		base := path.Base(p)
		if base == "package-lock.json" || base == "npm-shrinkwrap.json" || base == "packages.lock.json" || strings.HasPrefix(base, "packages.") && strings.HasSuffix(base, ".lock.json") {
			r.Coverage.LockCandidates++
			locksByDir[path.Dir(p)] = append(locksByDir[path.Dir(p)], p)
			if r.Coverage.LockCandidates <= limits.Lockfiles {
				lockAllowed[p] = true
			} else {
				r.Status = "partial"
				r.Coverage.OmittedFiles++
			}
		}
	}
	for dir := range locksByDir {
		slices.Sort(locksByDir[dir])
	}

	records := recordsFor(in)
	r.Coverage.ProjectRecords = len(records)
	dotnetCountByRoot := map[string]int{}
	for _, rec := range records {
		if rec.Project.Kind == "dotnet" && strings.EqualFold(path.Ext(rec.Project.ID), ".csproj") {
			dotnetCountByRoot[rec.Project.Root]++
		}
	}

	lockReadCache := map[string]parsedLock{}
	for _, rec := range records {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		ecosystem := ""
		manifest := strings.ReplaceAll(rec.Project.ID, "\\", "/")
		switch {
		case rec.Project.Kind == "npm" && path.Base(manifest) == "package.json":
			ecosystem = "npm"
		case rec.Project.Kind == "dotnet" && strings.EqualFold(path.Ext(manifest), ".csproj"):
			ecosystem = "nuget"
		default:
			continue
		}
		if len(r.Contexts) >= limits.Contexts {
			r.Status = "partial"
			r.Coverage.OmittedContexts++
			continue
		}
		ctxResult := Context{ProjectID: rec.Project.ID, Ecosystem: ecosystem, ManifestPath: manifest, AssociationState: "indeterminate", Checks: []Check{}, Boundaries: []Boundary{}}
		nugetSharedInputs := ecosystem == "nuget" && hasNuGetSharedInputs(paths, rec.Project.Root)
		lockPath, association, reason := associate(ecosystem, rec, records, locksByDir, dotnetCountByRoot)
		if association == "observed" && !inventoryComplete {
			association = "indeterminate"
			reason = "inventory-incomplete-association"
		}
		if nugetSharedInputs && association == "missing" {
			association = "indeterminate"
			reason = "nuget-shared-inputs-or-custom-lock-path-unresolved"
		}
		if association == "missing" && !inventoryComplete {
			association = "indeterminate"
			reason = "inventory-incomplete"
		}
		ctxResult.AssociationState = association
		if lockPath != "" {
			ctxResult.LockfilePath = lockPath
		}
		if reason != "" {
			ctxResult.Boundaries = append(ctxResult.Boundaries, Boundary{Path: lockPath, Reason: reason})
		}
		if association != "observed" {
			if association == "indeterminate" || association == "unsupported" {
				r.Status = "partial"
			}
			r.Contexts = append(r.Contexts, ctxResult)
			continue
		}
		if !lockAllowed[lockPath] {
			ctxResult.AssociationState = "indeterminate"
			ctxResult.Boundaries = append(ctxResult.Boundaries, Boundary{Path: lockPath, Reason: "lockfile-limit"})
			ctxResult.Checks = append(ctxResult.Checks, Check{Name: checkName(ecosystem), Status: "indeterminate", Explanation: "The bounded lockfile admission limit was reached before this file could be inspected."})
			r.Status = "partial"
			r.Contexts = append(r.Contexts, ctxResult)
			continue
		}
		parsed, found := lockReadCache[lockPath]
		if !found {
			f := files[lockPath]
			if f.NonRegular || f.Size < 0 || f.Size > limits.FileBytes || f.Size > limits.InputBytes-r.Coverage.InputBytes || in.ReadSelected == nil {
				parsed = parsedLock{state: "indeterminate", reason: "lockfile-unreadable"}
			} else {
				data, size, err := in.ReadSelected(ctx, lockPath, limits.FileBytes+1)
				if err != nil {
					if ctx.Err() != nil {
						return nil, ctx.Err()
					}
					if errors.Is(err, context.Canceled) {
						return nil, context.Canceled
					}
					if errors.Is(err, context.DeadlineExceeded) {
						return nil, context.DeadlineExceeded
					}
					if in.ErrorPolicy != "continue" {
						return nil, fmt.Errorf("read selected lockfile %q: %w", lockPath, err)
					}
					parsed = parsedLock{state: "indeterminate", reason: "file-read-error"}
					r.Coverage.OmittedFiles++
					r.Diagnostics = append(r.Diagnostics, Diagnostic{Path: lockPath, Code: "file-read-error", Message: "Selected lockfile could not be read."})
				} else if size != int64(len(data)) || size != f.Size || size > limits.FileBytes || size > limits.InputBytes-r.Coverage.InputBytes {
					parsed = parsedLock{state: "indeterminate", reason: "incomplete-lockfile-read"}
					r.Diagnostics = append(r.Diagnostics, Diagnostic{Path: lockPath, Code: "incomplete-lockfile-read", Message: "Selected lockfile changed, was incomplete, or exceeded the read limit."})
				} else {
					r.Coverage.InputBytes += size
					r.Coverage.LockfilesRead++
					parsed = parseLock(ecosystem, data)
				}
			}
			lockReadCache[lockPath] = parsed
		}
		ctxResult.LockfileVersion = parsed.version
		if parsed.state != "supported" {
			ctxResult.AssociationState = parsed.state
			ctxResult.Boundaries = append(ctxResult.Boundaries, Boundary{Path: lockPath, Reason: parsed.reason})
			ctxResult.Checks = append(ctxResult.Checks, Check{Name: checkName(ecosystem), Status: "indeterminate", Explanation: explanationFor(parsed.reason)})
			r.Status = "partial"
			r.Contexts = append(r.Contexts, ctxResult)
			continue
		}
		check, count, checkReason := compare(ecosystem, rec, parsed, limits.PackageNames-r.Coverage.PackageNames)
		if nugetSharedInputs {
			check.Status = "indeterminate"
			check.Explanation += " Ancestor Directory.Build.props/targets or Directory.Packages.props can add, condition, or version package references and was not evaluated."
			ctxResult.Boundaries = append(ctxResult.Boundaries, Boundary{Path: rec.Project.Root, Reason: "nuget-shared-inputs-or-custom-lock-path-unresolved"})
		}
		r.Coverage.PackageNames += count
		if checkReason != "" {
			ctxResult.Boundaries = append(ctxResult.Boundaries, Boundary{Path: lockPath, Reason: checkReason})
			r.Status = "partial"
		}
		if check.Status == "indeterminate" {
			r.Status = "partial"
		}
		ctxResult.Checks = append(ctxResult.Checks, check)
		r.Contexts = append(r.Contexts, ctxResult)
	}

	sort.Slice(r.Contexts, func(i, j int) bool {
		if r.Contexts[i].ManifestPath == r.Contexts[j].ManifestPath {
			return r.Contexts[i].Ecosystem < r.Contexts[j].Ecosystem
		}
		return r.Contexts[i].ManifestPath < r.Contexts[j].ManifestPath
	})
	if err := enforceOutputLimit(r, limits.OutputBytes); err != nil {
		return nil, err
	}
	return r, nil
}

type parsedLock struct {
	state              string
	reason             string
	version            string
	npmRoot            map[string]map[string]string
	nugetTarget        int
	nugetDirect        map[string]bool
	nugetExtendedKinds []string
}

func parseLock(ecosystem string, data []byte) parsedLock {
	root, err := declarations.ValidateJSON(data)
	if err != nil {
		return parsedLock{state: "unsupported", reason: "invalid-lockfile-json"}
	}
	switch ecosystem {
	case "npm":
		v, ok := integer(root["lockfileVersion"])
		if !ok || v < 2 || v > 3 {
			return parsedLock{state: "unsupported", reason: "unsupported-npm-lockfile-version"}
		}
		packages, ok := root["packages"].(map[string]any)
		if !ok {
			return parsedLock{state: "unsupported", reason: "npm-packages-table-missing", version: strconv.Itoa(v)}
		}
		entry, ok := packages[""].(map[string]any)
		if !ok {
			return parsedLock{state: "unsupported", reason: "npm-root-package-entry-missing", version: strconv.Itoa(v)}
		}
		deps := map[string]map[string]string{}
		for _, field := range npmDependencyFields {
			if raw, present := entry[field]; present {
				m, ok := raw.(map[string]any)
				if !ok {
					return parsedLock{state: "unsupported", reason: "invalid-npm-direct-table", version: strconv.Itoa(v)}
				}
				d := map[string]string{}
				for name, value := range m {
					text, ok := value.(string)
					if !ok || name == "" {
						return parsedLock{state: "unsupported", reason: "invalid-npm-direct-entry", version: strconv.Itoa(v)}
					}
					d[name] = text
				}
				deps[field] = d
			}
		}
		return parsedLock{state: "supported", version: strconv.Itoa(v), npmRoot: deps}
	case "nuget":
		v, ok := integer(root["version"])
		if !ok || v < 1 || v > 2 {
			return parsedLock{state: "unsupported", reason: "unsupported-nuget-lockfile-version"}
		}
		groups, ok := root["dependencies"].(map[string]any)
		if !ok || len(groups) == 0 {
			return parsedLock{state: "unsupported", reason: "nuget-targets-missing", version: strconv.Itoa(v)}
		}
		direct := map[string]bool{}
		extendedKinds := map[string]bool{}
		for _, rawTarget := range groups {
			target, ok := rawTarget.(map[string]any)
			if !ok {
				return parsedLock{state: "unsupported", reason: "invalid-nuget-target", version: strconv.Itoa(v)}
			}
			for name, raw := range target {
				entry, ok := raw.(map[string]any)
				if !ok {
					return parsedLock{state: "unsupported", reason: "invalid-nuget-package-entry", version: strconv.Itoa(v)}
				}
				kind, ok := entry["type"].(string)
				if !ok {
					return parsedLock{state: "unsupported", reason: "nuget-package-type-missing", version: strconv.Itoa(v)}
				}
				switch strings.ToLower(kind) {
				case "direct", "transitive":
				case "project", "centraltransitive":
					extendedKinds[strings.ToLower(kind)] = true
				default:
					return parsedLock{state: "unsupported", reason: "unsupported-nuget-package-type", version: strconv.Itoa(v)}
				}
				if strings.EqualFold(kind, "direct") {
					direct[strings.ToLower(name)] = true
				}
			}
		}
		var unexamined []string
		for kind := range extendedKinds {
			unexamined = append(unexamined, kind)
		}
		slices.Sort(unexamined)
		return parsedLock{state: "supported", version: strconv.Itoa(v), nugetTarget: len(groups), nugetDirect: direct, nugetExtendedKinds: unexamined}
	default:
		return parsedLock{state: "unsupported", reason: "unsupported-ecosystem"}
	}
}

var npmDependencyFields = []string{"dependencies", "devDependencies", "optionalDependencies", "peerDependencies"}

func compare(ecosystem string, rec declarations.ProjectRecord, lock parsedLock, remainingNames int) (Check, int, string) {
	if remainingNames <= 0 {
		return Check{Name: checkName(ecosystem), Status: "indeterminate", Explanation: "The package-name comparison budget was exhausted."}, 0, "package-name-limit"
	}
	switch ecosystem {
	case "npm":
		return compareNPM(rec, lock, remainingNames)
	case "nuget":
		return compareNuGet(rec, lock, remainingNames)
	default:
		return Check{Name: checkName(ecosystem), Status: "indeterminate", Explanation: "The ecosystem is outside the supported comparison subset."}, 0, "unsupported-ecosystem"
	}
}

func compareNPM(rec declarations.ProjectRecord, lock parsedLock, maxNames int) (Check, int, string) {
	c := Check{Name: "npm-direct-declaration-table-match", Status: "match", Explanation: "Compares exact declared dependency text and names in package.json with the root package entry's direct tables in npm lockfile v2/v3; text differences are not semver-resolved and this check does not validate the installed graph."}
	manifest := map[string]map[string]string{}
	unknown := false
	for _, ref := range rec.Project.References {
		if ref.Kind != "npm-dependency" && ref.Kind != "npm-local-dependency" && ref.Kind != "npm-workspace-dependency" {
			continue
		}
		if ref.Kind != "npm-dependency" || ref.State != "declared" {
			unknown = true
			continue
		}
		at := strings.LastIndexByte(ref.Value, '@')
		if at < 0 || at == 0 || at == len(ref.Value)-1 || ref.Condition == "" {
			unknown = true
			continue
		}
		name, value := ref.Value[:at], ref.Value[at+1:]
		if manifest[ref.Condition] == nil {
			manifest[ref.Condition] = map[string]string{}
		}
		manifest[ref.Condition][name] = value
	}
	if !rec.Parsed || !rec.Complete || unknown {
		c.Status = "indeterminate"
	}
	count := 0
	for _, field := range npmDependencyFields {
		declared := manifest[field]
		locked := lock.npmRoot[field]
		for name, value := range declared {
			count++
			if count > maxNames {
				c.Status = "indeterminate"
				return c, maxNames, "package-name-limit"
			}
			c.Compared++
			got, ok := locked[name]
			if !ok {
				c.Missing = append(c.Missing, name)
			} else if got != value {
				c.Mismatched = append(c.Mismatched, name)
			}
		}
		for name := range locked {
			if _, ok := declared[name]; !ok {
				count++
				if count > maxNames {
					c.Status = "indeterminate"
					return c, maxNames, "package-name-limit"
				}
				c.Unexpected = append(c.Unexpected, name)
			}
		}
	}
	if len(c.Missing) > 0 || len(c.Mismatched) > 0 || len(c.Unexpected) > 0 {
		if c.Status != "indeterminate" {
			c.Status = "different"
		}
	}
	for _, values := range [][]string{c.Missing, c.Mismatched, c.Unexpected} {
		slices.Sort(values)
	}
	return c, count, ""
}

func compareNuGet(rec declarations.ProjectRecord, lock parsedLock, maxNames int) (Check, int, string) {
	c := Check{Name: "nuget-observed-direct-package-presence", Status: "match", Explanation: "Checks that statically observed PackageReference IDs occur as Direct entries in the lockfile target groups; it does not compare requested/resolved versions, evaluate MSBuild, or establish restore consistency."}
	refs := map[string]bool{}
	conditional := false
	for _, req := range rec.Project.Requirements {
		if req.Kind != "package-reference" {
			continue
		}
		name := req.Value
		if before, _, ok := strings.Cut(name, "@"); ok {
			name = before
		}
		if !safeNuGetPackageID(name) {
			conditional = true
			continue
		}
		refs[strings.ToLower(name)] = true
		if req.State != "declared" || req.Condition != "" {
			conditional = true
		}
	}
	if len(refs) == 0 {
		c.Status = "not_applicable"
		c.Explanation = "No statically observed PackageReference IDs were available for this project; imported and computed references are outside this check."
		return c, 0, ""
	}
	if !rec.Parsed || !rec.Complete || conditional || lock.nugetTarget != 1 {
		c.Status = "indeterminate"
	}
	count := 0
	for name := range refs {
		count++
		if count > maxNames {
			c.Status = "indeterminate"
			return c, maxNames, "package-name-limit"
		}
		c.Compared++
		if !lock.nugetDirect[name] {
			c.Missing = append(c.Missing, name)
		}
	}
	if len(c.Missing) > 0 {
		if c.Status != "indeterminate" {
			c.Status = "different"
		}
	}
	slices.Sort(c.Missing)
	if len(lock.nugetExtendedKinds) > 0 {
		c.Explanation += " Project/CentralTransitive entries are present but not compared."
		return c, count, "nuget-project-or-central-transitive-unexamined"
	}
	if lock.nugetTarget > 1 {
		return c, count, "multiple-target-frameworks"
	}
	return c, count, ""
}

func associate(ecosystem string, rec declarations.ProjectRecord, records []declarations.ProjectRecord, locksByDir map[string][]string, dotnetCountByRoot map[string]int) (string, string, string) {
	root := rec.Project.Root
	if !rec.Parsed || !rec.Complete {
		return "", "indeterminate", "project-declarations-incomplete"
	}
	switch ecosystem {
	case "nuget":
		candidates := []string{}
		allCandidates := locksByDir[root]
		for _, p := range locksByDir[root] {
			base := path.Base(p)
			if strings.EqualFold(base, "packages.lock.json") || strings.EqualFold(base, "packages."+strings.TrimSuffix(path.Base(rec.Project.ID), path.Ext(rec.Project.ID))+".lock.json") {
				candidates = append(candidates, p)
			}
		}
		if len(candidates) == 0 {
			if len(allCandidates) > 0 {
				return "", "indeterminate", "nuget-lockfile-owner-unresolved"
			}
			if !hasDirectDeclarations(rec, ecosystem) {
				return "", "not_applicable", ""
			}
			return "", missingState(rec), "lockfile-not-present"
		}
		if len(candidates) != 1 {
			return "", "indeterminate", "ambiguous-nuget-lockfile-owner"
		}
		base := path.Base(candidates[0])
		if strings.EqualFold(base, "packages.lock.json") {
			if len(allCandidates) != 1 || dotnetCountByRoot[root] != 1 {
				return "", "indeterminate", "ambiguous-nuget-lockfile-owner"
			}
		} else {
			// Project-specific lock filenames are accepted only when every
			// selected NuGet lock in this directory maps uniquely to one project.
			// Arbitrary NuGetLockFilePath settings are not evaluated.
			for _, candidate := range allCandidates {
				candidateBase := path.Base(candidate)
				if candidateBase == "packages.lock.json" || !strings.HasPrefix(candidateBase, "packages.") || !strings.HasSuffix(candidateBase, ".lock.json") {
					return "", "indeterminate", "nuget-lockfile-owner-unresolved"
				}
				candidateName := strings.TrimSuffix(strings.TrimPrefix(candidateBase, "packages."), ".lock.json")
				owners := 0
				for _, other := range records {
					if other.Project.Kind == "dotnet" && other.Project.Root == root && strings.EqualFold(strings.TrimSuffix(path.Base(other.Project.ID), path.Ext(other.Project.ID)), candidateName) {
						owners++
					}
				}
				if owners != 1 {
					return "", "indeterminate", "nuget-lockfile-owner-unresolved"
				}
			}
		}
		return candidates[0], "observed", ""
	case "npm":
		own := npmLocks(locksByDir[root])
		if len(own) == 1 {
			return own[0], "observed", ""
		}
		if len(own) > 1 {
			return "", "indeterminate", "multiple-npm-lockfiles-at-project-root"
		}
		// An ancestor lockfile is not associated by path alone. Workspace
		// membership and the owning lock entry require scanner-level association
		// evidence; guessing here could compare a member against the wrong root.
		for dir := range locksByDir {
			if isAncestor(dir, root) && len(npmLocks(locksByDir[dir])) > 0 {
				return "", "indeterminate", "workspace-lockfile-owner-unresolved"
			}
		}
		if !hasDirectDeclarations(rec, ecosystem) {
			return "", "not_applicable", ""
		}
		return "", missingState(rec), "lockfile-not-present"
	}
	return "", "unsupported", "unsupported-ecosystem"
}

func npmLocks(paths []string) []string {
	var out []string
	for _, p := range paths {
		if path.Base(p) == "package-lock.json" || path.Base(p) == "npm-shrinkwrap.json" {
			out = append(out, p)
		}
	}
	return out
}

func hasDirectDeclarations(rec declarations.ProjectRecord, ecosystem string) bool {
	if ecosystem == "npm" {
		for _, ref := range rec.Project.References {
			if strings.HasPrefix(ref.Kind, "npm-") {
				return true
			}
		}
	}
	if ecosystem == "nuget" {
		for _, req := range rec.Project.Requirements {
			if req.Kind == "package-reference" {
				return true
			}
		}
	}
	return false
}

func missingState(rec declarations.ProjectRecord) string {
	if !rec.Parsed || !rec.Complete {
		return "indeterminate"
	}
	return "missing"
}

func recordsFor(in Input) []declarations.ProjectRecord {
	if len(in.ProjectRecords) > 0 {
		out := slices.Clone(in.ProjectRecords)
		slices.SortFunc(out, func(a, b declarations.ProjectRecord) int { return strings.Compare(a.Project.ID, b.Project.ID) })
		return out
	}
	out := make([]declarations.ProjectRecord, 0, len(in.Declarations.Projects))
	for _, p := range in.Declarations.Projects {
		out = append(out, declarations.ProjectRecord{Project: p, Parsed: true, Complete: in.Declarations.Status == "complete" && in.Declarations.Coverage.OmittedFiles == 0})
	}
	slices.SortFunc(out, func(a, b declarations.ProjectRecord) int { return strings.Compare(a.Project.ID, b.Project.ID) })
	return out
}

func defaults(l Limits) Limits {
	if l.InventoryPaths <= 0 {
		l.InventoryPaths = DefaultMaxInventoryPaths
	}
	if l.Lockfiles <= 0 {
		l.Lockfiles = DefaultMaxLockfiles
	}
	if l.FileBytes <= 0 {
		l.FileBytes = DefaultMaxFileBytes
	}
	if l.InputBytes <= 0 {
		l.InputBytes = DefaultMaxInputBytes
	}
	if l.PackageNames <= 0 {
		l.PackageNames = DefaultMaxPackageNames
	}
	if l.Contexts <= 0 {
		l.Contexts = DefaultMaxContexts
	}
	if l.OutputBytes <= 0 {
		l.OutputBytes = DefaultMaxOutputBytes
	}
	return l
}

func integer(v any) (int, bool) {
	switch n := v.(type) {
	case float64:
		if n >= 0 && n == float64(int(n)) {
			return int(n), true
		}
	case json.Number:
		i, err := strconv.Atoi(string(n))
		if err == nil && i >= 0 {
			return i, true
		}
	}
	return 0, false
}

func safeNuGetPackageID(s string) bool {
	if s == "" || len(s) > 256 {
		return false
	}
	for _, r := range s {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune(".-_", r)) {
			return false
		}
	}
	return true
}

func cleanRelative(p string) (string, bool) {
	if p == "" || strings.Contains(p, "\\") || path.IsAbs(p) {
		return "", false
	}
	c := path.Clean(p)
	if c == "." || c == ".." || strings.HasPrefix(c, "../") || c != p {
		return "", false
	}
	return c, true
}

func hasNuGetSharedInputs(paths []string, projectRoot string) bool {
	for _, p := range paths {
		base := strings.ToLower(path.Base(p))
		if base != "directory.build.props" && base != "directory.build.targets" && base != "directory.packages.props" {
			continue
		}
		dir := path.Dir(p)
		if dir == projectRoot || isAncestor(dir, projectRoot) {
			return true
		}
	}
	return false
}

func isAncestor(parent, child string) bool {
	rel, ok := relativeTo(parent, child)
	return ok && rel != "."
}

func relativeTo(parent, child string) (string, bool) {
	parent = strings.Trim(path.Clean(parent), "/")
	child = strings.Trim(path.Clean(child), "/")
	if parent == "." || parent == "" {
		if child == "." || child == "" {
			return ".", true
		}
		return child, true
	}
	if child == parent {
		return ".", true
	}
	if strings.HasPrefix(child, parent+"/") {
		return strings.TrimPrefix(child, parent+"/"), true
	}
	return "", false
}

func checkName(ecosystem string) string {
	if ecosystem == "npm" {
		return "npm-direct-declaration-table-match"
	}
	if ecosystem == "nuget" {
		return "nuget-observed-direct-package-presence"
	}
	return "direct-dependency-presence"
}

func explanationFor(reason string) string {
	switch reason {
	case "unsupported-npm-lockfile-version":
		return "Only npm lockfile versions 2 and 3 are supported; no dependency-resolution claim is made."
	case "unsupported-nuget-lockfile-version":
		return "Only NuGet packages.lock.json format version 1 is supported."
	case "invalid-lockfile-json":
		return "The selected lockfile is invalid or contains duplicate JSON keys; no comparison was made."
	default:
		return "The lockfile could not be safely interpreted within the supported bounded subset."
	}
}

func enforceOutputLimit(r *Report, limit int) error {
	data, err := json.Marshal(r)
	if err != nil {
		return err
	}
	if len(data) <= limit {
		return nil
	}
	return ErrOutputLimit
}
