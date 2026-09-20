package cli

import (
	"fmt"
	"io"
	"slices"

	"dircue/pkg/discovery"
	"dircue/pkg/projects"
)

func writeDiscovery(out io.Writer, r *discovery.Report) error {
	if r == nil {
		return nil
	}
	if _, err := fmt.Fprintf(out, "Metadata discovery; source: %s; status: %s\n%d regular files, %d bytes; content inspection: %s\n", r.Source.Mode, r.Status, r.Inventory.Files, r.Inventory.Bytes, r.Scope.ContentInspection); err != nil {
		return err
	}
	for _, g := range r.Categories {
		if _, err := fmt.Fprintf(out, "%s: %d files, %d bytes (%s)\n", g.Name, g.Files, g.Bytes, g.Basis); err != nil {
			return err
		}
	}
	for _, c := range r.Candidates {
		if _, err := fmt.Fprintf(out, "%s: %q (%s; %s)\n", c.Kind, c.Path, c.Format, c.Basis); err != nil {
			return err
		}
	}
	for _, entries := range []struct {
		label  string
		values map[string]int64
	}{{"Omitted candidates", r.OmittedCandidates}, {"Omitted inputs", r.Omissions}} {
		keys := make([]string, 0, len(entries.values))
		for k := range entries.values {
			keys = append(keys, k)
		}
		slices.Sort(keys)
		for _, k := range keys {
			if _, err := fmt.Fprintf(out, "%s: %d (%s)\n", entries.label, entries.values[k], k); err != nil {
				return err
			}
		}
	}
	return nil
}

func writeGraph(out io.Writer, r *projects.GraphReport) error {
	if r == nil {
		return nil
	}
	if _, err := fmt.Fprintf(out, "Project graph: %s; source: %s; status: %s\n%d projects, %d included edges, %d components, %d cyclic components\n", r.Kind, r.Source, r.Status, len(r.Nodes), r.Coverage.UniqueEdges, len(r.Components), len(r.Cycles)); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(out, r.EdgeRule); err != nil {
		return err
	}
	for _, n := range r.Nodes {
		if _, err := fmt.Fprintf(out, "%q: fan-in %d; fan-out %d; cyclic %t\n", n.ID, n.FanIn, n.FanOut, n.Cyclic); err != nil {
			return err
		}
	}
	for _, edge := range r.Edges {
		if !edge.Included {
			if _, err := fmt.Fprintf(out, "Excluded edge: %q -> %q (%s; %s)\n", edge.Source, edge.Value, edge.Certainty, edge.Resolution); err != nil {
				return err
			}
		}
	}
	for _, d := range r.Diagnostics {
		if _, err := fmt.Fprintf(out, "%q: %s (%s)\n", d.Path, d.Message, d.Code); err != nil {
			return err
		}
	}
	return nil
}
