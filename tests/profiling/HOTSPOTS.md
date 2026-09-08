# Initial profiling findings

This historical record covers RC1 and the identifier fast-path investigation. Subsequent work completed the [lexer subset optimization](lexer-subset-optimization.md), [fresh scanner profiling](results/scanner-opt2/README.md), [bounded generated-line scanning](getlines-optimization.md), and [binary model loading](model-format-optimization.md). The proposed next steps below belong to those earlier source states.

The first completed scenario is single-worker `GetLanguage` on all 3,388 pinned Linguist samples, with identical preloaded full bytes. Twenty fresh processes per library follow three excluded warmups. RC1 takes 1.327 s per warm corpus pass versus official Enry v2.9.6's 0.981 s. The maintained library allocates 68,457 bytes per call versus 20,693. Complete labels match 3,388 Ruby results versus 3,241 for the official library. Repository inclusion, disk access and CLI output are outside the timed API.

## Scenario receipts

Baseline: `.cache/enry-results-rc1/library-full-baseline.json`. Optimized, unstripped profile build: `.cache/enry-profiles-build-rc1/build-receipt.json`. Profile result: `.cache/enry-results-rc1/library-full-profile.json`. Every receipt records exact binary, harness and input hashes. The profile driver performs five warm passes; CPU sampling brackets only that loop. Allocation profiles also include preloading, first-pass model initialization and runtime setup. Explicit warm allocation counters exclude that setup.

## Ranked evidence

| Rank | Location / calling path | Metric and observed value | Category | Exact supporting artifact | Scope and limitation |
| --- | --- | --- | --- | --- | --- |
| 1 | `internal/tokenizer.LinguistTokenize` | 4.24 s cumulative, 62.54% of 6.78 s sampled CPU; capture matching allocates 1,186.54 MiB cumulatively | CPU / allocation | `.cache/enry-results-rc1/library-full-profiles/library-maintained-cpu-top.txt` and `library-maintained-heap-top.txt` | Allocation profile includes setup and first pass |
| 2 | `GetLanguagesByContent` / heuristics | 1.87 s cumulative, 27.58% CPU | CPU | Same CPU artifact | Parent and child costs overlap |
| 3 | `GetLanguagesByModeline` | 0.43 s cumulative, 6.34% CPU | CPU | Same CPU artifact | Whole population, including early exits |
| 4 | Token emission | 61.37 MiB allocated, 4.47% | Allocation | Same heap artifact | Broader scope than warm counters |
| 5 | Lazy model decoding | 27.11 MiB allocated, 1.98% | Allocation | Same heap artifact | Outside warm CPU interval |

For CPU, rank sampled self and cumulative time without summing overlapping parent/child costs. For allocations, distinguish bytes, objects, retained heap and peak RSS. For I/O, distinguish full logical language bytes from actual storage reads and waits. For locks, report waiting evidence rather than assuming a mutex is expensive because it exists.

## Hypothesis ledger

| Candidate explanation | Supports / rejects / unresolved | Evidence | Next discriminating measurement |
| --- | --- | --- | --- |
| Generic longest-match capture execution costs too much for unambiguous ASCII identifiers | Supports | Tokenizer consumes 62.54% CPU and 91.27% of broader allocation volume; warm allocation counters independently show excess | Guarded identifier fast path, lexical equivalence and actual Ruby token/label gate, then paired unprofiled measurement |
| Model decoding dominates small repository cold scans | Unresolved | Warm profile excludes initialization | First-scan scanner CPU profile |
| Git object lock limits large repository throughput | Unresolved | Preloaded single-worker library work does not exercise Git | Git CPU and mutex profiles |

The identifier fast path scores **12.5**: impact 5 × confidence 5 / effort 2. No other current lexical rule competes with an initial ASCII letter or underscore. Preserve longest match, rule order, token truncation, input limits and all action semantics; keep numeric prefixes, comments, strings and operators with the general lexer. Floating-point scoring and candidate ordering remain unchanged. Validate lexical decisions on boundaries and generated inputs, then all 3,388 ordered tokens and labels against actual Ruby outputs. Keep RC1 binaries for rollback and comparison. A failed equivalence check rejects the optimization.

Host limitations: Docker Desktop Linux arm64 on Apple Silicon; no global kernel, power or cache tuning. Other builds and benchmarks were excluded during measurement, but host scheduling and CPU frequency were not experimentally controlled. This is one baseline window, not three repeat windows. These results support scoped diagnosis, not a universal speed claim. I/O and contention are unmeasured in this preloaded single-worker scenario.

## After the identifier experiment

All 3,388 Ruby token sequences and labels still match. In separate paired windows,
the maintained full-corpus median fell from 1.327 s to 1.178 s and the prefix
median from 1.277 s to 1.120 s. The unchanged official binary remained near
0.98/0.92 s. The first optimization reduces runtime, but the maintained library
remains about 21% slower than upstream.

A fresh five-pass CPU profile still assigns 3.58 s (59.77% of 5.99 sampled CPU
seconds) to `LinguistTokenize`; `FindSubmatchIndex` accounts for 3.46 s (57.76%).
The identifier helper itself accounts for 0.03 s. Evidence:
`.cache/identifier-opt1/profiles/library-maintained-cpu-top.txt`; baseline and
profile build receipts are in `.cache/enry-builds-identifier-opt1` and
`.cache/enry-profiles-identifier-opt1`. These are separate unprofiled/profiling
builds with the same optimized source and different symbol settings.

The next tokenizer experiment will reduce irrelevant regex alternatives and
capture slots through conservative initial-ASCII-byte rule subsets. Derive
possible starts mechanically from `regexp/syntax`, retain original rule order
and longest-match selection, and fall back to the full lexer for non-ASCII or
uncertain analysis. Identical subsets can share compiled regexes. This scores
8.3 (impact 5 × confidence 5 / effort 3). It targets the remaining measured
capture-matching cost more broadly than a whitespace-only special case.

Before acceptance, compare exact original rule identities and consumed extents,
including nullable expressions, flags, cycles, all bytes and non-ASCII fallback.
Then retain full token/label gates and measure process startup as well as warm
classification: extra subset compilation could offset the warm gain. Do not
combine this with whitespace skipping, model-format changes or generated-line
selection in the same experiment or commit.
