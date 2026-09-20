# Checked aggregation models

This optional experiment evaluates whether Bend 2 reference models can strengthen confidence in dircue's bounded aggregation. It accompanies the production Go/Rust tests; it adds no dependency to the CLI, worker, release artifacts, or ordinary CI.

The experiment uses [Bend 2.0.16 at revision `981899d6`](https://github.com/bendlang/bend/tree/981899d6b2fb2c109ed83545cffed9d20910a025). Bend and Bun versions are fixed in [pin.json](pin.json); Go and Clang versions are recorded for each run. The runner disables Bend's version-check telemetry and requires an unchanged checkout of that revision. Go dependencies must already be cached.

The [recorded experiment](results/experiment.json) checked all 35 laws without unsafe warnings and passed the CPU, JavaScript, Python-reference and Go-bridge comparisons below. The separate [production mutation receipt](results/production-properties.json) records 17 detected source mutations and four passing baseline/restored controls. These receipts identify specific sources and executions; they are not certificates for later revisions.

## What the laws establish

The first model, [model.bend](model.bend), represents a nonempty histogram. Its laws cover initialization, conservation of observations, sequential partition composition, and per-bin additivity in a separate counting reference. A finite certificate checks all 4,225 ordered pairs of bin indices from 0 through 64. Sequential composition alone does not establish that independently accumulated histograms can be merged.

The [typed model](typed/model.bend) addresses that gap. Its `Histogram(n)` and `Index(n)` types require matching widths and admitted indices. Thirteen laws cover initialization, conservation, increment/merge compatibility, commutativity of merging, independent partition merge equivalence, and index conversion. Updates change only their designated bin, index conversion rejects out-of-range indices, and list admission round-trips encoded valid indices. Width zero has no admitted indices. Width 65 matches production's histogram layout.

The [top-K model](topk/model.bend) has sixteen laws. Its executable comparator agrees with a declared descending-value, ascending-identity order. The full insertion and merge results are ordered and preserve each entry's multiplicity. Bounded insertion retains the same prefix as the full result for every limit and input list. Further laws establish that independently retaining each partition's top-K entries suffices for global top-K, across any finite sequence of partitions in the specified merge fold. The model does not prove equivalence for arbitrary reassociations of a parallel merge tree.

Parts of the top-K order and insertion proofs adapt Bend's upstream demo under Apache-2.0. Its [license and modified-source attribution](topk/THIRD_PARTY_NOTICES.md) are retained alongside the model.

These are laws about reference models under the pinned checker. They do not prove dircue's Go or Rust implementation correct, nor establish that the parser found every function. Metric definitions, file selection, omitted inputs, and retained evidence remain separate questions.

## How the models meet production

The executable examples exercise 556 values in seven fixed populations: empty and singleton inputs, numeric boundaries, consecutive values, repeated ties, and values near `U32`'s maximum. The runner:

1. Requires the exact clean proof verdict, with zero unsafe warnings.
2. Builds and runs JavaScript and single-threaded native CPU versions and compares their output bytes.
3. Independently checks every bin assignment, population count, and histogram in Python.
4. Exercises every admitted index separately as a 65-by-65 identity matrix, rejection of index 65, and independently merged partitions in the typed model. A uniform histogram alone would miss swapped bins.
5. Feeds the same values to the production Go `HotspotReport.Add` implementation through [production.go](production.go), comparing histogram results.

The bridge also checks one production `u64` value for each of the 65 bins. Twelve top-K examples cover varied values, ties, empty inputs, zero limits, and limits beyond population size; Python independently sorts their 660 input occurrences. Two- and three-partition executions must match that same reference. The three examples with K=10 also run through Go's production aggregation with independently constructed per-file top-ten summaries over populations of up to 17 entries. Sorted identity ranks map to equal-width file paths and provider indices, preserving numeric identity order under Go's actual path/index tie-breaker. This finite mapping is not a proof about arbitrary production paths.

The bridge constructs synthetic metric records. It does not invoke BCA or validate repository discovery. The separate generated Rust and Go tests exercise population traversal, bounded top-ten retention, path/index tie-breaking, invalid spans, syntax cohorts, and the full `u64` boundary range against full-population sorting and independent histogram oracles.

## Counterexamples matter

The runner deliberately breaks observation conservation, the finite pair certificate, typed histogram merging, list admission, the top-K comparator, insertion, merging, and partition-state retention. Those changes must fail checking because the stated laws no longer hold. The admission and top-K mutants must first pass ordinary typechecking, distinguishing violated proofs from malformed programs. Open laws without their proofs must also fail.

It also replaces the metric-to-bin conversion with a constant zero. That program still satisfies the original histogram laws: those laws receive bin indices and say nothing about conversion from metric values. The runtime differential check must reject it. This accepted-but-wrong mutation is retained to show why a passing proof cannot justify a broader claim than its laws state.

[production_mutations.py](production_mutations.py) separately checks that production tests reject real source mutations in an isolated Rust copy and Go overlays. Compilation failures and timeouts do not count as detected defects. The runner requires passing baseline and restored controls and verifies that repository runtime sources remain unchanged.

## Reproduce

Use Python 3.10+, Go matching the module, Bun 1.4.2, and Clang on Linux or macOS. Native Windows is not a supported Bend host. Check out the pinned upstream source yourself; the runner does not install or update tools.

```sh
git clone https://github.com/bendlang/bend /tmp/dircue-bend
git -C /tmp/dircue-bend checkout --detach 981899d6b2fb2c109ed83545cffed9d20910a025

python3 research/bend-aggregation/run.py \
  --bend-checkout /tmp/dircue-bend \
  --bun /path/to/bun \
  --output /tmp/dircue-bend-results

python3 -m unittest discover -s research/bend-aggregation -p 'test_*.py'
```

Output directories must be new. The result includes source/tool hashes, backend output hashes, proof verdicts, counterexample outcomes, and captured logs. The runner verifies that its harness, toolchain checkout, and selected local Go inputs have not changed during execution. Logs can contain local filesystem paths; review them before sharing.

For production mutation checks, first make the worker's locked Cargo dependencies available, then use Rust 1.94 or later:

```sh
python3 research/bend-aggregation/production_mutations.py \
  --output /tmp/dircue-production-mutations
```

`--cargo-target-dir` can point to an existing cache. The mutation runner uses Cargo offline and runs neither a repository's build scripts nor source programs being profiled. It does compile dircue's own dependencies and tests.

## Trust and adoption boundaries

- The [upstream Lean file](https://github.com/bendlang/bend/blob/981899d6b2fb2c109ed83545cffed9d20910a025/bend2/bend.lean) explicitly says its formalization does not fully match the implemented checker. This experiment does not rerun or certify that metatheory.
- [Unsafe definitions and generated template instances](https://github.com/bendlang/bend/blob/981899d6b2fb2c109ed83545cffed9d20910a025/bend2/main.ts#L490) can produce a successful process exit. The harness inspects the verdict, not just the exit code.
- Source-level natural-number laws are distinct from compiled arithmetic limits. Bend's runtime `Nat` values have a 48-bit bound. Runtime examples stay within that bound and use `U32` metric values; production accepts `u64`. Production overflow tests remain necessary.
- Compiler lowering, native effects, the operating system, and Go/Rust code are outside these model proofs. CPU/JavaScript agreement on this corpus is finite execution evidence, not a proof of backend equivalence. No GPU or performance claim follows.

Retain the models as optional developer assurance. They complement finite production tests with general model properties and helped expose gaps in the initial specification. The experiment did not discover an existing production aggregation defect, and it adds a separate model and proof maintenance burden. No second proof assistant was used to validate the checker or proofs.

Defer Bend as a production accelerator: aggregation has not been established as a dominant CPU cost, and this experiment does not solve cross-platform distribution or conversion overhead. The adopt/defer decision and the original evaluation goals are recorded in [issue #46](https://github.com/war-and-code/dircue/issues/46). Stronger production refinement, compiler assurance, or acceleration would need separate evidence.
