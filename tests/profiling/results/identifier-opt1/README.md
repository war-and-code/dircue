# Identifier opt1 library measurements

**The identifier experiment improves the historical RC1 measurements, but remains slower than the official library measured alongside it.** This is experimental source, not a managed release candidate.

| Input | Historical RC1 median (s) | Opt1 median / p95 (s) | Current official median / p95 (s) | Historical time decrease | Opt1 slower than current official |
|---|---:|---:|---:|---:|---:|
| full | 1.327173 | 1.178462 / 1.205729 | 0.976598 / 0.993834 | 11.2% | 20.7% |
| prefix | 1.276631 | 1.120242 / 1.129206 | 0.920643 / 0.927784 | 12.3% | 21.7% |

| Input | Library | First-pass median (s) | Warm bytes / allocations per call | Peak process RSS (MiB) | Warm CV |
|---|---|---:|---:|---:|---:|
| full | official | 0.981 | 20,709 / 106.5 | 103.1 | 0.010 |
| full | maintained | 1.268 | 57,744 / 166.0 | 106.8 | 0.012 |
| prefix | official | 0.923 | 20,792 / 106.6 | 93.8 | 0.005 |
| prefix | maintained | 1.209 | 57,883 / 166.0 | 97.6 | 0.007 |

All four cells retain 20 paired samples after 3 symmetric warmups and exceed 10 seconds of warm measurement. No timed sample was discarded. Official versus opt1 measurements are paired within each run; comparisons against RC1 are **historical separate windows**, not a paired before/after experiment. Both current scenarios are `ParityToMargin` under the declared three-tier policy, despite remaining slower than official Enry; neither is a speed win.

The identical preloaded GetLanguage API sees all 3,388 original sample files. Full content is 28,231,769 bytes; the 128 KiB window is 22,922,457 bytes. All 3,388 actual Ruby full-content labels and ordered token hashes/counts match, with zero reference errors. The retained rebuilt probe hash matches the gate receipt. Both runtime label maps match the unchanged RC1 maps, preserving all 147 differences from official Enry. There is no independent prefix Ruby oracle.

The managed `PROVENANCE.json` intentionally still describes RC1 at experiment time. It must not be used as proof of opt1 source. Separate before/after build inventories and capture hashes verify all 42 actual fork files: one runtime file changed and two tests were added. Those exact changed files, correctness logs, source inventories, build receipt, original reports and stdout artifacts are archived. All official timing binaries are byte-identical to RC1; the maintained experiment binary differs. The separately rebuilt conformance official probe has a different hash but identical pinned module/sums and all labels; it is not one of the timing binaries. Managed regeneration and broader pipeline acceptance remain separate work.

Warm allocation counters exclude setup; first pass includes lazy initialization; process RSS/CPU include preload and setup. The same optimized Go 1.26.6 static Linux arm64 drivers, source hash, runtime controls, manifests and five-vCPU Docker VM were used. VM host governor/turbo/SMT and unrelated applications were not experimentally isolated; only one window per input was measured. No universal speed or three-window stability claim is made. See `honest-gate.json` for explicit waivers.

The archive retains original RC1 reports as historical comparators without changing the separately recorded RC1 evidence. Audit replay performs no build, profile, or benchmark:

```sh
python3 tests/profiling/record_identifier.py --audit-only
```
