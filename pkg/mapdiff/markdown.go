package mapdiff

import (
	"fmt"
	"io"
	"strings"
)

// WriteMarkdown renders the comparison report as a concise Markdown summary
// suitable for $GITHUB_STEP_SUMMARY or a pull-request comment body. It never
// executes inspected content and never contacts the network.
//
// The output groups changes by kind (components, deployables, interfaces,
// capabilities, edges). Indeterminate removals and partial coverage are shown
// rather than silently omitted. Noise controls are not yet applied; a future
// opt-in can suppress changes whose sole reason is a test-fixture or
// generated-content role.
func WriteMarkdown(out io.Writer, report Report) error {
	// Headline status line.
	statusLabel := map[string]string{
		"unchanged":     "No material changes",
		"changed":       fmt.Sprintf("%d material change(s)", report.Counts.Material),
		"incomparable":  "Incomparable maps",
		"indeterminate": "Indeterminate (coverage incomplete)",
	}
	status, ok := statusLabel[report.Status]
	if !ok {
		status = report.Status
	}
	if _, err := fmt.Fprintf(out, "## Architecture diff: %s\n\n", status); err != nil {
		return err
	}

	// Source binding.
	if _, err := fmt.Fprintf(out, "**Source binding:** %s | **Observer compatibility:** %s\n\n", report.SourceBinding, report.ObserverCompatibility); err != nil {
		return err
	}

	// Summary table.
	if _, err := fmt.Fprintf(out, "| Added | Removed | Changed | Unchanged |\n|---|---|---|---|\n| %d | %d | %d | %d |\n\n",
		report.Counts.Added, report.Counts.Removed, report.Counts.Changed, report.Counts.Unchanged); err != nil {
		return err
	}

	// Group material changes by entity kind bucket.
	type bucket struct {
		label   string
		changes []Change
	}
	buckets := []bucket{
		{label: "Components"},
		{label: "Deployables"},
		{label: "Interfaces"},
		{label: "Capabilities"},
		{label: "Packages"},
		{label: "Edges"},
		{label: "Other"},
	}
	// entityKindBucket maps entity+node-kind combinations to bucket index.
	kindBucket := func(c Change) int {
		if c.Entity == "edge" {
			return 5
		}
		switch c.Kind {
		case "component":
			return 0
		case "deployable":
			return 1
		case "interface":
			return 2
		case "capability":
			return 3
		case "package":
			return 4
		default:
			return 6
		}
	}
	for _, c := range report.Changes {
		if !c.Material {
			continue
		}
		idx := kindBucket(c)
		buckets[idx].changes = append(buckets[idx].changes, c)
	}

	hasSection := false
	for _, b := range buckets {
		if len(b.changes) == 0 {
			continue
		}
		hasSection = true
		if _, err := fmt.Fprintf(out, "### %s\n\n", b.label); err != nil {
			return err
		}
		for _, c := range b.changes {
			icon := map[string]string{"added": "+", "removed": "-", "changed": "~"}[c.Status]
			if icon == "" {
				icon = "?"
			}
			name := c.Label
			if name == "" {
				name = c.ID
			}
			if b.label == "Other" && c.Kind != "" {
				name = c.Kind + ": " + name
			}
			certainty := ""
			if c.Certainty == "indeterminate" || c.Certainty == "incomplete_coverage" {
				certainty = " _(coverage incomplete)_"
			}
			fields := ""
			if len(c.Fields) > 0 {
				fields = " [" + strings.Join(c.Fields, ", ") + "]"
			}
			reason := ""
			if c.Reason != "" {
				reason = ": " + c.Reason
			}
			if _, err := fmt.Fprintf(out, "- `%s` **%s** `%s`%s%s%s\n", icon, c.Status, name, certainty, fields, reason); err != nil {
				return err
			}
		}
		if _, err := fmt.Fprintln(out); err != nil {
			return err
		}
	}
	if !hasSection && report.Status == "unchanged" {
		if _, err := fmt.Fprintln(out, "_No material differences found._"); err != nil {
			return err
		}
	}

	// Indeterminate removals.
	if report.Counts.IndeterminateRemoval > 0 {
		if _, err := fmt.Fprintf(out, "> **Note:** %d removal(s) are indeterminate — the head map's coverage is incomplete for those observations, so absence cannot be confirmed as deletion.\n\n", report.Counts.IndeterminateRemoval); err != nil {
			return err
		}
	}

	// Coverage changes.
	if len(report.CoverageLedgerChanges) > 0 {
		if _, err := fmt.Fprintf(out, "**Provider coverage:** %s (%d change(s))\n\n", report.CoverageLedgerStatus, len(report.CoverageLedgerChanges)); err != nil {
			return err
		}
	}

	// Caveats.
	for _, caveat := range report.Caveats {
		if _, err := fmt.Fprintf(out, "> **Caveat:** %s\n", caveat); err != nil {
			return err
		}
	}
	if len(report.Caveats) > 0 {
		if _, err := fmt.Fprintln(out); err != nil {
			return err
		}
	}

	return nil
}
