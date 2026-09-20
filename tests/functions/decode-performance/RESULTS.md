# Decoder optimization results

Reusing the successful whole-response JSON check reduced the measured cost of
the optional 128-entry decoder from **34.03 to 25.07 ms** per operation, a **26.3%**
reduction. Allocated bytes fell from **12.85 to 9.01 MB per operation** (29.9%), and
allocation count fell from **438,735 to 255,774** (41.7%).

These results concern the Go response decoder, not total repository analysis
time. The benchmark excludes traversal, native parsing and CLI serialization.
The optimization changes nine production lines and replaces one caller line;
the original validation statements are retained byte-for-byte.

Twenty batch-average measurements per size and mode, on the same M1 Max host:

| Declared functions | Retained | Before median ms | After median ms | Median reduction |
| ---: | ---: | ---: | ---: | ---: |
| 1 | 1 | 0.510 | 0.432 | 15.4% |
| 10 | 10 | 2.906 | 2.223 | 23.5% |
| 50 | 50 | 13.511 | 10.521 | 22.1% |
| 100 | 100 | 26.579 | 20.903 | 21.4% |
| 128 | 128 | 34.031 | 25.068 | 26.3% |
| 500 | 128 | 33.836 | 25.176 | 25.6% |
| 1,000 | 128 | 35.275 | 25.492 | 27.7% |

Default-mode allocation counts were unchanged. Its p95 batch-average drift was
0.9–5.3%, inside the profiling skill's 10% noise envelope. The unchanged strict
envelope and standalone decoder benchmarks also stayed within that envelope.
No default-path speedup is claimed.

The opt-in p95 reductions ranged from 5.4% to 28.5%; the 100-function sample had
more tail variation than its median suggests. All samples, including outliers,
are retained. With only 20 batch measurements, p99/p99.9/p99.99 collapse to the
sample maximum and cannot estimate production latency tails.

Maximum process RSS for the full fixed-duration benchmark suite was 25.1 MiB
before and 25.2 MiB after. That does **not** show a peak-RSS reduction. Sampled
heap allocation volume and bytes/op describe allocation churn, not live heap
high-water or proportional set size. PSS and per-core utilization were not
available through this macOS setup. The benchmark process used approximately
99% of one CPU; `-test.cpu 1` was the same in both phases.

The after-profile still identifies strict JSON token walking and metric decoding
as substantial costs. Their percentages change because the workload completes
more operations in the fixed profiling interval. Compare bytes/op and ns/op for
before/after effects, not total sampled allocation volume across unlike counts.
Further parser or serialization changes were deliberately deferred to preserve
attribution to this one lever.

## Contract and correctness evidence

- The initial strict envelope check is unchanged: UTF-8, framing, duplicate
  nested keys, depth and aliases remain checked before typed decoding.
- Standalone `decodeFunctions` retains its original precheck. Only the caller
  whose exact JSON has already passed the stronger enclosing check reuses it.
- The same required-field, cardinality, population, name, index, span, numeric,
  metric-group and partial-status validation still runs. Canonical serialization
  and default-path statements are unchanged.
- `proof.py` reconstructs the original decoder body byte-for-byte from the
  wrapper and shared tail and verifies the sole outer-response caller change.
- Explicit duplicate/escaped-key, invalid UTF-8, depth, number, name and span
  mutations passed their rejection checks. Differential fuzzing completed
  1,274,273 executions in 31 seconds without disagreement.
- The full structure package tests, race tests and vet passed.
- `compare_cli.py` verified exact stdout, stderr and exit codes in 27 pre/post
  CLI cases, including mixed Java/C#/Python, parser recovery, generated source,
  the per-file cap and 1,024-entry global cap. It uses the same native worker on
  both sides. Input sources and commands are reproducible from the harness.

Both timing windows were coordinated to avoid other agent builds/tests. They
were separate before/after phases, not interleaved A/B runs. The host was not
isolated from OS activity and had existing swap usage; no tuning or cache
flushing was performed. Source and executable hashes were checked before and
after each phase. Full package source snapshots, native inputs, raw benchmark
logs, sampled CPU/heap profiles and fingerprints are retained.

Run `python3 tests/functions/decode-performance/verify.py` to verify the retained
evidence. This is a bounded performance result with explicit compatibility
evidence, not a release certification or a general speed guarantee.
