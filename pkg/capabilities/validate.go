package capabilities

import (
	"errors"
	"regexp"
	"slices"
)

var idPattern = regexp.MustCompile(`^[a-z][a-z0-9-]{0,63}$`)

func (d Descriptor) Validate() error {
	if d.SchemaVersion != SchemaVersion || d.Kind != "dircue-planner-capabilities" || d.Provider != "dircue" || !bounded(d.ProviderVersion) || d.Scope != "planner-supported-modules" {
		return errors.New("invalid capability descriptor identity")
	}
	if len(d.Modules) == 0 || len(d.Modules) > 64 {
		return errors.New("invalid capability module count")
	}
	seen := map[string]bool{}
	questions := map[string]bool{}
	last := ""
	for _, m := range d.Modules {
		if !idPattern.MatchString(m.ID) || m.ID <= last || seen[m.ID] || !bounded(m.Question) || questions[m.Question] || !slices.Equal(m.Command.ArgvPrefix, []string{"dircue", "analyze", m.ID, "--json"}) || !bounded(m.ProviderVersion) || !versionPattern.MatchString(m.AggregateSchemaSince) || !validStrings(m.Prerequisites) || !validStrings(m.RequiredInputs) || !validStrings(m.ExpectedEvidence) || !validStrings(m.SupportedLanguages) || !slices.Contains([]string{"metadata", "bounded-content", "full-content", "metadata-and-bounded-content"}, m.Cost.Inspection) {
			return errors.New("invalid capability module")
		}
		seen[m.ID], questions[m.Question], last = true, true, m.ID
	}
	return nil
}

var versionPattern = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+$`)

func bounded(v string) bool { return v != "" && len(v) <= 256 }
func validStrings(values []string) bool {
	if len(values) > 64 {
		return false
	}
	seen := map[string]bool{}
	for _, v := range values {
		if !bounded(v) || seen[v] {
			return false
		}
		seen[v] = true
	}
	return true
}
