package cli

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/spf13/cobra"
	"github.com/war-and-code/dircue/pkg/mapdiff"
	"github.com/war-and-code/dircue/pkg/mapdoc"
)

const maxMapComparisonBytes = 64 << 20

func newMapCompareCommand(opts *options) *cobra.Command {
	n := opts.displayName
	var format string
	command := &cobra.Command{
		Use:   "compare <base-map.json> <head-map.json>",
		Short: "Compare two saved directory maps",
		Long: "Compare two explicitly selected " + n + " map documents by stable node and relationship IDs. The comparison opens neither source tree. It separates material observations from evidence and coverage changes, and does not claim a deletion when head coverage is incomplete.\n\n" +
			"Output formats (--format):\n" +
			"  text     Plain text summary (default when stdout is a terminal).\n" +
			"  markdown Markdown summary for $GITHUB_STEP_SUMMARY or a PR comment body.\n\n" +
			"--json supersedes --format and writes the full comparison document.\n\n" +
			"A valid comparison exits 0 regardless of whether changes are present; only I/O or usage errors produce a non-zero exit.",
		Example: "  " + n + " map --json old-checkout > before.json\n  " + n + " map --json new-checkout > after.json\n  " + n + " map compare --json before.json after.json\n  " + n + " map compare --format markdown before.json after.json >> \"$GITHUB_STEP_SUMMARY\"",
		Args: func(_ *cobra.Command, args []string) error {
			if len(args) != 2 {
				return fmt.Errorf("map compare requires base and head map files; see: %s map compare --help", n)
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := rejectMapSavedInputFlags(cmd, opts); err != nil {
				return err
			}
			if format != "text" && format != "markdown" {
				return fmt.Errorf("unsupported --format %q; choose text or markdown", format)
			}
			base, err := loadMapDocument(args[0], "base map", n)
			if err != nil {
				return err
			}
			head, err := loadMapDocument(args[1], "head map", n)
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
			if format == "markdown" {
				return mapdiff.WriteMarkdown(cmd.OutOrStdout(), report)
			}
			return writeMapComparison(cmd.OutOrStdout(), report)
		},
	}
	command.Flags().StringVar(&format, "format", "text", "Output format: text (default) or markdown")
	setSavedReportHelp(command)
	return command
}

func loadMapDocument(filename, role, displayName string) (mapdoc.Document, error) {
	file, err := openInputFile(filename, role)
	if err != nil {
		return mapdoc.Document{}, &diagnosticError{message: "cannot open " + role + "; supply a readable regular JSON document saved by " + displayName + " map --json", cause: err}
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
	// Check the document kind before full parsing so non-map documents get a
	// helpful error pointing to the right command. An empty kind means the
	// document has no top-level kind field; treat it like a legacy profile.
	if kind := documentKind(data); kind != "map" {
		if kind != "" {
			return mapdoc.Document{}, fmt.Errorf("%s is not a map document (kind: %q); use: %s compare", role, kind, displayName)
		}
		// No kind field — check for a profile-shaped document.
		if isProfileDocument(data) {
			return mapdoc.Document{}, fmt.Errorf("%s appears to be a legacy profile document (no kind field); use: %s compare", role, displayName)
		}
	}
	document, err := mapdoc.UnmarshalStrict(data)
	if err != nil {
		return mapdoc.Document{}, fmt.Errorf("%s: %w", role, err)
	}
	if document.Kind != "map" {
		return mapdoc.Document{}, fmt.Errorf("%s is not a map document (kind: %q); use: %s compare", role, document.Kind, displayName)
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
	if _, err := fmt.Fprintf(out, "Provider observations: %s", report.ProviderStatus); err != nil {
		return err
	}
	if len(report.ProviderChanges) > 0 {
		if _, err := fmt.Fprintf(out, " (%d changes)", len(report.ProviderChanges)); err != nil {
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
