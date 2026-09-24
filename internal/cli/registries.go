package cli

import (
	"fmt"
	"io"
	"slices"

	"github.com/war-and-code/dircue/pkg/registries"
)

func writeRegistries(out io.Writer, r *registries.Report) error {
	if r == nil {
		return nil
	}
	if _, err := fmt.Fprintf(out, "Registry declarations; source: %s; status: %s\n%d candidate files; %d parsed; %d retained declarations\n", r.Source.Mode, r.Status, r.Coverage.CandidateFiles, r.Coverage.ParsedFiles, r.Coverage.RetainedDeclarations); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(out, "Declared configuration only; no effective feed set. Names and origins may identify internal infrastructure."); err != nil {
		return err
	}
	for _, cfg := range r.Configurations {
		if _, err := fmt.Fprintf(out, "%q (%s; %s; syntax: %s)\n", cfg.Path, cfg.Ecosystem, cfg.Status, cfg.SyntaxStatus); err != nil {
			return err
		}
		for _, d := range cfg.Declarations {
			if _, err := fmt.Fprintf(out, "  %d: %s/%s (%s)", d.Index, d.Section, d.Operation, d.Semantics); err != nil {
				return err
			}
			for _, field := range []struct {
				name  string
				label *registries.Label
			}{{"name", d.Name}, {"scope", d.Scope}, {"pattern", d.Pattern}} {
				if field.label != nil {
					if _, err := fmt.Fprintf(out, "; %s=%q (%s)", field.name, field.label.Value, field.label.Status); err != nil {
						return err
					}
				}
			}
			if d.Endpoint != nil {
				if _, err := fmt.Fprintf(out, "; endpoint=%q (%s)", d.Endpoint.Origin, d.Endpoint.Status); err != nil {
					return err
				}
			}
			if d.Disabled != nil {
				if _, err := fmt.Fprintf(out, "; disabled=%t", *d.Disabled); err != nil {
					return err
				}
			}
			if _, err := fmt.Fprintln(out); err != nil {
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
		if _, err := fmt.Fprintf(out, "Omitted inputs: %d (%s)\n", r.Omissions[key], key); err != nil {
			return err
		}
	}
	return nil
}
