package cli

import (
	"fmt"
	"io"
	"strings"

	"dircue/pkg/explain"
	"dircue/pkg/profile"
	"dircue/pkg/reportdiff"
	"dircue/pkg/scanner"
	"github.com/spf13/cobra"
)

func newExplainCommand(opts *options) *cobra.Command {
	var file, project, saved string
	command := &cobra.Command{Use: "explain [path]", Short: "Explain one file decision or retained project relationship", Long: "Inspect one language decision in its original source context, or explain retained evidence from an explicitly supplied aggregate report. Fresh file inspection traverses selected-source metadata for attribute context. Saved-report queries never open the recorded source directory or evidence paths.", Args: pathArgs}
	command.Example = "  dircue analyze explain --file src/main.go --json /checkout\n  dircue analyze explain --report profile.json --project app/app.csproj --json"
	command.Flags().StringVar(&file, "file", "", "Root-relative file whose language decision or retained evidence should be explained")
	command.Flags().StringVar(&project, "project", "", "Root-relative project manifest whose recorded declarations should be explained")
	command.Flags().StringVar(&saved, "report", "", "Read a saved aggregate report instead of inspecting a directory")
	command.RunE = func(cmd *cobra.Command, args []string) error {
		if (file == "") == (project == "") {
			return fmt.Errorf("explain requires exactly one of --file or --project")
		}
		for _, flag := range []string{"breakdown", "strategies"} {
			if cmd.Flags().Changed(flag) {
				return fmt.Errorf("--%s does not apply to explanations", flag)
			}
		}
		query := explain.Query{Kind: "language-path", Path: file}
		if project != "" {
			query = explain.Query{Kind: "project", Path: project}
		}
		if err := cmd.Context().Err(); err != nil {
			return err
		}
		var report *profile.Report
		var result *explain.Report
		var err error
		if saved != "" {
			if len(args) > 0 {
				return fmt.Errorf("--report cannot be combined with a source directory")
			}
			for _, flag := range []string{"workers", "max-file-bytes", "source", "rev", "tree", "on-error", "tree-size"} {
				if cmd.Flags().Changed(flag) {
					return fmt.Errorf("--%s does not apply to saved-report explanations", flag)
				}
			}
			input, err := openInputFile(saved, "saved report")
			if err != nil {
				return &diagnosticError{message: "cannot open saved report", cause: err}
			}
			defer input.Close()
			info, err := input.Stat()
			if err != nil || !info.Mode().IsRegular() {
				return fmt.Errorf("saved report must be a readable regular file")
			}
			if info.Size() > reportdiff.MaxInputBytes {
				return reportdiff.ErrLimit
			}
			retained, digest, err := reportdiff.ReadEvidence(input)
			if err != nil {
				return fmt.Errorf("saved report: %w", err)
			}
			result, err = explainProfileEvidence(retained, query, "retained", digest)
			if err != nil {
				return err
			}
			report = emptyExplanationProfile(retained.Root, result)
		} else {
			if opts.workers < 0 || opts.maxFileBytes < 0 {
				return fmt.Errorf("workers and max-file-bytes must not be negative")
			}
			root := "."
			if len(args) == 1 {
				root = args[0]
			}
			source, rev := opts.source, ""
			if source != "auto" && source != "git" && source != "directory" {
				return fmt.Errorf("--source must be auto, git, or directory")
			}
			if cmd.Flags().Changed("rev") {
				if source == "directory" {
					return fmt.Errorf("--rev requires a Git content source")
				}
				source = "git"
				rev = opts.revision
			}
			scanOptions := scanner.Options{Source: source, Revision: rev, Tree: opts.tree, ErrorPolicy: scanner.ErrorPolicy(opts.onError), MaxTreeSize: max(1, opts.maxTreeSize), Workers: opts.workers, MaxFileBytes: opts.maxFileBytes, ExplainPath: file}
			if project != "" {
				scanOptions.Declarations = true
				scanOptions.DeclarationsOnly = true
			}
			report, err = scanner.Scan(cmd.Context(), root, scanOptions)
			if err != nil {
				return err
			}
			if project != "" {
				result, err = explainProfileEvidence(report, query, "fresh", "")
				if err != nil {
					return err
				}
				report.Explanation = result
				report.SchemaVersion = profile.TargetedSchemaVersion
			}
		}
		if err := cmd.Context().Err(); err != nil {
			return err
		}
		for _, warning := range report.Warnings {
			if _, err := fmt.Fprintf(cmd.ErrOrStderr(), "warning: %s: %s (%s)\n", terminalValue(warning.Path), terminalValue(warning.Message), warning.Code); err != nil {
				return err
			}
		}
		if opts.json {
			return writeJSON(cmd.OutOrStdout(), report)
		}
		text, err := explain.Text(report.Explanation)
		if err != nil {
			return err
		}
		_, err = io.WriteString(cmd.OutOrStdout(), text)
		return err
	}
	return command
}

func emptyExplanationProfile(root string, result *explain.Report) *profile.Report {
	return &profile.Report{SchemaVersion: profile.TargetedSchemaVersion, Root: root, Languages: []profile.Language{}, Ecosystems: []profile.Finding{}, Frameworks: []profile.Finding{}, Layouts: []profile.Finding{}, Warnings: []profile.Warning{}, Explanation: result}
}

func explainProfileEvidence(report *profile.Report, query explain.Query, mode, digest string) (*explain.Report, error) {
	source := explain.Source{Mode: "unknown", Consistency: "not_retained"}
	add := func(candidate explain.Source) error {
		if candidate.Mode == "unknown" {
			return nil
		}
		if source.Mode != "unknown" && (source.Mode != candidate.Mode || source.Tree != candidate.Tree) {
			return fmt.Errorf("saved evidence has conflicting source identities")
		}
		source = candidate
		return nil
	}
	convert := func(mode, tree string) explain.Source {
		consistency := "live_directory"
		if mode == "git" {
			consistency = "selected_git_tree"
		}
		return explain.Source{Mode: mode, Tree: tree, Consistency: consistency}
	}
	if r := report.Declarations; r != nil {
		if err := add(convert(r.Source, r.Tree)); err != nil {
			return nil, err
		}
	}
	if r := report.Focus; r != nil {
		if err := add(convert(r.Source, r.Tree)); err != nil {
			return nil, err
		}
	}
	if r := report.Availability; r != nil {
		if err := add(convert(r.Source.Mode, r.Source.Tree)); err != nil {
			return nil, err
		}
	}
	if r := report.Explanation; r != nil {
		if err := add(r.Source); err != nil {
			return nil, err
		}
	}
	if r := report.Metrics; r != nil {
		if err := add(convert(r.Source, r.Tree)); err != nil {
			return nil, err
		}
	}
	if r := report.Projects; r != nil {
		if err := add(convert(r.Source, r.Tree)); err != nil {
			return nil, err
		}
	}
	if r := report.Formats; r != nil {
		if err := add(convert(r.Source.Mode, r.Source.Tree)); err != nil {
			return nil, err
		}
	}
	if r := report.Discovery; r != nil {
		if err := add(convert(r.Source.Mode, r.Source.Tree)); err != nil {
			return nil, err
		}
	}
	if r := report.Registries; r != nil {
		if err := add(convert(r.Source.Mode, r.Source.Tree)); err != nil {
			return nil, err
		}
	}
	if r := report.Rules; r != nil {
		if err := add(convert(r.Source.Kind, r.Source.Tree)); err != nil {
			return nil, err
		}
	}
	if r := report.Structure; r != nil {
		if err := add(convert(r.Source, r.Tree)); err != nil {
			return nil, err
		}
	}
	if mode == "retained" {
		if source.Mode == "unknown" {
			source.Consistency = "unknown_source_in_saved_report"
		} else if !strings.HasPrefix(source.Consistency, "retained_") {
			source.Consistency = "retained_" + source.Consistency
		}
	}
	input := explain.RetainedEvidence{Source: source, EvidenceMode: mode, ReportSHA256: digest, Declarations: report.Declarations, Focus: report.Focus, Availability: report.Availability, Explanation: report.Explanation}
	for _, language := range report.Languages {
		if language.Files != nil {
			input.FilesRetained = true
		}
		input.Languages = append(input.Languages, explain.LanguageEvidence{Name: language.Name, Files: language.Files})
	}
	return explain.RetainedReport(input, query)
}
