# v0.1 performance evidence

All **11 pinned repositories matched github-linguist 9.7.0 exactly** for language names, byte totals, percentage strings, and file sets. Every Auragaze median was faster. This report is generated from completed raw evidence by `tests/performance/record.py`, which independently checks timing summaries, CPU usage, throughput, and peak RSS against all recorded samples and reopens every correctness artifact.

Measured 2026-09-07T17:52:52Z to 2026-09-07T18:58:37Z; 20 runs per tool per repository after 3 warmups. Both complete CLI processes ran in the same Linux aarch64 container with the same read-only corpus on a Docker volume. Host: Apple M1 Max. Other project builds and tests were paused. The host scheduler and unrelated system activity were not controlled.

| Project / release | Tracked files | Linguist median (ms) | Auragaze median (ms) | Speedup |
|---|---:|---:|---:|---:|
| cobra v1.10.2 | 66 | 630.9 | 40.6 | 15.55× |
| flask 3.1.2 | 234 | 453.3 | 51.1 | 8.87× |
| express v5.1.0 | 225 | 474.2 | 46.3 | 10.23× |
| ripgrep 14.1.1 | 213 | 876.2 | 137.7 | 6.36× |
| rails v8.0.2 | 4,761 | 4,867.8 | 612.9 | 7.94× |
| jq jq-1.8.1 | 366 | 749.0 | 147.9 | 5.06× |
| typescript v5.8.3 | 73,637 | 44,486.4 | 8,259.7 | 5.39× |
| laravel v12.0.0 | 2,778 | 3,511.6 | 407.5 | 8.62× |
| spring-framework v7.0.8 | 11,348 | 17,758.0 | 1,865.2 | 9.52× |
| roslyn main snapshot 2026-09-07 | 35,220 | 49,854.6 | 7,208.6 | 6.92× |
| aspnetcore v10.0.0 | 16,517 | 18,706.6 | 2,316.7 | 8.07× |

| Project | Linguist p95 (ms) | Auragaze p95 (ms) | Linguist peak RSS (MiB) | Auragaze peak RSS (MiB) |
|---|---:|---:|---:|---:|
| cobra | 741.2 | 46.5 | 89.9 | 24.1 |
| flask | 472.7 | 53.1 | 70.2 | 27.4 |
| express | 502.9 | 47.6 | 70.2 | 28.6 |
| ripgrep | 927.8 | 277.4 | 91.1 | 48.2 |
| rails | 4,955.0 | 633.1 | 98.9 | 72.3 |
| jq | 773.3 | 178.9 | 91.9 | 51.6 |
| typescript | 45,333.8 | 8,513.7 | 253.6 | 445.3 |
| laravel | 3,582.8 | 485.0 | 102.8 | 77.1 |
| spring-framework | 18,179.4 | 1,920.3 | 111.8 | 169.5 |
| roslyn | 50,923.2 | 7,396.1 | 221.7 | 350.9 |
| aspnetcore | 19,157.1 | 2,425.2 | 123.5 | 214.2 |

| Project | Linguist median CPU (s) | Auragaze median CPU (s) |
|---|---:|---:|
| cobra | 0.620 | 0.040 |
| flask | 0.450 | 0.070 |
| express | 0.460 | 0.060 |
| ripgrep | 0.870 | 0.200 |
| rails | 4.855 | 0.990 |
| jq | 0.740 | 0.245 |
| typescript | 44.475 | 15.305 |
| laravel | 3.500 | 0.765 |
| spring-framework | 17.745 | 3.425 |
| roslyn | 49.845 | 12.960 |
| aspnetcore | 18.695 | 4.075 |

CPU is aggregate child-process user plus system time, distinct from elapsed time. Parallel execution can consume multiple CPU seconds per wall-clock second. GNU time records these counters to hundredths of a second, limiting precision for short scans. Wall time, CPU cost, and peak memory are separate comparisons.

Runtime variability exceeded 10% coefficient of variation for cobra/auragaze (13.4% CV), ripgrep/auragaze (29.1% CV). All observations, including outliers, are retained. Interpret medians alongside p95 and the raw maximums.

Candidate SHA-256: `618f5210478a286cc150bef7acc959de54b60b446208400ac49a28264fc67fd2`. Production source commit: `e70d865e38aed9713cd5692bd9630000b1058b71`. The build receipt confirms no production changes from that commit and records compiler/module details and source hashes. Release archives identify their executable hashes separately, allowing comparison with this candidate.

[Raw timings and environment](comparison.json) include every recorded run, source commits, output hashes, CPU time, RSS, variation, throughput, and quantiles. [Build receipt](build-receipt.json) identifies the binary and reference image. [Validated raw correctness outputs](correctness-artifacts.tar.gz) are retained with deterministic archive metadata (SHA-256 `3ae098eb8d2301d66e31be6df716fbdba6d7c48ba42bc894e206bf1542fae989`); inspect them with `tar -xzf correctness-artifacts.tar.gz`.

These are warm-cache measurements on these specific releases. They establish the measured compatibility and performance result, not universal equivalence or a performance guarantee for another repository, platform, filesystem, or Linguist version. Twenty runs do not estimate rare-tail latency: p99 and higher fields in the JSON are conservative observed order statistics. Framework/ecosystem profiling and cold-cache behavior are outside this benchmark.
