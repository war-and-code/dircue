# v1.0 preparation: v0.8.0 performance evidence

This is measurement-only work against released commit `121027f44f014ac8d0a003a451a04b080a5ffe88`. Production sources were unchanged throughout baseline construction and binding. Later candidate edits are separate from that frozen baseline. The baseline is optimized Go with symbols, not the stripped distribution executable. No installed tools, OS tuning, cache flushing, or corpus execution is involved.

## Workload contract

The CLI baseline measures pinned Spring Framework (Java), Roslyn and ASP.NET Core (C#) Git trees and the existing 128-file, 2 GiB XML directory. Every scenario has 3 warmup processes followed by 20 measured fresh processes, alternating lane order. Lanes are languages, metadata discovery, ordinary all, and projects; ASP.NET and XML additionally use combined optional declarations, graph, formats, environments, availability, registries and projects. These lanes answer different questions; their ratios are feature cost, not equivalent-work speedups.

`--workers 8` makes the scenarios comparable with previous repository evidence. The application default is `min(GOMAXPROCS, 16)` and would be 10 on this host absent external constraints. Eight is an explicit experiment setting, not a claim about default performance. No runtime limits are imposed.

The `benchmark.py prepare` command reuses the prior corpus fingerprint implementation. It records Git commit, tree, recursive entry manifest and Git object-storage state, or directory content hashes. The `measure` command verifies byte-identical JSON per lane and fingerprints input again after all timing. Full JSON is retained under ignored `.cache/v100/perf/golden-*.json.gz`; durable receipts keep output hashes. Repeatability does not substitute for a language correctness oracle; use the repository's pinned corpus acceptance evidence before optimizing behavior.

## Reproduction

From the repository root, build the baseline only while no measurement is active:

```sh
mkdir -p .cache/v100/baseline .cache/v100/perf
go build -mod=readonly -buildvcs=false -trimpath -ldflags '-X dircue/internal/cli.Version=0.8.0' -o .cache/v100/baseline/dircue .
go test -c -mod=readonly -buildvcs=false -trimpath -o .cache/v100/baseline/scanner.test ./pkg/scanner
python3 tests/performance/v100_preparation/benchmark.py prepare --corpus /path/to/existing/corpus --xml /path/to/existing/xml-only
python3 tests/performance/v100_preparation/benchmark.py measure
python3 tests/performance/v100_preparation/profile.py
```

The baseline reproduction commands require a fresh pinned v0.8.0 checkout and existing inputs; do not overwrite the retained baseline binaries. `prepare` and `measure` refuse existing receipts or samples. Set `V100_OUTPUT_DIR` to a fresh durable artifact directory for a new run. `validate.py` refuses to replace an existing source binding, especially after candidate edits; verify frozen binaries against their stored receipts instead. Historical harness versions referenced by earlier receipts are retained compressed in `harness_history/`. The CLI measurement implementation currently requires macOS `/usr/bin/time -l`.

`profile.py` uses the existing `BenchmarkProfileScanner` protocol: CPU filtered to `phase=scan`, allocation profiles interpreted through product caller stacks, and distinct mutex/block/trace invocations. Scanner `all` covers default detectors; optional CLI modules require the separate `profile_main.go.txt` wrapper. The wrapper compiles only when explicitly copied into ignored `.cache/v100/baseline`; no profiler is added to shipped code.

## Interpretation limits

The desktop measurement window was coordinated, but background system processes remained active. Host swap usage and process activity are recorded. Latency includes process startup, the time wrapper, JSON serialization and output piping. Peak RSS is measured in bytes by macOS time. User and system CPU have coarse time-command resolution on short workloads. Logical bytes/sec counts corpus bytes even when bounded reads avoid most content, so it is not disk bandwidth. File/block I/O, page faults and context switches are OS counters; they do not identify physical storage latency.

Twenty raw samples establish a diagnostic baseline. p95 is a worst-few-of-20 observation; p99, p99.9 and p99.99 all become worst observed under nearest-rank calculation and are not reliable population-tail estimates. All outliers are retained. There is no invented absolute SLA. Investigate relative differences over 10% with repeated interleaved same-host baseline/candidate runs; do not accept changes from historical medians alone. PSS, per-core process utilization and allocator peak heap are unavailable in this collection; allocation/live-heap profiles and standalone RSS answer different memory questions.

## Cache-only candidate

The follow-on experiment and its invariants are documented in [OPTIMIZATION.md](OPTIMIZATION.md). `build_candidate.py` extracts the frozen archive, proves an identical baseline rebuild, applies only the enumerated cache/lifetime/fork changes, and builds the candidate with the same flags. `compare_candidate.py verify` checks all 22 retained CLI goldens; `compare_candidate.py measure` records alternating adjacent pairs on six representative lanes. The latter requires a coordinated quiet window and refuses an existing final receipt. `profile_candidate.py` separately captures the Spring scanner CPU/allocation profiles against the source-bound candidate scanner executable, requiring the baseline report digest. Sampler runs do not overlap latency timing.

The standalone scripts preserve actual local input paths in machine-readable reproduction receipts. Public-facing summaries use corpus names and repository-relative paths; no corpus source contents are included. The ignored cache contains executable binaries, raw pprof/trace data and exact stdout goldens. Keep that cache with the source-bound receipts when reproducing or auditing this particular host run.
