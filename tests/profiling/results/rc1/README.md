# RC1 profiles

These profiles explain costs in the frozen RC1 implementation. They are not
post-optimization speed claims. `evidence.tar.gz` contains 1,013 files: unprofiled
samples, scanner fingerprints, CPU/allocation/mutex profiles, pprof text, source
snapshots and build/input receipts. Its internal manifest hashes every payload;
`archive-audit.json` records a complete byte-for-byte archive readback.

## Baselines

| Scenario | Fresh processes | Scan median / p95 | Peak RSS | Interpretation |
| --- | ---: | ---: | ---: | --- |
| ripgrep directory, one worker | 100 | 143.1 / 226.9 ms | 40.8 MiB | Noisy diagnostic, Scan CV 26.7% |
| jq directory, one worker | 100 | 181.0 / 310.5 ms | 44.2 MiB | Noisy diagnostic, Scan CV 26.5% |
| Roslyn Git, five workers | 20 | 7.102 / 7.202 s | 344.0 MiB | Scan CV 1.1% |

Three excluded warmup processes precede each scenario. Small cases exceed ten
seconds of measured scanning, but their tail variability prevents precise speed
claims. Their quarter-window medians were relatively stable; p95 varied materially.
The Docker VM reported five CPUs; host applications were not suspended. The cause of small-case tail noise
is unresolved. These profiles support qualitative attribution only for those
cases. No samples were removed.

All invocations use a fresh process and fixed GOMAXPROCS=5, GOGC=100 and
GOMEMLIMIT=off. The Scan timer excludes package initialization, test setup, result
validation and receipt writing. GNU time's child metrics include the full process.
Files are on read-only ext4 volumes in Docker Desktop; filesystem caches were not
flushed. The raw-Git directory views bypass checkout filters. Each fingerprint
has the same complete result digest as its scenario baseline. Independent pinned
Linguist comparisons are retained in the conformance/public benchmark results;
repeatable digests alone do not establish correctness.

## Findings

- **Warm classifier:** tokenization accounts for 62.54% of 6.78 sampled CPU seconds
  over five complete sample passes. Regex capture matching accounts for 86.50% of
  cumulative sampled allocation volume. The allocation profile also includes the
  first pass and preloading; independent warm counters confirm excess allocation.
  See the [library baseline](../../../enry-performance/results/library-rc1/README.md)
  and [ranked handoff](../../HOTSPOTS.md).
- **Small cold scans:** lazy model JSON decoding accounts for 100 ms in the
  ripgrep profile, 76.92% of total sampled CPU. Model decoding allocates about
  27–31 MiB in the two small scenarios. CPU profiles are only 130/220 ms of samples,
  so percentages are coarse; initialization is clearly visible, but these are not
  precise estimates of its latency contribution.
- **Roslyn allocations:** total sampled allocation is 2,354 MiB. `io.ReadAll`
  contributes 650 MiB, go-git's `MemoryObject.Write` 595 MiB, buffered reader
  creation 315 MiB and line splitting 279 MiB. These are cumulative allocations,
  not simultaneous live memory or peak RSS. Generated-file checks account for
  283 MiB cumulatively; `getLines` currently splits complete content even when a
  predicate needs only its first few lines.
- **Roslyn concurrency:** the Git object mutex accounts for 22.60 seconds of
  aggregate goroutine waiting in the separate mutex profile. Wait durations across
  workers overlap; this is not 22.60 seconds of wall-clock latency. CPU samples
  also show decompression, hashing and object reading under that path. Lock
  changes require a separate reader/cache ownership proof and race tests.

CPU sampling uses `phase=scan` for scanner attribution. Memory sampling is
cumulative and includes package/test setup. CPU, memory and mutex samplers ran
in separate invocations after unprofiled baselines. Physical disk bandwidth,
off-CPU I/O and live heap high-water are unmeasured; logical source bytes and
system-call samples must not be presented as disk throughput.

## Next experiments

The first experiment is the guarded ASCII identifier lexer path, with complete
token and label equivalence. Separately, bounded line selection scores 20
(impact 4 × confidence 5 / effort 1): it targets the fourth-largest allocation
site while preserving LF-only splitting, trailing empty elements and reverse
footer order. It must remain a separate change and measurement. Model decoding
and Git reader changes remain distinct opportunities, with their own proofs and
before/after gates.

These measurements use one Apple Silicon Docker host and one baseline window per
scenario. No global power, governor, cache or kernel settings changed. See
`honest-gate.json` for explicit methodological limitations.
