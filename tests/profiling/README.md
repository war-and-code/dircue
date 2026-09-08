# Scanner profiling protocol

The scanner benchmark has produced [RC1 evidence](results/rc1/README.md), [refreshed scanner profiles](results/scanner-opt2/README.md), and paired experiments for [generated-line scanning](getlines-optimization.md) and [model loading](model-format-optimization.md). Use this protocol for additional measurements. Do not run profiling while a release timing gate, corpus preparation, build, test suite, or another profiler is active.

`BenchmarkProfileScanner` calls the real `scanner.Scan` API against an existing directory or committed Git tree. It includes traversal, attribute resolution, Git object access, bounded reads, classification, aggregation, and optionally detectors. It never clones, generates, modifies, builds, or preloads source fixtures. There is no HTTP debug endpoint or production instrumentation.

Ordinary `go test ./...` does not execute benchmarks. An explicitly selected profiling benchmark skips when `DIRCUE_PROFILE_ROOT` is absent.

## Scenarios and correctness

Start with the existing independently checked public corpus, pinned by `tests/performance/corpus.json`:

| Fixture | Commit | Comparison dimensions |
| --- | --- | --- |
| `spring-framework` | `9e8cea3ef8ae02efb7956b071cd7bbef7c22cb82` | Java, directory/Git, 1/4/16 workers |
| `roslyn` | `ca7d6c1a040cda9fecd1ffe3720fb971251ace67` | C#, directory/Git, 1/4/16 workers |
| `aspnetcore` | `7387de91234d3ef751fa50b3d1bfede4130213ff` | .NET monorepo, directory/Git, 1/4/16 workers |

Use already-created Git-free views for the directory scenarios. A working checkout passed with `SOURCE=directory` is a different scenario if it contains untracked content or different attributes. Fingerprint the exact view and its accepted reference result. Retain packed/delta storage receipts for Git scenarios: identical source content does not imply equivalent object-access costs.

After profiling the public cases, consider the existing `tests/stress` fixtures: the Java export-shaped tree, XML log, .NET project graph, packed variants, and actual delta history. This harness does not regenerate them. A complete scan of the 100,000-file boundary fixture requires a higher `MAX_TREE_SIZE`; an empty report caused by the tree limit is rejected as a performance baseline.

Each measured result is compared with the baseline result, including sorted file evidence, findings, warnings, byte counts, and percentages. The comparison does not serialize an extra payload per iteration. The final fingerprint contains SHA-256 of the complete serialized report with only `root` cleared. An optional independent expected digest makes a mismatching result fail. Retain the Linguist reference comparison with the fixture receipt; repeatability alone cannot establish correctness.

## Required environment

All paths should be absolute. Artifacts must be outside the scanned tree, including through symlinked parent directories. The benchmark checks its output directory and the standard CPU/heap/mutex/block/trace paths before scanning.

| Variable prefix: `DIRCUE_PROFILE_` | Meaning |
| --- | --- |
| `ROOT` | Existing directory or exact Git repository root; required to enable the benchmark |
| `SOURCE` | `directory` (default) or `git`; `auto` is deliberately unavailable |
| `REVISION` | Required 40-character commit SHA for Git; must be unset for directory |
| `FIXTURE_ID` | Explicit scenario identity, including corpus name, revision/view and attribute variant |
| `RUN_ID` | A single filename component; the harness adds a unique suffix per benchmark invocation |
| `OUTPUT_DIR` | Directory for JSON fingerprints, outside the fixture |
| `FIXTURE_RECEIPT` | Existing small receipt identifying fixture bytes, Git tree/storage, and accepted oracle output |
| `BUILD_RECEIPT` | Existing receipt identifying the exact test binary, source revision, dirty state, toolchain and build flags |
| `HOST_RECEIPT` | Existing host/container, CPU, RAM/swap, storage/filesystem, isolation and power-state receipt |

Receipt files are limited to 8 MiB and hashed after timing. The acceptance stress manifest is larger: supply a small summary receipt containing its SHA-256, path, generation version, selected scenario, actual file/byte counts, Git storage details, and acceptance-result SHA-256. Do not pass the entire manifest or source payload as a receipt. Prepare and validate these receipts outside the measured process so their collection cannot warm source data during profiling.

Optional variables:

| Variable | Default | Meaning |
| --- | --- | --- |
| `WORKERS` | `1` | Explicit scanner worker count, 1–1024; compare 1, 4 and 16 before expanding |
| `MODE` | `languages` | `languages` or `all` using the real default detectors |
| `INCLUDE_FILES` | `0` | `1` includes per-language path arrays, corresponding to breakdown-style output |
| `MAX_TREE_SIZE` | `100000` | Explicit file-count boundary; keep consistent across the comparison |
| `INIT` | `warm` | `warm` or `first-scan`, defined below |
| `OS_CACHE` | `uncontrolled` | Recorded description, not a cache-flushing control |
| `EXPECTED_SHA256` | unset | Previously accepted semantic report digest for this exact configuration |
| `MUTEX_FRACTION` | `0` | Opt-in `runtime.SetMutexProfileFraction`; e.g. `5` |
| `BLOCK_RATE_NS` | `0` | Opt-in `runtime.SetBlockProfileRate`; e.g. `10000` |

The fingerprint also records Go build information, logical CPU count, GOMAXPROCS, GOGC, GOMEMLIMIT, memory sample rate, profiler flags, iterations, measured elapsed time, summary counts, and result digest. Supply hardware details through the host receipt; runtime CPU count is not a substitute for CPU model, available memory, filesystem or container limits. Do not change governors, caches, kernel settings, resource limits or affinity without a separate authorized experiment and receipt.

## Initialization and cache semantics

`INIT=warm` performs one untimed full scan, then measures subsequent scans in the same process. This warms scanner/classifier lazy state **if that fixture actually reaches it**, and also touches the fixture through normal reads. It does not guarantee all filesystem pages remain resident. The process-global centroid classifier cannot be reset through this harness.

`INIT=first-scan` requires a fresh process and exactly:

```text
-run='^$' -bench='^BenchmarkProfileScanner$' -benchtime=1x -count=1
```

For a compiled test binary those flags have the `-test.` prefix. The harness rejects repeated first-scan invocations in the same process. First-scan includes lazy work reached by the first `Scan`, but Go package initialization has already happened before benchmark entry. Standard Go CPU profiling also starts after package initialization. It is therefore **not** a complete cold process-start measurement, and it says nothing about cold filesystem caches. Use the existing separate-process CLI timing harness to measure deployment startup and end-to-end latency. Do not use `-count=20` to claim twenty cold engine runs; launch twenty fresh processes instead.

## Build and collect after the timing gate

Use the same host/container and source revision as the accepted baseline. Build a separate optimized, unstripped benchmark binary, not the size-stripped release artifact:

```sh
PROFILE_ARTIFACTS="$PWD/.cache/profiling/java-git-w1"
mkdir -p "$PROFILE_ARTIFACTS"
go test -c -o "$PROFILE_ARTIFACTS/scanner.test" ./pkg/scanner
```

Do not use `-race`, `-gcflags='all=-N -l'`, or `-ldflags='-s -w'` for performance collection. Normal Go compiler optimization, inlining and debug information should remain enabled. Record the test binary's SHA-256, `go version -m` output, exact compiler flags, source commit, and any local diff in the build receipt before running. Commit or explicitly fingerprint newly added files too; `git diff` alone omits untracked files.

Set the required receipt paths and scenario values. This example assumes receipts already exist:

```sh
export DIRCUE_PROFILE_ROOT="$PWD/.cache/corpus/spring-framework"
export DIRCUE_PROFILE_SOURCE=git
export DIRCUE_PROFILE_REVISION=9e8cea3ef8ae02efb7956b071cd7bbef7c22cb82
export DIRCUE_PROFILE_FIXTURE_ID=spring-framework-git-pinned
export DIRCUE_PROFILE_RUN_ID=java-git-w1-warm
export DIRCUE_PROFILE_OUTPUT_DIR="$PROFILE_ARTIFACTS"
export DIRCUE_PROFILE_FIXTURE_RECEIPT="$PROFILE_ARTIFACTS/fixture-receipt.json"
export DIRCUE_PROFILE_BUILD_RECEIPT="$PROFILE_ARTIFACTS/build-receipt.json"
export DIRCUE_PROFILE_HOST_RECEIPT="$PROFILE_ARTIFACTS/host-receipt.json"
export DIRCUE_PROFILE_WORKERS=1
export DIRCUE_PROFILE_INIT=warm
```

Collect an unprofiled repeated benchmark baseline first:

```sh
"$PROFILE_ARTIFACTS/scanner.test" \
  -test.run='^$' -test.bench='^BenchmarkProfileScanner$' \
  -test.benchtime=3x -test.count=20 -test.benchmem \
  > "$PROFILE_ARTIFACTS/baseline.txt"
```

Then collect a CPU profile in a separate invocation, leaving only one sampler enabled:

```sh
"$PROFILE_ARTIFACTS/scanner.test" \
  -test.run='^$' -test.bench='^BenchmarkProfileScanner$' \
  -test.benchtime=10x -test.count=1 -test.benchmem \
  -test.cpuprofile="$PROFILE_ARTIFACTS/cpu.pprof" \
  > "$PROFILE_ARTIFACTS/cpu-run.txt"
go tool pprof -top -tagfocus='phase=scan' \
  "$PROFILE_ARTIFACTS/scanner.test" "$PROFILE_ARTIFACTS/cpu.pprof" \
  > "$PROFILE_ARTIFACTS/cpu-scan-top.txt"
```

Confirm the `phase=scan` tag appears using `go tool pprof -tags` before interpreting the filtered profile. Warmup and validation are separately labeled. `StopTimer` stops Go benchmark accounting; it does **not** stop the CPU sampler. Without this filter, warmup, receipt hashing and result serialization can appear as misleading product costs. Labels propagate to scanner worker goroutines.

Collect allocation evidence separately:

```sh
"$PROFILE_ARTIFACTS/scanner.test" \
  -test.run='^$' -test.bench='^BenchmarkProfileScanner$' \
  -test.benchtime=10x -test.count=1 -test.benchmem \
  -test.memprofile="$PROFILE_ARTIFACTS/heap.pprof" \
  > "$PROFILE_ARTIFACTS/heap-run.txt"
go tool pprof -top -sample_index=alloc_space \
  "$PROFILE_ARTIFACTS/scanner.test" "$PROFILE_ARTIFACTS/heap.pprof" \
  > "$PROFILE_ARTIFACTS/alloc-space-top.txt"
```

These correspond to `go test -bench=... -cpuprofile=...` and `go test -bench=... -memprofile=...`; compiling once makes the profiled binary explicit and keeps build work outside measurement. If using `go test` directly, preserve its exact test binary with `-o` and verify the build receipt again.

Allocation profiles do not carry CPU phase labels. The raw heap profile is cumulative process evidence, including warmup, lazy initialization, benchmark machinery, retained baseline output, final JSON serialization and receipt hashing. Per-iteration result comparison does not allocate a serialized report, but one baseline report stays live. Inspect `Scan` call subtrees and concrete caller stacks; do not rank `profilingReportDigest`, `profilingFileDigest` or fingerprint encoding as product hotspots. Attribute `encoding/json` costs to their callers: manifest detectors can use it, and historical profiles include JSON decoding of the former classifier model format. Distinguish `alloc_space`, `alloc_objects`, and `inuse_space`; none is peak RSS.

For contention attribution, use a separate run with `MUTEX_FRACTION=5` plus `-test.mutexprofile=/absolute/path/mutex.pprof`, or `BLOCK_RATE_NS=10000` plus `-test.blockprofile=/absolute/path/block.pprof`. These controls are opt-in and add overhead. Standard test profiler flags can also enable sampling; the fingerprint records the active mutex fraction and block-profile flags. Do not compare profiled timing directly with unprofiled release timing.

## Interpreting results

`ns/op` is scanner wall time plus the small labeling/timer harness boundary. Go benchmark memory accounting is stopped for validation and fingerprint generation. Repeated `StartTimer`/`StopTimer` calls themselves require runtime accounting, and validation, retained expected output, garbage collection, and profiler activity can influence subsequent iterations. Confirm any apparent improvement later with the existing uninstrumented CLI harness.

`MB/s` and `language-B/op` use full attributed source byte totals, including files that only require prefix reads. They are **not** physical disk throughput. Use host I/O evidence to measure physical bytes, operations and wait time. For process memory, collect peak RSS separately (`/usr/bin/time -v` on Linux or `/usr/bin/time -l` on macOS); on Linux optionally record PSS and container memory limits. Identify CPU model, filesystem, mount/storage topology and competing load in the host receipt.

Twenty repetitions support a baseline and variance review, but cannot estimate p99.9 or p99.99 reliably. Benchmark `ns/op` is an iteration mean. Use individual separate-process measurements for percentile reporting, retaining p50/p95/p99/max and explicit sample counts. Investigate p95 movement within roughly 10% as possible noise before attributing it to a speedup. For scaling, compare recorded file/byte counts across prepared fixtures. Variants of one synthetic fixture are not independent public repositories.

[HOTSPOTS.md](HOTSPOTS.md) preserves the initial investigation. Later profiles and accepted optimization records are linked above. Before selecting another production change, collect fresh evidence for the current source, identify the remaining cost, and follow the change with correctness and uninstrumented timing checks.
