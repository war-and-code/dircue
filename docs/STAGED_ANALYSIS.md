# Choose follow-up analysis from an initial profile

Start with a profile of the directory, then decide which additional work its contents justify. Metrics, structural parsing, and external package inventories answer different questions; none needs to run merely because another ran.

The consumer example below uses the schema 1.2.0 project contract introduced in
0.3.0, which existing commands still support. For a pass limited to metadata,
[`analyze discovery`](DISCOVERY.md) inventories selected files without reading their
source payloads. Its manifest and artifact candidates can
help choose whether to request project declarations, language analysis, or
another inventory. Filename hints cannot validate content formats or establish
that follow-up work is unnecessary. Its schema `1.3.0` is not accepted by the
0.3.0 routing example below.

## First pass

```sh
dircue analyze all --projects --source directory --json /path/to/checkout
```

This command reports language totals, ecosystem/framework/layout findings, project declarations, and file/byte composition. It does not enable scc metrics, invoke the structural worker, or run Syft. It reads bounded file prefixes and recognized manifests, so it is more work than a filename listing. Runtime still depends on the number of entries, storage, attributes, and manifest contents.

Use a stable input directory throughout the stages. `--source directory` selects current files on disk, including working files absent from committed Git history. Without it, automatic selection at a Git repository root ordinarily reads committed `HEAD`. A Git profile and a later filesystem scan can therefore describe different inputs. The project report records `projects.source`; Git reports also identify `projects.tree`. Directory reports do not contain an immutable snapshot identifier.

The command emits aggregate schema `1.2.0`. Legacy `dircue --json` emits only Linguist-compatible language totals and cannot supply this discovery evidence. In particular, `{}` does not establish that a directory is empty or lacks packaged software.

For supported manifest details in 0.5.0, request a separate bounded pass:

```sh
dircue analyze declarations --source directory --json /path/to/checkout
```

This command reads supported manifests while avoiding language classification of
unrelated contents. It can establish declared workspace membership, requirements,
local references and named interfaces for supported ecosystems. Check its states,
diagnostics and coverage before interpreting missing relationships. It does not
run the declared interfaces or determine which package-manager invocation succeeds.
See [project declarations](DECLARATIONS.md).

For bounded format evidence on mixed data or artifact directories, use
[`analyze formats`](FORMATS.md). For measured function distributions after selecting
source for deeper parsing, use [`analyze structure --hotspots`](HOTSPOTS.md) with
an explicitly supplied worker. Both are separate choices; metadata discovery
stays free of source-payload reads.

## Read evidence before selecting work

| Report field | What it can establish | What it does not establish |
| --- | --- | --- |
| `languages` | Languages contributing to the selected statistics | All content types; generated, vendored, binary, and data files may be excluded |
| `ecosystems`, `frameworks`, `layouts` | Detector observations with roots and evidence paths | A complete dependency inventory or a working build |
| `projects.projects` | Recognized manifests and observed declarations, with project roots | Every possible project, evaluated build membership, or installed toolchains |
| `projects.composition` | File and byte counts by observed role, including data, binary, vendor, and unknown content | File-format inventory, archive contents, or a finding that arbitrary XML is a log |
| `projects.status`, `omitted_files`, `diagnostics`, and top-level `warnings` | Whether supported work finished, and recorded limitations | Universal understanding of all formats when status is `complete` |
| Project references and configuration candidates | Declared relationships and possible shared configuration | Permission to scan each root independently without ancestor/workspace context |

Examine file counts and project evidence independently of byte percentages. A tiny `.csproj` beside 2 GiB of XML data still warrants project consideration. XML manifests such as `.csproj` and `pom.xml` have a separate discovery path; they are not treated as ordinary XML data simply because XML is absent from language statistics.

Binary and vendored content may justify a package inventory even when there is no source code. The current composition report does not enumerate every archive or binary format. Vendor files can be counted without reading their contents, and filename-based manifest discovery is intentionally broader than detailed build parsing. Unknown content needs a fallback decision, not an assumption that no follow-up is useful.

## An executable consumer example

[route.py](../examples/staged_analysis/route.py) is a Python standard-library example for this release's schema and structural language list. It reads a report and emits independent follow-up candidates plus reasons requiring review. It does not execute commands or interpolate paths into shell commands. The example is not installed as part of dircue.

Save both reports outside the scanned directory so they do not become inputs on subsequent runs. From this source checkout, with dircue on `PATH`:

```sh
checkout=/path/to/checkout
output=$(mktemp -d)
if dircue analyze all --projects --source directory --json "$checkout" \
    > "$output/profile.json" 2> "$output/profile.stderr"; then
    if ! python3 examples/staged_analysis/route.py "$output/profile.json" \
        > "$output/followups.json"; then
        exit 1
    fi
else
    cat "$output/profile.stderr" >&2
    exit 1
fi
```

Both commands must succeed before a pipeline consumes `followups.json`. A routing-script failure is not a negative recommendation. A successful routing script can report `review_required: true`; exit zero means the example could interpret the report, not that downstream work can be skipped.

The example suggests metrics when language files are present, structural analysis for supported language evidence, and package inventory when project/ecosystem evidence or binary/vendor content is present. These are candidates, not automatic obligations. It marks warnings, partial/skipped project reports, omitted files, ambiguous attribution, unknown content, generated/vendor/binary content, source files outside language statistics, and configuration without project evidence for review or a pipeline-defined fallback.

A source/test count larger than the language file count triggers review, as does source/test content without a supported structural language in the language totals. These aggregate checks cannot establish which individual source files contributed to the language statistics. Attribute overrides can exclude a source file while including a data file, or hide one language while leaving another visible. The example does not detect every such case or provide complete structural-language coverage.

`no_followup_evidence: true` means this limited example found neither a candidate nor one of its review conditions. It is **not** a `safe_to_skip` verdict. For example, a complete XML-only fixture produces this result, but an XML document could still describe software in a format dircue does not recognize. Only a pipeline policy with sufficient knowledge of its inputs can turn that observation into a decision to stop.

The output includes `input_report_sha256`, the SHA-256 of the exact report bytes, so a consumer can associate recommendations with their input. This identifies the report, not an immutable snapshot of directory contents.

The example accepts schema `1.2.0` with directory source and validates the fields it consumes; it is not a full JSON Schema validator. Unsupported schemas, absent project reports, malformed consumed fields, and Git-source reports fail explicitly. Report size and provenance should be bounded by the surrounding pipeline when consuming reports from outside its own run.

## Follow-ups remain explicit

After choosing the work, these are independent dircue invocations:

```sh
dircue analyze metrics --source directory --json /path/to/checkout
dircue analyze structure --source directory --json --files \
    --structural-worker /path/to/dircue-structural-worker /path/to/checkout
```

A pipeline may also run its installed Syft executable. Dircue does not invoke Syft; an explicit execution option remains planned in [#21](https://github.com/war-and-code/dircue/issues/21).

Dircue 0.4.0 can [import an existing Syft report](PACKAGE_EVIDENCE.md)
with `analyze packages --syft-report FILE`. Importing is separate from choosing
or executing Syft. Match the cataloged source to the dircue inventory explicitly;
a plausible path match alone does not establish that both describe the same
contents. [Project-reference graphs](GRAPH.md) are another optional follow-up
for parsed .NET declarations.

Project roots can help narrow follow-ups, but keep shared parent manifests, workspace files, imports, and artifacts in view. `project_root_hints` in the example are observed locations, not a list of guaranteed self-contained scan targets. The paths are report data and must never be treated as executable shell fragments.

Separate invocations scan again; they do not share a persistent inventory cache or parse cache. Savings come from avoiding unnecessary work. When every deeper module is required, a combined invocation can avoid some repeated work:

```sh
dircue analyze all --projects --metrics --structure --source directory --json \
    --structural-worker /path/to/dircue-structural-worker /path/to/checkout
```

The [staged-analysis measurements](../tests/staged_analysis/README.md) compare the first pass, metrics alone, separate staged executions, and combined profiling on representative and synthetic inputs. Skipping a follow-up saves only the work that follow-up would have done: default source metrics already exclude ordinary XML data. Staging can therefore cost more when metrics would have been cheap or when all modules are needed.

The language-only CLI, default aggregate report, and module opt-ins retain their existing behavior. A successful scan can still contain warnings or partial module results, including a tree-limit result that omits analysis. Check process status first and then report coverage; do not rely on exit status alone to decide that an inventory is complete.

Dircue 0.4.0 also provides [metadata-only discovery](DISCOVERY.md). Further whole-workflow evaluation remains in [#22](https://github.com/war-and-code/dircue/issues/22); 0.5.0 adds [offline comparison of saved reports](COMPARISON.md). Comparison does not cache inventories or avoid the scans that produced its inputs; incremental reuse remains tracked in [#10](https://github.com/war-and-code/dircue/issues/10). See the [project guide](PROJECTS.md) for current parser and attribution limits.

The [roadmap](https://github.com/war-and-code/dircue/issues/41) connects those foundations to proposed entry-point
mapping, optional semantic providers, and portable context for caller-selected
analysis. It keeps observations separate from the policy that selects follow-ups.
