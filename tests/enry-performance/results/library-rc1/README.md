# RC1 controlled Enry library baseline

**The maintained RC1 classifier is slower than official Enry v2.9.6 on both declared workloads.** These are identical preloaded `GetLanguage` API calls, not repository CLI timings.

| Input | Library | Warm median / p95 (s) | First pass median (s) | Bytes / allocations per call | Peak process RSS (MiB) | Warm CV |
|---|---|---:|---:|---:|---:|---:|
| full | official | 0.981 / 0.998 | 0.984 | 20,693 / 106.5 | 103.2 | 0.009 |
| full | maintained | 1.327 / 1.348 | 1.415 | 68,457 / 192.1 | 109.0 | 0.013 |
| prefix | official | 0.920 / 0.935 | 0.930 | 20,884 / 106.6 | 93.6 | 0.009 |
| prefix | maintained | 1.277 / 1.309 | 1.361 | 68,370 / 192.1 | 97.6 | 0.014 |

Each cell has 20 retained paired samples after 3 symmetric warmups and exceeds 10 seconds of warm measurement. All 80 timed samples and 4 original correctness outputs are archived. No timed samples were discarded. p95 uses the nearest rank; these small samples cannot establish extreme-tail latency.

- **full: BelowParity.** RC1 takes 35.2% longer. Official/RC1 median ratio 0.739; paired bootstrap 95% interval [0.730, 0.743].
- **prefix: BelowParity.** RC1 takes 38.8% longer. Official/RC1 median ratio 0.721; paired bootstrap 95% interval [0.716, 0.730].

Full content covers 3,388 files and 28,231,769 bytes. The 128 KiB variant covers the same files and 22,922,457 bytes, truncating 37 files. Every full-content maintained label equals the existing pinned Linguist 9.7.0 oracle; official Enry matches 3,241. All 147 differences remain in the reports. Prefix labels happen to equal each implementation’s full-content labels here; **no independent Ruby prefix oracle was run**, so this is not a prefix compatibility claim.

Warm timers exclude preload, explicit pre-profile GC, and JSON output construction. First pass includes lazy initialization but excludes process/package initialization. RSS covers the entire process, including preload and first pass; it is not classifier-only heap usage. Allocation figures are warm-loop MemStats deltas. CPU time and external process distributions remain in the audit and original reports.

Both drivers use identical source, optimized Go 1.26.6, CGO disabled, Linux arm64, one worker, GOGC=100, GOMEMLIMIT=off, and the same five-vCPU Docker Desktop VM. The official dependency is isolated from the root replace; the maintained fork and both binaries have retained hashes. This intentionally measures different classifiers/data with different accuracy, not identical classification decisions.

**Limitations:** this is one measurement window per input, not three repeated windows. No governor/turbo/SMT isolation or exclusive bare-metal host was established. Host hardware/power detail is incomplete inside Docker. Parent scheduling kept heavy agent work out of the timing window; that is coordination evidence, not an independent host-load trace. See the explicit waivers in `honest-gate.json`. Results establish an RC1 investigation baseline, not universal performance or cross-machine reproducibility.

## Audit and reproduction

`evidence.tar.gz` is deterministic and contains exact raw reports, correctness stdout/stderr, both manifests, build/fork receipts, baseline harness sources, input-copy receipts, and the existing Ruby sample oracle. `audit.json` is recomputed independently from these records; no benchmark executes during audit.

```sh
python3 tests/enry-performance/record_library.py --audit-only \
  --output tests/enry-performance/results/library-rc1
```

The sibling harness README documents rebuilding isolated drivers and rerunning the measurements. Use the archived harness snapshot when reproducing this baseline after later harness changes; manifests identify original input bytes and official archive provenance. New optimizations require new binaries and separate evidence directories. This record does not include profiles or attribute a hotspot.
