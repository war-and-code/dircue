# Project declarations and interfaces

Added in 0.5.0.

```sh
dircue analyze declarations --json /checkout
dircue analyze all --declarations --discovery --json /checkout
```

The first command inventories selected regular files and reads supported manifests. It does not classify unrelated file contents, count lines, or start a structural worker. The second adds declaration observations to the usual aggregate profile. Neither command runs a package manager, build script, interpreter, or compiler.

Existing `analyze projects` keeps its earlier output, including filename-based discovery outside .NET and JVM projects. The new `declarations` module has its own report contract and does not change the legacy language-only path.

## Supported declarations

| Ecosystem | Inputs | Observations |
| --- | --- | --- |
| npm | `package.json` | Declared identity, version, private flag, package manager, engines, dependency scopes, workspace membership, explicit local references, unambiguous membership-only `workspace:*`/`workspace:^`/`workspace:~` links, script names and binary entrypoints |
| Go | `go.mod`, `go.work` | Module identity, minimum language version, suggested toolchain, require/exclude/replace directives, workspace membership and selected local replacement targets |
| Python and uv | `pyproject.toml` | Project and dynamic metadata, Python requirement, build backend, dependency groups and extras, named entrypoints, uv workspace membership and source declarations |
| Cargo | `Cargo.toml` | Package and workspace identity, members/default selection, supported inherited fields, scoped dependencies, declared feature names, targets and build-script observations |
| .NET, Maven and Gradle | Existing supported project/configuration manifests | Reused static requirements and references, with the same restrictions on build evaluation as the [project reader](PROJECTS.md) |

The Go grammar uses `golang.org/x/mod v0.40.0`. TOML parsing uses `go-toml v2.4.3`, with additional input and depth limits. Python/uv observations target a documented subset of uv 0.12.17. npm workspace behavior is checked against npm 11.16.0 for the supported pattern subset. A synthetic Cargo workspace is checked against Cargo 1.85.0 for membership, default-member selection, and inherited version, edition and minimum Rust version; this does not establish complete Cargo behavior. These readers do not emulate every package-manager version or configuration option.

Requirements, references, and interfaces retain evidence identifying the declaring manifest. Project IDs are root-relative manifest paths, rather than package names: two packages with the same name are still distinct observations. Shared or inherited declarations identify their supplying manifest and the relevant context.

## What relationships mean

Workspace membership, local dependencies, source overrides, and directory containment are different relationships. A nearby project with a matching name is not sufficient evidence of a dependency. A local replacement or source declaration does not establish that a particular invocation will use it.

`state` describes the declaration or its applicability. `target_status` describes what the reader established about a referenced target. A conditional reference can point to a present file. A target may also be missing, external, unparsed, ambiguous, unsupported, or inconsistent with the declared package name. `state: resolved` marks a supported relationship within the selected inventory; `target_status: present` alone establishes selected target presence. Neither establishes dependency resolution, installation, successful compilation, or the set of files a compiler would use.

Go's minimum language version and suggested toolchain are separate observations. dircue does not read `GOWORK`, `GOPATH`, the module cache, installed toolchains, or parent directories outside the selected root.

uv member source declarations override inherited workspace sources. Environment markers and dependency scopes are retained without evaluating an environment. A nearby `uv.lock` is associated by presence only; the report does not claim that it is fresh or complete.

Cargo workspace declarations are resolved separately from filesystem nesting. Supported local path dependencies can contribute membership; exclusions and explicit workspace pointers affect that interpretation. An invalid member entry does not erase independently valid sibling membership, but incomplete selection prevents confident inheritance. An invalid or capped exclusion still prevents unsafe positive membership conclusions. Optional dependencies, declared feature names, feature requirements, development/build scopes and target conditions remain qualified. Feature implication and activation are not evaluated. Explicit target declarations are observed; dircue does not reproduce Cargo's entire automatic target-discovery or feature-resolution process.

## Declared interfaces

The `interfaces` array contains named scripts, entrypoints, tools and targets where the manifest provides them. Script bodies are not included. A script named `test` establishes that declaration, not the quality or completeness of a test suite. A Python `module:object` entrypoint does not establish that importing it succeeds. A Cargo build-script candidate does not mean it has run.

The .NET/JVM expansion reuses static declarations. Raw build conditions are represented as present with their expressions withheld, and unsupported path/URL values are withheld. Callers needing the existing detailed project-reader contract can request `analyze projects` separately. No report is a guarantee that every possible secret embedded in arbitrary project metadata has been removed.

## Selected source and coverage

At a Git repository root, automatic selection normally reads committed `HEAD`. `--rev` selects a commit and `--tree` selects an exact Git tree. `--source directory` selects live filesystem contents instead. The declaration reader uses those same selected-source callbacks and never follows a reference to open an additional file. Directory mode is not an atomic snapshot. Read failures fail by default; `--on-error continue` records each unreadable manifest as a per-path `file-read-error` diagnostic in the partial report and keeps the remaining manifests.

Installed `node_modules` manifests are excluded from this module. Other supported manifests can be observed independently of their inclusion in language statistics. Symlinks and other nonregular entries are not followed. Missing targets are assessed within the selected inventory, not the host filesystem.

`status: complete` means the supported work completed without recorded omissions. It does not mean the repository builds or that every ecosystem was supported. The report lists `supported_ecosystems`, its selection rule and its limits. Read `diagnostics`, declaration states and coverage before using absence as evidence.

Workspace patterns support a bounded subset of path globs: `*`, `?`, character classes and `**` as a complete segment. Package-specific handling applies to npm exclusions and hidden directories. Unsupported syntax produces diagnostics; it is not silently translated into a different pattern language.

| Bound | Default maximum |
| --- | ---: |
| Selected inventory paths retained | 200,000 |
| Manifest candidates retained | 4,096, in lexical path order |
| Complete bytes per manifest | 1 MiB |
| Total manifest bytes admitted | 64 MiB |
| Observations per manifest | 2,048 |
| Observations in the report | 65,536 |
| Serialized declaration module | 16 MiB |
| Individual observation string | 8 KiB |
| Workspace patterns | 128 per adapter's manifest scope |

An explicitly smaller `--max-file-bytes` reduces the manifest read limit. Matching work is also capped per ecosystem and reported in `limits.resolution_work`. These are parser, work and retained-output bounds, not a process RSS ceiling. Exceeding a bound is visible; incomplete coverage cannot establish a definite missing target.

## JSON contract

Aggregate reports containing declarations use schema `1.4.0`. The module is described by [declarations.schema.json](../schema/declarations.schema.json); the enclosing report uses [profile.schema.json](../schema/profile.schema.json). Existing commands retain schemas 1.0–1.3 when declarations are not requested.

```sh
dircue analyze declarations --json /checkout > profile.json
jq '.declarations.projects[] | {id, kind, name, requirements, references, interfaces}' profile.json
```

Errors reading selected input fail the command. Unsupported declarations and bounded omissions produce qualified reports and can still exit zero. Consumers must check both the process result and the requested module's status.
