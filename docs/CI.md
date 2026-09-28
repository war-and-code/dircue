# CI and release preparation

Develop locally and keep the pull request in draft while it is changing. Mark it ready for review after the local checks pass and the proposed release is ready to assess. GitHub then runs the complete platform and conformance checks.

## When jobs run

| Event | Checks |
| --- | --- |
| Feature-branch push without a PR | None |
| Draft PR opened, updated, or reopened | Python example and packaging checks on `ubuntu-slim` |
| PR marked ready for review | Full Go, conformance, native-worker, and prototype suites |
| New commit on a ready PR | Full suites on the new commit |
| PR converted back to draft | Cancel older full-validation runs for that PR; lightweight checks only |
| Push to `main` | Lightweight checks, Linux Go and native-worker suites, and both conformance suites |
| Explicit workflow dispatch | Full checks for that workflow on the selected ref |

Each workflow has separate draft and full concurrency lanes per PR or ref, and cancels superseded runs within each lane. Ready PRs use the full suite even for documentation-only updates: an earlier successful commit is not evidence for a later one. Returning to draft before another editing cycle keeps that cycle inexpensive. Workflows use `pull_request`, not privileged `pull_request_target` execution.

All workflows remain event-driven or manually dispatched. Scheduled Actions require a separate maintainer decision, including after the repository becomes public. The [CI policy test](../tests/ci/test_workflow_policy.py) rejects schedule triggers and external Actions without full commit-hash pins.

The job conditions are evaluated before a matrix is expanded. They use event and draft state; they do not reference `matrix.os` at job level. The core test matrix selects Linux alone for `main` pushes and all three operating systems for ready PRs and manual runs. Structural validation uses Linux amd64 alone on main and all five native platforms for ready pull requests and manual runs.

The Linux core job installs `govulncheck` v1.8.0 and rejects reachable known Go vulnerabilities. The Linux amd64 structural-worker job installs `cargo-audit` v0.22.2 and rejects RustSec vulnerabilities and warnings in the locked worker dependency graph. These checks query current advisory databases, so a new real finding fails CI and requires dependency remediation or an explicit reviewed policy change.

Skipped draft checks are not a release certification. Before merging, inspect the full results for the current PR commit. Manual workflow runs are useful for diagnostics but do not substitute for required PR checks. These workflow changes do not configure repository branch protection or organization rulesets.

## Local checks

```sh
make check
python3 -m unittest discover -s tests/release -p 'test_*.py'
make build
python3 examples/staged_analysis/test_route.py --candidate bin/dircue
```

For language rules or selection changes, also run `make conformance` and `make samples` with Docker available. For structural changes, run the pinned Rust tests and the native-worker integration suite described in [STRUCTURE.md](STRUCTURE.md). Cross-compilation verifies that a target builds; it does not replace execution on Windows, macOS, or Linux.

## Action versions

Actions are pinned to full commit SHAs, with their release versions in comments. When updating them, resolve the latest stable release tag to its commit, review the release notes, and validate the full ready-PR suite. A major-version tag alone is mutable and is not a pin.

## Runners, caching, and limits

`ubuntu-slim` runs only the short Python tests. It has one CPU, 5 GB RAM, a 15-minute maximum job duration, and an unprivileged container environment. It cannot run our Docker-based Linguist comparison. The lightweight job has a five-minute timeout. See [GitHub's runner reference](https://docs.github.com/en/actions/reference/runners/github-hosted-runners).

Go/race tests and native grammar compilation retain normal runners. A timeout limits a stuck job; reducing it does not reduce charges for an ordinary job that finishes earlier. The worker and prototype budgets leave room for cold caches and slower Windows builds rather than assuming timings scale to a single CPU.

Go caches include the root and maintained dependency module files. Rust caches separate operating systems, architectures, targets, pinned toolchains, and Cargo inputs. They retain registry archives needed by the worker's license/source packaging checks. Cargo still checks the lockfile and rebuilds changed code; restoring a cache does not replace tests. CI artifacts expire after seven days. Download candidate artifacts that need longer retention before they expire. Release archives and their provenance remain separate from disposable CI caches.

## Cost comparison

Standard hosted runners are free for public repositories. Once this repository is public, Linux, Windows, and macOS execution on these standard runners will not consume the private-repository minutes allowance. Larger runners remain chargeable. The trigger and cancellation changes still avoid redundant work and reduce waiting; runner substitution is not the main optimization. See [GitHub's billing documentation](https://docs.github.com/en/billing/concepts/product-billing/github-actions).

The successful 2026-09-17 run of commit `062010f` executed 12 PR jobs and 10 additional branch-push jobs. Using rounded elapsed job minutes and GitHub's published standard-runner rates gives an illustrative list-rate estimate of $1.024 for the PR jobs and $1.237 for the duplicate push jobs, or $2.261 total. Keeping the PR checks removes about 55% of that estimate before cache savings; a lightweight check adds a small amount. This is one observed run, not an invoice, a promise about future durations, or an estimate of included-minute deductions.

Published per-minute rates checked on 2026-09-17:

| Runner | USD/minute |
| --- | ---: |
| Linux slim x64 | 0.002 |
| Standard Linux x64 | 0.006 |
| Standard Linux arm64 | 0.005 |
| Standard Windows x64 | 0.010 |
| Standard macOS | 0.062 |

[GitHub's pricing reference](https://docs.github.com/en/billing/reference/actions-runner-pricing) rounds partial job minutes up. Account allowances, enterprise arrangements, and artifact/cache storage affect actual billing. No budget or billing setting is changed by these workflows.

Cache hit rates and compressed cache sizes have not yet been established for these workflows. Build caches can compete for the repository's storage allowance; their presence is not a guarantee of lower total cost.

## Release automation

The core Go binary already supports cross-compilation with cgo disabled. The separate Rust worker requires its native platform build and tests. Our existing packaging scripts produce archives, wheels, notices, source bundles, provenance, and checksums; they are the starting point for hosted release preparation.

The manually dispatched **Prepare draft release** workflow selects an exact `main` commit and matching existing tag, builds and validates all five native platforms, then attaches assets to a draft GitHub Release. Publication stays a separate maintainer action. See [release automation](RELEASE_AUTOMATION.md) for inputs, receipts and review steps. Local packaging scripts remain supported.

From 0.7.0, both the native PR checks and release preparation exercise focus, availability and fresh/retained explanations through the packaged core. Release assembly requires matching per-platform targeted smoke receipts. PyPI publication remains separate, tracked in [#14](https://github.com/war-and-code/dircue/issues/14).

## Draft and full concurrency lanes

Ordinary draft pull-request events use a separate concurrency group from full validation. A delayed draft update therefore cannot cancel or replace a full run merely because both concern the same pull request. `converted_to_draft` remains in the full lane to cancel older full validation; its job gates still avoid expensive work.

Event snapshots do not establish delivery order. A stale conversion event can still arrive after a promotion, so release review must check successful full validation for the actual candidate commit. The [concurrency tests](../tests/ci/README.md) cover both running and pending admission cases.
