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
		{141, "Terminated by SIGPIPE on POSIX systems when a downstream reader closed early (128 + signal 13). The Go runtime's default disposition is intentionally preserved; treat 141 as an early consumer close rather than a dircue error."},
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
		{"GOMAXPROCS", "Go runtime", "Automatic file workers use min(GOMAXPROCS, 16); map --cpu-limit temporarily sets GOMAXPROCS, while --workers explicitly selects worker count."},
		{"GOMEMLIMIT", "Go runtime", "map --memory-limit temporarily sets the cooperative Go-managed memory target. Neither setting is a hard CPU, RSS, subprocess, or operating-system ceiling."},
		{"GOMEMLIMIT", "Go runtime", "Cooperative Go memory limit; not a hard process memory cap and does not constrain the separate native worker."},
		{"SystemRoot", "Windows worker cancellation", "Locates System32/taskkill.exe for worker-tree cancellation; falls back to C:\\Windows."},
	}
	d.Behavior = []string{"No dircue-specific environment configuration, telemetry, or scan-time downloads.", "Output is unstyled and noninteractive, including when NO_COLOR, CI, or TERM=dumb is set. These variables do not activate a separate rendering mode.", "Reports contain no wall-clock timestamps; SOURCE_DATE_EPOCH does not change them.", "Core inspection does not execute inspected code. Structural analysis executes only the explicitly supplied trusted worker; the worker is not a sandbox.", "Saved-report planning, comparison, and capabilities never open the report's declared source root. Planned argv is inert and requires caller revalidation.", "Metadata describes supported command syntax and explicit semantic restrictions; it is not a host tool-availability probe."}
	d.OutputContracts = []cliOutputContract{
		{"languages-directory", "language-keyed object", "languages", "whole output", "Legacy directory JSON; percentages are strings, including documented NaN for attributed empty files."},
		{"languages-file", "single-file inspection object", "", "unavailable", "Distinct legacy single-file layout; no bundled schema currently covers this variant."},
		{"findings", "finding array", "findings", "whole output", "Standalone ecosystem/framework results."},
		{"profile", "versioned aggregate profile", "profile", "whole output", "Version depends on selected modules. Module schemas below describe components, not the full standalone analyzer envelope."},
		{"map", "portable node and edge map", "map", "whole output", "Question coverage distinguishes unknown and partial evidence; --summary selects a separate compact text view."},
		{"map-summary", "compact text map", "", "unavailable", "Human summary omits detailed facts and evidence; use --json for the full map."},
		{"map-routing", "inert analyzer plan array", "", "self-described", "Plans contain placeholders and prerequisites; no plan is executed and host tool availability is not checked."},
		{"map-comparison", "saved map comparison", "", "self-described", "Stable IDs distinguish material, evidence, coverage, and indeterminate changes without opening either source."},
		{"map-settings", "effective map settings", "", "self-described", "Typed execution and coverage controls with resolved values, categories, origins, and honest resource-limit qualifications."},
		{"sarif-located", "SARIF 2.1.0 log with dircue.map location properties", "", "self-described", "Upstream SARIF with a dircue.map property extension. Unknown SARIF fields are retained; source binding and URI resolution determine annotation resolution."},
		{"sarif-location-summary", "resolution and per-node count object", "", "self-described", "Selected by --summary; emits counts instead of the annotated SARIF log."},
		{"comparison", "saved-report comparison", "comparison", "whole output", "Successful comparisons can contain differences; inspect coverage and module compatibility."},
		{"planner-capabilities", "planner module registry", "capabilities", "whole output", "Default capabilities output retains its independent versioned contract."},
		{"plan", "inert follow-up plan", "planning", "whole output", "argv placeholders and executable:false require caller revalidation; reported cost is evidence, not a timing prediction."},
		{"cli-capabilities", "versioned CLI contract", "cli-capabilities", "whole output", "This explicit CLI metadata view uses its independent schema_version 1.0.0."},
		{"guide", "versioned guide sections and example argv", "guide", "whole output", "Static guidance; examples are never executed."},
		{"json-schema", "Draft 2020-12 compound schema", "", "self-described", "Offline bundled resources have identifiers, not URLs that need fetching."},
		{"help-text", "unstructured help", "", "unavailable", "Plain-text usage suitable for humans; --json is accepted but ignored. Use capabilities --guide --json for structured guidance."},
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
	if name == "preset" && (cmd.CommandPath() == "dircue map" || cmd.CommandPath() == "dircue map settings") {
		return slices.Clone(mapPresetNames)
	}
	scanCommand := cmd.Parent() == nil || cmd.Name() == "analyze" || cmd.Name() == "map" || cmd.Parent() != nil && cmd.Parent().Name() == "analyze"
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
	case "dircue map":
		return []string{"map", "map-summary"}
	case "dircue map route":
		return []string{"map-routing"}
	case "dircue map compare":
		return []string{"map-comparison"}
	case "dircue map settings":
		return []string{"map-settings"}
	case "dircue map locate":
		return []string{"sarif-located", "sarif-location-summary"}
	case "dircue help":
		return []string{"help-text"}
	case "dircue analyze":
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
	if cmd.CommandPath() == "dircue map compare" {
		return append(r, "Exactly two saved map documents; inherited source scan flags are rejected. A zero exit status means the comparison completed, not that the maps are identical.")
	}
	if cmd.CommandPath() == "dircue map settings" {
		return append(r, "No source is scanned. --preset and repeatable --set resolve effective values; inherited analysis flags are rejected. Resource notes are contractual qualifications, not measured ceiling claims.")
	}
	switch cmd.Name() {
	case "all":
		r = append(r, "Optional modules require explicit flags; --environments reuses declarations, --graph includes projects. --files and metrics options require --metrics unless --files is used with --structure. Structural options require --structure.")
	case "map":
		r = append(r, "At most one directory path. --budget-files and --tree-size are alternative inventory limits. Named resource flags override --set, which overrides --preset. --json and --summary are mutually exclusive. --attach is repeatable KIND=PATH and accepts syft-json, sarif, noir-json, and bifrost-code-query-json reports. Coverage remains explicit when the source exceeds a budget or an observer cannot answer a question.")
	case "route":
		r = append(r, "Exactly one saved map document; inherited source scan flags are rejected. Output plans are inert templates with placeholders and require caller validation before execution.")
	case "locate":
		r = append(r, "Exactly one saved map and one SARIF 2.1.0 log; inherited source scan flags are rejected. Default output is annotated SARIF; --summary selects a separate JSON count object.")
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
	case "analyze":
		r = append(r, "Group entry point: choose a profiler subcommand. Inherited scan flags are advisory here and validated by the selected subcommand.")
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
