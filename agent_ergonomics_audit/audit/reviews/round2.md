# Round 2 independent CLI/schema review — CLEAN

Reviewed the v1.0 preparation changes under `internal/cli`, `schema`, and the
related README/design/planning/changelog documentation against baseline
`121027f44f014ac8d0a003a451a04b080a5ffe88`. Scanner/cache optimization,
third-party code, and the separately diagnosed baseline 0.8 saved-report issue
were outside this review.

No actionable correctness, safety, compatibility, or agent-ergonomics finding
remains in the assigned scope.

## Evidence checked

- The default `capabilities --json` path still serializes the original planner
  descriptor; `--cli`, `--guide`, and `--schema` require explicit selection and
  reject conflicting or explicitly false selectors.
- Saved-report commands hide inapplicable inherited scan flags from help while
  the CLI catalog identifies them as rejected. Their diagnostics point callers
  back to report creation rather than silently accepting ineffective options.
- The CLI catalog is derived from the completed Cobra tree, sorts commands and
  flags deterministically, separates whole-output schemas from profile-component
  schemas, and now calls `InitDefaultVersionFlag` before traversal so the root
  `--version` surface is represented.
- The guide's examples are static argument arrays in JSON, its plain rendering
  preserves the same examples and safety qualifications, and it does not scan a
  source tree or execute examples.
- Schema export uses an exact-name allowlist, embeds the transitive resource
  closure with absolute resource identities, returns caller-owned deterministic
  bytes, propagates writer/short-writer failures, and does not perform filesystem
  or network lookup.
- The directory-language schema accepts only the existing JSON string `"NaN"`
  spelling added for Linguist-compatible zero-byte output; aggregate-profile
  numeric percentage contracts remain unchanged.
- Planner prevalidation preserves the planner sentinels while adding bounded,
  copyable corrections. Plain plans render inert JSON argv and escape C0, C1,
  and Unicode format controls without changing the JSON argument value.
- Flag diagnostics bound and escape reflected names, avoid reflecting supplied
  private values in parser fallbacks, and do not execute spelling guesses.

## Runtime probes and limitation

Small probes used
`/Users/gingeleski/Workspace/dircue/.cache/review080/worktree/.cache/v100/integrated-r2/dircue`.
They confirmed deterministic 24-command catalog output, applicable help for
`capabilities` and `plan`, empty stdout and exit 1 for selector conflicts and
invalid selections, actionable module/question corrections, and byte-identical
default planner-capability output relative to the frozen baseline binary.

That candidate reports provider version `0.8.0` and predates the final
`InitDefaultVersionFlag` catalog correction. The correction was therefore
reviewed in the current source at `internal/cli/capability_cli.go:74-77`; its
focused catalog/help test was reported green by the coordinating reviewer. No
build or broad test suite was run in this pass during the profiling window.
