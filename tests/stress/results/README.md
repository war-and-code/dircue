# Synthetic scale acceptance evidence

All fourteen scenarios matched actual Ruby Linguist 9.7.0 in both Git and Git-free modes. Each candidate Git median was faster in this measured environment.

Measured 2026-09-07T18:58:37Z to 2026-09-07T20:38:55Z, with 20 recorded runs per tool after 3 warmups. Timing order rotates across Ruby Git, auragaze Git, and auragaze flat. Inputs reside on the same Docker volume and are mounted read-only; networking is disabled.

The three cutoff cases return an empty result after the tree-size check. Their timings measure that policy check, not classification of 100,000 files. The limit of 100,001 permits the complete 100,000-file scan and is listed as a full traversal.

| Case | Work | Counted bytes | Ruby Git median (ms) | Auragaze Git (ms) | Auragaze flat (ms) | Git speedup |
|---|---|---:|---:|---:|---:|---:|
| etl-pipeline-generated-excluded | Full traversal | 250 | 28,853.54 | 9,510.69 | 2,734.62 | 3.03× |
| etl-pipeline-generated-included | Full traversal | 805,306,618 | 41,758.26 | 9,621.61 | 2,705.94 | 4.34× |
| xml-log-default | Full traversal | 87 | 7,072.85 | 18.78 | 16.67 | 376.62× |
| xml-log-detectable | Full traversal | 1,153,433,688 | 7,067.67 | 18.94 | 16.49 | 373.11× |
| dotnet-graph-default | Full traversal | 2,291,540 | 1,560.40 | 472.99 | 150.15 | 3.30× |
| boundaries-default | Full traversal | 3,538,945 | 372.83 | 28.03 | 18.35 | 13.30× |
| tree-count-default | Cutoff; empty | 0 | 379.16 | 71.14 | 157.66 | 5.33× |
| tree-count-default-limit99999 | Cutoff; empty | 0 | 379.77 | 71.17 | 158.66 | 5.34× |
| tree-count-default-limit100000 | Cutoff; empty | 0 | 378.55 | 70.86 | 158.26 | 5.34× |
| tree-count-default-limit100001 | Full traversal | 1,888,890 | 15,635.52 | 6,088.95 | 1,509.70 | 2.57× |
| etl-pipeline-packed-generated-excluded | Full traversal | 250 | 30,019.96 | 7,952.59 | 2,791.87 | 3.77× |
| etl-pipeline-packed-generated-included | Full traversal | 805,306,618 | 44,167.78 | 8,019.69 | 2,843.82 | 5.51× |
| xml-log-packed-default | Full traversal | 87 | 7,064.27 | 18.99 | 16.20 | 371.98× |
| xml-log-packed-detectable | Full traversal | 1,153,433,688 | 7,062.22 | 18.91 | 16.28 | 373.41× |

Empirical p95 uses the nearest-rank sample; CV is population standard deviation divided by mean across these runs. Neither establishes rare-tail behavior or repeat-window stability.

| Case | Ruby Git p95 (ms) | Auragaze Git p95 (ms) | Auragaze flat p95 (ms) | Ruby CV | Git CV | Flat CV |
|---|---:|---:|---:|---:|---:|---:|
| etl-pipeline-generated-excluded | 28,959.36 | 9,713.83 | 2,828.20 | 0.002 | 0.015 | 0.021 |
| etl-pipeline-generated-included | 41,968.07 | 9,783.37 | 2,822.79 | 0.003 | 0.013 | 0.021 |
| xml-log-default | 7,181.60 | 19.60 | 17.38 | 0.008 | 0.032 | 0.044 |
| xml-log-detectable | 7,148.12 | 20.28 | 17.50 | 0.006 | 0.047 | 0.035 |
| dotnet-graph-default | 1,592.19 | 501.70 | 154.83 | 0.012 | 0.031 | 0.019 |
| boundaries-default | 389.79 | 29.31 | 19.53 | 0.033 | 0.021 | 0.051 |
| tree-count-default | 393.44 | 73.26 | 165.54 | 0.017 | 0.014 | 0.023 |
| tree-count-default-limit99999 | 397.16 | 72.55 | 162.23 | 0.020 | 0.013 | 0.013 |
| tree-count-default-limit100000 | 392.63 | 72.13 | 163.69 | 0.019 | 0.021 | 0.020 |
| tree-count-default-limit100001 | 16,505.54 | 6,229.52 | 1,556.15 | 0.111 | 0.013 | 0.016 |
| etl-pipeline-packed-generated-excluded | 30,941.47 | 8,109.54 | 3,023.78 | 0.016 | 0.015 | 0.035 |
| etl-pipeline-packed-generated-included | 45,024.73 | 8,334.38 | 3,013.87 | 0.019 | 0.022 | 0.035 |
| xml-log-packed-default | 7,167.83 | 19.80 | 16.81 | 0.008 | 0.053 | 0.024 |
| xml-log-packed-detectable | 7,251.08 | 19.69 | 17.53 | 0.008 | 0.026 | 0.036 |

Observed variability deserves attention: tree-count-default-limit100001 (Ruby Git): CV 0.111, maximum 23.832 s, median 15.636 s. Every sample, including these slow observations, is retained. The measurements do not identify their cause; no outlier was removed and this single window does not prove stability across repeated windows.

| Case | Ruby Git median CPU (s) | Auragaze Git CPU (s) | Auragaze flat CPU (s) |
|---|---:|---:|---:|
| etl-pipeline-generated-excluded | 28.840 | 19.725 | 13.120 |
| etl-pipeline-generated-included | 41.745 | 19.735 | 12.995 |
| xml-log-default | 7.060 | 0.020 | 0.020 |
| xml-log-detectable | 7.050 | 0.020 | 0.010 |
| dotnet-graph-default | 1.550 | 0.755 | 0.480 |
| boundaries-default | 0.360 | 0.030 | 0.020 |
| tree-count-default | 0.370 | 0.080 | 0.180 |
| tree-count-default-limit99999 | 0.370 | 0.080 | 0.180 |
| tree-count-default-limit100000 | 0.370 | 0.080 | 0.180 |
| tree-count-default-limit100001 | 15.620 | 9.260 | 5.250 |
| etl-pipeline-packed-generated-excluded | 30.010 | 18.140 | 13.340 |
| etl-pipeline-packed-generated-included | 44.155 | 18.355 | 13.600 |
| xml-log-packed-default | 7.050 | 0.020 | 0.010 |
| xml-log-packed-detectable | 7.050 | 0.020 | 0.010 |

CPU is user plus system time reported for the measured child, distinct from elapsed wall time. GNU time reports these CPU counters to hundredths of a second, so short-process CPU ratios would have limited precision.

| Case | Ruby Git peak RSS (MiB) | Auragaze Git (MiB) | Auragaze flat (MiB) |
|---|---:|---:|---:|
| etl-pipeline-generated-excluded | 70.2 | 165.9 | 37.5 |
| etl-pipeline-generated-included | 88.9 | 165.3 | 42.1 |
| xml-log-default | 1766.9 | 19.1 | 18.0 |
| xml-log-detectable | 1766.9 | 18.9 | 17.9 |
| dotnet-graph-default | 70.2 | 40.5 | 27.6 |
| boundaries-default | 70.2 | 21.3 | 20.0 |
| tree-count-default | 70.2 | 26.2 | 21.4 |
| tree-count-default-limit99999 | 70.2 | 26.4 | 21.5 |
| tree-count-default-limit100000 | 70.2 | 26.5 | 21.4 |
| tree-count-default-limit100001 | 209.7 | 124.2 | 45.3 |
| etl-pipeline-packed-generated-excluded | 869.9 | 166.4 | 38.5 |
| etl-pipeline-packed-generated-included | 1333.6 | 166.9 | 38.0 |
| xml-log-packed-default | 1766.9 | 18.8 | 17.7 |
| xml-log-packed-detectable | 1766.9 | 19.0 | 18.0 |

Unique allocated fixture file blocks: 8,163,360,768 bytes. Payloads are fully written and hash-verified; hardlinked flat views share their storage. Checkout bytes, Git-history storage and compressed pack sizes are distinct fields in the raw report.

Candidate SHA-256: `618f5210478a286cc150bef7acc959de54b60b446208400ac49a28264fc67fd2`. Production commit: `e70d865e38aed9713cd5692bd9630000b1058b71`. Fixture generator SHA-256: `1ccdc2094ff0357b728dfa35877ab73b522bb7c6d8b40e2d5561944d742fedf1`. The [raw report](comparison.json) contains every timing sample and the build receipt.

GNU time measures target-child CPU/RSS; wall time includes the same launcher overhead for every tool. Caches are warm, and fixture verification reads all unique payloads before profiling. Prefix reads are valid: logical source size is not physical bytes read during a scan. Twenty samples do not establish rare tails.

These are explicitly synthetic layouts and formats, not verified ETL-pipeline exports or log-transfer samples. The .NET graph is never built or restored. The results establish bounded compatibility and performance evidence for these inputs and this environment, not a guarantee for other repositories, histories, machines or future Linguist versions. See the [methodology and reproduction commands](../README.md).


The [fixture manifest](fixture-manifest.json.gz) retains every generated path, byte size, SHA-256 and Git blob ID. The [correctness outputs](correctness-artifacts.tar.gz) retain stdout, stderr, exit status and diagnostic resources, including `analyze all`. Both archives have deterministic metadata; [archive hashes](archive-sha256.json) permit integrity checks.

To extract and independently re-run this audit from the repository root:

```sh
mkdir -p .cache/stress-reaudit/comparison-details
cp tests/stress/results/comparison.json .cache/stress-reaudit/comparison.json
gzip -dc tests/stress/results/fixture-manifest.json.gz > .cache/stress-reaudit/manifest.json
tar -xzf tests/stress/results/correctness-artifacts.tar.gz -C .cache/stress-reaudit/comparison-details
python3 tests/stress/record.py --report .cache/stress-reaudit/comparison.json --fixture-manifest .cache/stress-reaudit/manifest.json --output .cache/stress-reaudit/verified
```
