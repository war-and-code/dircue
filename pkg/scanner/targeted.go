package scanner

import (
	"context"
	"dircue/pkg/explain"
	"dircue/pkg/profile"
	"errors"
	"os"
	"path"
	"strings"
)

func validateTargetedOptions(opts Options) error {
	if opts.Focus != nil {
		if opts.Discovery || opts.Formats || opts.Rules != nil || opts.Registries || opts.Projects || opts.Structure != nil || len(opts.Detectors) != 0 || opts.Availability || opts.ExplainPath != "" || opts.DiscoveryOnly || opts.FormatsOnly || opts.DeclarationsOnly || opts.RegistriesOnly || opts.RulesOnly || opts.AvailabilityOnly {
			return errors.New("focus runs only its declaration prepass and explicitly requested metrics")
		}
	}
	if opts.AvailabilityOnly {
		if !opts.Availability || opts.Discovery || opts.Formats || opts.Rules != nil || opts.Registries || opts.Projects || opts.Declarations || opts.Metrics != nil || opts.Structure != nil || len(opts.Detectors) != 0 || opts.ExplainPath != "" {
			return errors.New("availability-only requires availability and cannot run other profilers")
		}
	}
	if opts.Availability && (opts.DiscoveryOnly || opts.FormatsOnly || opts.DeclarationsOnly || opts.RegistriesOnly || opts.RulesOnly) {
		return errors.New("availability cannot run in another module-only scan")
	}
	if opts.ExplainPath != "" {
		if opts.Focus != nil || opts.Availability || opts.Discovery || opts.Formats || opts.Rules != nil || opts.Registries || opts.Projects || opts.Declarations || opts.Metrics != nil || opts.Structure != nil || len(opts.Detectors) != 0 {
			return errors.New("a targeted language explanation cannot run other profilers")
		}
		if !validTargetPath(opts.ExplainPath) {
			return errors.New("explanation path must be a clean root-relative file path")
		}
	}
	return nil
}

func validTargetPath(name string) bool {
	return name != "" && name != "." && path.Clean(name) == name && !path.IsAbs(name) && name != ".." && !strings.HasPrefix(name, "../") && !strings.ContainsAny(name, "\\\x00") && !strings.Contains(name, ":")
}

// The focused prepass retains original selected-source readers and resolved
// attributes. Its later consumer never reopens a different Git revision.
func analyzeSelectedFile(ctx context.Context, root *os.Root, item job, opts Options) (result, error) {
	if opts.ExplainPath != "" {
		if err := ctx.Err(); err != nil {
			return result{}, err
		}
		if item.path != opts.ExplainPath {
			return result{path: item.path, skipped: true}, nil
		}
		trace := &explain.LanguageTrace{}
		value, err := analyzeFileWithLanguageTrace(ctx, root, item, opts, trace)
		return result{path: item.path, skipped: true, warnings: value.warnings, languageTrace: trace}, err
	}
	if opts.Focus != nil {
		if err := ctx.Err(); err != nil {
			return result{}, err
		}
		return result{path: item.path, skipped: true, selectedJob: &item, declarationSelected: true, declarationFile: declarationCandidate(root, item)}, nil
	}
	if opts.AvailabilityOnly {
		if err := ctx.Err(); err != nil {
			return result{}, err
		}
		return result{path: item.path, skipped: true, selectedJob: &item}, nil
	}
	value, err := analyzeFile(ctx, root, item, opts)
	if opts.Availability {
		value.selectedJob = &item
	}
	return value, err
}

func finishLanguageExplanation(opts Options, snapshot *gitSnapshot, report *profile.Report, trace *explain.LanguageTrace, nonRegular bool) error {
	if trace == nil {
		reason := "not_in_selected_inventory"
		if nonRegular {
			reason = "non_regular_file"
		}
		trace = &explain.LanguageTrace{Path: opts.ExplainPath, Scope: explain.Scope{Module: "languages", Engine: "dircue/go-enry", Evidence: "fresh", Inventory: "selected-source-metadata-walk", Content: "not-inspected"}, ClassificationLimit: ClassificationBytes, Decision: explain.Decision{Status: "unavailable", Reason: reason}}
	}
	trace.Source = explain.Source{Mode: "directory", Consistency: "live_directory"}
	if snapshot != nil {
		trace.Source = explain.Source{Mode: "git", Tree: snapshot.tree.Hash.String(), Consistency: "selected_git_tree"}
	}
	for _, warning := range report.Warnings {
		switch warning.Code {
		case "tree_size_limit":
			trace.Decision = explain.Decision{Status: "unavailable", Reason: "tree_size_limit"}
			trace.Omissions = append(trace.Omissions, explain.Omission{Reason: "tree_size_limit", Count: 1})
		case "unsupported_gitattributes":
			trace.Omissions = append(trace.Omissions, explain.Omission{Reason: "unsupported_gitattributes", Count: 1})
		}
	}
	var err error
	report.Explanation, err = explain.LanguageReport(*trace)
	if err != nil {
		return err
	}
	report.SchemaVersion = profile.TargetedSchemaVersion
	return nil
}
