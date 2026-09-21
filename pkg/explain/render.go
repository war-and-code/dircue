package explain

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"
)

// Text renders a stable, short human explanation from the structured report.
func Text(report *Report) (string, error) {
	if err := ValidateReport(report); err != nil {
		return "", fmt.Errorf("unsupported explanation report")
	}
	var out strings.Builder
	fmt.Fprintf(&out, "%s: %s (%s)\n", printable(report.Query.Path), printable(report.Decision.Status), printable(report.Decision.Reason))
	if report.Decision.ReportedLanguage != "" {
		fmt.Fprintf(&out, "language: %s", printable(report.Decision.ReportedLanguage))
		if report.Decision.DetectedLanguage != "" && report.Decision.DetectedLanguage != report.Decision.ReportedLanguage {
			fmt.Fprintf(&out, " (detected %s)", printable(report.Decision.DetectedLanguage))
		}
		if report.Decision.Strategy != "" {
			fmt.Fprintf(&out, " via %s", printable(report.Decision.Strategy))
		}
		out.WriteByte('\n')
	}
	fmt.Fprintf(&out, "source: %s", printable(report.Source.Mode))
	if report.Source.Tree != "" {
		fmt.Fprintf(&out, " tree %s", printable(report.Source.Tree))
	}
	fmt.Fprintf(&out, " (%s)\n", printable(report.Source.Consistency))
	if report.Query.Kind == "project" {
		out.WriteString("extent: not retained for project facts\n")
	} else {
		fmt.Fprintf(&out, "extent: %d/%d bytes read; %d classified; complete=%t\n", report.Extent.ReadBytes, report.Extent.FileBytes, report.Extent.ClassifiedBytes, report.Extent.ContentComplete)
	}
	for _, override := range report.Overrides {
		if override.Provenance == "retained" {
			fmt.Fprintf(&out, "override: %s=%s at %s:%d\n", printable(override.Attribute), printable(override.Value), printable(override.Source), override.Line)
		} else {
			fmt.Fprintf(&out, "override: %s=%s (provenance unavailable)\n", printable(override.Attribute), printable(override.Value))
		}
	}
	for _, fact := range report.Facts {
		fmt.Fprintf(&out, "fact: %s %s", printable(fact.Kind), printable(fact.State))
		if fact.Project != "" {
			fmt.Fprintf(&out, " project=%s", printable(fact.Project))
		}
		if fact.Target != "" {
			fmt.Fprintf(&out, " target=%s", printable(fact.Target))
		}
		if fact.Evidence != "" {
			fmt.Fprintf(&out, " evidence=%s", printable(fact.Evidence))
		}
		if fact.Condition != "" {
			fmt.Fprintf(&out, " condition=%s", printable(fact.Condition))
		}
		if fact.Detail != "" {
			fmt.Fprintf(&out, " detail=%s", printable(fact.Detail))
		}
		out.WriteByte('\n')
	}
	for i, step := range report.Steps {
		fmt.Fprintf(&out, "%d. %s [%s]: %s", i+1, printable(step.Rule), printable(step.Provider), printable(step.Outcome))
		if step.Evidence != "" {
			fmt.Fprintf(&out, " — %s", printable(step.Evidence))
		}
		out.WriteByte('\n')
	}
	for _, omission := range report.Omissions {
		fmt.Fprintf(&out, "omitted: %s (%d)\n", printable(omission.Reason), omission.Count)
	}
	return out.String(), nil
}

// printable leaves normal human-readable evidence alone and quotes values
// containing terminal controls or invisible formatting characters.
func printable(value string) string {
	for _, r := range value {
		if unicode.IsControl(r) || !unicode.IsGraphic(r) {
			return strconv.QuoteToGraphic(value)
		}
	}
	return value
}
