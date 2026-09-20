# Compare saved profiles

Added in 0.5.0.

```sh
dircue analyze all --declarations --discovery --json /checkout > before.json
# Save another report after selecting the intended later source state.
dircue analyze all --declarations --discovery --json /checkout > after.json
dircue compare before.json after.json --json > changes.json
```

`compare` reads the two supplied files. It does not open their declared roots, follow evidence paths, rescan content, fetch Git history, or invoke another tool. Keep report files outside the inspected directory when taking filesystem snapshots so they do not become part of the next inventory.

Inputs must be aggregate dircue JSON reports using supported schema versions 1.0–1.4. The language-only object from `dircue --json` or `github-linguist --json` does not carry the required aggregate contract. Malformed JSON, duplicate keys, unknown fields, invalid schema values and unsupported schema versions fail explicitly. Input validation uses bundled schemas and does not retrieve remote resources.

## Read compatibility before changes

The caller chooses the pair. Matching root names, hashes or package names do not prove repository identity or report authenticity. `base.report_sha256` and `head.report_sha256` identify the exact input bytes; they are not source-tree signatures.

Each module reports its comparison `scope`, `compatibility`, input statuses, reasons, metadata differences and observation changes.

| Compatibility | Meaning |
| --- | --- |
| `compatible` | The supplied module provenance and selection policy permit comparison within the stated scope. |
| `observed_only` | Saved observations can be compared, but missing provenance or incomplete coverage limits interpretation. A difference does not establish a source-code change. |
| `incomparable` | Provider, scope or policy differs, matching identities are ambiguous, or detailed comparison is unsupported. |
| `unavailable` | One side lacks the module or the requested work was skipped. |

A module missing from one report is unavailable, not an empty result. With incomplete input coverage, a missing observation is not necessarily a removal. The change is marked unavailable when the absent side cannot establish absence. A retained observation can still be compared with its counterpart.

Language, finding, project and metrics reports, including those produced by 0.5.0, do not record every provider or selection detail needed for stronger attribution. Their comparisons remain qualified even when their values match. Changes in module provider, scope, limits or rule policy appear separately from changes in observed entities. A changed Git tree is source metadata, not a reason to suppress an otherwise applicable comparison.

## Compared populations

- Language totals, inventory summary, ecosystem/framework/layout findings.
- Existing project declarations, plus the richer opt-in declarations module.
- Discovery counts and candidates, registry declarations and caller-rule observations.
- Imported package, relationship and file observations, with imported-source qualifications retained.
- Aggregate line metrics and a separate `metrics_files` module when both reports include per-file details.

Detailed structural/function and graph comparisons are not supported in this version; their presence is reported explicitly. Renames are not inferred. Project IDs are manifest paths. Per-file metrics use paths; imported package and file observations retain provider IDs. Compound observation identities preserve their component boundaries. Relevant denominators accompany compared language and metric populations.

Collections are compared independently of presentation order. Large field values are represented by canonical-value digests and sizes instead of being copied in full. Thus a field can be known to differ even when its value is omitted from the comparison output.

```sh
jq '.modules[] | {name, scope, compatibility, reasons, counts}' changes.json
jq '.modules[] | select(.name == "declarations") | .changes' changes.json
```

## Bounds and process results

| Bound | Maximum |
| --- | ---: |
| Bytes per input report | 32 MiB |
| JSON nesting depth | 64 |
| JSON nodes per input | 1,000,000 |
| Individual decoded string | 64 KiB |
| Retained changes across modules | 4,096 |
| Retained field value | 2 KiB; larger values keep a digest and size |
| Evidence paths per side of a change | 16, each at most 1 KiB |
| Structured comparison output | 8 MiB |

Input files must be regular files; symlinks are refused. The command does not accept scan-selection flags such as `--source`, `--rev`, or `--workers`. Limits are validation and output bounds, not a process-memory ceiling.

A successful comparison exits zero even when observations differ or a module is unavailable. Invalid input, failed reads and failed writes return an error. The top-level comparison status describes whether comparison output was retained completely; it does not upgrade the coverage or compatibility of any input module. Inspect module statuses and reasons as well as that top-level status. Text output shows at most 200 changed observations; JSON exposes the bounded full result and omission counts.

The output uses its own [comparison schema 1.0.0](../schema/comparison.schema.json), identified by `kind: dircue-report-comparison`. It is distinct from aggregate profile schema versions.
