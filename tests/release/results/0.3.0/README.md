# 0.3.0 candidate validation

These receipts describe the **earlier Java/C#-only structural candidate**. The
subsequent language expansion changes the worker, dependency set, and core
adapter. These archived artifacts and CI results do not verify the expanded
candidate; new builds and validation are required before publication.

Core artifacts were built from clean committed source `2501e826a9201e1fb2d5285d58b5177282b1469c` with Go 1.26.6, cgo disabled, and fresh build/module caches. The native macOS arm64 archive contains the same executable as the final compatibility candidate: `61a4cd8da957f0ac4c43c1485eefa016175bbd383cd11459b39c69a08202e7af`.

The candidate remains on `codex/v0.3.0` in [PR #19](https://github.com/war-and-code/dircue/pull/19). These records do not represent a published release.

## Checks

- [Final CLI compatibility](../../../compatibility_v030/final-results.json): 118 existing invocations match the actual 0.2.0 release binary on stdout, stderr, and process status. Help, version, and the expanded command list are recorded separately.
- [Project corpus](../../../projects/results/corpus.json): deterministic inventory and attribution checks on Roslyn, ASP.NET Core, Spring Framework, and Apache Maven. Malformed fixtures and unsupported declarations remain diagnostic results.
- [Production structural comparisons](../../../structure/results.json): 150 sampled Java/C# files match direct worker observations, metrics, status, and provenance. One-worker/eight-worker output is identical. Three Roslyn files retain qualified partial results.
- [Performance and stress records](../../../performance_v030/README.md): the language path remains close to 0.2.0 on the measured corpora; optional project mapping passes the 2 GiB Talend-shaped, 1,100 MiB XML, and 2,048-project .NET fixtures under a 512 MiB container limit. These timing records identify an earlier implementation checkpoint before the Maven 4.1 extension.
- [Core archive audit](archive-audit.json): five platform archives match checksums, build provenance, executable headers, and committed README/license/notice payloads.
- [Core execution checks](archive-smoke.json): 24 checks pass across native macOS arm64, macOS amd64 through Rosetta, and Linux amd64 through Docker emulation. Windows archives were inspected locally; Windows source and CLI tests run in CI.
- [Offline combined scan](offline-production-combined.json): the final Linux arm64 core archive and audited worker run projects, scc, and structure together as a nonroot user with no network, read-only inputs, one CPU, and 512 MiB memory. XML is inventoried as data without becoming structural input.
- [Offline wheel checks](wheel-smoke.json): macOS arm64, Linux glibc arm64, and Linux musl arm64 install through offline `uvx` and match their archive binaries. All seven wheels pass [metadata checks](twine-check.log).
- [Worker artifact audit](worker-artifact-audit.json): all five native platform packages pass checksums and source verification, including 310 dependency crate archives across the packages. Their worker source inputs match the final candidate. Packages came from an earlier successful CI merge checkout, identified in each receipt; the subsequent Maven-only change did not alter those inputs.
- [Native worker CI](worker-ci.json): all five platform builds, extracted-package smoke tests, and native Go adapter/scanner/schema/CLI tests pass for the final core source commit.
- [Runtime CI](runtime-ci.json): Linux/macOS/Windows Go tests, Linguist and scc conformance, and both structural prototype jobs pass for the core source commit.

The optional worker is not part of the core archives or Python wheels. Both inspected Linux worker binaries require glibc 2.34 symbols; core Linux executables are independent of libc. A successful constrained smoke test does not establish a maximum process RSS for arbitrary inputs.

## Dependency checks

`govulncheck` 1.7.0 found no reachable vulnerable Go symbols. Its [full output](govulncheck.txt) retains three SSH package advisories and one OpenPGP module advisory in dependencies whose affected symbols are not called by this program. This is a reachability result, not a claim that every transitive dependency is free of advisories.

`cargo-audit` 0.22.2 reported [no RustSec advisories or informational warnings](cargo-audit.json) for the worker lockfile against the recorded advisory database revision. These are point-in-time checks.

## Reproduce

From a clean checkout, build the core archives with `scripts/release.py`, build wheels with `scripts/wheels.py`, and run `tests/release/wheel_smoke.py`. The scripts accept fresh output directories. Native worker packaging and five-platform checks are described in [the structural guide](../../../../docs/STRUCTURE.md).

The corresponding candidate artifacts are retained locally in `dist/v0.3.0-rc/`, including five core archives, seven wheels, five separate worker archives, provenance, and aggregate SHA-256 checksums. No artifacts were uploaded to a GitHub Release or PyPI during this validation.
