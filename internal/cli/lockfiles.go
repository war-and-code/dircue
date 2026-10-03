package cli

import (
	"fmt"
	"io"

	"github.com/war-and-code/dircue/pkg/lockfiles"
)

func writeLockfiles(out io.Writer, report *lockfiles.Report) error {
	if report == nil {
		return nil
	}
	if _, err := fmt.Fprintf(out, "Lockfile observations: %s\nChecks compare named declarations; resolved graphs and restore success are not verified.\n", report.Status); err != nil {
		return err
	}
	for _, c := range report.Contexts {
		if _, err := fmt.Fprintf(out, "%q: %s, lockfile %q (%s)\n", c.ManifestPath, c.Ecosystem, c.LockfilePath, c.AssociationState); err != nil {
			return err
		}
		for _, check := range c.Checks {
			if _, err := fmt.Fprintf(out, "  %s: %s (%d compared)\n", check.Name, check.Status, check.Compared); err != nil {
				return err
			}
		}
		for _, b := range c.Boundaries {
			if _, err := fmt.Fprintf(out, "  Boundary %q: %s\n", b.Path, b.Reason); err != nil {
				return err
			}
		}
	}
	return nil
}
