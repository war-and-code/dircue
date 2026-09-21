package cli

import "github.com/spf13/cobra"

func setExtendedCommandHelp(cmd *cobra.Command, mode string) {
	switch mode {
	case "languages":
		cmd.Short = "Classify source languages with Linguist-compatible statistics"
		cmd.Long = "Report included language bytes and percentages. Git sources use the selected commit or exact tree (HEAD by default); --source directory reads current files. Vendored, generated, binary, and non-detectable files follow Linguist selection rules and supported .gitattributes overrides. Empty statistics do not prove an empty directory; use discovery for a metadata inventory."
		cmd.Example = "  dircue --json /checkout\n  dircue analyze languages --source directory --breakdown /content"
	case "all":
		cmd.Short = "Combine language statistics, ecosystem hints, and selected profilers"
		cmd.Long = "Run the default language and filename-based detectors in one scan. Optional profilers run only when requested through flags such as --discovery, --declarations, --environments, --metrics, and --structure. Each module retains its own population, coverage, and omissions; totals need not describe identical files. Structural analysis requires an explicitly selected worker."
		cmd.Example = "  dircue analyze all --discovery --json /content\n  dircue analyze all --declarations --environments --metrics --json /checkout"
	case "metrics":
		cmd.Short = "Count lines, comments, code, and lexical complexity with scc"
		cmd.Long = "Measure selected files using scc. The default scope follows language-statistics inclusion; --metrics-scope text includes all detected text languages. Lexical complexity is a counting heuristic, not a quality score. Use --files for bounded per-file detail and inspect coverage before comparing totals."
		cmd.Example = "  dircue analyze metrics --files --json /checkout\n  dircue analyze metrics --metrics-scope text --json /content"
	case "projects":
		cmd.Short = "Discover supported project manifests and shared build inputs"
		cmd.Long = "Read bounded .NET and JVM declarations without executing MSBuild, Maven, or Gradle, and retain filename-based project identities for the other documented manifest names. Unresolved expressions, conditions, missing targets, and parser limits remain visible. Project attribution describes static evidence, not the effective build graph. Use declarations for deeper supported ecosystem semantics and graph for .NET reference components."
		cmd.Example = "  dircue analyze projects --json /checkout\n  dircue analyze all --projects --declarations --json /checkout"
	case "structure":
		cmd.Short = "Measure supported syntax through an explicitly supplied native worker"
		cmd.Long = "Run a trusted structural worker on bounded selected source files. Requires --structural-worker; the worker is executable code, not a sandbox. Unsupported languages, parse errors, file limits, and module scope remain visible. --functions adds function records; --hotspots ranks supported measurements without assigning a software-quality grade."
		cmd.Example = "  dircue analyze structure --structural-worker /tools/dircue-structural-worker --json /checkout\n  dircue analyze structure --structural-worker /tools/dircue-structural-worker --functions --hotspots --json /checkout"
	case "frameworks", "ecosystems":
		cmd.Short = "Report " + mode + " inferred from supported filename and manifest hints"
		cmd.Long = "Run built-in detectors over selected files and return findings with evidence paths. Findings are hints under the supported detector rules, not proof of installed packages, runtime behavior, or build success. For bounded manifest parsing and workspace relationships, use analyze declarations."
		cmd.Example = "  dircue analyze " + mode + " --json /checkout"

	case "environments":
		cmd.Short = "Map declared project environments without running builds"
		cmd.Long = "Reuse supported project requirements and inspect bounded global.json inputs from the selected source. SDK selection is modeled from each project root, not an observed build invocation. Requirements, conditions, missing context and unsupported constraints remain explicit; installed tools are never probed."
		cmd.Example = "  dircue analyze environments --json /checkout\n  dircue analyze all --environments --json /checkout"
	case "availability":
		cmd.Short = "Inspect source-acquisition boundaries without fetching content"
		cmd.Long = "Inspect bounded file prefixes for Git LFS pointers and retain selected Gitlinks and submodule declarations. Directory mode can inspect supported confined sparse-checkout metadata; committed-tree mode keeps that checkout evidence separate. Does not fetch, hydrate, or execute filters. Inspect inventory, metadata coverage, and omissions before interpreting missing content."
		cmd.Example = "  dircue analyze availability --json /checkout\n  dircue analyze availability --source directory --json /checkout\n  dircue analyze all --availability --declarations --json /checkout"

	case "formats":
		cmd.Short = "Inspect bounded content for format evidence"
		cmd.Long = "Inspect selected regular files, including data and vendor paths, for supported format evidence. Distinguish filename hints, header signatures, parsed prefixes, and complete syntax checks. Does not expand archives, execute content, or infer that XML contains logs. Inspect read scope, coverage, and omissions before interpreting results."
		cmd.Example = "  dircue analyze formats --source directory --json /content\n  dircue analyze all --formats --discovery --json /checkout"
	case "declarations":
		cmd.Short = "Read static project declarations and named interfaces"
		cmd.Long = "Read bounded npm, Go, Python/uv, Cargo, and supported .NET/JVM manifests from the selected source. Describe workspace membership, local references, requirements, and named interfaces without executing package managers or build scripts. Standalone declaration analysis does not classify unrelated file contents. Inspect declaration states, target status, diagnostics, and coverage before interpreting absence."
		cmd.Example = "  dircue analyze declarations --json /checkout\n  dircue analyze declarations --source git --rev HEAD --json /checkout\n  dircue analyze all --declarations --discovery --json /checkout"
	case "discovery":
		cmd.Short = "Inventory file metadata and candidate manifests or artifacts"
		cmd.Long = "Inventory regular-file metadata, including vendor and data paths, without reading source payloads. Filename hints do not validate formats or parse manifests. Git storage and bounded attributes may still be read. Inspect status and omissions before treating counts as complete."
		cmd.Example = "  dircue analyze discovery --source directory --json /content\n  dircue analyze discovery --source git --rev HEAD --json /checkout"
	case "rules":
		cmd.Short = "Apply explicit rules to selected file metadata and bounded content"
		cmd.Long = "Apply a caller-supplied JSON ruleset to selected regular files, including files outside language statistics. Metadata predicates run before bounded complete-file literal matching. Rules add observations; they cannot disable modules, change language inclusion, execute commands, or load other configuration. Inspect coverage and omitted candidates before treating results as complete."
		cmd.Example = "  dircue analyze rules --rules-file /trusted/observations.json --json /checkout\n  dircue analyze rules --discovery --rules-file /trusted/observations.json --source directory --json /content\n  dircue analyze all --rules-file /trusted/observations.json --json /checkout"
	case "registries":
		cmd.Short = "Observe selected NuGet and npm package-source declarations"
		cmd.Long = "Read bounded NuGet.Config and .npmrc files from the selected source. Report declarations and sanitized URL origins, never an effective feed set. No configuration discovery outside the selected source, variable expansion, network requests, or package-manager execution. Qualified identifiers and origins can reveal internal infrastructure names. Inspect scope, syntax status, and omissions before interpreting absence."
		cmd.Example = "  dircue analyze registries --discovery --source directory --json /content\n  dircue analyze all --projects --registries --json /checkout"
	case "graph":
		cmd.Short = "Describe static .NET project-reference graphs"
		cmd.Long = "Read project declarations and derive .NET ProjectReference components, cycles, and degrees. Conditional, unresolved, and missing targets remain separate evidence. Does not execute MSBuild or establish that a build succeeds. JSON includes the project inventory and graph coverage."
		cmd.Example = "  dircue analyze graph --json /checkout\n  dircue analyze graph --source directory --json /checkout"
	case "packages":
		cmd.Short = "Import an existing Syft JSON report with project context"
		cmd.Long = "Import a bounded native Syft JSON report and read project declarations for optional attribution. Requires --syft-report; never executes Syft. --syft-root explicitly maps report coordinates. Mapping alone does not verify source correspondence: inspect source match, association, and coverage fields."
		cmd.Example = "  dircue analyze packages --syft-report /reports/syft.json --json /checkout\n  dircue analyze packages --syft-report /reports/syft.json --syft-root / --json /checkout"
	}
}
