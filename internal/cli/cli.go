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

	"dircue/pkg/detectors"
	"dircue/pkg/profile"
	"dircue/pkg/scanner"
	"github.com/spf13/cobra"
)

// Version may be set by release builds with -ldflags "-X dircue/internal/cli.Version=...".
var Version = "0.1.0"

type options struct {
	json           bool
	breakdown      bool
	workers        int
	maxFileBytes   int64
	source         string
	revision       string
	maxTreeSize    int
	strategies     bool
	fileStrategies map[string]string
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
		Short:         "Profile repository languages and security tooling inputs",
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
			return fmt.Errorf("choose an analysis: languages, frameworks, ecosystems, or all")
		},
	}
	for _, mode := range []string{"languages", "frameworks", "ecosystems", "all"} {
		analyze.AddCommand(&cobra.Command{
			Use:   mode + " [path]",
			Short: "Analyze " + mode,
			Args:  pathArgs,
			RunE:  func(cmd *cobra.Command, args []string) error { return run(cmd, args, opts, mode) },
		})
	}
	root.AddCommand(analyze)
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
	if mode != "languages" {
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
	})
	if err != nil {
		return err
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
	default:
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
