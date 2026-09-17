# Language and project coverage

Dircue has separate language classifiers, line counters, structural parsers, and
project readers. Support in one does not imply support in all four. These tables
describe the 0.3.0 candidate's enabled integrations, rather than every feature
provided by its upstream dependencies.

## Analysis layers

| Layer | Coverage | Result and limits |
| --- | --- | --- |
| Language identification | Maintained Enry/Linguist catalog | Language and byte totals, selected file metadata, and Linguist attributes. The [compatibility notes](../tests/conformance/DISCREPANCIES.md) describe the pinned reference and deliberate differences. |
| Line metrics | Explicit [Enry-to-scc mappings](../pkg/codemetrics/grammars.go) | Lines, code, comments, blanks, bytes, and lexical complexity. Unsupported mappings and read limits have omission reasons. See [metrics](METRICS.md). |
| Structural metrics | 20 source languages listed below | Per-file BCA metrics and syntax health from one Tree-sitter parse. Optional native worker; no compiler type resolution or cross-file call graph. |
| Custom syntax declarations | Java and C# | Counts of selected declarations, imports, lambdas, and local functions. These additional fields are absent for other languages. |
| Project and build observations | Recognized manifest families | Detailed .NET/Maven declarations, conservative Gradle observations, and filename-based discovery elsewhere. A project relationship is not a resolved package dependency graph. |

The language and metrics paths cover substantially more languages than structural
analysis. An unsupported structural language can still contribute to language
statistics, line metrics, and content composition. XML and other data are excluded
from default source statistics and parsing; project XML manifests have a separate
selection path. Broader text counting is an explicit metrics option.

## Structural coverage

All rows have language identification and an explicit scc mapping. Every listed
structural parser returns upstream BCA metrics and syntax-node/recovery counts.
The extra Java/C# declaration fields should not be inferred for other rows.

| Detected language | Structural grammar | Relevant project discovery/build coverage |
| --- | --- | --- |
| C | C | No dedicated C build-manifest reader |
| C++ | C++ | No dedicated C++ build-manifest reader |
| C# | C# | .NET project/solution declarations and references |
| Elixir | Elixir | No dedicated Mix project reader |
| Go | Go | `go.mod` and `go.work` filename discovery |
| Groovy | Groovy | Conservative literal Gradle declarations where those manifests exist |
| Java | Java | Maven declarations and references; conservative Gradle observations |
| JavaScript, including JSX | JavaScript | `package.json` filename discovery |
| Kotlin | Kotlin | Maven/Gradle observations where those manifests exist |
| Lua | Lua | No dedicated Lua package-manifest reader |
| Objective-C | Objective-C | No dedicated Xcode project reader |
| Perl | Perl | No dedicated Perl package-manifest reader |
| PHP | PHP | `composer.json` filename discovery |
| Python | Python | `pyproject.toml`, `setup.py`, `setup.cfg`, and `requirements.txt` filename discovery |
| Ruby | Ruby | `Gemfile` filename discovery |
| Rust | Rust | `Cargo.toml` filename discovery |
| Shell | Bash | No dedicated shell project reader; other shell dialects may recover or misparse |
| Tcl | Tcl | No dedicated Tcl package-manifest reader |
| TSX | TSX | `package.json` filename discovery |
| TypeScript | TypeScript | `package.json` filename discovery |

Project readers follow manifests, not source-file language labels. A Maven
project's presence does not prove how Kotlin or another JVM language is compiled.
Similarly, F# and Visual Basic .NET project files receive .NET declaration
analysis, but neither language has an enabled structural parser in this release.
The [project guide](PROJECTS.md) lists interpreted files, conditions, and limits.

F5 iRules is supported by the standalone worker, but cannot currently be selected
through the pinned Enry language catalog. It is not counted among the 20 production
languages. Firefox-specific BCA parser variants are not enabled. Dircue does not
fetch extra grammars based on repository contents or installed software.

## Interpreting structural results

`structure.supported_languages` lists each production language, its pinned grammar,
available `observations` fields, and upstream `metric_groups`. `structure.observation_files` records how many
analyzed files contribute to each observation key. Read aggregate counts alongside that contributor coverage. A zero class count
measured only across Java/C# files does not establish that the other languages
have no classes. Unavailable observation fields are absent, not measured zeroes.

BCA's metric groups and their applicability vary by language. Shell, for example,
does not produce every object-oriented metric group. Upstream nulls and zeros are
retained, without inventing unavailable measurements or averaging unrelated
cross-language values. File metrics are available with `--files`.

`syntax_errors` and the file's `status` qualify parser acceptance. Some grammar
recovery is visible only through the root error flag, so zero exposed error or
missing-node counts do not override a partial status. A complete parse is not
proof that the code compiles, or that its grammar covers every modern construct.

Every enabled language has a small deterministic integration fixture. Java and
C# have the deepest real-project corpus checks; additional corpus samples cover
Python, JavaScript, TypeScript, C, Rust, Go, PHP, and Ruby. These checks establish
behavior on those inputs, not exhaustive dialect or metric correctness. See the
[breadth harness](../tests/structural_breadth/README.md) and
[Java/C# corpus harness](../tests/structure/README.md).
