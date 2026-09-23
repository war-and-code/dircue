# Initial 1.0 golden map corpus

This is the small, hand-written gate for issue #75. It is deliberately a
reviewable starting point rather than the promised 20–25 real repositories.
The fixtures exercise the map's contract on a non-source directory, a compact
polyglot component set, deployable configuration, and documentation that makes
claims without supplying evidence. They are authored facts, not output copied
from dircue.

The expectation file is independent of the implementation. Each `must_include`
assertion is a question about a node or edge; `must_not_include` protects
against treating README text as evidence. Missing optional observations are
reported as `allowed_unknown` and do not become false absences.

Run the gate against map JSON produced by a candidate binary:

```sh
python3 tests/map_corpus/run.py --binary ./dircue --output .cache/map-corpus
python3 tests/map_corpus/verify.py --results .cache/map-corpus
```

The runner uses the intended one-shot shape, `dircue map --json PATH`, and
stores one JSON document per fixture. It does not execute files in fixtures or
fetch repositories. `verify.py` also checks root-relative evidence paths,
documentation exclusion, canonical IDs, and deterministic re-encoding. A
passing result is an initial gate only; it does not establish the full #75
precision/recall, Linguist differential, real-repository, or performance
requirements.

