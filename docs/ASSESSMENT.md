# Repository measurements

Added in 1.4.0; structural summaries expanded in 1.5.0.

Assessment combines language statistics with factual repository measurements. It works on an extracted directory or a selected Git tree, without Syft, package-manager execution, network access, or a structural worker. It does not decide whether a repository is large, whether it is a monorepo, or which tools a caller should run.

```sh
dircue analyze assessment --source directory --json /content
dircue analyze all --assessment --source directory --json /content
dircue analyze all --assessment --source git --rev HEAD --json /checkout
dircue capabilities --schema assessment
```

Both analysis commands return the versioned aggregate profile. In 1.5.0, the `assessment` component is version `1.1.0` and uses profile schema `1.10.0`. Current readers also accept the earlier assessment version `1.0.0` in profile `1.9.0`; strict older readers may reject the new fields. Assessment requests metadata discovery, passive project declarations, supported lockfile observations, and manifest/deployment entry points. Languages keep their existing inclusion rules. File and byte measurements describe a wider population than language statistics.

`--source directory` reads current files and supports directories with no Git metadata. Automatic source selection at a Git repository root normally reads committed `HEAD`; it does not include dirty or untracked changes. Directory inspection is not an atomic snapshot. Read failures fail by default; `--on-error continue` records qualified evidence.

## Populations and definitions

The `definitions` array describes how measurements are formed. Each metric records a `count`, its `scope`, `completeness`, and any qualifying `reasons`. These are observations for caller-defined analysis, not organizational policy.

File totals count selected regular files. Bytes are logical content sizes, not allocated disk blocks, Git history size, archive-expanded bytes, or process memory.

The `inventory` field also counts `vendored_files` and `vendored_bytes`: files that Linguist's vendor path rules match (for example paths under `node_modules/`, `vendor/`, `dist/`, `test/fixtures/`, `testdata/`, or `.github/`) or that a `linguist-vendored` attribute marks. Without an attribute override, `.venv/` matches no Linguist vendor rule and stays in the unvendored file and byte totals.

Manifest counts are filename candidates grouped by normalized filename pattern and ecosystem, with a `kind` field identifying each group's role: `manifest`, `workspace`, `solution`, `configuration`, or `toolchain`. Variable project names become patterns such as `*.csproj`; concrete paths remain bounded evidence. They do not establish that a manifest parses or describes a functioning project. Installed npm contents are outside the project-manifest population. Lockfiles and checksum files (`package-lock.json`, `go.sum`, `Cargo.lock`, `yarn.lock`, and similar) record resolutions rather than declarations; they appear in `filename_candidates` under the `lockfile` kind, not in `manifest_candidates`.

`unparsed_manifest_candidates` counts selected manifests that the declaration parser did not read in full and interpret, because they were unreadable, invalid, unsupported, or over a declaration input limit. A manifest merged into another record (such as a `requirements.txt` beside a `pyproject.toml`), one read and found to hold no declarations, or one parsed and then dropped at the declaration report size limit is not counted as unparsed; the last makes project metrics a lower bound instead.

Parsed package and project records and their distinct directories are separate measurements. The project count excludes virtual workspace roots (`go.work`, a Cargo manifest with only `[workspace]`, a uv workspace without `[project]`), which still contribute workspace membership. It also excludes configuration, solution, and annotation records such as `config/application.rb`, Cargo manifests with neither a package nor a workspace table, and `pyproject.toml` files that hold only tool settings: neither a `project` nor a `build-system` table, and no Python lockfile beside them. An unrecognized declaration record kind makes project metrics a lower bound with reason `unclassified_declaration_kind`. Several manifests can describe one project and several projects can share a directory, so manifest counts are not project counts.

`projects_by_role` and `project_roots_by_role` partition those counts by the path roles `dircue map` uses ([role values](MAP.md#the-role-property)): `vendored`, `fixture`, `example`, `test`, `docs`, `tooling`, and `primary`. These are conventional filename and directory hints; `primary` is the fallback when no auxiliary role matches. A root takes the highest-priority role among its projects, in that order.

Workspace membership and local dependency declarations are separate relationships. A directory hierarchy or a matching package name alone does not establish either relationship. Conditions and unresolved targets remain qualified evidence. Reference counts include confined declared targets; they are not a count of proven connections between functioning projects. Cargo default-member selection remains in declarations and does not add workspace memberships.

Lockfile association counts describe supported static ownership rules. They do not establish dependency resolution, a fresh lockfile, a complete SBOM, or a successful locked restore.

A `complete` count covers the supported population named in its scope. It is not a claim to support every ecosystem or evaluate every build expression. A `lower_bound` count contains the observations retained under its disclosed omissions. Do not turn a lower bound into a denominator for a supposedly exact percentage.

## Structural summaries

`structure.populations` partitions five populations by ecosystem and path role: `filename_candidates`, `parsed_projects`, `distinct_roots`, `workspace_groups`, and `solution_groups`. Each row has its own metric and reasons. Candidate counts come from selected filenames before parsing or evidence trimming. Invalid manifests remain candidates; they cannot become parsed projects. Several projects may share one root. A physical directory containing manifests for several ecosystems appears in each ecosystem's root population, so those root counts cannot be added to obtain a distinct-directory total.

`workspace_groups` and `solution_groups` retain explicit declarations, including supported empty workspace groups. A group identifies its manifest, member count, bounded member paths, and unresolved members with their state and resolution. The full group totals and omitted-example counts remain separate from retained lists. Directory nesting establishes neither group membership nor dependency.

The group observers cover supported Maven modules, literal Gradle settings, npm and uv workspaces, Cargo workspaces, Go workspaces, and .NET solutions. Project identities also cover the other manifest families supported by the declaration adapters, including Dart, PHP, Ruby, Swift, and Elixir. Detail varies by ecosystem: recognizing a project does not establish full build-system support. Unsupported workspace declarations keep the affected structural scope partial rather than making unrelated ecosystems uncertain.

`structure.dependencies` separates definite local edges from `qualified_references`, grouped by ecosystem, declaration kind, state, and resolution. A definite edge requires retained, parsed endpoints and an unconditional supported local reference. A Maven dependency coordinate match additionally requires both projects in an explicitly declared, unconditional reactor group. Coordinate-only matches, ambiguous coordinates, conditions, missing targets, and self-references stay qualified. The legacy local-reference totals still count declarations; they are not interchangeable with definite-edge totals.

`connected_groups` counts weakly connected components in the observed definite-edge graph, including isolated parsed projects. Direction is ignored when forming a component but retained on each edge. This count is exact for that observed graph. It is not a lower bound on the number of components in the complete repository: missing edges can merge groups and missing projects can add groups. Consult the separate `dependency_connectivity` coverage before using it. Connected groups do not establish independent applications, services, builds, or deployability.

`structure.coverage` separates `workspace_membership`, `solution_membership`, `project_dependencies`, `dependency_connectivity`, and `entry_points` by ecosystem. Declaration diagnostics qualify the relationship scope they could affect; file traversal omissions separately qualify inventory populations. A complete inventory can therefore coexist with partial structural evidence. For example, `coordinate_match_without_declared_reactor`, `self_reference_qualified`, and `shared_msbuild_project_references_not_applied` identify distinct limits on dependency evidence. Gradle's unresolved build-evaluation requirements qualify its dependency and connectivity scopes with `build_declarations_require_evaluation`; its known project counts remain exact.

`structure.entry_points` lists supported manifest interfaces and deployment observations, with a project association when the existing static rules support one. Each row names its evidence path, kind, ecosystem, role, basis, and state: `declared`, `qualified`, or `unassociated`. A declared script name is retained without its shell command. Build and run associations stay qualified; file proximity alone does not prove that a project runs or builds through an entry point. The catalog reuses deployment observers without running the source-code intent detectors, so entry-point coverage remains partial with `source_entry_points_not_inspected`. This avoids treating no observed entry point as proof that none exists.

## Lockfile associations

The component exposes overall and per-ecosystem project counts, an eligible population, and covered, missing, not-applicable, unsupported, and unknown outcomes. `eligible` counts npm and NuGet projects whose outcome is `covered`, `missing`, or `unknown`; projects that are `not_applicable` (no direct declarations) or `unsupported` are excluded. It describes the static checker population; it is not a policy requirement.

Each lockfile row, including `lockfiles_overall`, has a `by_role` array partitioning its counts by the same path roles as `projects_by_role`. Each row also has `outcome_reasons`: a list of `{state, reason, count}` triples for every non-covered project, where `reason` is the project's lockfile boundary reason, `no-direct-declarations` for every `not_applicable` project, or one of `no-lockfile-context`, `lockfiles-skipped`, `lockfiles-not-run`, `ecosystem-outside-association-scope`, or `unspecified`.

Assessment always applies `--npm-workspace-locks` association semantics. A covered project has a supported observed association; its named check can still be `different` or `indeterminate`. The existing `lockfiles.contexts` provide per-project paths, checks, and boundaries.

A workspace member is covered by its workspace root's npm lockfile only when that root is the nearest ancestor whose `workspaces` list the member, as npm selects it, and the lockfile has the member's own package entry; the member is compared with that entry. NuGet can inspect confined literal imports, harmless shared files, version-only central package declarations, and supported literal custom lock paths. Its per-context `nuget_evidence` distinguishes candidate presence from ownership; named checks retain their separate status. Conditions, unknown expressions, shared package items, collisions, and input limits remain qualified. See [lockfile observations](LOCKFILES.md) for the exact subset and reason codes.

Selected `pnpm-workspace.yaml`, `lerna.json`, and `rush.json` files outside `node_modules` are recognized but not parsed, so their presence makes workspace membership and local dependency counts lower bounds (`pnpm_workspace_unparsed`, `lerna_workspace_unparsed`, `rush_workspace_unparsed`).

## Evidence and limits

If the selected tree exceeds `--tree-size` (default 100,000 entries), assessment is skipped: all counts are zero lower bounds with reason `tree_size_limit`. Raise `--tree-size` to measure larger trees.

A declaration omission makes project metrics a lower bound only when the omitted path could be a manifest, is a directory that could not be read (outside `node_modules`), or when omissions cannot be attributed to paths; a symbolic link or Git submodule link does not.

The scanner API can also summarize environment directories instead of traversing them. This qualifies inventory counts and any project, relationship, or association population the skipped subtree could affect. Installed `node_modules` contents remain outside the project-manifest population.

Candidate examples retain at most 256 paths per kind; project-directory examples retain at most 256 paths; workspace and local-reference examples each retain at most 64 entries. Paths longer than 3,072 bytes are excluded from these examples and counted in their omissions. These limits apply to display evidence independently of aggregate counts. Escaped JSON evidence also has a 7 MiB budget; the assessment component has an 8 MiB serialized ceiling. Budget trimming can reduce the retained examples further and increases their omitted-example counts without changing aggregates. An omitted example is not an omitted file. Parser limits, unreadable inputs, or an incomplete selected inventory are different: they qualify the affected project, relationship, or association population.

Structural examples retain at most 256 workspace groups and 256 solution groups, 64 resolved and 64 unresolved members per group, 512 definite edges, 256 connected components with 64 project paths each, 512 qualified-reference categories, and 512 entry-point rows. The same path and JSON-byte budgets can trim these lists further. Counts are formed before display trimming, and omitted counts reconcile each sample with its observed total.

The source traversal defaults to the existing 100,000-entry limit. Declaration parsing retains at most 4,096 documents, with a 1 MiB per-document and 64 MiB total input ceiling. Lockfile inspection retains at most 256 lockfiles and 4,096 contexts, with a 1 MiB per-file and 16 MiB total input ceiling. The companion modules expose their full limits and coverage.

The existing .NET declaration adapter cannot faithfully interpret a selected POSIX filename containing a literal backslash. Assessment retains its exact file and manifest counts, but qualifies the affected project, root, relationship, and association populations with `dotnet_selected_path_identity_ambiguous`; it does not invent a project at a normalized path. This differs from backslashes used as separators inside a build declaration.

Inspect the metric itself before interpreting absence. An empty language array does not mean an empty directory. Zero known project relationships does not prove independent builds when declarations were incomplete. Unsupported ecosystems and unknown outcomes remain visible.

The assessment module reuses the selected source and declaration or lockfile reports. It does not scan a second directory tree. Existing manifest, inventory, content-read, and output bounds still apply.

## Cost

The 1.4.0 assessment used roughly the same work as explicitly requesting discovery, declarations, and lockfiles. Version 1.5.0 also inspects selected deployment files and joins bounded structural evidence. The language-only and default aggregate commands do not opt into this work. Added cost depends on repository contents and the selected limits; warm-cache measurements cannot establish a guarantee for another machine or repository.

## Reading the report

The component is at `.assessment` in the JSON profile; per-project lockfile contexts are at `.lockfiles.contexts`.

Count of primary-role npm projects without a lockfile:

```sh
dircue analyze assessment --source directory --json /repo \
  | jq '.assessment.lockfiles[] | select(.ecosystem=="npm") | .by_role[] | select(.role=="primary") | .missing'
```

Manifest paths of npm projects where no lockfile was found:

```sh
dircue analyze assessment --source directory --json /repo \
  | jq '.lockfiles.contexts[] | select(.ecosystem=="npm" and .association_state=="missing") | .manifest_path'
```

Outcome reasons for npm projects (state, reason code, and count for each non-covered group):

```sh
dircue analyze assessment --source directory --json /repo \
  | jq '.assessment.lockfiles[] | select(.ecosystem=="npm") | .outcome_reasons[]'
```

Primary project populations by ecosystem and structural qualification:

```sh
dircue analyze all --assessment --source directory --json /repo \
  | jq '.assessment.structure | {populations: [.populations[] | select(.role=="primary")], coverage}'
```

Observed connected groups and their bounded project lists:

```sh
dircue analyze assessment --source directory --json /repo \
  | jq '.assessment.structure | {dependencies, coverage: [.coverage[] | select(.scope=="dependency_connectivity")]}'
```

## Optional Syft evidence

An existing Syft report can enrich the same invocation:

```sh
dircue analyze all --assessment --source directory --json \
  --syft-report /reports/syft.json --syft-root / /content
```

Dircue imports the report once through its existing [package-evidence importer](PACKAGE_EVIDENCE.md). It does not invoke Syft or recreate its package catalog. Native measurements remain based on the selected source: an attachment cannot establish a missing manifest, workspace membership, lockfile ownership, or complete native coverage. Package attribution retains its separate coordinate-mapping, source-binding, schema, and coverage checks. Keep supplied reports and saved dircue outputs outside the directory being measured if they should not contribute to its file or byte totals.

A caller can inspect the native report first, apply its own policy, and import a Syft report in a later invocation.
