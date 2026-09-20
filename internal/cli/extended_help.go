package cli

import "github.com/spf13/cobra"

func setExtendedCommandHelp(cmd *cobra.Command, mode string) {
	switch mode {
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
