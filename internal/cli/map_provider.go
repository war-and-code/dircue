package cli

import (
	"fmt"
	"io"
	"os"
	"slices"
	"strings"

	"dircue/pkg/coverageledger"
	"dircue/pkg/mapdoc"
	"dircue/pkg/providerjoin"
	"dircue/pkg/sariflocate"
	"github.com/spf13/cobra"
)

func joinMapAttachments(cmd *cobra.Command, doc mapdoc.Document, specs []string) (mapdoc.Document, error) {
	if len(specs) == 0 {
		return doc, nil
	}
	attachments, err := parseAttachments(specs)
	if err != nil {
		return mapdoc.Document{}, err
	}
	result, err := providerjoin.Join(cmd.Context(), providerInput(doc), attachments, providerjoin.Options{})
	if err != nil {
		return mapdoc.Document{}, err
	}
	doc.Nodes = append(doc.Nodes, result.Nodes...)
	doc.Edges = append(doc.Edges, result.Edges...)
	for _, run := range result.Ledger {
		doc.CoverageLedger = append(doc.CoverageLedger, mapdoc.CoverageLedgerEntry{
			Tool: run.Tool, ReportKind: run.ReportKind, Scope: run.Scope,
			Binding: string(run.Binding), Ran: run.Ran, CoveredFiles: run.CoveredFiles,
			State: run.State, Reason: run.Reason,
		})
	}
	doc.Coverage = mergeQuestionCoverage(doc.Coverage, result.Coverage)
	coverageledger.Reconcile(&doc)
	for _, diagnostic := range result.Diagnostics {
		if _, err := fmt.Fprintf(cmd.ErrOrStderr(), "warning: provider %s: %s\n", terminalValue(diagnostic.Kind), terminalValue(diagnostic.Message)); err != nil {
			return mapdoc.Document{}, err
		}
	}
	return mapdoc.Normalize(doc)
}

func parseAttachments(specs []string) ([]providerjoin.Attachment, error) {
	out := make([]providerjoin.Attachment, 0, len(specs))
	for _, spec := range specs {
		kind, filename, ok := strings.Cut(spec, "=")
		kind, filename = strings.TrimSpace(kind), strings.TrimSpace(filename)
		if !ok || kind == "" || filename == "" {
			return nil, fmt.Errorf("--attach requires KIND=PATH; supported kinds: syft-json, sarif, noir-json, bifrost-code-query-json")
		}
		if !slices.Contains([]string{"syft", "syft-json", "sarif", "noir", "noir-json", "bifrost", "bifrost-json", "bifrost-code-query-json"}, strings.ToLower(kind)) {
			return nil, fmt.Errorf("unsupported --attach kind %q; expected syft-json, sarif, noir-json, or bifrost-code-query-json", kind)
		}
		out = append(out, providerjoin.Attachment{Kind: kind, Path: filename})
	}
	return out, nil
}

func providerInput(doc mapdoc.Document) providerjoin.Input {
	return providerjoin.Input{Snapshot: providerjoin.Snapshot{
		Mode: doc.Source.Mode, Tree: doc.Source.Tree, Digest: doc.Source.Digest,
	}, Nodes: doc.Nodes, Edges: doc.Edges}
}

func mergeQuestionCoverage(base, extra []mapdoc.QuestionCoverage) []mapdoc.QuestionCoverage {
	byKey := make(map[string]mapdoc.QuestionCoverage, len(base)+len(extra))
	for _, q := range base {
		byKey[q.Question+"\x00"+q.Scope] = q
	}
	for _, q := range extra {
		byKey[q.Question+"\x00"+q.Scope] = q
	}
	out := make([]mapdoc.QuestionCoverage, 0, len(byKey))
	for _, q := range byKey {
		out = append(out, q)
	}
	return out
}

func newMapRouteCommand(opts *options) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "route <map.json>",
		Short:   "Create inert analyzer follow-up plans from a saved map",
		Long:    "Read a saved dircue map and emit deterministic, non-executable routing plans. Plans contain placeholders and declared prerequisites; dircue does not inspect PATH, download tools, or execute any plan.",
		Example: "  dircue map --json /checkout > map.json\n  dircue map route map.json --json",
		Args: func(_ *cobra.Command, args []string) error {
			if len(args) != 1 {
				return fmt.Errorf("map route requires one saved map file; see: dircue map route --help")
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := rejectMapSavedInputFlags(cmd, opts); err != nil {
				return err
			}
			doc, err := loadMapDocument(args[0], "map document")
			if err != nil {
				return err
			}
			plans := providerjoin.Route(providerInput(doc))
			if opts.json {
				return writeJSON(cmd.OutOrStdout(), plans)
			}
			for _, plan := range plans {
				if _, err := fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\t%s\t%s\n", plan.Tool, plan.Scope, plan.ReportKind, plan.Reason); err != nil {
					return err
				}
			}
			return nil
		},
	}
	setSavedReportHelp(cmd)
	return cmd
}

func newMapLocateCommand(opts *options) *cobra.Command {
	var sourceURI, digestSpec string
	var summary bool
	cmd := &cobra.Command{
		Use:     "locate <map.json> <results.sarif>",
		Short:   "Annotate SARIF locations with map ownership",
		Long:    "Read a saved dircue map and SARIF 2.1.0 log, then add dircue.map ownership properties to result locations. The inputs are treated as data and are never executed. The annotated SARIF is written to stdout; --summary emits bounded resolution counts instead.",
		Example: "  dircue map locate map.json results.sarif > located.sarif\n  dircue map locate --summary map.json results.sarif\n  dircue map locate --source-uri /checkout map.json results.sarif",
		Args: func(_ *cobra.Command, args []string) error {
			if len(args) != 2 {
				return fmt.Errorf("map locate requires a saved map and SARIF report; see: dircue map locate --help")
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := rejectMapSavedInputFlags(cmd, opts); err != nil {
				return err
			}
			doc, err := loadMapDocument(args[0], "map document")
			if err != nil {
				return err
			}
			input, err := readRegularFileBounded(args[1], sariflocate.DefaultLimits().Bytes, "SARIF report")
			if err != nil {
				return err
			}
			digest, err := parseMapDigest(digestSpec)
			if err != nil {
				return err
			}
			annotated, result, err := sariflocate.Annotate(input, doc, sariflocate.Options{SourceURI: sourceURI, Digest: digest})
			if err != nil {
				return err
			}
			if summary {
				return writeJSON(cmd.OutOrStdout(), result)
			}
			n, err := cmd.OutOrStdout().Write(annotated)
			if err != nil {
				return err
			}
			if n != len(annotated) {
				return io.ErrShortWrite
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&sourceURI, "source-uri", "", "Selected source root used only to relativize absolute SARIF URIs")
	cmd.Flags().StringVar(&digestSpec, "source-digest", "", "Directory snapshot identity as ALGORITHM:SCOPE:VALUE")
	cmd.Flags().BoolVar(&summary, "summary", false, "Emit JSON resolution and per-node counts instead of annotated SARIF")
	setSavedReportHelp(cmd)
	return cmd
}

func rejectMapSavedInputFlags(cmd *cobra.Command, _ *options) error {
	for _, flag := range analysisFlagNames {
		if cmd.Flags().Changed(flag) {
			return fmt.Errorf("--%s does not apply to saved map input", flag)
		}
	}
	return cmd.Context().Err()
}

func readRegularFileBounded(filename string, limit int, role string) ([]byte, error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", role, err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%s must be a readable regular file", role)
	}
	if info.Size() > int64(limit) {
		return nil, fmt.Errorf("%s exceeds %d-byte limit", role, limit)
	}
	return io.ReadAll(io.LimitReader(file, int64(limit)+1))
}

func parseMapDigest(spec string) (*mapdoc.Digest, error) {
	if spec == "" {
		return nil, nil
	}
	parts := strings.SplitN(spec, ":", 3)
	if len(parts) != 3 || parts[0] == "" || parts[1] == "" || parts[2] == "" {
		return nil, fmt.Errorf("--source-digest requires ALGORITHM:SCOPE:VALUE")
	}
	return &mapdoc.Digest{Algorithm: parts[0], Scope: parts[1], Value: parts[2]}, nil
}
