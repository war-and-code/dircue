# Optional structural analysis

Structural analysis extracts syntax observations and aggregate BCA metrics from
20 source languages through pinned BCA grammars. It is an explicit addition to dircue's ordinary language and
line-counting paths. Neither the existing commands nor the core Go executable
need a native parser installation.

The adapter requires an explicit path to a prebuilt `dircue-structural-worker`.
It does not search `PATH`, download grammars, restore dependencies, or execute
build scripts from the directory being profiled. The selected worker is an
executable: obtain it from a source you trust, and pass its actual filesystem
path. Filenames and source text are delivered through standard input, not shell
commands.

The development branch also supports [`--functions`](FUNCTIONS.md) for bounded
function-space metrics. This is not available in the 0.3.0 binaries.

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
both syntax observations and BCA metrics. Every enabled language reports syntax
node and recovery counts. Java and C# additionally report classes, interfaces,
records, structs, enums, methods, constructors, properties, imports, lambdas,
and local functions. Those custom declaration fields are absent for other
languages; absence is not a count of zero. These are syntax observations;
there is no compiler name resolution, type checking, project evaluation, or
cross-file call graph.

`supported_languages` lists the pinned grammar and available observation fields
for each production language. `observation_files` gives the analyzed-file coverage
for each aggregate observation key. Consumers can distinguish measured zeroes
from unavailable declarations. BCA metric groups differ by language and are
preserved as supplied by upstream.

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
records those examples. XML logs and other data are not admitted merely because
they are text. Parser acceptance does not establish support for every language
version or dialect. See the [capability matrix](CAPABILITIES.md) for the distinction
between language detection, counting, parsing, and project mapping.

## Enabled languages

The production adapter accepts C, C++, C#, Elixir, Go, Groovy, Java, JavaScript
(including JSX), Kotlin, Lua, Objective-C, Perl, PHP, Python, Ruby, Rust, Shell,
Tcl, TSX, and TypeScript. Shell uses BCA's Bash grammar; it is not a promise to
parse every shell dialect. TSX has its own grammar alongside TypeScript.

BCA supplies the metric groups for each enabled parser. Their definitions and
applicability vary by language; an upstream zero or null is not evidence that
an equivalent language feature was measured. Dircue preserves these per-file
metrics instead of presenting one cross-language quality score.

For future review views, the [roadmap](ROADMAP.md#useful-metrics-without-a-universal-grade)
describes how to combine measurements with their scope, definitions, and missing
evidence. Function-level hotspots and cross-file dependency graphs require
additional observations; neither can be inferred from the current file totals.

The [breadth harness](../tests/structural_breadth/README.md) covers every enabled
language with small source fixtures, direct-worker comparison, and deterministic
combined reports. Java and C# also retain the larger real-project corpus checks.
That deeper corpus validation is not implied for all other languages.

BCA's F5 iRules parser is available in the standalone worker, but the current
Enry catalog has no F5 iRules language identity. The production scanner therefore
does not claim iRules coverage. It does not silently treat arbitrary Tcl files
as iRules. Firefox-specific C++/JavaScript parser variants are not enabled.

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
| Language grammars and BCA helper grammars | Pinned in Cargo.lock | See each dependency's included license and notices |

The packaging script verifies each dependency's original crate archive against
Cargo.lock, and includes every resolved dependency's complete crate source
(including its licenses and notices). In particular, the complete unmodified
BCA source is `source/crates/big-code-analysis-2.2.0.crate`. Each package also
contains the worker's source, Cargo manifest and lockfile, a third-party notice,
provenance, and SHA-256 checksums. Preserve those files when redistributing the
add-on. Its dependency licenses remain distinct from dircue's MIT license.

The [dependency inventory](../prototypes/structural/DEPENDENCIES.md) lists the
enabled grammar versions. BCA and the grammar versions form a tested set. The Go adapter rejects a worker
claiming another BCA, runtime, or grammar version. Upgrade them together, rerun
real-worker tests and syntax fixtures, and compare corpus reports. A grammar
with the same language name is not automatically compatible with BCA's metric
implementation.
