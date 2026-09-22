# Round 5 independent CLI and schema review

Static review started from source commit `2bcd134dba4770b3792acc73baf6c2da1297de1e`. Bounded runtime probes used the supplied pre-fix comparison binary at `.cache/v100/review-final/dircue`, SHA-256 `1d42b6adead0750636ca7bf8b0d3cddbbe2683e570c13b41c6c80394dfeeb03d`. The schema correction described below was then applied and reviewed in the shared working tree.

## Finding

### [P2, fixed] Restrict the legacy `"NaN"` percentage to zero-byte rows

`schema/languages.schema.json` admitted a row such as `{"Go":{"size":1,"percentage":"NaN"}}`. Its percentage pattern allowed `"NaN"` independently of `size`, while the directory JSON writer emits `"NaN"` only when the total attributed byte population is zero and the schema documentation describes it only for attributed zero-byte populations. This let downstream validators accept a state that dircue cannot correctly emit and could cause consumers to treat corrupt legacy output as valid.

The fix adds a Draft 2020-12 conditional requiring `size` to equal zero when `percentage` equals `"NaN"`. `schema/export_test.go` now verifies all three relevant cases against both the source resource and standalone exported resource: zero bytes with `"NaN"` remains valid, positive bytes with a numeric percentage remains valid, and positive bytes with `"NaN"` is rejected.

Focused verification passed:

```text
go test ./schema -run '^TestLanguagesSchemaAcceptsExistingNaNStringOnly$' -count=1
ok  dircue/schema
```

`python3 -m json.tool schema/languages.schema.json` and `git diff --check` also passed.

## Other reviewed behavior

No additional actionable defect was found in the reviewed CLI and schema scope.

- Malformed flag and selector probes failed with empty stdout and exit status 1. Unknown flag values were not echoed, false or conflicting capability selectors were rejected, noncanonical schema names did not become paths or URLs, and an invalid plan selector was rejected before attempting to open its saved-report path.
- Saved-report open failures suppressed the private path. Control-bearing diagnostics were escaped or avoided, and long caller-controlled selector and flag values are bounded by the dedicated diagnostic paths.
- The capability catalog is derived from the completed Cobra command tree for command paths, flag types, and defaults. Finite source, error-policy, metrics-scope, schema, plan-module, plan-question, and plan-input domains are command-scoped. Saved-report commands identify inherited scan flags as rejected rather than presenting them as operational inputs.
- Plain capability, guide, plan, comparison, and schema writers propagate write failures in their callable paths. A closed process stdout probe terminated nonzero through the platform's normal `SIGPIPE` behavior.
- Schema export uses an explicit allowlist, assigns absolute resource identities, computes the transitive bundled-reference closure, preserves child resource scope, and performs no filesystem or network lookup. Static tracing found no path from a caller-supplied schema selector to file or URL loading.
- Existing successful legacy directory JSON and comparison exit semantics remain unchanged by the reviewed CLI work; the schema fix narrows only an impossible validation state.

## Limits

Scanner implementation and scanner correctness were excluded. I did not run a full build or the full test suite; release orchestration owns those checks. Runtime probes used the supplied comparison binary, whose embedded version is `0.8.0`, so they did not verify the final `1.0.0-dev` provider-version string or the post-fix rebuilt schema bytes. I did not perform a Windows runtime probe. The review covered the current `internal/cli` and `schema` implementation against baseline `121027f`, callers and callees needed to trace those paths, and bounded hostile-input probes; it did not audit unrelated packages.
