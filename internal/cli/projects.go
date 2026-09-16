package cli

import (
	"dircue/pkg/profile"
	"dircue/pkg/projects"
	"fmt"
	"io"
	"slices"
	"text/tabwriter"
)

func writeProjects(out io.Writer, r *projects.Report) error {
	if r == nil {
		return nil
	}
	if _, err := fmt.Fprintf(out, "Project inventory; source: %s; status: %s\n", r.Source, r.Status); err != nil {
		return err
	}
	t := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
	if _, err := fmt.Fprintln(t, "Project\tKind\tFiles\tBytes"); err != nil {
		return err
	}
	for _, p := range r.Projects {
		if _, err := fmt.Fprintf(t, "%q\t%s\t%d\t%d\n", p.ID, p.Kind, p.Files, p.Bytes); err != nil {
			return err
		}
	}
	if err := t.Flush(); err != nil {
		return err
	}
	for _, p := range r.Projects {
		if err := writeDeclarations(out, p.ID, p.Requirements, p.References); err != nil {
			return err
		}
	}
	for _, cfg := range r.Configurations {
		if _, err := fmt.Fprintf(out, "Configuration: %q (%d declarations, %d references)\n", cfg.Path, len(cfg.Requirements), len(cfg.References)); err != nil {
			return err
		}
		if err := writeDeclarations(out, cfg.Path, cfg.Requirements, cfg.References); err != nil {
			return err
		}
	}
	for _, role := range r.Composition {
		if _, err := fmt.Fprintf(out, "%s: %d files, %d bytes (%s)\n", role.Name, role.Files, role.Bytes, role.Basis); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintf(out, "Unassigned: %d files; ambiguous ownership: %d files\n", r.Unassigned.Files, r.Ambiguous.Files); err != nil {
		return err
	}
	for _, d := range r.Diagnostics {
		if _, err := fmt.Fprintf(out, "%q: %s (%s)\n", d.Path, d.Message, d.Code); err != nil {
			return err
		}
	}
	return nil
}
func writeDeclarations(out io.Writer, path string, requirements []projects.Requirement, references []projects.Reference) error {
	for _, q := range requirements {
		if _, err := fmt.Fprintf(out, "%q: %s = %q (%s", path, q.Kind, q.Value, q.State); err != nil {
			return err
		}
		if q.Condition != "" {
			if _, err := fmt.Fprintf(out, "; condition: %q", q.Condition); err != nil {
				return err
			}
		}
		if _, err := fmt.Fprintln(out, ")"); err != nil {
			return err
		}
	}
	for _, ref := range references {
		if _, err := fmt.Fprintf(out, "%q: %s -> %q (%s; target: %s", path, ref.Kind, ref.Value, ref.State, ref.TargetStatus); err != nil {
			return err
		}
		if ref.Target != "" {
			if _, err := fmt.Fprintf(out, " %q", ref.Target); err != nil {
				return err
			}
		}
		if ref.Condition != "" {
			if _, err := fmt.Fprintf(out, "; condition: %q", ref.Condition); err != nil {
				return err
			}
		}
		if _, err := fmt.Fprintln(out, ")"); err != nil {
			return err
		}
	}
	return nil
}

func writeStructure(out io.Writer, r *profile.StructureReport) error {
	if r == nil {
		return nil
	}
	if _, err := fmt.Fprintf(out, "%s %s; source: %s; status: %s\nAnalyzed: %d files; partial: %d; parses: %d\n", r.Engine, r.EngineVersion, r.Source, r.Status, r.AnalyzedFiles, r.PartialFiles, r.ParseCount); err != nil {
		return err
	}
	keys := make([]string, 0, len(r.Observations))
	for k := range r.Observations {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	for _, k := range keys {
		if _, err := fmt.Fprintf(out, "%s: %d\n", k, r.Observations[k]); err != nil {
			return err
		}
	}
	keys = keys[:0]
	for k := range r.Omissions {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	for _, k := range keys {
		if _, err := fmt.Fprintf(out, "Omitted: %d (%s)\n", r.Omissions[k], k); err != nil {
			return err
		}
	}
	if r.Files != nil {
		for _, f := range *r.Files {
			if _, err := fmt.Fprintf(out, "%q: %s %s\n", f.Path, f.Status, f.Reason); err != nil {
				return err
			}
			if len(f.Metrics) > 0 {
				if _, err := fmt.Fprintf(out, "  BCA metrics: %s\n", f.Metrics); err != nil {
					return err
				}
			}
		}
	}
	return nil
}
