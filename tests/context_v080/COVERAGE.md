# Coverage matrix

This matrix describes required evidence. Rows remain **not run** until a source-bound receipt is produced. Passing the 13 release-smoke MUSTs means those 13 contracts passed; it is not a claim of complete SDK, build-system, or environment-model conformance.

| Claim | Evidence and oracle | Required status before receipts |
|---|---|---|
| Packaged 0.8 context gate | 13 enumerated MUST checks in `context_release_smoke.py`; independently authored manifest/global.json facts, exact hashes, malformed and cap failures | not run |
| Broad inherited behavior | 278 raw exit/stdout/stderr cases against actual pinned 0.7.0 | not run |
| Released targeted behavior | 18 raw focus, availability, and explanation cases against actual pinned 0.7.0 | not run |
| Environment declarations | .NET target framework, Python constraint, nearest global.json SDK selection | not run |
| Worker determinism | packaged environment JSON with workers 1 and 8, byte-for-byte | not run |
| Capability identity | schema/provider version and required module inventory from packaged CLI | not run |
| Offline planning | source deleted before plan; exact saved-report SHA, capability identity, inert argv and source placeholder | not run |
| Focused saved-report comparison | two independently produced 1.6 snapshots; qualified primary population | not run |
| Availability saved-report comparison | two independently produced 1.6 snapshots; path-stable LFS change | not run |
| Fail-closed boundaries | malformed saved JSON and more than 64 planning requests rejected | not run |
| Internal validation | existing Go tests for forged identities, cross-field environment validity, bounds, cancellation, partial coverage, source semantics, and comparison qualification | source tests present; results belong to CI/test receipt |
| Inherited language performance | baseline/candidate language-only on content-hashed ASP.NET Core and authored materialized 2 GiB XML corpus | not run |
| Staged workflow performance | candidate discovery + offline plan + declarations versus unconditional all + declarations + metrics on two authored scenarios | not run; outcomes intentionally differ |
| Environment incremental cost | candidate declarations versus environments on identical authored small project | not run; environments includes declaration prepass plus normalization |

## Requirement accounting

| Contract set | MUST clauses | Tested by gate | Passing before execution | Divergences |
|---|---:|---:|---:|---:|
| Packaged 0.8 context smoke | 13 | 13 | 0 | 0 |
| Inherited broad raw compatibility | 278 cases | 278 cases | 0 | 0 |
| Released targeted raw compatibility | 18 cases | 18 cases | 0 | 0 |

The case matrices are compatibility evidence, not normative completeness counts. Package tests cover additional states without being rolled into a misleading single percentage.
