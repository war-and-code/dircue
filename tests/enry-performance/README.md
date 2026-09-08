# Upstream Enry performance comparison

The [final library and CLI comparison](results/final/README.md) is complete.
Controlled warm `GetLanguage` calls are **1.64× faster on full content** and
**1.71× faster on 128 KiB prefixes** than official v2.9.6 on the
3,388-file sample population. First-pass, whole-process, allocation and memory
costs are reported separately.

The CLI matrix includes all 11 public repositories and 14 stress variants.
Twenty-two rows are measured; three cutoff rows retain diagnostics only. Against
the official CLI, 19 measured rows are faster and three are inconclusive under
the declared margin/interval/p95 rule. Refreshed default and 128 KiB variants
have 22 faster rows each. Only six measured rows have identical path-to-language
maps across all tools; sixteen differ in inclusion or labels. The timings compare
complete processes on the same inputs, although the tools perform different work.
Stress variants are not independent workloads, and flat packed/unpacked pairs
do not measure Git pack handling. All losses and semantic differences are retained.

The historical RC1 full-content library baseline used 20 paired measurements over all
3,388 samples: the maintained classifier took a median 1.327 seconds per warm
corpus pass versus 0.981 seconds for official v2.9.6, or **35.2% longer**, and
allocated about **3.31 times** as many bytes per call. Its 147 differing labels
match the pinned Ruby oracle; these accuracy differences remain visible alongside
the timing result. The local receipt is
`.cache/enry-results-rc1/library-full-baseline.json`.

The historical RC1 128 KiB library baseline likewise took **38.8% longer**. Both
baselines, all label differences, raw measurements, build/input fingerprints,
and explicit environmental waivers are retained in the
[audited RC1 library record](results/library-rc1/README.md).

RC1 is preserved as the investigation baseline. The final evidence separately
records differing semantics, inconclusive cases, and memory costs.

## Baselines and attribution

The official CLI is a separate repository from the library. Its latest release
is [enry v1.3.0](https://github.com/go-enry/enry/releases/tag/v1.3.0), pinned here
to commit `f10711437bfbb25b15506eb69dde24bb7decd222`. Its
[module definition](https://github.com/go-enry/enry/blob/f10711437bfbb25b15506eb69dde24bb7decd222/go.mod)
uses go-enry **v2.8.4**. `pins.json` records the hashes of the official source,
module files, and Apache license, independently retrieved from that commit.

The comparison uses three baseline types:

| Name | Source | Question answered |
|---|---|---|
| Official Enry CLI v1.3.0 | Unmodified release source and dependencies | How does the published CLI perform for these checkouts? |
| Enry CLI dependency refresh | Identical CLI source, official library v2.9.6 | How much of the CLI result depends on its older library? This is an experiment, not an official release. |
| Controlled library drivers | Identical harness source, official v2.9.6 versus Dircue's maintained fork | What does the classifier implementation cost on identical preloaded bytes? |

Build official variants in fresh temporary modules outside the repository, with
`GOWORK=off`, explicit `GOMOD`, and no inherited `GOFLAGS` or replacements. Verify
`go list -m -json all`, the module sum, source hashes, `go version -m`, build flags,
and binary hashes. The official v2.9.6 module must have no `Replace` field and the
sum in `pins.json`. The maintained driver must record its explicit local replace
and fork provenance manifest. Never allow the root module's replace to affect an
official baseline. Preserve the official CLI license beside downloaded source.

Use Go 1.26.6, CGO disabled, Linux arm64, and the same optimization and trimpath
flags. Keep production binaries immutable. Make separate optimized builds with
symbols for profiles; do not turn off compiler optimizations or mix profiled
samples into timing results. Record a separate receipt for each binary.

## CLI work is similar but not identical

The pinned [CLI source](https://github.com/go-enry/enry/blob/f10711437bfbb25b15506eb69dde24bb7decd222/main.go)
walks working files serially. It does not implement Git revisions or attribute
overrides. Its default content window is 16 MiB. Dot, vendor, documentation,
configuration, and generated paths are excluded, and the default language types
are programming and markup. These policies differ from Linguist and Dircue.

For `-json -breakdown`, it emits language-to-file lists and does no byte-summary
pass. For `-json` alone, it opens and stats accepted files again to compute
percentages; that output includes language, percentage, color, and type, but no
file list or explicit byte count. Neither is Dircue's JSON schema. Its single
file mode can stream the entire file to count lines even when classification is
limited. The initial comparison therefore targets repository directories.

The primary end-to-end matrix uses JSON breakdown: untouched CLI defaults,
refreshed CLI defaults, refreshed CLI with a 128 KiB window, and Dircue
directory mode. Summary timing and the older CLI with a 128 KiB window are
optional follow-ups if profiles make them useful. The available API differences
are documented below; summary rows are not implemented in the initial runner:

| Mode | Enry arguments | Dircue arguments |
|---|---|---|
| Summary/default window | `-json PATH` | `--source directory --json PATH` |
| Summary/matched window | `-limit 128 -json PATH` | `--source directory --json PATH` |
| Files/default window | `-json -breakdown PATH` | `--source directory --json --breakdown PATH` |
| Files/matched window | `-limit 128 -json -breakdown PATH` | `--source directory --json --breakdown PATH` |

Every directory CLI receives the same directory view, materialized directly from
the pinned Git blobs. A clean checkout can still contain different bytes because
of checkout filters: ASP.NET Core has nine CRLF working files whose LF Git blobs
are 47 bytes smaller in total. The initial diagnostic retained that discrepancy;
it comes from the checkout's line endings. `materialize.py` bypasses checkout filters,
`export-ignore`, and `export-subst`, verifies every written byte against its Git
object ID and SHA-256, and records excluded symlinks/submodules. The measured
population is all regular Git blobs, rather than every possible checkout entry.
The comparison runner independently rehashes the views before invoking any tool.
Retain the Dircue Git result from the existing Linguist benchmark to represent
pipeline deployment. Label it as a different storage/access model; this runner
does not add a fifth timed Git cell. A CLI result where inclusion or classification
differs is an observed end-to-end runtime comparison, not proof that equal
classification work was completed faster.

## Declared workloads and correctness

Use all eleven existing pinned public repositories, including the added Java and
.NET repositories. Reuse their immutable content and receipts; do not pick only
the repositories where Dircue wins. Add the existing fourteen acceptance stress
cases as a separately reported matrix, with explicit per-case resource ceilings.
An Enry timeout or output error is a failure/censored observation, never a finite
speedup. Begin with correctness and resource diagnostics before scheduling twenty
long runs. Preserve generated-code and XML-detectable variants even though the
absence of Enry attributes means their results may be the same.

Before timing, capture JSON breakdown from every CLI. Normalize file lists into
path-to-language maps and derive sizes from the already verified file manifest.
Record missing, extra, relabeled files, byte totals, and percentages against the
actual Ruby oracle. Keep the original outputs, status, stderr, and hashes. Do not
silently drop mismatches, normalize away grouped-language differences, or apply
Dircue's selection rules to the official CLI result. Percentage-only output is
not sufficient to establish equal work.

Keep the existing all-sample accuracy result separate: official library v2.9.6
matches 3,241 of 3,388 pinned Ruby labels; Dircue matches 3,388. These numbers do
not measure the older CLI's accuracy. Regenerate the gate after any classifier
optimization, and retain the complete sample set.

## Controlled library benchmark

Build one identical Go benchmark/driver source in two isolated modules. The
timed API is `enry.GetLanguage(filename, content)` on both sides. Preload all
paths and bytes once before resetting the timer; use the same basename, content
window, stable order, number of calls, and output checksum. Do not include I/O,
JSON, directory filtering, attribute processing, or materialization on one side
only. Also time `enry.GetLanguagesByClassifier(filename, content, candidates)`
with identical explicit candidate sets to isolate tokenizer/model cost. The three-argument classifier API is present in the inspected source. The
initial driver supports it for diagnosis, but the primary matrix measures
GetLanguage on all samples with full content and matched prefixes.

The first two input groups are primary. Groups three and four are optional
follow-ups driven by measured hotspots, retaining the full population selected:

1. All 3,388 pinned Linguist samples, full content.
2. All those samples with the same raw 128 KiB prefix on both sides.
3. All preclassified real-repository file prefixes from the eleven-corpus
   manifest, including files not recognized by one side. This avoids selecting
   only classifier wins or only files admitted by one implementation.
4. Filename/extension-only, modeline/shebang, heuristic, and classifier-reaching
   strata, selected by reference strategy independently of elapsed time.

For forced-classifier comparisons, construct the union of both libraries'
extension candidates once outside the timed region and pass the identical list
to both. Report unsupported labels/model vocabulary differences explicitly.
The natural full-strategy population remains primary; forced classifier work is
diagnostic and must not substitute for it. Do not infer global correctness from
equal checksums: preserve the independently validated path-to-label outputs.

Measure a single worker first. Additional fixed worker counts (2, 4, and the
container CPU allowance) are optional scaling diagnostics. Both sides get exactly the same worker
allocation and total calls. The driver records explicit timed corpus passes, deterministic output sinks,
and runtime allocation deltas, with setup outside the warm timer. Preserve
raw nanoseconds, allocated bytes, and allocations per call. Goroutine dispatch
per corpus pass is identical on both sides and included in the timed work.
Use sequential independent process runs for comparison and profiles. A cached
preloaded library benchmark must never be called a cold-start CLI benchmark.

## Measurement and acceptance rules

Use the same Docker container, ext4 volume, read-only inputs, network disabled,
CPU allowance, GOMAXPROCS, and environment for both implementations. Record the
Docker VM and host distinction, architecture, toolchain, CPU, memory, kernel,
mount information, active workload isolation, cache policy, source and binary
hashes. Keep the host quiet. Do not change global kernel or power settings.

Discard three symmetric warmups, then rotate tool order with a recorded seed.
Collect at least twenty process samples and at least ten seconds of measured
wall time per cell; short cases need additional symmetric samples. GNU time
measures actual child peak RSS and user/system CPU, with wall time from a
monotonic parent clock. The small launcher avoids the inherited Python RSS
high-water artifact discovered in the earlier harness. Record every sample and
both directions of every ratio. Report median, empirical p95, max, CPU, peak
RSS, files/calls per second, and logical bytes per second. Higher empirical
quantiles have insufficient tail precision at twenty runs and must be labeled
accordingly. Logical input throughput is not physical disk bandwidth.

Use paired bootstrap confidence intervals for the ratio, retain the seed, and
investigate p95 drift above ten percent across three repeated measurement
windows. Classify inconclusive differences as inconclusive. A scoped faster
claim requires a median win with a confidence interval excluding parity and no
unexplained p95 regression. Publish all losses, resource failures, and mismatches
alongside wins. Do not collapse correctness and speed into a single score.

## Profiling

After the unprofiled baseline, use the same controlled driver to collect Go CPU
and heap profiles for both official and maintained libraries. The implemented
driver uses runtime/pprof with optimized symbols; preserve `pprof -top`
plus call graphs. Capture block/mutex profiles only for the worker-scaling
scenario, and process CPU/elapsed ratios plus I/O observations for CLI cases.
For Dircue scanning attribution, use a separate measurement-only program that
invokes its scanner and wraps `runtime/pprof`; do not add production CLI flags or
change behavior to collect a profile.

Produce `fingerprint.json`, `build-receipts.json`, `correctness.json`, raw outputs,
`timings.json`, CPU/allocation profiles, a ranked hotspot table, and a hypothesis
ledger. Every hotspot needs a source location and an artifact citation. Candidate
hypotheses include serial filesystem traversal, path regexes, content reads,
tokenizer allocations, centroid scoring, JSON output, and worker scheduling;
these are hypotheses to test. Rank targets from the evidence before choosing
an optimization.

Record the fourteen-item measurement acceptance checklist beside the results.
Document unavailable host controls as limitations. Performance claims must
identify the measured workloads and host, including the cases where Dircue
does more or less work than the comparison tool.

## Implemented entry points

These commands are reproduction templates for a quiet measurement window.
Builds, correctness diagnostics, and controlled library measurements have been
executed with separately recorded output paths; the final CLI timing matrix
is recorded in `results/final`. All output directories must be fresh to preserve prior evidence.

```sh
# Host: download only pinned CLI source and module dependencies, then cross-build.
python3 tests/enry-performance/build.py --output "$PWD/.cache/enry-builds"

# Linux reference container: mount the whole tests/ tree read-only because the
# new runner imports the existing GNU-time and fixture-verification helpers.
python3 /tests/enry-performance/compare.py \
  --builds /enry-builds --candidate /candidate/dircue \
  --candidate-receipt /receipts/build-receipt.json \
  --corpus /tests/performance/corpus.json --corpus-root /corpus \
  --corpus-flat-root /corpus-flat \
  --stress /stress/acceptance-final --output /results/cli-correctness.json \
  --runs 0

# Repeat with a fresh output path and --runs 20 for the full matrix. The default
# is 20 measured rounds, 3 warmups, and a ten-second floor, capped at 2,000 rounds;
# cells that miss the floor are explicitly advisory. A --case selection is
# diagnostic and recorded as partial, never complete coverage of the matrix.

# Linux: point root at a separately verified extraction of all pinned samples.
python3 /tests/enry-performance/manifest.py --root /samples \
  --expected-files 3388 --prefix 0 --output /results/samples-full.json
python3 /tests/enry-performance/manifest.py --root /samples \
  --expected-files 3388 --prefix 131072 --output /results/samples-prefix.json
python3 /tests/enry-performance/library.py --builds /enry-builds \
  --manifest /results/samples-full.json --output /results/library-full.json
python3 /tests/enry-performance/library.py --builds /enry-builds \
  --manifest /results/samples-prefix.json --output /results/library-prefix.json
```

For profiles, create a separate `build.py --profile` output directory and use
`library.py --profile-directory /results/profiles` with a fresh result path.
The driver sets CPU/heap profile paths through `DIRCUE_PROFILE_CPU` and
`DIRCUE_PROFILE_HEAP`; the runner rejects inherited values to prevent accidental
instrumentation of timing. Profile results are explicitly marked and have no
speedup summary. The pilot used for calibration is retained separately from
measured rounds. The same number of corpus passes is then used by both drivers.

The classifier manifest hashes the exact measured prefix and inventories every
regular file except Git internals and symlinks. It does not download or verify
the upstream archive; use the existing pinned archive verification first.
Candidate sets for the optional forced-classifier mode must be supplied in a
separately documented manifest. They are not silently inferred or filtered by
this runner. Optional reference-strategy strata, automatic pprof interpretation,
repeat-window drift checking, and publication attestation remain subsequent
steps; no harness result currently implies those gates have passed.

Additional source-review safeguards: the builder uses fresh temporary module and
compiler caches, disables persisted Go environment settings, selects the exact
Go toolchain (`GOTOOLCHAIN=go1.26.6`, without `+auto`) and architecture feature floors,
and records complete Go environment
output. The CLI runner rejects a candidate with different compiler, architecture,
experiment, or toolchain settings. All measured children receive GOGC=100 and
GOMEMLIMIT=off with GODEBUG cleared; shared GOMAXPROCS is recorded.

CPU profiling brackets the warm loop after explicit GC and before output hashing,
maps, and JSON. Heap snapshots precede output construction but still include
retained preloaded input and first-pass initialization. Use explicit warm-call
MemStats deltas for call allocation cost; cumulative heap profiles must not be
attributed wholly to the classifier. Paired library rounds extend until both warm
durations reach ten seconds or the recorded cap is reached. A capped run below
the floor is not an accepted performance baseline. Candidate repository results
must match Ruby byte accounting and the complete JSON contract before any speed
verdict can be accepted; Enry semantic differences remain visible separately.

Cells whose file count reaches or exceeds the effective tree cutoff, and cells
with empty Ruby results, retain correctness and one-shot
resource diagnostics only: their repeated timings are explicitly unmeasured and
advisory. Enry has no tree cutoff, so repeatedly timing its full 100,000-file scan
against an empty candidate result would not establish equal-work performance.
The 100,001 cutoff on 100,000 files remains timed because both tools scan the
complete population; merely specifying a tree limit does not make a cell advisory.
Only nonadvisory CLI cells use the adaptive ten-second floor, capped at 2,000
rounds. A faster verdict requires at least a 10% median advantage, a paired
confidence interval excluding parity, and no empirical p95 regression. Smaller
differences remain inconclusive even if statistically distinguishable.

The final pre-rename archive and report keys retain their original project name.
`record_final.py --audit-only` verifies the exact pinned archive before running
its preserved audit helpers in an isolated temporary tree. It no longer captures
new experiments; use the current build and comparison harnesses for new work.
