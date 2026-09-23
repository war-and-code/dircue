package cli

import (
	"fmt"
	"io"
	"os"
	"path"
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
			doc, err := mapbuild.Build(report, mapbuild.Options{Revision: mapRevision, Commit: report.Discovery.Source.Commit, Deployables: deployObserver.Finish(), Intent: intentReport})
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
	var b strings.Builder
	line := func(format string, args ...any) { fmt.Fprintf(&b, format+"\n", args...) }
	line("Directory map: %s (%s source)", d.Status, d.Source.Mode)
	components, deployables, interfaces, capabilities := []mapdoc.Node{}, []mapdoc.Node{}, []mapdoc.Node{}, []mapdoc.Node{}
	populations, packages := 0, 0
	type languageSummary struct {
		name       string
		percentage float64
	}
	languages := []languageSummary{}
	for _, n := range d.Nodes {
		switch n.Kind {
		case mapdoc.NodeComponent:
			if !auxiliaryMapNode(n) {
				components = append(components, n)
			}
		case mapdoc.NodeDeployable:
			if !auxiliaryMapNode(n) {
				deployables = append(deployables, n)
			}
		case mapdoc.NodeInterface:
			if !auxiliaryMapNode(n) {
				interfaces = append(interfaces, n)
			}
		case mapdoc.NodeCapability:
			if !auxiliaryMapNode(n) {
				capabilities = append(capabilities, n)
			}
		case mapdoc.NodePackage:
			packages++
		case mapdoc.NodeContent:
			if n.Properties["scope"] == "inventory_population" {
				populations++
			}
			if n.Properties["role"] != "language_population" {
				continue
			}
			percentage, _ := strconv.ParseFloat(n.Properties["percentage"], 64)
			languages = append(languages, languageSummary{name: n.Name, percentage: percentage})
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
			shown[i] = fmt.Sprintf("%s %.1f%%", safeMapLabel(languages[i].name), languages[i].percentage)
		}
		more := ""
		if len(languages) > len(shown) {
			more = fmt.Sprintf(" (+%d more)", len(languages)-len(shown))
		}
		line("Languages: %s%s", strings.Join(shown, ", "), more)
	} else {
		line("Languages: none observed")
	}
	line("Content populations: %d   Relationships: %d   Packages: %d", populations, len(d.Edges), packages)
	ecosystems := map[string]int{}
	for _, n := range components {
		ecosystem := n.Properties["ecosystem"]
		if ecosystem == "" {
			ecosystem = "other"
		}
		ecosystems[ecosystem]++
	}
	ecosystemNames := make([]string, 0, len(ecosystems))
	for name := range ecosystems {
		ecosystemNames = append(ecosystemNames, name)
	}
	slices.Sort(ecosystemNames)
	byEcosystem := make([]string, 0, len(ecosystemNames))
	for _, name := range ecosystemNames {
		byEcosystem = append(byEcosystem, fmt.Sprintf("%s %d", safeMapLabel(name), ecosystems[name]))
	}
	line("Components: %d (%s)", len(components), strings.Join(byEcosystem, ", "))
	slices.SortFunc(components, func(a, b mapdoc.Node) int { return strings.Compare(mapNodeLabel(a), mapNodeLabel(b)) })
	for _, n := range components[:min(4, len(components))] {
		line("  %s [%s]", mapNodeLabel(n), safeMapLabel(n.Properties["ecosystem"]))
	}
	if len(components) > 4 {
		line("  (+%d more components)", len(components)-4)
	}
	line("Deployables: %d", len(deployables))
	linked := map[string][]string{}
	componentByID := make(map[string]mapdoc.Node, len(components))
	for _, component := range components {
		componentByID[component.ID] = component
	}
	for _, e := range d.Edges {
		if e.Type != mapdoc.EdgeBuilds && e.Type != mapdoc.EdgeRuns {
			continue
		}
		if target, ok := componentByID[e.To]; ok {
			linked[e.From] = append(linked[e.From], string(e.Type)+" "+mapNodeLabel(target))
		}
	}
	slices.SortFunc(deployables, func(a, b mapdoc.Node) int { return strings.Compare(mapNodeLabel(a), mapNodeLabel(b)) })
	for _, n := range deployables[:min(4, len(deployables))] {
		links := linked[n.ID]
		slices.Sort(links)
		suffix := ""
		if len(links) > 0 {
			suffix = " → " + strings.Join(links[:min(2, len(links))], ", ")
		}
		line("  %s [%s]%s", mapNodeLabel(n), safeMapLabel(n.Properties["kind"]), suffix)
	}
	if len(deployables) > 4 {
		line("  (+%d more deployables)", len(deployables)-4)
	}
	writeMapNames := func(title string, nodes []mapdoc.Node) {
		names := make([]string, 0, len(nodes))
		for _, n := range nodes {
			names = append(names, mapNodeLabel(n))
		}
		slices.Sort(names)
		names = slices.Compact(names)
		if len(names) == 0 {
			line("%s: none observed", title)
			return
		}
		more := ""
		if len(names) > 4 {
			more = fmt.Sprintf(" (+%d more)", len(names)-4)
		}
		line("%s: %s%s", title, strings.Join(names[:min(4, len(names))], ", "), more)
	}
	writeMapNames("Interfaces", interfaces)
	writeMapNames("Capabilities", capabilities)
	suggestions := map[string]bool{}
	for _, entry := range d.AnalyzerCoverage {
		if entry.ExpectedApplicable && entry.NotCovered == "not_run" {
			suggestions[entry.Tool] = true
		}
	}
	tools := make([]string, 0, len(suggestions))
	for tool := range suggestions {
		tools = append(tools, tool)
	}
	slices.Sort(tools)
	if len(tools) > 0 {
		line("Possible next analyzers: %s", strings.Join(tools[:min(6, len(tools))], ", "))
	}
	line("Attached provider runs: %d", len(d.CoverageLedger))
	var unknown []string
	for _, q := range d.Coverage {
		if q.Status != mapdoc.CoverageComplete {
			unknown = append(unknown, mapCoverageSummary(q, d.Source.Mode))
		}
	}
	slices.Sort(unknown)
	if len(unknown) > 0 {
		line("Still uncertain:")
		for _, item := range unknown[:min(len(unknown), 6)] {
			line("  %s", item)
		}
		if len(unknown) > 6 {
			line("  (+%d more questions)", len(unknown)-6)
		}
	}
	line("Use --json for evidence and full coverage details.")
	_, err := io.WriteString(out, b.String())
	return err
}

func auxiliaryMapNode(n mapdoc.Node) bool {
	role := n.Properties["role"]
	if role == "test" || role == "fixture" || role == "example" || role == "vendored" {
		return true
	}
	for _, p := range n.Paths {
		p = strings.ToLower(p)
		if strings.HasPrefix(p, "test/") || strings.HasPrefix(p, "tests/") || strings.HasPrefix(p, "testdata/") || strings.HasPrefix(p, "fixtures/") || strings.HasPrefix(p, "examples/") || strings.HasPrefix(p, "vendor/") || strings.Contains(p, "/testdata/") || strings.Contains(p, "/fixtures/") {
			return true
		}
	}
	return false
}

func safeMapLabel(value string) string {
	value = terminalValue(strings.TrimSpace(value))
	runes := []rune(value)
	if len(runes) > 96 {
		value = string(runes[:96]) + "…"
	}
	return value
}

func mapNodeLabel(n mapdoc.Node) string {
	name := strings.TrimSpace(n.Name)
	if name == "" || name == "?" {
		name = n.Properties["root"]
		if name == "" || name == "." {
			if len(n.Paths) > 0 {
				name = n.Paths[0]
			}
		}
		name = strings.TrimSuffix(path.Base(name), path.Ext(name))
	}
	return safeMapLabel(name)
}

func mapCoverageSummary(q mapdoc.QuestionCoverage, mode string) string {
	switch q.Question {
	case "source_binding":
		if mode == "directory" {
			return "Source identity: live directory has no full-content digest"
		}
		return "Source identity: snapshot could not be fully verified"
	case "content":
		return "Content: some selected files were omitted or unreadable"
	case "components":
		return "Components: some project declarations or references may be unresolved"
	case "deployables":
		return "Deployables: some build or deployment declarations may be unrecognized"
	case "interfaces":
		return "Interfaces: some entry points or contracts may be unrecognized"
	case "capabilities":
		return "Capabilities: some service dependencies may be unrecognized"
	case "packages":
		return "Packages: no complete package inventory is established"
	case "routing":
		return "Routing: follow-up plans have not been evaluated"
	case "analyzer_coverage":
		if slices.Contains(q.Reasons, "no_analyzer_report_attached") {
			return "Analyzer coverage: no analyzer report attached"
		}
		return "Analyzer coverage: supplied reports do not prove exhaustive coverage"
	default:
		return safeMapLabel(strings.ReplaceAll(q.Question, "_", " ")) + ": needs review"
	}
}
