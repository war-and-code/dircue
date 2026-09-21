package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"dircue/pkg/capabilities"
	"dircue/pkg/planning"
	"dircue/pkg/reportdiff"
	"github.com/spf13/cobra"
)

func newCapabilitiesCommand(opts *options) *cobra.Command {
	return &cobra.Command{Use: "capabilities", Short: "Describe modules supported by saved-report planning", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		if err := rejectPlanningAnalysisFlags(cmd); err != nil {
			return err
		}
		d := capabilities.Dircue(Version)
		if err := d.Validate(); err != nil {
			return err
		}
		if opts.json {
			return json.NewEncoder(cmd.OutOrStdout()).Encode(d)
		}
		return writeCapabilities(cmd.OutOrStdout(), d)
	}}
}

func newPlanCommand(opts *options) *cobra.Command {
	var modules, questions, projects, inputs []string
	command := &cobra.Command{Use: "plan <report.json>", Short: "Plan selected follow-up analysis from a saved profile", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if err := rejectPlanningAnalysisFlags(cmd); err != nil {
			return err
		}
		if len(modules)+len(questions) == 0 {
			return fmt.Errorf("select at least one --module or --question")
		}
		file, err := openInputFile(args[0], "saved report")
		if err != nil {
			return fmt.Errorf("cannot open saved report")
		}
		defer file.Close()
		info, err := file.Stat()
		if err != nil || !info.Mode().IsRegular() {
			return fmt.Errorf("saved report must be a readable regular file")
		}
		if info.Size() > reportdiff.MaxInputBytes {
			return fmt.Errorf("saved report: %w", reportdiff.ErrLimit)
		}
		p, digest, err := reportdiff.ReadEvidence(file)
		if err != nil {
			return fmt.Errorf("saved report: %w", err)
		}
		report, err := planning.Build(cmd.Context(), planning.Input{Profile: p, ReportSHA256: digest, Capabilities: capabilities.Dircue(Version), Selection: planning.Selection{Questions: questions, Modules: modules, Projects: projects, Inputs: inputs}})
		if err != nil {
			return err
		}
		if opts.json {
			return json.NewEncoder(cmd.OutOrStdout()).Encode(report)
		}
		return writePlan(cmd.OutOrStdout(), report)
	}}
	command.Flags().StringSliceVar(&modules, "module", nil, "Planner-supported module to consider (repeatable)")
	command.Flags().StringSliceVar(&questions, "question", nil, "Follow-up question to consider (repeatable)")
	command.Flags().StringArrayVar(&projects, "project", nil, "Root-relative project selector for focus planning")
	command.Flags().StringSliceVar(&inputs, "input", nil, "Caller-supplied prerequisite name, such as structural-worker (repeatable)")
	return command
}

func rejectPlanningAnalysisFlags(cmd *cobra.Command) error {
	for _, flag := range []string{"breakdown", "strategies", "workers", "max-file-bytes", "source", "rev", "tree-size"} {
		if cmd.Flags().Changed(flag) {
			return fmt.Errorf("--%s does not apply to saved-report planning", flag)
		}
	}
	return nil
}

func writeCapabilities(out io.Writer, d capabilities.Descriptor) error {
	if _, err := fmt.Fprintf(out, "Planner capabilities: %s (%s)\n", d.ProviderVersion, d.SchemaVersion); err != nil {
		return err
	}
	for _, m := range d.Modules {
		if _, err := fmt.Fprintf(out, "  %s: %s [%s]\n", m.ID, m.Question, m.Cost.Inspection); err != nil {
			return err
		}
	}
	return nil
}

func writePlan(out io.Writer, r *planning.Report) error {
	if _, err := fmt.Fprintf(out, "Follow-up plan: %s\nSaved report: %s\nDeclared root is evidence only; every command requires caller revalidation.\n", r.Status, r.Identity.ReportSHA256); err != nil {
		return err
	}
	for _, d := range r.Decisions {
		if _, err := fmt.Fprintf(out, "  %s: %s", d.Module, d.Applicability); err != nil {
			return err
		}
		if len(d.Reasons) > 0 {
			if _, err := fmt.Fprintf(out, " — %s", strings.Join(d.Reasons, "; ")); err != nil {
				return err
			}
		}
		if _, err := fmt.Fprintln(out); err != nil {
			return err
		}
	}
	return nil
}
