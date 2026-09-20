package cli

import (
	"fmt"
	"io"
	"slices"

	"dircue/pkg/formats"
)

func writeFormats(out io.Writer, r *formats.Report) error {
	if r == nil {
		return nil
	}
	if _, err := fmt.Fprintf(out, "Format evidence; source: %s; status: %s\nInspected %d of %d selected files; read %d bytes; %d complete reads and %d prefixes.\n", r.Source.Mode, r.Status, r.Coverage.InspectedFiles, r.Coverage.SelectedFiles, r.Coverage.InspectedBytes, r.Coverage.CompleteReads, r.Coverage.PrefixReads); err != nil {
		return err
	}
	for _, item := range r.Observations {
		if _, err := fmt.Fprintf(out, "%q: %d bytes; inspected %d (%s)\n", item.Path, item.Bytes, item.BytesRead, item.ReadScope); err != nil {
			return err
		}
		for _, evidence := range item.Evidence {
			if _, err := fmt.Fprintf(out, "  %s: %s", evidence.Format, evidence.Basis); err != nil {
				return err
			}
			if evidence.Validation != "" {
				if _, err := fmt.Fprintf(out, " (%s)", evidence.Validation); err != nil {
					return err
				}
			}
			if _, err := fmt.Fprintln(out); err != nil {
				return err
			}
		}
		for _, diagnostic := range item.Diagnostics {
			if _, err := fmt.Fprintf(out, "  Diagnostic: %s\n", diagnostic); err != nil {
				return err
			}
		}
	}
	keys := make([]string, 0, len(r.Omissions))
	for key := range r.Omissions {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	for _, key := range keys {
		if _, err := fmt.Fprintf(out, "Format coverage omitted: %d (%s)\n", r.Omissions[key], key); err != nil {
			return err
		}
	}
	_, err := fmt.Fprintln(out, "Prefixes and signatures do not validate whole files or establish their purpose.")
	return err
}
