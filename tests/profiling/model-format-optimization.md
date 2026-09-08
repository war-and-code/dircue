# Binary centroid model experiment

The immediate profile baseline identified `loadCentroids` as 90 ms of jq's 160 ms sampled CPU and 120 ms of ripgrep's 180 ms. Loading the maintained Linguist model from compressed JSON built transient decoding structures on the first classification. This experiment replaces that serialization with a checked, versioned binary representation while preserving the vocabulary, IEEE float64 values, centroid maps and classifier arithmetic.

The accepted opt3 scanner binary is reused, with a new receipt proving its complete built source equals commit `0577e7e190dfe65551c9eda0be8fb8922c9de5b1`. Its original build receipt is retained; no rebuild is implied. The candidate is an isolated six-path source overlay, with every one of its 73 built files hashed before and after compilation. The model's canonical Linguist JSON identity and all 45 regenerated managed files were verified. The classifier arithmetic block is byte-identical. Runtime decoding uses checked bounds and rejects malformed format/version/hash/order/indices/nonfinite values; it introduces no unsafe code or dependencies.

The fixed paired experiment used identical read-only fixtures, Go 1.26.6, compiler flags and runtime settings. Each small scenario has 500 pairs; Roslyn has 20. Three warmup pairs per scenario and a separate five-pair small pilot are retained and excluded. Every scan/process cell exceeds 10 cumulative seconds. All 2,080 fingerprints, including the pilot, warmups and separate memory profiles, match the existing full RC1 result digests.

| Scenario | Scan median baseline → candidate | Paired scan ratio, 95% CI | Process median baseline → candidate | Peak RSS baseline → candidate |
|---|---|---|---|---|
| ripgrep directory, 1 worker | 128.893 → 27.863 ms | 4.626 [4.584, 4.688] | 156.474 → 52.886 ms | 40.9 → 29.4 MiB |
| jq directory, 1 worker | 147.467 → 49.082 ms | 3.005 [2.968, 3.038] | 174.739 → 74.354 ms | 41.1 → 29.9 MiB |
| Roslyn Git, 5 workers | 7.254 → 7.153 s | 1.014 [0.987, 1.022] | 7.298 → 7.196 s | 344.2 → 344.5 MiB |

The small-repository improvement meets the predefined 10% margin, confidence interval and p95 conditions. Roslyn timing is inconclusive; its p95 rises slightly from 7.497 to 7.538 seconds, and peak RSS is effectively unchanged. All outliers remain in the report, including baseline small-scan CVs of 20.3% and 15.8%. The results come from one paired Docker Desktop window, with uncontrolled host governor/turbo/SMT and cache state; three independent windows were not performed.

Small benchmark allocation volume falls from 41.35 to 16.96 MB/op for ripgrep and 42.14 to 17.76 MB/op for jq. Roslyn falls from 2.233 to 2.206 GB/op. Separate matching-binary memory profiles attribute 25.15 MB versus 3.02 MB to `loadCentroids`; total sampled allocation remains nearly unchanged (2,142.01 versus 2,140.76 MB). Sampled profile quantities, benchmark B/op, and peak RSS measure different costs.

The binary model grows from 1,713,719 to 2,223,374 bytes; the stripped CLI grows by 524,288 bytes (512 KiB). This size cost is accepted for the measured startup improvement. The new serialization preserves the model's mathematical data and classifier behavior.

Correctness checks passed for all 3,388 actual Ruby labels and ordered token sequences and all 11 pinned public repository breakdowns. Official Enry matches 3,241 of those sample labels, with all 147 differences retained. Candidate application-package and nested fork tests, migration identity tests, corruption tests, and regeneration checks passed. The isolated application root did not contain the full repository test tree. Later checks of the integrated source and release are recorded in the [final validation bundle](../release/results/final/README.md); the [final Enry comparison](../enry-performance/results/final/README.md) covers controlled library and CLI performance.

Raw paired samples, p95/CV, CPU/RSS/allocation distributions, receipts, profiles, source/model/updater bytes and actual Ruby outputs are in [the evidence package](results/centroid-wire-opt4/README.md). Replay its audit with `python3 tests/profiling/record_model.py --audit-only`. No measurements are repeated by the recorder.
