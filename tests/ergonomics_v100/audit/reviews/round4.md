# Round 4 independent CLI and schema review

Reviewed the 1.0 preparation source at `84e96f8223903120d58b427f2bd47636e48e3431` against baseline `121027f`, before reading any earlier review report. The scanner changes were excluded as requested. The review traced Cobra declarations into command handlers, output writers, saved-report readers, bundled schema export, and the user-facing offline help/catalog claims.

## Findings and fixes

### 1. The CLI catalog omitted finite values enforced by the same binary

`capabilities --cli --json` advertised an `allowed_values` field for every flag, but the implementation populated it only for `--source`, `--on-error`, and `--metrics-scope`. It therefore emitted an empty list for the finite `capabilities --schema`, `plan --module`, `plan --question`, and `plan --input` domains even though their handlers reject values against `schema.Names()` and `capabilities.Dircue(Version)`. An automation client consuming the advertised actual grammar could not construct these invocations from the catalog alone, and an empty allowed-value list was indistinguishable from an unconstrained flag.

Fixed in `internal/cli/capability_cli.go`: finite domains are now derived per command from the runtime schema registry and planner capability registry. Planner input values are the sorted union of caller-supplied prerequisites, excluding the separately modeled `source` and `project` placeholders; the restriction states that an input is accepted only when required by a selected module. Existing scan enums are also scoped to their actual commands so a future unrelated flag with the same name will not inherit the wrong domain.

`internal/cli/capability_domains_test.go` pins the exported schema, module, question, input, and metrics-scope domains to the registries that enforce them.

### 2. Saved-report open diagnostics discarded machine-classifiable causes

The new parser diagnostic wrapper preserves its underlying `pflag` cause while exposing bounded text, and planner selection errors preserve their sentinels. In contrast, saved-report open failures in `plan`, `compare`, and saved `analyze explain` replaced `openInputFile` errors with fresh text errors. Embedding callers could not use `errors.Is(err, fs.ErrNotExist)` (or classify another underlying filesystem failure), even though the public message intentionally withheld the caller's potentially private pathname.

Fixed in `internal/cli/planning.go`, `internal/cli/compare.go`, and `internal/cli/explain.go`: each path now returns `diagnosticError` with the existing static public message and the original cause. The process-facing text remains unchanged and does not expose the pathname or the underlying error string.

`internal/cli/saved_report_errors_test.go` covers all three commands, cause recovery, empty stdout/stderr from the embedding API, and non-disclosure of a private pathname.

## Reviewed behavior without a finding

- The CLI inventory is built from the completed Cobra tree, uses declared defaults rather than invocation-mutated values, separates accepted flags from explicitly rejected inherited scan flags, and sorts commands and flags deterministically.
- Capability view selection rejects false boolean selectors and conflicts by flag presence; schema output remains JSON regardless of `--json`.
- Ordinary legacy language JSON retains `{}` with exit zero for a successful empty population and for the documented tree-limit compatibility case. The catalog and guide distinguish this from aggregate reports whose successful output can have partial module coverage.
- The new plain-text guide, catalog, plan, comparison, and schema writers propagate writer failures. Direct schema export also detects a short write with no error.
- Schema export uses an exact allowlist, assigns absolute resource identities, follows the transitive relative-reference graph, retains each embedded resource's local `$defs` scope, returns caller-owned deterministic bytes, and is tested with external loading disabled. The directory language schema accepts the existing string `"NaN"` without accepting primitive numeric/null variants.
- Semantic selector errors intentionally name nonsecret schema and analysis identifiers for actionable correction. Parser-assigned values and underlying filesystem details remain the privacy boundary; this review does not claim that every user-supplied identifier is universally redacted.

## Verification and limits

Focused verification passed:

```text
go test ./internal/cli -run 'TestCLIContractPublishesFiniteCommandScopedFlagValues|TestSavedReportOpenErrorsRetainCauseWithoutLeakingPath' -count=1
ok  dircue/internal/cli  0.029s
```

`gofmt` and `git diff --check` were clean for the changed files. The root agent owns the full race suite, vet, compatibility matrix, and rebuilt-binary probes. This round did not review scanner internals, did not inspect prior review reports before forming findings, and did not evaluate the separately owned final process-error escaping change that had not yet landed when the source review began.
