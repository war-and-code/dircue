# Map accuracy on the labeled repository corpus

The seven repositories below were initially labeled by hand from source files, before the labelers ran dircue or saw its output. The labels and map implementation were then corrected during comparison, and this corpus became a regression suite. The scores below describe the final implementation against those corrected labels; they are not an independent holdout measurement. `tests/map_corpus/verify_golden.py` performs the comparison.

## Results

| Question | Precision | Recall | TP | FP | FN |
|---|---|---|---|---|---|
| components | 1.00 | 1.00 | 15 | 0 | 0 |
| deployables | 1.00 | 1.00 | 40 | 0 | 0 |
| interfaces | 1.00 | 0.98 | 46 | 0 | 1 |
| capabilities | 1.00 | 1.00 | 46 | 0 | 0 |
| edges | 0.995 | 0.98 | 216 | 1 | 4 |

Every question meets the gate (`make golden`): precision ≥ 0.90 and recall ≥ 0.80.

**Coverage statuses.** The map never claimed `complete` where the labels did not; that would be an overclaim. In 11 of 28 question checks, the map was more conservative than the labeler. For example, it reported `partial` components where the labeler judged the manifests exhaustive. That's recorded but doesn't count as a pass.

## Corpus

| Repository | Pinned commit | Oracle files |
|---|---|---|
| aws-sam-java-rest | `e10df9e4cd02` | 2 |
| loki | `9e2f79fa11e4` | 15 |
| mastodon | `2b0e8b47fa79` | 6 |
| ruff | `ea544ce22a7d` | 5 |
| spring-petclinic | `818c4136ea97` | 7 |
| superset | `8141d666d6de` | 7 |
| terraform-aws-vpc | `b3abd6df2ecf` | 7 |

For each repository, a set of **oracle files** bounds the evaluation. The files cover manifests, container and Compose definitions, Kubernetes, Terraform, SAM, entry-point sources and protobuf definitions. Every fact that should follow from those files is labeled, and only map output evidenced by those files is scored. That makes precision measurable, but it is not a whole-repository claim.

## How the labels were produced and corrected

- **Initial labels.** Before comparing results, the labelers read `docs/MAP.md` for vocabulary and repository files for facts, citing a file and line for each label. The final corpus was subsequently used to correct both labels and implementation, as described below.
- **Semantic matching.** Nodes match on semantic keys: name, root and ecosystem for components; kind, name and path for deployables and interfaces; capability and owner for capabilities; type and endpoints for edges. Hashed IDs are never used. `contains` labels match the map's reversed `member_of` edges.
- **Corrections.** Every change to a label after the first comparison is recorded, with its reason, in `golden_expectations.json` (`corrections`). Two rules held throughout: no label was added because dircue emitted it without checking the source, and no label was removed because dircue missed it.
  - Some corrections restate a label in the map's vocabulary. For example, "the Dockerfile produces an image" became "the Dockerfile builds the component at its build context", per the `builds` definition in `docs/MAP.md`.
  - Others add facts the labelers did not enumerate, derived mechanically from the manifests: Cargo and npm workspace members, and `file:` dependencies.
- **Fixes to dircue.** Disagreements that were dircue errors were fixed in the map:
  - declared ports from Compose and Kubernetes, and `EXPOSE` with `ARG`/`ENV` defaults;
  - `.` in npm workspaces;
  - development-only dependency groups;
  - optional dependencies (now conditional);
  - missing catalog entries;
  - Compose `depends_on` edges;
  - runtime prerequisites no longer reported as interfaces.
- **Out of scope for the 1.0 map.** These labels are listed as `non_goals` and excluded from scoring:
  - image nodes for externally built or pulled images;
  - one ecosystem's artifact packaged into another's (a Rust binary in a Python wheel);
  - Helm chart packaging;
  - capabilities implied by per-function IAM policies.

## Remaining disagreements

See `tests/map_corpus/DISAGREEMENTS.md`.

## Reproduce

```sh
python3 tests/map_corpus/fetch_golden.py --dest .cache/golden-repos
make golden
```
