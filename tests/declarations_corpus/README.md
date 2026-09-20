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
contact a network or modify their Git state.

The expectations were transcribed from manifest bytes before inspecting the
candidate's report. They cover:

- Cobra's module identity, minimum Go version and a pinned dependency.
- Express's package version, Node range and script names, with command bodies
  absent from the declaration report.
- Flask's literal version, Python range, build backend and console entry point.
- Ripgrep's Rust version, edition, workspace members, local dependencies,
  executable target and declared build script.
- Roslyn's literal target framework, unresolved MSBuild property and local
  project reference.
- Spring's literal module include, with Gradle evaluation still unresolved.
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

These checks validate the listed facts and acquisition invariants. They do not
certify complete dependency resolution, buildability or exhaustive language
support. Spring's dynamic per-project Gradle filenames remain outside the
conventional-manifest reader. `complete` describes retained supported
declarations, not execution of an ecosystem's build semantics. Recorded elapsed
times are diagnostic observations, not performance benchmarks.
