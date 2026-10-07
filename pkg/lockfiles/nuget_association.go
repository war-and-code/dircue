package lockfiles

import (
	"fmt"
	"path"
	"slices"
	"strings"

	"github.com/war-and-code/dircue/pkg/declarations"
)

// associateNuGetStatic decides which lockfile, if any, one project's restore
// would use, from its static evaluation. It returns the association, the
// basis of the lock path, and the causes behind any uncertainty.
func associateNuGetStatic(rec declarations.ProjectRecord, e *nugetEval, claims *nugetClaims, index *nugetFileIndex, omissions omissionScope, selectedComplete bool) (association, string, []NuGetCause) {
	unknown := func(reason string, cause NuGetCause) association {
		return association{state: "indeterminate", reason: reason, reasonPath: cause.Path, detail: cause.Detail}
	}
	if e == nil {
		return association{state: "indeterminate", reason: "nuget-project-config-unresolved"}, "unresolved", nil
	}
	if len(e.invalid) > 0 {
		causes := sortNuGetCauses(e.invalid)
		return unknown("nuget-project-config-unresolved", causes[0]), "unresolved", causes
	}
	values := e.finalLock()
	var causes []NuGetCause
	for _, v := range values {
		if v.cause.Reason != "" {
			causes = append(causes, v.cause)
		}
	}
	if len(e.unmodeled) > 0 {
		first := sortNuGetCauses(e.unmodeled)[0]
		return unknown("nuget-msbuild-input-unmodeled", first), "open_unmodeled", sortNuGetCauses(append(causes, e.unmodeled...))
	}
	causes = sortNuGetCauses(causes)
	for _, v := range values {
		if v.kind == nugetLockOpen {
			return unknown("nuget-custom-lock-path-unresolved", v.cause), "open_evidence", causes
		}
	}
	if len(values) == 1 {
		switch v := values[0]; v.kind {
		case nugetLockDefault:
			return claims.check(rec, associateNuGetDefault(rec, e, claims, index)), "conventional", causes
		case nugetLockLiteral:
			return claims.check(rec, associateNuGetLiteral(rec, e, v.value, index, omissions, selectedComplete)), "custom_literal", causes
		case nugetLockOutside:
			return unknown("nuget-custom-lock-path-unresolved", v.cause), "outside_snapshot", causes
		}
	}
	// Several values remain possible, or one unexpanded pattern. Restore uses
	// one of them; the result is definite only when none exists.
	basis := "conditional"
	outside := false
	var candidates []string
	for _, v := range values {
		switch v.kind {
		case nugetLockDefault:
			candidates = append(candidates, nugetConventionalCandidates(rec, index)...)
		case nugetLockLiteral:
			if index.regular(v.value) {
				candidates = append(candidates, v.value)
			}
			candidates = append(candidates, index.caseVariants(v.value)...)
		case nugetLockPattern:
			basis = "pattern"
			candidates = append(candidates, index.matching(v.value)...)
		case nugetLockOutside:
			outside = true
		}
	}
	slices.Sort(candidates)
	candidates = slices.Compact(candidates)
	if len(candidates) == 0 && !outside && index.complete && rec.Parsed && rec.Complete {
		if !e.mayHavePackages() {
			return association{state: "not_applicable"}, basis, causes
		}
		return association{state: "missing", reason: "lockfile-not-present", detail: "no lockfile exists at any possible NuGetLockFilePath value"}, basis, causes
	}
	cause := NuGetCause{Path: rec.Project.ID}
	if len(causes) > 0 {
		cause = causes[0]
	}
	a := unknown("nuget-lock-path-conditional", cause)
	if len(candidates) > 0 {
		a.detail = nugetDetail(fmt.Sprintf("%d existing possible lockfile(s), including %s", len(candidates), candidates[0]))
	}
	return a, basis, causes
}

// nugetConventionalCandidates lists regular files in the project directory
// that NuGet's default naming could select for this project.
func nugetConventionalCandidates(rec declarations.ProjectRecord, index *nugetFileIndex) []string {
	var out []string
	for _, name := range []string{nugetDefaultName(rec.Project.ID), "packages.lock.json"} {
		candidate := path.Join(rec.Project.Root, name)
		if index.regular(candidate) {
			out = append(out, candidate)
		}
		out = append(out, index.caseVariants(candidate)...)
	}
	return out
}

// associateNuGetDefault applies NuGet's default naming: the project-specific
// packages.<name>.lock.json when it exists, otherwise packages.lock.json.
// Other projects' claims on the selected file are checked by the caller.
func associateNuGetDefault(rec declarations.ProjectRecord, e *nugetEval, claims *nugetClaims, index *nugetFileIndex) association {
	unknown := func(reason, reasonPath string) association {
		return association{state: "indeterminate", reason: reason, reasonPath: reasonPath}
	}
	if !rec.Parsed || !rec.Complete {
		return unknown("project-declarations-incomplete", "")
	}
	root := rec.Project.Root
	specific := path.Join(root, nugetDefaultName(rec.Project.ID))
	generic := path.Join(root, "packages.lock.json")
	for _, candidate := range []string{specific, generic} {
		if index.regular(candidate) {
			if variants := index.caseVariants(candidate); len(variants) > 0 {
				return unknown("nuget-lockfile-case-unresolved", variants[0])
			}
			return association{lockPath: candidate, state: "observed"}
		}
		if variants := index.caseVariants(candidate); len(variants) > 0 {
			return unknown("nuget-lockfile-case-unresolved", variants[0])
		}
		if f, exists := index.files[candidate]; exists && f.NonRegular {
			return unknown("nuget-lockfile-owner-unresolved", candidate)
		}
	}
	// Another lock-named file in the directory is attributable only when it is
	// a sibling project's default project-specific name.
	for _, candidate := range index.byDir[root] {
		base := path.Base(candidate)
		if !isNuGetLockCandidateName(base) || strings.EqualFold(base, "packages.lock.json") {
			continue
		}
		if !slices.ContainsFunc(claims.projects[root], func(m string) bool {
			return m != rec.Project.ID && strings.EqualFold(base, nugetDefaultName(m))
		}) {
			return unknown("nuget-lockfile-owner-unresolved", candidate)
		}
	}
	if !e.mayHavePackages() {
		return association{state: "not_applicable"}
	}
	return association{state: "missing", reason: "lockfile-not-present"}
}

func associateNuGetLiteral(rec declarations.ProjectRecord, e *nugetEval, lockPath string, index *nugetFileIndex, omissions omissionScope, selectedComplete bool) association {
	unknown := func(reason, reasonPath string) association {
		return association{state: "indeterminate", reason: reason, reasonPath: reasonPath}
	}
	if !rec.Parsed || !rec.Complete {
		return unknown("project-declarations-incomplete", "")
	}
	if variants := index.caseVariants(lockPath); len(variants) > 0 {
		return unknown("nuget-lockfile-case-unresolved", variants[0])
	}
	if index.regular(lockPath) {
		// Any omitted MSBuild project could name the same custom path.
		if !selectedComplete || !omissions.nugetCustomOwnersKnown() {
			return unknown("project-inventory-incomplete-association", lockPath)
		}
		return association{lockPath: lockPath, state: "observed"}
	}
	if f, exists := index.files[lockPath]; exists && f.NonRegular {
		return unknown("nuget-custom-lock-path-unresolved", lockPath)
	}
	if !index.complete {
		return unknown("inventory-incomplete", lockPath)
	}
	if !e.mayHavePackages() {
		return association{state: "not_applicable"}
	}
	return association{state: missingState(rec), reason: "lockfile-not-present", reasonPath: lockPath}
}

// check downgrades an observed association when another project's possible
// lock paths include the same file, and names that project.
func (c *nugetClaims) check(rec declarations.ProjectRecord, a association) association {
	if a.state != "observed" {
		return a
	}
	others := c.others(rec.Project.ID, a.lockPath)
	if len(others) == 0 {
		return a
	}
	return association{lockPath: a.lockPath, state: "indeterminate", reason: "ambiguous-nuget-lockfile-owner", reasonPath: others[0],
		detail: nugetDetail(fmt.Sprintf("%d other MSBuild project(s) may use %s, including %s", len(others), a.lockPath, others[0]))}
}

// compareNuGet checks that every PackageReference identity the static
// evaluation keeps occurs as a Direct entry in the lockfile.
func compareNuGet(rec declarations.ProjectRecord, e *nugetEval, lock parsedLock, maxNames int) (Check, int, string, []NuGetCause) {
	c := Check{Name: "nuget-observed-direct-package-presence", Status: "match", Explanation: "Checks that the PackageReference identities kept by a static evaluation of the project and its selected MSBuild imports occur as Direct entries in the lockfile target groups; it does not compare requested/resolved versions, run MSBuild, or establish restore consistency."}
	if maxNames <= 0 {
		c.Status, c.Explanation = "indeterminate", "The package-name comparison budget was exhausted."
		return c, 0, "package-name-limit", nil
	}
	if e == nil {
		c.Status = "indeterminate"
		return c, 0, "nuget-project-config-unresolved", nil
	}
	causes := e.packageCauses()
	refs := e.presentIDs()
	if len(refs) == 0 && len(causes) == 0 {
		c.Status = "not_applicable"
		c.Explanation = "The static evaluation kept no PackageReference identities for this project."
		return c, 0, "", nil
	}
	if len(causes) > 0 {
		c.Explanation += " Some PackageReference items are conditional, computed, or come from MSBuild input outside the static model; check_reasons names them."
	}
	if !rec.Parsed || !rec.Complete || len(causes) > 0 || lock.nugetTarget != 1 {
		c.Status = "indeterminate"
	}
	count := 0
	for _, name := range refs {
		count++
		if count > maxNames {
			c.Status = "indeterminate"
			return c, maxNames, "package-name-limit", causes
		}
		c.Compared++
		if !lock.nugetDirect[name] {
			c.Missing = append(c.Missing, name)
		}
	}
	if len(c.Missing) > 0 && c.Status != "indeterminate" {
		c.Status = "different"
	}
	slices.Sort(c.Missing)
	if len(lock.nugetExtendedKinds) > 0 {
		c.Explanation += " Project/CentralTransitive entries are present but not compared."
		return c, count, "nuget-project-or-central-transitive-unexamined", causes
	}
	if lock.nugetTarget > 1 {
		return c, count, "multiple-target-frameworks", causes
	}
	if !rec.Parsed || !rec.Complete {
		return c, count, "project-declarations-incomplete", causes
	}
	return c, count, "", causes
}

// nugetCheckReasons lists why a completed check is indeterminate, or the
// qualifications attached to a definite result.
func nugetCheckReasons(rec declarations.ProjectRecord, check Check, reason string, causes []NuGetCause) []string {
	out := []string{}
	if reason != "" {
		out = append(out, reason)
	}
	for _, cause := range causes {
		out = append(out, cause.Reason)
	}
	if check.Status == "indeterminate" && (!rec.Parsed || !rec.Complete) {
		out = append(out, "project-declarations-incomplete")
	}
	slices.Sort(out)
	return slices.Compact(out)
}

// makeNuGetEvidence separates presence, ownership, and check results for one
// NuGet context.
func makeNuGetEvidence(rec declarations.ProjectRecord, e *nugetEval, index *nugetFileIndex, ctx Context, ownership, basis string, causes []NuGetCause, checkReasons []string, inventoryComplete bool) *NuGetEvidence {
	ev := &NuGetEvidence{PresenceState: "not_observed", CandidatePaths: []string{}, PresenceReasons: []string{}, OwnershipState: ownership, OwnershipReasons: []string{}, LockPathBasis: basis, CheckReasons: []string{}, Causes: []NuGetCause{}}
	if ev.LockPathBasis == "" {
		ev.LockPathBasis = "unresolved"
	}
	open := false
	var candidates []string
	values := []nugetLockValue{{kind: nugetLockDefault}}
	if e != nil && len(e.invalid) == 0 {
		values = e.finalLock()
	} else {
		open = true
	}
	for _, v := range values {
		switch v.kind {
		case nugetLockDefault, nugetLockUnmodeled, nugetLockOpen:
			for _, candidate := range index.byDir[rec.Project.Root] {
				if isNuGetLockCandidateName(path.Base(candidate)) {
					if index.files[candidate].NonRegular {
						ev.PresenceReasons = append(ev.PresenceReasons, "nuget-candidate-not-regular")
						continue
					}
					candidates = append(candidates, candidate)
				}
			}
			if v.kind != nugetLockDefault {
				open = true
			}
		case nugetLockLiteral:
			if index.regular(v.value) {
				candidates = append(candidates, v.value)
			} else if f, exists := index.files[v.value]; exists && f.NonRegular {
				ev.PresenceReasons = append(ev.PresenceReasons, "nuget-candidate-not-regular")
			}
			candidates = append(candidates, index.caseVariants(v.value)...)
		case nugetLockPattern:
			candidates = append(candidates, index.matching(v.value)...)
		case nugetLockOutside:
			open = true
		}
	}
	if ctx.LockfilePath != "" && index.regular(ctx.LockfilePath) {
		candidates = append(candidates, ctx.LockfilePath)
	}
	slices.Sort(candidates)
	candidates = slices.Compact(candidates)
	ev.CandidateCount = len(candidates)
	if candidates != nil {
		ev.CandidatePaths = candidates
	}
	switch {
	case ev.CandidateCount > 0:
		ev.PresenceState = "observed"
		ev.PresenceReasons = []string{}
	case len(ev.PresenceReasons) > 0 || !inventoryComplete || open:
		ev.PresenceState = "unknown"
		if !inventoryComplete {
			ev.PresenceReasons = append(ev.PresenceReasons, "inventory-incomplete")
		}
		if open {
			reason := "nuget-custom-lock-path-unresolved"
			if e == nil || len(e.invalid) > 0 {
				reason = "nuget-project-config-unresolved"
			} else if len(e.unmodeled) > 0 {
				reason = "nuget-msbuild-input-unmodeled"
			}
			ev.PresenceReasons = append(ev.PresenceReasons, reason)
		}
	}
	slices.Sort(ev.PresenceReasons)
	ev.PresenceReasons = slices.Compact(ev.PresenceReasons)
	if ev.CandidateCount > nugetCandidatePathLimit {
		ev.OmittedCandidatePaths = ev.CandidateCount - nugetCandidatePathLimit
		ev.CandidatePaths = ev.CandidatePaths[:nugetCandidatePathLimit]
	}
	if reason := ctx.OutcomeReason(); ownership != "observed" && ownership != "not_applicable" && reason != "" {
		ev.OwnershipReasons = append(ev.OwnershipReasons, reason)
	} else if ownership != ctx.AssociationState && reason != "" {
		ev.OwnershipReasons = append(ev.OwnershipReasons, reason)
	}
	switch {
	case len(ctx.Checks) == 0:
		ev.CheckState = "not_compared"
		switch {
		case ctx.OutcomeReason() != "":
			ev.CheckReasons = []string{ctx.OutcomeReason()}
		case ownership == "not_applicable":
			ev.CheckReasons = []string{"no-direct-declarations"}
		default:
			ev.CheckReasons = []string{"lockfile-not-associated"}
		}
	default:
		ev.CheckState = ctx.Checks[0].Status
		ev.CheckReasons = slices.Clone(checkReasons)
		if ev.CheckState == "indeterminate" && len(ev.CheckReasons) == 0 && ctx.OutcomeReason() != "" {
			ev.CheckReasons = []string{ctx.OutcomeReason()}
		}
	}
	if ev.CheckReasons == nil {
		ev.CheckReasons = []string{}
	}
	all := sortNuGetCauses(causes)
	if len(all) > nugetMaxCauses {
		ev.OmittedCauses = len(all) - nugetMaxCauses
		all = all[:nugetMaxCauses]
	}
	if all != nil {
		ev.Causes = all
	}
	return ev
}
