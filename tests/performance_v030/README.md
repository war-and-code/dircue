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

## Recorded checkpoint

The retained measurements use Go source commit `f7a90b109036bb542c00f481e14e25aaa911b678`, before the later Maven 4.1 namespace addition. The macOS arm64 candidate SHA-256 is `671e783b1f3522cc88782318c457da9d6becb4432e6ada28c202908c94babf08`. All three public corpus language outputs matched the published 0.2.0 binary exactly.

| Corpus | 0.2 language median | Candidate language median | Project mapping median | Project mapping peak RSS | Language observed p95 ratio |
| --- | ---: | ---: | ---: | ---: | ---: |
| Spring Framework | 1.559 s | 1.529 s | 1.570 s | 167.7 MiB | 0.980 |
| Roslyn | 6.254 s | 6.133 s | 6.212 s | 362.0 MiB | 1.006 |
| ASP.NET Core | 2.076 s | 2.091 s | 2.109 s | 225.0 MiB | 1.011 |

These small differences do not establish a speedup. Project mapping identified 508 .NET projects and 35 solutions in Roslyn, and 605 .NET projects among ASP.NET Core's mixed ecosystem entries. The public corpus reports are partial: they retain malformed-manifest diagnostics, omitted special files, unresolved expressions, missing targets, and ambiguous directory ownership. Gradle module declarations can name directories whose custom build scripts are outside the recognized manifest names; such references do not automatically become additional project roots.

The large synthetic fixtures ran separately in a read-only Linux container with networking disabled, two CPUs, and a 512 MiB memory limit with no swap. The Linux arm64 candidate SHA-256 is `5d353a6dd824d2b0dbbed4705ee3d6871405b31a14ae187d5fd17d8917a5e48a`.

| Fixture | Verified inventory | Single diagnostic wall time | Peak RSS |
| --- | --- | ---: | ---: |
| Talend-shaped | 2 GiB, 8,293 files, one Maven root | 7.60 s | 30.7 MiB |
| XML log | 1,100 MiB log classified as data, excluded from language statistics | 0.026 s | 18.8 MiB |
| .NET graph | 2,048 projects, 4,094 present references, 4,100 files | 0.386 s | 40.5 MiB |

All three stress reports have complete selected-inventory coverage, matching file/byte totals, and no structural or scc analysis requested. These are single diagnostic measurements; local release builds were allowed concurrently on the host after the native timing window, so do not treat the stress wall times as controlled performance comparisons. The read-only fixture's current sizes and allocations were checked against its retained generation manifest; this run did not reread every payload to verify its content hash.

`results.json` and `stress-results.json` retain samples and provenance. `stress-artifacts/` contains the compressed project reports. Verify recorded arithmetic, hashes, and count invariants with:

```sh
python3 tests/performance_v030/audit.py
```

To repeat the container checks after building a Linux arm64 candidate:

```sh
docker run --rm --network none --read-only \
  --cpus 2 --memory 512m --memory-swap 512m \
  --tmpfs /tmp:rw,noexec,nosuid,size=32m \
  -v "$PWD:/repo:ro" -v "$PWD/tests/performance_v030:/results" \
  -v auragaze-stress-v1:/stress:ro -w /repo \
  --entrypoint python3 dircue-linguist:9.7.0 \
  /repo/tests/performance_v030/stress.py \
  --candidate /repo/.cache/v030/dircue-linux-arm64 \
  --fixtures /stress/acceptance-final --output /results/stress-results.json
```

The historical volume name refers to the project's original name; its contents are the synthetic fixtures documented in the stress suite.
