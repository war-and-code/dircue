package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"
)

type guideSection struct {
	Title    string     `json:"title"`
	Guidance string     `json:"guidance"`
	Examples [][]string `json:"examples"`
}
type cliGuide struct {
	SchemaVersion string         `json:"schema_version"`
	Kind          string         `json:"kind"`
	Sections      []guideSection `json:"sections"`
}

func automationGuide(root *cobra.Command) cliGuide {
	name := root.Name()
	guide := cliGuide{"1.0.0", "dircue-automation-guide", []guideSection{
		{"Map a directory", "Run the one-shot map for content populations, components, deployables, declared interfaces and capabilities. JSON is the default when stdout is redirected; --summary gives a compact human view. Facts include evidence and per-question coverage. A budget hit exits 0 with partial coverage, so inspect status and coverage before acting on omissions. Inspected content is never executed.", [][]string{{name, "map", "--json", "/checkout"}, {name, "map", "--summary", "/content"}, {name, "capabilities", "--schema", "map"}}},
		{"Choose the source", "Repository roots read committed HEAD by default. Use --source directory for current files, or --source git to require Git. With no path, the current directory is selected. Use ./name or -- name for paths named commands.", [][]string{{name, "--json", "/checkout"}, {name, "--source", "directory", "--json", "/content"}}},
		{"Start with the evidence you need", "Discovery inventories metadata without source-payload classification. Select follow-up modules explicitly; analyze all keeps heavier modules opt-in. These examples do not execute inspected build scripts.", [][]string{{name, "analyze", "discovery", "--json", "/checkout"}, {name, "analyze", "all", "--declarations", "--json", "/checkout"}}},
		{"Read output contracts", "Legacy directory --json is a language-keyed map; analyze all and most analyzers return a versioned profile. Single-file JSON is a separate legacy layout with no bundled schema. Success exits 0; handled errors exit 1 except map compare --exit-code errors, which exit 3. A downstream reader closing early may deliver POSIX SIGPIPE (exit 141) since " + name + " keeps the runtime's default disposition; warnings go to stderr. Inspect status, coverage, omissions and schema_version: success need not mean complete evidence. Empty language statistics do not prove empty content.", [][]string{{name, "capabilities", "--cli", "--json"}, {name, "capabilities", "--schema", "profile", "--json"}}},
		{"Plan from a saved aggregate report", "Save an analyzer's --json stdout to first-pass.json, then select modules or questions. Plan reads that report only. Commands contain inert argv and a {source} placeholder; revalidate source identity, boundary and report freshness before filling placeholders as argument values. Never interpolate untrusted report content into a shell. Candidate quantities are not runtime or memory predictions.", [][]string{{name, "capabilities", "--json"}, {name, "plan", "first-pass.json", "--module", "declarations", "--json"}, {name, "plan", "first-pass.json", "--module", "structure", "--input", "structural-worker", "--json"}}},
		{"Compare retained evidence", "Compare saved aggregate reports without rescanning sources. Differences return success; module compatibility and coverage limit conclusions. The caller chooses source pairing.", [][]string{{name, "compare", "base.json", "head.json", "--json"}}},
		{"Select a trusted structural worker", "Structure needs an explicitly supplied matching executable. This runs that executable; it is not a sandbox. No worker download, PATH probing or repository build execution happens automatically.", [][]string{{name, "analyze", "structure", "--structural-worker", "/tools/dircue-structural-worker", "--json", "/checkout"}}},
		{"Choose detail and understand cost", "Discovery reads metadata; declarations and environments read bounded supported manifests; formats and availability inspect bounded content evidence. Language classification inspects bounded prefixes, while metrics count complete selected files. Structural analysis parses bounded complete files in a separate worker. Focus retains the original-root inventory and context: it is not a promise to avoid traversal. Each module has its own population and omissions. Comparing totals from different modules does not make their populations equivalent.", [][]string{{name, "analyze", "metrics", "--files", "--metrics-scope", "text", "--json", "/content"}, {name, "analyze", "structure", "--functions", "--hotspots", "--structural-worker", "/tools/dircue-structural-worker", "--json", "/checkout"}}},
		{"Bound resources explicitly", "--workers 0 chooses min(GOMAXPROCS, 16); explicit workers are 1..1024. --max-file-bytes N skips larger files with warnings; 0 disables that optional bound. --tree-size defaults to 100000: reaching the limit returns empty legacy statistics with warning and success, so raise it above the entry count for complete statistics. --on-error fail is the default; continue retains explicit omissions for recoverable per-file reads (including bounded manifest, format, registry, and global.json reads under analyze all) and per-file structural worker timeouts or crashes, but never protocol violations or non-file errors. Classification uses a bounded 128 KiB repository prefix. Metrics default to a 16 MiB complete-file limit, configurable through --metrics-max-file-bytes up to 256 MiB. Structural input is at most 8 MiB and --structural-timeout bounds each worker invocation. GOMEMLIMIT is a cooperative Go runtime limit, not a hard total-memory cap or a native-worker limit. External process/container limits may still be needed.", [][]string{{name, "analyze", "all", "--workers", "4", "--max-file-bytes", "16777216", "--on-error", "continue", "--json", "/checkout"}, {name, "analyze", "metrics", "--metrics-max-file-bytes", "33554432", "--json", "/checkout"}}},
		{"Recover from common mistakes", "Missing recent edits: select --source directory at a Git repository root. Empty language totals: use discovery and check warnings, tree-size, attributes and excluded populations. Unknown flags: use the suggested spelling; guesses never execute automatically. Missing structure worker: supply --structural-worker explicitly. Saved-report commands need aggregate JSON, not the legacy language map; choose selectors from capabilities, and supply source options when producing the report rather than to plan or compare. A nonzero exit requires checking stderr before consuming stdout.", [][]string{{name, "analyze", "discovery", "--source", "directory", "--json", "/checkout"}, {name, "capabilities", "--schema", "languages", "--json"}, {name, "help", "analyze", "focus"}}},
		{"Annotate SARIF locations with map ownership", "Run " + name + " map locate with a saved map and any SARIF 2.1.0 log to add dircue.map ownership properties to every result location. The log is treated as data and never executed. Unknown SARIF properties pass through unchanged. Use --source-uri to relativize absolute or file:// URIs emitted by tools running in containers or CI, and --uri-base NAME=URI to define a uriBaseId such as SRCROOT that a report uses without declaring. Use --source-digest to bind the report to a known directory snapshot. A binding mismatch is reported per run; results are never silently attributed across different snapshots. Use --summary for per-node result counts instead of the annotated log.", [][]string{{name, "map", "locate", "--source-uri", "/checkout", "map.json", "results.sarif"}, {name, "map", "locate", "--summary", "map.json", "results.sarif"}}},
		{"Profiler reference", "The following reference is taken from the installed command registrations. Every profiler supports --json; use its --help for exact options and defaults. Optional functionality stays explicit, and examples use placeholder paths. Component schemas such as formats describe fields inside the aggregate profile; use the profile schema for the whole analyzer response.", [][]string{{name, "analyze", "--help"}, {name, "capabilities", "--cli", "--json"}}},
	}}
	for _, group := range root.Commands() {
		switch group.Name() {
		case "analyze":
			for _, command := range group.Commands() {
				if command.Hidden {
					continue
				}
				guidance := command.Long
				if guidance == "" {
					guidance = command.Short
				}
				if command.Example != "" {
					guidance += "\nExamples:\n" + command.Example
				}
				guide.Sections = append(guide.Sections, guideSection{command.CommandPath(), guidance, [][]string{}})
			}
		case "map":
			for _, command := range group.Commands() {
				if command.Hidden {
					continue
				}
				guidance := command.Long
				if guidance == "" {
					guidance = command.Short
				}
				if command.Example != "" {
					guidance += "\nExamples:\n" + command.Example
				}
				guide.Sections = append(guide.Sections, guideSection{command.CommandPath(), guidance, [][]string{}})
			}
		}
	}
	return guide
}

func writeAutomationGuide(out io.Writer, guide cliGuide, displayName string) error {
	header := strings.ToUpper(displayName[:1]) + displayName[1:]
	if _, err := fmt.Fprintf(out, "%s automation guide\nExamples use caller-supplied paths and are never executed.\n", header); err != nil {
		return err
	}
	for _, section := range guide.Sections {
		if _, err := fmt.Fprintf(out, "\n%s\n%s\n", section.Title, section.Guidance); err != nil {
			return err
		}
		for _, argv := range section.Examples {
			if _, err := fmt.Fprintf(out, "  %s\n", strings.Join(argv, " ")); err != nil {
				return err
			}
		}
	}
	return nil
}
