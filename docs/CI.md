# CI and release preparation

Develop locally and keep the pull request in draft while it is changing. Mark it
ready for review after the local checks pass and the proposed release is ready
to assess. GitHub then runs the complete platform and conformance checks.

## When jobs run

| Event | Checks |
| --- | --- |
| Feature-branch push without a PR | None |
| Draft PR opened, updated, or reopened | Python example and packaging checks on `ubuntu-slim` |
| PR marked ready for review | Full Go, conformance, native-worker, and prototype suites |
| New commit on a ready PR | Full suites on the new commit |
| PR converted back to draft | Cancel older runs for that PR; lightweight checks only |
| Push to `main` | Lightweight checks and the Linux Go suite |
| Explicit workflow dispatch | Full checks for that workflow on the selected ref |

There are no feature-branch push runs duplicating PR checks. Each workflow has a
concurrency group per PR or ref and cancels its superseded runs. Ready PRs use the
full suite even for documentation-only updates: an earlier successful commit is
not evidence for a later one. Returning to draft before another editing cycle
keeps that cycle inexpensive. Workflows use `pull_request`, not privileged
`pull_request_target` execution.

The job conditions are evaluated before a matrix is expanded. They use event
and draft state; they do not reference `matrix.os` at job level. The core test
matrix selects Linux alone for `main` pushes and all three operating systems for
ready PRs and manual runs. Structural validation keeps all five native platforms.

Skipped draft checks are not a release certification. Before merging, inspect
the full results for the current PR commit. Manual workflow runs are useful for
diagnostics but do not substitute for required PR checks. These workflow changes
do not configure repository branch protection or organization rulesets.

## Local checks

```sh
make check
python3 -m unittest discover -s tests/release -p 'test_*.py'
make build
python3 examples/staged_analysis/test_route.py --candidate bin/dircue
```

For language rules or selection changes, also run `make conformance` and
`make samples` with Docker available. For structural changes, run the pinned
Rust tests and the native-worker integration suite described in
[STRUCTURE.md](STRUCTURE.md). Cross-compilation verifies that a target builds;
it does not replace execution on Windows, macOS, or Linux.

## Action versions

Actions are pinned to full commit SHAs, with their release versions in comments.
When updating them, resolve the latest stable release tag to its commit, review
the release notes, and validate the full ready-PR suite. A major-version tag alone
is mutable and is not a pin.

## Runners, caching, and limits

`ubuntu-slim` runs only the short Python tests. It has one CPU, 5 GB RAM, a
15-minute maximum job duration, and an unprivileged container environment.
It cannot run our Docker-based Linguist comparison. The lightweight job has a
five-minute timeout. See [GitHub's runner reference](https://docs.github.com/en/actions/reference/runners/github-hosted-runners).

Go/race tests and native grammar compilation retain normal runners. A timeout
limits a stuck job; reducing it does not reduce charges for an ordinary job that
finishes earlier. The worker and prototype budgets leave room for cold caches
and slower Windows builds rather than assuming timings scale to a single CPU.

Go caches include the root and maintained dependency module files. Rust caches
separate operating systems, architectures, targets, pinned toolchains, and Cargo
inputs. They retain registry archives needed by the worker's license/source
packaging checks. Cargo still checks the lockfile and rebuilds changed code;
restoring a cache does not replace tests. CI artifacts expire after seven days.
Download candidate artifacts that need longer retention before they expire.
Release archives and their provenance remain separate from disposable CI caches.

## Cost comparison

Standard hosted runners are free for public repositories. Once this repository
is public, Linux, Windows, and macOS execution on these standard runners will
not consume the private-repository minutes allowance. Larger runners remain
chargeable. The trigger and cancellation changes still avoid redundant work and
reduce waiting; runner substitution is not the main optimization. See
[GitHub's billing documentation](https://docs.github.com/en/billing/concepts/product-billing/github-actions).

The successful 2026-09-17 run of commit `062010f` executed 12 PR jobs and 10
additional branch-push jobs. Using rounded elapsed job minutes and GitHub's
published standard-runner rates gives an illustrative list-rate estimate of
$1.024 for the PR jobs and $1.237 for the duplicate push jobs, or $2.261 total.
Keeping the PR checks removes about 55% of that estimate before cache savings;
a lightweight check adds a small amount. This is one observed run, not an invoice,
a promise about future durations, or an estimate of included-minute deductions.

Published per-minute rates checked on 2026-09-17:

| Runner | USD/minute |
| --- | ---: |
| Linux slim x64 | 0.002 |
| Standard Linux x64 | 0.006 |
| Standard Linux arm64 | 0.005 |
| Standard Windows x64 | 0.010 |
| Standard macOS | 0.062 |

[GitHub's pricing reference](https://docs.github.com/en/billing/reference/actions-runner-pricing)
rounds partial job minutes up. Account allowances, enterprise arrangements, and
artifact/cache storage affect actual billing. No budget or billing setting is
changed by these workflows.

Cache hit rates and compressed cache sizes have not yet been established for
these workflows. Build caches can compete for the repository's storage allowance;
their presence is not a guarantee of lower total cost.

## Release automation

The core Go binary already supports cross-compilation with cgo disabled. The
separate Rust worker requires its native platform build and tests. Our existing
packaging scripts produce archives, wheels, notices, source bundles, provenance,
and checksums; they are the starting point for hosted release preparation.

[Issue #13](https://github.com/war-and-code/dircue/issues/13) tracks a manually
requested release-preparation workflow: select an exact source commit and
version, build and validate artifacts, then attach them to a draft GitHub Release.
Publication stays a separate maintainer action. Reusing an existing worker
artifact requires matching build inputs and provenance, not merely selecting the
most recent successful run. A PR merge commit must not be represented as the
final tagged source without an explicit equivalence check.

No release is published by the CI workflows in this change. PyPI publication
remains separate, tracked in [#14](https://github.com/war-and-code/dircue/issues/14).
Until release automation is implemented, the documented local release scripts
remain supported.
