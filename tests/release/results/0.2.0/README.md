# 0.2.0 candidate validation

Five standalone archives and seven Python wheels were prepared locally from
clean commit `4e6c46d3558f57711cde7034ce0b73df8a2f25ff`. No 0.2 tag, GitHub
Release, PyPI upload, or repository visibility change was made by these checks.
The artifacts remain in ignored `dist/release-0.2.0/` and
`dist/wheels-0.2.0/` directories for maintainer review.

The measured runtime is commit `bf315bb5fb58e7a45ba7cd66451c08590770de7b`.
The packaging commit adds documentation, test receipts, a wheel smoke helper,
and a general-purpose package summary; its Go source, modules, and embedded data
retain the measured runtime. The [metrics inventory](../../../metrics/results/build.json)
records runtime source hashes. Subsequent commits containing these release
receipts do not identify a different tested executable.

## Results

- All five archive checksums, executable hashes, and bundled README/license
  files match their recorded source. All seven wheels contain the corresponding
  archive's exact executable and release provenance.
- The packaged native executable reproduced the complete measured Spring,
  Roslyn, and ASP.NET language and metrics reports byte-for-byte.
  `corpus-output-equivalence.json` records those output hashes.
- All seven wheels passed Twine 6.2.0 strict metadata validation.
- Offline `uvx` installation and execution passed on native macOS arm64 and
  Linux arm64 glibc/musl. The checks compare legacy JSON, metrics, aggregate
  metrics, version, exit status, and installed binary identity with archive
  executables. Fixtures contain Java, C#, XML data, and paths with spaces.
- Linux arm64 and amd64 archive binaries passed Docker smoke tests with no
  network, a read-only root filesystem, UID 65532, dropped capabilities, a
  256 MiB container memory limit, and two CPUs. Checks include exact Linguist
  output on the pinned Cobra corpus, metrics, and a missing-path failure.
  The image executable hash matches the archive. These images were built
  directly from archive binaries, without another Go compilation.
- The macOS amd64 archive executable passed version, language/metrics JSON,
  and error comparisons against native arm64 execution under Rosetta.
- A second Linux arm64 build with fresh module and compiler caches produced
  a byte-identical archive. This proves the recorded reproduction on this
  toolchain and host, not every possible build environment.

Linux arm64 ran in Docker Desktop on an Apple Silicon Mac. Linux amd64 used
emulation; macOS amd64 used Rosetta. These are functionality checks, not native
Intel performance measurements. Windows code passed native hosted CI, but the
packaged Windows executable and wheel were not executed on Windows here.
Windows and Intel/Linux amd64 wheel metadata and binary payloads were checked;
installer execution was limited to the three environments stated above.

An independent artifact audit passed 246 checks with no failures; its detailed
receipt is `independent-artifact-audit.json`.

The five [runtime CI jobs](runtime-ci.json) and all five
[packaging-commit CI jobs](packaging-ci.json) passed: Linux, macOS, Windows,
Linguist conformance, and scc conformance. Full local vet/race and maintained
fork suites also passed. The [metrics results](../../../metrics/results/README.md)
and [vulnerability scan](../../../security/results/README.md) retain their
separate scope and limitations.

## Reproduction

From the packaging commit in a clean checkout, choose fresh output directories:

```sh
python3 scripts/release.py --version 0.2.0 --output dist/release-0.2.0
python3 scripts/wheels.py --release-dir dist/release-0.2.0 --output dist/wheels-0.2.0
python3 tests/release/wheel_smoke.py \
  --release-dir dist/release-0.2.0 --wheel-dir dist/wheels-0.2.0 \
  --version 0.2.0 --output .cache/wheel-smoke.json
uvx --offline --from twine==6.2.0 twine check --strict dist/wheels-0.2.0/*.whl
python3 scripts/release.py --version 0.2.0 --target linux/arm64 \
  --output dist/release-0.2.0-reproduced
```

The wheel smoke helper requires macOS arm64, uv, Python, Docker, and the two
cached arm64 uv images named in its source. It refuses to pull images and runs
installers offline. Twine must already be cached for its offline command.
The Go archive builder downloads checksum-verified build dependencies into fresh
caches; the offline claim applies to runtime and wheel installation checks.

`archive.Dockerfile` is the runtime-image definition. Place an extracted Linux
archive executable beside it and build with the matching `--platform` value;
`tests/release/smoke.py` accepts the resulting image and corpus volume.

`release-provenance.json` and `wheel-provenance.json` record build controls,
source identity, archive hashes, executable hashes, and package metadata.
`archive-SHA256SUMS` and `wheel-SHA256SUMS` identify the prepared files.
The smoke and audit receipts retain commands and results. Temporary paths are
replaced by placeholders; input contents, output values, and hashes are unchanged.
Original local receipts remain in `.cache/release-v020/`.

## Compatibility with the published 0.1.0 CLI

`compatibility-v010.json` compares the actual published macOS arm64 executable
with the 0.2 candidate. All 86 existing-command scenarios matched stdout, stderr,
and exit status. Coverage includes legacy and structured modes, attributes,
Git revisions, dirty working trees, empty directories, single files, size/tree
limits, invalid options, and missing paths. Version/help additions and the
expanded analysis-command listing are recorded separately. Corrected Git-reader
results are intentional bug fixes, not a guarantee of reproducing corrupted
0.1 output.

Run `python3 tests/release/results/0.2.0/compatibility-v010.py` from a checkout
with the named local archives available. The published 0.1 archive digest is
checked before execution. The retained report replaces the workspace prefix
with `$WORKSPACE`; its original remains in `.cache/compat-v010-published/`.
