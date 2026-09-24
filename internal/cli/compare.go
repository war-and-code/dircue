package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"github.com/war-and-code/dircue/pkg/reportdiff"
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
		return nil, &diagnosticError{message: savedReportOpenErrorMessage(role + " report"), cause: err}
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !opened.Mode().IsRegular() {
		return nil, fmt.Errorf("%s report must be a readable regular file", role)
	}
	if opened.Size() > reportdiff.MaxInputBytes {
		return nil, fmt.Errorf("%s report: %w", role, reportdiff.ErrLimit)
	}
	data, err := io.ReadAll(io.LimitReader(file, reportdiff.MaxInputBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read %s report: %w", role, err)
	}
	if documentKind(data) == "map" {
		return nil, fmt.Errorf("%s report is a map document (kind: \"map\"); use: dircue map compare", role)
	}
	snapshot, err := reportdiff.Load(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("%s report: %w", role, err)
	}
	return snapshot, nil
}

// documentKind peeks at the top-level "kind" string of a JSON document without
// fully parsing it. Returns an empty string when the field is absent or the
// document is not a JSON object.
func documentKind(data []byte) string {
	var envelope struct {
		Kind string `json:"kind"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		return ""
	}
	return envelope.Kind
}

// isProfileDocument returns true when data looks like a legacy dircue profile:
// a JSON object that has a "schema_version" field but no top-level "kind" field.
// This allows map compare to emit a helpful cross-command error.
func isProfileDocument(data []byte) bool {
	var envelope struct {
		SchemaVersion string `json:"schema_version"`
		Root          string `json:"root"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		return false
	}
	return envelope.SchemaVersion != "" && envelope.Root != ""
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
			if _, err := fmt.Fprintf(out, "  %s %s [%s]\n", change.Status, comparisonTextID(module.Name, change.ID), strings.Join(fields, ", ")); err != nil {
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

// comparisonTextID renders internal length-prefixed identities as quoted
// components. JSON retains the original IDs so saved-report consumers can
// continue to match them exactly.
func comparisonTextID(module, id string) string {
	prefix, count := "", 0
	switch module {
	case "ecosystems", "frameworks", "layouts":
		count = 4
	case "registries":
		count = 2
	case "focus_context":
		count = 5
	case "focus_relations":
		count = 6
	case "availability_sparse", "availability_diagnostics":
		count = 3
	case "focus_affected_projects", "availability_references":
		count = 4
	case "projects":
		prefix, count = "composition:", 2
	case "package_evidence":
		prefix, count = "relationship:", 5
	case "metrics":
		prefix, count = "language:", 2
	case "rules":
		prefix, count = "observation:", 3
	case "focus_related":
		prefix, count = "file:", 2
	case "focused_metrics_primary":
		prefix, count = "primary:language:", 2
	case "focused_metrics_related":
		if strings.HasPrefix(id, "project:") {
			rest := strings.TrimPrefix(id, "project:")
			var display string
			for offset := 0; offset < len(rest); {
				index := strings.Index(rest[offset:], ":language:")
				if index < 0 {
					break
				}
				index += offset
				project := rest[:index]
				components, ok := decodeComparisonKey(rest[index+len(":language:"):], 2)
				if ok {
					if display != "" {
						// The raw project prefix can itself contain :language:.
						// Multiple valid splits cannot be attributed safely.
						return strconv.Quote(id)
					}
					display = "project " + strconv.Quote(project) + " language " + formatComparisonComponents(components)
				}
				offset = index + 1
			}
			if display != "" {
				return display
			}
		}
	case "discovery":
		for _, choice := range []struct {
			prefix string
			count  int
		}{{"category:", 2}, {"role:", 2}, {"candidate-count:", 2}, {"candidate:", 3}} {
			if strings.HasPrefix(id, choice.prefix) {
				prefix, count = choice.prefix, choice.count
				break
			}
		}
	}
	if count == 0 || !strings.HasPrefix(id, prefix) {
		return strconv.Quote(id)
	}
	components, ok := decodeComparisonKey(strings.TrimPrefix(id, prefix), count)
	if !ok {
		return strconv.Quote(id)
	}
	return prefix + formatComparisonComponents(components)
}

func formatComparisonComponents(components []string) string {
	for i := range components {
		components[i] = strconv.Quote(components[i])
	}
	return "(" + strings.Join(components, ", ") + ")"
}

func decodeComparisonKey(encoded string, count int) ([]string, bool) {
	parts := make([]string, 0, count)
	for range count {
		colon := strings.IndexByte(encoded, ':')
		if colon < 1 {
			return nil, false
		}
		for i := 0; i < colon; i++ {
			if encoded[i] < '0' || encoded[i] > '9' {
				return nil, false
			}
		}
		size, err := strconv.Atoi(encoded[:colon])
		encoded = encoded[colon+1:]
		if err != nil || size > len(encoded) {
			return nil, false
		}
		parts = append(parts, encoded[:size])
		encoded = encoded[size:]
	}
	return parts, encoded == ""
}
