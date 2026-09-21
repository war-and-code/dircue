# Discrepancies and claim limits

There are no accepted conformance divergences encoded by this harness. A raw mismatch in `broad.py` or `targeted.py` fails its run and must be investigated rather than skipped.

Retained [candidate receipts](results/) record the native macOS ARM64 run against the pinned 0.7.0 release. See [validation](../../docs/releases/0.8.0-validation.md) for measurements and limitations; they are not an exhaustive compatibility or cross-platform proof.

Environment observations are declared constraints and modeled SDK-selection context. They do not probe installed tools, execute package managers or builds, resolve every build-system condition, or prove that compilation succeeds.

Planning output is inert advice over retained report evidence. Its argv uses a source placeholder and requires caller revalidation. The harness does not execute planned commands or claim that a deleted source is recoverable from a report.

The 278-case broad matrix and 18-case targeted matrix cover named inherited surfaces. Neither matrix proves exhaustive CLI compatibility. The 13 packaged smoke requirements cover the release-critical 0.8 contracts named in `COVERAGE.md`; they do not establish complete SDK or build-environment conformance.

Performance lanes answer different questions. The staged workflow produces discovery evidence, an offline follow-up plan, and declarations. The unconditional workflow also computes full metrics. Their outputs are not equivalent, so the report records ratios without calling them savings. Peak RSS for the staged workflow is the maximum observed stage peak; summing independent process peaks would be meaningless.

Measurements use warm caches after explicit lane warmups, five or more samples, and alternating lane order. Host scheduling, filesystem cache state, corpus revision, JSON size, and background services remain sources of variance. Every claim is limited to the executable hashes, content manifests, commands, and environment note in its receipt.
