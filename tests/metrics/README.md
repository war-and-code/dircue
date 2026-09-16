# Metrics integration checks

These checks exercise the embedded scc engine separately from language detection.
They compare counters with the pinned standalone scc v4.1.0 executable using the
same full-file bytes and generic counting settings. They do not assert that scc
understands every construct in each language.

Build the two executables:

```sh
mkdir -p .cache/metrics-v020
CGO_ENABLED=0 go build -trimpath -o .cache/metrics-v020/dircue-candidate .
GOBIN="$PWD/.cache/metrics-v020" CGO_ENABLED=0 go install github.com/boyter/scc/v4@v4.1.0
```

Run the fixture checks:

```sh
python3 tests/metrics/run.py \
  --candidate .cache/metrics-v020/dircue-candidate \
  --scc .cache/metrics-v020/scc \
  --output .cache/metrics-v020/differential.json
```

Add `--large-xml` to write a 1,100 MiB XML fixture. The default counting scope must
skip it. Explicit text counting must report incomplete coverage when the file
exceeds the content limit, without adding counts from a prefix. Temporary fixture
files are removed afterward.

The fixtures cover Java records and text blocks, C# records and string variants,
CRLF, Unicode, missing final newlines, TSX, and a block comment that crosses the
language detector's bounded read. Separate assertions cover generated, vendored,
and documentation files, aggregation totals, opt-in output schema, size limits,
and committed Git snapshots with staged, modified, and untracked working files.

The raw-string fixtures include a known scc 4.1.0 limitation: a standalone quote
inside a Java text block or C# raw string can cause a following `//` line to count
as a comment. Both tools currently report the same counters. Matching these
results establishes compatibility with scc, not syntax-aware accuracy.

For existing pinned checkouts from `tests/performance/corpus.json`:

```sh
python3 tests/metrics/corpus.py \
  --candidate .cache/metrics-v020/dircue-candidate \
  --scc .cache/metrics-v020/scc \
  --project spring-framework=.cache/corpus/spring-framework \
  --project roslyn=.cache/corpus/roslyn \
  --project aspnetcore=.cache/corpus/aspnetcore \
  --output .cache/metrics-v020/corpus.json
```

This scans each committed tree, selects 200 counted Java or C# files by the hash
of their path, adds every counted Java/C# file larger than 128 KiB, extracts the same committed bytes into a temporary directory, and
compares standalone counters. It also checks the Roslyn Visual Basic file that
exposed an upstream Git delta-reader corruption bug, and rejects unexpected
`input_changed` skips from immutable Git trees. Receipts record source commits, tree IDs, file
hashes, binary hashes, and matching counts.

## Language-only cost

Keep a baseline executable built from the previous version, using the same Go
version and build flags as the candidate. Stop concurrent builds and heavy tests
before timing. For example:

```sh
python3 tests/metrics/compare_language.py \
  --baseline .cache/metrics-v020/dircue-baseline \
  --candidate .cache/metrics-v020/dircue-candidate \
  --project spring-framework=.cache/corpus/spring-framework \
  --project roslyn=.cache/corpus/roslyn \
  --metrics --runs 20 \
  --output .cache/metrics-v020/performance.json
```

The harness checks exact language JSON equality before timing. It alternates
process order, runs three warmups, and records at least 20 samples per command.
`--metrics` adds a separate measurement of the extra counting work; it is not an
equivalent-work comparison against language-only analysis. Include empty and tiny
directories with `--source directory` to check startup cost.

Wall measurements include the same system `time` launcher for each executable.
Peak RSS comes from that launcher's child measurements, normalized to bytes on
macOS and Linux. Reported p99 and higher values are observed order statistics;
20 samples cannot estimate rare tails. The harness does not flush filesystem
caches, change power settings, or establish a universal performance guarantee.

To compare the optional metrics command with standalone scc on the same selected
synthetic source files:

```sh
python3 tests/metrics/compare_selected.py \
  --candidate .cache/metrics-v020/dircue-candidate \
  --scc .cache/metrics-v020/scc \
  --output .cache/metrics-v020/performance-selected.json
```

This verifies exact per-file counters before timing. Both tools count the same
files, but dircue also classifies them and reports coverage. These small fixtures
are useful for initialization measurements, not a general ranking of the tools.

`reader_corrections.py` compares per-file reports from the pre-fix and fixed Git
readers, then verifies each changed counter against standalone scc using native
`git show` bytes. Pass `--before`, `--candidate`, `--scc`, repeated `--project
NAME=PATH`, and `--output` arguments. The old executable is needed to reproduce
which counters the reader fix corrected.
