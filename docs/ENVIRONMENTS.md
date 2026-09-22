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

For parsed .NET projects, the CLI models SDK selection from the project root. The Go analyzer API can instead receive explicit invocation starts; the 0.8 CLI does not expose an invocation-start option. The analyzer finds the nearest selected-source `global.json` ancestor. This is candidate applicability: the real .NET CLI and MSBuild choose their search start from invocation context, which can differ. The implementation follows the [Microsoft `global.json` documentation](https://learn.microsoft.com/en-us/dotnet/core/tools/global-json), last updated 2026-03-09 and accessed 2026-09-21. SDK selection remains separate from the target framework.

`global.json` is read through the selected source callback and cached once per environment analysis. A Git source is bound to the selected commit or exact tree; a live directory can change between the earlier declaration pass and this bounded selection read. Nonregular candidates block ancestor fallback. Incomplete inventory, invalid JSON or Unicode, duplicate members, unsupported SDK fields, unsupported values, and exceeded budgets produce unresolved evidence rather than an absence claim. The environment analyzer accepts the documented JSONC leniency — a UTF-8 BOM, `//` and `/* */` comments, and trailing commas before `}` or `]` — and discloses each accepted lenient parse with an informational `global-json-lenient-syntax` diagnostic while keeping the environment status `complete`; the upstream declarations pass may be stricter and its partial state is surfaced as a `declarations-partial` boundary rather than propagated into the environment status. SDK search paths are reported as a boundary because no installation or filesystem probe is performed. Read failures fail by default; `--on-error continue` degrades that specific `global.json` to an unresolved selection with a per-path `file-read-error` diagnostic and returns a partial report, leaving every other selection in place.

Requirement evidence identifies the declaring file, and `global.json` evidence identifies the selected file and modeled invocation context. This release does not retain source spans or raw-content digests. A Git report binds those paths to its selected tree. A directory report is explicitly live evidence and does not claim that a later read has the same bytes.

The fixed maxima are 200,000 selected `global.json` inventory paths, 4,096 modeled contexts, 65,536 normalized requirements, 1 MiB per `global.json`, 16 MiB total `global.json` input, 128 JSON nesting levels, 100,000 decoded JSON values, 8,192 bytes per retained string, and 16 MiB of serialized environment output. A lower `--max-file-bytes` also lowers the per-file `global.json` read limit. Coverage counters and boundaries disclose omissions; limits do not describe process-memory use. `environments.ValidateReport` validates decoded reports before comparison or other reuse.

## Known boundary at 1.0

Environment coverage is limited to the dimensions above; other ecosystems' environment declarations are not modeled yet. See [capabilities](CAPABILITIES.md#known-boundaries-at-10) and [#65](https://github.com/war-and-code/dircue/issues/65).
