# Round 9 — final CLI/schema review

Verdict: **the actionable CLI diagnostics defect and one adjacent malformed-input bypass were fixed in source and passed focused tests**.

## Finding

### [P2] `analyze explain` reflects an unbounded `--on-error` value

`internal/cli/explain.go:90` passes `opts.onError` directly into `scanner.Scan` without the fixed-vocabulary validation used by the other analysis commands at `internal/cli/cli.go:241-243`. As a result, the fresh-source `analyze explain` path reflects the entire caller-supplied value in the process diagnostic. This is inconsistent with the new bounded/non-reflective flag diagnostics and with the cataloged `fail|continue` vocabulary.

Reproduced against the supplied review binary (SHA-256 `97cabacd5babea8ae9fcf4c944162c6ff7108780b77f7606a4dd157a42565324`, `dircue 1.0.0-dev`) using a temporary directory containing `main.go`:

```text
$ dircue-review analyze discovery --source directory --on-error private-value TEMP
Error: --on-error must be fail or continue

$ dircue-review analyze explain --source directory --on-error private-value --file main.go TEMP
Error: unknown error policy "private-value"
```

A 5,000-byte value produced 5,031 bytes on stderr, confirming the output is unbounded rather than merely naming a safe enum error.

Recommended correction: validate `opts.onError` in the fresh-source branch of `newExplainCommand` before constructing `scanner.Options`, using the same fixed message as `run`, and add a regression case with a private marker and a long value.

## Fix addendum

The authorized follow-up added the peer-equivalent validations in `internal/cli/explain.go:83-88` before fresh-source scanning:

- an explicitly empty `--tree` now returns `--tree requires a full Git tree object ID`;
- an invalid `--on-error` now returns `--on-error must be fail or continue` without reflecting its assigned value.

The adjacent empty-tree check closed the same explain-only malformed-input bypass: peer analyzers rejected explicit `--tree ""`, while fresh `analyze explain` previously allowed it to reach scanning. The checks remain inside the fresh-source branch, so saved-report mode still rejects both flags as inapplicable before opening a report.

Focused coverage in `internal/cli/error_safety_test.go:95-150` verifies peer-diagnostic equality, no stdout/stderr before the embedding caller handles errors, non-reflection of a 5,014-byte private value, explicit-empty-tree rejection, successful JSON output when tree is omitted and the default error policy is used, and saved-report rejection ordering.

Focused command:

```text
go test ./internal/cli -run 'Test(ExplainSemanticFlagErrorsMatchPeerWithoutReflectingValues|ExplainAllowsOmittedTreeWithDefaultErrorPolicy|SavedExplainStillRejectsScanFlagsBeforeOpeningReport|SafeCLIErrorDoesNotReintroducePrivateFlagValues|TargetedFlagsRejectUnappliedChoices)$' -count=1
ok  dircue/internal/cli  0.031s
```

`gofmt` and `git diff --check` also passed for the two authorized implementation/test files. No rebuilt binary was produced in this review task; root integration owns rebuild and full validation.

## Scope and limits

Reviewed `121027f..5ef618f` for `internal/cli`, exported schemas, and directly relevant README/schema docs and tests. Checked the installed command/flag catalog against help-facing registrations; inspected output-contract/schema-scope descriptions; exported all 15 named schemas and confirmed JSON/Draft-2020-12 identities plus the profile resource closure; exercised a small complete focus+metrics report; and compared five successful legacy language invocations byte-for-byte against the supplied `0.8.0` comparison binary (SHA-256 `a33099ebc799a61867f662b51018223997f07d419c8e467faf42bf4314998547`), all matching.

The initial review was read-only; the authorized follow-up implemented and tested the correction above. I did not build, run full repository test suites, execute repository source, review scanner/cache internals, or assess whole-tool correctness or performance. The available host lacked an independent Draft 2020-12 validator, so offline schema behavior beyond structural closure inspection relies on source/test review rather than a second validator implementation.
