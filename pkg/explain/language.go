package explain

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"path"
	"slices"
	"strings"
	"unicode/utf8"
)

var supportedDecisionStatus = map[string]bool{
	"included":    true,
	"excluded":    true,
	"unavailable": true,
}

// LanguageReport validates and bounds a scanner-produced trace. It never
// classifies content or fills missing evidence by inference.
func LanguageReport(trace LanguageTrace) (*Report, error) {
	if !validPath(trace.Path) {
		return nil, errors.New("explanation path must be normalized and root-relative")
	}
	if trace.Source.Mode != "git" && trace.Source.Mode != "directory" && trace.Source.Mode != "unknown" {
		return nil, errors.New("explanation source must be git, directory, or retained unknown")
	}
	if trace.Source.Mode == "git" && !validGitTree(trace.Source.Tree) {
		return nil, errors.New("git explanation requires a selected tree")
	}
	if trace.Source.Mode != "git" && trace.Source.Tree != "" {
		return nil, errors.New("only git explanations may identify a selected tree")
	}
	if trace.Source.Mode == "unknown" && (trace.Scope.Evidence != "retained" || trace.Source.ReportSHA256 == "") {
		return nil, errors.New("unknown source is allowed only for SHA-bound retained evidence")
	}
	if trace.Source.ReportSHA256 != "" && !validSHA256(trace.Source.ReportSHA256) {
		return nil, errors.New("retained report SHA-256 is invalid")
	}
	if trace.Source.ReportSHA256 != "" && trace.Scope.Evidence != "retained" {
		return nil, errors.New("fresh explanation must not claim retained report bytes")
	}
	if trace.Source.Consistency == "" || !validText(trace.Source.Consistency) {
		return nil, errors.New("explanation source consistency is required")
	}
	if trace.Scope.Module != "languages" || trace.Scope.Engine == "" || trace.Scope.Evidence == "" || trace.Scope.Inventory == "" || trace.Scope.Content == "" {
		return nil, errors.New("language explanation scope is incomplete")
	}
	if trace.Scope.Evidence != "fresh" && trace.Scope.Evidence != "retained" {
		return nil, errors.New("language explanation evidence mode is invalid")
	}
	if !validText(trace.Scope.Engine) || !validText(trace.Scope.Evidence) || !validText(trace.Scope.Inventory) || !validText(trace.Scope.Content) {
		return nil, errors.New("language explanation scope contains unsupported text")
	}
	if trace.Extent.FileBytes < 0 || trace.Extent.ReadBytes < 0 || trace.Extent.ClassifiedBytes < 0 || trace.Extent.ClassifiedBytes > trace.Extent.ReadBytes {
		return nil, errors.New("language explanation extent is inconsistent")
	}
	if !supportedDecisionStatus[trace.Decision.Status] || trace.Decision.Reason == "" {
		return nil, errors.New("language explanation decision is incomplete")
	}
	if trace.Decision.Status == "included" && trace.Decision.ReportedLanguage == "" {
		return nil, errors.New("included language explanation requires a reported language")
	}
	if !validText(trace.Decision.Reason) || !validOptionalText(trace.Decision.DetectedLanguage) || !validOptionalText(trace.Decision.ReportedLanguage) || !validOptionalText(trace.Decision.Strategy) {
		return nil, errors.New("language explanation decision contains unsupported text")
	}

	overrideCount := min(len(trace.Overrides), MaxOverrides)
	overrides := append([]Override{}, trace.Overrides[:overrideCount]...)
	for _, override := range overrides {
		if err := validateOverride(override); err != nil {
			return nil, err
		}
	}
	slices.SortFunc(overrides, compareOverride)
	overrides = slices.CompactFunc(overrides, func(a, b Override) bool { return a.Attribute == b.Attribute })
	omissionCount := min(len(trace.Omissions), MaxOmissions)
	if len(trace.Omissions) > MaxOmissions {
		omissionCount = MaxOmissions - 1
	}
	omissions := append([]Omission{}, trace.Omissions[:omissionCount]...)
	if len(trace.Omissions) > MaxOmissions {
		omissions = append(omissions, Omission{Reason: "omission_limit", Count: len(trace.Omissions) - omissionCount})
	}
	if len(trace.Overrides) > MaxOverrides {
		omissions = appendOmission(omissions, "override_limit", len(trace.Overrides)-MaxOverrides)
	}

	stepCount := min(len(trace.Steps), MaxSteps)
	steps := append([]Step{}, trace.Steps[:stepCount]...)
	for _, step := range steps {
		if step.Rule == "" || step.Provider == "" || step.Outcome == "" || !validText(step.Rule) || !validText(step.Provider) || !validText(step.Outcome) || !validOptionalText(step.Evidence) {
			return nil, errors.New("language explanation step is invalid")
		}
	}
	if len(trace.Steps) > MaxSteps {
		omissions = appendOmission(omissions, "step_limit", len(trace.Steps)-MaxSteps)
	}
	if len(steps) == 0 && trace.Decision.Status != "unavailable" {
		return nil, errors.New("language explanation has no decision steps")
	}
	factCount := min(len(trace.Facts), MaxFacts)
	facts := append([]Fact{}, trace.Facts[:factCount]...)
	for _, fact := range facts {
		if err := validateFact(fact); err != nil {
			return nil, err
		}
	}
	slices.SortFunc(facts, compareFact)
	facts = slices.Compact(facts)
	if len(trace.Facts) > MaxFacts {
		omissions = appendOmission(omissions, "fact_limit", len(trace.Facts)-MaxFacts)
	}
	for _, omission := range omissions {
		if omission.Reason == "" || omission.Count < 1 || !validText(omission.Reason) {
			return nil, errors.New("language explanation omission is invalid")
		}
	}
	if len(omissions) > MaxOmissions {
		omissions = omissions[:MaxOmissions]
	}

	status := "complete"
	if len(omissions) > 0 || trace.Decision.Status == "unavailable" {
		status = "partial"
	}
	report := &Report{
		SchemaVersion: SchemaVersion, Provider: Provider, ProviderVersion: ProviderVersion,
		Status: status, Source: trace.Source, Query: Query{Kind: "language-path", Path: trace.Path},
		Scope: trace.Scope, Extent: trace.Extent,
		Limits:    Limits{ClassificationBytes: trace.ClassificationLimit, Steps: MaxSteps, Overrides: MaxOverrides, Facts: MaxFacts, StringBytes: MaxStringBytes},
		Overrides: overrides, Facts: facts, Steps: steps, Decision: trace.Decision, Omissions: omissions,
	}
	report.ObservationID = observationID(report)
	return report, nil
}

func validateOverride(value Override) error {
	if value.Attribute == "" || value.Value == "" || !validText(value.Attribute) || !validText(value.Value) {
		return errors.New("language explanation override is incomplete")
	}
	switch value.Provenance {
	case "retained":
		if !validPath(value.Source) || value.Line < 1 {
			return errors.New("retained override provenance requires a source and line")
		}
	case "unavailable":
		if value.Source != "" || value.Line != 0 {
			return errors.New("unavailable override provenance must not invent a source or line")
		}
	default:
		return errors.New("language explanation override provenance is invalid")
	}
	return nil
}

func compareOverride(a, b Override) int {
	if n := strings.Compare(a.Attribute, b.Attribute); n != 0 {
		return n
	}
	if n := strings.Compare(a.Source, b.Source); n != 0 {
		return n
	}
	return a.Line - b.Line
}

func appendOmission(values []Omission, reason string, count int) []Omission {
	for i := range values {
		if values[i].Reason == reason {
			values[i].Count += count
			return values
		}
	}
	return append(values, Omission{Reason: reason, Count: count})
}

func observationID(report *Report) string {
	encoded, _ := json.Marshal(struct {
		Schema, ProviderVersion string
		Source                  Source
		Query                   Query
		Scope                   Scope
	}{SchemaVersion, ProviderVersion, report.Source, report.Query, report.Scope})
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:])
}

func validPath(value string) bool {
	return value != "" && len(value) <= MaxStringBytes && utf8.ValidString(value) && value == path.Clean(value) && value != "." && value != ".." && !strings.HasPrefix(value, "../") && !strings.HasPrefix(value, "/") && !strings.ContainsAny(value, "\\\x00\r\n")
}

func validText(value string) bool {
	return value != "" && validOptionalText(value)
}

func validOptionalText(value string) bool {
	return len(value) <= MaxStringBytes && utf8.ValidString(value) && !strings.ContainsAny(value, "\x00\r")
}

func validSHA256(value string) bool {
	return validLowerHex(value, 64)
}

func validGitTree(value string) bool {
	return validLowerHex(value, 40) || validLowerHex(value, 64)
}

func validLowerHex(value string, length int) bool {
	if len(value) != length {
		return false
	}
	for _, r := range value {
		if !(r >= '0' && r <= '9') && !(r >= 'a' && r <= 'f') {
			return false
		}
	}
	return true
}
