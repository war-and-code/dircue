# Scanner profile refresh after lexer subset optimization

Source `390ce5fbcb605eec72af524368d03bdf53715b24`, Go 1.26.6, optimized unstripped Linux arm64 scanner test binary `74ce945cd2a39eabfc1a816ccdbf411e0e7f3cfcbe4c7c6274632ccdd980bdda`. All 232 baseline/warmup/profile fingerprints match the immutable RC1 full result digests. No scanner binary is included; committed source snapshots and build controls are retained.

| Scenario | Measured runs | Scan median / p95 (s) | Scan CV | Process median (s) | Peak process RSS (MiB) |
|---|---:|---:|---:|---:|---:|
| ripgrep-directory-w1 | 100 | 0.146120 / 0.246958 | 0.257 | 0.171082 | 41.1 |
| jq-directory-w1 | 100 | 0.156299 / 0.268525 | 0.235 | 0.180652 | 42.1 |
| roslyn-git-w5 | 20 | 7.104411 / 7.189296 | 0.007 | 7.145693 | 346.5 |

Each scenario has 3 excluded warmup processes. All measured scan totals exceed 10 seconds. No samples were discarded. First-scan timing starts after Go package initialization; external process duration includes setup, scanning, fingerprints and shutdown. Filesystem caches were uncontrolled and warmed by integrity verification.

Compared with the separate RC1 window, Roslyn remains essentially unchanged (7.1044 s vs 7.1024 s). Small scans retain high variability: ripgrep 146.1 ms vs 143.1 ms and jq 156.3 ms vs 181.0 ms. These small-case comparisons are diagnostic, not accepted speedup claims.

Separate short CPU profiles identify model loading as a candidate: jq loadCentroids has 90 ms cumulative CPU out of 160 ms total sampled CPU (140 ms tagged phase=scan); ripgrep has 120 ms out of 180 ms total (160 ms tagged). Pprof displays 56.25% and 66.67% using the total-sample denominators. These are only 16 and 18 sampled CPU ticks, so exact rankings/percentages are uncertain. CPU may exceed wall duration because more than one thread contributes.

The separate Roslyn allocation profile reports 2,413.10 MB cumulative allocated space in pprof display units. io.ReadAll accounts for 650.06 MB, MemoryObject.Write 597.44 MB, bufio.NewReaderSize 335.77 MB, and bytes.genSplit 276.06 MB. data.getLines accumulates 275.52 MB (11.42%). This supports a bounded getLines allocation experiment; it does not establish its latency gain. Cumulative allocations include setup/runtime and are distinct from peak RSS and retained heap.

This refresh precedes any getLines or model-startup change. Baselines and profiled invocations are kept separate. Host limits remain Docker Desktop, uncontrolled governor/turbo/SMT and cache state, one historical window per version, and no independent quiet-host trace. No universal or three-window stability claim is made.

`evidence.tar.gz` retains raw profiles, all fingerprints and samples, symbol/build/input receipts, pprof commands and text, committed source snapshots, the recording script and helper, and a per-member SHA-256 manifest. Extract it to inspect or independently replay the calculations; original paths inside receipts identify their measurement-time mounts.
