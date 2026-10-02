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

To check that the binary exits 0 and returns a valid 1.0.0 JSON map document
for every repository in a corpus directory, use `verify_corpus_availability.py`:

```sh
python3 tests/map_corpus/verify_corpus_availability.py \
    --binary ./dircue \
    --root .cache/corpus
# or via make:
make corpus-availability CORPUS_ROOT=.cache/corpus
```

The script maps each immediate subdirectory of `--root`, writes a per-repo
summary (exit code, duration, first stderr line, schema_version validity), and
exits 1 if any repo fails.  Pass multiple `--root` flags to cover several
corpus directories in one run.  Unit tests in
`test_verify_corpus_availability.py` cover the helper functions using fake
binaries (no network required).

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

`robustness.py` feeds 90 deterministic combinations through the full CLI.
The first 80 cover repeated Terraform provider declarations, punctuation-heavy
workflows, and multi-document YAML.  Variants 80–89 cover doc-named config
files: workflows and Kubernetes manifests whose basenames start with
`readme`, `changelog`, or `contributing` (e.g. `.github/workflows/changelog.yml`,
`deploy/changelog-service.yaml`).  Each variant must return a parseable map;
a declaration identity collision, path mis-classification, or parser failure
must not discard the entire document.

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
deployable, interface, capability, and evidenced edge emitted from 29 reviewed
source paths, plus every coverage status. The entries include pinned
microservices-demo, eShop, Spring Petclinic, a Terraform provider-alias example,
Express, Flask, Helm examples, a checked-in TypeScript import fixture, and the
non-source fixture. Other map records remain outside the denominator and the
report says so explicitly. The TypeScript fixture checks that a value import
corroborates an optional PostgreSQL dependency, a type-only JWT import leaves
a peer dependency conditional, and `src/test_utils.ts` is not mistaken for a
test file:

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

The 2026-09-23 run passed 167 of 167 exact records in that slice: 56 nodes,
39 edges, and 72 coverage statuses. The separate 21-repository qualitative
gate passed 44 targeted assertions. Its labels are not exhaustive, so that
gate does not report whole-repository precision.

The microservices-demo slice now includes the C# cartservice project, its
Protobuf contract, container build, and a pinned Kubernetes manifest. Those
sources declare Redis caching, PostgreSQL, and Protobuf generation; the contract
declares `CartService` and its three RPCs. The manifest contains five Kubernetes
objects: two Deployments, two Services, and a ServiceAccount. The expected
categories are respectively `workload`, `service`, and `infrastructure`, with
the metadata names preserved. The emailservice manifest uses the same
Deployment, Service, and ServiceAccount distinction, and the expectations label
all three. The gate fails if a candidate collapses these Kubernetes kinds into
workloads, loses source names while disambiguating repeated resources, or omits
the cartservice Redis capability and its component relationship. The Helm
emailservice template is hash-pinned as supporting evidence but stays outside
exact identity scoring: its conditional resource set and templated names remain
unresolved without evaluating chart values, and `dircue` does not render Helm.
Supporting receipts also pin eShop project files referenced by
project-relationship edges, without expanding the scored path slice.

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

## Source-first holdouts

Four additional pinned repositories test code bases outside the original eight
golden-label projects: [OWASP BenchmarkJava](https://github.com/OWASP-Benchmark/BenchmarkJava),
[OWASP BenchmarkPython](https://github.com/OWASP-Benchmark/BenchmarkPython),
[AppFlowy editor](https://github.com/AppFlowy-IO/appflowy-editor) for Dart, and
[AWS CardDemo](https://github.com/aws-samples/aws-mainframe-modernization-carddemo)
for COBOL/JCL. Their labels in `holdout_labels/` were written from source and
committed in `9fe9ceffed94598576e46b8d4c72b88951488975` before dircue was
run on the checkouts. A subsequent source-only review added three hashed
supporting files and corrected the Java HSQLDB explanation and Python SQLite
edge citation; it did not add or remove any scored positive fact. The original
commit remains available to audit that sequence. Each current label pins the
upstream commit and the SHA-256 of
every cited source file. The labels assert selected positive map facts and
coverage expectations; they are **not exhaustive**, so they cannot support a
whole-repository precision estimate.

The holdout runner checks the upstream commits and file digests, runs the map,
and reports every labeled fact found or missed. It fails on a newly lost
baseline fact or an unsupported `complete` coverage claim. Optionally, it
compares legacy JSON against a local Linguist 9.7.0 container with networking
disabled. It does not clone repositories or download container images:

```sh
python3 tests/map_corpus/run_holdout.py \
  --binary ./dircue \
  --repo owasp_java=/path/to/BenchmarkJava \
  --repo owasp_python=/path/to/BenchmarkPython \
  --repo dart=/path/to/appflowy-editor \
  --repo cobol=/path/to/aws-mainframe-modernization-carddemo \
  --linguist-image dircue-linguist:9.7.0 \
  --output .cache/map-holdouts.json
```

The first source-first run found 5 of 18 labeled positives and made no
`complete` coverage overclaims. The gaps it exposed were a Maven WAR and its
build link, a Flask application and its SQLite use, and Dart HTTP-client and
local-path relationships. PR #143 then implemented those as general map
features. Against the labels exactly as frozen in `9fe9cef`, the new map
finds 12 of 18. Four labels named facts in terms the map contract does not
use: a WAR `service` instead of an `archive`, a `builds` edge in the reverse
direction, a README-derived name for an undeclared Python root, and a
`datastore:sqlite` category that the catalog spells `datastore:relational`.
They were restated with source citations, recorded in each file's
`corrections`. Against the corrected labels the map finds all 18, and
`run_holdout.py` now enforces that as a regression floor. Because these four
repositories guided the implementation, they no longer estimate accuracy on
unseen code; `fresh_labels/` does that. `holdout_results.json` records the
latest run, including each label file's corrections. The four legacy JSON
outputs match Linguist exactly. COBOL/JCL has no positive map-graph label
yet; its value is a language-parity and conservative-coverage probe, not
proof of project relationship support (#144).

## Fresh blind check

Before the #143 map changes were run on them, four further repositories were
labeled from source by a labeler who never ran dircue or read its output,
and frozen in `810100d`:
[OpenMRS core](https://github.com/openmrs/openmrs-core) (Maven reactor
with a WAR module), a
[Flask application](https://github.com/gothinkster/flask-realworld-example-app),
the [bloc](https://github.com/felangel/bloc) Dart monorepo, and a
[Prisma/Express application](https://github.com/gothinkster/node-express-realworld-example-app).
The frozen labels were intended to be exhaustive within named oracle files.
The subsequent source review found omissions, including OpenMRS module
membership whose child POMs were outside that original oracle set. Treat the
frozen precision as measured against the original labels, and the corrected
precision as an adjudicated regression score, not a whole-repository estimate.
See `fresh_labels/README.md` for selection and method.

```sh
python3 tests/map_corpus/fetch_fresh.py --dest .cache/fresh-repos
python3 tests/map_corpus/score_fresh.py --binary ./dircue \
  --repos .cache/fresh-repos --frozen \
  --output .cache/fresh-results-frozen-current.json
python3 tests/map_corpus/score_fresh.py --binary ./dircue \
  --repos .cache/fresh-repos \
  --output .cache/fresh-results-corrected-current.json
```

The three committed result files retain the per-fact matches and disagreements,
not just the rounded figures below. The commands write to `.cache/` so a
reproduction does not overwrite a historical receipt.

The first blind receipt is preserved separately in
`fresh_results_first_blind.json`. It records the original main-binary/main-
scorer comparison before later source-label adjudication or scorer changes:
relationships had 15 true positives, 25 false positives, and 2 false
negatives (precision **0.38**, recall **0.88**). The current
`fresh_results_frozen.json` is a regression run on the same frozen labels,
not the original blind receipt. Its edge progression makes the attribution
explicit: main binary + main scorer was 15/25/2; main binary + updated PR
scorer was 16/24/1; PR binary + updated PR scorer was 17/23/0. The latter
results are regression evidence and do not replace the first-run measurement.

| Question | Frozen labels P / R | Corrected labels P / R |
| --- | --- | --- |
| components | 1.00 / 1.00 | 1.00 / 1.00 |
| deployables | 0.80 / 0.80 | 1.00 / 0.83 |
| interfaces | 1.00 / 1.00 | 1.00 / 1.00 |
| capabilities | 0.60 / 1.00 | 1.00 / 1.00 |
| relationships | 0.38 / 0.88 (first blind run) | 1.00 / 1.00 |

The first blind frozen-label result is retained as measured; the labels were
incomplete, so its precision is not a whole-repository estimate. The current
frozen-label regression score is 0.425 / 1.00 after scorer changes and map
changes, as detailed above.
Most extra facts were real: compile-scope AWS S3 and Hibernate declarations,
six other reactor modules, the OpenMRS WAR the labeler had noted but could
not name, and structural containment. Each correction cites the oracle-file
line. The same review found real map errors. #143 fixed a Spring Security
prefix that called password hashing OAuth2, a missing Hibernate Search
Elasticsearch backend in the catalog, unfollowed `-r` includes, unsupported
Pipfiles, a tool-only
`setup.cfg` that created a second Python root, and unlinked Maven sibling
modules. The follow-up to #147 now links OpenMRS's root Dockerfile to the WAR
module through a partial, source-cited `COPY --from` edge. The #149 scorer
fix matches root Dockerfile edge endpoints by unique path. The remaining
known deployable gap is Procfile support (#146). These repositories have
informed development; their corrected scores are regression measurements,
not a new independent accuracy estimate.

## Withdrawn blind holdout history

The earlier `blind_holdout_v1/` artifact covered `gs-maven` and `flasky`.
Commit `7dadebc` introduced it as frozen before either checkout was scored.
Commit `01175ec` then changed its labels to use the map's vocabulary without
adding a `corrections` record. Commit `d9a12d2` added an implementation-aware
flask-on-docker manifest to the same directory. Commit `50e14e9` removed the
artifact and replaced it with `independent_labels/`. Because those changes
broke the
original blind-label provenance, the withdrawn holdout is not treated as a
scored accuracy receipt; the commits preserve the original and revised
source-only labels for audit.

As a transparent post-hoc check, the PR binary was built from the current
source and run against the pinned `gs-maven` `complete/` subtree at
`c65883f80b35bac86bb2580944de9146e2c6a55a`. It emits a `maven_main_class`
interface (`hello.HelloWorld`, sourced to `pom.xml:49`) and a `declares` edge
from the component to that interface. The original frozen source labels
explicitly marked both categories empty and complete, so both observations
conflict with those labels. This is a reported counterexample, not a valid
blind score: the artifact was withdrawn without a result receipt, and this
run happened after implementation. To reproduce the observation, check out
the pinned repository, build the PR source with `go build -o ./dircue .`, and
run `./dircue map --json --source directory /path/to/gs-maven/complete`.

## Independent source-first check

`independent_labels/` contains two further pinned public source oracles,
committed before the candidate binary was run on either checkout. The labeler
read upstream source and the published map vocabulary, but did not inspect the
implementation, tests, previous labels, or output. Each oracle file has a
SHA-256 digest. The Spring Boot Docker sample covers every tracked file in its
`complete/` subtree; the Flask-on-Docker sample is a bounded source slice.

Reproduce the scored run after checking out the commits named in those files:

```sh
python3 tests/map_corpus/score_independent.py \
  --binary ./dircue \
  --flask-checkout /path/to/flask-on-docker \
  --spring-checkout /path/to/gs-spring-boot-docker \
  --output .cache/independent-map-results.json
```

The runner checks the commits and file hashes before scanning. For the
exhaustive Spring subtree it also compares the oracle inventory with Git's
tracked-file list. It reports precision only for categories labeled
exhaustively. The first run found both Spring components and its Dockerfile
deployable with no extra component or deployable observations: **3/3 found,
3/3 observed correct**, within that small subtree. Other categories are
targeted recall only; their extra observations are unadjudicated, not false
positives. The Flask source slice found 2/3 labeled deployables, 2/2
interfaces, 0/1 components, 0/3 capabilities, and 1/8 edges under the
scorer's exact semantic keys. The Spring subtree found 0/3 labeled interfaces,
0/2 capabilities, and 1/3 edges. Some misses are name or vocabulary
disagreements (for example, `services/web` versus `web` and a qualified Java
class name versus its short name); the receipt retains them instead of
silently correcting the source-first labels. This is a small independent
check with useful counterexamples, not a population accuracy estimate or a
passing whole-map quality gate. `independent_results.json` records the first
run, binary and label hashes, and every matched or missed fact.

`independent_flask_triage.json` separately reviews all 12 Flask exact-key misses
against the pinned source and the first-run map (4 equivalent under a different
name or vocabulary, 6 real map misses, and 2 unresolved). It is a post-hoc
qualitative review: the exact-key scores above remain unchanged, and these
targeted categories do not estimate precision. A test checks ledger coverage,
unique keys, allowed categories, citations, and the frozen receipt and label
hashes. See [BUILD_PROVENANCE.md](BUILD_PROVENANCE.md) for reproduction details
of the independent receipt.

Two additional opt-in checks use bounded source slices from public projects
with a checked-in binary referenced by a build declaration. The
[zdh_web Maven POM](https://github.com/zhaoyachao/zdh_web/blob/1e420dcb3ec748011958a34e1d317ad58830c636/pom.xml#L1073-L1079)
references a local JAR through `systemPath`; the
[pyRevit project file](https://github.com/pyrevitlabs/pyRevit/blob/6294cf9c477130eadd73b9d156784f7a5553b4cd/dev/pyRevitLabs/pyRevitLabs.Common/pyRevitLabs.Common.csproj#L8)
references a checked-in DLL through `HintPath`. `unmanaged_binary_slice.py`
fetches the cited declaration files and binary from pinned commits, limits
response sizes, verifies the binary digests and references, and deletes the
files after the run. For pyRevit it also reads the pinned
`dev/Directory.Build.targets`, checks its `net48` to `netfx` property mapping,
and statically substitutes the path properties to show that
`$(PyRevitDevLibsDir)` selects `dev/libs/netfx/pyRevitLabs.Json.dll`. It does
not invoke MSBuild or run repository build logic, vendor binaries, or make a
claim about the complete source repositories:

```sh
python3 tests/map_corpus/unmanaged_binary_slice.py --binary ./dircue \
  > .cache/unmanaged-binary-slices.json
```

The documented run recognized the JAR as archive content and the DLL as binary
content. No package identity was inferred from those roles alone; package
coverage stayed `unknown` without a provider report. This is an inventory
smoke check, not a substitute for an SBOM tool or a test of whether a Syft
report would identify either binary. It requires network access when invoked
and is not part of ordinary CI. `unmanaged_binary_results.json` is the
portable receipt, with the candidate binary hash, source-file hashes, and both
upstream commits; no downloaded binary bytes are checked into this repository.

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
was built from commit `1b567071f54c9b177f25ed9881923d649a3271cf` and had
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
precursor commit `a5b8e35d9032cfb45bcf83c4fdcd2b19e78db5f5` (binary SHA-256
`0c1187da1b0f09fcae1c723806424b36745a81359ccc1fbdecba5365b27fd64d`);
the final 63-check run used the candidate commit and binary named above. Later
changes did not touch the legacy language path. The existing kernel-slice
Linguist gate also passed both JSON breakdown and strategy labels. These checks
establish only their named interfaces; they do not prove map-level parity with
Linguist, which has no equivalent map document.

The expanded Linguist differential also checks legacy JSON breakdown and text
breakdown with strategy labels on all 21 pinned repositories. Its 2026-09-23
run passed 62 of 63 comparisons exactly, with no unexplained differences. The
one known difference is the four-line ASP.NET Core text display discrepancy
recorded as DISC-009 in `tests/conformance/DISCREPANCIES.md`; both JSON modes
match. That run used commit `5c04f33bc38dd453f7a92bb4a1da217b3f3edc1b`
and binary SHA-256
`7bc6d72c4eb15c7fc6479eab977057f3136c514b0dc8c976fb3c8b2e6bfa6afd`.
