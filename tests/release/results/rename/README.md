# Dircue rename validation

These checks apply to renamed source `b578c1e6ef18c234acb03089ab15ba42d8db9720`.
The native packaged binary passed all 420 CLI conformance cases: 404 exact
comparisons, 16 independently checked expected differences, and zero failures.
The Docker runtime passed restricted version, Git-free profile, pinned Linguist
Cobra comparison, and missing-path checks; its binary equals the packaged Linux
arm64 executable. Linux amd64 passed version and Git-free profile checks under
Docker emulation. Windows and macOS amd64 are cross-compiled only.

All five archive and executable checksums were verified. Archive text payloads
match the committed source, executable modes are 0755, and executable/archive
names use `dircue`. See [provenance](provenance.json), [archive checksums](archive-SHA256SUMS),
[validation summary](validation.json), [raw conformance](conformance.json),
[independent conformance review](conformance-review.json), [Docker checks](docker-smoke.json),
and [amd64 smoke](amd64-smoke.json).

The old benchmark evidence remains unchanged and uses the former project name.
This rename check does not repeat performance measurements. Existing RC1/RC2
artifacts remain available; renamed archives are local under `dist/dircue-rc3/`.
Nothing was pushed or published to GitHub.
