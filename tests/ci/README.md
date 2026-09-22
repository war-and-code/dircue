# CI concurrency contract tests

Workflows run on repository events or manual dispatch. Do not add scheduled
Actions without the maintainer's explicit authorization; making the repository
public does not lift this restriction. Draft pull requests retain the lightweight
preflight path to limit Actions usage. `test_workflow_policy.py` checks the
allowed triggers and verifies that external Actions use full commit hashes.

`test_workflow_concurrency.py` reads the concurrency expressions and job gates
directly from the three pull-request workflows. It evaluates representative
event snapshots and models GitHub's one-running/one-pending admission rule for
each concurrency group. The pending-run regression is kept separate from the
running-run case because a newer pending run can replace an older pending run
even when running-run cancellation is disabled.

These deterministic tests do not replace a controlled GitHub Actions transition
check. Before relying on the change for release validation, verify on a test pull
request that a full run remains running or pending when an ordinary draft-valued
event is admitted later with the same head commit.

The lane expression classifies immutable event snapshots. A delayed
`converted_to_draft` event can therefore still supersede a later promotion. Any
fix for stale state-transition delivery needs a current-state check and targeted
cancellation; this concurrency key does not establish event ordering.
