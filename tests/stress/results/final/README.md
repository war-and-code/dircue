# Synthetic scale acceptance evidence

All fourteen scenarios matched actual Ruby Linguist 9.7.0 in both Git and Git-free modes. Each candidate Git median was faster in this measured environment.

Measured 2026-09-08T03:19:10Z to 2026-09-08T04:59:12Z, with 20 recorded runs per tool after 3 warmups. Timing order rotates across Ruby Git, auragaze Git, and auragaze flat. Inputs reside on the same Docker volume and are mounted read-only; networking is disabled.

The three cutoff cases return an empty result after the tree-size check. Their timings measure that policy check, not classification of 100,000 files. The limit of 100,001 permits the complete 100,000-file scan and is listed as a full traversal.

| Case | Work | Counted bytes | Ruby Git median (ms) | Auragaze Git (ms) | Auragaze flat (ms) | Git speedup |
|---|---|---:|---:|---:|---:|---:|
| etl-pipeline-generated-excluded | Full traversal | 250 | 28,926.99 | 9,711.26 | 2,727.05 | 2.98× |
| etl-pipeline-generated-included | Full traversal | 805,306,618 | 41,982.94 | 9,638.07 | 2,735.82 | 4.36× |
| xml-log-default | Full traversal | 87 | 7,080.54 | 20.02 | 17.77 | 353.64× |
| xml-log-detectable | Full traversal | 1,153,433,688 | 7,072.89 | 20.05 | 18.08 | 352.70× |
| dotnet-graph-default | Full traversal | 2,291,540 | 1,554.39 | 468.75 | 148.48 | 3.32× |
| boundaries-default | Full traversal | 3,538,945 | 368.79 | 28.91 | 18.89 | 12.76× |
| tree-count-default | Cutoff; empty | 0 | 396.35 | 73.42 | 171.26 | 5.40× |
| tree-count-default-limit99999 | Cutoff; empty | 0 | 383.49 | 72.52 | 166.04 | 5.29× |
| tree-count-default-limit100000 | Cutoff; empty | 0 | 381.23 | 73.23 | 167.14 | 5.21× |
| tree-count-default-limit100001 | Full traversal | 1,888,890 | 15,552.46 | 6,132.28 | 1,540.05 | 2.54× |
| etl-pipeline-packed-generated-excluded | Full traversal | 250 | 29,549.20 | 7,890.00 | 2,757.08 | 3.75× |
| etl-pipeline-packed-generated-included | Full traversal | 805,306,618 | 43,043.66 | 8,007.19 | 2,725.30 | 5.38× |
| xml-log-packed-default | Full traversal | 87 | 7,075.65 | 19.96 | 17.67 | 354.44× |
| xml-log-packed-detectable | Full traversal | 1,153,433,688 | 7,087.38 | 20.01 | 18.32 | 354.19× |

Full traversal means visiting the directory/tree entries and applying the bounded classification policy. It does not mean reading or validating every XML byte. Huge XML ratios reflect bounded prefix classification, default XML exclusion where applicable, and avoiding full-blob materialization; they are not full-XML processing throughput.

Empirical p95 uses the nearest-rank sample; CV is population standard deviation divided by mean across these runs. Neither establishes rare-tail behavior or repeat-window stability.

| Case | Ruby Git p95 (ms) | Auragaze Git p95 (ms) | Auragaze flat p95 (ms) | Ruby CV | Git CV | Flat CV |
|---|---:|---:|---:|---:|---:|---:|
| etl-pipeline-generated-excluded | 29,198.32 | 9,934.40 | 2,814.67 | 0.005 | 0.016 | 0.023 |
| etl-pipeline-generated-included | 42,478.05 | 9,862.58 | 2,796.42 | 0.008 | 0.016 | 0.021 |
| xml-log-default | 7,244.92 | 21.30 | 20.33 | 0.011 | 0.039 | 0.059 |
| xml-log-detectable | 7,216.32 | 21.24 | 19.41 | 0.009 | 0.054 | 0.051 |
| dotnet-graph-default | 1,951.89 | 490.62 | 156.94 | 0.105 | 0.039 | 0.022 |
| boundaries-default | 389.64 | 30.04 | 21.58 | 0.023 | 0.020 | 0.064 |
| tree-count-default | 422.27 | 78.24 | 179.92 | 0.050 | 0.034 | 0.035 |
| tree-count-default-limit99999 | 399.39 | 75.16 | 172.01 | 0.022 | 0.017 | 0.019 |
| tree-count-default-limit100000 | 397.60 | 75.06 | 173.08 | 0.019 | 0.016 | 0.022 |
| tree-count-default-limit100001 | 16,072.01 | 6,266.26 | 1,665.55 | 0.111 | 0.012 | 0.028 |
| etl-pipeline-packed-generated-excluded | 29,846.48 | 8,101.99 | 2,865.61 | 0.009 | 0.013 | 0.025 |
| etl-pipeline-packed-generated-included | 43,191.28 | 8,154.03 | 2,817.11 | 0.004 | 0.015 | 0.019 |
| xml-log-packed-default | 7,217.72 | 21.47 | 19.35 | 0.006 | 0.036 | 0.049 |
| xml-log-packed-detectable | 7,332.49 | 21.60 | 19.80 | 0.012 | 0.048 | 0.071 |

Observed variability deserves attention: dotnet-graph-default (Ruby Git): CV 0.105, maximum 2.239 s, median 1.554 s; tree-count-default-limit100001 (Ruby Git): CV 0.111, maximum 23.674 s, median 15.552 s. Every sample, including these slow observations, is retained. The measurements do not identify their cause; no outlier was removed and this single window does not prove stability across repeated windows.

| Case | Ruby Git median CPU (s) | Auragaze Git CPU (s) | Auragaze flat CPU (s) |
|---|---:|---:|---:|
| etl-pipeline-generated-excluded | 28.910 | 19.960 | 13.035 |
| etl-pipeline-generated-included | 41.970 | 19.950 | 13.125 |
| xml-log-default | 7.075 | 0.020 | 0.010 |
| xml-log-detectable | 7.060 | 0.020 | 0.020 |
| dotnet-graph-default | 1.540 | 0.750 | 0.480 |
| boundaries-default | 0.360 | 0.030 | 0.020 |
| tree-count-default | 0.380 | 0.080 | 0.190 |
| tree-count-default-limit99999 | 0.370 | 0.080 | 0.185 |
| tree-count-default-limit100000 | 0.370 | 0.080 | 0.190 |
| tree-count-default-limit100001 | 15.540 | 9.260 | 5.310 |
| etl-pipeline-packed-generated-excluded | 29.535 | 18.115 | 13.160 |
| etl-pipeline-packed-generated-included | 43.030 | 18.080 | 13.045 |
| xml-log-packed-default | 7.065 | 0.020 | 0.020 |
| xml-log-packed-detectable | 7.075 | 0.020 | 0.010 |

CPU is user plus system time reported for the measured child, distinct from elapsed wall time. GNU time reports these CPU counters to hundredths of a second, so short-process CPU ratios would have limited precision.

| Case | Ruby Git peak RSS (MiB) | Auragaze Git (MiB) | Auragaze flat (MiB) |
|---|---:|---:|---:|
| etl-pipeline-generated-excluded | 70.2 | 167.3 | 37.8 |
| etl-pipeline-generated-included | 89.0 | 167.3 | 38.2 |
| xml-log-default | 1767.0 | 19.0 | 18.5 |
| xml-log-detectable | 1767.0 | 19.0 | 18.2 |
| dotnet-graph-default | 70.2 | 41.7 | 28.0 |
| boundaries-default | 70.2 | 21.5 | 20.1 |
| tree-count-default | 70.2 | 26.6 | 21.8 |
| tree-count-default-limit99999 | 70.2 | 26.9 | 21.8 |
| tree-count-default-limit100000 | 70.2 | 26.8 | 21.6 |
| tree-count-default-limit100001 | 209.7 | 126.0 | 45.6 |
| etl-pipeline-packed-generated-excluded | 870.0 | 168.0 | 40.5 |
| etl-pipeline-packed-generated-included | 1333.6 | 168.6 | 38.3 |
| xml-log-packed-default | 1768.5 | 19.0 | 18.3 |
| xml-log-packed-detectable | 1768.5 | 18.9 | 18.3 |

Unique allocated fixture file blocks: 8,163,360,768 bytes. Payloads are fully written and hash-verified; hardlinked flat views share their storage. Checkout bytes, Git-history storage and compressed pack sizes are distinct fields in the raw report.

Candidate SHA-256: `d4c1011d3d615f303cd8d60760c0eeb8d3f234fda74ac2dfd77aa75e4e8ae637`. Production commit: `f13f06841a49f4c862319feb88bc2a7549cee06b`. Fixture generator SHA-256: `1ccdc2094ff0357b728dfa35877ab73b522bb7c6d8b40e2d5561944d742fedf1`. The [raw report](comparison.json) contains every timing sample and the build receipt.

GNU time measures target-child CPU/RSS; wall time includes the same launcher overhead for every tool. Caches are warm, and fixture verification reads all unique payloads before profiling. Prefix reads are valid: logical source size is not physical bytes read during a scan. Twenty samples do not establish rare tails.

These are explicitly synthetic layouts and formats, not verified ETL-pipeline exports or log-transfer samples. The .NET graph is never built or restored. The results establish bounded compatibility and performance evidence for these inputs and this environment, not a guarantee for other repositories, histories, machines or future Linguist versions. See the [methodology and reproduction commands](../../README.md).


The [fixture manifest](fixture-manifest.json.gz) retains every generated path, byte size, SHA-256 and Git blob ID. The [correctness outputs](correctness-artifacts.tar.gz) retain stdout, stderr, exit status and diagnostic resources, including `analyze all`. Both archives have deterministic metadata; [archive hashes](archive-sha256.json) permit integrity checks.

To extract and independently re-run this audit from the repository root:

```sh
mkdir -p .cache/stress-reaudit/comparison-details
cp tests/stress/results/final/comparison.json .cache/stress-reaudit/comparison.json
gzip -dc tests/stress/results/final/fixture-manifest.json.gz > .cache/stress-reaudit/manifest.json
tar -xzf tests/stress/results/final/correctness-artifacts.tar.gz -C .cache/stress-reaudit/comparison-details
python3 tests/stress/record.py --report .cache/stress-reaudit/comparison.json --fixture-manifest .cache/stress-reaudit/manifest.json --output .cache/stress-reaudit/verified
```

The original build receipt is preserved. [Packaging-source binding](packaging-source-binding.json) verifies all 79 recorded source files across the packaging-only descendant; the measured binary was reused without a rebuild. [Independent final replay](independent-audit.json) records fresh archive extraction and recorder verification.

These legacy Linguist suites use a fixed 20-run design. Some fast cells total less than 10 measured seconds, as listed in the audit receipt; their ratios are observed medians and do not satisfy the later Enry/library cumulative-duration criterion. No outliers were removed or extra rounds selectively added.
