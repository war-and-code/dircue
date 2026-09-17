# First-pass and staged analysis measurements

A separate first pass provides a routing decision, but it also adds work. In this
small diagnostic run, calling the code-metrics follow-up separately took longer
than requesting projects and metrics together. Even for 2 GiB of XML logs,
first-pass routing did not produce a demonstrated speed advantage: default
source-scoped metrics already excludes XML.

Measured on September 17, 2026, on an Apple M1 Max (10 CPU cores, 32 GiB RAM),
macOS arm64. Each workflow had one untimed warmup and three measured repetitions,
with execution order rotated between repetitions. The table reports median
elapsed seconds and the largest single-process peak RSS across those three runs.

| Input | First pass (s) | Combined (s) | Staged total (s) | First-pass RSS (MiB) | Combined RSS (MiB) | Staged RSS (MiB) |
|---|---:|---:|---:|---:|---:|---:|
| XML only, 2 GiB | 0.0588 | 0.0560 | 0.0589 | 72.5 | 86.0 | 74.5 |
| Same XML plus a .NET project | 0.0662 | 0.0594 | 0.0930 | 68.1 | 87.6 | 71.2 |
| Small Go source collection | 0.0241 | 0.0252 | 0.0488 | 36.3 | 37.1 | 36.8 |
| Spring Framework | 1.5109 | 1.4280 | 2.8596 | 67.5 | 73.4 | 67.5 |
| Roslyn | 3.2551 | 3.3197 | 6.4523 | 97.0 | 118.3 | 100.9 |

“First pass” runs `analyze all --projects`. “Combined” runs
`analyze all --projects --metrics`. “Staged” runs the first pass, inspects its
JSON, and runs `analyze metrics` unless the inventory has complete data-only
composition, no project entries, and no warnings. Only the XML-only case skips
the follow-up. The staged total includes both invocations where applicable;
it is not just the follow-up duration.

All commands use `--source directory --json --workers 8 --tree-size 1000000`.
Metrics use their default `source` scope and 16 MiB per-file limit. No native
structural analysis or external inventory tool runs in this benchmark. The
controller lives in this Python harness; these timings do not measure the
separate routing example or promise savings from running Syft conditionally.

When every requested analysis is already known, the combined invocation avoids
a second traversal and classification pass. Staging remains useful for choosing
whether to request additional analyses, but the value depends on the work that
can actually be omitted. These measurements provide no estimate for that saving
with other tools.

## Inputs and correctness

- XML: 128 fully written, valid XML files, each exactly 16 MiB, totaling
  **2,147,483,648 bytes**. The mixed case hardlinks the same XML payloads and adds
  an SDK-style `.csproj` plus C# source. The verifier hashes every unique XML inode
  against the deterministic expected payload, checks exact inventory byte
  accounting, and asserts that the mixed project and its source metrics remain
  present. These repetitive synthetic logs do not reproduce real MOVEit data.
- Small source: a generated Go module with 200 files and 100 function declarations
  per file. The verifier checks every generated source file's complete bytes.
- Spring and Roslyn: clean checkouts at the revisions in
  [the corpus manifest](../performance/corpus.json). Their resolved commits are
  recorded in the receipt. No repository code is built or executed.

Every run must produce identical JSON for its workflow. Combined output, after
removing the metrics field, must equal the first-pass output. For cases that run
the follow-up, its metrics object must equal the combined metrics object. This
checks outputs as well as timing. Complete first-pass and combined JSON reports
are retained as compressed artifacts; their checksums are in the receipt.

## Reproduce

Run on macOS with the pinned corpora already present. The generator requires
about 2 GiB of additional disk space. It uses hardlinks for the mixed XML view;
choose a fresh fixture directory if the fixture specification changes.

```sh
mkdir -p .cache/staged-analysis
CGO_ENABLED=0 go build -trimpath -o .cache/staged-analysis/dircue .
python3 tests/staged_analysis/benchmark.py \
  --candidate .cache/staged-analysis/dircue \
  --fixtures .cache/staged-analysis/fixtures \
  --corpus-root .cache/corpus \
  --output .cache/staged-analysis/results/macos-arm64.json
python3 tests/staged_analysis/verify.py \
  --report .cache/staged-analysis/results/macos-arm64.json
```

The [retained receipt](results/macos-arm64.json.gz) contains raw timing samples,
commands, output checksums, build information, hardware details, source commit,
and postmeasurement verification. `verify.py` accepts either JSON or gzip JSON;
verifying fixture content requires the fixture paths recorded in that receipt
to remain available.

The measured binary was built with `CGO_ENABLED=0 go build -trimpath`, without
release linker flags. It reports `dircue 0.3.0` and has SHA-256
`b3326b05bd20d5c420fb3169d5193998e857f5e11d173e9e711fb28f55f090c6`.

## Limits

Three warm-cache samples are diagnostic, not statistically strong performance
acceptance or a universal speed claim. Other project builds and heavy tests were
paused during timing, but unrelated host activity was not controlled. Small
process timings include startup noise. Wall time includes the Python controller's
JSON decoding and decision; peak RSS measures the largest individual dircue
child, excluding Python, rather than the whole process tree. There was no hard
CPU or memory budget.

The XML size describes inventory accounting, not bytes read by the profiler.
Its classification and exclusion paths need not read the full 2 GiB. Default
source-scoped metrics already avoids counting this data, which is why the
XML-only case provides no evidence of an additional staging speedup. Directory
mode also includes on-disk files according to its normal selection rules;
these measurements do not compare Git-tree traversal costs.
