# Production structural integration

`run.py` selects 50 tracked Java/C# files each from Spring Framework, Roslyn, and ASP.NET Core using the prototype's deterministic sampling method: spaced paths plus the largest eligible files, capped at 1 MiB. It copies their working bytes into temporary directories and uses explicit attributes to include generated/vendor samples.

For each corpus it compares production CLI output with one and eight file workers, then compares every file's observations, metrics, status, and provenance with a separate direct invocation of the pinned native worker. Syntax recovery must propagate to the aggregate status. This tests integration and determinism; it is not independent ground truth for BCA's metrics, nor a whole-repository performance measurement.

```sh
python3 tests/structure/run.py \
  --candidate .cache/v030/dircue \
  --worker prototypes/structural/worker/target/release/dircue-structural-worker \
  --corpus-root .cache/corpus \
  --output tests/structure/results.json
```

The receipt records source paths/hashes, corpus revisions, executable identities, exact comparison counts, and partial results. Source contents are not retained. Scanner tests separately exercise ordinary exclusions, Git snapshot selection, limits, worker failure, and shared content reads with scc.
