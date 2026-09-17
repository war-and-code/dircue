# Structural prototype validation

These checks exercise the experimental Java/C# worker and directory driver. They
do not establish production language coverage or validate the mathematical
definitions of BCA's metrics.

After building the prototype, run from the repository root:

```sh
python3 prototypes/structural/tests/validate.py \
  --worker prototypes/structural/worker/target/release/dircue-structural-worker \
  --driver prototypes/structural/bin/dircue-structural-prototype \
  --output prototypes/structural/tests/results/validation-local.json
```

The authored fixtures cover interfaces, records, nested classes, constructors,
methods, imports, lambdas, a C# property, and a file-scoped namespace. Assertions
check exact declaration counts. Combined analysis must produce the same
observations as structure-only mode and the same metrics as metrics-only mode.
Every mode must report one parser invocation. The worker's source has one
`Ast::parse` call; both consumers borrow that `Ast`. The counter checks that
contract, rather than independently instrumenting Tree-sitter internals.

Malformed Java/C# must report partial results. Other checks cover unsupported
languages, invalid modes, oversized requests, XML/prose exclusion, source size
and encoding exclusions, symlinks, metadata directories, timeout, and interrupt.
The cancellation test uses a sleeping helper to make interruption deterministic;
the parser and metric assertions run the actual BCA worker.

## Sample benchmarks

```sh
python3 prototypes/structural/tests/benchmark.py \
  --worker prototypes/structural/worker/target/release/dircue-structural-worker \
  --driver prototypes/structural/bin/dircue-structural-prototype \
  --corpus-root .cache/corpus \
  --output prototypes/structural/tests/results/benchmark-local.json
```

The corpus directory must contain checkouts named `spring-framework`, `roslyn`,
and `aspnetcore`. The report records each checkout's commit, origin, and selected
files' content hashes. It uses working files, so the content hashes are the
authoritative inputs even if a checkout has edits.

For each checkout, the script selects 50 tracked UTF-8 source files no larger
than 1 MiB. It samples evenly across sorted paths and includes the two largest
eligible files. Inputs are copied into a temporary directory. Each of the three
modes gets one warm-up and three measured runs. The driver launches a separate
worker for every file. Wall time includes that startup, JSON transport, parsing,
analysis, and report output; the worker also reports per-phase timings.

Scaling uses 10, 100, and 500 copies of the Java fixture. It tests whether retained
memory grows with file count at a fixed file size. It does not test arbitrarily
large individual syntax trees. The Linux offline checks verify their inputs run
under an enforced Docker container memory limit. The tested kernel did not expose
`memory.peak`, so that report records no measured container peak.

`time_max_rss_bytes` is `/usr/bin/time`'s maximum resident-set measurement. It is
not the sum of simultaneous driver and worker memory, nor a hard memory limit.
Compare it only within the same operating system and benchmark setup. Warmed
sample timings cannot predict an entire repository's time, cold-cache behavior,
or performance against Enry, Linguist, or scc: those tools perform different work.

These results establish whether sharing a parse works and expose integration
costs. They are not a throughput claim for a released dircue feature.

## Recorded macOS ARM64 run

The [validation report](results/validation-macos-arm64.json) records 56 passing
checks. The [benchmark report](results/benchmark-macos-arm64.json) contains exact
input hashes, checkout commits, binary hashes, and individual measurements.
Its build receipt records Go 1.26.6 with `CGO_ENABLED=0` and `-trimpath`, plus
Rust 1.100.0-nightly from the `nightly-2026-08-31` toolchain using the locked
release profile. Pass `--build-metadata path/to/receipt.json` to preserve build
commands and compiler versions when repeating the benchmark.

| Sample, 50 files each | Combined median | Structure median | Metrics median | Combined max RSS from `time` |
| --- | ---: | ---: | ---: | ---: |
| Spring Framework | 0.263 s | 0.232 s | 0.254 s | 16.77 MiB |
| Roslyn | 0.646 s | 0.540 s | 0.619 s | 42.31 MiB |
| ASP.NET Core | 0.433 s | 0.374 s | 0.421 s | 34.30 MiB |

Combined mode took about 44–46% less wall time than the sum of separate structure
and metrics runs on these samples. That saving includes avoiding a second worker
launch and JSON transfer, as well as a second parse. Combined mode reported 50
parses per sample; running both individual modes reported 100 in total.

Scaling from 10 to 100 to 500 copies of the Java fixture measured 6.67, 11.12, and
12.38 MiB maximum RSS respectively, with median times of 0.041, 0.334, and 1.733
seconds. This is consistent with releasing each tree rather than retaining all
trees, but is not a constant-memory guarantee for arbitrary inputs.

All files produced results, with no process failures or policy skips. Three
Roslyn files reported partial results because their parse trees contained error
nodes. [The parser limitation report](results/parser-limitations-macos-arm64.json)
lists their paths, hashes, and diagnostic probes. Removing preprocessor directive
lines eliminated errors from `Binder.ValueChecks.cs` and the generated Razor
`TestComponent.codegen.cs` fixture. The error in `ScriptBuilder.cs` comes from
`pdbStreamOpt?.Position = 0;`, a
[C# 14 null-conditional assignment](https://learn.microsoft.com/en-us/dotnet/csharp/language-reference/proposals/csharp-14.0/null-conditional-assignment).
Replacing only that statement with an explicit null check removed the error.
A minimal example reproduced the same grammar limitation; its explicit null-check
variant parsed without errors. Removing UTF-8 BOMs did not resolve these cases.
These probes do not change the benchmark inputs and are not a proposed
preprocessing step. Parser errors do not establish that the source is invalid C#;
they limit the reliability of observations derived from that tree.
