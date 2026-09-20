# Declaration corpus checks

This offline harness checks selected literal declarations in pinned public
repositories, a copy of dircue's selected working-tree files, and a synthetic
directory containing Go, npm, uv and Cargo workspaces. It never installs
dependencies or executes a corpus build, script or project tool.

Build a candidate with the source-bound performance harness, then run:

```sh
python3 tests/declarations_corpus/run.py \
  --candidate .cache/v050-sprint/performance-initial/dircue \
  --build-receipt .cache/v050-sprint/performance-initial/build.json \
  --output .cache/v050-sprint/declarations-corpus-results
```

The output directory must not already exist. Public checkouts must be available
under `.cache/corpus`, at the resolved commit IDs in `expectations.json`, with
clean working trees. Fetching them is a separate step. The harness does not
contact a network or modify their Git state. Run just the two public workspace
cases after obtaining their pinned checkouts:

```sh
python3 tests/declarations_corpus/run.py \
  --candidate .cache/v050-sprint/performance-final/dircue \
  --build-receipt .cache/v050-sprint/performance-final/build.json \
  --case opentelemetry-go --case pydantic-ai \
  --output .cache/v050-sprint/public-workspace-corpus-results
```

`--case` is repeatable; the default runs all nine public repositories and both
local/synthetic cases (80 invocations). The earlier six-public-repository run
remains a separate 56-invocation receipt. A `null` value in a selected project
fact means the optional field must be absent; it does not permit a fabricated
version for dynamic metadata.

The expectations were transcribed from manifest bytes before inspecting the
candidate's report. They cover:

- Cobra's module identity, minimum Go version and a pinned dependency.
- Express's package version, Node range and script names, with command bodies
  absent from the declaration report.
- npm CLI’s literal and glob workspace members, executable names and private
  packages. A versioned dependency stays external despite a nearby matching
  package; tracked `node_modules` manifests remain excluded.
- Flask's literal version, Python range, build backend and console entry point.
- Ripgrep's Rust version, edition, workspace members, local dependencies,
  executable target and declared build script.
- Roslyn's literal target framework, unresolved MSBuild property and local
  project reference.
- Spring's literal module include, with Gradle evaluation still unresolved.
- OpenTelemetry Go’s independent modules, including parent and sibling local
  replacements; nearby modules do not become an invented Go workspace.
- Pydantic AI’s five explicit uv members, named entrypoints and local source
  declarations, while dynamic metadata stays unevaluated.
- Mixed workspaces, local relationships, inherited Cargo metadata, a dynamic
  Python version and a deliberately malformed npm manifest.

For every public repository, the harness compares all eight combinations of
Git/directory source, standalone/aggregate command and one/eight workers.
Only `source` and `tree` are removed for this comparison. Project observations,
coverage, limits and diagnostics must match exactly. The two synthetic/local
copies exercise the four directory combinations. The live dircue directory is
not traversed: its ignored caches contain the public corpus itself.

Each receipt preserves the exact candidate binary hash, historical build
receipt, expectation and harness hashes, selected witness hashes, full input
inventory digest, per-run output hashes and compressed reports. Inventory
digests are rechecked after scanning. A module containing diagnostics or
omissions must retain its `partial` status. Nonzero exits and unexpected stderr
fail the run.

The initial harness run treated all stderr as failure. Inspection established
that Ripgrep and Spring each contain a tracked symlink; their directory scans
correctly warn that it was skipped. Those exact warnings are now expected,
preserved in receipts and checked for presence. Roslyn contains an intentional
invalid XML solution fixture; the harness explicitly requires its diagnostic.
Pydantic AI also contains tracked symlinks; their exact directory-mode warnings
are derived from its pinned file inventory. Its Hatch metadata hooks remain
unevaluated: workspace source declarations must not become inferred dynamic
dependencies. npm CLI includes deliberately malformed manifests and unsupported dependency/workspace test cases; its report remains partial while its root workspace observations are checked. The repository’s package version does not establish support for every npm version’s resolution behavior. No package manager or project hook is run to obtain these facts.

These checks validate the listed facts and acquisition invariants. They do not
certify complete dependency resolution, buildability or exhaustive language
support. Spring's dynamic per-project Gradle filenames remain outside the
conventional-manifest reader. `complete` describes retained supported
declarations, not execution of an ecosystem's build semantics. Recorded elapsed
times are diagnostic observations, not performance benchmarks.
