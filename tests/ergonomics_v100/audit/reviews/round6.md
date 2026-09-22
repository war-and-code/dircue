# Round 6 independent CLI and schema review

## Verdict

One actionable schema-contract defect was found and fixed during this review. I found no remaining actionable defect in the reviewed CLI and schema preparation after that correction.

The final two-file schema correction is frozen for the root reviewer. Full Go tests, race, vet, and compatibility runs are still pending there; I did not run a build or heavy suite.

## Fixed finding

### R6-01 — exported legacy-language schema accepted impossible percentages

**Severity:** P2

**Status:** fixed in the working tree; root verification pending

The newly exported `languages` schema originally allowed any nonnegative decimal string and `NaN` independently of `size`. It therefore accepted outputs the producer cannot emit, including:

- `{"size":1,"percentage":"NaN"}`
- `{"size":1,"percentage":"100.01"}`
- `{"size":1,"percentage":"999.99"}`

The producer emits `NaN` only when the total attributed byte population is zero, and ordinary percentages are formatted within `0.00` through `100.00`.

The correction in `schema/languages.schema.json` now:

- requires `size == 0` when `percentage == "NaN"`; and
- accepts ordinary percentage strings only in the bounded `0.00`–`100.00` range.

`schema/export_test.go` now preserves the real zero-byte `NaN` case, rejects positive-size `NaN`, accepts `0.00`, `99.99`, and `100.00`, and rejects `100.01` and `999.99`. This intentionally does not attempt cross-language sum validation, which is not expressible as a local row constraint in this schema.

Static verification completed: the schema parses with `jq`, and `git diff --check` is clean. The root reviewer is running the rebound tests.

## Clean review results

I reviewed the CLI and schema changes against `121027f`, excluding the separately reviewed scanner and vendored changes. The final CLI runtime freeze was `2bcd134`; subsequent source commits did not alter the reviewed CLI. The final schema source includes the later partial-focus and language-contract corrections plus the frozen two-file working-tree delta described above.

### CLI catalog and help contracts

- `capabilities --cli --json` returned 24 sorted commands and all 15 bundled schema resources.
- Command flags, types, defaults, inheritance, examples, output contracts, and rejected inherited flags derive from the completed Cobra tree.
- Finite choices are command-scoped for `source`, `on-error`, `metrics-scope`, schema names, planner modules, questions, and caller-supplied prerequisite names.
- Saved-report help for `plan`, `compare`, and `capabilities` shows applicable local flags and `--json`, while hiding inherited scan flags. Those inherited flags remain parse-compatible and are rejected with an actionable message when the command reaches execution.
- The default planner-capabilities JSON contract remains byte-compatible by construction and test.
- Bare missing paths that resemble profilers or commands receive a precise hint. Explicit `./name` and `-- name` paths do not receive command guesses, and an existing directory named `languages` remains analyzable as a legacy path.
- Legacy directory-language JSON and aggregate-profile JSON remain distinct and are described as such. The catalog does not falsely map the legacy single-file response to the directory schema.

### Diagnostics, privacy, and error identity

- Unknown flag values are not reflected in diagnostics. Long or control-bearing names are bounded and terminal-escaped.
- Final `Execute` errors escape C0, C1, and Unicode format controls before `main` prints them.
- The escaping wrapper retains the original error in its unwrap chain. Ordinary safe errors retain byte content and identity.
- Saved-report open failures in plan, compare, and explain now retain their underlying cause behind a bounded public diagnostic.
- The runtime terminal-control probe emitted printable ASCII escape sequences only; a private value supplied through an unknown flag was not echoed.

### Schema export and saved-focus strictness

- Every export has a deterministic absolute identity under `https://dircue.invalid/schema/`.
- The exported profile compound document contained the profile identity plus the seven transitive component identities: availability, declarations, environments, explanation, focus, formats, and hotspots.
- Relative references remain relative to each embedded resource's own `$id`; local fragment scopes are preserved.
- Export names are an exact allowlist, so caller input is never interpreted as a path or URL. Export is in-memory and offline.
- Partial focused metrics can round-trip through strict saved-report loading with the explicit `focus_scope_partial` marker.
- The marker is prohibited from aggregate metrics, cannot carry an invented file count, requires partial metric status, and requires partial focus status when present in primary or related focused metrics.

### Planning, writers, and execution boundaries

- Runtime planning from a saved discovery report produced structured argv with `executable:false`, explicit placeholders, and source identity/boundary/freshness revalidation requirements.
- Plain plan output rendered the same argv as valid inert JSON and did not expose the saved report's declared root as executable input.
- Capabilities, guide, schema export, comparison, and planning propagate writer errors; schema export detects a nil-error short write.
- Capability and guide generation only traverse in-memory command declarations and embedded schemas. Planning reads the explicitly selected report and builds data; it does not scan the declared root, probe tools, or execute planned commands.

## Runtime evidence

The immutable review binary was:

- path: `.cache/v100/review-final/dircue`
- SHA-256: `1d42b6adead0750636ca7bf8b0d3cddbbe2683e570c13b41c6c80394dfeeb03d`
- source behavior freeze: `2bcd134`
- reported build version: `0.8.0` (the coordinator identified this as the deliberate comparison build; source defaults to `1.0.0-dev`)

Bounded runtime probes covered catalog generation, saved-report help, view conflicts, rejected inherited flags, command/path disambiguation, all schema export identities, profile reference closure, discovery-to-plan, self-comparison, saved explanation, and terminal-control/private-flag diagnostics.

## Limits

- I did not read prior review reports or score files.
- Scanner and vendored Git changes were excluded as directed.
- I did not run a build, full Go test suite, race detector, vet, compatibility suite, or performance suite; the root reviewer owns those checks.
- Runtime probes used the immutable `2bcd134` binary and therefore do not exercise the final source-only language-schema correction.
- Runtime coverage was on the current Unix host; Windows-specific input-opening behavior was not executed.
