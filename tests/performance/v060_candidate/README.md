# 0.6 candidate measurements

These harnesses separate preservation of existing commands from the added cost of optional format and hotspot profiling. They run already-built executables on fixed inputs, retain timing samples and output hashes, and refuse to overwrite evidence directories. They do not run builds, scripts or package managers from inspected repositories.

## Build and identify a comparison candidate

From the repository root, use fresh output paths:

```sh
python3 tests/performance/v060_candidate/benchmark.py build \
  --candidate .cache/v060-measure/dircue \
  --build-receipt .cache/v060-measure/build.json
```

The build records Go source, embedded schema and module-input hashes before and after compilation. It also checks that the working-tree state did not change during the build. Stop other writers while it runs. `CGO_ENABLED=0`, `GOWORK=off`, `-trimpath`, `-buildvcs=false` and release-style linker flags are explicit.

Only the reported version is overridden to **0.5.0**, so complete output can be compared with that release without removing version fields. This executable is a comparison candidate, not a 0.5.0 release artifact. Its receipt records the actual source revision and whether the source was dirty.

The [compatibility harness](../../compatibility_v060/README.md) accepts the same build receipt. Its 278-case native matrix includes 0.5 declaration and saved-report comparison commands; the timing lanes below do not separately benchmark declarations.

## Existing paths and optional formats

Obtain and verify the published 0.5.0 binary for the host. The default reference digest identifies macOS ARM64; pass `--baseline-sha256` on other platforms.

```sh
python3 tests/performance/v060_candidate/benchmark.py measure \
  --baseline /verified/v0.5.0/dircue \
  --candidate .cache/v060-measure/dircue \
  --build-receipt .cache/v060-measure/build.json \
  --corpus-root /path/to/pinned-corpus \
  --xml-root /path/to/large-xml-fixture \
  --repetitions 10 --warmups 3 --workers 8 \
  --environment-note 'Describe hardware, background activity and other relevant conditions.' \
  --output .cache/v060-measure/formats
```

`--corpus-root` expects the commits pinned in [corpus.json](../corpus.json) for Cobra, Express, Flask, Ripgrep, Roslyn and Spring Framework. Each runs in Git mode; Roslyn also runs as a directory. `--xml-root` is optional and requires at least 1 GiB of readable regular-file XML content. An explicit `--case name:git:/path` or `--case name:directory:/path` can select another input.

Each case has six lanes:

| Lane | Purpose |
|---|---|
| Released / candidate language command | Exact existing output and timing comparison |
| Released / candidate `analyze all` | Exact existing aggregate output and timing comparison |
| Candidate `analyze all --formats` | Incremental format-inspection cost |
| Candidate `analyze formats` | Standalone format-inspection cost |

Baseline/candidate outputs for existing commands must match byte for byte. Removing only the new `formats` member and restoring the old schema version must recover the existing aggregate output. Standalone and combined format modules must agree. Repeated outputs, corpus inventories and executable identities must remain stable.

Directory inventory hashes complete file contents outside timed sections. The format module itself inspects bounded prefixes: its `inspected_bytes` counter includes charged lookahead, but excludes other module reads, attributes, Git decompression and unrelated process I/O. A fast scan of a multi-gigabyte XML directory therefore does **not** establish full-content parsing or multi-gigabyte read throughput. XML evidence does not establish a log-file role.

## Incremental hotspot cost

Prepare fixed structural inputs before measuring:

```sh
python3 tests/performance/v060_candidate/structure.py prepare \
  --corpus-root /path/to/pinned-corpus \
  --files 40 \
  --output .cache/v060-measure/structural-inputs

python3 tests/performance/v060_candidate/structure.py measure \
  --prepared .cache/v060-measure/structural-inputs \
  --candidate .cache/v060-measure/dircue \
  --worker /verified/candidate/dircue-structural-worker \
  --build-receipt .cache/v060-measure/build.json \
  --repetitions 5 --warmups 1 --workers 8 \
  --environment-note 'Describe hardware, background activity and other relevant conditions.' \
  --output .cache/v060-measure/hotspots
```

Preparation reads pinned Git blobs from Spring Framework, Roslyn and Flask. It selects a bounded spread of paths plus large files, restricted to regular UTF-8 source blobs of at most 1 MiB; the exact selection and hashes are recorded. Attributes explicitly include these selected sources, even where their original repository might classify them as generated or vendored. These are source subsets, not whole-repository measurements.

Two additional inputs cover all 21 existing structural fixtures across 20 languages, and the 1,315-function authored population with a late high-valued function beyond both historical retention caps.

For each prepared input, the harness alternates `analyze structure --files` with the same command plus `--hotspots`. It verifies one parse per analyzed file, source-bound rankings, population/histogram invariants and preservation of earlier report fields. If invalid function spans make hotspot evidence partial, the recorded qualification distinguishes that from underlying file coverage. The helper records both worker and core hashes; its shared timing implementation is identified separately in the receipt.

## Interpretation and limits

Both harnesses require at least five measured repetitions and one warmup. They record individual samples, medians, observed minimum/maximum times and peak resident-set statistics. Lane order reverses on alternating rounds. Input hashing and warmups make these **warm-cache** measurements; no cold-cache eviction or host isolation is claimed.

Wall time includes CLI startup and the timing wrapper, while excluding fixture construction and output validation. Structural timing also includes worker startup and capability probes. macOS uses `/usr/bin/time -l`; Linux requires GNU `/usr/bin/time`. Windows runtime correctness is covered separately by native CI, not by these timing helpers.

For the single-process core, RSS is the operating system's peak resident-set measurement. For structural runs, the reported maximum is **not a simultaneous sum of core and worker memory**, a process-tree budget or an enforced resource limit. Report elapsed-time ranges and memory changes together; small median differences alone do not establish a universal speedup or statistical equivalence.

A receipt establishes results for its recorded executables, fixtures and environment. Later source edits require a new candidate or an explicit source-input equivalence check. Published summaries should retain sample counts, corpus scope, qualifications and receipt identities.
