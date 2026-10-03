# Declared environments

Environment analysis is an opt-in inventory of declarations. It does not prove that a project builds, inspect installed tools, choose a machine, execute repository code, modify `PATH`, or use the network.

```sh
dircue analyze environments --json /checkout
dircue analyze environments --source git --rev HEAD~1 --json /checkout
dircue analyze environments --source git --tree 0123456789abcdef0123456789abcdef01234567 --json /checkout
dircue analyze all --environments --json /checkout
```

The standalone command emits aggregate schema 1.7.0 and includes the declaration evidence it reused. `analyze all --environments` adds the same module to the ordinary selected population. Save JSON reports outside a directory source when they will be used as later inputs, so the report file does not enter a subsequent inventory.

The analyzer reuses parsed declaration records for project manifests. Its dimensions deliberately keep language minimums, runtime constraints, project SDK references, CLI toolchain selection, target frameworks, platform targets, language modes, and advisory toolchain preferences separate.

| Existing declaration fact | Environment dimension |
| --- | --- |
| `go-language-minimum`, Cargo `rust-version` | language minimum |
| Python `requires-python`, npm `engines` | runtime constraint |
| npm `packageManager`, Java toolchain, Gradle/Maven wrapper | toolchain selection |
| Go `toolchain` suggestion | advisory toolchain |
| MSBuild project `Sdk` | project SDK reference |
| .NET target framework | target framework |
| .NET runtime identifier | platform target |
| language version, Java release, Cargo edition | language mode |

The only constraint intersection currently implemented is comma-separated numeric Python comparisons using `<`, `<=`, `==`, `>=`, and `>`. It applies within one project context. Unsupported grammar and conditional declarations remain unresolved. Requirements from independent projects are not conflicts.

For parsed .NET projects, the CLI models SDK selection from the project root. The Go analyzer API can instead receive explicit invocation starts; the CLI does not expose an invocation-start option. The analyzer finds the nearest selected-source `global.json` ancestor. This is candidate applicability: the real .NET CLI and MSBuild choose their search start from invocation context, which can differ. The implementation follows the [Microsoft `global.json` documentation](https://learn.microsoft.com/en-us/dotnet/core/tools/global-json), last updated 2026-03-09 and accessed 2026-09-21. SDK selection remains separate from the target framework.

The analyzer reads each `global.json` through the selected source callback and caches it once per environment analysis. Git input is bound to the selected commit or exact tree. A live directory can change between the earlier declaration pass and this bounded selection read. Nonregular candidates block ancestor fallback.

The environment provider reports bounded literal declarations from `.python-version`, `.node-version`, `.nvmrc`, `rust-toolchain`, and `rust-toolchain.toml`. These records identify the source path, tool, syntax kind, literal value or values, state, and containing directory. Python version files may declare multiple values; nvmrc files accept one value. Rustup TOML records expose a literal `channel`; a `path` toolchain remains unresolved because it points to an external installation. Malformed or out-of-subset files are retained as unresolved or unsupported records without preserving their raw contents. The parser does not expand environment variables, execute manager configuration, or inspect files above the selected root.

These are declarations, not installed or effective tool versions. Applicability is bounded to the source directory. The report does not associate declarations with projects, implement each manager's ancestor lookup or invocation overrides, or choose a winner between nested or conflicting files. Same-directory disagreements are visible as conflicts; nested files remain separate with a boundary. Provider version 1.1.0 adds these optional records and their coverage counters while preserving the meaning and contents of .NET SDK selection fields. The bundled schema accepts both 1.0.0 and 1.1.0 environment reports. An older strict 1.0.0 schema reader will reject the newer provider version and added fields.

Incomplete inventory, invalid JSON or Unicode, duplicate members, unsupported SDK fields or values, and exceeded budgets produce unresolved evidence rather than an absence claim. The parser accepts the documented JSONC syntax: a UTF-8 BOM, `//` and `/* */` comments, and trailing commas before `}` or `]`. Each accepted lenient parse produces an informational `global-json-lenient-syntax` diagnostic.

An upstream declaration gap keeps environment status `partial`, except when the gap consists only of strict JSON rejections of `global.json` files that this analyzer independently reads and parses successfully. That exception requires no omitted files, diagnostics, or declaration-only requirements such as `msbuild-sdks` left unextracted. The `declarations-partial` boundary remains visible even when successful reads justify complete environment coverage. JSONC support does not excuse malformed project manifests, declaration limits, or unvalidated configurations. SDK search paths are reported as a boundary because dircue performs no installation or filesystem probe. Read failures fail by default. With `--on-error continue`, a read failure degrades that `global.json` to an unresolved selection, adds a per-path `file-read-error` diagnostic, and returns a partial report; other selections remain in place.

Requirement evidence identifies the declaring file, and `global.json` evidence identifies the selected file and modeled invocation context. This release does not retain source spans or raw-content digests. A Git report binds those paths to its selected tree. A directory report is explicitly live evidence and does not claim that a later read has the same bytes.

The fixed maxima are 200,000 selected inventory paths, 4,096 toolchain declaration files, 64 KiB per toolchain file, 4 MiB total toolchain file input, 16 version selectors per `.python-version` file, 128 bytes per selector, 64 TOML nesting levels, 4,096 modeled contexts, 65,536 normalized requirements, 1 MiB per `global.json`, 16 MiB total `global.json` input, 128 JSON nesting levels, 100,000 decoded JSON values, 8,192 bytes per retained string, and 16 MiB of serialized environment output. A lower `--max-file-bytes` also lowers the per-file `global.json` and toolchain read limits. Coverage counters and boundaries disclose omissions; limits do not describe process-memory use. `environments.ValidateReport` validates decoded reports before comparison or other reuse.

## Known boundaries

Environment coverage is limited to the dimensions above; other ecosystems' environment declarations are not modeled yet. See [capabilities](CAPABILITIES.md) and [#65](https://github.com/war-and-code/dircue/issues/65).
