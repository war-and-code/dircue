# Provenance

Reference: the published dircue v0.3.0 macOS ARM64 core executable and structural worker. The receipt records local artifact paths; those paths are not required on another machine.

| Executable | SHA-256 |
| --- | --- |
| Released core | `ff0d723411657a61dc4385c84fc011c1392ce7696066fc13f1c663b1c3967ade` |
| Released worker | `dac0c16d4cd5d030d4fbb7b710e61f4ef3dc84e5cf9d9f14753cdc50a44323ba` |
| Initial candidate | `98acb2d5c54ecc55a302fe110f5c813cf7aacbcec3a9582435d779324e86f818` |

[The initial receipt](results/initial-macos-arm64.json.gz) was captured on 2026-09-19, from 20:58:08 to 20:58:17 UTC. It contains all reference captures, fixture file hashes, source file hashes, harness and reused fixture-generator hashes, platform identification and observed version output. The source snapshot had HEAD `b566daaae8d1c8ed07db3583a479bf35e37cdd4c` with uncommitted changes; its build-input digest was `5f9c3006e52aeb57cdb07b959aee44927f5177d3ba1d6e3f6b1e888af97d1eb4`.

The executable was supplied prebuilt. The source snapshot records the workspace observed during the run; it does not independently establish that those bytes produced the candidate. Release verification must bind its final executable to its own build provenance and rerun comparisons if runtime changes follow this check.

The 118 retained cases come from the existing v0.3 compatibility fixture generator, now executed against the actual v0.3 release rather than the earlier v0.2 reference. The 21 structural input files come from `tests/structural_breadth/testdata`, with expected language identities in `fixtures.json`. Additional small project and malformed-input fixtures are generated explicitly in this harness. No outputs from the candidate seeded the reference expectations.

Reference health assertions check several intended fixture properties independently of equality. The same released worker processes both sides, isolating compatibility of the core's integration. This does not compare worker versions or independently validate all upstream metrics.

Fixtures and executables are hashed before and after execution. Working paths remain present in captured output and differ between separate runs; exact comparison is within each paired run. No cross-run path rewriting is applied.
