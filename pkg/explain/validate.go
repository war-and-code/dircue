package explain

import (
	"errors"
	"reflect"
)

// ValidateReport applies the same semantic validation and normalization used
// by the builders to an untrusted decoded explanation.
func ValidateReport(report *Report) error {
	if report == nil {
		return errors.New("explanation report is required")
	}
	var rebuilt *Report
	var err error
	switch report.Query.Kind {
	case "language-path":
		rebuilt, err = LanguageReport(LanguageTrace{
			Source: report.Source, Path: report.Query.Path, Scope: report.Scope, Extent: report.Extent,
			ClassificationLimit: report.Limits.ClassificationBytes,
			Overrides:           report.Overrides, Facts: report.Facts, Steps: report.Steps,
			Decision: report.Decision, Omissions: report.Omissions,
		})
	case "project":
		rebuilt, err = ProjectReport(ProjectTrace{
			Source: report.Source, Project: report.Query.Path, Scope: report.Scope,
			Facts: report.Facts, Steps: report.Steps, Decision: report.Decision, Omissions: report.Omissions,
		})
	default:
		return errors.New("explanation query kind is invalid")
	}
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(rebuilt, report) {
		return errors.New("explanation report is not in canonical bounded form")
	}
	return nil
}
