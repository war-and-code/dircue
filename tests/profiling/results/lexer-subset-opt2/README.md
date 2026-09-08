# Lexer subset opt2 measurements

Experimental measurements of the same preloaded GetLanguage API. Warm classifier time, first pass, and total process costs are distinct; a warm improvement alone is not an end-to-end speed claim.

| Input | Library | Warm passes / process | Warm median / p95 per corpus pass (s) | First-pass median / p95 (s) | Process median / p95 (s) | Nonwarm process median (s) | Peak RSS (MiB) |
|---|---|---:|---:|---:|---:|---:|---:|
| full | official | 1 | 0.976746 / 0.983785 | 0.978549 / 0.989916 | 2.048953 / 2.067447 | 1.071379 | 103.3 |
| full | maintained | 1 | 0.593149 / 0.602002 | 0.688831 / 0.700632 | 1.367111 / 1.389680 | 0.772116 | 104.4 |
| prefix | official | 1 | 0.920476 / 0.935667 | 0.924235 / 0.942401 | 1.930916 / 1.946023 | 1.008863 | 93.5 |
| prefix | maintained | 1 | 0.531016 / 0.539219 | 0.622780 / 0.639719 | 1.231359 / 1.243666 | 0.699337 | 92.4 |

| Input | Library | Warm bytes / allocations per call | Median process CPU (s) | Warm CV | Samples |
|---|---|---:|---:|---:|---:|
| full | official | 20,731 / 106.54 | 2.090 | 0.006 | 20 |
| full | maintained | 11,801 / 165.83 | 1.405 | 0.007 | 20 |
| prefix | official | 20,844 / 106.58 | 1.980 | 0.007 | 20 |
| prefix | maintained | 11,848 / 165.83 | 1.260 | 0.007 | 20 |

| Input | Official/candidate warm ratio | Paired 95% interval | Warm verdict | Process ratio / paired 95% interval |
|---|---:|---|---|---|
| full | 1.647 | [1.632, 1.654] | faster with margin | 1.499 / [1.490, 1.505] |
| prefix | 1.733 | [1.724, 1.746] | faster with margin | 1.568 / [1.559, 1.579] |

| Input | Historical baseline | Warm per-pass time decrease | First-pass time change | Process time change |
|---|---|---:|---:|---|
| full | rc1 | 55.3% | -51.3% | -51.7% |
| full | opt1 | 49.7% | -45.7% | -45.9% |
| prefix | rc1 | 58.4% | -54.2% | -54.6% |
| prefix | opt1 | 52.6% | -48.5% | -48.8% |

Historical comparisons are separate windows. Warm times are divided by the actual complete corpus pass count. First pass is already one full pass. Process time and peak RSS include preload, package/runtime setup, the first pass, all warm passes, GC and output; process costs are never divided by warm iterations. Nonwarm process time is external process duration minus the measured warm loop, not a pure initialization or classifier metric.

Every completed cell has at least 20 retained samples after at least 3 symmetric warmups and at least 10 measured warm seconds. No timed samples are removed. Original outputs, discrepancies, raw resources, input/build identities and actual source inventories are archived. All 3,388 full-content Ruby labels and ordered-token hashes/counts passed; prefix agreement does not establish a separate prefix Ruby oracle. Official timing binaries are unchanged.

The managed manifest predates this experiment. Actual source inventories before/after the build and at capture identify the runtime; the changed tokenizer and new test are archived separately. The opt1-to-opt2 manifest change is expected because opt1 was integrated after its experimental record. Managed opt2 integration and broader release acceptance remain separate gates.

The original subset.patch and experiment-receipt.json describe the initial source-only proposal. The subsequent ruleids-test-only.patch removes runtime state used only by tests. Those proposal receipts do not identify the final measured runtime: the matching actual source inventories and final build receipt do.

Environment limits remain: Docker Desktop, no experimentally isolated governor/turbo/SMT, no independent quiet-host trace, and only one measurement window per input. No universal or three-window stability claim is justified.

Audit only: `python3 tests/profiling/record_subset.py --audit-only`. No benchmark runs during recording or audit.

The archive also retains the completed initial retained-ID full-input run as a separate diagnostic variant. Its samples are not pooled with the final variant.

| Diagnostic variant | Warm median per pass (s) | Process median (s) | Peak RSS (MiB) | Official/candidate warm ratio / paired 95% interval |
|---|---:|---:|---:|---|
| Initial retained IDs, full only | 0.594577 | 1.368119 | 104.2 | 1.644 / [1.634, 1.652] |
