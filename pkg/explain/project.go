package explain

import (
	"errors"
	"slices"
	"strings"
)

type ProjectTrace struct {
	Source    Source
	Project   string
	Scope     Scope
	Facts     []Fact
	Steps     []Step
	Decision  Decision
	Omissions []Omission
}

// ProjectReport validates and bounds facts retained by declaration, focus, and
// availability modules. It does not infer build-tool resolution.
func ProjectReport(trace ProjectTrace) (*Report, error) {
	if !validPath(trace.Project) {
		return nil, errors.New("explanation project must be normalized and root-relative")
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
	if !validText(trace.Source.Consistency) || trace.Scope.Module != "project-evidence" || !validText(trace.Scope.Engine) || !validText(trace.Scope.Evidence) || !validText(trace.Scope.Inventory) || !validText(trace.Scope.Content) {
		return nil, errors.New("project explanation scope is incomplete")
	}
	if trace.Scope.Evidence != "fresh" && trace.Scope.Evidence != "retained" {
		return nil, errors.New("project explanation evidence mode is invalid")
	}
	if trace.Decision.Status != "reported" && trace.Decision.Status != "unavailable" {
		return nil, errors.New("project explanation decision status is invalid")
	}
	if !validText(trace.Decision.Reason) {
		return nil, errors.New("project explanation decision reason is required")
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
	omissionCount := min(len(trace.Omissions), MaxOmissions)
	if len(trace.Omissions) > MaxOmissions {
		omissionCount = MaxOmissions - 1
	}
	omissions := append([]Omission{}, trace.Omissions[:omissionCount]...)
	if len(trace.Omissions) > MaxOmissions {
		omissions = append(omissions, Omission{Reason: "omission_limit", Count: len(trace.Omissions) - omissionCount})
	}
	if len(trace.Facts) > MaxFacts {
		omissions = appendOmission(omissions, "fact_limit", len(trace.Facts)-MaxFacts)
	}
	stepCount := min(len(trace.Steps), MaxSteps)
	steps := append([]Step{}, trace.Steps[:stepCount]...)
	for _, step := range steps {
		if step.Rule == "" || step.Provider == "" || step.Outcome == "" || !validText(step.Rule) || !validText(step.Provider) || !validText(step.Outcome) || !validOptionalText(step.Evidence) {
			return nil, errors.New("project explanation step is invalid")
		}
	}
	if len(trace.Steps) > MaxSteps {
		omissions = appendOmission(omissions, "step_limit", len(trace.Steps)-MaxSteps)
	}
	if trace.Decision.Status == "reported" && (len(facts) == 0 || len(steps) == 0) {
		return nil, errors.New("reported project explanation requires facts and a decision step")
	}
	for _, omission := range omissions {
		if omission.Count < 1 || !validText(omission.Reason) {
			return nil, errors.New("project explanation omission is invalid")
		}
	}
	if len(omissions) > MaxOmissions {
		omissions = omissions[:MaxOmissions]
	}
	status := "complete"
	if trace.Decision.Status == "unavailable" || len(omissions) > 0 {
		status = "partial"
	}
	report := &Report{
		SchemaVersion: SchemaVersion, Provider: Provider, ProviderVersion: ProviderVersion,
		Status: status, Source: trace.Source, Query: Query{Kind: "project", Path: trace.Project},
		Scope: trace.Scope, Extent: Extent{},
		Limits:    Limits{Steps: MaxSteps, Overrides: MaxOverrides, Facts: MaxFacts, StringBytes: MaxStringBytes},
		Overrides: []Override{}, Facts: facts, Steps: steps, Decision: trace.Decision, Omissions: omissions,
	}
	report.ObservationID = observationID(report)
	return report, nil
}

func validateFact(fact Fact) error {
	if !validText(fact.Kind) || !validText(fact.State) || !validOptionalText(fact.Target) || !validOptionalText(fact.Condition) || !validOptionalText(fact.Detail) {
		return errors.New("project explanation fact is invalid")
	}
	if fact.Project != "" && !validPath(fact.Project) {
		return errors.New("project explanation fact project is invalid")
	}
	if fact.Evidence != "" && !validPath(fact.Evidence) {
		return errors.New("project explanation fact evidence is invalid")
	}
	return nil
}

func compareFact(a, b Fact) int {
	for _, pair := range [][2]string{{a.Kind, b.Kind}, {a.Project, b.Project}, {a.Target, b.Target}, {a.State, b.State}, {a.Evidence, b.Evidence}, {a.Condition, b.Condition}, {a.Detail, b.Detail}} {
		if n := strings.Compare(pair[0], pair[1]); n != 0 {
			return n
		}
	}
	return 0
}
