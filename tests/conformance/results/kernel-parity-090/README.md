# Linguist strategy-parity receipt

This directory records the bounded regression evidence for the 0.9.0
candidate-strategy corrections. It does not claim universal equivalence with
Linguist. The durable fixtures and runners live in `tests/conformance`; public
source bytes remain outside this repository and are pinned by hash in
`public_corpus.json`.

`focused.json` records the exact before/after binaries, source revisions,
reference container, commands, report hashes, and result counts. The full raw
reports are intentionally kept as build artifacts because they contain more
than 290 KiB of duplicated CLI output. `classifier-window.json` is small enough
to retain in full and includes the mutation-sensitive uncapped control.

The result distinguishes three kinds of change:

- `.pl`, `.pm`, and `.t` candidate narrowing and generic-extension deferral are
  corrections to the intended Linguist 9.7.0 classification contract. Some
  legacy outputs had the same final language but reported a different strategy.
- The 50 KiB heuristic boundary is a classification correction. Its controls
  use raw-byte offsets, including a UTF-8 case, so a codepoint-based limit
  cannot pass accidentally.
- The centroid classifier already enforced its separate 50 KiB limit. The
  retained test proves that behavior and would fail if the existing slice were
  removed; no classifier production change was needed.

The focused candidate passes 30/30 kernel regressions, 94/94 generic-extension
checks, four of four pinned public-corpus comparisons, and all seven classifier
window controls. The preserved pre-fix binary passes 9/30, 52/94, and one of
four respectively. These counts apply only to the enumerated fixtures and
pinned public revisions.

The frozen final runtime candidate also passes the complete 600-result
synthetic matrix with 582 direct passes, 18 documented expected differences,
and no failures. The upstream sample gate records 3,388/3,388 exact labels and
3,388/3,388 exact ordered token sequences; the independent stock Enry control
matches 3,241/3,388 labels. These are bounded corpus counts.
