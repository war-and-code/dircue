package lockfiles

import (
	"encoding/json"
	"errors"
	"path"
	"slices"
	"strings"
	"unicode/utf8"
)

// ValidateReport validates the bounded wire contract emitted by Analyze.
// It checks report-internal consistency only; it does not authenticate source
// bytes or re-evaluate the manifest/lockfile comparison.
func ValidateReport(r *Report) error {
	if r == nil {
		return errors.New("lockfile report is required")
	}
	legacy := r.ProviderVersion == LegacyProviderVersion
	current := r.ProviderVersion == ProviderVersion
	if r.Provider != Provider || (!legacy && !current) || !validStatus(r.Status) ||
		(r.Source != "git" && r.Source != "directory") || (r.Source == "git") != (r.Tree != "") || !validText(r.Tree, 64) {
		return errors.New("lockfile report identity is invalid")
	}
	if r.Source == "git" && !validGitTree(r.Tree) {
		return errors.New("lockfile report tree is invalid")
	}
	workspacePaths := len(r.Semantics) == 3 && r.Semantics[2] == npmWorkspaceSemantics
	if (len(r.Semantics) != 2 && !workspacePaths) || r.Semantics[0] != npmSemantics || r.Semantics[1] != nugetSemantics {
		return errors.New("lockfile report semantics are invalid")
	}
	l := r.Limits
	if l.InventoryPaths < 1 || l.InventoryPaths > DefaultMaxInventoryPaths || l.Lockfiles < 1 || l.Lockfiles > DefaultMaxLockfiles ||
		l.FileBytes < 1 || l.FileBytes > DefaultMaxFileBytes || l.InputBytes < 1 || l.InputBytes > DefaultMaxInputBytes ||
		l.PackageNames < 1 || l.PackageNames > DefaultMaxPackageNames || l.Contexts < 1 || l.Contexts > DefaultMaxContexts ||
		l.OutputBytes < 1 || l.OutputBytes > DefaultMaxOutputBytes {
		return errors.New("lockfile report limits are invalid")
	}
	c := r.Coverage
	if c.InventoryPaths < 0 || c.InventoryPaths > l.InventoryPaths || c.ProjectRecords < 0 || c.ProjectRecords > DefaultMaxInventoryPaths ||
		c.LockCandidates < 0 || c.LockCandidates > c.InventoryPaths || c.LockfilesRead < 0 || c.LockfilesRead > c.LockCandidates || c.LockfilesRead > l.Lockfiles ||
		c.InputBytes < 0 || c.InputBytes > l.InputBytes || c.PackageNames < 0 || c.PackageNames > l.PackageNames ||
		c.OmittedFiles < 0 || c.OmittedContexts < 0 || len(r.Contexts) > l.Contexts || c.OmittedContexts > c.ProjectRecords-len(r.Contexts) {
		return errors.New("lockfile report coverage is inconsistent")
	}
	if r.Status == "complete" && (c.OmittedFiles != 0 || c.OmittedContexts != 0 || len(r.Diagnostics) != 0) {
		return errors.New("complete lockfile report contains omissions or diagnostics")
	}
	if r.Status == "skipped" && len(r.Contexts) != 0 {
		return errors.New("skipped lockfile report contains contexts")
	}
	hasObservedAssociation := false
	seenProjectIDs := make(map[string]bool, len(r.Contexts))
	seenManifestPaths := make(map[string]bool, len(r.Contexts))
	for _, ctx := range r.Contexts {
		if !requiredText(ctx.ProjectID, 8192) || !validSelectedRelative(ctx.ProjectID, workspacePaths) || !requiredText(ctx.ManifestPath, 8192) || !validSelectedRelative(ctx.ManifestPath, workspacePaths) || ctx.ProjectID != ctx.ManifestPath ||
			(ctx.Ecosystem != "npm" && ctx.Ecosystem != "nuget") || !validAssociation(ctx.AssociationState) || !validText(ctx.LockfilePath, 8192) || !validText(ctx.LockfileVersion, 128) {
			return errors.New("lockfile context identity is invalid")
		}
		if seenProjectIDs[ctx.ProjectID] || seenManifestPaths[ctx.ManifestPath] {
			return errors.New("lockfile report contains duplicate project contexts")
		}
		seenProjectIDs[ctx.ProjectID] = true
		seenManifestPaths[ctx.ManifestPath] = true
		if ctx.Ecosystem == "npm" && path.Base(ctx.ManifestPath) != "package.json" || ctx.Ecosystem == "nuget" && !IsNuGetLockProject(ctx.ManifestPath) {
			return errors.New("lockfile context manifest does not match its ecosystem")
		}
		if ctx.LockfilePath != "" && !validSelectedRelative(ctx.LockfilePath, workspacePaths) {
			return errors.New("lockfile context path is invalid")
		}
		if ctx.AssociationState == "observed" && (ctx.LockfilePath == "" || !validLockfileVersion(ctx.Ecosystem, ctx.LockfileVersion)) {
			return errors.New("observed lockfile association lacks a selected path or supported version")
		}
		hasObservedAssociation = hasObservedAssociation || ctx.AssociationState == "observed"
		if ctx.AssociationState == "missing" && ctx.LockfilePath != "" {
			return errors.New("missing lockfile association contains a lockfile path")
		}
		if r.Status == "complete" && (ctx.AssociationState == "indeterminate" || ctx.AssociationState == "unsupported") {
			return errors.New("complete lockfile report contains an indeterminate association")
		}
		if len(ctx.Checks) > 1 || ctx.AssociationState == "observed" && len(ctx.Checks) != 1 {
			return errors.New("lockfile context check count is invalid")
		}
		if ctx.Ecosystem == "nuget" && current {
			if !validNuGetEvidence(ctx.NuGetEvidence, ctx, workspacePaths) {
				return errors.New("NuGet evidence is invalid")
			}
		} else if ctx.Ecosystem == "nuget" && legacy && ctx.NuGetEvidence != nil {
			return errors.New("legacy NuGet context contains newer evidence")
		} else if ctx.Ecosystem == "npm" && ctx.NuGetEvidence != nil {
			return errors.New("npm context contains NuGet evidence")
		}
		for _, check := range ctx.Checks {
			if !validCheck(ctx.Ecosystem, check, l.PackageNames) {
				return errors.New("lockfile check is invalid")
			}
			if r.Status == "complete" && check.Status == "indeterminate" {
				return errors.New("complete lockfile report contains indeterminate evidence")
			}
		}
		for _, boundary := range ctx.Boundaries {
			if !validText(boundary.Path, 8192) || boundary.Path != "" && !validSelectedRelative(boundary.Path, workspacePaths) || !requiredText(boundary.Reason, 256) || !validText(boundary.Detail, 8192) {
				return errors.New("lockfile boundary is invalid")
			}
		}
	}
	if hasObservedAssociation && c.LockfilesRead == 0 {
		return errors.New("observed lockfile association lacks a successful selected read")
	}
	for _, diagnostic := range r.Diagnostics {
		if !requiredText(diagnostic.Path, 8192) || diagnostic.Path != "." && !validSelectedRelative(diagnostic.Path, workspacePaths) || !requiredText(diagnostic.Code, 256) || !requiredText(diagnostic.Message, 8192) {
			return errors.New("lockfile diagnostic is invalid")
		}
	}
	encoded, err := json.Marshal(r)
	if err != nil || len(encoded) > l.OutputBytes {
		return errors.New("lockfile report exceeds output byte limit")
	}
	return nil
}

func validNuGetEvidence(e *NuGetEvidence, ctx Context, workspacePaths bool) bool {
	if e == nil || (e.PresenceState != "observed" && e.PresenceState != "not_observed" && e.PresenceState != "unknown") ||
		!validAssociation(e.OwnershipState) || e.CandidateCount < 0 || e.CandidateCount > DefaultMaxInventoryPaths ||
		len(e.CandidatePaths) > nugetCandidatePathLimit || e.OmittedCandidatePaths < 0 || e.OmittedCandidatePaths != e.CandidateCount-len(e.CandidatePaths) ||
		len(e.PresenceReasons) > 32 || len(e.OwnershipReasons) > 32 {
		return false
	}
	if e.OwnershipState != ctx.AssociationState && !validNuGetContentQualifiedOwnership(e, ctx) {
		return false
	}
	if e.PresenceState == "observed" && e.CandidateCount == 0 || e.PresenceState != "observed" && e.CandidateCount != 0 {
		return false
	}
	last := ""
	for _, p := range e.CandidatePaths {
		if !validSelectedRelative(p, workspacePaths) || p <= last {
			return false
		}
		last = p
	}
	for _, list := range [][]string{e.PresenceReasons, e.OwnershipReasons} {
		for _, reason := range list {
			if !requiredText(reason, 256) {
				return false
			}
		}
	}
	return true
}

func validNuGetContentQualifiedOwnership(e *NuGetEvidence, ctx Context) bool {
	reason := ctx.OutcomeReason()
	if e.OwnershipState != "observed" || (ctx.AssociationState != "unsupported" && ctx.AssociationState != "indeterminate") || ctx.LockfilePath == "" {
		return false
	}
	if !slices.Contains(e.CandidatePaths, ctx.LockfilePath) && e.OmittedCandidatePaths == 0 &&
		!(reason == "lockfile-unreadable" && e.PresenceState != "observed") {
		return false
	}
	switch reason {
	case "lockfile-limit", "lockfile-unreadable", "file-read-error", "incomplete-lockfile-read", "invalid-lockfile-json", "duplicate-nuget-package-id",
		"unsupported-nuget-lockfile-version", "nuget-targets-missing", "invalid-nuget-target", "invalid-nuget-package-entry",
		"nuget-package-type-missing", "unsupported-nuget-package-type":
		return true
	default:
		return false
	}
}

func validLockfileVersion(ecosystem, value string) bool {
	switch ecosystem {
	case "npm":
		return value == "2" || value == "3"
	case "nuget":
		return value == "1" || value == "2"
	default:
		return false
	}
}

func validCheck(ecosystem string, c Check, maxNames int) bool {
	wantName := checkName(ecosystem)
	if !requiredText(c.Name, 256) || c.Name != wantName || !requiredText(c.Explanation, 8192) || c.Compared < 0 || c.Compared > maxNames ||
		(c.Status != "match" && c.Status != "different" && c.Status != "indeterminate" && c.Status != "not_applicable") ||
		len(c.Missing) > maxNames || len(c.Mismatched) > maxNames || len(c.Unexpected) > maxNames {
		return false
	}
	for _, values := range [][]string{c.Missing, c.Mismatched, c.Unexpected} {
		for _, value := range values {
			if !validText(value, 256) {
				return false
			}
		}
	}
	if c.Status == "match" && (len(c.Missing)+len(c.Mismatched)+len(c.Unexpected) != 0) || c.Status == "not_applicable" && c.Compared != 0 {
		return false
	}
	if c.Status == "different" && len(c.Missing)+len(c.Mismatched)+len(c.Unexpected) == 0 {
		return false
	}
	return true
}

func validAssociation(value string) bool {
	switch value {
	case "observed", "missing", "unsupported", "indeterminate", "not_applicable":
		return true
	default:
		return false
	}
}

func validStatus(value string) bool {
	return value == "complete" || value == "partial" || value == "skipped"
}

func validText(value string, max int) bool {
	return len(value) <= max && utf8.ValidString(value) && !strings.ContainsRune(value, '\x00')
}

func requiredText(value string, max int) bool {
	return value != "" && validText(value, max)
}

func validRelative(value string) bool {
	return value != "" && value != "." && !path.IsAbs(value) && path.Clean(value) == value && value != ".." && !strings.HasPrefix(value, "../") && !strings.Contains(value, "\\") && !strings.ContainsRune(value, '\x00')
}

func validSelectedRelative(value string, preservePOSIXBackslash bool) bool {
	if !preservePOSIXBackslash {
		return validRelative(value)
	}
	return value != "" && value != "." && !path.IsAbs(value) && path.Clean(value) == value && value != ".." && !strings.HasPrefix(value, "../") && !strings.ContainsRune(value, '\x00')
}

func validGitTree(value string) bool {
	if len(value) != 40 {
		return false
	}
	for _, r := range value {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f') {
			return false
		}
	}
	return true
}
