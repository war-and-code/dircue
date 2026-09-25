package cli

import (
	"fmt"
	"io"
	"slices"

	"github.com/spf13/cobra"
	"github.com/war-and-code/dircue/pkg/availability"
	"github.com/war-and-code/dircue/pkg/profile"
)

func addFocusFlags(command *cobra.Command, opts *options) {
	command.Short = "Profile a declared project with its original-root context"
	command.Long = "Select a .NET or Python/uv project by its root-relative manifest path. The plan uses static declarations and directory ownership, not evaluated compiler inputs. Shared declarations remain context; metrics count only the primary project and separately requested related projects. The original root inventory is still traversed."
	command.Example = "  dircue analyze focus --project services/app/app.csproj --metrics --json .\n  dircue analyze focus --affected-by Directory.Build.props --json ."
	command.Flags().StringVar(&opts.focusProject, "project", "", "Primary root-relative .NET or Python/uv manifest path")
	command.Flags().StringArrayVar(&opts.focusRelated, "related-project", nil, "Explicit additional project to profile separately (repeatable)")
	command.Flags().StringVar(&opts.focusAffectedBy, "affected-by", "", "Query projects with retained context evidence for this root-relative input")
	command.Flags().BoolVar(&opts.metrics, "metrics", false, "Count lines separately for the primary and explicitly selected related projects")
}

func validateFocusFlags(cmd *cobra.Command, opts *options) error {
	if (opts.focusProject == "") == (opts.focusAffectedBy == "") {
		return fmt.Errorf("focus requires exactly one of --project or --affected-by")
	}
	if opts.focusAffectedBy != "" && (len(opts.focusRelated) > 0 || opts.metrics) {
		return fmt.Errorf("--affected-by cannot be combined with --related-project or --metrics")
	}
	for _, flag := range []string{"breakdown", "strategies"} {
		if cmd.Flags().Changed(flag) {
			return fmt.Errorf("--%s is not supported by focus; use --metrics --files for file counts", flag)
		}
	}
	return nil
}

func writeFocus(out io.Writer, report *profile.Report) error {
	r := report.Focus
	if r == nil {
		return nil
	}
	if _, err := fmt.Fprintf(out, "Focus: %s (%s)\nScope: %s\n", r.Scope.Role, r.Status, r.Scope.ID); err != nil {
		return err
	}
	if r.PrimaryProject != nil {
		if _, err := fmt.Fprintf(out, "Primary: %q\nPopulation: %d files, %d bytes\n", r.PrimaryProject.ID, r.Coverage.PrimaryFiles, r.Coverage.PrimaryBytes); err != nil {
			return err
		}
	}
	for _, input := range r.Context {
		if _, err := fmt.Fprintf(out, "Context: %q for %q (%s; %s)\n", input.Path, input.ProjectID, input.Basis, input.Applicability); err != nil {
			return err
		}
	}
	if r.AffectedProjects != nil {
		for _, project := range r.AffectedProjects.Projects {
			if _, err := fmt.Fprintf(out, "Candidate project: %q (%s; %s)\n", project.ProjectID, project.Basis, project.Applicability); err != nil {
				return err
			}
		}
	}
	for _, omission := range r.Omissions {
		if _, err := fmt.Fprintf(out, "Omitted: %s (%d)\n", omission.Reason, omission.Count); err != nil {
			return err
		}
	}
	if report.FocusedMetrics != nil {
		if _, err := fmt.Fprintln(out, "Primary metrics:"); err != nil {
			return err
		}
		if err := writeMetrics(out, report.FocusedMetrics.Primary, report.FocusedMetrics.Primary != nil && report.FocusedMetrics.Primary.Files != nil); err != nil {
			return err
		}
		for _, related := range report.FocusedMetrics.Related {
			if _, err := fmt.Fprintf(out, "Related metrics: %q\n", related.Project); err != nil {
				return err
			}
			if err := writeMetrics(out, related.Metrics, related.Metrics != nil && related.Metrics.Files != nil); err != nil {
				return err
			}
		}
	}
	return nil
}

func writeAvailability(out io.Writer, r *availability.Report) error {
	if r == nil {
		return nil
	}
	if _, err := fmt.Fprintf(out, "Source availability: %s (%s)\nLFS pointers: %d; pointer-like files: %d; Gitlinks: %d\nInspected prefixes: %d; content bytes read: %d\n", r.Status, r.Source.Consistency, r.Counts.ValidPointers, r.Counts.PointerLikeFiles, r.Counts.Gitlinks, r.Coverage.PointerInspections, r.Coverage.ContentBytesRead); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(out, "Inventory complete: %t; checkout metadata inspected: %t, complete: %t\n", r.Coverage.SelectedInventoryComplete, r.Coverage.CheckoutMetadataInspected, r.Coverage.CheckoutMetadataComplete); err != nil {
		return err
	}
	reasons := make([]string, 0, len(r.Omissions))
	for reason := range r.Omissions {
		reasons = append(reasons, reason)
	}
	slices.Sort(reasons)
	for _, reason := range reasons {
		if _, err := fmt.Fprintf(out, "Omitted: %s (%d)\n", reason, r.Omissions[reason]); err != nil {
			return err
		}
	}
	for _, d := range r.Diagnostics {
		if _, err := fmt.Fprintf(out, "Diagnostic: %q: %s (%s)\n", d.Path, d.Message, d.Code); err != nil {
			return err
		}
	}
	for _, pointer := range r.LFS {
		if _, err := fmt.Fprintf(out, "%q: %s (%s)\n", pointer.Path, pointer.Kind, pointer.LFSAttribute); err != nil {
			return err
		}
	}
	for _, link := range r.Gitlinks {
		if _, err := fmt.Fprintf(out, "%q: gitlink %s\n", link.Path, link.Commit); err != nil {
			return err
		}
	}
	for _, reference := range r.References {
		if _, err := fmt.Fprintf(out, "Reference %q: %s\n", reference.Target, reference.Qualification); err != nil {
			return err
		}
	}
	return nil
}
