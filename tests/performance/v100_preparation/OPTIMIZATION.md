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

## Directory walk fold (Findings A + B, r100)

Two additional directory-mode changes land alongside the packfile cache: the pre-open `Lstat` in the per-file worker read is skipped when the walker already promised the entry was regular (Finding A, `readBoundedSizeAssumeRegular` at `pkg/scanner/scanner.go`), and the serial `directoryTreeLimit` pre-walk is replaced by inline counting inside the main walk so a limit-below-threshold scan traverses the tree once (Finding B). Correctness for A is protected by `TestReadBoundedSizeAssumeRegularRejectsNonRegular` (`pkg/scanner/read_toctou_test.go`); a final-component swap to a symlink, FIFO or directory is still refused via `O_NOFOLLOW`/`O_NONBLOCK` and the post-open fstat. Correctness for B is protected by three tests in `pkg/scanner/tree_size_stream_test.go`: an unreadable file in a tree above the limit still produces the byte-identical `tree_size_limit` skeleton under the default fail policy (0.8.0 pre-walk ordering); an unreadable file in a tree below the limit still surfaces the read error to the caller; and `--on-error continue` produces the file-read warning below the limit and the skeleton above it.

The streaming walker dispatches jobs to workers as they are discovered rather than buffering them until the walk completes. That preserves the O(N) walk cost (one traversal, not two) without the peak-RSS bump an intermediate buffer would create, and lets worker classification overlap with directory I/O. Two subtleties needed to preserve 0.8.0 semantics:

1. The pre-walk always finished before any file was opened. With streaming, a worker could hit a read failure before the walker knows the tree exceeds the limit. So a per-file worker error under the default fail policy is captured but does NOT cancel the context: the walker keeps counting entries until either it crosses the limit (skeleton wins over the error, matching 0.8.0) or the walk finishes below the limit (the deferred error is surfaced). Once the error is captured, `stopDispatch` prevents further job dispatch and workers drain remaining jobs without processing, so total CPU cost stays bounded by the walk itself.
2. When the walker crosses `MaxTreeSize`, workers may already have contributed to the availability / registry / rules accumulators. The skeleton response is therefore constructed from fresh accumulators (new `newAvailabilityAccumulator`, `newRegistryAccumulator`, `newRulesAccumulator` calls) rather than the possibly-partially-fed ones, so the emitted skeleton matches what 0.8.0's pre-walk (which never opened a file) would have produced.

Byte-identical stdout/stderr/exit vs. locally-built v0.8.0 was verified at `--tree-size` boundaries (limit−1, limit, limit+1) with regular, symlink, FIFO and directory entries adjacent to the boundary, and on the synthetic 50 307-file fixture for `--json --breakdown` at `--workers 1,16`; for `analyze all --json --projects --declarations --metrics --environments --formats` at `--workers 1,16`; for `analyze availability --json` at `--workers 1,16`; and on the review worktree itself at `--workers 1,16`. All lanes matched sha-for-sha on stdout and stderr with exit 0.

Interleaved timing under the same shared macOS host, ten measured pairs per size after two warmup pairs (baseline binary built at PR head prior to the A/B changes; candidate binary built with A+B applied via the streaming walker; `--json --breakdown --workers 16 --source directory --tree-size 1000000` so the limit never fires):

| Fixture | Baseline median wall (s) | Candidate median wall (s) | Delta | Baseline median RSS (MiB) | Candidate median RSS (MiB) | RSS Δ |
|---|---:|---:|---:|---:|---:|---:|
| tree10k (10 000 files)  | 0.22 | 0.15 | −31.8% | 51.5 | 51.2 | −0.6% |
| tree50k (50 000 files)  | 1.10 | 0.75 | −31.8% | 60.5 | 58.6 | −3.2% |
| tree200k (200 000 files) | 4.82 | 3.19 | −33.8% | 73.1 | 73.0 | −0.1% |

At tree200k the host was noticeably contended during the window (baseline wall min/max 4.11/7.47 s; candidate 2.73/6.60 s over 12 measured pairs; the range overlap is host noise, medians are cleanly separated). Peak RSS is within 1% of baseline at every measured size — the streaming walker never buffers a proportional-to-MaxTreeSize slice, so the only growth left in the process image is the OS's own inode/dcache pressure that the pre-1.0 preflight already caused. An earlier buffered-dispatch prototype paid ~+70 MiB peak at 200k (71.5 → 140.5 MiB) for the same wall-time win; the streaming walker replaced it because a first public release should not trade nearly-doubled peak RSS for a −13% wall time. The 500k measurement from `r100/07-profiling.md F1` (a −31.3% wall clock on the same host) is not re-run here; the 10k/50k/200k re-measurement is consistent with what the profiling slice reported at those sizes.
