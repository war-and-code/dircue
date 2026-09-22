# 1.0 compatibility promise

Version 1.0 is the point at which dircue's public surfaces become
contract. This document names those surfaces, what they promise, how
additive changes are versioned, and what deprecation looks like. It
complements [DESIGN_PRINCIPLES.md](DESIGN_PRINCIPLES.md), which
explains why we treat these surfaces as contract at all.

Anything not listed here is not covered. If a downstream consumer
depends on unspecified behavior, the correct next step is an
[issue](https://github.com/war-and-code/dircue/issues) proposing that
we cover it explicitly.

## What is frozen in 1.x

Successful invocations of the surfaces below produce byte-identical
output across 1.x releases unless a defect is being corrected, in
which case the diff is recorded in `CHANGELOG.md` and in the
`tests/compatibility_v100/` harness as an explicit exception.

- **Legacy Linguist CLI.** `dircue` with no subcommand emits the
  language-keyed directory JSON when invoked with `--json`, and the
  Linguist plain-text layout otherwise. Supported flags — `-j/--json`,
  `-b/--breakdown`, `-s/--strategies`, `-r/--rev`, `--tree`,
  `-t/--tree-size`, `--source`, `--on-error`, `--workers`,
  `--max-file-bytes`, `-v/--version`, `-h/--help` — keep their
  documented behavior, defaults, and short forms. The
  language-percentage string encoding, including the legacy
  `"NaN"` for an attribute-forced language on an empty file, is part
  of the contract.
- **`analyze` subcommands and flags.** The names, applicable flags,
  and documented output shapes of `analyze languages`,
  `analyze ecosystems`, `analyze frameworks`, `analyze discovery`,
  `analyze metrics`, `analyze projects`, `analyze declarations`,
  `analyze environments`, `analyze focus`, `analyze availability`,
  `analyze explain`, `analyze graph`, `analyze packages`,
  `analyze rules`, `analyze registries`, `analyze formats`,
  `analyze structure`, and `analyze all` (with its module toggles) are
  frozen. Enum values (`--source auto|git|directory`,
  `--on-error fail|continue`, `--metrics-scope source|text`) are
  frozen; adding a new value counts as an additive change.
- **`compare` and `plan`.** The saved-report readers accept the JSON
  their producers emit, with the documented `--module` names and
  positional argument shapes.
- **`capabilities` outputs.** `capabilities --json` (planner
  descriptor), `capabilities --cli --json` (CLI catalog),
  `capabilities --guide[ --json]` (offline guide), and
  `capabilities --schema NAME` (Draft 2020-12 schema export) keep
  their documented `kind`, top-level fields, and schema identifier
  scheme (`https://dircue.invalid/schema/…`). The set of valid schema
  names is additive-only.
- **JSON schema families.** The bundled schemas under `schema/` — the
  aggregate `profile`, the standalone `languages`, `availability`,
  `capabilities`, `cli-capabilities`, `comparison`, `declarations`,
  `environments`, `explanation`, `findings`, `focus`, `formats`,
  `guide`, `hotspots`, and `planning` documents — are the reference
  for what a dircue JSON output contains. Fields present in a 1.x
  schema stay present, with the same types and semantics. The
  reserved `$defs["_dircue_bundled_resources"]` key is part of the
  export contract; do not add a definition with that name to a source
  schema.
- **`schema_version`.** Every schema carries its own
  `schema_version`. A minor bump signals additive fields only; a
  major bump would signal removals or renames and is not planned
  within 1.x. The aggregate profile schema currently tops out at
  `1.7.0`; adding a new module in a 1.x release bumps that minor
  version. Standalone schemas keep their own numbers.
- **Exit statuses.** `0` on success, `1` for handled errors. The
  `check exit before consuming stdout` rule from the README is part
  of the contract. Note the SIGPIPE caveat below.
- **stdout/stderr discipline.** Machine-readable output goes to
  stdout only; diagnostics, warnings, and progress go to stderr. A
  successful `--json` invocation writes no bytes to stderr other than
  warnings the report also lists.
- **Boundary guarantees.** Dircue does not execute inspected content,
  does not open the network, and does not read outside the selected
  source. Documented resource bounds (tree-size, packfile descriptor
  cap, saved-report size caps, structural worker deadline, attribute
  rule budget) are also part of the contract.

### SIGPIPE caveat

When stdout is closed by a downstream reader (`dircue --json .
| head`), dircue may exit with the standard Unix SIGPIPE status
(`141`) rather than `0`. Consumers that must treat that case as
success should read all of stdout before closing. This matches Go's
default runtime behavior and is not classified as an error.

## What is not frozen

- **stderr text.** The exact wording of warnings, spelling
  suggestions, and diagnostic messages is not part of the contract.
  Warning **codes** (for example `tree_size_limit`,
  `file_read_error`, `non_regular_file`, `invalid-global-json`)
  are stable. New codes may be added; codes are not silently removed
  within 1.x.
- **Warning presence in JSON.** Warnings in a report's `warnings[]`
  array are diagnostic evidence. Their code set is stable in the
  additive sense above; consumers should treat unknown codes as
  informational and not fail on them.
- **Help text and long descriptions.** The commands and their flags
  are contract; the copy that explains them is not.
- **JSON object-key order.** As stated in the README, key order is
  not part of the contract. Ordering of arrays is documented
  per-module (the language and metrics arrays are stably ordered,
  file breakdowns follow Linguist's rules, and so on).
- **Performance.** Measured elapsed time, allocation, and peak RSS
  are not part of the contract. We record the workload and the
  result when we claim a speedup; we do not commit to preserving it
  across releases.
- **The structural worker protocol.** The wire format between the Go
  CLI and the native structural worker is not a public integration
  surface. Third-party workers are unsupported.
- **Internal Go packages.** `internal/` is not importable from
  outside the module and its APIs are not part of the contract.
  `pkg/` packages remain importable, but embedding callers must
  accept that we may refine their signatures within 1.x if the
  refinement preserves the CLI contract above.
- **Text output for non-`--json` invocations of new subcommands.**
  Legacy `dircue` (no subcommand) text output is frozen; text
  rendering of `analyze all`, `plan`, `compare`, and `capabilities`
  is stable in shape but not byte-identical.

## Additive changes and `schema_version`

New optional modules, new warning codes, new enum values, and new
fields inside existing objects are additive. When they touch a
bundled schema, the affected schema's `schema_version` gets a minor
bump and the CHANGELOG names the surface. Consumers must ignore
unknown fields on read; the schema does not use
`additionalProperties: false` in every subschema, and the
[schema readme](../schema/README.md) explains where that would fight
future additions.

Removing a field, tightening an enum, or narrowing accepted input
requires a major bump and is not planned within 1.x. When the design
requires such a change, we would ship it in a 2.0 with a migration
note.

## Deprecation policy

- A deprecation is announced in a CHANGELOG entry that names the
  surface, the replacement, and the earliest release the surface
  might be removed in.
- The deprecated surface keeps working for at least two subsequent
  minor releases after the announcement. A shorter window requires a
  security or correctness justification, called out in the CHANGELOG.
- `dircue` prints a deprecation notice to stderr on invocations that
  touch the deprecated surface. `--json` output is never mutated to
  carry a deprecation message.
- Removal happens no earlier than the next major release.

## What a 2.0 would mean

A major bump is reserved for changes we cannot make additively:
renaming or removing a documented CLI flag, tightening an existing
JSON field's type or values, dropping support for a saved-report
schema version, changing the exit-code semantics for a documented
invocation, or removing the legacy Linguist directory JSON output.
The release notes would list every such change and provide a
migration recipe. We do not plan a 2.0 for the 1.x line.

## Supported platforms and toolchains

Release archives, wheels, and the Docker image ship for:

- Linux amd64 (glibc-compatible, and musl through the packaged
  `musllinux` wheel tag).
- Linux arm64 (glibc-compatible, and musl).
- macOS 12+ on Apple Silicon and Intel.
- Windows 10 / Server 2016 and newer, amd64.

Building from source requires Go pinned to the version in `go.mod`
(currently 1.26.6). The release workflow reproduces that exact
compiler version; local builds should as well. The Python wheel
launcher needs Python 3.10 or newer. The optional structural worker
is packaged separately and has its own build toolchain
(Rust 1.94.0, pinned in the [worker guide](STRUCTURE.md)).

See the [distribution guide](DISTRIBUTION.md) for the packaging
matrix and the [release automation guide](RELEASE_AUTOMATION.md) for
which of these platforms the release workflow builds and validates.

## Known boundaries at 1.0

These are documented deferrals, not defects. Runtime output for the
listed cases still respects every contract above.

- Bare, unborn, or SHA-256 Git repositories, Git alternates, and
  `GIT_DIR` overrides; subdirectory discovery inside a repository
  root. `--source git` fails with a specific error for the
  unsupported forms; `--source auto` may fall back to directory mode
  without a stderr warning today (see the README under "Content
  selection"). Tracked in
  [#66](https://github.com/war-and-code/dircue/issues/66).
- `analyze focus --affected-by <path>` for a path with no matching
  declaration returns `status: complete` and echoes the query rather
  than emitting a dedicated "no match" sentinel. Tracked in
  [#67](https://github.com/war-and-code/dircue/issues/67).
- Registry adapter coverage: `analyze registries` reads NuGet.Config
  and .npmrc; Maven `settings.xml`, `pip.conf`, Cargo `config.toml`,
  Yarn configuration, and other package sources are outside the
  current supported scope. Tracked in
  [#68](https://github.com/war-and-code/dircue/issues/68).
- Environment adapter breadth: `analyze environments` covers the
  dimensions listed in [ENVIRONMENTS.md](ENVIRONMENTS.md); other
  ecosystems' environment declarations are not modeled yet. Tracked
  in [#65](https://github.com/war-and-code/dircue/issues/65).
- Structural worker isolation: the worker inherits the caller's
  process environment and working directory and is not sandboxed;
  the caller is responsible for the worker binary's origin. Tracked
  in [#69](https://github.com/war-and-code/dircue/issues/69).
