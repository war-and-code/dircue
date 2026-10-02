# Differences

The initial run found no differences in the 209 equality cases.

Ten interface probes are recorded separately. The following probes differed because the candidate exposes additional opt-in commands or flags:

- `analyze` without a subcommand: the command list in its error changes; both executions exit 1.
- `analyze --help` and `analyze all --help` include additional analysis choices.
- `analyze discovery --help`, `analyze graph --help` and `analyze packages --help` now describe those commands. The released executable already exits 0 for these help invocations, so exit status alone would miss the change.

Root help, version, project help and structural help were identical in this run. Both binaries reported `dircue 0.3.0`; the candidate used a compatibility build version, and its SHA-256 identifies the tested executable. The version string does not imply that these new features were shipped in 0.3.0.

These probes are visible exceptions, not output normalization or a blanket exemption for other differences. Any new discrepancy in an equality case fails the harness. Retain a failed receipt and investigate it before changing fixture expectations or coverage.

## 1.1.0 candidate versus published 1.0.1

The later-minor harness requires an explicitly pinned release executable and
reported version. Its equality cases retain exact stdout, stderr and exit codes.
The matrix does not cover every map, focus or context interface; see the
[harness scope](README.md#comparing-later-minor-releases).

Intentional changes outside that equality matrix are listed in the
[1.1.0 changelog](../../CHANGELOG.md#110-unreleased): new evidence and references,
capability and deployable observer versions, fixed-bound settings entries, and
opt-in comparison exit codes. Project/declaration reference arrays may gain
`local-artifact` entries. Maven test-scope requirements remain in those reports,
but no longer imply runtime capabilities in the map. Help and version text
also change. The comparison's default successful exit behavior stays unchanged.

These additions need semantic and schema tests, not replacement legacy
goldens. The seven-repository map check uses its existing adjudicated labels;
an improved result on those labels is regression evidence, not an independent
accuracy estimate.
