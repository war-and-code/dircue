package cli

import (
	"fmt"
	"io"

	"dircue/pkg/structure"
)

func writeHotspots(out io.Writer, r *structure.HotspotReport) error {
	if r == nil {
		return nil
	}
	if _, err := fmt.Fprintf(out, "Function hotspots: %s; status: %s; file coverage: %s\nMeasured %d function spaces in %d files; invalid spans: %d; recovered files: %d.\nRankings cover measured populations, with nested spaces that may overlap. They are not quality grades.\n", r.Provider, r.Status, r.FileCoverageStatus, r.TotalSpaces, r.AnalyzedFiles, r.InvalidSpanSpaces, r.RecoveredFiles); err != nil {
		return err
	}
	for _, group := range r.Groups {
		if _, err := fmt.Fprintf(out, "%s (%s syntax; %s): %d files\n", group.Language, group.SyntaxCohort, group.Grammar, group.AnalyzedFiles); err != nil {
			return err
		}
		for _, metric := range group.Metrics {
			if _, err := fmt.Fprintf(out, "  %s: %d measured", metric.Metric, metric.Count); err != nil {
				return err
			}
			if metric.Min != nil && metric.Max != nil {
				if _, err := fmt.Fprintf(out, "; min %d, max %d %s", *metric.Min, *metric.Max, metric.Unit); err != nil {
					return err
				}
			}
			if _, err := fmt.Fprintln(out); err != nil {
				return err
			}
			for _, entry := range metric.Top {
				displayPath := entry.Path
				if entry.PathStatus == "omitted" {
					displayPath = "[omitted; sha256:" + entry.PathSHA256 + "]"
				}
				if _, err := fmt.Fprintf(out, "    %d: %q:%d-%d %q (name: %s)\n", entry.Value, displayPath, entry.StartLine, entry.EndLine, entry.Name, entry.NameStatus); err != nil {
					return err
				}
			}
		}
	}
	return nil
}
