# Release validation

The [pre-rename v0.1 evidence](results/final/README.md) records root and maintained-module
vet/race checks, 420 CLI cases, 3,388 exact sample labels/token sequences, 11 public
repositories, 14 stress cases, Docker smoke checks, five verified platform
archives, an identical repeated Linux arm64 archive, and an emulated Linux amd64
smoke test. It preserves the initial macOS packaging-path failure and its fix.
Source and binary hashes identify the runtime checked by each result.

Audit that bundle without building or running any workloads:

```sh
python3 tests/release/record_final.py --audit-only --output tests/release/results/final
```

This audit also passed in a minimal directory without Git, Go, Docker or workload
caches. Final repeated performance evidence is recorded separately.

The other files directly under `results/` retain historical RC1 validation.
`results/checks.json` records the host Go 1.26.6 race suite, vet, and maintained Enry module tests. `results/linux-race.txt` records the complete race suite executed in the Linux arm64 builder image. Both passed for that earlier runtime. The CI workflow defines Go tests on Linux, macOS, and Windows. Remote execution must be verified from a completed workflow run.

Reproduce the Linux test environment with:

```sh
docker build --target build -t dircue-build-check:0.1.0 .
docker run --rm dircue-build-check:0.1.0 go test -race ./...
```

The test run may download test-only Go modules through the checksum-verified module system. Historical image scans in `results/docker-smoke.json` use networking disabled, a read-only filesystem, no capabilities, no new privileges, 256 MiB memory, and two CPUs. They verify version output, Git-free `analyze all`, exact Linguist output for the Cobra checkout, and nonzero failure with stderr for a missing path. The image's Linux arm64 binary was extracted and its SHA-256 compared with that performance candidate: the bytes are identical. The final bundle retains the same checks against the final binary separately.

Reproduce these image checks after building the image and fetching the benchmark corpus:

```sh
python3 tests/release/smoke.py --corpus-volume dircue-benchmark-corpus \
  --candidate bin/dircue-linux-arm64
```

Build release archives with:

```sh
python3 scripts/release.py --version 0.1.0
```

It requires a clean committed checkout and builds five targets: Linux amd64/arm64, macOS amd64/arm64, and Windows amd64. Archives include the executable, README, project license, and third-party license notices. `dist/SHA256SUMS` covers every archive; `dist/provenance.json` records the source commit, compiler, build flags, and both archive and executable hashes. These local build outputs are ignored by Git. Cross-compilation alone does not establish native runtime testing on every platform.

The output directory must be fresh, even if an existing directory is empty. Use
`--output dist/<new-name>` when `dist` already contains previous releases. The
packager builds an isolated copy of regular committed Git blobs, preserving the
relative maintained-library replacement while excluding ignored working files
and checkout filters. It rejects source symlinks/submodules and local module
replacements outside that snapshot. It verifies that HEAD and the clean working
state still match after building, before creating the output directory.

The compiler version is selected exactly from `go.mod`; inherited Go workspace,
flags, architecture features, and private proxy/auth configuration are cleared.
Dependencies and compilation use fresh temporary caches with public Go checksum
verification. Architecture floors are amd64 v1 and arm64 v8.0. Archive entries
have fixed order, modes, owners and timestamps; gzip stores no source filename.
The provenance records these controls and the Python/zlib packaging versions.
Identical archives require identical source/version/compiler and packaging
versions; reproducibility across arbitrary compression implementations is not
claimed.

Focused checks require Python and Git, without a Go build:

```sh
python3 -m unittest discover -s tests/release -p 'test_packaging.py' -v
```

After committing a clean source state, check archive reproducibility by building
one target twice into separate fresh directories:

```sh
python3 scripts/release.py --version 0.1.0 --target linux/arm64 --output dist/repro-a
python3 scripts/release.py --version 0.1.0 --target linux/arm64 --output dist/repro-b
diff dist/repro-a/SHA256SUMS dist/repro-b/SHA256SUMS
```

`--target` can be repeated; omitting it retains the five-target release. Existing
RC1 archives remain immutable. The new packager's full-build reproduction is a
separate check from those earlier archive receipts. The final validation built
all five targets, then repeated Linux arm64 with fresh build caches and verified
an identical archive. The final bundle includes both provenance records.

Historical `results/linux-amd64-smoke.json` records version output and Git-free language/ecosystem profiling by the RC1 Linux amd64 executable under Docker Desktop's emulation on arm64. The final bundle retains a fresh equivalent smoke test for its archive executable. These verify execution under emulation, not native amd64 performance. Windows and macOS amd64 are compile-checked locally; their native CI runs remain pending a remote setup.

The release profile uses `CGO_ENABLED=0`, `-trimpath`, `-buildvcs=false`, and a fixed version through linker flags. Source revision is recorded separately, so documentation/evidence commits do not change executable bytes. The final bundle identifies both native and Linux executables and their original source commits; historical reports retain their own binary identities. The packager itself does not publish a release or upload artifacts.
