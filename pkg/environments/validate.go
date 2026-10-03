package environments

import (
	"encoding/json"
	"errors"
	"path"
	"strings"
)

// ValidateReport rejects decoded reports that are inconsistent or outside the
// same bounded wire form produced by Analyze.
func ValidateReport(r *Report) error {
	if r == nil {
		return errors.New("environment report is required")
	}
	legacy := r.ProviderVersion == LegacyProviderVersion && r.SemanticsReference == legacySemanticsReference
	current := r.ProviderVersion == ProviderVersion && r.SemanticsReference == semanticsReference
	if r.Provider != Provider || (!legacy && !current) {
		return errors.New("environment report identity is invalid")
	}
	if r.Status != "complete" && r.Status != "partial" && r.Status != "skipped" {
		return errors.New("environment report status is invalid")
	}
	if (r.Source != "git" && r.Source != "directory") || (r.Source == "git") != (r.Tree != "") || !wireOptional(r.Tree) {
		return errors.New("environment source identity is invalid")
	}
	if current && r.Source == "git" && !validEnvironmentGitTree(r.Tree) {
		return errors.New("environment source tree is invalid")
	}
	l := r.Limits
	if l.InventoryPaths < 1 || l.InventoryPaths > DefaultMaxInventoryPaths || l.GlobalJSONBytes < 1 || l.GlobalJSONBytes > DefaultMaxGlobalJSONBytes || l.InputBytes < 1 || l.InputBytes > DefaultMaxInputBytes || l.Requirements < 1 || l.Requirements > DefaultMaxRequirements || l.Contexts < 1 || l.Contexts > DefaultMaxContexts || l.OutputBytes < 1 || l.OutputBytes > DefaultMaxOutputBytes {
		return errors.New("environment limits are invalid")
	}
	if current {
		if l.ToolchainFiles < 1 || l.ToolchainFiles > DefaultMaxToolchainFiles || l.ToolchainFileBytes < 1 || l.ToolchainFileBytes > DefaultMaxToolchainFileBytes || l.ToolchainInputBytes < 1 || l.ToolchainInputBytes > DefaultMaxToolchainInputBytes {
			return errors.New("environment toolchain limits are invalid")
		}
	} else if l.ToolchainFiles != 0 || l.ToolchainFileBytes != 0 || l.ToolchainInputBytes != 0 || len(r.ToolchainDeclarations) != 0 {
		return errors.New("legacy environment report contains newer toolchain fields")
	}
	c := r.Coverage
	if c.InventoryPaths < 0 || c.InventoryPaths > l.InventoryPaths || c.ProjectRecords < 0 || c.ParsedProjectRecords < 0 || c.ParsedProjectRecords > c.ProjectRecords || c.Contexts != len(r.Selections) || c.Contexts > l.Contexts || c.GlobalJSONCandidates < 0 || c.GlobalJSONRead < 0 || c.GlobalJSONRead > c.GlobalJSONCandidates || c.InputBytes < 0 || c.InputBytes > l.InputBytes || c.Requirements != len(r.Requirements) || c.Requirements > l.Requirements || c.OmittedFiles < 0 || c.OmittedRequirements < 0 {
		return errors.New("environment coverage is inconsistent")
	}
	if current {
		if c.ToolchainCandidates < 0 || c.ToolchainCandidates > c.InventoryPaths || c.ToolchainRead < 0 || c.ToolchainRead > c.ToolchainCandidates || c.ToolchainBytes < 0 || c.ToolchainBytes > l.ToolchainInputBytes || c.ToolchainDeclarations != len(r.ToolchainDeclarations) || c.ToolchainDeclarations > c.ToolchainCandidates || c.ToolchainDeclarations > l.ToolchainFiles || c.ToolchainRead > c.ToolchainDeclarations || c.OmittedToolchainFiles < 0 || int64(c.ToolchainRead)+c.OmittedToolchainFiles != int64(c.ToolchainCandidates) {
			return errors.New("environment toolchain coverage is inconsistent")
		}
	} else if c.ToolchainCandidates != 0 || c.ToolchainRead != 0 || c.ToolchainBytes != 0 || c.ToolchainDeclarations != 0 || c.OmittedToolchainFiles != 0 {
		return errors.New("legacy environment report contains newer toolchain coverage")
	}
	contexts := map[string]bool{}
	for _, s := range r.Selections {
		if !wireString(s.ContextID) || contexts[s.ContextID] || !wireString(s.ProjectID) || !wireString(s.StartDirectory) || !wireString(s.StartBasis) || !wireOptional(s.GlobalJSON) || !wireOptional(s.SDKVersion) || !wireOptional(s.RollForward) || !wireString(s.Applicability) || (s.State != "declared" && s.State != "unresolved" && s.State != "unconstrained") {
			return errors.New("environment selection is invalid")
		}
		if s.State == "unconstrained" && (s.SDKVersion != "" || s.RollForward != "" || s.AllowPrerelease != nil) {
			return errors.New("unconstrained environment selection contains a policy")
		}
		if s.State == "declared" && s.SDKVersion == "" && s.RollForward == "" && s.AllowPrerelease == nil {
			return errors.New("declared environment selection lacks a policy")
		}
		contexts[s.ContextID] = true
	}
	for _, q := range r.Requirements {
		if !wireString(q.ProjectID) || !wireString(q.ContextID) || !wireString(q.Dimension) || !wireString(q.Kind) || !wireOptional(q.Value) || !wireString(q.State) || !wireString(q.Evidence) || !wireOptional(q.Condition) || !wireString(q.Applicability) {
			return errors.New("environment requirement is invalid")
		}
		contexts[q.ContextID] = true
	}
	toolchainPaths := map[string]bool{}
	toolchainIncomplete := false
	for _, d := range r.ToolchainDeclarations {
		if !wireString(d.SourcePath) || !validToolchainPath(d.SourcePath) || !wireString(d.Tool) || !wireString(d.Kind) || !validToolchainScope(d.ScopeDirectory) || d.Applicability != toolchainApplicability || (d.State != "declared" && d.State != "unresolved" && d.State != "unsupported") || len(d.Values) > 16 || toolchainPaths[d.SourcePath] {
			return errors.New("environment toolchain declaration is invalid")
		}
		kind, ok := toolchainFilenames[path.Base(d.SourcePath)]
		if !ok || d.Tool != kind.tool || d.Kind != kind.kind || d.ScopeDirectory != toolchainScope(d.SourcePath) || d.Tool != "python" && len(d.Values) > 1 {
			return errors.New("environment toolchain declaration identity is inconsistent")
		}
		toolchainPaths[d.SourcePath] = true
		if d.State == "declared" && len(d.Values) == 0 || d.State != "declared" && len(d.Values) != 0 {
			return errors.New("environment toolchain declaration values do not match its state")
		}
		for _, value := range d.Values {
			if !wireString(value) || !safeToolchainSelector(value) {
				return errors.New("environment toolchain declaration value is invalid")
			}
		}
		toolchainIncomplete = toolchainIncomplete || d.State != "declared"
	}
	if r.Status == "complete" && current && (toolchainIncomplete || c.OmittedToolchainFiles != 0 || c.ToolchainCandidates != c.ToolchainRead || c.ToolchainRead != c.ToolchainDeclarations) {
		return errors.New("complete environment report contains unresolved or omitted toolchain evidence")
	}
	for _, b := range r.Boundaries {
		if !wireOptional(b.Path) || !wireOptional(b.ProjectID) || !wireOptional(b.ContextID) || !wireString(b.Reason) || !wireOptional(b.Detail) || (b.ContextID != "" && !contexts[b.ContextID]) {
			return errors.New("environment boundary is invalid")
		}
	}
	for _, d := range r.Diagnostics {
		if !wireString(d.Path) || !wireString(d.Code) || !wireString(d.Message) {
			return errors.New("environment diagnostic is invalid")
		}
		if r.Status == "complete" && current {
			if _, toolchainPath := toolchainFilenames[path.Base(d.Path)]; toolchainPath {
				return errors.New("complete environment report contains toolchain diagnostics")
			}
		}
	}
	for _, x := range r.Conflicts {
		if !wireString(x.ContextID) || !wireString(x.Dimension) || !wireString(x.Explanation) || len(x.Values) < 1 || len(x.Values) > 16 || len(x.Evidence) != len(x.Values) {
			return errors.New("environment conflict is invalid")
		}
		for _, v := range append(append([]string{}, x.Values...), x.Evidence...) {
			if !wireString(v) {
				return errors.New("environment conflict text is invalid")
			}
		}
	}
	encoded, err := json.Marshal(r)
	if err != nil || len(encoded) > l.OutputBytes {
		return errors.New("environment report exceeds output byte limit")
	}
	return nil
}

func wireString(v string) bool   { return v != "" && len(v) <= 8192 }
func wireOptional(v string) bool { return len(v) <= 8192 }

func validToolchainPath(value string) bool {
	return value != "" && !path.IsAbs(value) && path.Clean(value) == value && value != "." && value != ".." && !strings.HasPrefix(value, "../") && !strings.ContainsAny(value, "\\\x00")
}

func validToolchainScope(value string) bool {
	if value == "." {
		return true
	}
	return validToolchainPath(value)
}

func toolchainScope(sourcePath string) string {
	scope := path.Dir(sourcePath)
	if scope == "" {
		return "."
	}
	return scope
}

func validEnvironmentGitTree(value string) bool {
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
