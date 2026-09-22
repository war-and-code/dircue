# CLI ergonomics evidence (v1.0 preparation)

This directory records the CLI usability pass started from dircue 0.8.0 as
preparation for 1.0. It contains measured invocations, evidence-backed rubric
assessments, regression checks, and the implementation handoff. It is not a
product roadmap.

Start with `audit/manifest.json` for status and
`audit/phase0_scope_decision.md` for preserved contracts and tooling
adaptations. Scores are reviewer judgments under a stated rubric; task
outcomes and subprocess captures provide the observable evidence. A score
alone is not a correctness guarantee.

The audit runs in the same repository and branch as its implementation. Raw
local scratch and intermediate scorer files are ignored; finalized evidence
and replayable tests are retained. No inspected repository code is executed
by the audit fixtures.

## Location and redaction

Originally captured under `agent_ergonomics_audit/` at the repository root;
moved to `tests/ergonomics_v100/` before the 1.0.0 release so it sits
alongside the other 1.0-preparation harnesses instead of at the repository
front door.

The transcripts and manifests originally embedded the recording host's
absolute home paths (`/Users/<user>/…`, pyenv paths, Codex-skill install
paths, and worktree layout). Those bytes have been redacted to
`<workspace>/…` in every file that no external checksum manifest binds. No
checksum in this repository binds these transcripts; the audit's own
`binary_sha256` fingerprints the dircue candidate binary, not the transcript
bytes. Regression tests replay against `<candidate binary>` at execution
time.
