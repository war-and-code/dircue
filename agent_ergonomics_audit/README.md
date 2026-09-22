# CLI ergonomics evidence

This directory records the CLI usability pass started from dircue 0.8.0 as preparation for 1.0. It contains measured invocations, evidence-backed rubric assessments, regression checks, and the implementation handoff. It is not a product roadmap.

Start with `audit/manifest.json` for status and `audit/phase0_scope_decision.md` for preserved contracts and tooling adaptations. Scores are reviewer judgments under a stated rubric; task outcomes and subprocess captures provide the observable evidence. A score alone is not a correctness guarantee.

The audit runs in the same repository and branch as its implementation. Raw local scratch and intermediate scorer files are ignored; finalized evidence and replayable tests are retained. No inspected repository code is executed by the audit fixtures.
