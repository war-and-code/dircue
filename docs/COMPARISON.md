# Compare saved profiles

Added in 0.5.0.

```sh
dircue analyze all --declarations --discovery --json /checkout > before.json
# Save another report after selecting the intended later source state.
dircue analyze all --declarations --discovery --json /checkout > after.json
dircue compare before.json after.json --json > changes.json
```

`compare` reads the two supplied files. It does not open their declared roots, follow evidence paths, rescan content, fetch Git history, or invoke another tool. Keep report files outside the inspected directory when taking filesystem snapshots so they do not become part of the next inventory.

Inputs must be aggregate dircue JSON reports using supported schema versions 1.0–1.8. The language-only object from `dircue --json` or `github-linguist --json` does not carry the required aggregate contract. Malformed JSON, duplicate keys, unknown fields, invalid schema values and unsupported schema versions fail explicitly. Input validation uses bundled schemas and does not retrieve remote resources.

From 0.8.0, comparison accepts the focused and source-availability reports introduced in schema 1.6.0. It also accepts schema 1.7.0 environment reports and schema 1.8.0 lockfile reports. From 1.2, comparison retains environment requirements, .NET selections and declared Python/Node/Rust toolchains, plus lockfile associations and named checks. These are observation comparisons: partial evidence and environment provider-scope changes keep absence uncertain. Neither a matching lockfile check nor an unchanged toolchain declaration verifies an effective build environment. Explanation comparison remains unsupported. Other supported modules in these reports can still be compared.

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

Language, finding, project and metrics reports, including those produced by 0.5.0, do not record every provider or selection detail needed for stronger attribution. Their comparisons remain qualified even when their values match. Language rows, analyzed-file counts, or language-byte totals establish that the legacy language population ran; when the report has no warnings, a language missing from that population can be reported as added or removed. Warnings keep absence uncertain. An all-zero summary with an empty language array remains ambiguous because a module-only command has the same shape, so missing observations are unavailable. Legacy finding arrays lack an equivalent execution signal and remain unable to establish absence. Changes in module provider, scope, limits or rule policy appear separately from changes in observed entities. A changed Git tree is source metadata, not a reason to suppress an otherwise applicable comparison.

## Compared populations

- Language totals, inventory summary, ecosystem/framework/layout findings.
- Existing project declarations, plus the richer opt-in declarations module.
- Discovery counts and candidates, registry declarations and caller-rule observations.
- Imported package, relationship and file observations, with imported-source qualifications retained.
- Aggregate line metrics and a separate `metrics_files` module when both reports include per-file details.
- Format evidence by path, with inspected population, read extent and supported validation profiles.
- Focused primary and related project populations, shared context, affected projects, and separate per-population metrics. Different project selections or counting policies are incomparable; incomplete focus coverage cannot establish removals.
- Source-availability evidence, including LFS pointers, Gitlinks, submodule declarations and checkout observations. Different prerequisite coverage qualifies reference observations; a changed Git tree alone does not prevent comparison.
- Hotspot distributions and retained top evidence by language, grammar, syntax cohort and metric.

Detailed per-file structural/function and graph comparisons remain unsupported; their presence is reported explicitly. The separate `hotspots` comparison describes measured distributions and rankings. It does not match function identities or infer deletion when an entry leaves a top-ten list. Missing populations remain unavailable, and changes to provider, rule or selection policy are incomparable. Renames are not inferred. Project IDs are manifest paths. Per-file metrics use paths; imported package and file observations retain provider IDs. Compound observation identities preserve their component boundaries: JSON retains the stable encoded IDs, while text output quotes their individual components. Relevant denominators accompany compared language and metric populations.

Collections are compared independently of presentation order. Large field values are represented by canonical-value digests and sizes instead of being copied in full. Thus a field can be known to differ even when its value is omitted from the comparison output.

Availability reference correlations require declaration evidence from the same source mode and selected tree within each input report. Reports that mix those snapshots are rejected. The base and head reports may still name different Git trees: that is the ordinary revision delta being compared, not a policy incompatibility.

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
| Numeric token | 128 bytes; decimal exponent magnitude at most 1,024 |
| Retained changes across modules | 4,096 |
| Retained field value | 2 KiB; larger values keep a digest and size |
| Evidence paths per side of a change | 16, each at most 1 KiB |
| Structured comparison output | 8 MiB |

Input files must be regular files; symlinks are refused. The command does not accept scan-selection flags such as `--source`, `--rev`, or `--workers`. Numbers must fit their report field types. Floating-point fields reject nonfinite values and nonzero values that underflow to zero; representable subnormal values remain supported. Numeric tokens retain their original precision during validation. Limits are validation and output bounds, not a process-memory ceiling.

A successful comparison exits zero even when observations differ or a module is unavailable. Invalid input, failed reads and failed writes return an error. The top-level comparison status describes whether comparison output was retained completely; it does not upgrade the coverage or compatibility of any input module. Inspect module statuses and reasons as well as that top-level status. Text output shows at most 200 changed observations; JSON exposes the bounded full result and omission counts.

The output uses its own [comparison schema 1.0.0](../schema/comparison.schema.json), identified by `kind: dircue-report-comparison`. It is distinct from aggregate profile schema versions.

When global retention limits bind, the comparison shares its change and byte budgets across modules instead of allowing early populations to consume all capacity. Counts remain complete for compared entities even when their detailed changes are omitted. Text includes each module’s omitted-change count, and its 200-row notice appears only when it actually hides retained rows. Input statuses remain distinct from the availability of a comparison implementation.
