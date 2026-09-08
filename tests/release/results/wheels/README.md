# Local PyPI wheel validation

Seven wheels were packaged from the five existing Dircue RC3 release archives.
Every bundled executable remains byte-identical to its standalone archive.
No Go build or publication occurred. The [wheel provenance](wheel-provenance.json)
binds wheel hashes, executable hashes, original archive hashes, and the wheel
builder source; [release provenance](release-provenance.json) identifies the Go build.

Eight packaging/launcher tests passed, including deterministic output, RECORD
integrity, license inclusion, malformed or tampered input refusal, platform
header checks, and launcher argument/exit behavior. All seven wheels passed
[Twine 6.2.0 strict metadata validation](twine-check.log).

Real installation checks used the local wheels with network access disabled at
the installer, and Docker network disabled for Linux. [Native checks](native-smoke.json)
cover macOS arm64 uvx, uv tool install, pip, python -m dircue, exact Java/C# profile
JSON, working directories with spaces, error output/status, and installed binary
identity. [Linux checks](linux-smoke.json) cover glibc and musl arm64 uvx in
unprivileged read-only containers, including version, language/extended JSON,
and errors. The executed smoke scripts are retained alongside these reports;
they expect local `dist/dircue-wheels-rc3/` wheels and the RC3 standalone binaries.

Windows amd64, macOS amd64, and Linux amd64 wheel installation were not executed
in this round. Their archive binaries retain earlier cross-compilation or smoke
evidence, and their wheel payloads/metadata were verified. Windows console
interrupt handling has a unit test, but no native Windows execution test.

No claim is made about a successful PyPI upload, package-name ownership, public
download, or GitHub Release. Those remain separate publication checks after
maintainer review. This packaging layer does not repeat the classifier benchmarks.

To repeat the native smoke with the local artifacts present, from the repository
root:

```sh
mkdir -p .cache/pypi-preparation
python3 tests/release/results/wheels/native-smoke.py
```

The Linux runner also requires the two uv/Python Docker images identified in its
report to be locally available before network-disabled execution.
