<!--
Please open an issue first if you have not already. See CONTRIBUTING.md
for the review expectations this project uses.
-->

## Summary

<!-- What does this change do, and why? Reference the issue that discussed it. -->

## Design principles

<!--
Which design principles does this touch? (docs/DESIGN_PRINCIPLES.md)
Note any tension you noticed and how you resolved it.
-->

## Contract impact

- [ ] No successful CLI output or exit status changes for existing invocations.
- [ ] If schemas change, `schema_version` is bumped per docs/COMPATIBILITY.md, and the compatibility harness records the diff.
- [ ] Diagnostic text changes are limited to stderr and remain non-normative.

## Tests

<!--
Which tests would have failed before this change and now pass?
List the exact commands you ran (go, python, or otherwise).
-->

- [ ] `go test -race ./...`
- [ ] `go vet ./...`
- [ ] `python3 -m pytest tests/release -q` (if release tooling or docs changed)
- [ ] Additional targeted tests: <!-- e.g. tests/compatibility_v100/run.py, tests/scanner/ -->

## Evidence

<!--
For performance, coverage, or comparison claims: link the receipt
(paired samples, environment, harness commit). "It felt faster" is
not evidence.
-->

## Follow-up

<!-- Anything intentionally left out, or issues to open after this merges. -->
