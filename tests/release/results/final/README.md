# Final v0.1 correctness and packaging evidence

The final maintained classifier passed the pinned Linguist 9.7.0 correctness
gates: 404 exact CLI matches plus 16 independently validated differences, all
3,388 upstream sample labels and ordered token sequences, all 11 pinned public
repositories, and all 14 synthetic scale cases. Root and maintained-module vet
and race suites passed. Statement coverage was 81.1% for the root module and
54.0% for the maintained module; these percentages do not establish universal
behavioral coverage.

The validated Linux arm64 executable has SHA-256
`d4c1011d3d615f303cd8d60760c0eeb8d3f234fda74ac2dfd77aa75e4e8ae637`.
Correctness checks used commit `f13f06841a49f4c862319feb88bc2a7549cee06b`.
Packaging used `e2f9ae7035e087062afe55c8335e3f43569a1d8c`, which changed only the
packager and its regression test. The retained source inventories establish
that every runtime file remained identical, and the archive executable matches
the tested binary exactly.

The initial packaging attempt rejected a valid local dependency because macOS
exposes `/var` through `/private/var`. Its failure log remains in the evidence.
The fix canonicalizes the source boundary; five focused packaging tests passed,
including an aliased-root regression and rejection of real directory escapes.
Successful correctness checks were retained, and packaging resumed separately.

All five Linux/macOS/Windows archives passed checks for names, checksums,
executable and license bytes, permissions and metadata. A separate Linux arm64
build produced an identical archive. The Docker image passed constrained,
network-disabled version, Git-free profiling, Linguist comparison and error
checks. Linux amd64 also passed version and Git-free profiling smoke checks
under Docker Desktop emulation. Windows and macOS amd64 were compile checked;
this record does not claim native execution on those platforms.

The final govulncheck v1.7.0 scan, using Go 1.26.6 and the database timestamp
recorded in its raw output, found zero symbol-reachable vulnerabilities. It
also reported four unreachable dependency advisories: GO-2026-5932,
GO-2026-6303, GO-2026-6354 and GO-2026-6355. The module inventory, tool checksums,
source identity and complete output are retained. This is a scoped static
analysis result, not a general security certification.

## Audit without running workloads

Replay the retained evidence with:

```sh
python3 tests/release/record_final.py --audit-only \
  --output tests/release/results/final
```

The audit reads the published archive and checksums only. It replays raw CLI
comparisons, verifies the specific alternate Ruby oracles behind the 16
differences, checks sample labels/tokens and public/stress outputs, binds the
independent reviews to their report hashes, and checks the retained source,
packaging and security receipts. It does not build software, launch Docker,
download dependencies, scan repositories or repeat benchmarks. Python's
standard library is sufficient; local benchmark corpora and `.cache` are not
required for audit-only use.

[The portability check](portability.json) passed in a fresh minimal tree containing
only the recorder and the three evidence files, with an empty command-search
path and no Git repository or workload cache. The user's HOME was preserved.
Duplicate, nonregular, traversal and noncanonical archive entries were rejected;
Python's assertion-disabling `-O` mode was also rejected.

The deterministic `evidence.tar.gz` retains raw reports, logs, independent
reviews, source inventories, the packaging failure and fix, and the original
executed validation, packaging and security runners. The prepared cache recorder
and runbook are labeled separately from the public recorder that actually
captured this bundle. `SHA256SUMS.json` identifies every archived entry;
`audit.json` records the replayed assertions. The distributable archives remain
separate and are identified by their actual payload provenance and checksums.

RC1 evidence remains historical and unchanged. The public projects and synthetic
ETL-pipeline/XML/.NET inputs establish compatibility only for their recorded content.
This bundle contains correctness and packaging checks, not the final repeated
performance matrix. No final-release performance claim should be inferred from
its diagnostic resource readings.
