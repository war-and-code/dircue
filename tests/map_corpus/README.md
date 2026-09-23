# Initial 1.0 golden map corpus

This directory contains three complementary gates for issue #75. The compact
fixtures exercise non-source content, a polyglot component set, deployable and
interface declarations, configuration/import intent, misleading filenames,
and documentation that makes claims without supplying evidence. Assertions
name the source declaration or specification used as their oracle; they were
not copied from dircue output.

The expectation file is independent of the implementation. Each `must_include`
assertion is a question about a node or edge; `must_not_include` protects
against treating README text as evidence. Missing optional observations are
reported as `allowed_unknown` and do not become false absences.

Run the gate against map JSON produced by a candidate binary:

```sh
python3 tests/map_corpus/run.py --binary ./dircue --output .cache/map-corpus
python3 tests/map_corpus/verify.py --results .cache/map-corpus
python3 tests/map_corpus/metamorphic.py --binary ./dircue
```

The runner uses the intended one-shot shape, `dircue map --json PATH`, and
stores one JSON document per fixture. It does not execute files in fixtures or
fetch repositories. `verify.py` also checks root-relative evidence paths,
documentation exclusion, canonical IDs, and deterministic re-encoding. A
passing result is an initial gate only; it does not establish the full #75
precision/recall, Linguist differential, real-repository, or performance
requirements.

`metamorphic.py` independently checks worker-count equivalence, relocation and
creation-order portability, documentation non-interference, honest byte/file
budget coverage, absence of absolute source paths, and self-comparison. It uses
temporary copies and leaves no corpus output in the repository.

An already-materialized pinned public corpus can be checked without downloads:

```sh
python3 tests/map_corpus/verify_public.py \
  --binary ./dircue \
  --corpus-root /path/to/pinned-corpus \
  --output .cache/public-map-gate.json
```

`public_expectations.json` binds twenty-one upstream commits to facts taken
directly from cited manifests, source files, and tree inventories. It includes
Terraform, an unrendered Helm chart, Serverless Framework examples, uv, Kotlin,
.NET samples, a Python functions framework, OCI client source, and a Java SAM
application in addition to the original language corpus. The gate verifies
every commit and cited path before scanning. Its report retains each question,
its source URL, targeted negative checks, and questions deliberately left
unknown.

Precision is not claimed: the hand-authored expectations do not exhaustively
label every observation emitted from these repositories. Per-question recall
therefore covers only the cited positive facts; negative assertions are narrow
false-positive checks, and unknowns do not become passes. The repositories
also do not yet include a bounded Linux-kernel slice. These limits remain
visible instead of being converted into an aggregate 100% score.

The optional fetch helper is manual and never runs in CI. It requires every
repository ID explicitly and refuses to update or replace existing checkouts:

```sh
python3 tests/map_corpus/fetch_public.py \
  --destination /path/to/pinned-corpus \
  --id helm-examples --id aws-sam-java-rest
```

Fetching the full corpus can consume substantial disk space, so the helper has
no `--all` mode. The verification gate itself never accesses the network.
