package environments

import (
	"encoding/json"
	"errors"
)

// ValidateReport rejects decoded reports that are inconsistent or outside the
// same bounded wire form produced by Analyze.
func ValidateReport(r *Report) error {
	if r == nil {
		return errors.New("environment report is required")
	}
	if r.Provider != Provider || r.ProviderVersion != ProviderVersion || r.SemanticsReference != semanticsReference {
		return errors.New("environment report identity is invalid")
	}
	if r.Status != "complete" && r.Status != "partial" && r.Status != "skipped" {
		return errors.New("environment report status is invalid")
	}
	if (r.Source != "git" && r.Source != "directory") || (r.Source == "git") != (r.Tree != "") || !wireOptional(r.Tree) {
		return errors.New("environment source identity is invalid")
	}
	l := r.Limits
	if l.InventoryPaths < 1 || l.InventoryPaths > DefaultMaxInventoryPaths || l.GlobalJSONBytes < 1 || l.GlobalJSONBytes > DefaultMaxGlobalJSONBytes || l.InputBytes < 1 || l.InputBytes > DefaultMaxInputBytes || l.Requirements < 1 || l.Requirements > DefaultMaxRequirements || l.Contexts < 1 || l.Contexts > DefaultMaxContexts || l.OutputBytes < 1 || l.OutputBytes > DefaultMaxOutputBytes {
		return errors.New("environment limits are invalid")
	}
	c := r.Coverage
	if c.InventoryPaths < 0 || c.InventoryPaths > l.InventoryPaths || c.ProjectRecords < 0 || c.ParsedProjectRecords < 0 || c.ParsedProjectRecords > c.ProjectRecords || c.Contexts != len(r.Selections) || c.Contexts > l.Contexts || c.GlobalJSONCandidates < 0 || c.GlobalJSONRead < 0 || c.GlobalJSONRead > c.GlobalJSONCandidates || c.InputBytes < 0 || c.InputBytes > l.InputBytes || c.Requirements != len(r.Requirements) || c.Requirements > l.Requirements || c.OmittedFiles < 0 || c.OmittedRequirements < 0 {
		return errors.New("environment coverage is inconsistent")
	}
	contexts := map[string]bool{}
	for _, s := range r.Selections {
		if !wireString(s.ContextID) || contexts[s.ContextID] || !wireString(s.ProjectID) || !wireString(s.StartDirectory) || !wireString(s.StartBasis) || !wireOptional(s.GlobalJSON) || !wireOptional(s.SDKVersion) || !wireOptional(s.RollForward) || !wireString(s.Applicability) || (s.State != "declared" && s.State != "unresolved" && s.State != "unconstrained") {
			return errors.New("environment selection is invalid")
		}
		contexts[s.ContextID] = true
	}
	for _, q := range r.Requirements {
		if !wireString(q.ProjectID) || !wireString(q.ContextID) || !wireString(q.Dimension) || !wireString(q.Kind) || !wireOptional(q.Value) || !wireString(q.State) || !wireString(q.Evidence) || !wireOptional(q.Condition) || !wireString(q.Applicability) {
			return errors.New("environment requirement is invalid")
		}
		contexts[q.ContextID] = true
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
	}
	for _, x := range r.Conflicts {
		if !wireString(x.ContextID) || !wireString(x.Dimension) || !wireString(x.Explanation) || len(x.Values) < 2 || len(x.Values) > 16 || len(x.Evidence) != len(x.Values) {
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
