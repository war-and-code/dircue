# Paired scanner experiment

Baseline and candidate ran adjacently with reversed tool order each round. Every full result digest matched RC1. Scan, external process and allocation costs have different scopes.

| Scenario | Tool | Samples | Scan median / p95 (s) | Scan CV | Process median / p95 (s) | Allocated bytes / count per scan | Peak RSS (MiB) |
|---|---|---:|---:|---:|---:|---:|---:|
| ripgrep-directory-w1 | baseline | 100 | 0.132652 / 0.250612 | 0.285 | 0.159754 / 0.283847 | 43,614,480 / 218,793 | 40.9 |
| ripgrep-directory-w1 | candidate | 100 | 0.133890 / 0.232809 | 0.381 | 0.159814 / 0.257988 | 41,325,936 / 218,708 | 40.9 |
| jq-directory-w1 | baseline | 100 | 0.156439 / 0.238012 | 0.220 | 0.182868 / 0.268332 | 46,692,320 / 231,754 | 42.4 |
| jq-directory-w1 | candidate | 100 | 0.148547 / 0.258873 | 0.247 | 0.174914 / 0.290834 | 42,146,716 / 231,654 | 40.8 |
| roslyn-git-w5 | baseline | 20 | 7.480478 / 7.726560 | 0.023 | 7.524058 / 7.771006 | 2,528,089,016 / 8,089,168 | 347.5 |
| roslyn-git-w5 | candidate | 20 | 7.330955 / 7.536627 | 0.018 | 7.373056 / 7.580544 | 2,232,674,120 / 8,089,427 | 347.4 |

| Scenario | Metric | Baseline/candidate ratio | Paired 95% interval | Verdict |
|---|---|---:|---|---|
| ripgrep-directory-w1 | scan | 0.991 | [0.941, 1.047] | no speed verdict |
| ripgrep-directory-w1 | process | 1.000 | [0.953, 1.036] | no speed verdict |
| ripgrep-directory-w1 | bytes | 1.055 | [1.024, 1.058] | no speed verdict |
| ripgrep-directory-w1 | allocations | 1.000 | [1.000, 1.001] | no speed verdict |
| jq-directory-w1 | scan | 1.053 | [1.009, 1.086] | no speed verdict |
| jq-directory-w1 | process | 1.045 | [1.013, 1.074] | no speed verdict |
| jq-directory-w1 | bytes | 1.108 | [1.092, 1.110] | no speed verdict |
| jq-directory-w1 | allocations | 1.000 | [1.000, 1.001] | no speed verdict |
| roslyn-git-w5 | scan | 1.020 | [1.007, 1.028] | no speed verdict |
| roslyn-git-w5 | process | 1.020 | [1.007, 1.028] | no speed verdict |
| roslyn-git-w5 | bytes | 1.132 | [1.131, 1.137] | no speed verdict |
| roslyn-git-w5 | allocations | 1.000 | [0.999, 1.001] | no speed verdict |

All raw samples, including excluded warmup processes, remain in result.json and their original stdout/fingerprints. P95 uses nearest rank; CV uses population standard deviation. No outlier is removed. Ratios resample matched round indexes and are meaningful only for this workload and measurement window. A speed verdict requires at least a 10% margin, a confidence interval excluding parity and no p95 regression. Raw B/op and allocs/op are Go benchmark counters, distinct from cumulative heap profiles and peak process RSS.

First-scan excludes Go package initialization; external process timing includes initialization, setup, scanning, fingerprint/output work and shutdown. OS cache, governor/turbo/SMT and host interference are uncontrolled within Docker Desktop. A single paired window does not establish three-window stability or a universal speedup. High CV and duration-floor advisories remain visible in the JSON. Separate memory profiles are never pooled with these timing samples.

This experiment is accepted for reduced allocation volume, with no headline speed claim. Separate Roslyn allocation profiles attribute 297.19 MB to getLines in the baseline and 2.50 MB in the candidate (pprof display units). These sampled cumulative values are separate from benchmark B/op and peak RSS. The original isolated proof is retained as historical evidence; the current accepted proof is [getlines-optimization.md](../../getlines-optimization.md).
