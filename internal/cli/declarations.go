package cli

import (
	"fmt"
	"io"

	"dircue/pkg/declarations"
)

func writeProjectDeclarations(out io.Writer, r *declarations.Report) error {
	if r == nil {
		return nil
	}
	if _, err := fmt.Fprintf(out, "Project declarations; source: %s; status: %s\n", r.Source, r.Status); err != nil {
		return err
	}
	for _, p := range r.Projects {
		if _, err := fmt.Fprintf(out, "%q: %s", p.ID, p.Kind); err != nil {
			return err
		}
		if p.Name != "" {
			if _, err := fmt.Fprintf(out, "; name: %q", p.Name); err != nil {
				return err
			}
		}
		if p.Version != "" {
			if _, err := fmt.Fprintf(out, "; version: %q", p.Version); err != nil {
				return err
			}
		}
		if _, err := fmt.Fprintln(out); err != nil {
			return err
		}
		if err := writeDeclarations(out, p.ID, p.Requirements, p.References); err != nil {
			return err
		}
		for _, v := range p.Interfaces {
			if _, err := fmt.Fprintf(out, "  %s %q (%s)", v.Kind, v.Name, v.State); err != nil {
				return err
			}
			if v.Target != "" {
				if _, err := fmt.Fprintf(out, " -> %q", v.Target); err != nil {
					return err
				}
			}
			if v.Condition != "" {
				if _, err := fmt.Fprintf(out, "; condition: %q", v.Condition); err != nil {
					return err
				}
			}
			if _, err := fmt.Fprintln(out); err != nil {
				return err
			}
		}
	}
	for _, d := range r.Diagnostics {
		if _, err := fmt.Fprintf(out, "%q: %s (%s)\n", d.Path, d.Message, d.Code); err != nil {
			return err
		}
	}
	_, err := fmt.Fprintf(out, "Parsed %d of %d manifest candidates; retained %d observations. Declarations do not establish build success.\n", r.Coverage.ParsedManifests, r.Coverage.ManifestCandidates, r.Coverage.RetainedObservations)
	return err
}
