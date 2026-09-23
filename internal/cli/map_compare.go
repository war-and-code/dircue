package cli

import (
	"encoding/json"
	"fmt"
	"io"

	"dircue/pkg/mapdiff"
	"dircue/pkg/mapdoc"
	"github.com/spf13/cobra"
)

const maxMapComparisonBytes = 64 << 20

func newMapCompareCommand(opts *options) *cobra.Command {
	command := &cobra.Command{
		Use:     "compare <base-map.json> <head-map.json>",
		Short:   "Compare two saved directory maps",
		Long:    "Compare two explicitly selected dircue map documents by stable node and relationship IDs. The comparison opens neither source tree. It separates material observations from evidence and coverage changes, and does not claim a deletion when head coverage is incomplete.",
		Example: "  dircue map --json old-checkout > before.json\n  dircue map --json new-checkout > after.json\n  dircue map compare --json before.json after.json",
		Args: func(_ *cobra.Command, args []string) error {
			if len(args) != 2 {
				return fmt.Errorf("map compare requires base and head map files")
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := rejectMapSavedInputFlags(cmd, opts); err != nil {
				return err
			}
			base, err := loadMapDocument(args[0], "base map")
			if err != nil {
				return err
			}
			head, err := loadMapDocument(args[1], "head map")
			if err != nil {
				return err
			}
			report, err := mapdiff.Compare(base, head)
			if err != nil {
				return err
			}
			if opts.json {
				return json.NewEncoder(cmd.OutOrStdout()).Encode(report)
			}
			return writeMapComparison(cmd.OutOrStdout(), report)
		},
	}
	setSavedReportHelp(command)
	return command
}

func loadMapDocument(filename, role string) (mapdoc.Document, error) {
	file, err := openInputFile(filename, role)
	if err != nil {
		return mapdoc.Document{}, &diagnosticError{message: "cannot open " + role + "; supply a readable regular JSON document saved by dircue map --json", cause: err}
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return mapdoc.Document{}, fmt.Errorf("%s must be a readable regular file", role)
	}
	if info.Size() > maxMapComparisonBytes {
		return mapdoc.Document{}, fmt.Errorf("%s exceeds the %d-byte input limit", role, maxMapComparisonBytes)
	}
	data, err := io.ReadAll(io.LimitReader(file, maxMapComparisonBytes+1))
	if err != nil {
		return mapdoc.Document{}, fmt.Errorf("read %s: %w", role, err)
	}
	if len(data) > maxMapComparisonBytes {
		return mapdoc.Document{}, fmt.Errorf("%s exceeds the %d-byte input limit", role, maxMapComparisonBytes)
	}
	document, err := mapdoc.UnmarshalStrict(data)
	if err != nil {
		return mapdoc.Document{}, fmt.Errorf("%s: %w", role, err)
	}
	return document, nil
}

func writeMapComparison(out io.Writer, report mapdiff.Report) error {
	if _, err := fmt.Fprintf(out, "Map comparison: %s\nSource binding: %s\nObserver compatibility: %s\nMaterial changes: %d\nObserved entries: %d added, %d removed, %d changed, %d unchanged\n", report.Status, report.SourceBinding, report.ObserverCompatibility, report.Counts.Material, report.Counts.Added, report.Counts.Removed, report.Counts.Changed, report.Counts.Unchanged); err != nil {
		return err
	}
	if report.Counts.IndeterminateRemoval > 0 {
		if _, err := fmt.Fprintf(out, "Indeterminate removals: %d\n", report.Counts.IndeterminateRemoval); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintf(out, "Provider coverage: %s", report.CoverageLedgerStatus); err != nil {
		return err
	}
	if len(report.CoverageLedgerChanges) > 0 {
		if _, err := fmt.Fprintf(out, " (%d changes)", len(report.CoverageLedgerChanges)); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintln(out); err != nil {
		return err
	}
	for _, caveat := range report.Caveats {
		if _, err := fmt.Fprintf(out, "Caveat: %s\n", caveat); err != nil {
			return err
		}
	}
	for _, change := range report.Changes {
		fields := ""
		if len(change.Fields) > 0 {
			fields = " [" + joinComma(change.Fields) + "]"
		}
		if _, err := fmt.Fprintf(out, "%s %s %s (%s)%s\n", change.Status, change.Entity, change.ID, change.Certainty, fields); err != nil {
			return err
		}
	}
	return nil
}

func joinComma(values []string) string {
	result := ""
	for i, value := range values {
		if i > 0 {
			result += ", "
		}
		result += value
	}
	return result
}
