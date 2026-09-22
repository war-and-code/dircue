# Bounded Git packfile cache experiment

The single optimization reuses at most eight packfile descriptors within a Git snapshot. The measured baseline identified repeated packfile opening and reader construction as the leading actionable CPU/allocation cost. The cache keeps the existing object mutex and large-object threshold. It does not change parser, classifier, worker count, ordering, or optional analysis algorithms.

## Isolation and correctness

`candidate-build.json` records a cache-only build from the frozen v0.8.0 source archive. Before applying the nine listed source/test/provenance files, a fresh build reproduced the original baseline executable byte for byte. Both executables use identical optimized, unstripped build flags and the version override `0.8.0`. Concurrent CLI/schema changes are excluded from this causal comparison. The integrated application receives separate compatibility checks.

`candidate-equivalence.json` records exact stdout equality for all 18 core CLI lanes and four supplemental lanes (mixed language/workspace, combined optional analysis, saved-report comparison, separate structural worker). Every case exits zero with empty stderr. Each timed A/B invocation also checks its exact frozen golden. These are regression oracles for the selected scenarios, not a general proof of classification correctness.

The report-producing algorithms and their iteration, sorting, tie-breaking, floating-point operations and random sources are unchanged. Cached objects identify the same Git hashes. Lazy large-object readers open their own file descriptor and remain valid after cache eviction. The existing mutex continues to cover complete blob-reader lifetimes. Scan waits for its producer and workers before releasing storage. No cached-descriptor options are passed into temporary alternate stores.

Storage ownership is explicit: failed snapshot initialization closes its retained storage, and Scan/Inspect close successful snapshots on every return. An existing primary error takes precedence; a cleanup error on an otherwise successful operation is surfaced instead of silently claiming success. Closing a snapshot consumes ownership exactly once. The maintained fork also closes a newly opened pack if cache insertion fails while evicting an older pack. It preserves the original eviction error; the operating system's state after a failed close cannot be guaranteed or safely repaired by retrying an arbitrary descriptor number.

The new scanner fixture exceeds cache capacity with eleven packs and exercises a lazy object larger than the bounded-read threshold, 1/8/16 workers, a linked worktree, alternate object reads, repeated success/error/cancellation cleanup, and idempotent close-error forwarding. Ordinary alternate-only Scan still encounters the pre-existing go-git size-lookup limitation; this experiment does not expand alternate repository support. The maintained-fork regression injects an eviction close error and proves the failed replacement is closed. Focused scanner tests, the fork filesystem/packfile suite and maintained-fork provenance verification pass; the independent cache review records additional scanner/race checks.

## Measurement contract

`compare_candidate.py measure` runs three warmup pairs followed by twenty measured adjacent baseline/candidate pairs for each selected lane. Pair order alternates AB/BA. All samples and outliers are retained, binaries and corpus fingerprints are checked, and no agent build/test/install is allowed during the measurement window. This remains a shared macOS desktop with background system activity, not an isolated production host. Runtime controls match the source-bound build receipt.

The paired median ratio and fixed-seed bootstrap interval describe this host/window/workload. The interval resamples twenty paired ratios and is not a claim about production uncertainty or extreme tails. At this sample count, p99/p99.9/p99.99 equal the observed maximum. RSS is standalone peak resident memory; sampled heap allocation is a separate metric. Logical input throughput is not disk bandwidth. Eight cache slots is a bounded design choice, not an empirically tuned optimum.

## Interleaved results

Window: 2026-09-21T23:37:36.636987+00:00 to 2026-09-21T23:52:07.178974+00:00. All twenty measured pairs and three warmup pairs per lane matched exact baseline stdout, with empty stderr and zero exit status. Binary and corpus checks passed.

| Workload | Baseline / candidate median ms | Paired time ratio [95% bootstrap] | Baseline / candidate p95 ms | Baseline / candidate RSS MiB |
|---|---:|---:|---:|---:|
| spring-framework languages | 1583.31 / 957.69 | 0.602 [0.599, 0.609] | 1680.06 / 1001.43 | 158.4 / 150.0 |
| spring-framework discovery | 1231.50 / 890.98 | 0.724 [0.717, 0.727] | 1248.39 / 900.81 | 45.2 / 44.8 |
| roslyn languages | 6285.38 / 4495.13 | 0.715 [0.711, 0.723] | 6547.77 / 4659.46 | 359.3 / 356.3 |
| roslyn all | 9416.91 / 7511.24 | 0.800 [0.790, 0.819] | 10277.61 / 8593.41 | 357.9 / 354.7 |
| aspnetcore optional | 2947.89 / 1682.66 | 0.570 [0.568, 0.580] | 3013.07 / 1798.96 | 315.2 / 317.9 |
| xml-2gib optional | 171.66 / 170.74 | 1.000 [0.991, 1.014] | 175.50 / 178.29 | 79.5 / 78.5 |

Ratios below 1 mean lower elapsed time. CPU percentage, logical throughput, all raw CPU/time/RSS/I/O counters and individual paired ratios are retained in `cache-ab.json.gz`. The paired interval does not repair shared-host bias or imply precision for p95 and rarer tails.

## Attribution and acceptance

**Accept this one bounded-cache lever for the measured workloads.** Every Git lane improved: paired median elapsed time fell 20–43%, and every paired bootstrap upper bound is below 1. The directory control passes: its paired ratio is 1.000, with interval 0.991–1.014, consistent with no material change. Its observed p95 rose 1.6%; this is below the predeclared 10% investigation threshold and does not establish a tail regression with twenty samples. No claim of faster directory processing is made.

Peak RSS is lower or effectively stable in five lanes; ASP.NET optional rises from 315.2 to 317.9 MiB (0.9%). This small bounded-cache tradeoff is accepted alongside its 43% elapsed-time reduction. CPU utilization percentage can rise because the same work finishes sooner, so total user+system CPU seconds per process are shown below rather than treating utilization as work.

| Workload | Median CPU seconds, baseline / candidate |
|---|---:|
| spring-framework languages | 3.030 / 2.335 |
| spring-framework discovery | 1.470 / 1.090 |
| roslyn languages | 11.375 / 9.445 |
| roslyn all | 14.840 / 12.855 |
| aspnetcore optional | 4.920 / 3.570 |
| xml-2gib optional | 0.260 / 0.260 |

The separate Spring scanner profile requires the exact baseline report digest and confirms the hypothesized mechanism. With the same ten-iteration heap-profile protocol, scanner allocation falls from 458,243,695 to 231,691,667 bytes/op (49.4%) and from 2,265,749 to 999,282 allocations/op (55.9%). Cumulative sampled process allocation falls from 5,613.85 to 2,892.44 MB. `bufio.NewReaderSize` falls from 1,437.48 to 34.13 MB. The previously dominant packfile-open/construction frames have no retained CPU samples in the candidate phase-filtered capture; this means below sampling resolution, not literally zero execution cost. Compare [baseline CPU](profiles/baseline-cache-attribution-cpu.txt), [candidate CPU](profiles/candidate-cache-attribution-cpu.txt), [baseline allocations](profiles/baseline-cache-attribution-heap.txt), and [candidate allocations](profiles/candidate-cache-attribution-heap.txt).

Remaining Spring allocation is now dominated by `MemoryObject.Write` (977.58 MB, 33.80%) and `readAllBounded` (776.52 MB, 26.85%). Scheduler wakeups and remaining object reads are prominent CPU costs. These are observations for a future experiment, not additional changes in this one. CPU percentages retain the whole-profile denominator even after the scan-phase filter; heap captures include warmup/calibration/validation. Sampled stacks corroborate the mechanism; the interleaved unprofiled CLI runs support the latency claim.

The frozen baseline and pure cache candidate remain available in the ignored cache. Rollback is the scanner cache activation/lifetime commit; retain the independent maintained-fork close-on-insertion-error correctness fix. Integration with the new CLI and schemas is validated separately by the main preparation workflow. This evidence supports a draft PR and external review, not release approval.

The accepted runtime change is commit `3b9c1c5`; prerequisite fork cleanup is `df1b8cb`. `candidate-scanner-build-clarification.json` identifies an inherited CLI build-info field in the original scanner receipt and supplies the actual scanner build metadata without rewriting the receipt hashed by the profiles. The scanner binary hash, build command and source delta were correct in the original receipt.
