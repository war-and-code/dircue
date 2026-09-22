package cli

import (
	"fmt"
	"io"
	"slices"
	"strings"

	"dircue/pkg/capabilities"
	"dircue/schema"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

type cliFlagContract struct {
	Name          string   `json:"name"`
	Shorthand     string   `json:"shorthand"`
	Type          string   `json:"type"`
	Default       string   `json:"default"`
	Description   string   `json:"description"`
	Inherited     bool     `json:"inherited"`
	AllowedValues []string `json:"allowed_values"`
}

type cliCommandContract struct {
	Path            []string          `json:"path"`
	Usage           string            `json:"usage"`
	Summary         string            `json:"summary"`
	Description     string            `json:"description"`
	Examples        []string          `json:"examples"`
	Flags           []cliFlagContract `json:"flags"`
	RejectedFlags   []string          `json:"rejected_inherited_flags"`
	Restrictions    []string          `json:"restrictions"`
	OutputContracts []string          `json:"output_contracts"`
}

type cliOutputContract struct {
	ID            string `json:"id"`
	Shape         string `json:"shape"`
	Schema        string `json:"schema"`
	SchemaScope   string `json:"schema_scope"`
	Qualification string `json:"qualification"`
}

type cliSchemaResource struct {
	Name    string   `json:"name"`
	Scope   string   `json:"scope"`
	Pointer string   `json:"profile_pointer"`
	Argv    []string `json:"export_argv"`
}

type cliContract struct {
	SchemaVersion   string               `json:"schema_version"`
	Kind            string               `json:"kind"`
	ProviderVersion string               `json:"provider_version"`
	Commands        []cliCommandContract `json:"commands"`
	ExitCodes       []struct {
		Code    int    `json:"code"`
		Meaning string `json:"meaning"`
	} `json:"exit_codes"`
	SourceSelection []string `json:"source_selection"`
	Environment     []struct {
		Name   string `json:"name"`
		Scope  string `json:"scope"`
		Effect string `json:"effect"`
	} `json:"environment"`
	OutputContracts []cliOutputContract `json:"output_contracts"`
	SchemaResources []cliSchemaResource `json:"schema_resources"`
	Behavior        []string            `json:"behavior"`
}

// describeCLI traverses the actual completed Cobra tree only when requested.
// Framework declarations supply command names, options, types and defaults;
// semantic qualifications below describe constraints Cobra cannot introspect.
func describeCLI(root *cobra.Command) cliContract {
	root.InitDefaultHelpCmd()
	root.InitDefaultVersionFlag()
	d := cliContract{SchemaVersion: "1.0.0", Kind: "dircue-cli-capabilities", ProviderVersion: Version,
		Commands: []cliCommandContract{}, SchemaResources: []cliSchemaResource{}}
	var visit func(*cobra.Command)
	visit = func(cmd *cobra.Command) {
		if cmd.Hidden {
			return
		}
		cmd.InitDefaultHelpFlag()
		entry := cliCommandContract{Path: strings.Split(cmd.CommandPath(), " "), Usage: cmd.UseLine(), Summary: cmd.Short, Description: cmd.Long,
			Examples: []string{}, Flags: []cliFlagContract{}, RejectedFlags: []string{}, Restrictions: commandRestrictions(cmd), OutputContracts: commandOutputContracts(cmd)}
		for _, line := range strings.Split(cmd.Example, "\n") {
			if strings.TrimSpace(line) != "" {
				entry.Examples = append(entry.Examples, strings.TrimSpace(line))
			}
		}
		flags := map[string]cliFlagContract{}
		collect := func(inherited bool) func(*pflag.Flag) {
			return func(flag *pflag.Flag) {
				if flag.Hidden {
					return
				}
				if savedReportFlagRejected(cmd, flag.Name) {
					entry.RejectedFlags = append(entry.RejectedFlags, "--"+flag.Name)
					return
				}
				description := flag.Usage
				if inherited && cmd.CommandPath() == "dircue help" {
					if flag.Name == "json" {
						description = "Ignored by help; help always emits plain text. Use dircue capabilities --guide --json or dircue capabilities --cli --json for structured guidance. Retained only for parser compatibility."
					} else {
						description = "Ignored by help; retained only for parser compatibility. Analysis-command meaning: " + flag.Usage
					}
				}
				flags[flag.Name] = cliFlagContract{flag.Name, flag.Shorthand, flag.Value.Type(), flag.DefValue, description, inherited, flagAllowedValues(cmd, flag.Name)}
			}
		}
		cmd.InheritedFlags().VisitAll(collect(true))
		cmd.LocalFlags().VisitAll(collect(false))
		for _, flag := range flags {
			entry.Flags = append(entry.Flags, flag)
		}
		slices.SortFunc(entry.Flags, func(a, b cliFlagContract) int { return strings.Compare(a.Name, b.Name) })
		slices.Sort(entry.RejectedFlags)
		entry.RejectedFlags = slices.Compact(entry.RejectedFlags)
		d.Commands = append(d.Commands, entry)
		for _, child := range cmd.Commands() {
			visit(child)
		}
	}
	visit(root)
	slices.SortFunc(d.Commands, func(a, b cliCommandContract) int {
		return strings.Compare(strings.Join(a.Path, " "), strings.Join(b.Path, " "))
	})
	d.ExitCodes = []struct {
		Code    int    `json:"code"`
		Meaning string `json:"meaning"`
	}{
		{0, "Successful command; reports can still have partial coverage. Differences in a comparison are not an error. The legacy tree-size limit returns empty statistics with a warning."},
		{1, "Handled CLI, input, analysis, cancellation, or output error; diagnostics go to stderr. Check status before consuming stdout."},
	}
	d.SourceSelection = []string{
		"With no path, analyze the current directory. At a committed Git repository root, auto reads HEAD; dirty and untracked files are excluded.",
		"Plain directories, uncommitted repositories, and explicitly requested repository subdirectories use filesystem contents in auto mode.",
		"--source directory reads current files; --source git requires committed Git content. --rev chooses a commit; --tree selects an exact tree and is mutually exclusive with --rev.",
		"Use ./name or -- name for paths named commands, and -- for dash-leading paths. Single-file inspection uses its separate legacy layout and HEAD behavior.",
	}
	d.Environment = []struct {
		Name   string `json:"name"`
		Scope  string `json:"scope"`
		Effect string `json:"effect"`
	}{
		{"GOMAXPROCS", "Go runtime", "Automatic file workers use min(GOMAXPROCS, 16); --workers explicitly overrides worker count."},
		{"GOMEMLIMIT", "Go runtime", "Cooperative Go memory limit; not a hard process memory cap and does not constrain the separate native worker."},
		{"SystemRoot", "Windows worker cancellation", "Locates System32/taskkill.exe for worker-tree cancellation; falls back to C:\\Windows."},
	}
	d.Behavior = []string{"No dircue-specific environment configuration, telemetry, or scan-time downloads.", "Output is unstyled and noninteractive, including when NO_COLOR, CI, or TERM=dumb is set. These variables do not activate a separate rendering mode.", "Reports contain no wall-clock timestamps; SOURCE_DATE_EPOCH does not change them.", "Core inspection does not execute inspected code. Structural analysis executes only the explicitly supplied trusted worker; the worker is not a sandbox.", "Saved-report planning, comparison, and capabilities never open the report's declared source root. Planned argv is inert and requires caller revalidation.", "Metadata describes supported command syntax and explicit semantic restrictions; it is not a host tool-availability probe."}
	d.OutputContracts = []cliOutputContract{
		{"languages-directory", "language-keyed object", "languages", "whole output", "Legacy directory JSON; percentages are strings, including documented NaN for attributed empty files."},
		{"languages-file", "single-file inspection object", "", "unavailable", "Distinct legacy single-file layout; no bundled schema currently covers this variant."},
		{"findings", "finding array", "findings", "whole output", "Standalone ecosystem/framework results."},
		{"profile", "versioned aggregate profile", "profile", "whole output", "Version depends on selected modules. Module schemas below describe components, not the full standalone analyzer envelope."},
		{"comparison", "saved-report comparison", "comparison", "whole output", "Successful comparisons can contain differences; inspect coverage and module compatibility."},
		{"planner-capabilities", "planner module registry", "capabilities", "whole output", "Default capabilities output retains its independent versioned contract."},
		{"plan", "inert follow-up plan", "planning", "whole output", "argv placeholders and executable:false require caller revalidation; reported cost is evidence, not a timing prediction."},
		{"cli-capabilities", "versioned CLI contract", "cli-capabilities", "whole output", "This explicit CLI metadata view uses its independent schema_version 1.0.0."},
		{"guide", "versioned guide sections and example argv", "guide", "whole output", "Static guidance; examples are never executed."},
		{"json-schema", "Draft 2020-12 compound schema", "", "self-described", "Offline bundled resources have identifiers, not URLs that need fetching."},
	}
	for _, name := range schema.Names() {
		scope, pointer := "whole output", ""
		switch name {
		case "availability", "declarations", "environments", "explanation", "focus", "formats":
			scope, pointer = "profile component", "/"+name
		case "hotspots":
			scope, pointer = "profile component", "/structure/hotspots"
		}
		d.SchemaResources = append(d.SchemaResources, cliSchemaResource{name, scope, pointer, []string{"dircue", "capabilities", "--schema", name, "--json"}})
	}
	return d
}

func flagAllowedValues(cmd *cobra.Command, name string) []string {
	scanCommand := cmd.Parent() == nil || cmd.Name() == "analyze" || cmd.Parent() != nil && cmd.Parent().Name() == "analyze"
	if scanCommand {
		switch name {
		case "source":
			return []string{"auto", "git", "directory"}
		case "on-error":
			return []string{"fail", "continue"}
		}
	}
	if slices.Contains([]string{"dircue analyze all", "dircue analyze focus", "dircue analyze metrics"}, cmd.CommandPath()) && name == "metrics-scope" {
		return []string{"source", "text"}
	}
	if cmd.CommandPath() == "dircue capabilities" && name == "schema" {
		return schema.Names()
	}
	if cmd.CommandPath() == "dircue plan" {
		registry := capabilities.Dircue(Version)
		values := []string{}
		for _, module := range registry.Modules {
			switch name {
			case "module":
				values = append(values, module.ID)
			case "question":
				values = append(values, module.Question)
			case "input":
				for _, input := range module.RequiredInputs {
					if input != "source" && input != "project" {
						values = append(values, input)
					}
				}
			}
		}
		slices.Sort(values)
		return slices.Compact(values)
	}
	return []string{}
}

func commandOutputContracts(cmd *cobra.Command) []string {
	switch cmd.CommandPath() {
	case "dircue", "dircue analyze languages":
		return []string{"languages-directory", "languages-file"}
	case "dircue analyze ecosystems", "dircue analyze frameworks":
		return []string{"findings"}
	case "dircue capabilities":
		return []string{"planner-capabilities", "cli-capabilities", "guide", "json-schema"}
	case "dircue plan":
		return []string{"plan"}
	case "dircue compare":
		return []string{"comparison"}
	case "dircue analyze", "dircue help":
		return []string{}
	default:
		if cmd.Parent() != nil && cmd.Parent().Name() == "analyze" {
			return []string{"profile"}
		}
		return []string{}
	}
}

func commandRestrictions(cmd *cobra.Command) []string {
	r := []string{}
	if cmd.Parent() == nil || (cmd.Parent() != nil && cmd.Parent().Name() == "analyze") {
		r = append(r, "At most one source path. --rev and --tree are mutually exclusive. --rev requires a Git source.")
	}
	switch cmd.Name() {
	case "all":
		r = append(r, "Optional modules require explicit flags; --environments reuses declarations, --graph includes projects. --files and metrics options require --metrics unless --files is used with --structure. Structural options require --structure.")
	case "plan":
		r = append(r, "Exactly one saved aggregate report and at least one --module or --question. Module/question vocabulary and prerequisites come from default capabilities. One --project, only for focus. Cataloged --input values are the union of caller-supplied prerequisites; each is accepted only when required by a selected module.")
	case "compare":
		r = append(r, "Exactly two saved aggregate reports; no source scan options.")
	case "capabilities":
		r = append(r, "No positional arguments. --cli, --guide, and --schema are mutually exclusive by flag presence. Schema output is JSON whether or not --json is supplied.")
	case "help":
		r = append(r, "Help always emits plain text. Inherited analysis options and --json are accepted only for parser compatibility and ignored; use dircue capabilities --guide --json or dircue capabilities --cli --json for structured guidance.")
	case "structure":
		r = append(r, "Requires an explicit --structural-worker. --functions and --hotspots opt into bounded additional output.")
	case "focus":
		r = append(r, "Exactly one of --project or --affected-by. --affected-by cannot combine with --related-project or --metrics. --breakdown/--strategies unsupported.")
	case "explain":
		r = append(r, "Exactly one of --file or --project. --report prohibits source path and inherited scan options. --breakdown/--strategies unsupported.")
	case "rules":
		r = append(r, "Requires --rules-file; rules are caller-supplied, never discovered automatically.")
	case "packages":
		r = append(r, "Requires --syft-report. --syft-report-sha256 and --syft-source-tree must be supplied together.")
	}
	return r
}

func writeCLIContract(out io.Writer, d cliContract) error {
	if _, err := fmt.Fprintf(out, "CLI capabilities: dircue %s (contract %s)\nUse --json for command flags, output contracts, schema resources, and restrictions.\n", d.ProviderVersion, d.SchemaVersion); err != nil {
		return err
	}
	for _, command := range d.Commands {
		if _, err := fmt.Fprintf(out, "  %s: %s\n", strings.Join(command.Path, " "), command.Summary); err != nil {
			return err
		}
	}
	return nil
}
