package scanner

import (
	"context"
	"errors"
	"os"
	"slices"
	"strconv"

	"github.com/war-and-code/dircue/pkg/explain"
)

// analyzeFileWithLanguageTrace runs the ordinary file-analysis path with an
// opt-in trace sink. The selected-source walker supplies the already resolved
// attributes and exact provenance on the job.
func analyzeFileWithLanguageTrace(ctx context.Context, root *os.Root, item job, opts Options, trace *explain.LanguageTrace) (result, error) {
	if trace == nil {
		return result{}, errors.New("language trace is required")
	}
	if trace.Path != "" && trace.Path != item.path {
		return result{}, errors.New("language trace path does not match selected job")
	}
	trace.Path = item.path
	trace.Scope.Module = "languages"
	if trace.Scope.Engine == "" {
		trace.Scope.Engine = "go-enry/Linguist 9.7.0"
	}
	if trace.Scope.Evidence == "" {
		trace.Scope.Evidence = "fresh"
	}
	if trace.Scope.Inventory == "" {
		trace.Scope.Inventory = "selected-source-metadata-walk"
	}
	if trace.Scope.Content == "" {
		trace.Scope.Content = "target-file-bounded-prefix"
	}
	trace.ClassificationLimit = ClassificationBytes
	trace.Extent = explain.Extent{FileBytes: item.size}
	trace.Overrides = languageOverrides(item.attrs, item.traceOverrides)
	trace.Steps = nil
	trace.Decision = explain.Decision{}
	opts.languageTrace = trace
	return analyzeFile(ctx, root, item, opts)
}

func languageOverrides(attrs overrides, retained []explain.Override) []explain.Override {
	values := slices.Clone(retained)
	has := func(attribute string) bool {
		return slices.ContainsFunc(values, func(value explain.Override) bool { return value.Attribute == attribute })
	}
	add := func(attribute, value string) {
		if !has(attribute) {
			values = append(values, explain.Override{Attribute: attribute, Value: value, Provenance: "unavailable"})
		}
	}
	if attrs.languageSet {
		value := attrs.language
		if value == "" {
			value = "unknown"
		}
		add("linguist-language", value)
	}
	for _, value := range []struct {
		attribute string
		value     *bool
	}{
		{"linguist-vendored", attrs.vendored},
		{"linguist-generated", attrs.generated},
		{"linguist-detectable", attrs.detectable},
		{"linguist-documentation", attrs.documentation},
	} {
		if value.value != nil {
			add(value.attribute, strconv.FormatBool(*value.value))
		}
	}
	if attrs.lfsTracked {
		add("filter", "lfs")
	}
	slices.SortFunc(values, compareTraceOverride)
	return values
}

func compareTraceOverride(a, b explain.Override) int {
	if a.Attribute < b.Attribute {
		return -1
	}
	if a.Attribute > b.Attribute {
		return 1
	}
	return 0
}

func addLanguageTraceStep(trace *explain.LanguageTrace, rule, provider, outcome, evidence string) {
	if trace != nil {
		trace.Steps = append(trace.Steps, explain.Step{Rule: rule, Provider: provider, Outcome: outcome, Evidence: evidence})
	}
}

func languageTraceOverrideProvider(trace *explain.LanguageTrace, attribute string) string {
	if trace != nil {
		for _, override := range trace.Overrides {
			if override.Attribute == attribute && override.Provenance == "retained" {
				return override.Source
			}
		}
	}
	return ".gitattributes (source unavailable)"
}

func setLanguageTraceDecision(trace *explain.LanguageTrace, status, reason, detected, reported, strategy string) {
	if trace != nil {
		trace.Decision = explain.Decision{Status: status, Reason: reason, DetectedLanguage: detected, ReportedLanguage: reported, Strategy: strategy}
	}
}
