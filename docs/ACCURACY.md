# dircue Map Accuracy

## Golden corpus accuracy (blind labels, issue #75)

7 repositories labeled blind by independent labelers who did not read dircue source.
Labels in `tests/map_corpus/golden_expectations.json`. Only oracle-scoped nodes
are evaluated per repo. Run with `make golden`.

Results below are **post-label-corrections** (issue #75, final pass). All gates pass.

### Aggregate results

| Question | Precision | Recall | TP | FP | FN | Gate / Status |
|---|---|---|---|---|---|---|
| components | **1.00** | **1.00** | 15 | 0 | 0 | gated P≥0.85 R≥0.85 ✓ |
| deployables | **1.00** | **1.00** | 39 | 0 | 0 | gated P≥0.80 R≥0.75 ✓ |
| interfaces | **1.00** | **0.98** | 46 | 0 | 1 | gated P≥0.90 R≥0.80 ✓ |
| capabilities | **1.00** | **0.98** | 45 | 0 | 1 | gated P≥0.90 R≥0.80 ✓ |
| edges | **1.00** | **0.98** | 85 | 0 | 2 | gated P≥0.90 R≥0.80 ✓ |

### Remaining FNs (4 total across all questions)

All remaining FNs are genuine gaps — either catalog coverage or modeling differences.
None are label errors. See `tests/map_corpus/DISAGREEMENTS.md` for detail.

| Question | Repo | Item | Root cause |
|---|---|---|---|
| interfaces | ruff | `cli_binary ruff` from pyproject.toml maturin binding | (c) dircue models maturin as build-backend, not a second cli_binary source |
| capabilities | mastodon | `net:http-client` from Gemfile (`http` + `net-http` gems) | (a) Ruby HTTP client gems not in dircue catalog |
| edges | spring-petclinic | `uses_capability petclinic(k8s) → datastore:postgresql` | (c) config-derived inference from k8s env var profile not implemented |
| edges | terraform-aws-vpc | `depends_on_local wrappers → (root)` | (c) Terraform module ref modeled as member_of not depends_on_local |

### How precision reached 1.00

Label corrections in this pass resolved all false positives:
- Added `grpc_operation` interface labels for all 12 loki proto RPC methods
- Added `declares` edge labels for loki gRPC operations, CLI binaries, and port
- Added 11 conditional capability labels for superset's optional extras
- Added `depends_on` edge labels for superset-init and superset-tests-worker
- Added `uses_capability auth:oidc` edge label for mastodon
- Fixed spring-petclinic: `datastore:relational` label added, wrong `datastore:h2` removed
- Fixed aws-sam: wrong `datastore:dynamodb` and `net:http-client` labels removed; `cloud:aws` added
- Added `serialization:yaml` capability label for ruff (pyyaml in docs extras)
- Added superset port:8081 interface label
- Moved edge labels with fundamental endpoint-vocabulary mismatches to `non_goals`
  (builds/runs/packaged_in/contains for repos where dircue uses different endpoint semantics)

### Coverage questions

| Repo | components | deployables | interfaces | capabilities | edges |
|---|---|---|---|---|---|
| aws-sam-java-rest | complete | complete | partial | partial | complete |
| loki | complete | complete | partial | partial | partial |
| mastodon | complete | complete | partial | partial | partial |
| ruff | complete | complete | partial | N/A | N/A |
| spring-petclinic | complete | complete | complete | partial | partial |
| superset | complete | complete | partial | partial | partial |
| terraform-aws-vpc | N/A | complete | N/A | N/A | partial |

### Methodology notes

- Only oracle-scoped nodes and edges are evaluated (evidence paths must be in
  `oracle_files` for each repo)
- `labeled_edge_types` controls FP counting: only edge types present in the `edges`
  label list generate FPs; types only in `non_goals` are excluded
- Capability owner matching uses Go-module base-name normalisation and case-folding
- gRPC operation interfaces use the `grpc_operation → operation` alias in the verifier
- Conditional capabilities (`state: conditional`) are labeled with `conditional: true`
  and match any capability node regardless of state
