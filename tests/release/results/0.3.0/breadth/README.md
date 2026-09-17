# Expanded 0.3.0 candidate validation

The expanded candidate was built from `0f77a0e0d251f516cdf801beea1889461fb0b6fe`
on `codex/v0.3.0`. The subsequent validation-record commit does not change the
runtime or package inputs. These records supersede the earlier two-language
candidate for release preparation; neither candidate is a published release.

## Coverage and correctness

- [CLI compatibility](compatibility.json): all 118 existing invocations match
  the shipped 0.2.0 binary on stdout, stderr, and exit status. Help, version, and
  new commands are accounted for separately. The tested macOS arm64 executable
  matches the rebuilt core archive byte for byte.
- [Language fixtures](../../../../structural_breadth/results/fixtures.json):
  21 fixtures cover 20 naturally detected languages, including JavaScript and
  JSX. Direct-worker comparison, structure/metrics mode separation, single-parse
  counts, worker-count determinism, combined modules, and omissions pass.
- [Additional corpus](../../../../structural_breadth/results/corpus.json): 160
  committed source files from Flask, Express, TypeScript, jq, ripgrep, Cobra,
  Laravel, and Rails match direct worker results. Two TypeScript and thirteen
  jq samples produce qualified partial results; the other samples are complete.
- [Java/C# corpus rerun](java-csharp-corpus.json): 150 samples from Spring,
  Roslyn, and ASP.NET Core match direct worker results; the same three Roslyn
  samples remain partial. This checks integration, not independent mathematical
  correctness of BCA's metrics.
- [Parser limitations](PARSER_LIMITATIONS.md): diagnostic probes isolate selected
  C macro-loop and TypeScript tuple-label recoveries. No preprocessing or source
  rewriting was added to conceal these results.
- Native-worker [race tests](native-race-tests.log), Go vet, eight Rust test
  functions exercising all native grammars, strict Clippy, 15 packaging tests,
  and the [56-check prototype harness](prototype.json) pass.

## Cross-platform and distribution checks

- [Runtime CI](runtime-ci.json) passes Linux/macOS/Windows Go tests and Linguist
  and scc conformance. Both [prototype CI jobs](prototype-ci.json) pass.
- All five [native worker jobs](worker-ci.json) pass: Linux amd64/arm64, macOS
  amd64/arm64, and Windows amd64. Each runs extracted-archive smoke tests and
  the 20-language production CLI harness. The `breadth-*.json` records identify
  the executable hashes and checks on each platform.
- The [worker package audit](worker-artifact-audit.json) verifies all five
  archives, internal checksums, binary provenance, source snapshots, and 410
  dependency crate archives across those packages. Every worker build input
  matches the local committed source. CI uses a merge checkout; its exact commit
  is retained in each package's provenance.
- The [core archive audit](archive-audit.json) verifies five archives against
  committed payloads, executable headers, build metadata, and checksums.
  [24 execution checks](archive-smoke.json) pass on native macOS arm64,
  macOS amd64 through Rosetta, and Linux amd64 through Docker emulation.
  Windows core binaries were inspected locally; Windows source execution is
  covered by CI, not a local run of the Windows release archive.
- [Offline wheel checks](wheel-smoke.json) pass on macOS arm64 and Linux arm64
  glibc/musl. All seven wheels pass strict Twine 6.2.0 [metadata checks](twine-check.log).
- A [combined offline scan](offline-combined.json) passes all 20 structural
  languages alongside projects and scc in a read-only, nonroot Linux arm64
  container with one CPU and 512 MiB memory. Its XML data file stays outside
  structural scope. This host did not expose a cgroup peak-memory counter;
  the recorded null is unavailable telemetry, not zero memory use.
- [RustSec audit](cargo-audit.json) reports no advisories or warnings for the
  expanded 83-package lockfile against its recorded 2026-09-17 database revision.
  Go dependencies did not change in this expansion; the earlier Go reachability
  audit remains separately dated in the parent directory.

The core executable remains independent of the optional native worker.
Worker runtime requirements, including glibc dependencies, are recorded per
platform in the package audit. Constrained fixture success does not establish
an arbitrary-input memory ceiling or comprehensive dialect support.

## Performance observations

The [three-run warm-cache checkpoint](performance.json) compares the actual
0.2.0 executable, the rebuilt candidate's language command, and optional project
mapping. Language stdout/stderr agree before timing.

| Repository | 0.2.0 language median | Candidate language median | Candidate projects median |
| --- | ---: | ---: | ---: |
| Spring Framework | 1.548 s | 1.529 s | 1.543 s |
| Roslyn | 6.166 s | 6.129 s | 6.137 s |
| ASP.NET Core | 2.126 s | 2.089 s | 2.100 s |

These short local samples show no material regression in the tested language
path; they do not establish a universal speedup or a reliable tail estimate.
The [worker cost diagnostic](worker-cost.json) records binary growth from
6.47 MB to 35.07 MB and warm tiny-Java subprocess medians of 2.86 ms and 3.15 ms.
Its sampled memory and startup results are not bounds for larger source files.
The earlier multi-gigabyte project-scale checks are historical measurements of
the unchanged project readers; they were not repeated in this breadth pass.

## Artifacts

The local bundle is `dist/v0.3.0-breadth-rc/`: five core archives, seven wheels,
five separate worker archives, provenance, and aggregate SHA-256 checksums.
Core artifacts use Go 1.26.6 with cgo disabled and fresh build/module caches.
Worker packages use Rust 1.94.0 and include the complete pinned crate sources.
[Core provenance](core-provenance.json), [wheel provenance](wheel-provenance.json),
and the checksum files bind these records to their actual binaries.

The macOS arm64 core hash is
`ff0d723411657a61dc4385c84fc011c1392ce7696066fc13f1c663b1c3967ade`.
The earlier `dist/v0.3.0-rc/` bundle is retained as historical evidence. Nothing
was uploaded to a GitHub Release or PyPI during this preparation.
