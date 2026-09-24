package cli

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"
	"github.com/war-and-code/dircue/pkg/rules"
)

func loadRules(cmd *cobra.Command, opts *options, mode string) (*rules.Program, error) {
	if mode != "rules" && mode != "all" {
		return nil, nil
	}
	if opts.rulesFile == "" {
		if mode == "rules" || cmd.Flags().Changed("rules-file") {
			return nil, fmt.Errorf("rules analysis requires a nonempty --rules-file")
		}
		return nil, nil
	}
	if err := cmd.Context().Err(); err != nil {
		return nil, err
	}
	file, err := openInputFile(opts.rulesFile, "rules file")
	if err != nil {
		return nil, fmt.Errorf("open --rules-file: %w", err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("inspect --rules-file: %w", err)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("--rules-file must be a regular file")
	}
	if info.Size() > rules.MaxConfigBytes {
		return nil, fmt.Errorf("--rules-file exceeds %d bytes", rules.MaxConfigBytes)
	}
	data, err := io.ReadAll(io.LimitReader(file, rules.MaxConfigBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read --rules-file: %w", err)
	}
	if len(data) > rules.MaxConfigBytes {
		return nil, fmt.Errorf("--rules-file exceeds %d bytes", rules.MaxConfigBytes)
	}
	program, err := rules.Compile(data)
	if err != nil {
		return nil, fmt.Errorf("compile --rules-file: %w", err)
	}
	return program, nil
}

func writeRules(out io.Writer, r *rules.Report) error {
	if r == nil {
		return nil
	}
	if _, err := fmt.Fprintf(out, "Observation rules: %s; source: %s; status: %s\nRuleset SHA-256: %s\n%d selected regular files; %d matches; %d omitted matches\n", r.Provider, r.Source.Kind, r.Status, r.RulesSHA256, r.InventoryFiles, r.TotalMatches, r.OmittedMatches); err != nil {
		return err
	}
	for _, rule := range r.Rules {
		if _, err := fmt.Fprintf(out, "%q: %d matched; %d evaluated; %d content omissions\n", rule.ID, rule.MatchedFiles, rule.EvaluatedFiles, rule.ContentOmittedFiles); err != nil {
			return err
		}
	}
	for _, observation := range r.Observations {
		if _, err := fmt.Fprintf(out, "%q: %q (%s; %d bytes)\n", observation.RuleID, observation.Path, observation.Evidence, observation.Size); err != nil {
			return err
		}
	}
	return nil
}
