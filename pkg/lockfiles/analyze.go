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
	"unicode"
	"unicode/utf8"

	"github.com/war-and-code/dircue/pkg/declarations"
)

const (
	npmSemantics          = "npm-package-lock-direct-tables-v1"
	nugetSemantics        = "nuget-packages-lock-direct-presence-v1"
	npmWorkspaceSemantics = "npm-workspace-member-lock-descriptors-v1"
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
	if in.OmittedFiles < 0 || in.Declarations.Coverage.OmittedFiles < 0 || in.Declarations.Coverage.OmittedDiagnostics < 0 {
		return nil, errors.New("lockfile input omission counts cannot be negative")
	}
	projectRecordCount := len(in.ProjectRecords)
	if projectRecordCount == 0 {
		projectRecordCount = len(in.Declarations.Projects)
	}
	if len(in.Inventory) > DefaultMaxInventoryPaths || projectRecordCount > DefaultMaxInventoryPaths {
		return nil, errors.New("lockfile input exceeds the supported inventory maximum")
	}
	if (in.Source != "directory" && in.Source != "git") ||
		(in.Source == "git" && !validGitTree(in.Tree)) ||
		(in.Source == "directory" && in.Tree != "") {
		return nil, errors.New("lockfile input source identity is invalid")
	}
	if in.Declarations.Source != "" || in.Declarations.Tree != "" {
		if in.Declarations.Source != in.Source || in.Declarations.Tree != in.Tree {
			return nil, errors.New("lockfile declaration source identity does not match selected input")
		}
	}
	limits = defaults(limits)
	if limits.InventoryPaths > DefaultMaxInventoryPaths || limits.Lockfiles > DefaultMaxLockfiles ||
		limits.FileBytes > DefaultMaxFileBytes || limits.InputBytes > DefaultMaxInputBytes ||
		limits.PackageNames > DefaultMaxPackageNames || limits.Contexts > DefaultMaxContexts ||
		limits.OutputBytes > DefaultMaxOutputBytes {
		return nil, errors.New("lockfile limits exceed the supported maximum")
	}
	semantics := []string{npmSemantics, nugetSemantics}
	if in.WorkspaceLocks {
		semantics = append(semantics, npmWorkspaceSemantics)
	}
	r := &Report{Provider: Provider, ProviderVersion: ProviderVersion, Status: "complete", Source: in.Source, Tree: in.Tree, Semantics: semantics, Limits: limits, Contexts: []Context{}, Diagnostics: []Diagnostic{}}
	r.Coverage.OmittedFiles = in.OmittedFiles
	if !in.InventoryComplete || in.OmittedFiles > 0 {
		r.Status = "partial"
	}
	if in.Declarations.Status != "" && in.Declarations.Status != "complete" ||
		in.Declarations.Coverage.OmittedFiles > 0 || in.Declarations.Coverage.OmittedDiagnostics > 0 {
		r.Status = "partial"
	}
	omissions := newOmissionScope(in)

	files := make(map[string]File)
	selectedFiles := make(map[string]File)
	inventoryComplete := in.InventoryComplete && in.OmittedFiles == 0
	paths := make([]string, 0, min(len(in.Inventory), limits.InventoryPaths))
	ordered := slices.Clone(in.Inventory)
	slices.SortFunc(ordered, func(a, b File) int { return strings.Compare(a.Path, b.Path) })
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	selectedComplete := in.SelectedFilesComplete
	selectedOrdered := slices.Clone(in.SelectedFiles)
	slices.SortFunc(selectedOrdered, func(a, b File) int { return strings.Compare(a.Path, b.Path) })
	for _, f := range selectedOrdered {
		clean, ok := cleanSelectedRelative(f.Path, in.WorkspaceLocks)
		if !ok || f.Size < 0 {
			selectedComplete = false
			continue
		}
		f.Path = clean
		if previous, exists := selectedFiles[clean]; exists {
			if previous.Size != f.Size || previous.NonRegular != f.NonRegular {
				selectedComplete = false
			}
			continue
		}
		if len(selectedFiles) >= limits.InventoryPaths {
			selectedComplete = false
			continue
		}
		selectedFiles[clean] = f
	}
	readableFiles := make(map[string]File, len(files)+len(selectedFiles))
	for p, f := range files {
		readableFiles[p] = f
	}
	for p, f := range selectedFiles {
		readableFiles[p] = f
	}
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
		clean, ok := cleanSelectedRelative(f.Path, in.WorkspaceLocks)
		if !ok {
			r.Status = "partial"
			inventoryComplete = false
			r.Diagnostics = append(r.Diagnostics, Diagnostic{Path: ".", Code: "invalid-inventory-path", Message: "An inventory path was not a confined root-relative path."})
			continue
		}
		if existing, exists := files[clean]; exists {
			if existing.Size != f.Size || existing.NonRegular != f.NonRegular {
				r.Status = "partial"
				r.Coverage.OmittedFiles++
				inventoryComplete = false
				r.Diagnostics = append(r.Diagnostics, Diagnostic{Path: clean, Code: "conflicting-inventory-entry", Message: "Conflicting metadata was supplied for the same selected inventory path."})
			}
			continue
		}
		f.Path = clean
		files[clean] = f
		paths = append(paths, clean)
		r.Coverage.InventoryPaths++
	}

	locksByDir := map[string][]string{}
	lockAllowed := map[string]bool{}
	markersByDir := map[string][]string{}
	for _, p := range paths {
		base := path.Base(p)
		if IsAlternativeNPMMarker(base) {
			markersByDir[path.Dir(p)] = append(markersByDir[path.Dir(p)], p)
		}
		if base == "package-lock.json" || base == "npm-shrinkwrap.json" || isNuGetLockCandidateName(base) {
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
	for dir := range markersByDir {
		slices.Sort(markersByDir[dir])
	}

	records := recordsFor(in)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	r.Coverage.ProjectRecords = len(records)
	validRecords := records[:0]
	recordIndex := map[string]int{}
	duplicateDiagnostics := map[string]bool{}
	for _, rec := range records {
		manifest, root, ok := cleanProjectPathsMode(rec.Project.ID, rec.Project.Root, in.WorkspaceLocks)
		if !ok {
			r.Status = "partial"
			r.Coverage.OmittedContexts++
			r.Diagnostics = append(r.Diagnostics, Diagnostic{Path: ".", Code: "invalid-project-record", Message: "A project record had an invalid or inconsistent root-relative path and was omitted."})
			continue
		}
		rec.Project.ID = manifest
		rec.Project.Root = root
		if index, exists := recordIndex[manifest]; exists {
			validRecords[index].Parsed = false
			validRecords[index].Complete = false
			r.Status = "partial"
			r.Coverage.OmittedContexts++
			if !duplicateDiagnostics[manifest] {
				r.Diagnostics = append(r.Diagnostics, Diagnostic{Path: manifest, Code: "duplicate-project-record", Message: "Duplicate project records were found; their dependency evidence was treated as incomplete."})
				duplicateDiagnostics[manifest] = true
			}
			continue
		}
		recordIndex[manifest] = len(validRecords)
		validRecords = append(validRecords, rec)
	}
	records = validRecords

	npm := newNPMResolver(in, records, locksByDir, markersByDir, omissions)
	lockReadCache := map[string]parsedLock{}
	selectedReadCache := map[string]selectedFileRead{}
	// Evaluate every MSBuild project, including project types that receive no
	// NuGet context, so a lock path is attributed only when no other project's
	// possible values can name it.
	nugetFiles := make(map[string]File, len(selectedFiles)+len(files))
	for p, f := range files {
		nugetFiles[p] = f
	}
	for p, f := range selectedFiles {
		nugetFiles[p] = f
	}
	nugetIndex := newNuGetFileIndex(nugetFiles, selectedComplete && inventoryComplete)
	walker := &nugetWalker{ctx: ctx, in: in, index: nugetIndex, limits: limits, inputBytes: &r.Coverage.InputBytes, reads: selectedReadCache, parsed: map[string]*nugetParsed{}, globalSDKs: map[string]nugetGlobalSDKParse{}}
	nugetEvals := map[string]*nugetEval{}
	for _, rec := range records {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if rec.Project.Kind != "dotnet" || !isMSBuildProjectRecord(path.Ext(rec.Project.ID)) {
			continue
		}
		e, err := walker.evaluate(rec.Project.ID)
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			return nil, fmt.Errorf("read selected project file %q: %w", rec.Project.ID, err)
		}
		nugetEvals[rec.Project.ID] = e
	}
	nugetClaims := newNuGetClaims(nugetEvals, nugetIndex)
	customLockCounted := map[string]bool{}
	for _, rec := range records {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		ecosystem := ""
		manifest := rec.Project.ID
		if !in.WorkspaceLocks {
			manifest = strings.ReplaceAll(manifest, "\\", "/")
		}
		switch {
		case rec.Project.Kind == "npm" && path.Base(manifest) == "package.json":
			ecosystem = "npm"
		case rec.Project.Kind == "dotnet" && IsNuGetLockProject(manifest):
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
		var a association
		var nugetE *nugetEval
		var nugetBasis, ownershipState string
		var nugetCauses []NuGetCause
		var checkReasons []string
		appendContext := func() {
			if ecosystem == "nuget" {
				ctxResult.NuGetEvidence = makeNuGetEvidence(rec, nugetE, nugetIndex, ctxResult, ownershipState, nugetBasis, nugetCauses, checkReasons, inventoryComplete && selectedComplete)
			}
			r.Contexts = append(r.Contexts, ctxResult)
		}
		if ecosystem == "npm" {
			a = npm.associate(rec)
		} else {
			nugetE = nugetEvals[rec.Project.ID]
			if nugetE == nil || nugetE.projectUnreadable {
				r.Status = "partial"
				r.Coverage.OmittedFiles++
				r.Diagnostics = append(r.Diagnostics, Diagnostic{Path: manifest, Code: "nuget-project-config-unresolved", Message: "The selected project XML could not be fully inspected within the bounded input limits."})
			}
			a, nugetBasis, nugetCauses = associateNuGetStatic(rec, nugetE, nugetClaims, nugetIndex, omissions, selectedComplete)
		}
		selectedNPMShrinkwrap := ecosystem == "npm" && a.state == "observed" && path.Base(a.lockPath) == "npm-shrinkwrap.json"
		if ecosystem == "nuget" && a.state == "observed" && !omissions.nugetOwnersKnown(rec.Project.Root) {
			a.override("indeterminate", "project-inventory-incomplete-association")
		}
		if a.state == "observed" && !inventoryComplete {
			a.override("indeterminate", "inventory-incomplete-association")
		}
		if ecosystem == "nuget" && a.state == "observed" && !lockAllowed[a.lockPath] {
			// The inventory pass counted only conventional lock names; a
			// custom path is counted once, when a project first owns it.
			_, inventoried := files[a.lockPath]
			counted := inventoried && isNuGetLockCandidateName(path.Base(a.lockPath))
			if !counted && !customLockCounted[a.lockPath] {
				customLockCounted[a.lockPath] = true
				r.Coverage.LockCandidates++
			}
			if r.Coverage.LockCandidates > limits.Lockfiles {
				a.override("indeterminate", "lockfile-limit")
			} else {
				lockAllowed[a.lockPath] = true
			}
		}
		if a.state == "missing" && !inventoryComplete {
			a.override("indeterminate", "inventory-incomplete")
		}
		ownershipState = a.state
		lockPath, association, workspaceRoot := a.lockPath, a.state, a.workspaceRoot
		ctxResult.AssociationState = association
		if lockPath != "" {
			ctxResult.LockfilePath = lockPath
		}
		if ecosystem == "nuget" && a.reason != "" && a.reasonPath != "" && a.state != "observed" && a.state != "not_applicable" {
			// The project or file the ownership decision names, such as
			// another project that can use the same lockfile.
			nugetCauses = append(nugetCauses, NuGetCause{Reason: a.reason, Path: a.reasonPath, Detail: nugetDetail(a.detail)})
		}
		if a.reason != "" {
			boundaryPath := a.reasonPath
			if boundaryPath == "" {
				boundaryPath = lockPath
			}
			ctxResult.Boundaries = append(ctxResult.Boundaries, Boundary{Path: boundaryPath, Reason: a.reason, Detail: a.detail})
		}
		ctxResult.Boundaries = append(ctxResult.Boundaries, a.extra...)
		if selectedNPMShrinkwrap {
			ctxResult.Boundaries = append(ctxResult.Boundaries, Boundary{Path: lockPath, Reason: "npm-v11-shrinkwrap-selection"})
		}
		if association != "observed" {
			if association == "indeterminate" || association == "unsupported" {
				r.Status = "partial"
			}
			appendContext()
			continue
		}
		if !lockAllowed[lockPath] {
			ctxResult.AssociationState = "indeterminate"
			ctxResult.Boundaries = append(ctxResult.Boundaries, Boundary{Path: lockPath, Reason: "lockfile-limit"})
			ctxResult.Checks = append(ctxResult.Checks, Check{Name: checkName(ecosystem), Status: "indeterminate", Explanation: "The bounded lockfile admission limit was reached before this file could be inspected."})
			r.Status = "partial"
			appendContext()
			continue
		}
		parsed, found := lockReadCache[lockPath]
		if !found {
			f, selectedFile := files[lockPath]
			if !selectedFile {
				f, selectedFile = selectedFiles[lockPath]
			}
			if !selectedFile || f.NonRegular || f.Size < 0 || f.Size > limits.FileBytes || f.Size > limits.InputBytes-r.Coverage.InputBytes || in.ReadSelected == nil {
				parsed = parsedLock{state: "indeterminate", reason: "lockfile-unreadable"}
			} else {
				data, size, err := in.ReadSelected(ctx, lockPath, limits.FileBytes+1)
				if ctx.Err() != nil {
					return nil, ctx.Err()
				}
				if err != nil {
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
					if err := ctx.Err(); err != nil {
						return nil, err
					}
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
			appendContext()
			continue
		}
		comparisonLock := parsed
		if workspaceRoot != "" {
			memberRel, ok := relativeTo(workspaceRoot, rec.Project.Root)
			entry, state, why := parsed.npmMember(memberRel)
			if !ok || memberRel == "." {
				state, why = "indeterminate", "npm-workspace-lock-member-entry-missing"
			}
			if state != "supported" {
				explanation := "The selected workspace lockfile has no package descriptor for this member path; no member declaration comparison was made."
				if state == "unsupported" {
					explanation = explanationFor(why)
				}
				ctxResult.AssociationState = state
				ctxResult.Boundaries = append(ctxResult.Boundaries, Boundary{Path: lockPath, Reason: why})
				ctxResult.Checks = append(ctxResult.Checks, Check{Name: checkName(ecosystem), Status: "indeterminate", Explanation: explanation})
				r.Status = "partial"
				appendContext()
				continue
			}
			comparisonLock.npmRoot = entry
			ctxResult.Boundaries = append(ctxResult.Boundaries, Boundary{Path: lockPath, Reason: "npm-workspace-lock-ownership-observed", Detail: "Workspace owner " + path.Join(workspaceRoot, "package.json") + " declares member " + manifest + "."})
		}
		var check Check
		var count int
		var checkReason string
		var checkCauses []NuGetCause
		if ecosystem == "nuget" {
			check, count, checkReason, checkCauses = compareNuGet(rec, nugetE, comparisonLock, limits.PackageNames-r.Coverage.PackageNames)
		} else {
			check, count, checkReason = compare(ecosystem, rec, comparisonLock, limits.PackageNames-r.Coverage.PackageNames)
		}
		if workspaceRoot != "" {
			check.Explanation = strings.Replace(check.Explanation, "the root package entry's direct tables", "the selected workspace member package entry's direct tables", 1)
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if ecosystem == "npm" && in.Declarations.Coverage.OmittedDiagnostics > 0 {
			check.Status = "indeterminate"
			check.Explanation = "Declaration diagnostics were omitted, so the complete direct package table cannot be established."
			ctxResult.Boundaries = append(ctxResult.Boundaries, Boundary{Path: manifest, Reason: "npm-manifest-declarations-unresolved"})
		} else if ecosystem == "npm" && npm.hasDiagnostic(manifest, npmComparisonDiagnostics...) {
			check.Status = "indeterminate"
			check.Explanation += " The selected package manifest has dependency-field diagnostics, so its complete direct declaration table is unknown."
			ctxResult.Boundaries = append(ctxResult.Boundaries, Boundary{Path: manifest, Reason: "npm-manifest-declarations-unresolved"})
		}
		if selectedNPMShrinkwrap {
			check.Explanation += " Shrinkwrap selection follows npm v11 static precedence; npm v12 ignores project npm-shrinkwrap.json files, so this does not establish which lockfile npm v12 would use."
		}
		if len(checkCauses) > 0 {
			nugetCauses = append(nugetCauses, checkCauses...)
			seen := map[string]bool{}
			for _, cause := range checkCauses {
				if seen[cause.Reason] {
					continue
				}
				seen[cause.Reason] = true
				ctxResult.Boundaries = append(ctxResult.Boundaries, Boundary{Path: cause.Path, Reason: cause.Reason, Detail: cause.Detail})
			}
		}
		if ecosystem == "nuget" {
			checkReasons = nugetCheckReasons(rec, check, checkReason, checkCauses)
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
		appendContext()
	}

	sort.Slice(r.Contexts, func(i, j int) bool {
		if r.Contexts[i].ManifestPath == r.Contexts[j].ManifestPath {
			return r.Contexts[i].Ecosystem < r.Contexts[j].Ecosystem
		}
		return r.Contexts[i].ManifestPath < r.Contexts[j].ManifestPath
	})
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := enforceOutputLimit(r, limits.OutputBytes); err != nil {
		return nil, err
	}
	if err := ValidateReport(r); err != nil {
		return nil, fmt.Errorf("validate lockfile report: %w", err)
	}
	return r, nil
}

func isMSBuildProjectRecord(ext string) bool {
	switch strings.ToLower(ext) {
	case ".csproj", ".fsproj", ".vbproj", ".vcxproj", ".sqlproj", ".wixproj", ".shproj", ".proj":
		return true
	default:
		return false
	}
}

// npmComparisonDiagnostics leave a manifest's complete direct dependency
// table unknown.
var npmComparisonDiagnostics = []string{"invalid-npm-manifest", "invalid-npm-field", "invalid-npm-dependency", "unsupported-npm-dependency", "unsupported-npm-workspace-dependency", "unsupported-npm-workspaces", "unsupported-npm-workspace-pattern", "unsupported-npm-workspace-field", "npm-workspace-match-limit"}

// npmMembershipDiagnostics leave a workspace root's member list unknown, or
// make npm itself reject the workspace (duplicate member names). Fields that
// npm ignores, such as Yarn's nohoist, do not affect membership.
var npmMembershipDiagnostics = []string{"unsupported-npm-workspaces", "unsupported-npm-workspace-pattern", "npm-workspace-match-limit", "unsupported-npm-workspace-identity", "duplicate-npm-workspace-name", "npm-resolution-limit"}

type parsedLock struct {
	state              string
	reason             string
	version            string
	npmRoot            map[string]map[string]string
	npmPackages        map[string]any
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
		deps, reason := npmDirectTables(entry)
		if reason != "" {
			return parsedLock{state: "unsupported", reason: reason, version: strconv.Itoa(v)}
		}
		return parsedLock{state: "supported", version: strconv.Itoa(v), npmRoot: deps, npmPackages: packages}
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
			targetNames := make(map[string]bool, len(target))
			for name, raw := range target {
				foldedName := strings.ToLower(name)
				if targetNames[foldedName] {
					return parsedLock{state: "unsupported", reason: "duplicate-nuget-package-id", version: strconv.Itoa(v)}
				}
				targetNames[foldedName] = true
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
					direct[foldedName] = true
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

func npmDirectTables(entry map[string]any) (map[string]map[string]string, string) {
	deps := map[string]map[string]string{}
	for _, field := range npmDependencyFields {
		raw, present := entry[field]
		if !present {
			continue
		}
		m, ok := raw.(map[string]any)
		if !ok {
			return nil, "invalid-npm-direct-table"
		}
		d := make(map[string]string, len(m))
		for name, value := range m {
			text, ok := value.(string)
			if !ok || !safeNPMReportName(name) {
				return nil, "invalid-npm-direct-entry"
			}
			d[name] = text
		}
		deps[field] = d
	}
	return deps, ""
}

// npmMember returns the direct tables of one workspace member descriptor.
// Only the selected member's descriptor is interpreted; other packages
// entries do not affect its comparison or the package-name budget.
func (p parsedLock) npmMember(location string) (map[string]map[string]string, string, string) {
	raw, found := p.npmPackages[location]
	if !found {
		return nil, "indeterminate", "npm-workspace-lock-member-entry-missing"
	}
	entry, ok := raw.(map[string]any)
	if !ok {
		return nil, "unsupported", "invalid-npm-package-descriptor"
	}
	deps, reason := npmDirectTables(entry)
	if reason != "" {
		return nil, "unsupported", reason
	}
	return deps, "supported", ""
}

// NuGet's conventional lockfile names are case-insensitive on common
// case-insensitive filesystems. Preserve case-variant candidates so they do
// not turn into a false "missing" result; association decides whether the
// spelling is strong enough to claim ownership.
func isNuGetLockCandidateName(base string) bool {
	if strings.EqualFold(base, "packages.lock.json") {
		return true
	}
	const prefix, suffix = "packages.", ".lock.json"
	return len(base) > len(prefix)+len(suffix) &&
		strings.EqualFold(base[:len(prefix)], prefix) &&
		strings.EqualFold(base[len(base)-len(suffix):], suffix)
}

func safeNPMReportName(value string) bool {
	if value == "" || len(value) > 256 || !utf8.ValidString(value) {
		return false
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}

var npmDependencyFields = []string{"dependencies", "devDependencies", "optionalDependencies", "peerDependencies"}

func compare(ecosystem string, rec declarations.ProjectRecord, lock parsedLock, remainingNames int) (Check, int, string) {
	if remainingNames <= 0 {
		return Check{Name: checkName(ecosystem), Status: "indeterminate", Explanation: "The package-name comparison budget was exhausted."}, 0, "package-name-limit"
	}
	switch ecosystem {
	case "npm":
		return compareNPM(rec, lock, remainingNames)
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
		name, value, ok := splitNPMDeclaration(ref.Value)
		if !ok || ref.Condition == "" {
			unknown = true
			continue
		}
		if manifest[ref.Condition] == nil {
			manifest[ref.Condition] = map[string]string{}
		}
		if previous, exists := manifest[ref.Condition][name]; exists && previous != value {
			unknown = true
			continue
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
		for _, name := range sortedStringMapKeys(declared) {
			value := declared[name]
			count++
			if count > maxNames {
				c.Status = "indeterminate"
				sortCheckNames(&c)
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
		for _, name := range sortedStringMapKeys(locked) {
			if _, ok := declared[name]; !ok {
				count++
				if count > maxNames {
					c.Status = "indeterminate"
					sortCheckNames(&c)
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
	sortCheckNames(&c)
	return c, count, ""
}

func splitNPMDeclaration(value string) (name, spec string, ok bool) {
	separator := -1
	if strings.HasPrefix(value, "@") {
		slash := strings.IndexByte(value, '/')
		if slash <= 1 {
			return "", "", false
		}
		relative := strings.IndexByte(value[slash+1:], '@')
		if relative < 0 {
			return "", "", false
		}
		separator = slash + 1 + relative
	} else {
		separator = strings.IndexByte(value, '@')
	}
	if separator <= 0 || separator == len(value)-1 {
		return "", "", false
	}
	name, spec = value[:separator], value[separator+1:]
	if !safeNPMName(name) || !safeNPMRange(spec) {
		return "", "", false
	}
	return name, spec, true
}

func safeNPMName(value string) bool {
	if len(value) > 214 || value == "" {
		return false
	}
	if strings.HasPrefix(value, "@") {
		scope, name, ok := strings.Cut(value[1:], "/")
		return ok && safeNPMIdentifier(scope) && safeNPMIdentifier(name)
	}
	return safeNPMIdentifier(value)
}

func safeNPMIdentifier(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("-_.", r)) {
			return false
		}
	}
	return true
}

func safeNPMRange(value string) bool {
	if len(value) > 1024 || value == "" {
		return false
	}
	for _, r := range value {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune(".-_+*^~<>=| ", r)) {
			return false
		}
	}
	return true
}

func sortedStringMapKeys[V any](values map[string]V) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return keys
}

func sortCheckNames(c *Check) {
	slices.Sort(c.Missing)
	slices.Sort(c.Mismatched)
	slices.Sort(c.Unexpected)
}

// association is one project's lockfile ownership decision. reasonPath names
// the boundary evidence when it is not the selected lockfile.
type association struct {
	lockPath      string
	workspaceRoot string
	state         string
	reason        string
	reasonPath    string
	detail        string
	extra         []Boundary
}

func (a *association) override(state, reason string) {
	a.state, a.reason, a.reasonPath, a.detail, a.workspaceRoot = state, reason, "", "", ""
}

// npmResolver decides which npm lockfile, if any, npm would use for a
// selected package.json. Without workspace mode an ancestor npm lockfile
// keeps the historical indeterminate result. Workspace mode follows npm's
// workspace-root selection: the nearest ancestor package.json whose
// workspaces include the project owns it, and ancestors that do not list it
// are skipped.
type npmResolver struct {
	workspace          bool
	manifests          map[string]declarations.ProjectRecord
	locks              map[string][]string
	markers            map[string][]string
	diagnostics        map[string][]string
	omissions          omissionScope
	omittedDiagnostics bool
}

func newNPMResolver(in Input, records []declarations.ProjectRecord, locks, markers map[string][]string, omissions omissionScope) *npmResolver {
	r := &npmResolver{workspace: in.WorkspaceLocks, manifests: map[string]declarations.ProjectRecord{}, locks: locks, markers: markers, diagnostics: map[string][]string{}, omissions: omissions, omittedDiagnostics: in.Declarations.Coverage.OmittedDiagnostics > 0}
	for _, rec := range records {
		if rec.Project.Kind == "npm" && path.Base(rec.Project.ID) == "package.json" {
			r.manifests[rec.Project.Root] = rec
		}
	}
	for _, diagnostic := range in.Declarations.Diagnostics {
		r.diagnostics[diagnostic.Path] = append(r.diagnostics[diagnostic.Path], diagnostic.Code)
	}
	return r
}

func (r *npmResolver) hasDiagnostic(manifest string, codes ...string) bool {
	return slices.ContainsFunc(r.diagnostics[manifest], func(code string) bool { return slices.Contains(codes, code) })
}

// ancestors returns the selected npm manifests above root, nearest first.
func (r *npmResolver) ancestors(root string) []declarations.ProjectRecord {
	var out []declarations.ProjectRecord
	for dir := root; dir != "."; {
		dir = path.Dir(dir)
		if rec, ok := r.manifests[dir]; ok {
			out = append(out, rec)
		}
	}
	return out
}

func (r *npmResolver) associate(rec declarations.ProjectRecord) association {
	if !rec.Parsed || !rec.Complete {
		return association{state: "indeterminate", reason: "project-declarations-incomplete"}
	}
	root := rec.Project.Root
	if manager := declaredPackageManager(rec); manager != "" && manager != "npm" {
		return association{state: "unsupported", reason: "npm-alternative-package-manager", reasonPath: rec.Project.ID, detail: "The project declares packageManager " + manager + "."}
	}
	ancestors := r.ancestors(root)
	notShared := false
	if r.workspace {
		owner, why := r.workspaceOwner(rec, ancestors)
		if why != "" {
			return association{state: "indeterminate", reason: why}
		}
		if owner != nil {
			return r.associateMember(rec, *owner, ancestors)
		}
		notShared = true
	}
	if lock := npmLockAt(r.locks[root]); lock != "" {
		return association{lockPath: lock, state: "observed"}
	}
	if a, ok := alternativeAt(r.markers[root]); ok {
		return a
	}
	var extra []Boundary
	if nearest := r.nearestAncestorLock(root); nearest != "" {
		// Without workspace evaluation an ancestor lockfile is not associated by
		// path alone; guessing could compare a member against the wrong root.
		if !notShared {
			return association{state: "indeterminate", reason: "workspace-lockfile-owner-unresolved"}
		}
		extra = append(extra, Boundary{Path: nearest, Reason: "npm-ancestor-lock-not-shared"})
	}
	// Another manager above the project cannot give it something to lock.
	if !hasDirectDeclarations(rec, "npm") {
		if r.declarationsUnresolved(rec) {
			return association{state: "indeterminate", reason: "npm-manifest-declarations-unresolved", reasonPath: rec.Project.ID, extra: extra}
		}
		return association{state: "not_applicable", extra: extra}
	}
	if evidence := r.ancestorAlternative(rec, ancestors); evidence != "" {
		return association{state: "indeterminate", reason: "npm-ancestor-alternative-package-manager", reasonPath: evidence, extra: extra}
	}
	return association{state: missingState(rec), reason: "lockfile-not-present", extra: extra}
}

// workspaceOwner returns the nearest ancestor whose workspaces include rec.
// A non-empty reason means a selected ancestor could list rec, or npm could
// reject the workspace, but the declarations do not establish which.
func (r *npmResolver) workspaceOwner(rec declarations.ProjectRecord, ancestors []declarations.ProjectRecord) (*declarations.ProjectRecord, string) {
	root := rec.Project.Root
	// A project at the selected root has no ancestor an omission could hide.
	if root != "." && r.omissions.blocks(isPackageJSON, func(dir string) bool { return isAncestor(dir, root) }) {
		return nil, "npm-workspace-lock-owner-incomplete"
	}
	if len(ancestors) > 0 && r.omittedDiagnostics {
		return nil, "npm-workspace-lock-owner-incomplete"
	}
	for _, ancestor := range ancestors {
		if !ancestor.Parsed || !ancestor.Complete || r.hasDiagnostic(ancestor.Project.ID, npmMembershipDiagnostics...) {
			return nil, "npm-workspace-lock-owner-incomplete"
		}
		listed := 0
		for _, ref := range ancestor.Project.References {
			// npm skips a listed path that has no package.json, so a
			// definitely absent member cannot be rec or make npm reject the
			// workspace.
			if ref.Kind != "npm-workspace-member" || ref.Evidence != ancestor.Project.ID || ref.State == "missing" {
				continue
			}
			if ref.State != "resolved" {
				return nil, "npm-workspace-lock-membership-unresolved"
			}
			if ref.Target == rec.Project.ID {
				listed++
			}
		}
		if listed == 0 {
			continue
		}
		if listed > 1 {
			return nil, "npm-workspace-lock-membership-unresolved"
		}
		// npm rejects a workspace whose members share a name, so an omitted
		// manifest beneath the owner leaves that check unknown.
		if r.omissions.blocks(isPackageJSON, func(dir string) bool { return isAncestor(ancestor.Project.Root, dir) }) {
			return nil, "npm-workspace-lock-owner-incomplete"
		}
		return &ancestor, ""
	}
	return nil, ""
}

func isPackageJSON(base string) bool { return base == "package.json" }

func (r *npmResolver) associateMember(rec, owner declarations.ProjectRecord, ancestors []declarations.ProjectRecord) association {
	// npm uses the outer root for a member that declares its own workspaces
	// but does not install that member's workspaces.
	if declaresNPMWorkspace(rec) {
		return association{state: "indeterminate", reason: "nested-npm-workspace-owner-unresolved", reasonPath: owner.Project.ID}
	}
	ownerRoot := owner.Project.Root
	if manager, declarer := nearestPackageManager(rec, ancestors); manager != "" && manager != "npm" {
		// Corepack applies the nearest declaration. Within the workspace it
		// names the workspace's manager; above it, the owner is uncertain.
		if declarer.Project.Root == ownerRoot || isAncestor(ownerRoot, declarer.Project.Root) {
			return association{state: "unsupported", reason: "npm-alternative-package-manager", reasonPath: declarer.Project.ID, detail: "The workspace declares packageManager " + manager + "."}
		}
		return association{state: "indeterminate", reason: "npm-ancestor-alternative-package-manager", reasonPath: declarer.Project.ID}
	}
	if lock := npmLockAt(r.locks[ownerRoot]); lock != "" {
		return association{lockPath: lock, workspaceRoot: ownerRoot, state: "observed"}
	}
	if a, ok := alternativeAt(r.markers[ownerRoot]); ok {
		return a
	}
	state := "missing"
	if !hasDirectDeclarations(rec, "npm") {
		if r.declarationsUnresolved(rec) {
			return association{state: "indeterminate", reason: "npm-manifest-declarations-unresolved", reasonPath: rec.Project.ID}
		}
		state = "not_applicable"
	}
	return association{state: state, reason: "npm-workspace-root-lockfile-not-present", reasonPath: owner.Project.ID}
}

// declarationsUnresolved reports whether rec's dependency fields could not all
// be read, so the absence of direct declarations is not established either.
func (r *npmResolver) declarationsUnresolved(rec declarations.ProjectRecord) bool {
	return r.omittedDiagnostics || r.hasDiagnostic(rec.Project.ID, npmComparisonDiagnostics...)
}

func (r *npmResolver) nearestAncestorLock(root string) string {
	for dir := root; dir != "."; {
		dir = path.Dir(dir)
		if lock := npmLockAt(r.locks[dir]); lock != "" {
			return lock
		}
	}
	return ""
}

// ancestorAlternative names ancestor evidence that another package manager
// may own the project. Workspace mode has already evaluated package.json
// workspaces, which Yarn and Bun share with npm, so an ancestor lockfile in any
// format matters only through that ownership. pnpm and Rush define membership
// in pnpm-workspace.yaml and rush.json, which remain unresolved there.
func (r *npmResolver) ancestorAlternative(rec declarations.ProjectRecord, ancestors []declarations.ProjectRecord) string {
	for dir := rec.Project.Root; dir != "."; {
		dir = path.Dir(dir)
		for _, marker := range r.markers[dir] {
			if !r.workspace || !isAlternativeLockName(path.Base(marker)) {
				return marker
			}
		}
	}
	if manager, declarer := nearestPackageManager(rec, ancestors); manager != "" && manager != "npm" {
		return declarer.Project.ID
	}
	return ""
}

// nearestPackageManager returns the packageManager declaration Corepack
// would apply to rec: its own, or the nearest ancestor's.
func nearestPackageManager(rec declarations.ProjectRecord, ancestors []declarations.ProjectRecord) (string, declarations.ProjectRecord) {
	if manager := declaredPackageManager(rec); manager != "" {
		return manager, rec
	}
	for _, ancestor := range ancestors {
		if manager := declaredPackageManager(ancestor); manager != "" {
			return manager, ancestor
		}
	}
	return "", declarations.ProjectRecord{}
}

// alternativeAt reports another package manager's lockfile or workspace file
// in the directory npm would otherwise use.
func alternativeAt(markers []string) (association, bool) {
	for _, marker := range markers {
		if isAlternativeLockName(path.Base(marker)) {
			return association{lockPath: marker, state: "unsupported", reason: "npm-alternative-lockfile-format"}, true
		}
	}
	if len(markers) > 0 {
		return association{state: "unsupported", reason: "npm-alternative-package-manager", reasonPath: markers[0]}, true
	}
	return association{}, false
}

func isAlternativeLockName(base string) bool {
	return base == "yarn.lock" || base == "pnpm-lock.yaml" || base == "bun.lock" || base == "bun.lockb"
}

// IsAlternativeNPMMarker matches files that place a package.json under
// another package manager's lockfile or workspace definition.
func IsAlternativeNPMMarker(base string) bool {
	return isAlternativeLockName(base) || base == "pnpm-workspace.yaml" || base == "rush.json"
}

func declaresNPMWorkspace(rec declarations.ProjectRecord) bool {
	for _, req := range rec.Project.Requirements {
		if req.Kind == "npm-workspace-root" && req.State == "declared" {
			return true
		}
	}
	return false
}

// declaredPackageManager returns the name from a valid packageManager
// declaration. npm ignores the field and invalid values carry no name, so
// they are not evidence of another manager.
func declaredPackageManager(rec declarations.ProjectRecord) string {
	for _, req := range rec.Project.Requirements {
		if req.Kind == "package-manager" {
			name, _, _ := strings.Cut(req.Value, "@")
			return name
		}
	}
	return ""
}

// omissionScope records which manifests and unreadable directories the
// declaration collector omitted, so an omission blocks only the ownership
// decisions it could change.
type omissionScope struct {
	declarations bool
	attributed   bool
	paths        []string
	trees        []string
}

func newOmissionScope(in Input) omissionScope {
	s := omissionScope{declarations: in.Declarations.Status != "" && in.Declarations.Status != "skipped"}
	omitted := in.Declarations.Coverage.OmittedFiles
	if omitted == 0 {
		s.attributed = true
		return s
	}
	if !in.DeclarationOmissionsAttributed || int64(len(in.DeclarationOmissions)+len(in.DeclarationOmittedTrees)) > omitted {
		return s
	}
	for _, list := range []struct {
		in  []string
		out *[]string
	}{{in.DeclarationOmissions, &s.paths}, {in.DeclarationOmittedTrees, &s.trees}} {
		for _, p := range list.in {
			// Only a manifest can change an ownership decision.
			if list.out == &s.paths && !declarations.IsManifest(p) {
				continue
			}
			if !in.WorkspaceLocks {
				p = strings.ReplaceAll(p, "\\", "/")
			}
			clean, ok := cleanSelectedRelative(p, in.WorkspaceLocks)
			if !ok {
				return omissionScope{declarations: s.declarations}
			}
			*list.out = append(*list.out, clean)
		}
	}
	s.attributed = true
	return s
}

// blocks reports whether an omitted file whose base name satisfies name, in a
// directory that satisfies dir, could exist. An unreadable directory can hide
// files at any depth, but the projects and workspace roots being compared lie
// outside it, so testing the directory itself is enough for every predicate
// used here.
func (s omissionScope) blocks(name, dir func(string) bool) bool {
	if !s.attributed || slices.ContainsFunc(s.trees, dir) {
		return true
	}
	return slices.ContainsFunc(s.paths, func(p string) bool { return name(path.Base(p)) && dir(path.Dir(p)) })
}

// nugetOwnersKnown reports whether every MSBuild project in root was retained,
// so a conventional lockfile name maps to a known set of projects.
func (s omissionScope) nugetOwnersKnown(root string) bool {
	return s.declarations && !s.blocks(func(base string) bool { return isMSBuildProjectRecord(path.Ext(base)) }, func(dir string) bool { return dir == root })
}

// IsNuGetLockProject matches SDK-style project files whose restore can write
// packages.lock.json.
func IsNuGetLockProject(manifest string) bool {
	switch strings.ToLower(path.Ext(manifest)) {
	case ".csproj", ".fsproj", ".vbproj":
		return true
	default:
		return false
	}
}

// npmLockAt returns the lockfile npm uses in one directory, or "".
// npm-shrinkwrap.json takes precedence over package-lock.json. Inventory paths
// are unique after cleaning, so a directory holds at most one of each.
func npmLockAt(paths []string) string {
	lock := ""
	for _, p := range paths {
		switch path.Base(p) {
		case "npm-shrinkwrap.json":
			return p
		case "package-lock.json":
			lock = p
		}
	}
	return lock
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
	if p == "" || !validText(p, 8192) || strings.Contains(p, "\\") || path.IsAbs(p) {
		return "", false
	}
	c := path.Clean(p)
	if c == "." || c == ".." || strings.HasPrefix(c, "../") || c != p {
		return "", false
	}
	return c, true
}

func cleanSelectedRelative(p string, preservePOSIXBackslash bool) (string, bool) {
	if !preservePOSIXBackslash {
		return cleanRelative(p)
	}
	if p == "" || !validText(p, 8192) || path.IsAbs(p) {
		return "", false
	}
	c := path.Clean(p)
	if c == "." || c == ".." || strings.HasPrefix(c, "../") || c != p {
		return "", false
	}
	return c, true
}

func cleanProjectPathsMode(id, root string, preservePOSIXBackslash bool) (string, string, bool) {
	if !preservePOSIXBackslash {
		id = strings.ReplaceAll(id, "\\", "/")
		root = strings.ReplaceAll(root, "\\", "/")
	}
	cleanID, ok := cleanSelectedRelative(id, preservePOSIXBackslash)
	if !ok {
		return "", "", false
	}
	cleanRoot := root
	if cleanRoot != "." {
		cleanRoot, ok = cleanSelectedRelative(cleanRoot, preservePOSIXBackslash)
		if !ok {
			return "", "", false
		}
	}
	if path.Dir(cleanID) != cleanRoot {
		return "", "", false
	}
	return cleanID, cleanRoot, true
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
		return "Only NuGet packages.lock.json format versions 1 and 2 are supported; v2 Project and CentralTransitive records remain outside the direct-package presence check."
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
