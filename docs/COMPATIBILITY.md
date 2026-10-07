# 1.x compatibility policy

dircue 1.0 establishes a public contract centered on the directory map ([issue #81](https://github.com/war-and-code/dircue/issues/81)). This document lists the surfaces that keep their documented meaning across 1.x releases, and what is deliberately excluded. It complements [DESIGN_PRINCIPLES.md](DESIGN_PRINCIPLES.md), which explains why documented compatibility surfaces matter.

Anything not listed here is not covered. If a downstream consumer depends on unspecified behavior, the right next step is an [issue](https://github.com/war-and-code/dircue/issues) proposing that we cover it explicitly.

## 1.x compatibility surfaces

The surfaces below retain their documented meanings across 1.x releases. Compatibility tests compare exact outputs for fixed fixtures where applicable. That does not freeze classification data, detected findings, tool versions, or all report bytes: upstream language patterns and supported analysis can improve. Changes belong in `CHANGELOG.md` and focused regression tests. A harness exception does not authorize breaking an established CLI or report contract: those changes require the versioning and migration described below. Diagnostic-text exceptions apply only to wording explicitly left unfrozen.

- **Command names.** `dircue` and `dirq` are the same program. Every documented invocation works under either name with the same flags, exit statuses and machine-readable output; help and usage text show the name that was invoked. Both names ship on every install route: release archives, wheels, `go install` (`github.com/war-and-code/dircue` for `dircue`, `github.com/war-and-code/dircue/cmd/dirq` for `dirq`) and images built from the `Dockerfile`.
- **The directory map.** `dircue map` and its subcommands (`map compare`, `map locate`, `map route`, `map settings`), with their documented flags, `--preset` names, `--set` setting names, and `--attach` report kinds. The map document (`schema/map.schema.json`, `schema_version` 1.0.0), the forest document (`schema/forest.schema.json`), the comparison document (`schema/map-compare.schema.json`) and the run-statistics document (`schema/stats.schema.json`) keep every documented field, type and meaning. Also frozen:
  - node kinds, edge types and coverage statuses: `complete`, `partial`, `unknown`, `not_run`, `tool_error`;
  - the rule that `complete` means exhaustive evidence for its scope;
  - stable node and edge IDs, derived from paths, declared names and kinds, so the same tree yields the same IDs at any location, worker count or host.

  New node kinds, edge types, properties, coverage reasons and interface or capability names may be added. Consumers must ignore values they do not recognize. Reason codes are stable in the same additive sense as warning codes. The set of facts the map observes improves within 1.x: a newer release may find more, or classify something more precisely. Such changes are listed in `CHANGELOG.md`.
- **Legacy Linguist CLI.** `dircue` with no subcommand emits the language-keyed directory JSON when invoked with `--json`, and the Linguist plain-text layout otherwise. These flags keep their documented behavior, defaults, and short forms: `-j/--json`, `-b/--breakdown`, `-s/--strategies`, `-r/--rev`, `--tree`, `-t/--tree-size`, `--source`, `--on-error`, `--workers`, `--max-file-bytes`, `-v/--version` and `-h/--help`. The language-percentage string encoding, including the legacy `"NaN"` for an attribute-forced language on an empty file, is part of the contract.
- **`analyze` subcommands and flags.** The names, applicable flags, and documented output shapes of `analyze languages`, `analyze ecosystems`, `analyze frameworks`, `analyze discovery`, `analyze metrics`, `analyze projects`, `analyze declarations`, `analyze environments`, `analyze focus`, `analyze availability`, `analyze explain`, `analyze graph`, `analyze packages`, `analyze rules`, `analyze registries`, `analyze formats`, `analyze structure`, and `analyze all` (with its module toggles) are frozen. Enum values (`--source auto|git|directory`, `--on-error fail|continue`, `--metrics-scope source|text`) are frozen as well; adding a new value counts as an additive change.
- **`compare` and `plan`.** The saved-report readers accept the JSON their producers emit for the documented supported profile versions and input families, with the documented `--module` names and positional argument shapes. A valid aggregate report is not necessarily supported by every comparison or planning module.
- **`capabilities` outputs.** `capabilities --json` (planner descriptor), `capabilities --cli --json` (CLI catalog), `capabilities --guide[ --json]` (offline guide), and `capabilities --schema NAME` (Draft 2020-12 schema export) keep their documented `kind`, top-level fields, and schema identifier scheme (`https://dircue.invalid/schema/…`). The set of valid schema names is additive-only.
- **JSON schema families.** The bundled schemas under `schema/` are the reference for what a dircue JSON output contains: the aggregate `profile` and the standalone `languages`, `assessment`, `lockfiles`, `availability`, `capabilities`, `cli-capabilities`, `comparison`, `declarations`, `environments`, `explanation`, `findings`, `focus`, `formats`, `guide`, `hotspots`, `planning`, `map`, `map-compare`, `forest` and `stats` documents. Fields present in a 1.x schema stay present, with the same types and semantics. The reserved `$defs["_dircue_bundled_resources"]` key is part of the export contract; do not add a definition with that name to a source schema.
- **`schema_version`.** Versioned report families carry a `schema_version`; legacy language JSON does not. JSON Schema documents also carry their own identifiers. An incompatible report change requires an explicit migration rather than silently reusing an existing version. The aggregate profile schema currently tops out at `1.10.0`; adding a new module in a 1.x release bumps that minor version. Standalone schemas keep their own numbers.
- **Exit statuses.** `0` on success, `1` for handled errors. The `check exit before consuming stdout` rule from the README is part of the contract. `map compare --exit-code`, added in 1.1, explicitly selects outcome exits: `0` for observed unchanged, `1` for observed changed, `2` for uncertainty under its default policy, and `3` for handled errors. Comparison outcomes follow a complete report on stdout and have no error diagnostic; handled errors write diagnostics to stderr. Without the flag, handled errors keep exit `1`. Note the SIGPIPE caveat below.
- **stdout/stderr discipline.** Machine-readable output goes to stdout only; process diagnostics go to stderr. Module diagnostics may also be represented in reports. Legacy language JSON has no warning field, so its warnings are available only on stderr.
- **Boundary guarantees.** Built-in profiling is offline and does not execute inspected content. Directory containment, explicit caller inputs, Git object storage, and the trusted optional worker have distinct boundaries; see [SECURITY.md](../SECURITY.md). Documented resource controls retain their meanings. They are not hard operating-system memory or CPU limits.

### SIGPIPE caveat

When stdout is closed by a downstream reader (`dircue --json . | head`), dircue may exit with the standard Unix SIGPIPE status (`141`) rather than `0`. Consumers that must treat that case as success should read all of stdout before closing. This matches Go's default runtime behavior and is not classified as an error.

## Exclusions from the 1.x contract

- **stderr text.** The exact wording of warnings, spelling suggestions, and diagnostic messages is not part of the contract. Warning **codes** (for example `tree_size_limit`, `file_read_error`, `non_regular_file`, `invalid-global-json`) are stable. New codes may be added; codes are not silently removed within 1.x.
- **Warning presence in JSON.** Warnings in a report's `warnings[]` array are diagnostic evidence. Their code set is stable in the additive sense above. Consumers should preserve unknown codes and use module status and coverage to determine completeness; an unfamiliar warning must not be treated as evidence that analysis succeeded completely.
- **Help text and long descriptions.** The commands and their flags are contract; the copy that explains them is not.
- **JSON object-key order.** As stated in the README, key order is not part of the contract. Ordering of arrays is documented per-module (the language and metrics arrays are stably ordered, file breakdowns follow Linguist's rules, and so on).
- **Performance.** Measured elapsed time, allocation, and peak RSS are workload- and host-dependent. We record the workload and the result when claiming a speedup and investigate regressions before release. No universal latency or memory guarantee is implied.
- **The structural worker protocol.** The wire format between the Go CLI and the native structural worker is not a public integration surface. Third-party workers are unsupported.
- **Go packages.** dircue 1.x is a command-line tool; it has no supported Go API. `internal/` is not importable from outside the module. `pkg/` packages are importable but unsupported: any 1.x release may rename them, change their signatures or move them under `internal/`. A supported Go entry point is planned in [#178](https://github.com/war-and-code/dircue/issues/178) and [#179](https://github.com/war-and-code/dircue/issues/179).
- **Text output for non-`--json` invocations of new subcommands.** Legacy `dircue` (no subcommand) text output is frozen; text rendering of `analyze all`, `plan`, `compare`, and `capabilities` is stable in shape but not byte-identical.

## Additive changes and `schema_version`

New optional modules and commands can be added without changing existing invocations. In 1.1, the map adds `references_artifact` to the edge enum while retaining `schema_version: "1.0.0"`: this is an additive observation under the policy above, with existing edge meanings unchanged. An older strict schema or saved-report reader may reject a 1.1 map containing that edge. Use the current bundled schema and a reader that preserves unknown values; dircue 1.1 reads existing 1.0 maps. This does not promise forward readability by old binaries.

In 1.2, `--lockfiles` and `analyze lockfiles` explicitly select an aggregate profile at `schema_version: "1.8.0"`. Profiles without this module keep their existing aggregate version selection. The environment observer advances to provider version `1.1.0` and adds source-scoped Python, Node and Rust toolchain declarations; the registry observer advances to rule version `1.1.0` and adds selected Maven/Cargo configuration. These optional module reports intentionally change, even when no new supported files are present, because their provider metadata and declared scope describe the expanded observers. Existing .NET environment selections and NuGet/npm registry facts retain their meanings. Current readers accept the documented older environment/profile forms; older strict readers may reject the newer aggregate version, module metadata or fields. Legacy language commands and default `analyze all` do not opt into these modules.

In 1.3, the map adds partial Gradle settings-backed workspace membership, root Procfile `process` deployables and declaration-backed Aspire `runs` relationships. Existing map schema version 1.0.0 and edge types are retained. New settings-only Gradle components and membership facts use `dircue/gradle-workspace@1.0.0`; the deployable observer advances to 1.2.0. Its version metadata changes even when an input has none of the newly supported files. Aspire name-only relationships are corrected: a call must now have supported syntax, an AppHost SDK declaration and an unambiguous direct selected project reference. Unsupported forms can therefore lose earlier heuristic nodes or links. New read-only `map settings` bounds describe these observers. Default language output and default `analyze all` remain unchanged. As with other additive observations, consumers should use the current schema and tolerate unknown properties and provider values; old binaries are not promised to load newer reports.

New fields, warning codes, or enum values still require a review of producer schemas and saved-report readers: a strict existing reader may reject them. Report versioning and consumer guidance must describe that impact. A newer binary reads the report versions it documents; an older binary is not promised to accept reports emitted by a newer version.

Removing a field, tightening an enum, or narrowing accepted input requires a major bump and is not planned within 1.x. When the design requires such a change, we would ship it in a 2.0 with a migration note.

## Deprecation policy

- A deprecation is announced in a CHANGELOG entry that names the surface, the replacement, and the earliest release the surface might be removed in.
- The deprecated surface keeps working for at least two subsequent minor releases after the announcement. A shorter window requires a security or correctness justification, called out in the CHANGELOG.
- `dircue` prints a deprecation notice to stderr on invocations that touch the deprecated surface. `--json` output is never mutated to carry a deprecation message.
- Removal happens no earlier than the next major release.

## What a 2.0 would mean

A major bump is reserved for changes we cannot make additively: renaming or removing a documented CLI flag, tightening an existing JSON field's type or values, dropping support for a saved-report schema version, changing the exit-code semantics for a documented invocation, or removing the legacy Linguist directory JSON output. The release notes would list every such change and provide a migration recipe. We do not plan a 2.0 for the 1.x line.

## Supported platforms and toolchains

Core release archives and wheels are prepared for:

- Linux amd64 (glibc-compatible, and musl through the packaged `musllinux` wheel tag).
- Linux arm64 (glibc-compatible, and musl).
- macOS 12+ on Apple Silicon and Intel.
- Windows 10 / Server 2016 and newer, amd64.

Building from source requires Go pinned to the version in `go.mod` (currently 1.26.6). The release workflow reproduces that exact compiler version; local builds should as well. The Python wheel launcher needs Python 3.10 or newer. The optional structural worker is packaged separately and has its own build toolchain (Rust 1.94.0, pinned in the [worker guide](STRUCTURE.md)).

The Dockerfile supports local image builds; no container registry publication is currently part of the release process.

See the [distribution guide](DISTRIBUTION.md) for the packaging matrix and the [release automation guide](RELEASE_AUTOMATION.md) for which of these platforms the release workflow builds and validates.

<a id="known-boundaries-at-10"></a>

## Current boundaries

These are current limitations and follow-up work. They do not establish support for untested inputs or override the coverage reported by each module.

- Bare, unborn, or SHA-256 Git repositories, Git alternates, and `GIT_DIR` overrides; subdirectory discovery inside a repository root. Support and diagnostics vary by repository shape; do not assume all unsupported forms fail identically. When `--source auto` falls back from a SHA-256, unborn or unreadable Git directory to directory mode, it says so on stderr (`git_object_format_unsupported`, `git_no_commits_or_corrupt_gitdir`, `git_head_not_found`). Tracked in [#66](https://github.com/war-and-code/dircue/issues/66).
- `analyze focus --affected-by <path>` for a path with no matching declaration returns `status: complete` and echoes the query rather than emitting a dedicated "no match" sentinel. Tracked in [#67](https://github.com/war-and-code/dircue/issues/67).
- Registry adapter coverage: `analyze registries` reads selected NuGet.Config, .npmrc, Maven `settings.xml` and `.cargo/config`/`.cargo/config.toml` files. Other package sources, including `pip.conf`, Yarn configuration and Maven POM repository declarations, remain outside scope. Tracked in [#68](https://github.com/war-and-code/dircue/issues/68).
- Environment adapter breadth: `analyze environments` covers the dimensions listed in [ENVIRONMENTS.md](ENVIRONMENTS.md); other ecosystems' environment declarations are not modeled yet. Tracked in [#65](https://github.com/war-and-code/dircue/issues/65).
- Structural worker isolation: the worker inherits the caller's process environment and working directory and is not sandboxed; the caller is responsible for the worker binary's origin. Tracked in [#69](https://github.com/war-and-code/dircue/issues/69).

## 1.4 repository measurements

`analyze assessment` and `analyze all --assessment` opt into aggregate schema `1.9.0`, with an `assessment` component at version `1.0.0`. The component adds file and logical-byte populations, vendored-file and vendored-byte totals, filename-selected manifest counts with ecosystem and kind, parsed project roots, explicit local relationships, and supported lockfile association totals with per-metric completeness and per-role breakdowns. Older strict profile readers may reject it. Profiles without this opt-in keep their existing version selection.

Assessment and `--npm-workspace-locks` enable shared npm v2/v3 workspace-lock ownership checks when selected declarations establish one owner and the lock contains the member's own package entry. The additional association check and preservation of literal POSIX backslashes in selected paths are disclosed in the lockfile module's semantics array; older strict lockfile readers may reject that opt-in semantics array.

The following behaviors differ from 1.3.1 in the default `analyze lockfiles` and `analyze all --lockfiles` output (without `--npm-workspace-locks`):

- Yarn, pnpm, Bun, and Rush files (`yarn.lock`, `pnpm-lock.yaml`, `bun.lock`, `bun.lockb`, `pnpm-workspace.yaml`, `rush.json`) and non-npm `packageManager` declarations now yield `unsupported` or `indeterminate` outcomes; 1.3.1 usually reported these projects as `missing`.
- A project whose own `package.json` declares a non-npm `packageManager` is `unsupported` even beside its own npm lockfile, which 1.3.1 reported as `observed` with a named check.
- An npm project with no readable dependency declarations whose dependency or workspace fields have diagnostics is `indeterminate` (`npm-manifest-declarations-unresolved`); 1.3.1 reported it as `not_applicable`.
- `.fsproj` and `.vbproj` projects receive NuGet lockfile contexts alongside `.csproj` projects.
- An omitted path that cannot be an MSBuild project file in the project's directory no longer makes a NuGet association indeterminate.
- A NuGet project with no direct package declarations and selected shared MSBuild inputs is now `indeterminate`, rather than `not_applicable`, because those inputs may introduce packages. An `Update`-only item no longer creates a direct package declaration: without a lockfile or shared/imported inputs it is `not_applicable`, rather than `missing`.
- The module's `inventory_paths` coverage count includes those Yarn, pnpm, Bun, and Rush files.

Npm manifests beginning with a UTF-8 byte-order mark are now parsed as npm parses them. This can add declaration, project, or map observations that older versions omitted. MSBuild `PackageReference` and `PackageVersion` `Update` items no longer introduce package observations; `Include` declarations remain. These are parser corrections, not changes to the map's field semantics.

Optional Syft evidence stays separate and does not establish native ownership or completeness.

The command catalog, workflow guide, schema exports, module capability registry and inert planner gain assessment entries. Saved-profile comparison accepts schema `1.9.0` and compares assessment aggregates; capped example lists remain metadata. Legacy language output, default `analyze all`, map field semantics, and existing exit codes are unchanged.

## 1.5 structural assessment

Assessment opt-ins use aggregate schema `1.10.0` with assessment version `1.1.0`. Existing fields retain their definitions; the new `structure` object adds ecosystem/role populations, explicit workspace and solution groups, definite local edges, qualified-reference counts, observed connected groups, static entry-point observations, and scoped coverage. Current readers accept the earlier assessment version `1.0.0`; older strict readers may reject the new aggregate version and fields. Commands without assessment retain their previous aggregate version selection.

Connected-group counts describe the retained observed graph. They do not count independent applications or estimate an unknown full graph. Assessment qualifies Maven coordinate-only relationships without an explicitly declared reactor even when the map retains its coordinate-based relationship. Entry points reuse supported manifest/deployment rules, omit script bodies, and disclose that source-code entry-point inspection did not run.

The map keeps schema version 1.0.0 and its edge types, but some edges and deployables change. A `member_of` or `depends_on_local` edge whose `condition` holds only a dependency section or scope (npm, Go replacements, Dart, Cargo scopes, uv groups) is now complete instead of partial when the declaration establishes a local relationship. A Go local replacement without a matching `require` in the same `go.mod` is retained as a qualified reference (`go_replacement_activation_unresolved`), rather than being promoted to a dependency on the directive alone. Workspace (`go.work`) replacement observations retain partial coverage; workspace activation is not evaluated. A Maven `parent` edge whose declared parent coordinates differ from the POM at its path becomes a `qualified_local_reference` fact with `parent_coordinates_mismatch` or `parent_version_mismatch`, and one whose declared parent uses a property or leaves coordinates for Maven 4 to infer becomes partial. The deployable observer advances to 1.3.0: CloudFormation `AWS::Serverless::Function` and `AWS::Lambda::Function` resources have kind `function`, ECS task definitions, Batch job definitions, and App Runner services have kind `container_task`, Helm charts with `type: library` have kind `library`, and a Skaffold artifact without `context` is recorded with Skaffold's default context instead of being skipped. Its version metadata changes even when an input has none of these files. Map text summaries count non-compute CloudFormation resources and Helm libraries as supporting declarations rather than runnable deployables.

Lockfile observations advance to provider version `1.1.0`. Profiles selecting lockfiles without assessment remain aggregate schema `1.8.0`; provider versions describe the additional module fields. Current readers also accept lockfile provider `1.0.0`. NuGet contexts add `nuget_evidence`, separating candidate presence and ownership from the existing named-check result. This intentionally changes optional lockfile output even on inputs that receive no more specific conclusion.

Compared with 1.4.0, supported literal custom lock paths, harmless shared MSBuild inputs, version-only central package declarations, and confined unconditional imports can now produce an observed association or a named direct-ID check where the older analyzer returned indeterminate. A path collision qualifies only the owners of that path. Unsupported expressions, SDK imports outside the static model, version-qualified SDK identities and selected project-SDK overrides, shared package items, and incomplete selected inputs remain indeterminate. The codes `nuget-imported-project-input-unresolved` and `nuget-shared-inputs-or-custom-lock-path-unresolved` are retired in favor of `nuget-msbuild-input-unmodeled`, `nuget-lock-path-conditional`, `nuget-package-reference-conditional`, `nuget-package-reference-dynamic`, and the existing specific codes. Outcomes can also change where MSBuild's own rules now apply, for example when a project disables `Directory.Build.targets` that sets a lock path, or when a shared file's name differs from `Directory.Build.props` only by letter case, which is a possible import on a case-insensitive file system and is now qualified. These are static evidence refinements, not claims about successful restore, effective package versions, or dependency freshness; [LOCKFILES.md](LOCKFILES.md) defines the exact subset.

Saved-report comparison includes structural aggregates, scoped coverage, distinct entry-point, row, and project-association totals, and the NuGet check and presence partitions. Bounded group/member/edge/entry-point examples and lockfile causes remain metadata, so trimming samples cannot look like removed projects. Default languages, default `analyze all`, map field meanings, and exit codes retain their contracts. The optional assessment performs additional bounded deployment observation and joins; its cost is separate from the preserved language-only path.
