# Directory maps

`dircue map` produces a portable description of a selected Git tree or ordinary
directory. It combines language populations, content roles, declared projects,
local project relationships, deployable definitions, interfaces, capabilities,
and explicit coverage in one bounded scan. The command does not build the
project, restore packages, run inspected code, contact a network service, or
invoke another analyzer.

```sh
dircue map --json /path/to/checkout > map.json
dircue map --summary /path/to/checkout
```

JSON is the default when stdout is redirected. A terminal receives the compact
summary unless `--json` is present. `--summary` forces the text view. The JSON
document is the automation contract; the summary deliberately omits evidence,
stable IDs, facts, and most coverage details.

## What the map represents

The top-level `source` identifies the input snapshot. Every observation is a
node, and every supported relationship is an edge between stable node IDs.
Evidence records the path, basis, source kind, and dircue rule or external
provider responsible for an observation.

| Node kind | Examples |
| --- | --- |
| `content` | Language populations and source, test, documentation, generated, configuration, data, binary, archive, certificate, and other content roles. |
| `component` | Supported projects and workspace members inferred from declarations. |
| `deployable` | Container, Compose, Kubernetes, Helm, Terraform, serverless, and CI definitions recognized by the built-in observer. |
| `interface` | Supported declared or syntax-observed interfaces, including protobuf services and attached Noir endpoints. |
| `capability` | Bounded code/configuration observations such as imported services. |
| `package` | Packages imported from an explicitly attached Syft report. |
| `tool_run` | Metadata and snapshot binding for an explicitly attached provider report. |

Relationships include containment and membership, local dependencies, build
and run relationships, exposed interfaces, declared capabilities, package
containment, and provider analysis. A relationship means only what its type
and evidence establish. For example, a declaration can establish a local
reference without proving that the project builds.

Paths are clean, root-relative paths. Node and edge IDs are deterministic
hash-based identities derived from their kind, paths, and discriminator. They
can be compared across maps of the same logical content, but an ID is not a
content checksum.

Export the bundled Draft 2020-12 schema without network access:

```sh
dircue capabilities --schema map --json > map.schema.json
```

## Source selection and binding

Source selection follows the rest of dircue:

- At a usable Git repository root, `--source auto` maps the committed `HEAD`
  tree. Dirty and untracked files are excluded.
- `--source directory` maps current filesystem content and does not require
  `.git`.
- `--source git` requires committed Git content.
- `--rev REV` chooses a Git revision. `--tree ID` chooses an exact tree object.

A Git map records the exact selected tree object. Its `revision` field records
the caller's revision expression, which can be symbolic, such as `HEAD`. A
symbolic revision is not treated as equal to an external tool's commit hash.
A provider binding is verified only when comparable snapshot identities match.

The CLI does not calculate a full-content digest for a live directory. Such a
map reports `source_binding: unknown` with
`live_directory_has_no_full_content_digest`. Embedding producers can supply the
optional directory digest defined by the map schema. Absent that digest,
attached reports and SARIF locations can still be joined by path, but the
output does not claim they came from the same immutable snapshot.

## Coverage and limits

Successful execution means dircue produced a valid map; it does not imply that
every question was answered completely. Inspect both top-level `status` and
each entry in `coverage`.

| Coverage status | Meaning |
| --- | --- |
| `complete` | The named observer completed for its declared scope. |
| `partial` | Some relevant input was omitted, bounded, unreadable under `--on-error continue`, or supplied with unverified provider binding. |
| `unknown` | The observer did not have evidence sufficient to answer the question. |

The default inventory budget is 100,000 source entries. `--budget-files N` and
`--tree-size N` set the same inventory limit and cannot be combined. Crossing
the map budget returns a valid partial map and exit status `0`; the content
coverage reasons record `tree_size_limit`. `--max-file-bytes N` changes which
files can contribute content evidence and therefore changes coverage. It is
not a hard process-memory limit.

Warnings go to stderr. JSON remains on stdout. Treat a zero exit status,
warning-free stderr, and complete coverage as separate facts.

## Control execution and coverage

The default `balanced` preset retains automatic worker selection and the
100,000-entry inventory limit. Other presets make a small set of named choices:

| Preset | Current effect |
| --- | --- |
| `balanced` | Automatic workers and the default inventory limit. |
| `fast` | Use 16 file workers. This is an execution preference and must preserve answers. |
| `low-memory` | Use two file workers. This is a relative preference, not a hard RSS ceiling. |
| `thorough` | Raise the inventory limit to 250,000 entries; this can replace a partial answer with a more complete one. |

Inspect effective values, categories, and origins without scanning:

```sh
dircue map settings
dircue map settings --preset low-memory --json
dircue map settings --preset fast --set workers=8 --json
```

`--set` accepts `workers`, `inventory.files`, and `content.file_bytes`. A named
flag such as `--workers`, `--budget-files`, `--tree-size`, or
`--max-file-bytes` has the highest precedence, followed by `--set`, then the
preset. The settings report labels worker concurrency `performance-only` and
the two skip/inventory bounds `coverage-affecting`.

`GOMEMLIMIT` remains a cooperative Go runtime control inherited from the
process environment. It is not a hard process limit. Use operating-system or
container controls when a workload requires enforced CPU, memory, or wall-time
bounds.

## Attach saved provider reports

`--attach KIND=PATH` joins an existing report into the map. The flag is
repeatable. Dircue reads the file as bounded JSON data and never runs the
provider.

```sh
dircue map --json \
  --attach syft-json=syft.json \
  --attach sarif=analysis.sarif \
  --attach noir-json=noir.json \
  /path/to/checkout > enriched-map.json
```

Supported attachment kinds are:

- `syft-json`: package nodes, supported package relationships, reported file
  locations, provider metadata, and snapshot binding.
- `sarif`: run/tool metadata, reported artifact coverage, invocation state,
  ownership links, and snapshot binding. Findings, rules, severity, and verdicts
  are intentionally outside this importer.
- `noir-json`: endpoint interfaces, supported parameters, ownership links,
  reported files, and snapshot binding.

An attachment without comparable source identity is retained with an `unknown`
binding. A mismatched identity is not silently promoted to repository evidence.
Provider coverage replaces the corresponding `packages`, `analyzer_coverage`,
and `routing` question entries in the resulting document. Each attached run
also receives a typed `coverage_ledger` entry with its tool, report kind,
scope, binding, run state, and reported files. This ledger records only what
the supplied report disclosed. It is not comprehensive per-component,
per-language blind-spot accounting, and an empty `covered_files` array means
unknown coverage rather than a clean run. The default per-file attachment bound
is 32 MiB and the default record bound is 100,000.

## Route follow-up tools

Routing reads a saved map and emits deterministic plans without rescanning the
source:

```sh
dircue map route --json map.json > routes.json
```

Plans describe applicability, scope, prerequisites, and report kind. A plan has
an argv template only where dircue carries a reviewed recipe; its values contain
placeholders such as `{scope}` and `{report}`. Other tools return an empty argv
plus the unmet `exact_invocation` prerequisite. Templates are data, not approved
shell commands. Dircue does not inspect `PATH`, check installed tool versions,
download anything, or execute a plan. The caller must choose tools, substitute
confined paths, apply resource policy, and validate the final invocation.

Current route descriptors cover Syft, scc, BCA, Noir, OpenTaint, and Bifrost
where the map contains applicable evidence. A routing descriptor does not mean
that dircue can import that tool's report; attachment support is the narrower
list above.

## Compare saved maps

```sh
dircue map compare --json before.json after.json > map-change.json
```

Comparison uses stable IDs and opens neither source tree. It separates material
node/edge changes from evidence-only and coverage-only changes. If the head map
has incomplete coverage, a missing observation is reported as an indeterminate
removal rather than a proven deletion. The caller selects the pair; dircue does
not infer repository identity or rename relationships. A valid comparison exits
`0` even when changes are present.

`dircue map compare` consumes map documents. The older `dircue compare`
command consumes aggregate `analyze all` profiles; their input and output
contracts are different.

## Locate SARIF results in the map

`map locate` adds a `dircue.map` property to physical SARIF result locations:

```sh
dircue map locate map.json results.sarif > located.sarif
dircue map locate --summary map.json results.sarif > location-summary.json
```

Annotations can name owning components, deployables, interfaces, and a content
role. Resolution is one of `resolved`, `ambiguous`, `not_in_snapshot`,
`outside_root`, or `unresolvable_uri`. Existing SARIF fields and location
properties are preserved.

Use `--source-uri /selected/root` only to relativize absolute SARIF artifact
URIs. The value is not copied into output. Windows source paths are matched
case-insensitively; other paths are case-sensitive by default. Directory maps
with a producer-supplied digest can pass the corresponding SARIF snapshot
identity as `--source-digest ALGORITHM:SCOPE:VALUE`. This flag cannot make an
ordinary undigested directory map immutable after the fact.

Inputs are limited to regular files. The saved map limit is 64 MiB. SARIF is
limited to 64 MiB, 64 runs, 1,000,000 results, and 4,000,000 locations. The
command supports SARIF 2.1.0 physical locations; logical-only locations and
unsupported or escaping URIs remain unresolved.

## Current boundaries

- The map is a static evidence graph, not a build graph produced by evaluating
  arbitrary build logic.
- Dynamic dependency resolution, compiler type information, call graphs, and
  runtime behavior are outside the one-shot map.
- Project and deployable recognition is limited to documented built-in
  adapters. Unknown syntax remains unknown rather than guessed.
- Repository prose cannot establish components, interfaces, deployables, or
  capabilities. Documentation remains a distinct content observation.
- Attached provider reports are trusted as caller-supplied data, with source
  binding and coverage disclosed. Dircue does not attest that a provider ran
  correctly or completely.
- The map command uses bounded native observers. Optional Tree-sitter/BCA
  structural analysis remains a separate explicit workflow.

For exact command and flag metadata from an installed binary, run:

```sh
dircue map --help
dircue map settings --help
dircue map compare --help
dircue map route --help
dircue map locate --help
dircue capabilities --cli --json
```
