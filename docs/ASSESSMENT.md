# Repository measurements

Added in the 1.4.0 candidate.

Assessment combines language statistics with factual repository measurements. It works on an extracted directory or a selected Git tree, without Syft, package-manager execution, network access, or a structural worker. It does not decide whether a repository is large, whether it is a monorepo, or which tools a caller should run.

```sh
dircue analyze assessment --source directory --json /content
dircue analyze all --assessment --source directory --json /content
dircue analyze all --assessment --source git --rev HEAD --json /checkout
dircue capabilities --schema assessment
```

Both analysis commands return the versioned aggregate profile. The new `assessment` component uses profile schema `1.9.0`; existing commands without this option keep their previous contracts. Assessment requests metadata discovery, passive project declarations, and supported lockfile observations. Languages keep their existing inclusion rules. File and byte measurements describe a wider population than language statistics.

`--source directory` reads current files and supports directories with no Git metadata. Automatic source selection at a Git repository root normally reads committed `HEAD`; it does not include dirty or untracked changes. Directory inspection is not an atomic snapshot. Read failures fail by default; `--on-error continue` records qualified evidence.

## Populations and definitions

The `definitions` array describes how measurements are formed. Each metric records a `count`, its `scope`, `completeness`, and any qualifying `reasons`. These are observations suitable for caller-defined assessment, not an organizational policy.

- File totals count selected regular files. Bytes are logical content sizes, not allocated disk blocks, Git history size, archive-expanded bytes, or process memory.
- Manifest counts are filename candidates grouped by normalized filename pattern and ecosystem. Variable project names become patterns such as `*.csproj`; concrete paths remain bounded evidence. They do not establish that a manifest parses or describes a functioning project. Installed npm contents are outside the project-manifest population.
- Successfully parsed package/project records and their distinct directories are separate measurements. Configuration and solution records can contribute references but do not inflate the project count. Virtual Cargo and uv workspace records are included; configuration-only Go workspace records are excluded. Several project manifests can occupy one directory. Multiple files can contribute to a project declaration; manifest counts must not be used as an interchangeable project count.
- Workspace membership and local dependency declarations are separate relationships. A directory hierarchy or a matching package name alone does not establish either relationship. Conditions and unresolved targets remain qualified evidence. Reference counts include confined declared targets; they are not a count of proven connections between functioning projects. Cargo default-member selection remains in declarations and does not add workspace memberships.
- Lockfile association counts describe supported static ownership rules. They do not establish dependency resolution, a fresh lockfile, a complete SBOM, or a successful locked restore.

A `complete` count covers the supported population named in its scope. It is not a claim to support every ecosystem or evaluate every build expression. A `lower_bound` count contains the observations retained under its disclosed omissions. Do not turn a lower bound into a denominator for a supposedly exact percentage.

## Lockfile associations

The component exposes overall and per-ecosystem project counts, an eligible population, and covered, missing, not-applicable, unsupported, and unknown outcomes. Eligibility describes the documented static checker population; it does not mean that an organization requires that project to use a lockfile. The outcome counts partition their reported population; the eligible count is an overlapping denominator rather than another outcome to add to that partition.

A covered project has a supported observed association. Its named check can still be different or indeterminate. The existing `lockfiles.contexts` provide per-project paths, checks, and boundaries.

For npm, assessment can associate a shared v2/v3 root lock with a member only when the selected declarations establish unambiguous workspace ownership and the lock contains the member's own package entry. Merely finding a lockfile in a parent directory is insufficient. Members are compared with their own entry, not the root package's dependencies. Assessment also preserves literal backslashes in selected POSIX filenames instead of treating them as path separators; the opt-in lockfile semantics identify this behavior. Incomplete membership, conflicting owners, unsupported formats, and ambiguous lock selection remain qualified.

NuGet associations retain the existing passive restrictions on conditional project references, imported/shared inputs, multiple projects in one directory, and custom lockfile paths. A missing conventional file does not mean that lockfile use was requested. The module does not reproduce MSBuild evaluation. See [lockfile observations](LOCKFILES.md) for supported formats and named checks.

## Evidence and limits

Candidate examples retain at most 256 paths per kind; project-directory examples retain at most 256 paths; workspace and local-reference examples each retain at most 64 entries. Paths longer than 3,072 bytes are excluded from these examples and counted in their omissions. These limits apply to display evidence independently of aggregate counts. Escaped JSON evidence also has a 7 MiB budget; the assessment component has an 8 MiB serialized ceiling. Budget trimming can reduce the retained examples further and increases their omitted-example counts without changing aggregates. An omitted example is not an omitted file. Parser limits, unreadable inputs, or an incomplete selected inventory are different: they qualify the affected project, relationship, or association population.

Inspect the metric itself before interpreting absence. An empty language array does not mean an empty directory. Zero known project relationships does not prove independent builds when declarations were incomplete. Unsupported ecosystems and unknown outcomes remain visible.

The existing .NET declaration adapter cannot faithfully interpret a selected POSIX filename containing a literal backslash. Assessment retains its exact file and manifest counts, but qualifies the affected project, root, relationship, and association populations with `dotnet_selected_path_identity_ambiguous`; it does not invent a project at a normalized path. This differs from backslashes used as separators inside a build declaration.

The new module reuses the selected source and declaration/lockfile reports. It does not scan a second directory tree. Existing manifest, inventory, content-read, and output bounds still apply; their disclosure is part of the result rather than a silent success.

The source traversal defaults to the existing 100,000-entry limit. Declaration parsing retains at most 4,096 documents, with a 1 MiB per-document and 64 MiB total input ceiling. Lockfile inspection retains at most 256 lockfiles and 4,096 contexts, with a 1 MiB per-file and 16 MiB total input ceiling. The companion modules expose their full limits and coverage. CLI resource controls can lower these ceilings; they do not turn skipped parsing into complete project metrics.

## Optional Syft evidence

An existing Syft report can enrich the same invocation:

```sh
dircue analyze all --assessment --source directory --json \
  --syft-report /reports/syft.json --syft-root / /content
```

Dircue imports the report once through its existing [package-evidence importer](PACKAGE_EVIDENCE.md). It does not invoke Syft or recreate its package catalog. Native measurements remain based on the selected source: an attachment cannot establish a missing manifest, workspace membership, lockfile ownership, or complete native coverage. Package attribution retains its separate coordinate-mapping, source-binding, schema, and coverage checks. Keep supplied reports and saved dircue outputs outside the directory being measured if they should not contribute to its file/byte totals.

A caller can inspect the native report first, apply its own policy, and import a Syft report in a later invocation.
