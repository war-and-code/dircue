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
		Use:   "compare <base.json> <head.json>",
		Short: "Compare two saved profiles without rescanning their sources",
		Long:  "Compare explicitly selected aggregate dircue JSON reports. Compatibility is checked per module; missing provenance and partial coverage limit conclusions. Evidence paths are never opened. Successful comparisons return zero even when observations differ.",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			for _, flag := range []string{"breakdown", "strategies", "workers", "max-file-bytes", "source", "rev", "tree-size"} {
				if cmd.Flags().Changed(flag) {
					return fmt.Errorf("--%s does not apply to saved-report comparison", flag)
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
	return command
}

func loadComparisonFile(name, role string) (*reportdiff.Snapshot, error) {
	file, err := openInputFile(name, role+" report")
	if err != nil {
		return nil, fmt.Errorf("cannot open %s report", role)
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
	shown := 0
	for _, module := range report.Modules {
		if module.BaseStatus == "unavailable" && module.HeadStatus == "unavailable" && module.Compatibility == "unavailable" {
			continue
		}
		if _, err := fmt.Fprintf(out, "\n%s: %s (%s) — %d added, %d removed, %d changed, %d unchanged, %d unavailable\n", module.Name, module.Status, module.Compatibility, module.Counts.Added, module.Counts.Removed, module.Counts.Changed, module.Counts.Unchanged, module.Counts.Unavailable); err != nil {
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
	if shown == 200 {
		if _, err := fmt.Fprintln(out, "\nText output shows at most 200 observations; use --json for the bounded structured comparison."); err != nil {
			return err
		}
	}
	return nil
}
