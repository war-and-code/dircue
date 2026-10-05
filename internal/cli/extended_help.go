package cli

import "github.com/spf13/cobra"

func setExtendedCommandHelp(cmd *cobra.Command, mode, name string) {
	switch mode {
	case "languages":
		cmd.Short = "Classify source languages with Linguist-compatible statistics"
		cmd.Long = "Report included language bytes and percentages. Git sources use the selected commit or exact tree (HEAD by default); --source directory reads current files. Vendored, generated, binary, and non-detectable files follow Linguist selection rules and supported .gitattributes overrides. Empty statistics do not prove an empty directory; use discovery for a metadata inventory."
		cmd.Example = "  " + name + " --json /checkout\n  " + name + " analyze languages --source directory --breakdown /content"
	case "all":
		cmd.Short = "Combine language statistics, ecosystem hints, and selected profilers"
		cmd.Long = "Run the default language and filename-based detectors in one scan. Optional profilers run only when requested through flags such as --assessment, --discovery, --declarations, --environments, --metrics, and --structure. Each module retains its own population, coverage, and omissions; totals need not describe identical files. Structural analysis requires an explicitly selected worker."
		cmd.Example = "  " + name + " analyze all --discovery --json /content\n  " + name + " analyze all --declarations --environments --metrics --json /checkout"
	case "metrics":
		cmd.Short = "Count lines, comments, code, and lexical complexity with scc"
		cmd.Long = "Measure selected files using scc. The default scope follows language-statistics inclusion; --metrics-scope text includes all detected text languages. Lexical complexity is a counting heuristic, not a quality score. Use --files for bounded per-file detail and inspect coverage before comparing totals."
		cmd.Example = "  " + name + " analyze metrics --files --json /checkout\n  " + name + " analyze metrics --metrics-scope text --json /content"
	case "projects":
		cmd.Short = "Discover supported project manifests and shared build inputs"
		cmd.Long = "Read bounded .NET and JVM declarations without executing MSBuild, Maven, or Gradle, and retain filename-based project identities for the other documented manifest names. Unresolved expressions, conditions, missing targets, and parser limits remain visible. Project attribution describes static evidence, not the effective build graph. Use declarations for deeper supported ecosystem semantics and graph for .NET reference components."
		cmd.Example = "  " + name + " analyze projects --json /checkout\n  " + name + " analyze all --projects --declarations --json /checkout"
	case "structure":
		cmd.Short = "Measure supported syntax through an explicitly supplied native worker"
		cmd.Long = "Run a trusted structural worker on bounded selected source files. Requires --structural-worker; the worker is executable code, not a sandbox. Unsupported languages, parse errors, file limits, and module scope remain visible. --functions adds function records; --hotspots ranks supported measurements without assigning a software-quality grade."
		cmd.Example = "  " + name + " analyze structure --structural-worker /tools/dircue-structural-worker --json /checkout\n  " + name + " analyze structure --structural-worker /tools/dircue-structural-worker --functions --hotspots --json /checkout"
	case "frameworks", "ecosystems":
		cmd.Short = "Report " + mode + " inferred from supported filename and manifest hints"
		cmd.Long = "Run built-in detectors over selected files and return findings with evidence paths. Findings are hints under the supported detector rules, not proof of installed packages, runtime behavior, or build success. For bounded manifest parsing and workspace relationships, use analyze declarations."
		cmd.Example = "  " + name + " analyze " + mode + " --json /checkout"

	case "assessment":
		cmd.Short = "Measure repository populations, project relationships, and lockfile associations"
		cmd.Long = "Collect languages, selected regular-file and byte totals, filename manifest counts, distinct declared project roots, workspace membership, local project relationships, and supported npm/NuGet lockfile associations in one versioned report. Each measurement discloses its population, completeness, and evidence limits. Shared npm locks require proven workspace ownership and member entries. No policy verdict, package resolution, Git requirement, or Syft dependency. Use --source directory for current files, or --source git for a selected committed tree. Optional Syft report import on analyze all adds separate package evidence without changing native measurements."
		cmd.Example = "  " + name + " analyze assessment --source directory --json /content\n  " + name + " analyze all --assessment --json /checkout\n  " + name + " capabilities --schema assessment"
	case "lockfiles":
		cmd.Short = "Associate selected lockfiles and compare named dependency declarations"
		cmd.Long = "Read supported npm lockfile v2/v3 and NuGet packages.lock.json v1/v2 from the selected source. Report association separately from named static checks: npm direct declaration text and NuGet direct package presence. Missing, unsupported, and indeterminate evidence remain distinct. This does not resolve dependency graphs, verify lockfile freshness, execute restore, or contact registries. Shared or ambiguous ownership remains explicit."
		cmd.Example = "  " + name + " analyze lockfiles --json /checkout\n  " + name + " analyze all --lockfiles --environments --json /checkout"
	case "environments":
		cmd.Short = "Map declared project environments without running builds"
		cmd.Long = "Reuse supported project requirements and inspect bounded global.json and Python, Node, and Rust toolchain declarations from the selected source. SDK selection is modeled from each project root, not an observed build invocation. Requirements, conditions, missing context and unsupported constraints remain explicit; installed tools are never probed."
		cmd.Example = "  " + name + " analyze environments --json /checkout\n  " + name + " analyze all --environments --json /checkout"
	case "availability":
		cmd.Short = "Inspect source-acquisition boundaries without fetching content"
		cmd.Long = "Inspect bounded file prefixes for Git LFS pointers and retain selected Gitlinks and submodule declarations. Directory mode can inspect supported confined sparse-checkout metadata; committed-tree mode keeps that checkout evidence separate. Does not fetch, hydrate, or execute filters. Inspect inventory, metadata coverage, and omissions before interpreting missing content."
		cmd.Example = "  " + name + " analyze availability --json /checkout\n  " + name + " analyze availability --source directory --json /checkout\n  " + name + " analyze all --availability --declarations --json /checkout"

	case "formats":
		cmd.Short = "Inspect bounded content for format evidence"
		cmd.Long = "Inspect selected regular files, including data and vendor paths, for supported format evidence. Distinguish filename hints, header signatures, parsed prefixes, and complete syntax checks. Does not expand archives, execute content, or infer that XML contains logs. Inspect read scope, coverage, and omissions before interpreting results."
		cmd.Example = "  " + name + " analyze formats --source directory --json /content\n  " + name + " analyze all --formats --discovery --json /checkout"
	case "declarations":
		cmd.Short = "Read static project declarations and named interfaces"
		cmd.Long = "Read bounded npm, Go, Python/uv, Cargo, and supported .NET/JVM manifests from the selected source. Describe workspace membership, local references, requirements, and named interfaces without executing package managers or build scripts. Standalone declaration analysis does not classify unrelated file contents. Inspect declaration states, target status, diagnostics, and coverage before interpreting absence."
		cmd.Example = "  " + name + " analyze declarations --json /checkout\n  " + name + " analyze declarations --source git --rev HEAD --json /checkout\n  " + name + " analyze all --declarations --discovery --json /checkout"
	case "discovery":
		cmd.Short = "Inventory file metadata and candidate manifests or artifacts"
		cmd.Long = "Inventory regular-file metadata, including vendor and data paths, without reading source payloads. Filename hints do not validate formats or parse manifests. Git storage and bounded attributes may still be read. Inspect status and omissions before treating counts as complete."
		cmd.Example = "  " + name + " analyze discovery --source directory --json /content\n  " + name + " analyze discovery --source git --rev HEAD --json /checkout"
	case "rules":
		cmd.Short = "Apply explicit rules to selected file metadata and bounded content"
		cmd.Long = "Apply a caller-supplied JSON ruleset to selected regular files, including files outside language statistics. Metadata predicates run before bounded complete-file literal matching. Rules add observations; they cannot disable modules, change language inclusion, execute commands, or load other configuration. Inspect coverage and omitted candidates before treating results as complete."
		cmd.Example = "  " + name + " analyze rules --rules-file /trusted/observations.json --json /checkout\n  " + name + " analyze rules --discovery --rules-file /trusted/observations.json --source directory --json /content\n  " + name + " analyze all --rules-file /trusted/observations.json --json /checkout"
	case "registries":
		cmd.Short = "Observe selected NuGet, npm, Maven, and Cargo package-source declarations"
		cmd.Long = "Read bounded NuGet.Config, .npmrc, Maven settings.xml, and Cargo .cargo/config inputs from the selected source. Report declarations and sanitized URL origins, never an effective feed set. No configuration discovery outside the selected source, variable expansion, network requests, or package-manager execution. Qualified identifiers and origins can reveal internal infrastructure names. Inspect scope, syntax status, and omissions before interpreting absence."
		cmd.Example = "  " + name + " analyze registries --discovery --source directory --json /content\n  " + name + " analyze all --projects --registries --json /checkout"
	case "graph":
		cmd.Short = "Describe static .NET project-reference graphs"
		cmd.Long = "Read project declarations and derive .NET ProjectReference components, cycles, and degrees. Conditional, unresolved, and missing targets remain separate evidence. Does not execute MSBuild or establish that a build succeeds. JSON includes the project inventory and graph coverage."
		cmd.Example = "  " + name + " analyze graph --json /checkout\n  " + name + " analyze graph --source directory --json /checkout"
	case "packages":
		cmd.Short = "Import an existing Syft JSON report with project context"
		cmd.Long = "Import a bounded native Syft JSON report and read project declarations for optional attribution. Requires --syft-report; never executes Syft. --syft-root explicitly maps report coordinates. Mapping alone does not verify source correspondence: inspect source match, association, and coverage fields."
		cmd.Example = "  " + name + " analyze packages --syft-report /reports/syft.json --json /checkout\n  " + name + " analyze packages --syft-report /reports/syft.json --syft-root / --json /checkout"
	}
}
