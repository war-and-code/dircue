# 1.1 candidate measurements

These results compare an actual published 1.0.1 executable with a source-built
candidate on one macOS ARM64 host. They record local workload behavior, not a
universal performance guarantee or an independent map accuracy estimate.

## Timing scope

The [primary receipt](results/macos-arm64.json.gz) records five interleaved
AB/BA pairs after warming both executables. `/usr/bin/time -l` supplied peak
RSS; the parent measured wall time. Every measured language/format invocation
matched the release's stdout, stderr and exit status exactly. Map output is
intentionally different because the candidate adds observations. Both Ruff
maps retained the same skipped-symlink warning.

| Workload | Released median | Candidate median | Released / candidate median peak RSS |
| --- | --- | --- | --- |
| Spring Petclinic languages | 42.54 ms | 46.96 ms | 41.70 / 41.27 MiB |
| ASP.NET Core `src/Http` languages | 111.08 ms | 113.71 ms | 40.94 / 39.74 MiB |
| Synthetic XML-prefix formats | 22.30 ms | 22.12 ms | 22.37 / 22.66 MiB |
| Spring Petclinic map | 88.25 ms | 92.49 ms | 43.24 / 42.64 MiB |
| Ruff map | 2.824 s | 2.849 s | 72.19 / 71.28 MiB |

The small language timings warranted more samples. The
[extended receipt](results/extended-macos-arm64.json.gz) contains 30 Java and
15 .NET pairs. Candidate-minus-release paired medians were +0.33 ms and
+0.89 ms, respectively; means were +0.12 ms and -0.29 ms. The candidate was
slower in 16/30 Java pairs and 8/15 .NET pairs. The distributions overlap
widely. These observations do not establish a consistent language-path
regression, nor do they prove exact performance equivalence. The map medians
also do not support claiming a speedup; added analysis can cost time or memory.

Both binaries used Go 1.26.6, CGO disabled and trimpath. The published executable
is symbol-reduced; the candidate retains debug information. Exact published
linker strip flags cannot be recovered from embedded metadata, so binary-size
and resource differences cannot be attributed solely to source changes.

The timed candidate was source commit
`8e61302e07e0301b4f896d7ae5969f51125e5eb7`. A later change constrained Cargo
attribution to root Dockerfiles with known contexts; the final compatibility
and packaging receipts are tied separately to `a205c3b`. Do not treat the
timing receipt as a measurement of a different binary.

## Inputs and reproduction

The receipts retain binary hashes, command arguments, source/build metadata,
regular-file content digests, individual measurements and capture hashes.
Workloads are pinned to:

- Spring Petclinic: `818c4136ea971c21674525f9053de0d9c7ad8cfe`.
- ASP.NET Core: `7387de91234d3ef751fa50b3d1bfede4130213ff`, subtree `src/Http`.
- Ruff: `ea544ce22a7db999b26cd66be76b88d27b9170b5`.
- One synthetic 128 MiB-class XML-shaped file, outside a Git repository.

The synthetic file tests prefix recognition, not whole-document validity or
bulk XML parsing. The formats scan read its bounded 65,537-byte prefix. The
initial language-only pilot emitted no languages because XML is outside the
default language-statistics scope; that pilot is excluded from the final
timing matrix. The tree digests omit symlinks and `.git` entries; they are not
raw filesystem snapshots.

The archived experiment scripts are
[the primary harness](results/benchmark-harness.py.gz),
[the formats harness](results/formats-harness.py.gz),
[the extended harness](results/extended-harness.py.gz) and
[the synthetic generator](results/xml-generator.py.gz).
They document this experiment's cache paths and macOS timing commands, rather
than defining a portable CI runner. To repeat the experiment, extract them
with `gzip -dc`, obtain the pinned workloads and published executable, build
the recorded candidate, and adapt the cache paths to fresh output directories.
Retain new receipts rather than overwriting these results. No background
schedule or network-fetching benchmark was enabled.

## Map regression oracle

The [candidate score](results/golden-candidate.json.gz) uses the unchanged
seven-repository labels and scorer. The
[published-1.0.1 score](results/golden-released-v101.json.gz) matches the reference
[source-build score](results/golden-v101-sourcebuild.json.gz), which used commit
`510575cb7878fbb5184444d8efd976ee37a8c0b3`, whose change after 1.0.1 was
documentation only. The final candidate resolves three relationship misses,
one false relationship and one interface miss:

| Class | Reference TP / FP / FN | Candidate TP / FP / FN |
| --- | --- | --- |
| Components | 15 / 0 / 0 | 15 / 0 / 0 |
| Deployables | 40 / 0 / 0 | 40 / 0 / 0 |
| Interfaces | 46 / 0 / 1 | 47 / 0 / 0 |
| Capabilities | 46 / 0 / 0 | 46 / 0 / 0 |
| Relationships | 216 / 1 / 4 | 219 / 0 / 1 |
| Coverage statuses | 17 / 0 / 11 | 17 / 0 / 11 |

The remaining relationship miss concerns workload capability modeling (#103).
Coverage-status disagreements are unchanged; this sprint does not expand every
bounded observer into exhaustive coverage. These are adjudicated regression
results. Labels were not changed to accommodate the candidate, and the scores
do not establish accuracy on arbitrary repositories.
