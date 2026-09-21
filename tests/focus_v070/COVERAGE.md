# Coverage matrix

The retained [0.7.0 results](results/) contain passing runs for this matrix, including the verified native worker. The table describes the required evidence before a run, not a replacement for its receipt. See the [candidate validation report](../../docs/releases/0.7.0-validation.md) for measured coverage and limits.

| Claim | Evidence | Oracle | Status before a receipt |
|---|---|---|---|
| Broad inherited CLI compatibility | 278 raw exit/stdout/stderr cases across retained 0.2, projects, native structure, 0.4 modules and 0.5 declarations | actual pinned 0.6.1 core and worker | not run |
| 0.6.1 language output remains compatible | raw exit/stdout/stderr on both fixtures | released pinned executable | not run |
| 0.6.1 metrics output remains compatible | raw exit/stdout/stderr, including file records | released pinned executable | not run |
| 0.6.1 declarations/formats/all remain compatible | raw exit/stdout/stderr | released pinned executable | not run |
| Native hotspot behavior remains compatible | optional raw structure/hotspot command | released core plus verified worker | untested unless worker supplied |
| .NET primary ownership | nested source plus unsupported and invalid nested barriers; selection is explicitly partial | authored exact path list and status | not run |
| Declared linked .NET file does not imply source ownership | linked file outside selected root | authored exclusion | not run |
| .NET context | ancestor `Directory.Build.props` candidate and conditional declared import | authored required context set | not run |
| Explicit related .NET metrics | library project | authored separate path list and full-root subset | not run |
| Python/uv member primary ownership | workspace member plus nested Cargo barrier | authored exact path list | not run |
| Python/uv parent context | parent workspace manifest and `uv.lock` | authored required context set | not run |
| Explicit related Python metrics | sibling workspace project | authored separate path list and full-root subset | not run |
| Reverse shared-input query | `Directory.Build.props` and `uv.lock` | selected project must appear; reported candidates retained | not run |
| Focus metric equivalence | per-file records and recomputed totals | candidate full-root scc report filtered by authored paths | not run |
| Default-path cost | released/candidate complete CLI process | alternating repeated measurements | not run |
| Focus prepass and metric cost | plan, focus metrics and full metrics lanes | alternating repeated measurements on content-hashed monorepo | not run |

Pure-package tests separately cover duplicate roots, ownership ambiguity, incomplete inventory and declarations, cycles, bounded queries, work and output limits, cancellation, nonregular barriers, unsupported selection, and output trimming without execution-set mutation. The CLI harness does not duplicate those implementation-level assertions.
