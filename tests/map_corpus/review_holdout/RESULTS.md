# Frozen review holdout: first-run results

Labels were frozen in commit `0fb62770cc7faa80ab04c195dfc0e3d6f473bbaf` before the candidate was built or run on any target repository. The candidate source was `0fb62770cc7faa80ab04c195dfc0e3d6f473bbaf`; it was built with Go 1.26.6 as `CGO_ENABLED=0 go build -buildvcs=false -trimpath`. The binary SHA-256 is recorded in `receipts/build.json`. One Git-source map invocation ran for each pinned commit. All four exited 0 and emitted valid JSON. `receipts/*.receipt.json` records pins, tree IDs, commands, times, stdout/stderr hashes, and exit statuses; `receipts/raw/` preserves the byte-exact JSON and stderr.

The bounded positive score is 15/29 raw-exact matches (51.7%) and 27/29 source-fact matches after explicit name and vocabulary normalization (93.1%). The raw-exact score requires labels to use the same serialized names and categories as map output. Normalization only accepts the same source path and fact type, with these recorded differences: Go module name versus local component name; PyPI versus `python`; Dockerfile deployable `(root)` names; port interface names (`5000` versus `port:5000`); and npm `npm_start` versus interface kind `script`. These score different questions, so the normalized score should not be read as a schema-exact pass rate.

The two positive assertions absent from both raw and normalized matching are Compose `runs` edges: `docker-compose.yml:changedetection → changedetection.io` and `docker-compose.yml:umami → umami`. The frozen labels inferred image identity from package-name similarity. [`docs/MAP.md`](../../../docs/MAP.md) defines `runs` using a matching declared image identity; the source files do not explicitly declare that identity for the local component. These are first-run discrepancies requiring adversarial label adjudication, not established map defects.

The 12 bounded negative assertions yield 10 not contradicted by output, 0 contradicted, and 2 unscored because the map evidence fell outside the frozen oracle-file scope. The two unscored Chi checks concern a capability edge evidenced by Go source while the negative was scoped to `go.mod`, and nested example binaries attached to the root component while the negative oracle was `chi.go`. Chi also raises a separate semantic concern: the map attributes `net:http-client` to the root component with `net/http` import evidence, although the router source uses `net/http` for server-side handler interfaces. The map additionally assigns binary interfaces from `_examples/` paths to the root component. Both deserve review of client/server classification and `_examples` role/ownership.

The output contains further unscored observations: FastAPI's optional `httpx` and `PyYAML` extra dependencies plus a test-only SQLAlchemy import; extra npm components and a `crypto:library` capability for Changedetection; and Umami workspace components plus Redis, relational-database, and Kafka capabilities. Chi's nested example components and binaries are likewise outside the frozen positive scope. These are not false positives under this bounded label set.

Every map reports `partial` overall status. Component, deployable, interface, and capability coverage is `partial` because the recognizers are bounded; content and source binding are `complete`. These results cover four repositories and 29 selected positive facts, not whole-repository precision or recall. Negative non-contradiction is not proof of absence.

Reproduce scoring without rerunning maps with:

```sh
python3 tests/map_corpus/review_holdout/score_first_run.py --write
```

`run_once.py` refuses to overwrite an existing receipt directory, verifies pinned commits and clean checkouts, and applies a 300-second timeout to future invocations. The recorded first run completed before that timeout was added; its receipts reflect the actual first-run return values and hashes.
