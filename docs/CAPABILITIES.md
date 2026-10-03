# Language and project coverage

Dircue has separate language classifiers, line counters, structural parsers, and project readers. Support in one does not imply support in all four. These tables describe the enabled integrations in the 1.x line, rather than every feature provided by its upstream dependencies.

Version 0.4.0 additionally offers [metadata discovery](DISCOVERY.md), [.NET declaration graphs](GRAPH.md), [Syft report import](PACKAGE_EVIDENCE.md), [caller-supplied rules](RULES.md), [package-source declarations](REGISTRIES.md), and [bounded function metrics](FUNCTIONS.md). These additions have their own scope and coverage fields; they do not expand the structural grammar list or turn filename hints into parsed declarations.

Version 0.5.0 adds explicit [project declarations](DECLARATIONS.md) for npm, Go, Python/uv and Cargo, alongside the existing .NET/JVM readers, and [offline comparison](COMPARISON.md) of saved reports. These are opt-in operations.

Version 0.6.0 adds [format evidence](FORMATS.md) and [population-aware function hotspots](HOTSPOTS.md), both explicit selections. Existing language, line-count and structural grammar coverage is unchanged.

Version 0.7.0 adds [focused .NET/Python project profiling](FOCUS.md), [source-availability evidence](AVAILABILITY.md), and [targeted explanations](EXPLANATIONS.md). These additions preserve the existing language catalog and structural grammar coverage.

Version 0.8.0 adds [declared environment requirements](ENVIRONMENTS.md), a limited [planner capability registry and offline follow-up plans](PLANNING.md), and comparison of focused/source-availability evidence. Version 1.2.0 adds explicit [lockfile observations](LOCKFILES.md), selected Python/Node/Rust toolchain declarations, and selected Maven/Cargo registry configuration. These modules do not expand the language or grammar catalogs.

## Analysis layers

| Layer | Coverage | Result and limits |
| --- | --- | --- |
| Environment requirements | Supported declaration facts, bounded .NET `global.json` contexts, and selected Python/Node/Rust toolchain files | Declared constraints and qualified selection inputs; no installed-environment or build-success claim. |
| Follow-up planning | Explicit caller requests and one saved aggregate report | Inert commands, prerequisites, retained evidence and cost quantities; no execution or source access. |
| Focused profiling | Parsed .NET and Python/uv selections | Original-root context and qualified containment; optional separate primary/related scc metrics. |
| Source availability | Explicit bounded prefixes, selected Gitlinks, supported checkout metadata | Acquisition-boundary evidence; no fetching, hydration, or filter execution. |
| Explanations | Targeted language decisions and retained project/focus/availability facts | Fresh or saved evidence, with source identity, limits, and unavailable detail. |
| Metadata discovery | Selected regular files, including non-code and vendor paths | Filename and size inventory, manifest/artifact candidates, explicit coverage; no source-payload reads. |
| Format evidence | Bounded prefixes of selected regular files | Separate filename hints, signatures and supported syntax checks; no archive expansion or purpose inference. |
| Function hotspots | Eligible BCA Function-kind spaces in selected parsed files | Fixed-bin distributions and top-ten evidence by language/grammar and clean/recovered syntax; includes overlapping nested spaces. |
| Package-source declarations | Selected NuGet, npm, Maven and Cargo configuration | Bounded declarations and sanitized origins; no effective feed resolution. |
| Lockfile observations | Selected npm and NuGet project/lockfile associations | Named direct-declaration checks with ownership and coverage qualifications; no restore or dependency-graph consistency claim. |
| Caller rules | Explicit bounded JSON ruleset | Factual filename/path/literal matches; no automatic configuration or execution. |
| Imported package evidence | Existing supported native Syft JSON | Explicit source binding and coordinate mapping; does not run Syft. |
| Language identification | Maintained Enry/Linguist catalog | Language and byte totals, selected file metadata, and Linguist attributes. The [compatibility notes](../tests/conformance/DISCREPANCIES.md) describe the pinned reference and deliberate differences. |
| Line metrics | Explicit [Enry-to-scc mappings](../pkg/codemetrics/grammars.go) | Lines, code, comments, blanks, bytes, and lexical complexity. Unsupported mappings and read limits have omission reasons. See [metrics](METRICS.md). |
| Structural metrics | 20 source languages listed below | Per-file BCA metrics and syntax health from one Tree-sitter parse. Optional native worker; no compiler type resolution or cross-file call graph. |
| Custom syntax declarations | Java and C# | Counts of selected declarations, imports, lambdas, and local functions. These additional fields are absent for other languages. |
| Project declarations (opt-in) | Selected supported manifests | Bounded workspace, local dependency, requirement and interface declarations; no package-manager or build execution. |
| Saved-report comparison | Two explicit aggregate JSON files | Per-module observation changes with provenance and coverage qualifications; no rescanning. |
| Project and build observations | Recognized manifest families | Detailed .NET/Maven declarations, conservative Gradle observations, and filename-based discovery elsewhere. A project relationship is not a resolved package dependency graph. |

The language and metrics paths cover substantially more languages than structural analysis. An unsupported structural language can still contribute to language statistics, line metrics, and content composition. XML and other data are excluded from default source statistics and parsing; project XML manifests have a separate selection path. Broader text counting is an explicit metrics option.

## Profile schema versions

The `schema_version` of an aggregate [profile](../schema/profile.schema.json) report depends on the modules requested:

| Requested output | Schema version |
| --- | --- |
| Aggregate report without optional modules | `1.0.0` |
| Metrics, without projects or structure | `1.1.0` |
| Projects or structure, with optional metrics | `1.2.0` |
| Discovery, graph, imported package evidence, rules, registries, or function metrics | `1.3.0` |
| Project declarations, alone or with other modules | `1.4.0` |
| File-format evidence | `1.5.0` |
| Focus, availability, explanations, or focused metrics | `1.6.0` |
| Declared environments, with reused declarations | `1.7.0` |
| Lockfile observations, with reused declarations | `1.8.0` |

## Structural coverage

All rows have language identification and an explicit scc mapping. Every listed structural parser returns upstream BCA metrics and syntax-node/recovery counts. The extra Java/C# declaration fields should not be inferred for other rows.

| Detected language | Structural grammar | Relevant project discovery/build coverage |
| --- | --- | --- |
| C | C | No dedicated C build-manifest reader |
| C++ | C++ | No dedicated C++ build-manifest reader |
| C# | C# | .NET project/solution declarations and references |
| Elixir | Elixir | No dedicated Mix project reader |
| Go | Go | Opt-in `go.mod`/`go.work` declarations and local relationships |
| Groovy | Groovy | Conservative literal Gradle declarations where those manifests exist |
| Java | Java | Maven declarations and references; conservative Gradle observations |
| JavaScript, including JSX | JavaScript | Opt-in npm workspace, dependency and interface declarations |
| Kotlin | Kotlin | Maven/Gradle observations where those manifests exist |
| Lua | Lua | No dedicated Lua package-manifest reader |
| Objective-C | Objective-C | No dedicated Xcode project reader |
| Perl | Perl | No dedicated Perl package-manifest reader |
| PHP | PHP | `composer.json` filename discovery |
| Python | Python | Opt-in `pyproject.toml`/uv declarations and bounded static `requirements*.txt`/`setup.py` project observations; filename discovery for other recognized manifests |
| Ruby | Ruby | `Gemfile` filename discovery |
| Rust | Rust | Opt-in Cargo package/workspace declarations and local relationships |
| Shell | Bash | No dedicated shell project reader; other shell dialects may recover or misparse |
| Tcl | Tcl | No dedicated Tcl package-manifest reader |
| TSX | TSX | Opt-in npm workspace, dependency and interface declarations |
| TypeScript | TypeScript | Opt-in npm workspace, dependency and interface declarations |

Project readers follow manifests, not source-file language labels. A Maven project's presence does not prove how Kotlin or another JVM language is compiled. Similarly, F# and Visual Basic .NET project files receive .NET declaration analysis, but neither language has an enabled structural parser in this release. The [project guide](PROJECTS.md) lists interpreted files, conditions, and limits.

F5 iRules is supported by the standalone worker, but cannot currently be selected through the pinned Enry language catalog. It is not counted among the 20 production languages. Firefox-specific BCA parser variants are not enabled. Dircue does not fetch extra grammars based on repository contents or installed software.

## Interpreting structural results

`structure.supported_languages` lists each production language, its pinned grammar, available `observations` fields, and upstream `metric_groups`. `structure.observation_files` records how many analyzed files contribute to each observation key. Read aggregate counts alongside that contributor coverage. A zero class count measured only across Java/C# files does not establish that the other languages have no classes. Unavailable observation fields are absent, not measured zeroes.

BCA's metric groups and their applicability vary by language. Shell, for example, does not produce every object-oriented metric group. Upstream nulls and zeros are retained, without inventing unavailable measurements or averaging unrelated cross-language values. File metrics are available with `--files`.

`syntax_errors` and the file's `status` qualify parser acceptance. Some grammar recovery is visible only through the root error flag, so zero exposed error or missing-node counts do not override a partial status. A complete parse is not proof that the code compiles, or that its grammar covers every modern construct.

Every enabled language has a small deterministic integration fixture. Java and C# have the deepest real-project corpus checks; additional corpus samples cover Python, JavaScript, TypeScript, C, Rust, Go, PHP, and Ruby. These checks establish behavior on those inputs, not exhaustive dialect or metric correctness. See the [breadth harness](../tests/structural_breadth/README.md) and [Java/C# corpus harness](../tests/structure/README.md).

<a id="known-boundaries-at-10"></a>

## Current boundaries

These are known boundaries of the 1.0 implementation. The 1.x compatibility policy is documented in [COMPATIBILITY.md](COMPATIBILITY.md); it does not imply that the analysis layers below cover every ecosystem or repository shape.

- **Git repository shapes:** bare and unborn repositories, SHA-256 object format, Git alternates, `GIT_DIR` overrides, and subdirectory discovery inside a repository root are not fully modeled. `--source git` fails with an explicit error for the unsupported forms; `--source auto` may fall back to directory mode silently on the legacy Linguist surface. See [source availability](AVAILABILITY.md) for related evidence and [#66](https://github.com/war-and-code/dircue/issues/66).
- **Absent focus query:** `analyze focus --affected-by <path>` for a path with no matching declaration returns `status: complete` with the echoed query rather than a dedicated "no match" sentinel. See [focus guide](FOCUS.md) and [#67](https://github.com/war-and-code/dircue/issues/67).
- **Registry adapter coverage:** `analyze registries` reads selected NuGet.Config, .npmrc, Maven `settings.xml` and `.cargo/config`/`.cargo/config.toml` files. Other package sources, such as `pip.conf`, Yarn configuration and Maven POM repository declarations, remain outside scope. See [registries guide](REGISTRIES.md#uncovered-configuration) and [#68](https://github.com/war-and-code/dircue/issues/68).
- **Environment adapter breadth:** `analyze environments` covers the dimensions in [ENVIRONMENTS.md](ENVIRONMENTS.md); other ecosystems' environment declarations are not modeled yet. See [#65](https://github.com/war-and-code/dircue/issues/65).
- **Structural worker isolation:** the worker inherits the caller's process environment and working directory and is not sandboxed. The caller is responsible for the worker binary's origin. See [structural guide](STRUCTURE.md#dependencies-and-redistribution) and [#69](https://github.com/war-and-code/dircue/issues/69).
