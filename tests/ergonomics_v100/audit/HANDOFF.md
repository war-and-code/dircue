# CLI ergonomics handoff: 1.0.0 preparation

This pass prepares a draft PR for external adversarial review. It does not
authorize a release. The validated runtime is commit `54f82a5`; subsequent
commits retain evidence and documentation.

## Delivered surfaces

The installed binary now provides an offline guide, a catalog derived from its
Cobra registrations, and standalone JSON Schema export through explicit
`capabilities` selectors. The guide includes ten workflow/resource/recovery
sections and references for all 18 analyzers. The catalog describes 24 commands;
schema export supports 15 resources, including the guide and catalog themselves.
The original planner capability output and ordinary analysis paths retain their
contracts.

Help and errors teach the applicable invocation. Spelling suggestions are
bounded and never executed. Saved-report help omits ineffective scan flags,
and plain plans show inert argv, prerequisites and cost qualifications. Schema
corrections accept existing empty-file language percentages and partial focused
metrics reports. Every recommendation has a public-binary regression test.

## Evidence

- `agent_surfaces_pre_pass1.jsonl`: 108 baseline surfaces, two independent
  assessments and a blinded tiebreak where required.
- `agent_surfaces.jsonl`: 114 post-pass surfaces, two independent assessments
  and eight blinded dimension tiebreaks. All contributing judgments and evidence
  are embedded in each aggregated row; ignored partial files are scratch, not
  the only surviving assessments.
- `scorecard_pass_1.md`, `heatmap.svg`, `uplift_diff.md`,
  `regression_alerts.md`: full results, matched-cohort comparison and every
  dimension decrease requiring investigation.
- `evidence/recommendation_regressions.jsonl`: real baseline failures and
  candidate passes, with executable/test hashes and raw output.
- `evidence/post_intent_probes.jsonl`: 29 retained invalid/ambiguous invocation
  probes replayed with unchanged exit statuses.
- `reviews/`: findings, fixes and independent clean review scopes.
- `agent_simulations/`: pre/post binary-only task transcripts.
- `../../tests/compatibility_v100/`: source-bound integrated builds and the
  319-case compatibility run, separate from rubric judgments.

The matched 108-surface median moves from 618 to 818; the median paired
difference is 195. Six additions do not inflate that comparison. There is no
overall decrease greater than 50. These are subjective scores, not a percentage
improvement in productivity or a probability of correct use. Pre/post cohorts
changed from Astra to Sol at the user's request. Some applicability judgments
also changed: a formerly static schema gained an executable export interface.
The 22 dimension decreases and their investigation remain visible.

Scores identify CLI source `3ef3961`; the later saved-report schema correction,
terminal-error hardening, finite-choice catalog refinements and language-schema
constraints, plus the framework-help catalog disclosure, are separately tested.
Later source/help captures
and build receipts identify the exact integrated candidate. No scores were
raised retroactively to credit those fixes.

## Method and remaining scope

The audit workspace is in the target repository. The user's draft-PR request
superseded the skill's initial no-new-branch instruction; unrelated checkout
changes were untouched. Shared CLI primitives were committed together, with
recommendation-level test mappings, rather than split into artificial dependent
commits. Performance and independent correctness fixes have separate commits.
The ambition self-prompt and final prioritization round are retained in
`ambition_bar_check.md`.

Homebrew installation supplied the requested `flock` and GNU `timeout` helpers;
the original skill preflight passed. Native tests/vet and independent review
cover the optional UBS slot because UBS is not installed. The heatmap helper's
non-executable script was run explicitly through Bash, without changing the
installed skill. The mandatory pass/scorecard validators are run against the
finished artifacts.

The local reviews use independent agents from the same vendor model family;
they are not cross-family corroboration. External adversarial review is the
next gate. Ready-PR multi-platform CI, conformance and installed release-artifact
checks remain release gates.

Remaining ergonomics/validation work belongs in
[#40](https://github.com/war-and-code/dircue/issues/40): legacy single-file JSON
still lacks a bundled schema; direct process-signal and Windows taskkill
lookup/fallback tests remain limited. Text help remains text, with explicit
structured alternatives. Runtime environment parsing is not silently replaced
by CLI policy. Broader source-selection support remains
[#66](https://github.com/war-and-code/dircue/issues/66).

No automatic correction, implicit expensive module, new telemetry, network
lookup, or multi-root interpretation was added. Existing language statistics,
source populations and 0/1 exit semantics remain compatibility requirements.

## Final verification

The final source passes `go test -race ./...`, `go vet ./...`, all 319 release
comparisons, all eleven baseline-failing/candidate-passing CLI checks, and the
29 retained intent probes. Point-in-time pending-verification notes in earlier
review reports are resolved by the final receipts in
`../../tests/compatibility_v100/results/local-validation.json`.

The final development executable is `.cache/v100/handoff/dircue-review`, version
`1.0.0-dev`, SHA-256
`7c995ab0b2e25aac891e902f33c8143de49f5cccdca50855be51269499fa28ed`.
It is a local review build, not a packaged release artifact.

The fresh binary-only exercise completed all eight tasks in 22 CLI calls, with
six first intended operations succeeding and a median of two calls per task.
One attempt used unsupported pipe-backed comparison inputs; regular saved
reports resolved it. Unlike the baseline exercise, this run performed that
saved-report comparison, so total call counts are not a controlled productivity
comparison. It retained an immutable `2bcd134` binary; final schema/catalog
follow-ups have separate regression checks. Simulation transcripts cap output
at 4 KiB and disclose manual whitespace compaction; they are observational
records, not byte-exact goldens. The compatibility harness retains full raw
outputs separately.

Supplementary process checks in `explain-options.json` preserve a valid default
explanation byte for byte and record two malformed-input corrections. An
explicitly empty tree now fails instead of selecting a default source; an
invalid error policy now gets a fixed diagnostic. These are intentional fixes
outside the inherited 319-case corpus, not claimed byte equivalence.

The final independent reviews, rounds 10 and 11, found no actionable defect in
their CLI/schema scope on `54f82a5`. Earlier findings remain in the review
history with their fixes and validation. This meets the two-clean-review audit
gate; it does not replace the requested external model-family review or
release CI. Both scorecard validators and the completed-pass validator pass.
