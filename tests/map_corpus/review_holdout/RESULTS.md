# Frozen review holdout: first-run results

The labels were authored from source by the same team that develops the detector, before the first map run on these repositories. They are source-first, not independent.

Labels were frozen in commit `0fb62770cc7faa80ab04c195dfc0e3d6f473bbaf` before the candidate was built or run on any target repository. The candidate source was `0fb62770cc7faa80ab04c195dfc0e3d6f473bbaf`; it was built with Go 1.26.6 as `CGO_ENABLED=0 go build -buildvcs=false -trimpath`. The binary SHA-256 is recorded in `receipts/build.json`. A fresh build from `git archive` of that commit with the recorded command reproduced the binary hash `eea45058364ef25557b510e35371e97b79244e5e0f0375017b87234ef3d97b7b`. One Git-source map invocation ran for each pinned commit. All four exited 0 and emitted valid JSON. `receipts/*.receipt.json` records pins, tree IDs, commands, times, stdout/stderr hashes, and exit statuses; `receipts/raw/` preserves the byte-exact JSON and stderr.

The bounded positive score is 15/29 raw-exact matches (51.7%) and 27/29 source-fact matches after explicit name and vocabulary normalization (93.1%). The raw-exact score requires labels to use the same serialized names and categories as map output. Semantic matching accepts only these defined identities: the emitted component name or its declared Go `go_module`; a Dockerfile container-build name `(root)`; an exact deployable name otherwise; a capability with the labeled source path and owning component; and the mapped vocabulary pairs PyPI/`python`, declared port `5000`/node `port:5000`, and npm `npm_start`/interface kind `script`. Other name differences do not count as matches. These score different questions, so the normalized score should not be read as a schema-exact pass rate.

The two positive assertions absent from both raw and normalized matching are Compose `runs` edges: `docker-compose.yml:changedetection → changedetection.io` and `docker-compose.yml:umami → umami`. The frozen labels inferred image identity from package-name similarity. [`docs/MAP.md`](../../../docs/MAP.md) defines `runs` using a matching declared image identity; the source files do not explicitly declare that identity for the local component. These are first-run discrepancies requiring adversarial label adjudication, not established map defects.

The 12 bounded negative assertions yield 10 not contradicted by output, 0 contradicted, and 2 unscored because the map evidence fell outside the frozen oracle-file scope. The two unscored Chi checks concern a capability edge evidenced by Go source while the negative was scoped to `go.mod`, and nested example binaries attached to the root component while the negative oracle was `chi.go`. Chi also raises a separate semantic concern: the map attributes `net:http-client` to the root component with `net/http` import evidence, although the router source uses `net/http` for server-side handler interfaces. The map additionally assigns binary interfaces from `_examples/` paths to the root component. Both deserve review of client/server classification and `_examples` role/ownership.

The output contains further unscored observations: FastAPI's optional `httpx` and `PyYAML` extra dependencies plus a test-only SQLAlchemy import; extra npm components and a `crypto:library` capability for Changedetection; and Umami workspace components plus Redis, relational-database, and Kafka capabilities. Chi's nested example components and binaries are likewise outside the frozen positive scope. These are not false positives under this bounded label set.

After inspecting this first-run result, the detector was changed to require a recognized client API reference before a Go `net/http` import yields `net:http-client`. A development-guided check of Chi then removed the capability evidence from the server-only `chi.go` file (one evidence item before, zero after). On the final branch, which also excludes `http.NewRequest` and `net/http` sub-packages such as `httptest`, the root component keeps the capability only through four test files that call a local test server (`mux_test.go` and three middleware tests). Test-only capability evidence is tracked in #177. This later check does not replace the frozen first-run receipt or provide an independent score for the final branch.

Every map reports `partial` overall status. Component, deployable, interface, and capability coverage is `partial` because the recognizers are bounded; content and source binding are `complete`. These results cover four repositories and 29 selected positive facts, not whole-repository precision or recall. Negative non-contradiction is not proof of absence.

Reproduce scoring without rerunning maps with:

```sh
python3 tests/map_corpus/review_holdout/score_first_run.py
```

The scorer verifies each label file against its blob in the label-freeze commit `0fb6277`, so it needs a checkout containing that commit. Merge this work with a merge commit, because squash and rebase merges drop it. The Linux CI job keeps full history, reruns the scorer, and requires its output to equal `receipts/score.json`.

## Adjudication after the first run

`adjudications.json` records three source-cited corrections made after the first run was inspected. Each correction cites file and line at a pinned commit; the scorer rejects an entry without pinned citations or one that names no scored assertion, and checks this repository's own citations against the cited commit.

- **Both Compose `runs` positives are dropped as label errors.** `runs` requires an image matching the component's declared image identity, and neither repository declares one for the local component. The labels inferred it from name similarity.
- **Chi's package-clause citation is corrected** from `chi.go:1` to `chi.go:57`. No score changes.

The adjudicated score is 27/27 source-fact matches and 15/27 raw-exact matches. It is not an accuracy estimate: the only assertions removed were the two first-run misses, and the adjudication was made after seeing output. The frozen first-run score above remains the measurement.

`run_once.py` refuses to overwrite an existing receipt directory, verifies pinned commits and clean checkouts, and applies a 300-second timeout to future invocations. The recorded first run completed before that timeout was added; its receipts reflect the actual first-run return values and hashes.
