package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"dircue/pkg/reportdiff"
	"github.com/spf13/cobra"
)

func newCompareCommand(opts *options) *cobra.Command {
	command := &cobra.Command{
		Use:     "compare <base.json> <head.json>",
		Short:   "Compare two saved profiles without rescanning their sources",
		Long:    "Compare explicitly selected aggregate dircue JSON reports. Compatibility is checked per module; missing provenance and partial coverage limit conclusions. Evidence paths are never opened. Successful comparisons return zero even when observations differ.",
		Example: "  dircue compare base.json head.json --json\n  dircue analyze discovery --json /checkout > profile.json",
		Args: func(_ *cobra.Command, args []string) error {
			if len(args) != 2 {
				return fmt.Errorf("compare requires two saved aggregate reports; use: dircue compare base.json head.json --json")
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			for _, flag := range analysisFlagNames {
				if cmd.Flags().Changed(flag) {
					return fmt.Errorf("--%s does not apply to saved-report comparison; set scan options when creating a report with dircue analyze discovery --json /checkout", flag)
				}
			}
			if err := cmd.Context().Err(); err != nil {
				return err
			}
			base, err := loadComparisonFile(args[0], "base")
			if err != nil {
				return err
			}
			if err := cmd.Context().Err(); err != nil {
				return err
			}
			head, err := loadComparisonFile(args[1], "head")
			if err != nil {
				return err
			}
			report, err := reportdiff.Compare(base, head)
			if err != nil {
				return err
			}
			if err := cmd.Context().Err(); err != nil {
				return err
			}
			if opts.json {
				return json.NewEncoder(cmd.OutOrStdout()).Encode(report)
			}
			return writeComparison(cmd.OutOrStdout(), report)
		},
	}
	setSavedReportHelp(command)
	return command
}

func loadComparisonFile(name, role string) (*reportdiff.Snapshot, error) {
	file, err := openInputFile(name, role+" report")
	if err != nil {
		return nil, &diagnosticError{message: fmt.Sprintf("cannot open %s report; supply a readable regular aggregate JSON report, for example one saved by: dircue analyze discovery --json /checkout", role), cause: err}
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !opened.Mode().IsRegular() {
		return nil, fmt.Errorf("%s report must be a readable regular file", role)
	}
	if opened.Size() > reportdiff.MaxInputBytes {
		return nil, fmt.Errorf("%s report: %w", role, reportdiff.ErrLimit)
	}
	snapshot, err := reportdiff.Load(file)
	if err != nil {
		return nil, fmt.Errorf("%s report: %w", role, err)
	}
	return snapshot, nil
}

func writeComparison(out io.Writer, report *reportdiff.Report) error {
	if _, err := fmt.Fprintf(out, "Saved-report comparison: %s\nBase: %s\nHead: %s\nSource pairing is caller-selected; repository identity is not verified.\n", report.Status, report.Base.ReportSHA256, report.Head.ReportSHA256); err != nil {
		return err
	}
	shown, hidden := 0, 0
	for _, module := range report.Modules {
		if module.BaseStatus == "unavailable" && module.HeadStatus == "unavailable" && module.Compatibility == "unavailable" {
			continue
		}
		if _, err := fmt.Fprintf(out, "\n%s: %s (%s) — %d added, %d removed, %d changed, %d unchanged, %d unavailable", module.Name, module.Status, module.Compatibility, module.Counts.Added, module.Counts.Removed, module.Counts.Changed, module.Counts.Unchanged, module.Counts.Unavailable); err != nil {
			return err
		}
		if module.Counts.OmittedChanges > 0 {
			if _, err := fmt.Fprintf(out, ", %d omitted changes", module.Counts.OmittedChanges); err != nil {
				return err
			}
		}
		if _, err := fmt.Fprintln(out); err != nil {
			return err
		}
		for _, reason := range module.Reasons {
			if _, err := fmt.Fprintf(out, "  %s\n", strings.ReplaceAll(reason, "_", " ")); err != nil {
				return err
			}
		}
		for _, field := range module.Metadata {
			if _, err := fmt.Fprintf(out, "  metadata changed: %s\n", field.Field); err != nil {
				return err
			}
		}
		for _, change := range module.Changes {
			if shown >= 200 {
				hidden++
				continue
			}
			fields := make([]string, 0, len(change.Fields))
			for _, field := range change.Fields {
				fields = append(fields, field.Field)
			}
			if _, err := fmt.Fprintf(out, "  %s %q [%s]\n", change.Status, change.ID, strings.Join(fields, ", ")); err != nil {
				return err
			}
			shown++
		}
	}
	if hidden > 0 {
		if _, err := fmt.Fprintln(out, "\nText output shows at most 200 observations; use --json for the bounded structured comparison."); err != nil {
			return err
		}
	}
	return nil
}
