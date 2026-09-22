# Baseline scorer A method

Independent GPT-6 Astra assessment against rubric 1.0.0 and surface-class anchors.
No scorer B files were read. Scores are explicit judgments grouped by shared
implementation contract; differences for planner errors, group/help commands,
schema completeness, and mature aggregate output are recorded per surface.
Existing tests receive credit; the first pass does not pretend tests are absent.
Schema is an explicit output-contract kind adaptation. N/A dimensions follow
the rubric's 1000-with-reason rule, so overall scores should not be read as a
probability or compared across unrelated surface classes.

Evidence includes all 24 actual help outputs, source references bound to released
commit 121027f, 29 invalid invocations, repeated successful legacy/aggregate/
discovery/capabilities/plan/comparison probes, and four non-TTY environment runs.
Windows environment/signal behavior is source-reviewed, not runtime-tested here.
Host root strings are normalized only after equality comparison; no corpus
content from private external repositories is recorded.
