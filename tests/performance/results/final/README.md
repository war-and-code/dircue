# v0.1 performance evidence

All **11 pinned repositories matched github-linguist 9.7.0 exactly** for language names, byte totals, percentage strings, and file sets. Every Auragaze median was faster. This report is generated from completed raw evidence by `tests/performance/record.py`, which independently checks timing summaries, CPU usage, throughput, and peak RSS against all recorded samples and reopens every correctness artifact.

Measured 2026-09-08T02:13:32Z to 2026-09-08T03:19:10Z; 20 runs per tool per repository after 3 warmups. Both complete CLI processes ran in the same Linux aarch64 container with the same read-only corpus on a Docker volume. Host: Apple M1 Max. Other project builds and tests were paused. The host scheduler and unrelated system activity were not controlled.

| Project / release | Tracked files | Linguist median (ms) | Auragaze median (ms) | Speedup |
|---|---:|---:|---:|---:|
| cobra v1.10.2 | 66 | 652.6 | 44.5 | 14.66× |
| flask 3.1.2 | 234 | 463.8 | 51.7 | 8.98× |
| express v5.1.0 | 225 | 492.3 | 45.7 | 10.77× |
| ripgrep 14.1.1 | 213 | 898.2 | 64.1 | 14.02× |
| rails v8.0.2 | 4,761 | 4,908.8 | 596.4 | 8.23× |
| jq jq-1.8.1 | 366 | 777.7 | 69.6 | 11.18× |
| typescript v5.8.3 | 73,637 | 44,390.7 | 8,248.7 | 5.38× |
| laravel v12.0.0 | 2,778 | 3,463.4 | 369.8 | 9.37× |
| spring-framework v7.0.8 | 11,348 | 17,777.6 | 1,780.1 | 9.99× |
| roslyn main snapshot 2026-09-07 | 35,220 | 49,650.7 | 7,046.9 | 7.05× |
| aspnetcore v10.0.0 | 16,517 | 18,644.0 | 2,271.2 | 8.21× |

| Project | Linguist p95 (ms) | Auragaze p95 (ms) | Linguist peak RSS (MiB) | Auragaze peak RSS (MiB) |
|---|---:|---:|---:|---:|
| cobra | 688.7 | 53.3 | 90.0 | 23.0 |
| flask | 533.6 | 59.3 | 70.2 | 26.0 |
| express | 537.4 | 56.4 | 70.2 | 25.6 |
| ripgrep | 953.4 | 69.3 | 91.3 | 37.3 |
| rails | 5,170.8 | 621.8 | 98.8 | 69.5 |
| jq | 808.5 | 80.4 | 91.9 | 37.5 |
| typescript | 46,019.1 | 8,712.7 | 250.0 | 420.2 |
| laravel | 3,484.2 | 384.8 | 102.7 | 62.9 |
| spring-framework | 18,015.6 | 1,815.2 | 111.8 | 154.8 |
| roslyn | 50,015.5 | 7,228.6 | 223.3 | 345.3 |
| aspnetcore | 18,916.4 | 2,304.2 | 122.2 | 206.6 |

| Project | Linguist median CPU (s) | Auragaze median CPU (s) |
|---|---:|---:|
| cobra | 0.645 | 0.040 |
| flask | 0.450 | 0.070 |
| express | 0.480 | 0.060 |
| ripgrep | 0.885 | 0.100 |
| rails | 4.895 | 0.940 |
| jq | 0.765 | 0.120 |
| typescript | 44.375 | 14.790 |
| laravel | 3.450 | 0.610 |
| spring-framework | 17.765 | 3.070 |
| roslyn | 49.635 | 12.530 |
| aspnetcore | 18.630 | 3.910 |

CPU is aggregate child-process user plus system time, distinct from elapsed time. Parallel execution can consume multiple CPU seconds per wall-clock second. GNU time records these counters to hundredths of a second, limiting precision for short scans. Wall time, CPU cost, and peak memory are separate comparisons.

Runtime variability exceeded 10% coefficient of variation for cobra/auragaze (16.0% CV), jq/auragaze (19.1% CV). All observations, including outliers, are retained. Interpret medians alongside p95 and the raw maximums.

Candidate SHA-256: `d4c1011d3d615f303cd8d60760c0eeb8d3f234fda74ac2dfd77aa75e4e8ae637`. Production source commit: `f13f06841a49f4c862319feb88bc2a7549cee06b`. The build receipt confirms no production changes from that commit and records compiler/module details and source hashes. Release archives identify their executable hashes separately, allowing comparison with this candidate.

[Raw timings and environment](comparison.json) include every recorded run, source commits, output hashes, CPU time, RSS, variation, throughput, and quantiles. [Build receipt](build-receipt.json) identifies the binary and reference image. [Validated raw correctness outputs](correctness-artifacts.tar.gz) are retained with deterministic archive metadata (SHA-256 `3ae098eb8d2301d66e31be6df716fbdba6d7c48ba42bc894e206bf1542fae989`); inspect them with `tar -xzf correctness-artifacts.tar.gz`.

These are warm-cache measurements on these specific releases. They establish the measured compatibility and performance result, not universal equivalence or a performance guarantee for another repository, platform, filesystem, or Linguist version. Twenty runs do not estimate rare-tail latency: p99 and higher fields in the JSON are conservative observed order statistics. Framework/ecosystem profiling and cold-cache behavior are outside this benchmark.

The original build receipt is preserved. [Packaging-source binding](packaging-source-binding.json) verifies all 79 recorded source files across the packaging-only descendant; the measured binary was reused without a rebuild. [Independent final replay](independent-audit.json) records fresh archive extraction and recorder verification.

These legacy Linguist suites use a fixed 20-run design. Some fast cells total less than 10 measured seconds, as listed in the audit receipt; their ratios are observed medians and do not satisfy the later Enry/library cumulative-duration criterion. No outliers were removed or extra rounds selectively added.
