# Guarded ASCII identifier experiment

This historical experiment passed lexical equivalence tests, the maintained-module suite, and all 3,388 actual Ruby ordered-token and label comparisons, with zero reference errors. Both unprofiled 20-pair library measurements are retained in the [opt1 record](results/identifier-opt1/README.md). Its file inventory was verified before and after building. Managed patch integration and clean regeneration are complete. Later release checks are recorded in the [final validation bundle](../release/results/final/README.md).

| Input | Historical RC1 median | Opt1 median / p95 | Official median in opt1 run | Historical time decrease | Opt1 slower than official |
|---|---:|---:|---:|---:|---:|
| Full content | 1.327173 s | 1.178462 / 1.205729 s | 0.976598 s | 11.2% | 20.7% |
| 128 KiB prefix | 1.276631 s | 1.120242 / 1.129206 s | 0.920643 s | 12.3% | 21.7% |

RC1 comparisons are historical separate windows, not paired before/after measurements. The official comparison within each opt1 run is paired; opt1 remains slower. All raw samples, allocations, RSS, first-pass costs, label differences and environmental waivers are retained. There is no independent prefix Ruby oracle or repository CLI speed claim.

## Evidence and one changed mechanism

The baseline profile is recorded in [HOTSPOTS.md](HOTSPOTS.md): `LinguistTokenize` accounts for 4.24 seconds cumulative, 62.54% of the maintained library warm CPU profile. Capture matching accounts for 1,186.54 MiB in the broader allocation profile. The opportunity score is 5 impact × 5 confidence / 2 effort = 12.5. Linked receipts retain baseline medians, byte-identical sample inputs, build hashes, and the scope of each profile. These observations motivated the experiment; performance measurements were still needed to assess the change.

The change avoids longest-match regex capture matching only when the first byte is an ASCII letter or underscore. It consumes the maximal `[A-Za-z0-9_]+` prefix and calls the existing token-emission closure. All other first bytes use the original lexer and action dispatch. Whitespace, model decoding, storage, scoring, token allocation strategy, and parallelism are unchanged.

## Equivalence argument

For either lexer state, inspect the current ordered rule lists. Among their alternatives, only `[.@#$]?[A-Za-z0-9_]+` and the final one-rune fallback can begin with an ASCII letter or underscore. None of the beginning-of-line comment/shebang rules starts with these bytes. The optional identifier punctuation cannot match an initial letter/underscore, so the identifier extent is exactly the maximal ASCII letter/digit/underscore run.

That run is at least one byte. It beats the fallback on length when longer than one byte, and on declaration order when exactly one byte. The selected action is therefore always `feed`. The run contains no LF, so the existing post-consumption beginning-of-line state is always false. The fast branch sets that same state and resumes at the same next byte. Inductively, later lexical states, action dispatch, and emitted token order are identical.

The guard intentionally rejects digit starts: numeric skip rules may tie or outmatch an identifier. It also rejects optional identifier punctuation, non-ASCII and malformed UTF-8 bytes, whitespace, quotes, and operator/comment prefixes, leaving their competing rules unchanged.

- Ordering preserved: same consumed extent, `feed` action, next byte, and beginning-of-line state; no token reorder or omission.
- Tie-breaking unchanged: identifier precedes one-rune fallback in both rule lists; all other ties retain the general lexer.
- Floating-point: unchanged; centroid weighting, accumulation order and candidate sorting are untouched.
- RNG seeds: production has none; generated equivalence tests use seed 97001.
- Truncation: the existing 100,000-byte input cap runs before the fast branch; the unchanged emitter truncates tokens to 16 bytes after consuming the complete identifier. Classifier's separate 50 KiB cap is unchanged.
- Golden outputs: all 3,388 ordered Ruby token sequences (NUL-separated SHA256 plus token count) and labels match with zero reference errors. Existing broader repository/CLI gates still require validation before release acceptance.

## Correctness checks and experiment procedure

`linguist_identifier_test.go` compares the fast-path guard, exact consumed extent and selected rule/action against the existing longest-match regex in both lexer states. It exercises every possible first-byte/next-byte pair, termination by every byte, token truncation lengths, large identifiers, newline/NUL/non-ASCII boundaries, and 10,000 deterministic generated inputs. This direct extent assertion is necessary because equal 16-byte emitted prefixes alone could hide overconsumption.

Ordered-token comparisons use `linguist_reference_test.go`, a frozen copy of the RC1 tokenizer loop that never calls the fast helper. Its original source SHA256 is `a25e3a34163965df585fdafb66ab8f3450fbbcec617c54bdc43668d0d4c0055f`. It exercises comments, strings, escapes, shebangs, numeric competition, beginning-of-line transitions, invalid bytes, and the input cap. The reference retains shared declarative lexer tables, which are unchanged in this experiment; this is a regression oracle for this single change, not an independent reimplementation of upstream semantics. The actual Ruby population gate remains required.

The procedure for this experiment was to run the tokenizer tests in the nested maintained module, then the complete maintained-library tests and all-sample Ruby ordered-token/label comparison. Any lexical mismatch rejects the change. Updating the managed patch/provenance requires clean regeneration before release acceptance.

Candidate builds preserve every RC1 binary and receipt. Compare unprofiled full and prefix library calls on identical inputs, along with relevant cold scanner scenarios. Record medians, empirical tails, allocations, and scope; profile the result before selecting another optimization. To discard an unproven or regressing change, restore only this experiment's runtime file and remove its new tests, or revert its dedicated commit. Preserve unrelated work.

## Managed fork integration

`third_party/update_enry.py` regenerates runtime source and regression tests from official Enry, the pinned Linguist inputs, and `third_party/patches/enry-linguist-9.7.patch`. The patch now includes this runtime delta and both regression tests. A fresh output regenerated all 41 managed files, passed the complete maintained-module test suite, and reproduced the runtime, tests, model and generator warnings byte for byte. The updater-generated provenance was then adopted into the maintained fork.

The separate [regeneration receipt](results/identifier-opt1/regeneration.json) records generator, patch and provenance hashes. The original timing archive remains unchanged: its actual source hashes describe the experiment before managed integration, and the regeneration confirms those runtime bytes were preserved. Broader release checks remain required for subsequent release binaries.
