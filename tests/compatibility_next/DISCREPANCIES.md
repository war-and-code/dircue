# Differences

The initial run found no differences in the 209 equality cases.

Ten interface probes are recorded separately. The following probes differed because the candidate exposes additional opt-in commands or flags:

- `analyze` without a subcommand: the command list in its error changes; both executions exit 1.
- `analyze --help` and `analyze all --help` include additional analysis choices.
- `analyze discovery --help`, `analyze graph --help` and `analyze packages --help` now describe those commands. The released executable already exits 0 for these help invocations, so exit status alone would miss the change.

Root help, version, project help and structural help were identical in this run. Both binaries reported `dircue 0.3.0`; the candidate used a compatibility build version, and its SHA-256 identifies the tested executable. The version string does not imply that these new features were shipped in 0.3.0.

These probes are visible exceptions, not output normalization or a blanket exemption for other differences. Any new discrepancy in an equality case fails the harness. Retain a failed receipt and investigate it before changing fixture expectations or coverage.
