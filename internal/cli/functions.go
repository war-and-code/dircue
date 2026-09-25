package cli

import (
	"fmt"
	"io"
	"slices"

	"github.com/war-and-code/dircue/pkg/profile"
)

func writeFunctions(out io.Writer, r *profile.FunctionReport) error {
	if r == nil {
		return nil
	}
	if _, err := fmt.Fprintf(out, "Function spaces: %s; status: %s; file coverage: %s\nRetained: %d of %d observed spaces; omitted: %d; invalid spans: %d\nMetrics include nested spaces; do not sum overlapping entries.\n", r.Provider, r.Status, r.ParentStatus, len(r.Entries), r.TotalSpaces, r.OmittedSpaces, r.InvalidSpanSpaces); err != nil {
		return err
	}
	keys := make([]string, 0, len(r.Omissions))
	for key := range r.Omissions {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	for _, key := range keys {
		if _, err := fmt.Fprintf(out, "Function coverage omitted: %d (%s)\n", r.Omissions[key], key); err != nil {
			return err
		}
	}
	for _, entry := range r.Entries {
		if _, err := fmt.Fprintf(out, "%q:%d-%d: %q (name: %s; %s)\n  BCA metrics: %s\n", entry.Path, entry.StartLine, entry.EndLine, entry.Name, entry.NameStatus, entry.Language, entry.Metrics); err != nil {
			return err
		}
	}
	return nil
}
