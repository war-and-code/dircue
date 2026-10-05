# Repository measurements

Added in the 1.4.0 candidate.

Assessment combines language statistics with factual repository measurements. It works on an extracted directory or a selected Git tree, without Syft, package-manager execution, network access, or a structural worker. It does not decide whether a repository is large, whether it is a monorepo, or which tools a caller should run.

```sh
dircue analyze assessment --source directory --json /content
dircue analyze all --assessment --source directory --json /content
dircue analyze all --assessment --source git --rev HEAD --json /checkout
dircue capabilities --schema assessment
```

Both analysis commands return the versioned aggregate profile. The `assessment` component uses profile schema `1.9.0`; existing commands without this option keep their previous contracts. Assessment requests metadata discovery, passive project declarations, and supported lockfile observations. Languages keep their existing inclusion rules. File and byte measurements describe a wider population than language statistics.

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

## Lockfile associations

The component exposes overall and per-ecosystem project counts, an eligible population, and covered, missing, not-applicable, unsupported, and unknown outcomes. `eligible` counts npm and NuGet projects whose outcome is `covered`, `missing`, or `unknown`; projects that are `not_applicable` (no direct declarations) or `unsupported` are excluded. It describes the static checker population; it is not a policy requirement.

Each lockfile row, including `lockfiles_overall`, has a `by_role` array partitioning its counts by the same path roles as `projects_by_role`. Each row also has `outcome_reasons`: a list of `{state, reason, count}` triples for every non-covered project, where `reason` is the project's lockfile boundary reason, `no-direct-declarations` for every `not_applicable` project, or one of `no-lockfile-context`, `lockfiles-skipped`, `lockfiles-not-run`, `ecosystem-outside-association-scope`, or `unspecified`.

Assessment always applies `--npm-workspace-locks` association semantics. A covered project has a supported observed association; its named check can still be `different` or `indeterminate`. The existing `lockfiles.contexts` provide per-project paths, checks, and boundaries.

A workspace member is covered by its workspace root's npm lockfile only when that root is the nearest ancestor whose `workspaces` list the member, as npm selects it, and the lockfile has the member's own package entry; the member is compared with that entry. NuGet associations keep their restrictions on conditional references, imported or shared build inputs, several projects in one directory, and custom lockfile paths. See [lockfile observations](LOCKFILES.md) for the rules, named checks, and reason codes.

Selected `pnpm-workspace.yaml`, `lerna.json`, and `rush.json` files outside `node_modules` are recognized but not parsed, so their presence makes workspace membership and local dependency counts lower bounds (`pnpm_workspace_unparsed`, `lerna_workspace_unparsed`, `rush_workspace_unparsed`).

## Evidence and limits

If the selected tree exceeds `--tree-size` (default 100,000 entries), assessment is skipped: all counts are zero lower bounds with reason `tree_size_limit`. Raise `--tree-size` to measure larger trees.

A declaration omission makes project metrics a lower bound only when the omitted path could be a manifest, is a directory that could not be read (outside `node_modules`), or when omissions cannot be attributed to paths; a symbolic link or Git submodule link does not.

The scanner API can also summarize environment directories instead of traversing them. This qualifies inventory counts and any project, relationship, or association population the skipped subtree could affect. Installed `node_modules` contents remain outside the project-manifest population.

Candidate examples retain at most 256 paths per kind; project-directory examples retain at most 256 paths; workspace and local-reference examples each retain at most 64 entries. Paths longer than 3,072 bytes are excluded from these examples and counted in their omissions. These limits apply to display evidence independently of aggregate counts. Escaped JSON evidence also has a 7 MiB budget; the assessment component has an 8 MiB serialized ceiling. Budget trimming can reduce the retained examples further and increases their omitted-example counts without changing aggregates. An omitted example is not an omitted file. Parser limits, unreadable inputs, or an incomplete selected inventory are different: they qualify the affected project, relationship, or association population.

The source traversal defaults to the existing 100,000-entry limit. Declaration parsing retains at most 4,096 documents, with a 1 MiB per-document and 64 MiB total input ceiling. Lockfile inspection retains at most 256 lockfiles and 4,096 contexts, with a 1 MiB per-file and 16 MiB total input ceiling. The companion modules expose their full limits and coverage.

The existing .NET declaration adapter cannot faithfully interpret a selected POSIX filename containing a literal backslash. Assessment retains its exact file and manifest counts, but qualifies the affected project, root, relationship, and association populations with `dotnet_selected_path_identity_ambiguous`; it does not invent a project at a normalized path. This differs from backslashes used as separators inside a build declaration.

Inspect the metric itself before interpreting absence. An empty language array does not mean an empty directory. Zero known project relationships does not prove independent builds when declarations were incomplete. Unsupported ecosystems and unknown outcomes remain visible.

The assessment module reuses the selected source and declaration or lockfile reports. It does not scan a second directory tree. Existing manifest, inventory, content-read, and output bounds still apply.

## Cost

A five-run directory-mode comparison on rails, npm-cli, Roslyn, dotnet-samples, and ASP.NET Core found `analyze all --assessment` between 3% faster and 42% slower than plain `analyze all`. It was within 2% of `analyze all --discovery --declarations --lockfiles --npm-workspace-locks`, which performs the same companion analyses. After removing the assessment component and its schema-version bump, those companion reports matched exactly. These are descriptive warm-cache medians on one machine, not a performance guarantee; the added cost depends on repository contents and the chosen analysis limits.

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

## Optional Syft evidence

An existing Syft report can enrich the same invocation:

```sh
dircue analyze all --assessment --source directory --json \
  --syft-report /reports/syft.json --syft-root / /content
```

Dircue imports the report once through its existing [package-evidence importer](PACKAGE_EVIDENCE.md). It does not invoke Syft or recreate its package catalog. Native measurements remain based on the selected source: an attachment cannot establish a missing manifest, workspace membership, lockfile ownership, or complete native coverage. Package attribution retains its separate coordinate-mapping, source-binding, schema, and coverage checks. Keep supplied reports and saved dircue outputs outside the directory being measured if they should not contribute to its file or byte totals.

A caller can inspect the native report first, apply its own policy, and import a Syft report in a later invocation.
