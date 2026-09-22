# Focus a project without losing its context

Added in 0.7.0.

```sh
dircue analyze focus --project services/catalog/catalog.csproj --json /checkout
dircue analyze focus --project services/catalog/catalog.csproj --metrics --files --json /checkout
dircue analyze focus --project services/catalog/catalog.csproj --related-project libraries/core/core.csproj --metrics --json /checkout
dircue analyze focus --project services/api/pyproject.toml --metrics --json /checkout
dircue analyze focus --affected-by Directory.Build.props --json /checkout
```

Pass the original repository or directory root, followed by a root-relative manifest selector. Supported selections are parsed .NET projects and Python/uv projects or workspace roots. Other manifests can still establish ownership boundaries. An unsupported or unparsed selected project fails explicitly.

The command inventories the original source and reads supported declarations before choosing a population. With Git source, both phases use the same selected tree. Directory reads remain live and non-atomic. Root-relative paths and ancestor attributes keep their meaning; no copied subtree is created.

## What the scope means

A primary file belongs to the nearest project-manifest directory under the declared containment rule. This is a qualified directory population, not a compiler-resolved source list. Explicit linked files, MSBuild conditions, SDK default globs, arbitrary imports, and dynamic build logic are not evaluated. A nested malformed, unsupported, or nonregular manifest remains a boundary; a parent does not silently claim its contents. Multiple project manifests at the same root make ownership ambiguous.

The report keeps these populations separate:

- `focus.primary`: retained primary file observations.
- `focus.context`: selected manifests and supported shared-configuration candidates.
- `focus.relations`: retained declared relationships, including conditions and target states.
- `focus.related`: additional project populations requested with `--related-project`.
- `focus.boundaries` and `focus.omissions`: ambiguous ownership and incomplete evidence.

.NET ancestor configuration names are candidates, not proof that MSBuild imports or applies them. Declared imports retain their conditions and target status. Python member context follows supported declared uv workspace membership, rather than assuming every ancestor `pyproject.toml` owns a child.

`--affected-by` lists projects with candidate or declared context evidence for the supplied input. It is a bounded reverse declaration query, not a runtime blast-radius calculation. It selects no source population and cannot be combined with `--metrics` or `--related-project`.

At 1.0 an `--affected-by` query for a path with no matching declaration returns `status: complete` and echoes the query rather than emitting a dedicated "no match" sentinel. See [capabilities](CAPABILITIES.md#known-boundaries-at-10) and [#67](https://github.com/war-and-code/dircue/issues/67).

## Focused metrics

`--metrics` opts into scc counting after planning. `focused_metrics.primary` and each `focused_metrics.related` entry have separate totals. A project reference alone never causes another project's source to be counted. Context-only files do not enter a source population merely because they appear in the context list.

The existing `--metrics-scope source|text`, `--metrics-max-file-bytes`, and `--files` choices apply within each population. Language inclusion and attributes still use original-root paths. The top-level repository `metrics` field is absent from a focused report, preventing accidental mixing of denominators.

Other deeper profilers are not focus consumers in 0.7.0. A focused run does not implicitly invoke structural analysis or other tools. The metadata and declaration prepass has a cost; focusing a tiny project in a small directory may not save time.

## Limits and identity

The focus planner bounds its inventory at 200,000 paths, declaration records at 4,096, relationships at 65,536, work at 4,194,304 steps, and JSON presentation at 16 MiB. The ordinary tree-size limit still applies; exceeding it prevents project selection and returns an error. Manifest parsing has its own [declaration limits](DECLARATIONS.md).

Presentation trimming never changes the internal selection used for metrics. Incomplete execution selection qualifies metric results as partial. Check coverage and omission reasons; an empty population is not proof that a project has no source.

`focus.scope.id` binds the provider version, selection rule, requested projects or query, and selected-source identity. It is not a hash of directory contents. A live directory can change while retaining the same scope ID.

New focused reports use aggregate schema 1.6.0. From 0.8.0, `compare` accepts that schema and preserves these population distinctions, with scope, policy and coverage qualifications. See [saved-report comparison](COMPARISON.md). Existing unfocused report schemas remain available.
