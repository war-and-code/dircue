package cli

import (
	"fmt"
	"io"
	"strconv"
	"text/tabwriter"

	"github.com/war-and-code/dircue/pkg/profile"
)

func writeMetrics(out io.Writer, report *profile.MetricsReport, includeFiles bool) error {
	if report == nil {
		return nil
	}
	if _, err := fmt.Fprintf(out, "%s %s; scope: %s; status: %s\n", report.Engine, report.EngineVersion, report.Scope, report.Status); err != nil {
		return err
	}
	table := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
	if _, err := fmt.Fprintln(table, "Language\tFiles\tBytes\tLines\tCode\tComment\tBlank\tComplexity"); err != nil {
		return err
	}
	for _, language := range report.Languages {
		c := language.Counts
		if _, err := fmt.Fprintf(table, "%s\t%d\t%d\t%d\t%d\t%d\t%d\t%d\n", language.Language, c.Files, c.Bytes, c.Lines, c.Code, c.Comment, c.Blank, c.Complexity); err != nil {
			return err
		}
	}
	c := report.Totals
	if _, err := fmt.Fprintf(table, "Total\t%d\t%d\t%d\t%d\t%d\t%d\t%d\n", c.Files, c.Bytes, c.Lines, c.Code, c.Comment, c.Blank, c.Complexity); err != nil {
		return err
	}
	if err := table.Flush(); err != nil {
		return err
	}
	for _, skip := range report.Skipped {
		count := "unknown"
		if skip.Files != nil {
			count = strconv.FormatInt(*skip.Files, 10)
		}
		if _, err := fmt.Fprintf(out, "Skipped: %s (%s)\n", count, skip.Reason); err != nil {
			return err
		}
	}
	if includeFiles && report.Files != nil {
		for _, file := range *report.Files {
			if file.Counts != nil {
				c := file.Counts
				if _, err := fmt.Fprintf(out, "%q\t%s\t%d lines\t%d code\t%d comment\t%d blank\t%d complexity\n", file.Path, file.Language, c.Lines, c.Code, c.Comment, c.Blank, c.Complexity); err != nil {
					return err
				}
			} else if _, err := fmt.Fprintf(out, "%q\tskipped (%s)\n", file.Path, file.Reason); err != nil {
				return err
			}
		}
	}
	return nil
}
