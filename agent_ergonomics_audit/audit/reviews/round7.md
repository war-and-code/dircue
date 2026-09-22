# Round 7 independent CLI/schema review

## Result

One concrete machine-contract defect was reproduced and corrected in the bounded review scope. No other actionable defect was reproduced in the inspected CLI, offline-schema, documentation, or focused test changes.

The reviewed source freeze was `932fb3e2f79a1001dac3e62b20ffcdfe5261b648`. The bounded correction was subsequently integrated as `5ef618facf6227c41daa243c9e262e42b2060711`. Runtime probes used the frozen `1.0.0-dev` binary `.cache/v100/frozen/dircue-review`, SHA-256 `109cbe8ebc3b48b619e032f96c98ce8454b7b815cd2c78140399ae20fed04322`. The comparison binary reported `0.8.0` and had SHA-256 `e567a97ff9a552078a52c405280a3372f312540274c8b31060e60ca895eb6944`.

## Finding and correction

### Medium: the new CLI catalog falsely described `dircue help --json` as JSON output

`capabilities --cli --json` included the framework-generated `dircue help` command and cataloged all inherited analysis flags. In particular, `--json` had the unqualified description `Emit JSON`, while the help command had no restriction explaining its output behavior.

The public binary reproduced the mismatch:

- `dircue help --json plan` exited `0`, emitted plain-text help on stdout, and emitted no stderr.
- `dircue help --source git analyze discovery` exited `0` with ordinary plain-text help; the source selector was ignored.
- `dircue help --workers 5 capabilities` likewise exited `0` with plain-text help; the worker setting was ignored.

An agent consuming the new catalog could reasonably select `--json`, accept the successful exit, and then fail while parsing stdout. The catalog was therefore describing accepted parser grammar without disclosing the command's effective behavior.

The bounded correction preserves the existing framework help parser, output, and exit semantics. In `internal/cli/capability_cli.go`, inherited help flags now say that help ignores them and retains them only for parser compatibility. The `--json` description explicitly states that help always emits plain text and points to `dircue capabilities --guide --json` and `dircue capabilities --cli --json`. A help-specific restriction repeats that contract. Flag names, defaults, and parsed types remain derived from Cobra.

`internal/cli/capability_views_test.go` now executes `help --json plan`, confirms successful plain-text output, and checks that the machine-readable catalog discloses the ignored inherited flags, structured alternatives, and the actual boolean type of `--json`.

Focused verification passed:

```text
go test ./internal/cli -run 'TestCLIContract(DisclosesIgnoredHelpFlagsAndPlainTextOutput|DerivesActualCommandsFlagsAndDefaultValues)$' -count=1
ok   dircue/internal/cli  0.031s
```

`git diff --check` was also clean for the two edited files.

## Other bounded checks

- Inspected the implementation diff from `121027f` in `internal/cli`, `schema`, README/documentation, and directly relevant tests.
- Confirmed the frozen binary's root, capabilities, plan, and compare help surfaces; plan/compare help hides rejected inherited scan options and still advertises the applicable `--json` workflow.
- Probed typo and selection errors (`--jsno`, `--jason`, misspelled analysis and schema names, conflicting capability views, missing plan/compare operands). The observed failures exited `1`, left stdout empty, and produced bounded stderr with a useful correction or valid next command.
- Exported all 15 advertised schemas twice. Each pair was byte-identical, parsed as Draft 2020-12 JSON, and carried an absolute `https://dircue.invalid/schema/` resource identity. The CLI-catalog and guide JSON views also parsed with their expected `1.0.0` contract kinds.
- Reviewed the compound-schema closure implementation, schema scope labeling, directory-language percentage constraints, focused-metrics correction, and the producer/schema/saved-report regression tests. No additional mismatch was reproduced.

## Limits

This was a bounded CLI/schema review. It excluded the Git scanner/cache work, release packaging, performance claims, and broader architecture. I did not build a new public binary or run the full race, vet, compatibility, or repository test suites; those remain owned by the root integration pass. Runtime probes therefore exercised the stated frozen pre-correction binary, while the catalog correction itself was verified through the focused Go tests above. This report does not claim whole-tool correctness, complete schema proof, cross-platform behavior, or performance validation.
