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
python3 tests/map_corpus/robustness.py --binary ./dircue
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

`robustness.py` feeds 80 deterministic combinations of repeated Terraform
provider declarations, punctuation-heavy workflows, and multi-document YAML
through the full CLI. Each must return a parseable map; a declaration identity
collision or parser failure must not discard the entire document.

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
false-positive checks, and unknowns do not become passes. These limits remain
visible instead of being converted into an aggregate 100% score. A separate
five-file, hash-pinned Linux-kernel slice lives in
`tests/conformance/public_corpus.json`; it covers ambiguous Perl and the 50 KiB
classifier window rather than claiming to represent the complete kernel.

An additional bounded quality gate measures exact precision and recall rather
than extrapolating from targeted assertions. It labels every component,
deployable, interface, capability, and evidenced edge emitted from 20 reviewed
source paths, plus every coverage status. The eight entries include pinned
microservices-demo, eShop, Spring Petclinic, a Terraform provider-alias example,
Express, Flask, Helm examples, and the checked-in non-source fixture. Other map
records remain outside the denominator and the report says so explicitly:

```sh
python3 tests/map_corpus/verify_public_quality.py \
  --binary ./dircue \
  --corpus-root /path/to/pinned-corpus \
  --output .cache/public-map-quality.json
```

Each oracle path is content-hashed and linked to its exact upstream commit.
The microservices-demo run edge also uses a separately hashed Skaffold file as
supporting evidence; that file does not expand the scored path set.
For microservices-demo, the same gate also checks the full repository's twelve
service roots, twelve primary Dockerfiles, and nine GitHub workflow files
against source-enumerated path sets. These full-population guards catch dropped
services or workflows, but are not included in the bounded precision/recall
denominator.
The expectations cover multi-document Kubernetes, Python requirements-only
components, Dockerfile-to-component build edges, .NET project relationships,
declared Redis and event-bus capabilities, Terraform provider aliases, manifest
interfaces, and honest partial/unknown coverage. This is a precise score for a
reviewable slice, not a claim about every observation in the full repositories.

The optional fetch helper requires every repository ID explicitly and refuses
to update or replace existing checkouts. It fetches complete snapshots because
dircue's local Git reader cannot fill missing blobs from a partial clone.
Ordinary push and pull-request CI does
not fetch the public corpus; the event-driven `Public map quality` workflow runs
it only when explicitly dispatched:

```sh
python3 tests/map_corpus/fetch_public.py \
  --destination /path/to/pinned-corpus \
  --id helm-examples --id aws-sam-java-rest
```

Fetching the full corpus can consume substantial disk space, so the helper has
no `--all` mode. The verification gate itself never accesses the network.

## On-demand resource and compatibility evidence

The resource harness records wall time, user and system CPU, peak RSS, output
bytes, map status, node and edge counts, and raw and semantic output hashes. It
runs the `balanced` and `low-memory` presets by default and fails if they change
map bytes. Effective settings remain available through `map settings` rather
than inside the map document. It streams maps to temporary files and enforces an
output size limit instead of retaining every map in memory:

```sh
python3 tests/map_corpus/benchmark_public.py \
  --binary ./dircue \
  --candidate-commit "$(git rev-parse HEAD)" \
  --corpus-root /path/to/pinned-corpus \
  --output .cache/public-map-benchmark.json
```

Three complete runs on 2026-09-23 on an Apple M1 Max (macOS 26.5.2) compared
the prior `fast` preset with `balanced` and `low-memory` across all 21 pinned
repositories (63 scans per run). All preset outputs were byte-identical for
each repository. The aggregate wall times were 18.64–18.98 seconds for
`balanced`, 18.64–18.83 seconds for `fast`, and 25.75–26.08 seconds for
`low-memory`. Maximum observed RSS across the corpus was 596–608 MiB for
`balanced`, 638–669 MiB for `fast`, and 274–323 MiB for `low-memory`. `fast`
showed no consistent or material speed improvement and used more memory, so it
was removed; custom worker and Git-cache values remain available via `--set`.
On this corpus,
`low-memory` traded roughly 38% more wall time for about half the maximum RSS.
These are warm-cache observations on one machine, without approved thresholds;
they are not portable performance or memory guarantees. The measured binary
was built from commit `20bfd2077adf1dc595c181ff638be4f94606be47` and had
SHA-256 `020b9850fd9ce452d5b3ba9a1dd94a83cf9f4fc7ce5d41646268dabb7ea064a3`.

Two optional compatibility harnesses exercise the same exact commits. The
first requires the pinned local Linguist 9.7.0 container and disables its
network. The second byte-compares a candidate with a published dircue binary:

```sh
python3 tests/map_corpus/compare_public_languages.py \
  --binary ./dircue --corpus-root /path/to/pinned-corpus \
  --candidate-commit "$(git rev-parse HEAD)" \
  --output .cache/public-language-parity.json

python3 tests/map_corpus/compare_public_legacy.py \
  --candidate ./dircue --baseline /path/to/dircue-0.9.0 \
  --candidate-commit "$(git rev-parse HEAD)" \
  --corpus-root /path/to/pinned-corpus \
  --output .cache/public-legacy-v090.json
```

The 2026-09-23 runs matched Linguist's legacy JSON on 21 of 21 repositories and
matched dircue 0.9.0 byte-for-byte in all 63 legacy checks: JSON, breakdown
JSON, and breakdown with strategies. The Linguist differential used map-only
precursor commit `0aa0cb924b3e2285f7ee505bb3b25e16a8cf0592` (binary SHA-256
`0c1187da1b0f09fcae1c723806424b36745a81359ccc1fbdecba5365b27fd64d`);
the final 63-check run used the candidate commit and binary named above. Later
changes did not touch the legacy language path. The existing kernel-slice
Linguist gate also passed both JSON breakdown and strategy labels. These checks
establish only their named interfaces; they do not prove map-level parity with
Linguist, which has no equivalent map document.
