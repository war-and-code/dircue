# Benchmarks

This document describes the three-tier performance measurement system introduced in the 1.0 quality program (issues #89, #90).

## Design principles

- **Deterministic vs measured.** Every run has two kinds of performance data. *Deterministic cost counters* (files enumerated, files read, bytes requested, limit hits) are identical for the same input and settings regardless of worker count or hardware. *Measurements* (wall time, heap, GC count) are observed values that vary with the OS scheduler, CPU load, and memory pressure. CI gates use only deterministic counters; wall time is recorded and reported but never gated on hosted runners.
- **Event-driven only.** No scheduled jobs. Deep harnesses run on `workflow_dispatch`. PR gates are `pull_request`-triggered.
- **Neutral profiler.** dircue is not a security tool. Statistics describe logical work, not findings or verdicts.

## Tier 1: every pull request (deterministic counters)

The `counter-regression` CI job (`.github/workflows/ci.yml`) runs on every non-draft pull request. It builds the pull request head and its base commit, maps each `tests/map_corpus/fixtures/*` directory with both builds using `--stats-json`, and writes a base → head table of the `deterministic_costs` section to the job summary (`tests/bench/counter_compare.py`). Timings are never compared, because they vary between runs.

The job reports; it does not fail on counter changes. A counter that moves is a prompt to read the change, not a verdict: new observers legitimately read more files. Run the same comparison locally with `python3 tests/bench/counter_compare.py --base OLD --head NEW tests/map_corpus/fixtures/*/`.

## Tier 2 — On-demand stable-hardware runs

The `perf-stable.yml` workflow (`workflow_dispatch` only) runs repeated timed measurements on a configurable fixture and uploads a `perf-stable-results` artifact containing all stats documents and a summary.

Inputs:

| Input | Default | Description |
| --- | --- | --- |
| `fixture` | `tests/map_corpus/fixtures/polyglot-deployable` | Directory to scan |
| `source_mode` | `directory` | `directory` or `git` |
| `workers` | `0` | Worker count (0 = auto) |
| `runs` | `5` | Number of repeated timed runs |
| `notes` | _(empty)_ | Free-text hardware description for the artifact |

**Using a dedicated machine.** The job targets `ubuntu-latest` by default. For repeatable, low-noise results, register a self-hosted runner, label it `stable-perf`, and change `runs-on: ubuntu-latest` to `runs-on: [self-hosted, stable-perf]` in a local fork or feature branch. Self-hosted runners are free for public repositories. Do not commit wall-time baselines from shared hosted runners.

**Building a results history.** The artifact `perf-stable-results/summary.json` contains the full run metadata including deterministic cost counters, measured wall times, and heap observations. To build a history, download and append the artifact after each dispatch, or use a tool such as `github-action-benchmark` or Bencher, which can read the per-run `stats.json` files directly.

## Tier 3 — Release report

At each release the full corpus is benchmarked against the previous release and Linguist/scc, and the results are committed as `docs/releases/<version>-validation.md`. Resource-constrained runs (cgroup memory and CPU limits) prove each preset's and memory ceiling's claims.

See `docs/releases/` for historical results and `docs/RESOURCE_BUDGETS.md` for resource-limit documentation.

## run-statistics document (`--stats-json`)

The `map` command accepts `--stats-json PATH` to write a separate `run-stats` JSON document. This document is intentionally separate from the deterministic map payload: measurements vary with hardware and scheduling and must never enter a content-addressed output.

```sh
dircue map --source directory --stats-json stats.json /my/repo
cat stats.json
```

The schema is available offline:

```sh
dircue capabilities --schema stats --json
```

**Document shape:**

```json
{
  "schema_version": "1.0.0",
  "kind": "run-stats",
  "command": "map",
  "source_mode": "directory",
  "deterministic_costs": {
    "files_enumerated": 1234,
    "files_content_read": 987,
    "bytes_requested": 4567890,
    "limit_hits": { "file_bytes": 0, "tree_size": 0 }
  },
  "measurements": {
    "wall_time_ns": 5000000000,
    "phases": { "scan_ns": 4000000000, "build_ns": 800000000 },
    "peak_heap_inuse_bytes": 45678901,
    "gc_count": 3
  }
}
```

## Profiling hooks

The `map` command accepts two profiling flags for diagnostic use. These flags are off by default; they add overhead and must not be used in production automation.

| Flag | Effect |
| --- | --- |
| `--cpuprofile PATH` | Write a Go CPU profile to `PATH` when the scan completes. Analyze with `go tool pprof PATH`. |
| `--memprofile PATH` | Write a Go heap profile to `PATH` after the scan and build complete. Analyze with `go tool pprof --alloc_space PATH` or `--inuse_space`. |

Example:

```sh
dircue map --json \
  --cpuprofile cpu.prof \
  --memprofile mem.prof \
  /my/large/repo > map.json
go tool pprof -top cpu.prof
go tool pprof -top mem.prof
```

For finer-grained package-level profiling see the existing `BenchmarkProfileScanner` harness in `pkg/scanner/profiling_benchmark_test.go` and its documentation in `tests/profiling/README.md`.

## Go micro-benchmarks

Sixteen Go micro-benchmarks live in the scanner and related packages. Run them with:

```sh
go test -bench=. -benchmem ./pkg/scanner/ ./pkg/formats/ ./pkg/declarations/ \
    ./tests/packageevidence/... -count=5
```

Compare across commits with `golang.org/x/perf/cmd/benchstat`:

```sh
go install golang.org/x/perf/cmd/benchstat@latest
go test -bench=. -benchmem -count=5 ./pkg/scanner/ | tee before.txt
# ... make changes ...
go test -bench=. -benchmem -count=5 ./pkg/scanner/ | tee after.txt
benchstat before.txt after.txt
```

On shared CI runners, gate only on `allocs/op` and `B/op` (allocation counters are deterministic); wall-time variance is typically ±20%.
