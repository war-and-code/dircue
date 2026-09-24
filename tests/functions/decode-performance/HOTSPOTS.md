# Baseline findings and proposed change

The full 128-entry response took a median **34.03 ms** and **12.85 MB allocated
per operation**, with **438,735 allocations**. The corresponding default response
took 81.38 microseconds and 37,041 allocated bytes. Those are different response
populations; the ratio is not an optimizer speedup. Twenty batch measurements
per case are retained in `baseline/receipt.json` and `baseline/bench.txt`.

Ranked application costs from the five-second CPU/allocation sample:

| Rank | Location | CPU cumulative | Allocation cumulative | Evidence |
| --- | --- | ---: | ---: | --- |
| 1 | `decodeFunctions` | 29.52% | 66.22% | `baseline/cpu-top.txt`, `baseline/alloc-top.txt` |
| 2 | `functionJSONValue` | 19.19% | 58.89% | Same profiles; called from both validators |
| 3 | `strictFunctionResponse` | 10.70% | 31.72% | Same profiles |
| 4 | `decodeMetrics` | 4.43% | 19.72% | Same profiles |

These costs overlap: `decodeFunctions` includes one `functionJSONValue` traversal.
The runtime's GC/allocator system-stack costs dominate much of the remaining
profile (`kevent` 34.32%, `madvise` 19.93% flat). Do not sum overlapping rows or
interpret sampled allocation volume as peak resident memory.

Independent stage measurements support the attribution: whole-response strict
validation takes a median 9.99 ms and 4.11 MB allocated; standalone function-block
validation/decoding takes 21.34 ms and 8.48 MB allocated. The input source contains
128 functions, and the response retains all 128. The profiler uses optimized
Go code with symbols and one benchmark P; no OS settings were changed.

## Hypothesis ledger

| Hypothesis | Finding | Evidence |
| --- | --- | --- |
| Repeated strict JSON traversal contributes substantial cost | Supported | Both callers traverse the same function subtree; token walking accounts for 58.89% cumulative allocated bytes |
| Metric decoding/canonical serialization has additional cost | Supported, deferred | `decodeMetrics` and JSON encoding appear in profiles; changing these would be a separate lever |
| Tree-sitter parsing explains this boundary benchmark | Rejected for this scenario | The benchmark consumes retained response bytes; no native worker is invoked in the timed loop |
| Filesystem reads explain this boundary benchmark | Rejected for this scenario | Inputs are loaded and decompressed before the timer; decoder performs no filesystem operations |
| A lock redesign is needed | Unsupported | This scenario measures a synchronous local decoder; runtime GC costs do not establish a package-level lock problem |

## Opportunity and behavior proof

Proposed single change: reuse successful whole-response strict validation when
decoding its function block. Keep standalone `decodeFunctions` strict, so its
existing callers/tests retain all validation.

Impact 4 × confidence 5 / effort 1 = **20**, above the optimization skill's
minimum score of 2. The repeated walk is a top application hotspot by both CPU
and allocations. The expected benefit is fewer token allocations; measurement
must establish the actual improvement.

The whole-response check already validates UTF-8, JSON framing, duplicate keys
at every nested level and a maximum depth of 40. A function subtree begins one
level deeper, so passing the whole-response depth limit implies passing the
standalone subtree limit. The raw function bytes are extracted from that same
validated response without transformation.

Only the redundant framing/token walk may be bypassed. Required fields, spelling,
entry-count bounds before typed allocation, population equations, source spans,
indices, name restrictions, metric groups, numeric bounds and partial-status
validation stay in the same decoder. Whole-response aliases and duplicate keys
are still rejected before it runs. The default path is unchanged.

- Ordering and tie-breaking: the same typed decoder visits entries in input order.
- Floating point: unchanged metric parsing and canonical serialization.
- RNG: none.
- Output proof: compare with the original standalone decoder after a successful
  envelope check; retain mutation/fuzz checks and real CLI output comparisons.
- Rollback: revert only the two production hunks that move the caller past the
  redundant precheck.

No production optimization had been applied when this baseline and proposed
proof were recorded. Before/after measurements will be reported separately.
