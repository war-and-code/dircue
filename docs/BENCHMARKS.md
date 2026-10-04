# Benchmarks

Performance evidence needs a known input, explicit builds and a correctness check. A faster command that changes its answer is not evidence of a behavior-preserving optimization.

All workflows are event-driven. Hosted-runner timings are descriptive; CI does not enforce wall-time thresholds or accept new baselines automatically.

## Whole-CLI comparisons

`tests/bench/run.py` compares two explicit binaries against the same declared inputs. It checks exit status and exact stdout and stderr before collecting timings, then checks every warmup and timed sample against that initial output. A changed answer, timeout, failed command or changed input makes the run fail and withholds all timing ratios.

```sh
make bench-cli \
  BENCH_BASELINE=/path/to/baseline/dircue \
  BENCH_CANDIDATE=/path/to/candidate/dircue \
  BENCH_CORPUS=. \
  BENCH_OUTPUT=.cache/bench/comparison.json
```

The binaries must have distinct SHA-256 hashes. Build both with the same Go version, compiler flags and resource settings when measuring an optimization. Record those build commands alongside the receipt; the harness identifies the supplied files but does not build them or infer their source commits.

The starter manifests are:

| Manifest | Work exercised |
| --- | --- |
| `tests/bench/scenarios.json` | Legacy language JSON, default `analyze all`, standalone languages, discovery, ecosystems, frameworks, projects, declarations, metrics, formats and lockfiles, maps, saved-report comparison and a report attachment. |
| `tests/bench/scenarios.topology.json` | Maps and projects from the Gradle, Procfile and Aspire fixture. |
| `tests/bench/scenarios.worker.example.json` | Explicit structural-worker function and hotspot analysis. Supply `BENCH_WORKER=/path/to/dircue-structural-worker`. |

These small fixtures exercise the harness and named command paths. They do not represent every ecosystem, repository size, source mode, attachment format or option combination. For a representative workload, supply `BENCH_MANIFEST` with pinned repository inputs and their provenance. Include every source tree, saved report and external file that can affect a command; hashing only a convenient subset would give incomplete input provenance. Commands should read inputs, not modify them. The before/after digest check does not enforce a read-only filesystem or detect a write that is undone before the final digest; use immutable inputs and trusted builds for an optimization comparison. Git-mode scenarios need a repository root and declared Git metadata as well as source files.

The manifest is limited to 8 MiB and uses `schema_version: 1`. Each scenario has a unique `name`, an argument array, `cwd`, declared corpus-relative `inputs`, and optional `env`, `timeout_seconds` and `provenance`. `{corpus}`, `{cwd}` and an explicitly supplied `{worker}` expand without a shell. Only successful exit-0 scenarios are supported. See the committed manifests for examples.

The receipt contains binary, worker, manifest and input hashes; arguments and working directories; output hashes and saved reference bytes; platform details; and each sample's elapsed time and execution order. Input and executable identities are checked again after execution. A safe failed reuse of a previously passing receipt marks it failed rather than leaving a stale success. Keep the report and its adjacent `*-outputs/` directory together. Outputs cannot overlap measured inputs, the manifest or executables. Choose a fresh receipt path for each run: an existing adjacent output directory is never removed or reused.

Samples alternate baseline-first and candidate-first. Medians, p95 values and paired ratios describe warm runs: correctness preflight and explicit warmups happen first, and the harness does not flush caches. Peak RSS is reported as unavailable rather than estimated from cumulative child-process usage. Measure process memory separately with appropriate OS tooling when assessing a memory claim.

Equality is a necessary check for an isomorphic optimization, not an accuracy oracle. A release that deliberately adds observations may fail strict comparison; validate those additions with labeled semantic tests and document them separately. Do not normalize away the new output to obtain a favorable speed ratio.

Run harness self-tests with `make test-bench`. Full Linux CI also compares two differently packaged builds of the same source on the starter scenarios. That is an installation and harness smoke test, not evidence of a speed improvement.

## Go benchmarks

`make bench` runs the existing scanner benchmarks. Package benchmarks isolate parsing, graph construction, structural responses and report comparison:

```sh
go test ./pkg/scanner ./pkg/projects ./pkg/deployables ./pkg/declarations \
  ./pkg/intentmap ./pkg/structure ./pkg/reportdiff ./pkg/packageevidence ./pkg/rules \
  -run '^$' -bench . -benchmem -count=5
```

The Procfile, Gradle and Aspire benchmarks check their fixture answers before timing. They cover supported and guarded parser paths; they do not replace map-level relationship tests. Full Linux CI executes these benchmark assertions once with `-benchtime=1x`.

Full Linux CI also runs the deployable fuzz targets with a 1,000-execution budget and an explicit cache. Locally, `make fuzz-campaign FUZZ_TIME=30 FUZZ_CACHE=.cache/fuzz` discovers and runs every Go fuzz target. `FUZZ_COUNT=10000` instead uses Go's execution-count limit, including corpus processing and minimization work, with at most 100 minimization attempts per input. This avoids the wall-clock cancellation race documented in [Go issue 75804](https://github.com/golang/go/issues/75804) without treating timeout failures as successes. Discovery errors, missing targets and failing executions stop the campaign. Generated coverage inputs use the chosen cache; failure reproducers go into the affected package's `testdata/fuzz` directory.

Use `benchstat` to compare repeated measurements from the same machine and build configuration. Time and allocation figures both need interpretation: scheduling, worker counts, runtime versions and new functionality can change them. No automatic allocation threshold is currently enforced.

## Deterministic work counters

`dircue map --stats-json PATH` writes a separate run-statistics document. It keeps timing and heap observations out of the deterministic map payload. The schema is available offline with `dircue capabilities --schema stats --json`.

```sh
dircue map --source directory --stats-json .cache/stats.json /path/to/input
```

The `deterministic_costs` section describes enumerated files, reads, requested bytes and limit hits. The `measurements` section contains observed wall time, phase times, sampled peak heap and GC count. Heap measurements are not process RSS or an enforced memory quota.

The `counter-regression` CI job maps corpus fixtures with a non-draft pull request's head and base builds and reports changes in logical work. It is report-only: a new observer can legitimately read more files. Timings are not compared. Locally:

```sh
python3 tests/bench/counter_compare.py --base OLD --head NEW tests/map_corpus/fixtures/*/
```

## Manual performance workflow

The `Paired CLI performance` workflow in `.github/workflows/perf-stable.yml` requires an explicit `baseline_ref`. It builds that commit and the selected workflow revision with the same flags, records their commits and build settings, then runs the strict whole-CLI comparison. Its other inputs select a fixture, source mode, worker count, sample count and optional notes. The default is a directory-mode map of `polyglot-deployable` with five alternating pairs.

Results and failures are uploaded as `perf-paired-results`. A changed map prevents timing ratios from appearing in the job summary. The workflow uses `ubuntu-latest`; it does not provision or discover a dedicated performance runner. A local run on a quiet machine is preferable when small timing differences matter.

## Profiling and release evidence

`map --cpuprofile PATH` and `map --memprofile PATH` write Go diagnostic profiles. These flags add overhead and are off by default. Inspect profiles with `go tool pprof`; the scanner profiling harness is documented in `tests/profiling/README.md`.

Release validation combines compatibility, schemas, corpus expectations, mutation tests and appropriate performance measurements. Historical reports in `docs/releases/` describe the actual scope of each run; they are not a promise that every release benchmarks every tool or every resource preset. Preserve first-run and frozen receipts, and label later adjudicated runs as regression evidence. See `docs/RESOURCE_BUDGETS.md` for the distinction between cooperative settings and externally enforced resource limits.
