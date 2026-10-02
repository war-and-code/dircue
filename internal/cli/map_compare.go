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
	var exitCode bool
	var onUncertain string
	command := &cobra.Command{
		Use:   "compare <base-map.json> <head-map.json>",
		Short: "Compare two saved directory maps",
		Long: "Compare two explicitly selected " + n + " map documents by stable node and relationship IDs. The comparison opens neither source tree. It separates material observations from evidence and coverage changes, and does not claim a deletion when head coverage is incomplete.\n\n" +
			"Output formats (--format):\n" +
			"  text     Plain text summary (default when stdout is a terminal).\n" +
			"  markdown Markdown summary for $GITHUB_STEP_SUMMARY or a PR comment body.\n\n" +
			"--json supersedes --format and writes the full comparison document.\n\n" +
			"By default, a valid comparison exits 0 regardless of changes. With --exit-code, unchanged exits 0, changed exits 1, and uncertain exits 2. Uncertainty includes indeterminate removals, incomplete source binding, decreased question coverage, or differing observer identities. --on-uncertain allow explicitly permits an uncertain result to use its observed changed/unchanged exit; the report still retains its uncertainty. Handled errors, including input and output failures, exit 3 and print a diagnostic. Comparison reports are written before an outcome exit.",
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
			if onUncertain != "fail" && onUncertain != "allow" {
				return fmt.Errorf("unsupported --on-uncertain %q; choose fail or allow", diagnosticValue(onUncertain))
			}
			if cmd.Flags().Changed("on-uncertain") && !exitCode {
				return fmt.Errorf("--on-uncertain requires --exit-code")
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
			var writeErr error
			switch {
			case opts.json:
				writeErr = json.NewEncoder(cmd.OutOrStdout()).Encode(report)
			case format == "markdown":
				writeErr = mapdiff.WriteMarkdown(cmd.OutOrStdout(), report)
			default:
				writeErr = writeMapComparison(cmd.OutOrStdout(), report)
			}
			if writeErr != nil {
				return writeErr
			}
			if exitCode {
				if onUncertain == "fail" && (!comparisonSourceBound(base) || !comparisonSourceBound(head)) {
					return &comparisonExit{code: 2, status: "uncertain"}
				}
				return comparisonOutcome(report, onUncertain)
			}
			return nil
		},
	}
	command.Flags().StringVar(&format, "format", "text", "Output format: text (default) or markdown")
	command.Flags().BoolVar(&exitCode, "exit-code", false, "Exit 1 for changed, 2 for uncertain, or 3 for handled errors; unchanged exits 0")
	command.Flags().StringVar(&onUncertain, "on-uncertain", "fail", "With --exit-code, uncertainty: fail (exit 2) or allow the observed changed/unchanged exit")
	setSavedReportHelp(command)
	return command
}

// comparisonExit is an outcome after a complete report was written, not a
// diagnostic error. Main uses its status without printing an error message.
type comparisonExit struct {
	code   int
	status string
}

func (e *comparisonExit) Error() string { return "map comparison: " + e.status }

func comparisonOutcome(report mapdiff.Report, onUncertain string) error {
	unboundDirectory := func(source mapdoc.Source) bool { return source.Mode == "directory" && source.Digest == nil }
	uncertain := report.Status == "indeterminate" || report.Counts.IndeterminateRemoval > 0 || report.SourceBinding == "unknown" || unboundDirectory(report.Base) || unboundDirectory(report.Head) || report.ObserverCompatibility == "different" || comparisonCoverageDecreased(report)
	if uncertain && onUncertain == "fail" {
		return &comparisonExit{code: 2, status: "uncertain"}
	}
	if report.Status == "changed" {
		return &comparisonExit{code: 1, status: report.Status}
	}
	return nil
}

func comparisonSourceBound(doc mapdoc.Document) bool {
	for _, coverage := range doc.Coverage {
		if coverage.Question == mapdoc.QuestionSourceBinding && coverage.Scope == "." {
			return coverage.Status == mapdoc.CoverageComplete
		}
	}
	return false
}

func comparisonCoverageDecreased(report mapdiff.Report) bool {
	rank := func(status mapdoc.CoverageStatus) int {
		switch status {
		case mapdoc.CoverageComplete:
			return 3
		case mapdoc.CoveragePartial:
			return 2
		case mapdoc.CoverageUnknown:
			return 1
		default:
			return 0
		}
	}
	for _, change := range report.CoverageChanges {
		if change.Base.Present && (!change.Head.Present || rank(change.Head.Status) < rank(change.Base.Status)) {
			return true
		}
	}
	return false
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
