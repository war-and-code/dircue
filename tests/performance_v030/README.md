# 0.3 performance measurements

This harness compares the unchanged language-only command with the actual 0.2.0 binary and measures the additional cost of project mapping. It requires matching language stdout/stderr before timing a corpus.

```sh
python3 tests/performance_v030/run.py \
  --baseline bin/dircue \
  --candidate .cache/v030/dircue \
  --project spring-framework=.cache/corpus/spring-framework \
  --project roslyn=.cache/corpus/roslyn \
  --project aspnetcore=.cache/corpus/aspnetcore \
  --source git --warmup 1 --runs 3 \
  --output tests/performance_v030/results.json
```

Run on an otherwise idle host. Every command gets one explicit warmup before three alternating-order measurements. The report retains individual wall/CPU/RSS samples, executable and harness hashes, corpus commits, observed manifests/project roots, and ownership/composition count checks. Optional project mode performs more work than language mode; its cost is reported separately.

With three samples, nearest-rank p95 equals the observed maximum. These are short regression measurements, not reliable estimates of production tail latency or a promise of a particular speedup. Peak RSS comes from the target process via `/usr/bin/time`, using the existing metrics measurement helper. Timings include the small launcher overhead; filesystem caches are warm and are not flushed.

`passed` means execution, language-output equality, and project count invariants passed. It does not impose a performance threshold or hide slower measurements. Compare ratios and individual samples before making release claims.

The existing stress corpus can also be supplied with `--source directory`: its Talend-shaped fixture contains about 2 GiB of synthetic content, its XML log is fully written at 1,100 MiB, and its .NET graph has 2,048 projects. See [the stress fixture documentation](../stress/README.md) for fidelity limits and reproduction. Existing fixture sizes are logical content bytes; profiling throughput is not physical disk throughput because most content only needs a prefix read.
