// Package cli provides legacy language analysis and structured profiling commands.
package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"dircue/pkg/detectors"
	"dircue/pkg/profile"
	"dircue/pkg/projects"
	"dircue/pkg/scanner"
	"dircue/pkg/structure"
	"github.com/spf13/cobra"
)

// Version may be set by release builds with -ldflags "-X dircue/internal/cli.Version=...".
var Version = "0.6.1"

type options struct {
	json                   bool
	breakdown              bool
	workers                int
	maxFileBytes           int64
	source                 string
	revision               string
	maxTreeSize            int
	strategies             bool
	fileStrategies         map[string]string
	metrics                bool
	metricsScope           string
	metricsMaxFileBytes    int64
	metricsFiles           bool
	projects               bool
	declarations           bool
	discovery              bool
	registries             bool
	graph                  bool
	structure              bool
	structureFunctions     bool
	structureHotspots      bool
	formats                bool
	structuralWorker       string
	structuralMaxFileBytes int64
	structuralTimeout      time.Duration
	rulesFile              string
	syftReport             string
	syftRoot               string
	syftReportSHA256       string
	syftSourceTree         string
	syftMaxBytes           int64
}

// Execute runs one invocation. Errors are returned without printing; the caller
// decides how to display them and which process exit code to use.
func Execute(ctx context.Context, args []string, out, errOut io.Writer) error {
	// Cobra treats nil as a request to consume the host process's arguments.
	// This API always uses only the arguments supplied by its caller.
	if args == nil {
		args = []string{}
	}
	opts := &options{}
	root := &cobra.Command{
		Use:           "dircue [path]",
		Short:         "Profile source code repos and other directories of computer content",
		Long:          "Analyze languages in a Git revision, or profile a plain directory without Git. With no subcommand, emit the github-linguist directory output format. Git repositories use committed HEAD content by default; --source directory scans current files.",
		Version:       Version,
		Args:          pathArgs,
		SilenceErrors: true,
		SilenceUsage:  true,
		RunE:          func(cmd *cobra.Command, args []string) error { return run(cmd, args, opts, "languages") },
	}
	root.CompletionOptions.DisableDefaultCmd = true
	root.SetVersionTemplate("dircue {{.Version}}\n")
	root.SetOut(out)
	root.SetErr(errOut)
	root.SetArgs(args)
	flags := root.PersistentFlags()
	flags.BoolVarP(&opts.json, "json", "j", false, "Emit JSON")
	flags.BoolVarP(&opts.breakdown, "breakdown", "b", false, "Include file paths in language results")
	flags.BoolVarP(&opts.strategies, "strategies", "s", false, "Show the language detection strategy for each file (text output)")
	flags.IntVar(&opts.workers, "workers", 0, "Number of concurrent file workers (0 selects automatically)")
	flags.Int64Var(&opts.maxFileBytes, "max-file-bytes", 0, "Skip files larger than this many bytes (0 disables the optional size limit)")
	flags.StringVar(&opts.source, "source", "auto", "Content source: auto (Git when present), git, or directory")
	flags.StringVarP(&opts.revision, "rev", "r", "HEAD", "Git revision to analyze")
	flags.IntVarP(&opts.maxTreeSize, "tree-size", "t", 100000, "Maximum number of files scanned")
	analyze := &cobra.Command{
		Use:   "analyze",
		Short: "Run a selected profiler",
		RunE: func(cmd *cobra.Command, args []string) error {
			return fmt.Errorf("choose an analysis: languages, discovery, formats, rules, registries, metrics, projects, declarations, graph, packages, structure, frameworks, ecosystems, or all")
		},
	}
	for _, mode := range []string{"languages", "discovery", "formats", "rules", "registries", "metrics", "projects", "declarations", "graph", "packages", "structure", "frameworks", "ecosystems", "all"} {
		command := &cobra.Command{
			Use:   mode + " [path]",
			Short: "Analyze " + mode,
			Args:  pathArgs,
			RunE:  func(cmd *cobra.Command, args []string) error { return run(cmd, args, opts, mode) },
		}
		setExtendedCommandHelp(command, mode)
		if mode == "metrics" || mode == "all" || mode == "structure" {
			command.Flags().BoolVar(&opts.metricsFiles, "files", false, "Include per-file metrics or structural observations")
		}
		if mode == "structure" || mode == "all" {
			command.Flags().BoolVar(&opts.structureHotspots, "hotspots", false, "Summarize measured function populations and retain highest-valued metric evidence")
			command.Flags().BoolVar(&opts.structureFunctions, "functions", false, "Include bounded function-space metrics from the structural worker")
			command.Flags().StringVar(&opts.structuralWorker, "structural-worker", "", "Path to the optional native structural worker")
			command.Flags().Int64Var(&opts.structuralMaxFileBytes, "structural-max-file-bytes", structure.MaxSourceBytes, "Maximum complete source bytes for structural analysis (at most 8388608)")
			command.Flags().DurationVar(&opts.structuralTimeout, "structural-timeout", 10*time.Second, "Time limit for each structural worker invocation")
		}
		if mode == "metrics" || mode == "all" {
			command.Flags().StringVar(&opts.metricsScope, "metrics-scope", "source", "Metrics selection: source (language statistics) or text (all detected text languages)")
			command.Flags().Int64Var(&opts.metricsMaxFileBytes, "metrics-max-file-bytes", 16777216, "Skip metrics for larger files; maximum 268435456 bytes")
		}
		if mode == "rules" || mode == "registries" || mode == "all" {
			command.Flags().BoolVar(&opts.discovery, "discovery", false, "Summarize regular-file metadata and candidate manifests/artifacts")
		}
		if mode == "all" {
			command.Flags().BoolVar(&opts.formats, "formats", false, "Inspect bounded content for format evidence, including data and artifact files")
			command.Flags().BoolVar(&opts.declarations, "declarations", false, "Read declared project identities, workspace relationships, requirements, and interfaces")
			command.Flags().BoolVar(&opts.registries, "registries", false, "Read selected NuGet.Config and .npmrc declarations; disclose qualified names and sanitized origins")
			command.Flags().BoolVar(&opts.graph, "graph", false, "Analyze static .NET project-reference graphs (includes project inventory)")
			command.Flags().BoolVar(&opts.projects, "projects", false, "Map projects, declared build requirements, and content composition")
			command.Flags().BoolVar(&opts.structure, "structure", false, "Run optional structural analysis with the specified worker")
			command.Flags().BoolVar(&opts.metrics, "metrics", false, "Count code, comment, and blank lines and estimate complexity with scc")
		}
		if mode == "rules" || mode == "all" {
			command.Flags().StringVar(&opts.rulesFile, "rules-file", "", "Apply an explicit caller-supplied JSON ruleset; never discovers rulesets automatically")
		}
		if mode == "packages" || mode == "all" {
			addPackageFlags(command, opts)
		}
		analyze.AddCommand(command)
	}
	root.AddCommand(analyze)
	root.AddCommand(newCompareCommand(opts))
	return root.ExecuteContext(ctx)
}

func pathArgs(_ *cobra.Command, args []string) error {
	if len(args) > 1 {
		return fmt.Errorf("expected at most one directory path, received %d", len(args))
	}
	return nil
}

func run(cmd *cobra.Command, args []string, opts *options, mode string) error {
	if opts.workers < 0 {
		return fmt.Errorf("--workers must be zero or greater")
	}
	if opts.maxFileBytes < 0 {
		return fmt.Errorf("--max-file-bytes must be zero or greater")
	}
	packageReport, err := loadPackageEvidence(cmd, opts, mode)
	if err != nil {
		return err
	}
	ruleProgram, err := loadRules(cmd, opts, mode)
	if err != nil {
		return err
	}
	var metrics *scanner.MetricsOptions
	if mode == "metrics" || (mode == "all" && opts.metrics) {
		if opts.metricsScope != "source" && opts.metricsScope != "text" {
			return fmt.Errorf("--metrics-scope must be source or text")
		}
		if opts.metricsMaxFileBytes <= 0 || opts.metricsMaxFileBytes > 268435456 {
			return fmt.Errorf("--metrics-max-file-bytes must be between 1 and 268435456")
		}
		metrics = &scanner.MetricsOptions{Scope: opts.metricsScope, MaxFileBytes: opts.metricsMaxFileBytes, IncludeFiles: opts.metricsFiles}
	} else if mode == "all" {
		for _, flag := range []string{"files", "metrics-scope", "metrics-max-file-bytes"} {
			if cmd.Flags().Changed(flag) && !(flag == "files" && opts.structure) {
				return fmt.Errorf("--%s requires --metrics", flag)
			}
		}
	}
	var structural *structure.Client
	if mode == "structure" || (mode == "all" && opts.structure) {
		if opts.structuralMaxFileBytes < 1 || opts.structuralMaxFileBytes > structure.MaxSourceBytes {
			return fmt.Errorf("--structural-max-file-bytes must be between 1 and %d", structure.MaxSourceBytes)
		}
		if opts.structuralTimeout <= 0 {
			return fmt.Errorf("--structural-timeout must be positive")
		}
		var err error
		structural, err = structure.New(structure.Options{Hotspots: opts.structureHotspots, Functions: opts.structureFunctions, Worker: opts.structuralWorker, MaxFileBytes: opts.structuralMaxFileBytes, Timeout: opts.structuralTimeout})
		if err != nil {
			return err
		}
	} else if mode == "all" {
		for _, flag := range []string{"hotspots", "functions", "structural-worker", "structural-max-file-bytes", "structural-timeout"} {
			if cmd.Flags().Changed(flag) {
				return fmt.Errorf("--%s requires --structure", flag)
			}
		}
	}
	path := "."
	if len(args) == 1 {
		path = args[0]
	}
	if opts.source != "auto" && opts.source != "git" && opts.source != "directory" {
		return fmt.Errorf("--source must be auto, git, or directory")
	}
	if info, err := os.Stat(path); err == nil && !info.IsDir() {
		if mode != "languages" {
			return fmt.Errorf("%s analysis requires a directory", mode)
		}
		if cmd.Flags().Changed("rev") && opts.source == "directory" {
			return fmt.Errorf("--rev requires a Git content source")
		}
		// Linguist's single-file branch always inspects HEAD (or a flat
		// FileBlob), independently of the directory-only --rev argument.
		fileOpts := *opts
		fileOpts.revision = ""
		return runFile(cmd, path, &fileOpts)
	}
	if cmd.Flags().Changed("rev") && opts.source == "directory" {
		return fmt.Errorf("--rev requires a Git content source")
	}
	if cmd.Flags().Changed("rev") && opts.source == "auto" {
		opts.source = "git"
	}
	if !cmd.Flags().Changed("rev") || opts.source == "directory" {
		opts.revision = ""
	}
	var hooks []profile.Detector
	if mode == "all" || mode == "frameworks" || mode == "ecosystems" {
		hooks = detectors.Default()
	}
	report, err := scanner.Scan(cmd.Context(), path, scanner.Options{
		Source:   opts.source,
		Revision: opts.revision,
		// Linguist accepts nonpositive limits and emits empty statistics.
		// A limit of one has the same result for every nonempty tree,
		// while preserving zero as the embedding API's default sentinel.
		MaxTreeSize:       max(1, opts.maxTreeSize),
		Workers:           opts.workers,
		MaxFileBytes:      opts.maxFileBytes,
		IncludeFiles:      opts.breakdown || opts.strategies,
		IncludeStrategies: opts.strategies,
		Detectors:         hooks,
		Metrics:           metrics,
		Projects:          mode == "projects" || mode == "graph" || packageReport != nil || (mode == "all" && (opts.projects || opts.graph)),
		Declarations:      mode == "declarations" || (mode == "all" && opts.declarations),
		DeclarationsOnly:  mode == "declarations",
		Formats:           mode == "formats" || (mode == "all" && opts.formats),
		FormatsOnly:       mode == "formats",
		Discovery:         mode == "discovery" || opts.discovery,
		DiscoveryOnly:     mode == "discovery",
		Rules:             ruleProgram,
		RulesOnly:         mode == "rules",
		Registries:        mode == "registries" || (mode == "all" && opts.registries),
		RegistriesOnly:    mode == "registries",
		Structure:         structural,
		StructureFiles:    opts.metricsFiles,
	})
	if err != nil {
		return err
	}
	if mode == "graph" || (mode == "all" && opts.graph) {
		report.Graph = projects.AnalyzeGraph(report.Projects)
		report.SchemaVersion = profile.EnhancedSchemaVersion
	}
	if packageReport != nil {
		report.PackageEvidence, err = associatePackages(packageReport, report.Projects, opts)
		if err != nil {
			return err
		}
		report.SchemaVersion = profile.EnhancedSchemaVersion
	}
	if report.Declarations != nil {
		report.SchemaVersion = profile.DeclarationsSchemaVersion
	}
	if report.Formats != nil || (report.Structure != nil && report.Structure.Hotspots != nil) {
		report.SchemaVersion = profile.ContentSchemaVersion
	}
	for _, warning := range report.Warnings {
		if _, err := fmt.Fprintf(cmd.ErrOrStderr(), "warning: %s: %s (%s)\n", warning.Path, warning.Message, warning.Code); err != nil {
			return err
		}
	}
	opts.fileStrategies = report.Strategies
	out := cmd.OutOrStdout()
	if mode == "languages" {
		return writeLanguages(out, report.Languages, opts)
	}
	if opts.json {
		switch mode {
		case "frameworks":
			return writeJSON(out, nonNilFindings(report.Frameworks))
		case "ecosystems":
			return writeJSON(out, nonNilFindings(report.Ecosystems))
		default:
			return writeJSON(out, report)
		}
	}
	switch mode {
	case "frameworks":
		return writeFindings(out, report.Frameworks)
	case "ecosystems":
		return writeFindings(out, report.Ecosystems)
	case "projects":
		return writeProjects(out, report.Projects)
	case "declarations":
		return writeProjectDeclarations(out, report.Declarations)
	case "formats":
		return writeFormats(out, report.Formats)
	case "discovery":
		return writeDiscovery(out, report.Discovery)
	case "rules":
		if err := writeDiscovery(out, report.Discovery); err != nil {
			return err
		}
		return writeRules(out, report.Rules)
	case "registries":
		if err := writeDiscovery(out, report.Discovery); err != nil {
			return err
		}
		return writeRegistries(out, report.Registries)
	case "graph":
		return writeGraph(out, report.Graph)
	case "packages":
		return writePackageEvidence(out, report.PackageEvidence)
	case "structure":
		return writeStructure(out, report.Structure)
	case "metrics":
		return writeMetrics(out, report.Metrics, opts.metricsFiles)
	default:
		if err := writeFormats(out, report.Formats); err != nil {
			return err
		}
		if err := writeRegistries(out, report.Registries); err != nil {
			return err
		}
		if err := writeRules(out, report.Rules); err != nil {
			return err
		}
		if err := writePackageEvidence(out, report.PackageEvidence); err != nil {
			return err
		}
		if err := writeDiscovery(out, report.Discovery); err != nil {
			return err
		}
		if err := writeGraph(out, report.Graph); err != nil {
			return err
		}
		if _, err := fmt.Fprintln(out, "Languages:"); err != nil {
			return err
		}
		if err := writeLanguages(out, report.Languages, opts); err != nil {
			return err
		}
		for _, section := range []struct {
			label    string
			findings []profile.Finding
		}{
			{"Ecosystems", report.Ecosystems}, {"Frameworks", report.Frameworks}, {"Layouts", report.Layouts},
		} {
			if _, err := fmt.Fprintf(out, "\n%s:\n", section.label); err != nil {
				return err
			}
			if err := writeFindings(out, section.findings); err != nil {
				return err
			}
		}
		if err := writeProjectDeclarations(out, report.Declarations); err != nil {
			return err
		}
		if report.Projects != nil {
			if _, err := fmt.Fprintln(out, "\nProjects:"); err != nil {
				return err
			}
			if err := writeProjects(out, report.Projects); err != nil {
				return err
			}
		}
		if report.Structure != nil {
			if _, err := fmt.Fprintln(out, "\nStructure:"); err != nil {
				return err
			}
			if err := writeStructure(out, report.Structure); err != nil {
				return err
			}
		}
		if report.Metrics != nil {
			if _, err := fmt.Fprintln(out, "\nMetrics:"); err != nil {
				return err
			}
			return writeMetrics(out, report.Metrics, opts.metricsFiles)
		}
		return nil
	}
}

func nonNilFindings(findings []profile.Finding) []profile.Finding {
	if findings == nil {
		return []profile.Finding{}
	}
	return findings
}

func writeJSON(out io.Writer, value any) error {
	encoder := json.NewEncoder(out)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}

func writeLanguages(out io.Writer, languages []profile.Language, opts *options) error {
	var totalBytes int64
	for _, language := range languages {
		totalBytes += language.Bytes
	}
	percentage := func(language profile.Language) string {
		if totalBytes == 0 {
			// Linguist formats 0/0 as the string NaN when an attribute forces
			// an otherwise empty file into its language statistics.
			return "NaN"
		}
		return fmt.Sprintf("%.2f", language.Percentage)
	}
	if opts.json {
		type legacyLanguage struct {
			Size       int64     `json:"size"`
			Percentage string    `json:"percentage"`
			Files      *[]string `json:"files,omitempty"`
		}
		result := make(map[string]legacyLanguage, len(languages))
		for _, language := range languages {
			entry := legacyLanguage{Size: language.Bytes, Percentage: percentage(language)}
			if opts.breakdown {
				files := language.Files
				if files == nil {
					files = []string{}
				}
				entry.Files = &files
			}
			result[language.Name] = entry
		}
		return json.NewEncoder(out).Encode(result)
	}
	ordered := slices.Clone(languages)
	slices.SortFunc(ordered, func(a, b profile.Language) int {
		if a.Bytes > b.Bytes {
			return -1
		}
		if a.Bytes < b.Bytes {
			return 1
		}
		// Ruby sorts ascending by size then reverses the stable result.
		return strings.Compare(b.FirstFile, a.FirstFile)
	})
	for _, language := range ordered {
		if _, err := fmt.Fprintf(out, "%-7s %-10s %s\n", percentage(language)+"%", strconv.FormatInt(language.Bytes, 10), language.Name); err != nil {
			return err
		}
	}
	if opts.breakdown || opts.strategies {
		if _, err := fmt.Fprintln(out); err != nil {
			return err
		}
		// Linguist emits breakdown groups in tree encounter order, independent
		// of the descending-size summary above.
		groups := slices.Clone(languages)
		slices.SortFunc(groups, func(a, b profile.Language) int {
			if a.FirstFile != "" && b.FirstFile != "" {
				return strings.Compare(a.FirstFile, b.FirstFile)
			}
			return strings.Compare(a.Name, b.Name)
		})
		for _, language := range groups {
			if _, err := fmt.Fprintf(out, "%s:\n", language.Name); err != nil {
				return err
			}
			for _, path := range language.Files {
				label := path
				if opts.strategies && opts.fileStrategies[path] != "" {
					label += " [" + opts.fileStrategies[path] + "]"
				}
				if _, err := fmt.Fprintf(out, "  %s\n", label); err != nil {
					return err
				}
			}
			if _, err := fmt.Fprintln(out); err != nil {
				return err
			}
		}
	}
	return nil
}

func writeFindings(out io.Writer, findings []profile.Finding) error {
	for _, finding := range findings {
		if _, err := fmt.Fprintf(out, "%s\t%s\n", finding.Name, finding.Root); err != nil {
			return err
		}
	}
	return nil
}
