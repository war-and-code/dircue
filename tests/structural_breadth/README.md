# Structural language breadth

`run.py` exercises 21 small handwritten source fixtures across all 20 production
structural languages, including JavaScript and JSX separately. It requires a built
Go candidate and the expanded native worker. It does not execute the fixture code.

```sh
python3 tests/structural_breadth/run.py \
  --candidate .cache/v030/dircue-breadth \
  --worker prototypes/structural/worker/target/release/dircue-structural-worker \
  --output tests/structural_breadth/results/fixtures.json
```

Checks include natural Enry classification, exactly one parse per admitted file,
syntax acceptance, direct-worker equality, independent structure/metrics mode
equality, deterministic output with one and eight scanner workers, and identical
structural output when projects and scc metrics are requested together. Java/C#
retain their custom declaration fields; other languages must expose only supported
syntax observations. Additional inputs verify unsupported Swift, excluded XML and
generated Go, and Python syntax recovery.

`fixtures.json` names the source files in `testdata/`, their canonical language,
and expected parser status. The native worker's unit tests independently cover
all enabled parser selections, including F5 iRules, which the production Enry
catalog cannot currently select. Worker packaging smoke tests also consume this
fixture manifest on each supported native platform.

## Additional project samples

`corpus.py` uses eight repositories already pinned by the language comparison
suite: Flask, Express, TypeScript, jq, ripgrep, Cobra, Laravel, and Rails. It verifies
their checkout commits, samples 20 nonempty source files per repository, and reads
committed blobs rather than potentially modified working copies. Half the sample
spans sorted paths; the remainder takes the largest files, with a 1 MiB ceiling.
Explicit attributes admit generated/vendor/documentation samples.

```sh
python3 tests/structural_breadth/corpus.py \
  --candidate .cache/v030/dircue-breadth \
  --worker prototypes/structural/worker/target/release/dircue-structural-worker \
  --corpus-root .cache/corpus \
  --output tests/structural_breadth/results/corpus.json
```

For every selected file, production observations, metrics, source size, language,
status, and provenance must match a separate direct worker invocation. One- and
eight-worker output must be identical. Syntax recovery remains explicit in both
per-file and aggregate statuses. Receipts retain revisions, blob identities,
source hashes, executable hashes, and partial-result paths without copying the
upstream source contents into this repository.

These are integration comparisons against the same pinned BCA implementation,
not independent ground truth for metric formulas, exhaustive language-version
coverage, or whole-repository performance measurements. The separate
[Java/C# corpus checks](../structure/README.md) retain their broader sample.
Historical two-language release receipts do not certify the expanded worker or
its rebuilt native packages.

## Recorded results

The current [fixture receipt](results/fixtures.json) records 21 passing fixtures
across 20 production languages. The [corpus receipt](results/corpus.json) records
160 exact production/direct-worker comparisons, 20 per repository:

| Corpus | Language | Complete files | Files with syntax recovery |
| --- | --- | --- | --- |
| Flask | Python | 20 | 0 |
| Express | JavaScript | 20 | 0 |
| TypeScript | TypeScript | 18 | 2 |
| jq | C | 7 | 13 |
| ripgrep | Rust | 20 | 0 |
| Cobra | Go | 20 | 0 |
| Laravel | PHP | 20 | 0 |
| Rails | Ruby | 20 | 0 |

The partial results are retained, not removed from the comparison. The jq sample
includes generated and vendored C under explicit attributes; the TypeScript
sample includes conformance fixtures. These samples demonstrate why downstream
consumers must inspect structural status rather than assume a supported language
always yields a complete parse. Executable hashes in the receipts identify the
binaries tested; earlier release-package receipts describe a different worker.
