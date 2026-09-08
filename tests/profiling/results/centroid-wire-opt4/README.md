# Paired scanner experiment

Baseline and candidate ran adjacently with reversed tool order each round. Every full result digest matched RC1. Scan, external process and allocation costs have different scopes.

| Scenario | Tool | Samples | Scan median / p95 (s) | Scan CV | Process median / p95 (s) | Allocated bytes / count per scan | Peak RSS (MiB) |
|---|---|---:|---:|---:|---:|---:|---:|
| ripgrep-directory-w1 | baseline | 500 | 0.128893 / 0.192885 | 0.203 | 0.156474 / 0.220566 | 41,346,608 / 218,704 | 40.9 |
| ripgrep-directory-w1 | candidate | 500 | 0.027863 / 0.031387 | 0.071 | 0.052886 / 0.061865 | 16,964,032 / 41,442 | 29.4 |
| jq-directory-w1 | baseline | 500 | 0.147467 / 0.208180 | 0.158 | 0.174739 / 0.234360 | 42,141,956 / 231,647 | 41.1 |
| jq-directory-w1 | candidate | 500 | 0.049082 / 0.054858 | 0.071 | 0.074354 / 0.085184 | 17,760,224 / 54,394 | 29.9 |
| roslyn-git-w5 | baseline | 20 | 7.254321 / 7.496540 | 0.021 | 7.297589 / 7.539448 | 2,232,510,108 / 8,089,250 | 344.2 |
| roslyn-git-w5 | candidate | 20 | 7.153143 / 7.538241 | 0.025 | 7.195659 / 7.584462 | 2,206,338,868 / 7,910,199 | 344.5 |

| Scenario | Metric | Baseline/candidate ratio | Paired 95% interval | Verdict |
|---|---|---:|---|---|
| ripgrep-directory-w1 | scan | 4.626 | [4.584, 4.688] | faster with margin |
| ripgrep-directory-w1 | process | 2.959 | [2.924, 2.982] | faster with margin |
| ripgrep-directory-w1 | bytes | 2.437 | [2.432, 2.474] | no speed verdict |
| ripgrep-directory-w1 | allocations | 5.277 | [5.275, 5.279] | no speed verdict |
| jq-directory-w1 | scan | 3.005 | [2.968, 3.038] | faster with margin |
| jq-directory-w1 | process | 2.350 | [2.323, 2.379] | faster with margin |
| jq-directory-w1 | bytes | 2.373 | [2.369, 2.376] | no speed verdict |
| jq-directory-w1 | allocations | 4.259 | [4.257, 4.260] | no speed verdict |
| roslyn-git-w5 | scan | 1.014 | [0.987, 1.022] | no speed verdict |
| roslyn-git-w5 | process | 1.014 | [0.987, 1.022] | no speed verdict |
| roslyn-git-w5 | bytes | 1.012 | [1.010, 1.016] | no speed verdict |
| roslyn-git-w5 | allocations | 1.023 | [1.022, 1.024] | no speed verdict |

All raw samples, including excluded warmup processes, remain in result.json and their original stdout/fingerprints. P95 uses nearest rank; CV uses population standard deviation. No outlier is removed. Ratios resample matched round indexes and are meaningful only for this workload and measurement window. A speed verdict requires at least a 10% margin, a confidence interval excluding parity and no p95 regression. Raw B/op and allocs/op are Go benchmark counters, distinct from cumulative heap profiles and peak process RSS.

First-scan excludes Go package initialization; external process timing includes initialization, setup, scanning, fingerprint/output work and shutdown. OS cache, governor/turbo/SMT and host interference are uncontrolled within Docker Desktop. A single paired window does not establish three-window stability or a universal speedup. High CV and duration-floor advisories remain visible in the JSON. Separate memory profiles are never pooled with these timing samples.

Accepted for the scoped small-repository startup and allocation improvements. Roslyn timing remains inconclusive and peak RSS is effectively unchanged. Separate sampled loadCentroids allocation decreases from 25.15 MB to 3.02 MB; the total sampled profile is nearly unchanged. The stripped CLI grows by 524,288 bytes (512 KiB). These results are a paired model-format comparison, not an upstream Enry CLI or final release acceptance claim. See [model-format-optimization.md](../../model-format-optimization.md).

Replay without workloads: `python3 tests/profiling/record_model.py --audit-only`. The deterministic archive retains source, model, updater, original/normalized build receipts, all raw samples, profiles and correctness outputs. No binaries or corpus checkouts are embedded.
