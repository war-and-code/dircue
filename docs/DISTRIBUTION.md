# Distribution

GitHub Release archives and platform-specific Python wheels contain the same Go
executable bytes for a given operating system, architecture, and version.
Existing releases include both formats. The 0.9.0 release does not include a
PyPI publication step; use the wheel from the GitHub Release URL directly, or
install one of the standalone archives.

The examples below target 0.9.0. Tagged download URLs become available when
the release is published; local packaging works from the prepared source. For
an earlier release, substitute its version consistently in commands and filenames.

## Pipeline use if a future release adds PyPI publication

The 0.9.0 release does not publish to PyPI. If a later release adds it, the
following commands become available:

```sh
uvx dircue@<version> --breakdown --json /path/to/checkout
uvx dircue@<version> analyze all --json /path/to/checkout
```

For a persistent installation on a self-hosted runner:

```sh
uv tool install 'dircue==<version>'
dircue --breakdown --json /path/to/checkout
```

With uv's default index settings, these commands require the matching version
on PyPI. A configured private index or a local wheel collection supplied with
`--no-index --find-links` can also resolve the package by name. Until PyPI
publication is announced, use GitHub wheels as shown below. The package name and executable
name are both `dircue`.

## Two names, one binary

Starting with 1.0.0, the binary answers to both `dircue` and `dirq`. The two
names behave identically; help and usage text shows the name that was invoked.

- **Archive extract** — the tar archives ship `dircue` plus a relative symlink
  `dirq -> dircue`. After extracting on Linux or macOS you can call either name
  from the same directory:
  ```sh
  tar -xzf dircue_1.0.0_linux_amd64.tar.gz
  ./dircue --version
  ./dirq --version
  ```
  The Windows zip ships `dircue.exe` and a byte-identical `dirq.exe` copy.

- **Go install** — two entry points are published:
  ```sh
  go install github.com/war-and-code/dircue@v1.0.0        # installs dircue
  go install github.com/war-and-code/dircue/cmd/dirq@v1.0.0  # installs dirq
  ```

- **Wheel** — both console scripts are included in every wheel:
  ```sh
  uvx --offline --no-index \
    --from ./dist/wheels-1.0.0/dircue-1.0.0-py3-none-macosx_12_0_arm64.whl \
    dirq map /path/to/checkout
  ```
  `dircue` and `dirq` are both installed by pip and call the same bundled binary.

## Go module installation

The root module embeds its maintained Enry and go-git packages beneath
`github.com/war-and-code/dircue/third_party/...`; it uses no local module
`replace` directives. After the repository is public and a version tag exists,
the expected installation forms are:

```sh
go install github.com/war-and-code/dircue@v1.0.0        # installs dircue
go install github.com/war-and-code/dircue/cmd/dirq@v1.0.0  # installs dirq
```

Verify these commands against a clean module cache and the actual public tag as
a release gate. While the repository is private, a maintainer can clone with
authenticated GitHub access and build from that checkout. A versioned `go
install module@version` cannot be confirmed in a clean anonymous environment
until the tag is publicly fetchable; local `go install .` does not prove the
remote installation contract.

`uvx` installs into an isolated cached environment. It avoids changing the
checkout's Python dependencies, but its first invocation still needs the wheel
and a compatible Python interpreter. uv can obtain Python when permitted. For
restricted-network runners, pre-stage Python and the wheel, then use `--offline`.
No Go compiler or Ruby installation is needed. The launcher does not download
anything or run project code; it invokes the bundled executable with the original
arguments and working directory. On Unix it replaces itself with the Go process.

For repeat jobs, keep a runner-owned uv cache or install the pinned tool once in
the runner image. Use explicit versions in CI. `uvx` includes environment
resolution and launch overhead beyond the standalone binary timings in the
benchmark reports. See [uv's tool documentation](https://docs.astral.sh/uv/guides/tools/)
and [cache behavior](https://docs.astral.sh/uv/concepts/tools/#tool-environments).

## Prepare archives and wheels locally

From a clean committed checkout, choose fresh output directories:

```sh
python3 scripts/release.py --version 0.9.0 --output dist/release-0.9.0
python3 scripts/wheels.py --release-dir dist/release-0.9.0 --output dist/wheels-0.9.0
```

The wheel builder consumes existing release archives; it does not compile Go,
run their executables, contact a package index, or publish files. It verifies
archive checksums and records the binary checksums in wheel provenance.
Keep archive and wheel provenance together with their SHA-256 checksum files.

The approach follows [Simon Willison's Go/PyPI distribution example](https://simonwillison.net/2026/Feb/4/distributing-go-binaries/).
Our packager uses the Python standard library and retains the archives' license
notices and provenance. The launcher requires only Python and the bundled binary.

The wheel builder produces these platform variants:

| Binary | Wheel platform tags |
| --- | --- |
| Linux amd64 | `manylinux_2_17_x86_64`, `musllinux_1_2_x86_64` |
| Linux arm64 | `manylinux_2_17_aarch64`, `musllinux_1_2_aarch64` |
| macOS Intel | `macosx_12_0_x86_64` |
| macOS Apple Silicon | `macosx_12_0_arm64` |
| Windows amd64 | `win_amd64` |

The Linux pairs contain identical static Go binaries. The wheel's libc tag
controls installer selection; the packaged executable does not dynamically link
that libc. Go 1.26 requires Linux kernel 3.2+, macOS 12+, and Windows 10 or Server
2016+. Wheel tags cannot express every operating-system constraint. The launcher
requires Python 3.10+. Supported versions must be reviewed when the Go toolchain
changes. See [Go's platform requirements](https://go.dev/wiki/MinimumRequirements).

Test a local wheel on an Apple Silicon Mac without accessing a package index:

```sh
uvx --offline --no-index \
  --from ./dist/wheels-0.9.0/dircue-0.9.0-py3-none-macosx_12_0_arm64.whl \
  dircue --version
```

For a Linux amd64 runner, replace the filename with the `manylinux_2_17_x86_64`
wheel, or the `musllinux_1_2_x86_64` wheel on Alpine. A pre-staged wheel directory
can also be used with `--no-index --find-links /path/to/wheels` and a pinned
package version. Ordinary `pip install /path/to/compatible.whl` is supported.
There is no source-distribution fallback that compiles Go on the runner.

## Use a wheel from GitHub

Public PyPI is optional. After a compatible wheel is attached to a GitHub Release,
uv can install it directly. For a Linux amd64 runner, the release URL would be:

```sh
uvx --from \
  https://github.com/war-and-code/dircue/releases/download/v0.9.0/dircue-0.9.0-py3-none-manylinux_2_17_x86_64.whl \
  dircue --breakdown --json /path/to/checkout
```

Use `uv tool install` with the same wheel URL for a persistent installation.
Choose the filename for the runner's OS and architecture; a direct URL selects
one wheel. A Python package index can select the compatible wheel automatically.
A Git clone alone is not an installable Python package here: this repository has
no Python source-package build backend. Standalone binary archives also require
ordinary extraction rather than installation through uv.

## Private or draft GitHub downloads

A private asset URL (for example, a draft release that has not yet flipped to
public) cannot be used as an anonymous public download. With an authenticated
GitHub CLI, download the compatible wheel first, then give uv the local file:

```sh
gh release download v0.9.0 --repo war-and-code/dircue \
  --pattern 'dircue-0.9.0-py3-none-manylinux_2_17_x86_64.whl' \
  --dir ./dircue-download
uvx --offline --no-index \
  --from ./dircue-download/dircue-0.9.0-py3-none-manylinux_2_17_x86_64.whl \
  dircue analyze projects --json /path/to/checkout
```

This requires the release asset to exist and a compatible Python interpreter to
be available locally. Replace the wheel filename for another platform.

## Optional structural worker

The archives and wheels above contain the core Go executable. Language profiling,
discovery, project/graph mapping, rules, package-source declarations, imported
package evidence, and scc counting do not require a separate parser. Structural
analysis adds a native `dircue-structural-worker` selected by explicit path:

```sh
dircue analyze structure --json --files \
  --structural-worker /opt/dircue/dircue-structural-worker /path/to/checkout
```

Build its platform archive separately:

```sh
python3 scripts/structural_worker_release.py --version 0.9.0 \
  --platform darwin-arm64 --output dist/structural-worker-0.9.0 --smoke-test
```

See the [worker guide](STRUCTURE.md#building-the-add-on) for the pinned Rust
build toolchain and supported targets. The worker uses
[big-code-analysis](https://github.com/dekobon/big-code-analysis) and Tree-sitter;
it is not included in Python wheels or the core Docker image. A built worker
runs offline and requires neither Cargo nor a grammar download at runtime.

Native-worker platform requirements differ from the static Go executable.
In particular, Linux worker packages use glibc and are not supported by the
core wheel's musllinux compatibility claim. Check the package's runtime
provenance and validate it on the intended runner. Keep its license notices and
complete dependency-source archives when redistributing it; BCA's MPL-2.0
license is separate from dircue's MIT license.

## Publication

Publish after source and documentation review. Before a PyPI upload, confirm
ownership of `dircue`, review package metadata, validate all intended platforms,
and verify that archive and wheel
binary hashes agree. An absent PyPI project page does not reserve the name.

For each release, upload the verified standalone archives, checksums,
provenance, and release notes to a GitHub Release. Attach wheels there too for
direct uv installation without PyPI. For PyPI publication, upload only the
intended `.whl` files. Both channels must identify the same version and source. Verify
real downloads and pinned invocations after publication; local installation
checks do not establish that publication succeeded. Never replace an already
published version with different executable bytes.

The [release automation guide](RELEASE_AUTOMATION.md) covers local packaging and
the Actions workflow that prepares and verifies GitHub draft releases, including
all seven wheels. PyPI publication remains separate and is tracked in
[#14](https://github.com/war-and-code/dircue/issues/14).

## Historical 0.1 validation

The 0.1 validation prepared seven wheels under `dist/dircue-wheels-rc3/`, using
the exact RC3 release executables. All passed strict Twine metadata checks. Actual offline
installation/execution passed on macOS arm64 (uvx, uv tool install, pip) and
Linux arm64 glibc/musl (uvx in Docker). Windows, Intel macOS, and Linux amd64
wheel installations were not executed in that round. See the retained wheel validation (archived in
[evidence-archive-1](https://github.com/war-and-code/dircue/releases/tag/evidence-archive-1);
restore with `make fetch-receipts`) for platform scope, commands, outputs, and checksums.

## 0.2 candidate validation

Five archives and seven wheels were built locally from committed source. All
wheels passed strict metadata checks and contain the same executable bytes as
their corresponding archives. Offline `uvx` execution passed on macOS arm64 and
Linux arm64 glibc/musl. The Linux amd64 binary passed Docker smoke checks under
emulation, and the macOS amd64 binary passed under Rosetta. Windows code passed
native CI, but the packaged Windows executable and wheel have not been executed
on Windows in this round.

Two independent fresh-cache Linux arm64 builds produced byte-identical archives.
See the 0.2 release validation (archived in
[evidence-archive-1](https://github.com/war-and-code/dircue/releases/tag/evidence-archive-1);
restore with `make fetch-receipts`) for source commits, checksums, commands, and platform limits. These are local
candidate checks. Final release assets carry their own source and checksum
provenance on the [release page](https://github.com/war-and-code/dircue/releases/tag/v0.2.0).
