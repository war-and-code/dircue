# Staged-analysis checkpoint for 0.3.0

This checkpoint adds a [workflow guide](../../../../../docs/STAGED_ANALYSIS.md),
a Python standard-library [report consumer](../../../../../examples/staged_analysis/route.py),
CLI regression cases, and reproducible [workflow measurements](../../../../staged_analysis/README.md).
It changes no Go runtime code, command defaults, output schema, or exit semantics.
The earlier [structural breadth validation](../breadth/README.md) remains applicable
with its recorded source, binary identities, and limitations.

## Evidence handling

The consumer suggests independent metrics, structure, and package-inventory
follow-ups. It reports uncertainty for caller review rather than executing tools
or declaring a directory safe to skip. It accepts the documented directory-source
project report and identifies its exact input JSON by SHA-256; that digest is not
an immutable directory snapshot.

Regression coverage includes XML-only content, binary and unknown inputs,
8 MiB of genuine XML beside small Maven/.NET manifests, tree and file limits,
missing roots, and omitted optional modules. Process tests assert both successful
omission reports with exit 0 and handled invocation failures with exit 1. The
consumer tests include actual CLI output, attribute-based exclusions, invalid
reports, and independent follow-up candidates. CI runs them against the built
binary on Linux, macOS, and Windows.

Local Go vet, the full Go race suite, consumer tests against a built CLI, and all
15 release-packaging tests passed. Final branch CI status is recorded on PR #19.
No runtime fix was needed for these cases.

## Measurements

The new diagnostic run measures `analyze all --projects --source directory`,
combined projects plus source-scoped metrics, and a first pass followed by a
separate metrics invocation when selected by the benchmark's stated policy.
It includes 2 GiB of fully written XML, the same XML alongside a .NET project,
a small source fixture, and pinned Spring and Roslyn checkouts. Reports and
counter equivalence are checked, not merely timings.

First-pass warm-cache medians on this macOS arm64 host were about 59 ms for the
XML fixture, 1.51 s for Spring, and 3.26 s for Roslyn. Source-scoped metrics already
exclude ordinary XML; staging did not demonstrate a benefit there. For Spring
and Roslyn, a separate metrics follow-up roughly doubled total time compared
with a combined invocation. Detailed process peak-memory observations, raw
samples, source/binary identities, fixture checksums, and measurement limits are
in the benchmark records. Syft and structural analysis were not timed in this run.

These results support choosing follow-ups when their necessity is unknown and
combining modules when all are already required. They do not establish a universal
speedup, a memory ceiling, or permission to skip package inspection.

## Pending artifacts

The refreshed local bundle is `dist/v0.3.0-staged-rc/`, with core archives and
wheels rebuilt from the committed checkpoint so the archive README is current.
Its provenance identifies the source and individual executable hashes. Native
worker inputs are unchanged; the existing verified five-platform worker archives
are retained with their original provenance. The earlier
`dist/v0.3.0-breadth-rc/` bundle remains a historical checkpoint.

This work updates the pending candidate and PR. It does not create a release,
move a tag, change repository visibility, or publish to PyPI.
