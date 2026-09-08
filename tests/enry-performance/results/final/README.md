# Final Enry comparison evidence

The final maintained classifier is faster on both declared preloaded library populations. Repository CLI results are separately scoped: selection, language data and output work differ from the official Enry CLI. Every Auragaze directory result still matches its pinned Ruby repository contract. These are measured workloads on one Docker Desktop environment, not universal superiority.

| Input | Library | Warm median / p95 (s) | First pass (s) | Whole process median / p95 (s) | Bytes / allocations per call | Peak RSS (MiB) |
|---|---|---:|---:|---:|---:|---:|
| full | official | 0.9756 / 0.9881 | 0.9840 | 2.0524 / 2.0774 | 20,739 / 106.6 | 104.0 |
| full | maintained | 0.5945 / 0.6009 | 0.6014 | 1.2859 / 1.3002 | 11,732 / 165.7 | 90.5 |
| prefix | official | 0.9205 / 0.9271 | 0.9334 | 1.9421 / 1.9487 | 20,773 / 106.5 | 93.5 |
| prefix | maintained | 0.5382 / 0.5582 | 0.5436 | 1.1579 / 1.2409 | 11,807 / 165.8 | 80.2 |

full: official/maintained warm median ratio **1.641×**, paired 95% interval [1.629, 1.647]; faster with margin. Whole-process ratio 1.596×.


prefix: official/maintained warm median ratio **1.710×**, paired 95% interval [1.696, 1.728]; faster with margin. Whole-process ratio 1.677×.


Both libraries use the identical driver and all 3,388 input files; 20 paired measured processes per cell follow three warmups and exceed 10 cumulative warm seconds. Warm timers exclude preload, lazy initialization and JSON output; the first-pass and whole-process costs above remain separate. Allocation counts can be higher even when byte volume and runtime are lower. Every full-content maintained label matches actual Linguist 9.7.0; official v2.9.6 matches 3,241, and all 147 differences remain in the raw report. Prefix labels equal their respective full-content maps here, but there is no independent Ruby prefix oracle.

The CLI matrix covers all 11 pinned public repositories and 14 stress variants. Of 22 measured cases, only six have equal path-to-language maps across all tools; sixteen differ in inclusion or labels. The official CLI comparison has 19 faster and three inconclusive outcomes; refreshed default and 128 KiB variants have 22 faster outcomes each. These are scoped same-input end-to-end observations where work differs. The 22 measured rows comprise 11 public repositories and 11 stress variants, not 22 independent workloads. Packed/unpacked stress pairs repeat equivalent flat directory bytes in this CLI matrix and do not measure Git pack handling. Ratios are not averaged and mismatch counts are not summed across variants. “Official” is unmodified Enry CLI v1.3.0 with library v2.8.4; “refreshed” is the identical CLI source with official v2.9.6; “128K” additionally changes its read window to 128 KiB. Auragaze uses directory mode on the same raw Git blob materialization. “Different” means file inclusion or language labels differ; these ratios then compare observed end-to-end workloads, not equivalent classification work.

| Case | Auragaze median / p95 (ms) | Official ratio / verdict / labels | Refreshed ratio / verdict / labels | 128K ratio / verdict / labels |
|---|---:|---|---|---|
| public-cobra | 18.00 / 19.31 | 1.41× / faster / same | 1.43× / faster / same | 1.43× / faster / same |
| public-flask | 23.09 / 25.05 | 1.67× / faster / different | 1.27× / faster / different | 1.27× / faster / different |
| public-express | 22.40 / 23.87 | 1.50× / faster / same | 1.31× / faster / same | 1.31× / faster / same |
| public-ripgrep | 28.13 / 30.36 | 2.23× / faster / different | 1.42× / faster / different | 1.43× / faster / different |
| public-rails | 158.83 / 164.20 | 8.06× / faster / different | 2.62× / faster / different | 2.62× / faster / different |
| public-jq | 32.65 / 36.50 | 2.25× / faster / different | 1.60× / faster / different | 1.60× / faster / different |
| public-typescript | 2366.10 / 2385.32 | 11.41× / faster / different | 3.69× / faster / different | 3.57× / faster / different |
| public-laravel | 106.57 / 110.94 | 7.86× / faster / different | 2.82× / faster / different | 2.54× / faster / different |
| public-spring-framework | 561.00 / 576.65 | 11.00× / faster / different | 3.18× / faster / different | 3.19× / faster / different |
| public-roslyn | 1698.15 / 1918.74 | 12.52× / faster / different | 3.67× / faster / different | 3.60× / faster / different |
| public-aspnetcore | 655.60 / 683.49 | 9.88× / faster / different | 3.10× / faster / different | 3.00× / faster / different |
| stress-talend-generated-excluded | 2763.30 / 2902.02 | 2.61× / faster / different | 2.08× / faster / different | 2.51× / faster / different |
| stress-talend-generated-included | 2759.88 / 2871.95 | 2.64× / faster / different | 2.12× / faster / different | 2.52× / faster / different |
| stress-xml-log-default | 17.69 / 19.13 | 1.09× / inconclusive / same | 1.31× / faster / same | 1.31× / faster / same |
| stress-xml-log-detectable | 17.58 / 18.76 | 1.10× / inconclusive / different | 1.32× / faster / different | 1.32× / faster / different |
| stress-dotnet-graph-default | 150.21 / 156.60 | 8.25× / faster / same | 2.17× / faster / same | 2.10× / faster / same |
| stress-boundaries-default | 18.29 / 20.61 | 1.71× / faster / different | 1.90× / faster / different | 1.66× / faster / different |
| stress-tree-count-default | Unmeasured cutoff/empty work | Advisory | Advisory | Advisory |
| stress-tree-count-default-limit99999 | Unmeasured cutoff/empty work | Advisory | Advisory | Advisory |
| stress-tree-count-default-limit100000 | Unmeasured cutoff/empty work | Advisory | Advisory | Advisory |
| stress-tree-count-default-limit100001 | 1538.77 / 1587.06 | 8.82× / faster / same | 2.89× / faster / same | 2.89× / faster / same |
| stress-talend-packed-generated-excluded | 2779.96 / 2870.00 | 2.63× / faster / different | 2.07× / faster / different | 2.50× / faster / different |
| stress-talend-packed-generated-included | 2779.01 / 2899.75 | 2.63× / faster / different | 2.13× / faster / different | 2.56× / faster / different |
| stress-xml-log-packed-default | 18.14 / 21.83 | 1.10× / faster / same | 1.32× / faster / same | 1.33× / faster / same |
| stress-xml-log-packed-detectable | 17.72 / 19.54 | 1.09× / inconclusive / different | 1.32× / faster / different | 1.32× / faster / different |

All 22 nonadvisory cells retain at least 20 balanced rounds, three warmups, and 10 measured seconds per tool. Fast cases extend symmetrically under the declared cap; all observations are retained. Three 100,000-file cutoff cases retain diagnostics only. The limit of 100,001 allows the full scan and remains measured. A faster verdict requires a 10% median margin, a paired interval excluding parity and no p95 regression. All confidence intervals and missing/extra/reclassified paths are retained in `audit.json` and the raw archive.

| Case | Peak RSS MiB: official / refreshed / 128K / Auragaze | CV: official / refreshed / 128K / Auragaze |
|---|---:|---:|
| public-cobra | 24.8 / 28.8 / 28.9 / 21.4 | 0.093 / 0.093 / 0.117 / 0.159 |
| public-flask | 24.9 / 28.6 / 28.9 / 22.8 | 0.035 / 0.050 / 0.064 / 0.063 |
| public-express | 25.6 / 29.5 / 29.4 / 22.1 | 0.022 / 0.023 / 0.025 / 0.041 |
| public-ripgrep | 27.9 / 32.4 / 33.0 / 34.0 | 0.015 / 0.022 / 0.021 / 0.039 |
| public-rails | 36.8 / 44.0 / 44.6 / 36.0 | 0.012 / 0.014 / 0.015 / 0.020 |
| public-jq | 27.4 / 31.9 / 32.2 / 33.1 | 0.015 / 0.018 / 0.045 / 0.065 |
| public-typescript | 61.0 / 70.6 / 69.1 / 93.6 | 0.004 / 0.005 / 0.005 / 0.006 |
| public-laravel | 45.8 / 52.1 / 43.5 / 40.3 | 0.049 / 0.093 / 0.015 / 0.023 |
| public-spring-framework | 42.5 / 52.3 / 52.7 / 49.5 | 0.002 / 0.006 / 0.008 / 0.014 |
| public-roslyn | 57.3 / 66.8 / 63.9 / 69.4 | 0.028 / 0.019 / 0.018 / 0.050 |
| public-aspnetcore | 43.3 / 53.0 / 51.8 / 49.2 | 0.016 / 0.021 / 0.026 / 0.019 |
| stress-talend-generated-excluded | 59.0 / 75.6 / 46.8 / 38.5 | 0.050 / 0.069 / 0.013 / 0.028 |
| stress-talend-generated-included | 58.9 / 70.4 / 46.9 / 40.6 | 0.072 / 0.046 / 0.004 / 0.021 |
| stress-xml-log-default | 23.3 / 27.5 / 27.6 / 18.8 | 0.035 / 0.053 / 0.063 / 0.051 |
| stress-xml-log-detectable | 23.4 / 27.5 / 27.6 / 18.7 | 0.035 / 0.043 / 0.062 / 0.038 |
| stress-dotnet-graph-default | 37.9 / 42.1 / 36.5 / 29.9 | 0.020 / 0.042 / 0.017 / 0.022 |
| stress-boundaries-default | 32.1 / 36.4 / 30.7 / 20.6 | 0.490 / 0.418 / 0.053 / 0.054 |
| stress-tree-count-default-limit100001 | 68.0 / 78.1 / 79.5 / 44.7 | 0.006 / 0.011 / 0.008 / 0.014 |
| stress-talend-packed-generated-excluded | 57.6 / 70.5 / 46.9 / 44.1 | 0.056 / 0.055 / 0.005 / 0.020 |
| stress-talend-packed-generated-included | 57.6 / 69.9 / 46.9 / 41.1 | 0.022 / 0.047 / 0.011 / 0.027 |
| stress-xml-log-packed-default | 23.3 / 27.6 / 27.5 / 18.6 | 0.094 / 0.124 / 0.104 / 0.145 |
| stress-xml-log-packed-detectable | 23.3 / 27.5 / 27.6 / 18.7 | 0.046 / 0.039 / 0.043 / 0.051 |

RSS is the entire child process; CPU, observed maxima, allocation deltas and empirical p95 remain in the audit. TypeScript uses 32.5% more peak RSS than refreshed Enry and 35.5% more than its 128 KiB variant; smaller peak-only losses also occur for ripgrep, jq and Roslyn. These memory costs remain part of the result. Higher RSS and variability are not hidden by a faster median. Twenty samples do not characterize extreme-tail latency; this single window does not demonstrate three-window stability. Docker virtualization, warm/uncontrolled caches and uncontrolled governor/turbo/SMT are explicit waivers. Synthetic Talend shapes are not verified Talend exports.

The release binary is `d4c1011d3d615f303cd8d60760c0eeb8d3f234fda74ac2dfd77aa75e4e8ae637`. Its original build receipt identifies `f13f06841a49f4c862319feb88bc2a7549cee06b`; a preserved 79-file equality binding identifies packaging-only descendant `e2f9ae7035e087062afe55c8335e3f43569a1d8c`. The binary was reused, not represented as a new build. All three official baseline binaries and the shared driver match the prior experiment byte-for-byte. Historical RC1 results remain unchanged.

## Offline evidence replay

`evidence.tar.gz` contains the raw three reports, all 129 correctness artifacts, source/model/build and input manifests, sample oracle, original receipts, execution order and harness snapshots. `audit.json` independently recomputes every retained distribution, paired interval, output difference and byte total. No benchmark, network or compiler is invoked by replay. The repository supplies the pinned audit helpers, whose archived hashes are checked.

```sh
python3 tests/enry-performance/record_final.py --audit-only
```

The complementary final Git pipeline results are [11 public repositories](../../../performance/results/final/README.md) and [14 stress variants](../../../stress/results/final/README.md). Those compare the same release binary with actual Ruby Linguist on Git snapshots.
