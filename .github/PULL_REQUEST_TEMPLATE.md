<!--
Outside pull requests are not accepted at present; please use GitHub Issues.
This template is for maintainer changes. See CONTRIBUTING.md.
-->

## Summary

<!-- What does this change do, and why? Reference the issue that discussed it. -->

## Design principles

<!--
Which design principles does this touch? (docs/DESIGN_PRINCIPLES.md)
Note any tension you noticed and how you resolved it.
-->

## Contract impact

- [ ] Existing CLI and report contracts are preserved, or necessary corrections are documented.
- [ ] Schema changes and their compatibility impact are documented and tested.
- [ ] Diagnostic text changes are limited to stderr and remain non-normative.

## Tests

<!--
Which tests would have failed before this change and now pass?
List the exact commands you ran (go, python, or otherwise).
-->

- [ ] `go test -race ./...`
- [ ] `go vet ./...`
- [ ] `python3 -m unittest discover -s tests/release -p 'test_*.py'` (if release tooling or docs changed)
- [ ] Additional targeted tests: <!-- e.g. tests/compatibility_v100/run.py, tests/scanner/ -->

## Evidence

<!--
For performance, coverage, or comparison claims: link the receipt
(paired samples, environment, harness commit). "It felt faster" is
not evidence.
-->

## Follow-up

<!-- Anything intentionally left out, or issues to open after this merges. -->
