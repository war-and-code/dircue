package cli

import (
	"fmt"
	"io"
	"os"
	"runtime"
	"runtime/debug"
	"slices"
	"strconv"
	"strings"

	"dircue/pkg/deployables"
	"dircue/pkg/detectors"
	"dircue/pkg/intentmap"
	"dircue/pkg/mapbuild"
	"dircue/pkg/mapdoc"
	"dircue/pkg/scanner"
	"github.com/spf13/cobra"
)

func newMapCommand(opts *options) *cobra.Command {
	var summary bool
	var budgetFiles int
	var attachments []string
	var attachBinding string
	var settingsFlags mapSettingsFlags
	cmd := &cobra.Command{
		Use:     "map [path]",
		Short:   "Map directory content and evidence-backed relationships in one pass",
		Long:    "Produce a portable, deterministic map from the selected Git tree or directory. Cheap native observers run together. JSON is the default when stdout is redirected; a terminal gets a compact summary. Use --json to force the map document or --summary to force the summary. Unknown questions and limits remain visible as coverage. Inspected content is never executed.",
		Example: "  dircue map --json /checkout\n  dircue map --summary --source directory /content\n  dircue map --budget-files 50000 --json /checkout",
		Args:    pathArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			for _, name := range []string{"breakdown", "strategies"} {
				if cmd.Flags().Changed(name) {
					return fmt.Errorf("--%s applies to language output, not map", name)
				}
			}
			if opts.json && summary {
				return fmt.Errorf("--json and --summary select different map output formats")
			}
			if budgetFiles < 1 {
				return fmt.Errorf("--budget-files must be positive")
			}
			if cmd.Flags().Changed("budget-files") && cmd.Flags().Changed("tree-size") {
				return fmt.Errorf("--budget-files and --tree-size set the same inventory limit; choose one")
			}
			if opts.maxFileBytes < 0 || opts.workers < 0 {
				return fmt.Errorf("--max-file-bytes and --workers must be nonnegative")
			}
			if !slices.Contains([]string{"auto", "git", "directory"}, opts.source) {
				return enumValueError("--source", "auto, git, or directory", opts.source, []string{"auto", "git", "directory"})
			}
			if !slices.Contains([]string{"fail", "continue"}, opts.onError) {
				return enumValueError("--on-error", "fail or continue", opts.onError, []string{"fail", "continue"})
			}
			if opts.source == "directory" && (cmd.Flags().Changed("rev") || cmd.Flags().Changed("tree")) {
				return fmt.Errorf("--rev and --tree require a Git source")
			}
			if opts.source == "auto" && (cmd.Flags().Changed("rev") || cmd.Flags().Changed("tree")) {
				opts.source = "git"
			}
			if cmd.Flags().Changed("tree") && opts.tree == "" {
				return fmt.Errorf("--tree requires a full Git tree object ID")
			}
			settings, err := resolveMapSettings(cmd, opts, budgetFiles, settingsFlags)
			if err != nil {
				return err
			}
			restoreRuntime := applyMapRuntimeSettings(settings)
			defer restoreRuntime()
			root := "."
			if len(args) == 1 {
				root = args[0]
			}
			if st, err := os.Stat(root); err == nil && !st.IsDir() {
				return fmt.Errorf("map requires a directory")
			}
			revision := ""
			if cmd.Flags().Changed("rev") {
				revision = opts.revision
			}
			deployObserver := deployables.NewCollector(deployables.Options{})
			intentObserver := intentmap.New(intentmap.Options{})
			hooks := append(detectors.Default(), deployObserver, intentObserver)
			report, err := scanner.Scan(cmd.Context(), root, scanner.Options{
				Source: opts.source, Revision: revision, Tree: opts.tree,
				ErrorPolicy: scanner.ErrorPolicy(opts.onError), Workers: settings.Workers,
				MaxTreeSize: settings.MaxFiles, MaxFileBytes: settings.MaxFileBytes,
				Detectors: hooks, Discovery: true, Declarations: true,
				Formats: true, Availability: true, Environments: true, Registries: true,
			})
			if err != nil {
				return missingPathCommandHint(cmd, args, err)
			}
			if report.Declarations != nil {
				intentObserver.AddDeclarations(report.Declarations.Projects)
			}
			intentReport, err := intentObserver.Finish(cmd.Context())
			if err != nil {
				return err
			}
			for _, w := range report.Warnings {
				if _, err := fmt.Fprintf(cmd.ErrOrStderr(), "warning: %s: %s (%s)\n", terminalValue(w.Path), terminalValue(w.Message), w.Code); err != nil {
					return err
				}
			}
			mapRevision := revision
			if opts.tree != "" {
				mapRevision = "tree:" + opts.tree
			}
			doc, err := mapbuild.Build(report, mapbuild.Options{Revision: mapRevision, Deployables: deployObserver.Finish(), Intent: intentReport})
			if err != nil {
				return err
			}
			if attachBinding != "" && attachBinding != "caller-asserted" {
				return fmt.Errorf("--attach-binding supports only caller-asserted")
			}
			doc, err = joinMapAttachments(cmd, doc, attachments, attachBinding == "caller-asserted")
			if err != nil {
				return err
			}
			doc, err = mapdoc.Normalize(doc)
			if err != nil {
				return err
			}
			if summary || !opts.json && terminalOutput(cmd.OutOrStdout()) {
				return writeMapSummary(cmd.OutOrStdout(), doc)
			}
			data, err := mapdoc.Marshal(doc)
			if err != nil {
				return err
			}
			n, err := cmd.OutOrStdout().Write(data)
			if err != nil {
				return err
			}
			if n != len(data) {
				return io.ErrShortWrite
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&summary, "summary", false, "Print a compact human-readable map summary")
	cmd.Flags().IntVar(&budgetFiles, "budget-files", scanner.DefaultMaxTreeSize, "Maximum source entries to inventory; a hit returns partial coverage and exit 0")
	cmd.Flags().StringArrayVar(&attachments, "attach", nil, "Join a saved provider report as KIND=PATH (repeatable: syft-json, sarif, noir-json, bifrost-code-query-json)")
	cmd.Flags().StringVar(&attachBinding, "attach-binding", "", "Assert binding for reports without snapshot identity: caller-asserted")
	addMapSettingsFlags(cmd, &settingsFlags)
	cmd.AddCommand(newMapLocateCommand(opts), newMapRouteCommand(opts), newMapSettingsCommand(opts))
	return cmd
}

func applyMapRuntimeSettings(settings resolvedMapSettings) func() {
	previousCPU := 0
	if settings.CPULimit > 0 {
		previousCPU = runtime.GOMAXPROCS(settings.CPULimit)
	}
	previousMemory := int64(0)
	if settings.MemoryLimit > 0 {
		previousMemory = debug.SetMemoryLimit(settings.MemoryLimit)
	}
	return func() {
		if settings.MemoryLimit > 0 {
			debug.SetMemoryLimit(previousMemory)
		}
		if settings.CPULimit > 0 {
			runtime.GOMAXPROCS(previousCPU)
		}
	}
}

func terminalOutput(out io.Writer) bool {
	f, ok := out.(*os.File)
	if !ok {
		return false
	}
	st, err := f.Stat()
	return err == nil && st.Mode()&os.ModeCharDevice != 0
}

func writeMapSummary(out io.Writer, d mapdoc.Document) error {
	counts := make(map[mapdoc.NodeKind]int)
	populations := 0
	for _, n := range d.Nodes {
		counts[n.Kind]++
		if n.Kind == mapdoc.NodeContent && n.Properties["scope"] == "inventory_population" {
			populations++
		}
	}
	if _, err := fmt.Fprintf(out, "Directory map (%s; %s source)\n", d.Status, d.Source.Mode); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(out, "Content populations: %d   Components: %d   Relationships: %d\n", populations, counts[mapdoc.NodeComponent], len(d.Edges)); err != nil {
		return err
	}
	type languageSummary struct {
		name       string
		percentage float64
		text       string
	}
	languages := []languageSummary{}
	for _, n := range d.Nodes {
		if n.Kind == mapdoc.NodeContent && n.Properties["role"] == "language_population" {
			percentage, _ := strconv.ParseFloat(n.Properties["percentage"], 64)
			languages = append(languages, languageSummary{name: n.Name, percentage: percentage, text: n.Name + " " + n.Properties["percentage"] + "%"})
		}
	}
	slices.SortFunc(languages, func(a, b languageSummary) int {
		if a.percentage > b.percentage {
			return -1
		}
		if a.percentage < b.percentage {
			return 1
		}
		return strings.Compare(a.name, b.name)
	})
	if len(languages) > 0 {
		shown := make([]string, min(6, len(languages)))
		for i := range shown {
			shown[i] = languages[i].text
		}
		more := ""
		if len(languages) > len(shown) {
			more = fmt.Sprintf(" (+%d more)", len(languages)-len(shown))
		}
		if _, err := fmt.Fprintf(out, "Languages: %s%s\n", strings.Join(shown, ", "), more); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintf(out, "Deployables: %d   Interfaces: %d   Capabilities: %d   Packages: %d\n", counts[mapdoc.NodeDeployable], counts[mapdoc.NodeInterface], counts[mapdoc.NodeCapability], counts[mapdoc.NodePackage]); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(out, "Attached provider runs: %d\n", len(d.CoverageLedger)); err != nil {
		return err
	}
	if len(d.AnalyzerCoverage) > 0 {
		if _, err := fmt.Fprintf(out, "Analyzer coverage entries: %d   Blind spots: %d\n", len(d.AnalyzerCoverage), len(d.AnalyzerBlindSpots)); err != nil {
			return err
		}
		for _, spot := range d.AnalyzerBlindSpots {
			if _, err := fmt.Fprintf(out, "  %s: %d\n", spot.Reason, spot.Entries); err != nil {
				return err
			}
		}
	}
	var unknown []string
	for _, q := range d.Coverage {
		if q.Status != mapdoc.CoverageComplete {
			reason := strings.Join(q.Reasons, ", ")
			unknown = append(unknown, fmt.Sprintf("%s: %s (%s)", q.Question, q.Status, reason))
		}
	}
	slices.Sort(unknown)
	if len(unknown) > 0 {
		if _, err := fmt.Fprintln(out, "Coverage to inspect:"); err != nil {
			return err
		}
		for _, item := range unknown[:min(len(unknown), 12)] {
			if _, err := fmt.Fprintln(out, "  "+item); err != nil {
				return err
			}
		}
	}
	_, err := fmt.Fprintln(out, "Use --json for evidence, IDs, and complete coverage details.")
	return err
}
