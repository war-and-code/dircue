# Tests

This directory holds automated harnesses, regression fences, and evidence receipts for dircue.

## Receipts policy

Evidence files fall into three categories:

### Regression fences (stay in-tree)

These files **must remain committed** because CI re-runs the harness and compares against them:

| Directory | CI job | Purpose |
|-----------|--------|---------|
| `tests/map_corpus/` | `test` | Golden map-corpus expected outputs (141 repos) |
| `tests/compatibility_next/composition/results/canonical/` | `preflight` | Composition golden expectations |
| `tests/conformance/` | `linguist-conformance` | Linguist 9.7.0 differential |
| `tests/focus_v070/` | `preflight` | Focused-analysis receipt contracts |
| `tests/context_v080/` | `preflight` | Context-aware receipt contracts |
| `tests/compatibility_v060/` | `preflight` | Compatibility with v0.6.0 output |
| `tests/compatibility_next/` | `preflight` | Compatibility matrix harness |
| `tests/metrics/` | `metrics-conformance` | scc 4.1.0 differential |
| `tests/registries/` | `preflight` | Registry evidence fences |
| `tests/map_corpus/review_holdout/` | `test` (Linux) | Source-first holdout raw receipts and frozen score; scorer verified against `receipts/score.json` |

**Rule:** If a CI job or `make test` imports a file from `tests/`, that file is a fence and stays in-tree. Never move a fence to the evidence archive.

### Evidence archive (moved out)

Bulky historical evidence that is NOT read by any CI job, `make test`, or in-tree harness is stored as assets on the GitHub release tagged `evidence-archive-1`. The manifest is at `tests/receipts/evidence-archive.json`.

To restore these files locally (needed only for manual re-audit, not for CI):

```sh
make fetch-receipts
# or
python3 scripts/fetch_receipts.py
```

While the repository is private, the release download URLs need authentication.
The script then retries through the GitHub API with a token from `GH_TOKEN`,
`GITHUB_TOKEN` or `gh auth token`. The restored directories are listed in
`.gitignore`, so the working tree stays clean.

To verify files are present without downloading:

```sh
python3 scripts/fetch_receipts.py --check
```

**Why moved?** These families are hardware-dependent measurements or historical audit dumps. They inflated `git clone` size by ~80 MiB packed. Removing them from history (via `git-filter-repo`) reduces a `--depth=1` clone by ~80 MiB.

Moved families:

| Family | Packed MiB | Reason |
|--------|------------|--------|
| `tests/enry-performance/results/` | ~75 | Language-detection optimization evidence; hardware-dependent |
| `tests/stress/results/` | ~3 | Stress-test timings and RSS; hardware-dependent |
| `tests/profiling/results/` | ~2 | Raw CPU/memory profiles; host-specific |
| `tests/release/results/` | ~1 | Historical release audit dumps; not re-runnable |

### Delete (no active claim)

Files with no active test claim and no reusable evidence value. These are identified in `tests/receipts/inventory.json` with `classification: delete`. They are removed from the working tree and (after history rewrite) from all history.

## Adding new evidence

- **Regression fence:** add a `test_*.py` that imports the file and is wired to a CI job in `.github/workflows/ci.yml`. The file stays in-tree.
- **Archivable evidence:** do NOT commit large binary dumps, tarballs, or raw timing JSON directly. Instead, record a manifest entry in `tests/receipts/evidence-archive.json` and upload the asset to the relevant GitHub release. The `fetch_receipts.py` script handles retrieval.
- **No network in CI:** CI jobs must not call `make fetch-receipts` or touch the evidence archive. All in-tree fences must be self-contained.
