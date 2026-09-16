# Optional structural analysis

Structural analysis extracts syntax observations and aggregate BCA metrics from
Java and C# source. It is an explicit addition to dircue's ordinary language and
line-counting paths. Neither the existing commands nor the core Go executable
need a native parser installation.

The adapter requires an explicit path to a prebuilt `dircue-structural-worker`.
It does not search `PATH`, download grammars, restore dependencies, or execute
build scripts from the directory being profiled. The selected worker is an
executable: obtain it from a source you trust, and pass its actual filesystem
path. Filenames and source text are delivered through standard input, not shell
commands.

## Commands

```sh
dircue analyze structure --structural-worker /opt/dircue/dircue-structural-worker --json /path/to/checkout
dircue analyze structure --structural-worker ./dircue-structural-worker --files --source directory --json .
dircue analyze all --structure --structural-worker ./dircue-structural-worker --files --json .
```

`analyze structure` reports structural coverage and declaration totals. Add
`--files` for each file's BCA metrics, provenance, and status. BCA metrics are
kept per file because many are not meaningfully additive. In `analyze all`,
`--structure` is required to opt in. Its `--files` flag applies to both metrics
and structure when both modules are requested.

`--structural-max-file-bytes` accepts values from 1 through 8388608, with
8388608 as the default. `--structural-timeout` sets a positive per-file duration
such as `10s` or `500ms`.

Structural analysis uses the scanner's selected inventory and Linguist-style
source selection, including `.gitattributes` overrides. Its default source mode
uses the selected committed Git snapshot when available; `--source directory`
reads working files. `--source git --rev <revision>` selects a committed revision.
Generated, vendored, documentation, and data exclusions remain visible as
omissions. Unsupported selected source languages, configured input-size limits, and
non-regular-file omissions make coverage partial; ordinary out-of-scope content
does not.

## What the result means

Each eligible file is parsed once. The same BCA-owned Tree-sitter tree supplies
both declaration counts and BCA metrics. The worker reports classes, interfaces,
records, structs, enums, methods, constructors, properties, imports, lambdas,
local functions, and syntax recovery counts. These are syntax observations;
there is no compiler name resolution, type checking, project evaluation, or
cross-file call graph.

The Go adapter preserves only deterministic results. Parser timings remain in
the standalone experimental worker protocol for benchmark use but do not enter
production reports. Results include the source byte count, parse count, parser
provenance, and one of these statuses:

| Status | Meaning |
| --- | --- |
| `complete` | Both consumers finished, and the grammar reported no syntax recovery. This does not prove semantic correctness or complete language-version support. |
| `partial` | Both consumers finished, but the grammar recovered from missing or unrecognized syntax. Counts and metrics need that qualification. |
| `skipped` | Unsupported language, oversized input, invalid UTF-8, NUL-containing source, or invalid display path. No parse occurred. |

Process failures, deadlines, malformed responses, unexpected parser versions,
and excessive worker output are errors. They must not silently become a
successful complete analysis. A parser partial result is distinct from a worker
failure.

The current C# grammar has known limitations on parts of the Roslyn corpus,
including modern syntax. The prototype's
[parser limitation report](../prototypes/structural/tests/results/parser-limitations-macos-arm64.json)
records those examples. Java and C# are the only enabled grammars. XML logs and
other data are not admitted merely because they are text.

## Resource boundaries

The adapter admits one worker process at a time. Each worker handles one file
and exits, releasing its tree before another file is admitted. The default and
maximum source size are both 8 MiB; callers may select a smaller limit. The
default deadline is 10 seconds per worker invocation. Waiting for admission
respects caller cancellation.

The source limit is **not a process memory limit**. Parse trees, JSON buffers,
metrics, and native allocator overhead can exceed source size substantially.
Use an operating-system or container memory limit when a hard ceiling is
required. Worker standard output is limited to 16 MiB and standard error to
64 KiB; either overflow cancels the invocation. The prototype demonstrated a
small offline workload under a 256 MiB container limit, not an upper bound for
all supported inputs.

## Building the add-on

The native worker's canonical source remains in
[`prototypes/structural/worker`](../prototypes/structural/worker). Keeping one
implementation lets the existing parse-reuse and grammar regression fixtures
exercise exactly what the adapter invokes. Its separate Cargo manifest does not
add cgo or Rust requirements to ordinary `go build`.

Use Python 3.12 or newer for packaging. Install Rust 1.94.0 and the relevant
target, then build a local archive in a fresh output location:

```sh
rustup toolchain install 1.94.0 --profile minimal
python3 scripts/structural_worker_release.py --platform darwin-arm64 --smoke-test
```

Use the platform matching the build host unless its native cross-compilation
requirements have been installed. Supported packaging targets are
`darwin-arm64`, `darwin-amd64`, `linux-amd64`, `linux-arm64`, and
`windows-amd64`. The separate Structural worker workflow builds and tests each
on its corresponding runner and uploads workflow artifacts. It does not create
a tag or publish a release.

Unlike the core Go executable, Linux add-on artifacts use glibc: the build
runners are Ubuntu 22.04 for amd64 and Ubuntu 24.04 for arm64. These artifacts
are not promised to run on Alpine/musl or older Linux distributions. Package
provenance records each Linux binary's required glibc symbol versions and linked
libraries. The macOS deployment target is pinned to 11.0; packaging checks that
its dynamic dependencies are system libraries. This is a deployment target,
not a claim that CI ran on macOS 11.

Windows packaging requests static MSVC C runtime linkage and inspects the
executable to reject Visual C++ Redistributable DLL dependencies. The Rust
Windows target requires Windows 10 or Windows Server 2016 and later. The worker
still uses operating-system DLLs. See Rust's [Windows target support](https://doc.rust-lang.org/rustc/platform-support/windows-msvc.html)
and [C runtime linkage](https://doc.rust-lang.org/reference/linkage.html#static-and-dynamic-c-runtimes).

Portable add-on packaging is validated separately from the existing core
archives and Python wheels; the worker is not bundled into those wheels.

Builds fetch pinned dependencies. Executing a built worker requires no network,
Cargo installation, grammar cache, or writeable source directory.

## Dependencies and redistribution

| Component | Version | License |
| --- | --- | --- |
| big-code-analysis | 2.2.0 | MPL-2.0 |
| Tree-sitter | 0.26.12 | MIT |
| tree-sitter-java | 0.23.5 | MIT |
| tree-sitter-c-sharp | 0.23.5 | MIT |

The packaging script verifies each dependency's original crate archive against
Cargo.lock, and includes every resolved dependency's complete crate source
(including its licenses and notices). In particular, the complete unmodified
BCA source is `source/crates/big-code-analysis-2.2.0.crate`. Each package also
contains the worker's source, Cargo manifest and lockfile, a third-party notice,
provenance, and SHA-256 checksums. Preserve those files when redistributing the
add-on. Its dependency licenses remain distinct from dircue's MIT license.

BCA and the grammar versions form a tested set. The Go adapter rejects a worker
claiming another BCA, runtime, or grammar version. Upgrade them together, rerun
real-worker tests and syntax fixtures, and compare corpus reports. A grammar
with the same language name is not automatically compatible with BCA's metric
implementation.
