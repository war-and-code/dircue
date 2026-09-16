# Distribution

Dircue has two intended release channels: standalone GitHub Release archives and
platform-specific Python wheels on PyPI. Both contain the same Go executable
bytes for a given operating system, architecture, and version. Local release
preparation is available; no public release or PyPI upload has been made.

## Pipeline use after publication

For a pinned invocation without a persistent tool installation:

```sh
uvx dircue@0.2.0 --breakdown --json /path/to/checkout
uvx dircue@0.2.0 analyze all --json /path/to/checkout
```

For a persistent installation on a self-hosted runner:

```sh
uv tool install 'dircue==0.2.0'
dircue --breakdown --json /path/to/checkout
```

These commands require the matching release to have been published. Until then,
use a locally prepared wheel with `--from`, as shown below. The package name and
executable name are both `dircue`.

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
python3 scripts/release.py --version 0.2.0 --output dist/release-0.2.0
python3 scripts/wheels.py --release-dir dist/release-0.2.0 --output dist/wheels-0.2.0
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
  --from ./dist/wheels-0.2.0/dircue-0.2.0-py3-none-macosx_12_0_arm64.whl \
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
  https://github.com/war-and-code/dircue/releases/download/v0.2.0/dircue-0.2.0-py3-none-manylinux_2_17_x86_64.whl \
  dircue --breakdown --json /path/to/checkout
```

Use `uv tool install` with the same wheel URL for a persistent installation.
Choose the filename for the runner's OS and architecture; a direct URL selects
one wheel. A Python package index can select the compatible wheel automatically.
A Git clone alone is not an installable Python package here: this repository has
no Python source-package build backend. Standalone binary archives also require
ordinary extraction rather than installation through uv.

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

Hosted release automation is tracked in [#13](https://github.com/war-and-code/dircue/issues/13);
PyPI distribution and publication checks are tracked in [#14](https://github.com/war-and-code/dircue/issues/14).
Manual release preparation remains supported for v0.2.

## Historical 0.1 validation

The 0.1 validation prepared seven wheels under `dist/dircue-wheels-rc3/`, using
the exact RC3 release executables. All passed strict Twine metadata checks. Actual offline
installation/execution passed on macOS arm64 (uvx, uv tool install, pip) and
Linux arm64 glibc/musl (uvx in Docker). Windows, Intel macOS, and Linux amd64
wheel installations were not executed in that round. See the
[retained wheel validation](../tests/release/results/wheels/README.md) for
platform scope, commands, outputs, and checksums.

## 0.2 candidate validation

Five archives and seven wheels were built locally from committed source. All
wheels passed strict metadata checks and contain the same executable bytes as
their corresponding archives. Offline `uvx` execution passed on macOS arm64 and
Linux arm64 glibc/musl. The Linux amd64 binary passed Docker smoke checks under
emulation, and the macOS amd64 binary passed under Rosetta. Windows code passed
native CI, but the packaged Windows executable and wheel have not been executed
on Windows in this round.

Two independent fresh-cache Linux arm64 builds produced byte-identical archives.
See the [0.2 release validation](../tests/release/results/0.2.0/README.md) for
source commits, checksums, commands, and platform limits. These are local
candidate checks; 0.2 has not been published.
