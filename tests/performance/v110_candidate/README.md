# 1.1 candidate measurements

These results compare an actual published 1.0.1 executable with a source-built candidate on one macOS ARM64 host. They record local workload behavior, not a universal performance guarantee or an independent map accuracy estimate.

## Historical timing scope

The [primary receipt](https://github.com/war-and-code/dircue/releases/download/evidence-archive-1/v110-review-original-macos-arm64.json.gz) records five interleaved AB/BA pairs after warming both executables. `/usr/bin/time -l` supplied peak RSS; the parent measured wall time. Every measured language/format invocation matched the release's stdout, stderr and exit status exactly. Map output is intentionally different because the candidate adds observations. Both Ruff maps retained the same skipped-symlink warning.

| Workload | Released median | Candidate median | Released / candidate median peak RSS |
| --- | --- | --- | --- |
| Spring Petclinic languages | 42.54 ms | 46.96 ms | 41.70 / 41.27 MiB |
| ASP.NET Core `src/Http` languages | 111.08 ms | 113.71 ms | 40.94 / 39.74 MiB |
| Synthetic XML-prefix formats | 22.30 ms | 22.12 ms | 22.37 / 22.66 MiB |
| Spring Petclinic map | 88.25 ms | 92.49 ms | 43.24 / 42.64 MiB |
| Ruff map | 2.824 s | 2.849 s | 72.19 / 71.28 MiB |

These compact workloads do not represent the largest repositories. A separate review comparison reported longer map times on three larger checkouts:

| Repository | Published 1.0.1 | Candidate | Change |
| --- | ---: | ---: | ---: |
| Spring Framework | 1.10 s | 1.40 s | +27% |
| ASP.NET Core | 1.8–1.9 s | 2.1 s | +15% |
| Roslyn | 5.9–6.1 s | 6.3–6.4 s | +5% |

The raw samples for this separate comparison are not in the retained timing receipt, so these figures are reported measurements rather than independently reproducible results from this directory. The compact receipt above does not establish that large-map runtime is unchanged.

The small language timings warranted more samples. The [extended receipt](https://github.com/war-and-code/dircue/releases/download/evidence-archive-1/v110-review-original-extended-macos-arm64.json.gz) contains 30 Java and 15 .NET pairs. Candidate-minus-release paired medians were +0.33 ms and +0.89 ms, respectively; means were +0.12 ms and -0.29 ms. The candidate was slower in 16/30 Java pairs and 8/15 .NET pairs. The distributions overlap widely. These observations do not establish a consistent language-path regression, nor do they prove exact performance equivalence. The map medians also do not support claiming a speedup; added analysis can cost time or memory.

## Corrected candidate measurements

The archived [corrected large-map receipt](https://github.com/war-and-code/dircue/releases/download/evidence-archive-1/v110-review-corrected-large-macos-arm64.json.gz) supersedes the earlier review estimates above for these three repositories. It records 20 warm, interleaved AB/BA pairs per repository using Git input mode and identical pinned checkouts. Median elapsed times and median peak RSS were:

| Repository | Published 1.0.1 | Corrected candidate | Median change | Median peak RSS, published / candidate |
| --- | ---: | ---: | ---: | ---: |
| Spring Framework | 1.120 s | 1.261 s | +12.645% | 145.9 / 149.1 MiB |
| ASP.NET Core | 1.801 s | 1.946 s | +8.028% | 226.0 / 226.6 MiB |
| Roslyn | 5.725 s | 5.951 s | +3.945% | 307.0 / 304.8 MiB |

These measurements show a runtime cost on the tested large repositories. Peak RSS rose by 3.2 MiB on Spring Framework and 0.5 MiB on ASP.NET Core, and fell by 2.2 MiB on Roslyn. They were collected on a shared Apple M1 Max developer host with warm filesystem caches and no OS tuning or isolation; they are evidence about these workloads, not a stable cross-machine performance guarantee. The receipt binds the candidate to source commit `063cf27380bc3bd9bcd35ca5ffdabb76add00e8f` and executable SHA-256 `45631f32ca09ccbae18c8c89b115cd1a09375f1fd3308c558bfc393aa6b5d508`.

Both binaries used Go 1.26.6, CGO disabled and trimpath. The published executable is symbol-reduced; the candidate retains debug information. Exact published linker strip flags cannot be recovered from embedded metadata, so binary-size and resource differences cannot be attributed solely to source changes.

The original small-workload candidate was source commit `8e61302e07e0301b4f896d7ae5969f51125e5eb7`. Historical compatibility and packaging receipts use `a205c3b`. The corrected candidate measured above uses `063cf27`; none of those earlier receipts should be treated as a measurement of this later binary.

## Inputs and reproduction

The historical small-workload receipts retain binary hashes, command arguments, source/build metadata, regular-file content digests, individual measurements and capture hashes. Those workloads are pinned to:

- Spring Petclinic: `818c4136ea971c21674525f9053de0d9c7ad8cfe`.
- ASP.NET Core: `7387de91234d3ef751fa50b3d1bfede4130213ff`, subtree `src/Http`.
- Ruff: `ea544ce22a7db999b26cd66be76b88d27b9170b5`.
- One synthetic 128 MiB-class XML-shaped file, outside a Git repository.

The corrected large-map workload pins are Spring Framework `9e8cea3ef8ae02efb7956b071cd7bbef7c22cb82`, the complete ASP.NET Core checkout at the revision above, and Roslyn `ca7d6c1a040cda9fecd1ffe3720fb971251ace67`. The readable [large-repository harness](large_repositories.py) requires macOS, caller-supplied checkouts and pinned executables. Its receipt records Git revisions, clean status, tracked-path digests, raw capture hashes, all samples and p95 values. Parent wall times include subprocess timing overhead; twenty samples on one shared host do not characterize long-tail production latency. The [raw capture archive](https://github.com/war-and-code/dircue/releases/download/evidence-archive-1/v110-review-corrected-raw-captures.tar.gz) retains the measured output bytes.

The synthetic file tests prefix recognition, not whole-document validity or bulk XML parsing. The formats scan read its bounded 65,537-byte prefix. The initial language-only pilot emitted no languages because XML is outside the default language-statistics scope; that pilot is excluded from the final timing matrix. The tree digests omit symlinks and `.git` entries; they are not raw filesystem snapshots.

The experiment scripts are readable source: [primary harness](benchmark-harness.py), [formats harness](formats-harness.py), [extended harness](extended-harness.py), and [synthetic generator](xml-generator.py). They document the original cache layout and macOS timing commands, rather than a portable CI runner. To repeat the experiment, obtain the pinned workloads and published executable, build the recorded candidate, and adapt cache paths to fresh output directories. Retain new receipts instead of overwriting historical results.

Historical measurements are release assets, listed with sizes and SHA-256 hashes in [the evidence manifest](../../receipts/evidence-archive.json). Run `make fetch-receipts` to restore their ignored local paths. Host workspace prefixes in command/build metadata use `{workspace}`; captured output and capture hashes are unchanged. The original primary and extended receipts describe the pre-review candidate, not the corrected binary. Fresh validation must identify its own build. No scheduled or network-fetching benchmark was enabled.

## Map regression oracle

The corrected candidate score uses the unchanged seven-repository labels and scorer. The archived [published-1.0.1 score](https://github.com/war-and-code/dircue/releases/download/evidence-archive-1/v110-review-original-golden-released-v101.json.gz) matches the reference [source-build score](https://github.com/war-and-code/dircue/releases/download/evidence-archive-1/v110-review-original-golden-v101-sourcebuild.json.gz), which used commit `510575cb7878fbb5184444d8efd976ee37a8c0b3`, whose change after 1.0.1 was documentation only. The corrected candidate rerun resolves two relationship misses, removes one false relationship and one interface miss. The archived `golden-candidate.json.gz` is the historical `a205c3b` result (219 / 0 / 1); it is intentionally retained as that historical receipt and is not the current score below. The [corrected score receipt](https://github.com/war-and-code/dircue/releases/download/evidence-archive-1/v110-review-corrected-golden.json.gz) binds the result to the candidate executable and preserves the historical score separately:

| Class | Reference TP / FP / FN | Candidate TP / FP / FN |
| --- | --- | --- |
| Components | 15 / 0 / 0 | 15 / 0 / 0 |
| Deployables | 40 / 0 / 0 | 40 / 0 / 0 |
| Interfaces | 46 / 0 / 1 | 47 / 0 / 0 |
| Capabilities | 46 / 0 / 0 | 46 / 0 / 0 |
| Relationships | 216 / 1 / 4 | 218 / 0 / 2 |
| Coverage statuses | 17 / 0 / 11 | 17 / 0 / 11 |

The two remaining relationship misses concern workload capability modeling (#103) and Ruff's root Dockerfile. Ruff's build stage copies `crates`, runs `cargo zigbuild`, then copies `/ruff` from that stage; the preceding `cp` uses a dynamic target path, so the static analyzer cannot verify that artifact flow and leaves the build edge unresolved. Coverage-status disagreements are unchanged; this sprint does not expand every bounded observer into exhaustive coverage. These are adjudicated regression results. Labels were not changed to accommodate the candidate, and the scores do not establish accuracy on arbitrary repositories.

The separate [corrected public-quality receipt](https://github.com/war-and-code/dircue/releases/download/evidence-archive-1/v110-review-corrected-public-quality.json.gz) records 176/0/0 over its bounded evaluated facts. Its nine sources include seven pinned public repositories, a checked-in TypeScript import fixture and a non-source fixture; the public labels were source-adjudicated, not independently blind-labeled. This score has its own denominator and should not be combined with the seven-repository golden score. The [pinned qualitative receipt](https://github.com/war-and-code/dircue/releases/download/evidence-archive-1/v110-review-corrected-public-qualitative.json.gz) separately verifies all 44 cited assertions across 21 pinned repositories; seven additional questions are intentionally unknown.
