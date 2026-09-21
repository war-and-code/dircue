# Projects and build declarations

`dircue analyze projects` maps declared projects, their references, and the composition of the selected file inventory. It reads manifests without running builds, restoring packages, or evaluating repository code.

```sh
dircue analyze projects /path/to/checkout
dircue analyze projects --json /path/to/checkout
dircue analyze all --projects --metrics --json /path/to/checkout
```

The existing language-only commands and `analyze all` keep their previous output unless a new analysis is requested. Reports with project mapping or structural analysis use schema `1.2.0`. Metrics alone still use `1.1.0`; reports without these optional analyses use `1.0.0`.

## What is identified

| Files | Observations |
| --- | --- |
| `.csproj`, `.fsproj`, `.vbproj`, `.vcxproj`, `.sqlproj`, `.wixproj`, `.shproj`, `.proj` | Declared SDKs, target frameworks, framework versions, language versions, runtime identifiers, project references, imports, package declarations, and selected generation inputs from the supported passive MSBuild subset |
| `.sln`, `.slnx`, `.slnf` | Solution membership for MSBuild projects; solution-filter members are relative to the referenced solution directory, and virtual solution folders do not become projects |
| `global.json` | SDK version, roll-forward policy, prerelease policy, and MSBuild SDK versions |
| `Directory.Build.props`, `Directory.Build.targets`, `Directory.Packages.props` | Shared declarations and imports, with their conditions |
| `packages.config`, `nuget.config` | Legacy package declarations where present; registry credentials and arbitrary NuGet settings are not inventoried |
| `pom.xml` | Maven 4.0/4.1 coordinates, declared modules/subprojects and parent references, Java compiler settings, dependencies, plugins, and selected generation/toolchain declarations |
| `toolchains.xml` | Declared Java toolchain versions |
| Gradle build/settings files | Conservative observations from literal declarations; executable build configuration remains unresolved or conditional |
| `gradle.properties`, `gradle-wrapper.properties` | Recognized wrapper/toolchain settings; arbitrary property values are not reported |
| Other recognized ecosystem manifests | Project discovery by filename, explicitly marked `manifest-discovery: filename-only` |

Other ecosystem manifests include `package.json`, `pyproject.toml`, `setup.py`, `setup.cfg`, `requirements.txt`, `go.mod`, `go.work`, `Cargo.toml`, `Gemfile`, and `composer.json`. Discovery does not establish that a manifest is valid, that every declared package can be obtained, or that a workspace contains every project. Filename-only discoveries of the same ecosystem in one directory are grouped, with all contributing manifests retained as evidence. Distinct parsed projects sharing a directory remain separate and can make file attribution ambiguous.

## References and requirements

Each observation identifies its source manifest in `evidence`. References retain the original `value`; a statically resolvable path also has a root-relative `target`. Windows path separators are normalized. Absolute paths, paths escaping the scan root, and unsupported dynamic expressions are not followed.

`state` describes the declaration:

- `declared`: a requirement was read from a manifest.
- `conditional`: the declaration depends on a condition that dircue has not evaluated. `condition` preserves the relevant expression or context.
- `unresolved`: the value or target cannot be established by this passive reader.
- `resolved`: a reference target is present in the selected inventory (a file, or a Gradle module directory).
- `missing`: a statically determined reference target is absent from that inventory.

Requirements use the first three states. References also have `target_status`: `present`, `missing`, or `unresolved`. A conditional reference can therefore identify a present target while retaining its unevaluated condition. `resolved` means inventory presence only; it does not mean a successful build or a resolved dependency graph. Likewise, a missing parent POM may be available through Maven's repository lookup.

.NET values containing MSBuild expressions remain unresolved. SDK imports require SDK resolution and are not mistaken for files next to the project. Conditions on parent elements remain attached to declarations. Project references do not establish whether a referenced project participates in a particular build configuration. MSBuild property, item and item-metadata names use MSBuild's case-insensitive name semantics within case-sensitive XML structural elements; XML attributes remain case-sensitive.

The Maven reader recognizes unnamespaced POMs and the explicit `http://maven.apache.org/POM/4.0.0` and `http://maven.apache.org/POM/4.1.0` namespaces. Maven 4.1 `subprojects/subproject` entries use the same relative-path graph as older `modules/module` entries. Model 4.2 namespaces remain explicitly unsupported. A Maven 4.1 parent with no explicit relative path reports the default lookup as conditional because reactor and repository resolution have not been evaluated.

Maven's local literal properties can be substituted when supported; inherited values, activation, plugin behavior, and externally supplied properties are not fully evaluated. Gradle observations remain conditional on script evaluation. No build command is inferred or executed.

## Configuration candidates

`configuration_candidates` lists observed configuration files within the project's ecosystem and ancestor scopes. Conventional Gradle wrapper configuration is associated with its build root. These are candidates for investigation, not a computed effective configuration. Lookup can depend on command invocation, imports, override properties, and build-tool rules. The report does not claim that every candidate applies to every project.

Arbitrary imported `.props` or `.targets` files are not parsed in this release. A reference may point to an existing file whose contents were not interpreted. Similarly, an SDK or dependency name does not establish that its toolchain or package is installed.

## File attribution and composition

Projects use their manifest path as `id`, with the containing directory as `root`. File counts use the rule recorded in `attribution`: `nearest-unique-project-directory`.

For each selected file, dircue looks for the nearest containing directory with a discovered project. One project receives that file's byte and file counts. If multiple projects share that directory, the file contributes to `ambiguous`. If no containing project exists, it contributes to `unassigned`. Solution and workspace nodes describe membership and do not own files.

This is directory attribution, not compiler input resolution. MSBuild includes/removes, linked files outside project directories, Maven source-directory overrides, and other evaluated build rules can produce different build membership.

`composition` places each inventoried file into one category and records the classification basis. Categories include source, tests, configuration, generated files, vendored files, documentation, data, binary, and unknown content. Test classification uses path/name conventions; generated/vendor/documentation classification uses available Linguist rules and attributes. These observations help describe the directory; they are not instructions to exclude files from another tool.

Project manifests have their own selection path, so XML manifests remain visible even when XML does not contribute to language percentages. Non-code XML data can appear in composition without becoming source code or receiving structural analysis.

## Inventory and limits

Project mapping follows the selected inventory source. With automatic source selection at a Git repository root, that ordinarily means committed `HEAD` contents. `--rev` selects a commit and `--tree` selects an exact 40-hex Git tree object. Use `--source directory` to inspect the current files on disk. Git-free directories are supported. File selection and tree limits still apply; missing targets are assessed against the inventory actually selected. Read failures fail by default; `--on-error continue` retains supported partial results with explicit omissions for recoverable per-file reads.

The scanner reads at most 1 MiB per recognized manifest, further constrained by an explicitly smaller general file-read limit. Oversized or incomplete manifests produce diagnostics. XML readers accept UTF-8 and BOM-signaled UTF-16LE/BE, require any XML encoding declaration to agree with that byte encoding, bound nesting and element counts, reject DTDs, and never fetch external entities. Other encodings, including ISO-8859-1 and Windows-1252, are not transcoded. .NET declaration extraction also caps individual conditions at 64 KiB, aggregate expanded condition/observation text at 4 MiB, and observations at 8,192 per manifest. Reaching a limit preserves the project identity and earlier observations, emits a `declaration-limit` diagnostic, and makes coverage partial; these are parser bounds, not a process RSS ceiling. Malformed or unsupported manifests produce diagnostics rather than silently becoming valid projects. JVM declarations are limited to 4,096 observations and 1 MiB of retained observation text per manifest; individual property expansions are limited to 64 KiB. A budget overrun produces a `declaration-budget-exceeded` diagnostic and partial coverage. NuGet configuration is not a source of credential or registry-URL output.

`status` is `complete`, `partial`, or `skipped`. Diagnostics and omitted files make a report partial. `complete` describes completion of the supported inventory work; it does not assert that every build expression was resolved or every referenced file was present. Consumers should inspect declaration states, reference target status, diagnostics, and ambiguous/unassigned counts for the decisions they need to make.

Project mapping is available in the Go binary alone. Optional [structural analysis](STRUCTURE.md) has its own parser worker, supported-language coverage, and resource limits.
