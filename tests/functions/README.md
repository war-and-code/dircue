# Function-space evidence checks

These checks exercise optional BCA function-space evidence without assigning a
quality grade. The ordinary structural path remains the reference behavior.
Nothing in the fixture repositories is compiled or executed.

Run from the repository root with Python 3.12 or newer:

```sh
python3 -m unittest discover -s tests/functions -p 'test_*.py'
python3 tests/functions/run.py \
  --baseline-worker /path/to/v0.3.0/dircue-structural-worker \
  --worker /path/to/new/dircue-structural-worker \
  --candidate /path/to/new/dircue \
  --output /tmp/functions-checks.json
```

The output path must be new. Omitting `--candidate` checks only the worker and
records that the CLI was not exercised.

The harness checks:

- 67 requests against the released worker: the existing 21 structural fixtures
  in three modes, plus malformed Java/C#/Python and unsupported XML. Exit codes,
  stderr and stdout must match exactly, except the existing `timings_ns` object.
- Opt-in evidence for the same 21 fixtures, covering 20 languages, with one parse
  per source, qualified coverage, physical line bounds, ten available function
  metric groups, deterministic indices and bounded names/entries.
- Handwritten Java, C# and Python counterexamples. The `defective` methods contain
  possible division by zero but have cyclomatic sum 1. Adding a protective `if`
  gives `guarded` sum 2. These six values are hand counted from BCA 2.2.0's base
  plus decision rule; they are not thresholds or a defect detector. Source spans
  are independently marked in the harness.
- Table dispatch, polymorphism, callbacks, comprehension and exception examples.
  Their metrics are retained as provider observations, without inferring resolved
  calls, execution paths or correctness. Python's nested-function example checks
  that parent metrics include nested spaces; adding parent and child values would
  double count.
- Parser recovery, empty source, a 140-function file exceeding the 128-entry cap,
  overlong names, UTF-8/CRLF/final-newline spans and a suppression comment that must
  not hide evidence. The CLI additionally checks source hashes, exact provider
  measurements and generated-file exclusion with visible coverage omissions.

The CLI's all-language, multi-worker, aggregate-cap and schema checks are in
`schema/functions_native_test.go`; this harness does not repeat that entire suite.
The protocol receipt is evidence of the checked cases, not a proof of every BCA
formula or every supported language construct. No candidate output is promoted
automatically into an expected golden result.

## Incremental cost

On macOS, use existing checkouts at the commits in
[`tests/performance/corpus.json`](../performance/corpus.json):

```sh
python3 tests/functions/benchmark.py \
  --baseline /path/to/v0.3.0/dircue \
  --candidate /path/to/new/dircue \
  --baseline-worker /path/to/v0.3.0/dircue-structural-worker \
  --worker /path/to/new/dircue-structural-worker \
  --candidate-source /path/to/candidate/worktree \
  --corpus-root .cache/corpus \
  --output /tmp/function-cost
```

The harness reads pinned Git blobs for Spring's `StringUtils.java`, Roslyn's
`CSharpSyntaxTree.cs` and Flask's `app.py`, each under 1 MiB. It also uses the three
small handwritten examples, the 140-function cap case and a mixed-language case.
It does not download checkouts or retain their source in this repository.

Each case has one warm-up and three runs in rotating order. The CLI comparisons
are released core/worker, candidate core/worker, and candidate with `--functions`.
Single-file cases also measure the worker directly in all three configurations.
Every run must retain the checked output; raw samples, commands, compressed
reports, source hashes, binary hashes and source snapshots are saved. Default
reports must agree, and opt-in reports must retain existing structural evidence
and match the worker's function entries.

Wall time includes process startup and the `/usr/bin/time` wrapper. Maximum RSS
comes from macOS `time -l`, with the CLI and standalone worker measured separately.
It is not a simultaneous process-tree peak, and those peaks must not be added.
These warm-cache measurements on a single host have no CPU/RAM limits. They show
the cost for these bounded inputs, not a universal overhead bound or a claim that
function metrics improve software quality. Source snapshots are checked for
changes but do not replace reproducible-build provenance.
