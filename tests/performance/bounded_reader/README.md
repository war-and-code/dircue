# Bounded-reader performance evidence

The reader uses file-size hints to reduce allocation while preserving the existing read limits and returned bytes. These experiments compare it with the released v0.6.0 runtime. They show a substantial improvement for the XML fixture, smaller changes on source repositories, and a real speed/memory tradeoff. They do not establish a universal speedup or memory reduction.

## Which implementation was measured?

There are two candidate implementations in this evidence. Their results must remain separate:

1. **Original reader:** the eight-corpus benchmark and allocation profiles measured the first implementation. It reserved at most 1 MiB plus the existing lookahead byte and grew geometrically when necessary.
2. **Final reader:** an additional correction caps growth by the remaining positive size hint. This avoids retaining almost twice a file's size when a file slightly exceeds a growth boundary and the configured read limit is much larger. The XML and Roslyn directory follow-ups and the final eight-corpus benchmark measured this implementation. The smaller initial reservations are experimental overlays, not production settings.

The v0.6.0-runtime baseline executable is identical across the uninstrumented experiments: SHA-256 `0ec849e2950a7b7c8d4cd726dc2d8d55cacb6885ab5467a4eb6547e32712b35b`. Both versions report `0.5.0` solely to permit exact output comparisons; the baseline runtime comes from v0.6.0 (`bf211aeb0ba272bf5819dd326c805b68011dac7a`). These are comparison builds, not the published release binaries. The final reader executable is `bc79509c78e74b6bd8bfb3faf0cd852c0e5b0dc1fe81c331404fd3eb3384803a`. Instrumented executables are different builds and report `0.6.0`.

[The original benchmark](benchmark-results.json) retains all eight corpora, samples, source identities and measured output hashes. [The focused build manifest](followups/focused-builds.json.gz) records the final and experimental executables, effective compilation inputs, module metadata, compiler/tool hashes and build flags. [Capacity comparisons](capacity-comparison.json) document the growth correction separately.

## Results

The uninstrumented measurements used fresh CLI processes, eight workers, warm filesystem caches on one macOS ARM64 host. The original and focused follow-ups used 20 alternating pairs per comparison; the final full-corpus run used 10 pairs after three warmups per lane. Build, test and proof jobs were paused during focused timing. Ordinary desktop activity remained. Runtime is elapsed wall time; memory is the operating system's maximum resident-set statistic per invocation. Tables show medians. MiB means bytes divided by 1,048,576; the JSON retains raw bytes.

The original benchmark had no increase in median runtime across its eight language and aggregate comparisons. Some differences were small enough to be noise. Aggregate median RSS fell on six corpora, increased 1.97% on Roslyn Git, and increased **16.04% on the XML fixture**. This original result remains part of the evidence:

| Implementation and workload | Baseline time | Candidate time | Time change | Baseline RSS | Candidate RSS | RSS change |
|---|---:|---:|---:|---:|---:|---:|
| Original reader, XML aggregate | 60.25 ms | 38.37 ms | −36.32% | 67.46 MiB | 78.28 MiB | **+16.04%** |
| Final reader, XML aggregate | 61.56 ms | 41.00 ms | −33.40% | 68.43 MiB | 74.58 MiB | **+8.99%** |
| Final reader, XML languages | 31.76 ms | 27.77 ms | −12.56% | 54.19 MiB | 50.21 MiB | −7.34% |
| Final reader, Roslyn directory aggregate | 3,225.43 ms | 3,141.61 ms | −2.60% | 87.15 MiB | 81.97 MiB | −5.94% |
| Final reader, Roslyn directory languages | 3,183.51 ms | 3,142.75 ms | −1.28% | 74.48 MiB | 69.85 MiB | −6.22% |

The two XML aggregate rows were collected in separate runs. Their difference does not prove that the growth correction reduced RSS: this XML path already reaches its read limit at the initial reservation, so the corrected growth branch is not used. Runtime scheduling and GC state vary between fresh processes. The follow-up confirms the speed gain and continued memory tradeoff without replacing the original samples.

The XML fixture contains 128 files totaling 2 GiB. Its full contents are hashed before and after measurement, but CLI profiling reads bounded prefixes. The Roslyn directory contains approximately 434 MiB of regular-file content. Neither result is a full-content throughput measurement. The focused harness invokes `analyze languages`; the original benchmark uses the compatible root language command.

All baseline/candidate language and aggregate outputs matched byte-for-byte, with matching exit status and empty stderr. The optional format lanes in the original benchmark measure additional candidate-only work; they are not baseline-versus-candidate speed improvements. See [XML samples](followups/xml-results.json) and [Roslyn samples](followups/roslyn-directory-results.json).

## Final full-corpus confirmation

The [final reader run](final-corpus-results.json) repeats all eight inputs with the growth correction and the exact final executable identified above. Its 480 measured invocations and 144 warmups passed output and corpus-identity checks. Existing language and aggregate output matched byte-for-byte for each input.

| Input | Language time | Language RSS | Aggregate time | Aggregate RSS |
|---|---:|---:|---:|---:|
| cobra-git | -0.37% | -3.65% | -0.17% | -5.93% |
| express-git | -0.93% | -4.08% | -1.28% | -2.24% |
| flask-git | -2.58% | -1.59% | -2.40% | -2.66% |
| ripgrep-git | -1.44% | -2.71% | -2.04% | -1.22% |
| roslyn-git | -0.46% | +1.07% | -0.87% | +1.83% |
| spring-framework-git | -1.15% | -1.83% | -1.46% | -4.51% |
| roslyn-directory | -2.52% | -7.06% | -3.27% | -5.88% |
| large-xml-directory | -12.29% | -4.96% | -33.90% | +7.58% |

Values are median changes relative to the baseline in that run. The XML aggregate took **64.65 → 42.73 ms (−33.90%)**, with **67.53 → 72.65 MiB (+7.58%)** peak RSS. Roslyn's selected Git tree also had a small RSS increase. The small time differences on most source repositories are not strong evidence of a speedup; this run shows no median slowdown on those inputs. All samples are retained, including negative tradeoffs. Comparing the original and final runs does not isolate the effect of the growth correction, because host and runtime state differ.

## Why keep the 1 MiB reservation?

Smaller reservations reduced the XML aggregate RSS cost but also surrendered much of the speed gain. Each row below has its own interleaved baseline samples. The reservation includes one additional lookahead byte.

| Initial reservation | Aggregate time change | Aggregate RSS change |
|---|---:|---:|
| 1 MiB, final default | −33.40% | +8.99% |
| 512 KiB experiment | −21.02% | +2.65% |
| 128 KiB experiment | −10.28% | −0.51% |

The final default retains the larger speed gain. No global garbage-collection policy or new CLI flag was introduced.

## Existing memory tuning

A separate experiment used the same final executable with `GOMEMLIMIT` explicitly unset or set to each target. `GOGC`, `GODEBUG` and `GOMAXPROCS` were unset throughout. These are characterization points, not universal recommended settings:

| Soft target | Unset time → target time | Unset RSS → target RSS |
|---|---:|---:|
| `32MiB` | 39.31 → 61.31 ms | 77.05 → 49.34 MiB |
| `64MiB` | 38.03 → 39.13 ms | 78.63 → 76.10 MiB |
| `128MiB` | 39.58 → 38.00 ms | 76.39 → 78.90 MiB |

At `32MiB`, median RSS fell **35.96%** and runtime increased **55.95%**, with identical answers. The smaller differences at 64 and 128 MiB should not be treated as reliable general improvements. [All samples and environment settings](followups/memory-target-results.json) are retained.

On a POSIX shell, a caller can choose this existing Go runtime setting explicitly:

```sh
GOMEMLIMIT=32MiB dircue analyze all --json --workers 8 /path/to/directory
```

`GOMEMLIMIT` is a **soft Go runtime memory target, not a hard process RSS limit**. The measured 32 MiB target still produced 49.34 MiB median RSS. It does not constrain optional external processes. Use an operating-system or container limit when a hard boundary is required, understanding that exceeding it may terminate a process. Choose settings for the actual workload; a small target can cause substantial extra collection work.

## Allocation and retention

The original-reader instrumentation ran three fresh processes per implementation. Median allocated bytes fell from 345,965,832 to 161,137,296 (**53.42%**), allocation count from 22,735 to 13,700, and automatic collections from 20 to 7. Lower allocation did not imply lower peak RSS.

A separate diagnostic forced two consecutive idle collections. Median live heap after the first was 4.76 → 9.04 MiB; after the second it was 3.96 → 3.96 MiB. Stack profiles showed retained regular-expression pools after the first collection. This supports changed GC cadence and idle pool retention as an explanation; it does not prove the cause of every production RSS sample or exclude unrelated leaks.

[Original allocation samples and profiles](followups/original-allocation/allocation-results.json) and [the separate two-GC diagnostic](followups/original-idle-gc/allocation-results.json) retain their source identities. **Instrumented timings are not production latency evidence.** No instrumentation timing claim is made. Allocation sampling and forced collections change runtime behavior; compilation and test jobs also ran concurrently during instrumentation. See [the allocation harness documentation](README-allocation.md).

## Reproduction

Use a checkout with the release runtime plus only the reader changes. Other runtime changes deliberately fail baseline identity checks. An already-installed matching Go toolchain and cached dependencies are required; builds disable automatic toolchain downloads and module fetching, and use read-only module selection. The recorded run used Go 1.26.6. All output directories must be new and should be ignored local directories.

```sh
python3 tests/performance/bounded_reader/build_variants.py \
  --go-binary /path/to/installed/go \
  --output .cache/reader-reproduction

python3 tests/performance/bounded_reader/measure_variants.py \
  --build-dir .cache/reader-reproduction \
  --corpus /path/to/xml-fixture --name xml

python3 tests/performance/bounded_reader/measure_variants.py \
  --build-dir .cache/reader-reproduction \
  --corpus /path/to/roslyn --name roslyn-directory --variants growth

python3 tests/performance/bounded_reader/measure_memory_targets.py \
  --build-dir .cache/reader-reproduction --xml-root /path/to/xml-fixture
```

Stop competing compute work before timing and record the actual host and background activity. The directory fixture identities and pinned public repository revisions appear in the receipts. The helpers preserve raw reports, binaries and local paths under the chosen ignored directory. They use the existing [v0.6.0 measurement utilities](../v060_candidate/benchmark.py) for corpus hashing, timeout handling and operating-system RSS collection.

The portable helpers are adaptations of the collection scripts: they accept paths explicitly and add environment/harness checks. Recorded collection-script hashes remain unchanged in the receipts; the adaptations do not claim to have produced the historical samples. A later guard hardening also checks complete release-file presence and every non-test Go/native compilation source before package selection, so a deleted init-only file or a changed build tag cannot silently narrow the baseline. Eleven temporary-fixture tests cover that guard. A separate [integration check](reproduction-check.json) then ran the hardened helper on the complete checkout and reproduced all four historical executable hashes and effective input maps exactly. This does not retroactively apply the guard to the original captures. No further timing runs were used for this reproduction check.

Use `export_followups.py --help` for exporting fresh follow-up results, and [the allocation instructions](README-allocation.md) for instrumented runs. `export_benchmark.py` handles either full-corpus receipt. Exports preserve raw measurements and hashes while omitting binaries, full repository reports, temporary source files and local filesystem paths. The [export receipt](followups/export-receipt.json) records transformations and source receipt hashes.
