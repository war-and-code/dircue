package cli

import (
	"fmt"
	"io"

	"github.com/war-and-code/dircue/pkg/environments"
)

func writeEnvironments(out io.Writer, report *environments.Report) error {
	if report == nil {
		return nil
	}
	if _, err := fmt.Fprintf(out, "Declared environments: %s\nProject contexts: %d; retained requirements: %d\nSelection starts are modeled from project roots; installed tools and build success are not checked.\n", report.Status, report.Coverage.Contexts, len(report.Requirements)); err != nil {
		return err
	}
	for _, r := range report.Requirements {
		if _, err := fmt.Fprintf(out, "%q: %s %q (%s; %s), evidence %q\n", r.ProjectID, r.Kind, r.Value, r.State, r.Applicability, r.Evidence); err != nil {
			return err
		}
	}
	for _, s := range report.Selections {
		if _, err := fmt.Fprintf(out, "SDK context %q: global.json %q, version %q, roll-forward %q (%s)\n", s.ProjectID, s.GlobalJSON, s.SDKVersion, s.RollForward, s.State); err != nil {
			return err
		}
	}
	for _, d := range report.ToolchainDeclarations {
		if _, err := fmt.Fprintf(out, "Toolchain %q: %s/%s %q (%s; scope %q; %s)\n", d.SourcePath, d.Tool, d.Kind, d.Values, d.State, d.ScopeDirectory, d.Applicability); err != nil {
			return err
		}
	}
	for _, c := range report.Conflicts {
		if _, err := fmt.Fprintf(out, "Conflict %q: %s %q (%s)\n", c.ContextID, c.Dimension, c.Values, c.Explanation); err != nil {
			return err
		}
	}
	for _, b := range report.Boundaries {
		if _, err := fmt.Fprintf(out, "Unresolved %q: %s\n", b.ProjectID, b.Reason); err != nil {
			return err
		}
	}
	return nil
}
