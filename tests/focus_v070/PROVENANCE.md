# Provenance

The default compatibility reference is the macOS ARM64 `dircue 0.6.1` executable staged at `.cache/v070-sprint/baseline/dircue`. Its pinned SHA-256 is `2cfe46f0467a422124a94ec1931cc91ccd92be95fa1d61f6ff043ba260f29145`. A different reference is accepted only with an explicitly supplied, independently verified digest.

All conformance fixtures are authored byte strings in `fixture.py`. They are synthetic and have no upstream repository claim. Their receipt manifest records every root-relative path, byte count and SHA-256. Expected primary and related paths are literal lists beside the fixture content; the runner does not derive them from dircue output.

Candidate provenance comes from `build.py`. Its receipt binds the executable digest to `go list -deps` compilation inputs, `go.mod`, `go.sum`, Git commit and tree, dirty status, binary diff digest, Go version, build information, flags and environment. The conformance and performance receipts embed that record and its own digest.

The performance corpus is caller supplied. The runner records a deterministic manifest over every regular file's full content plus every symlink target, excluding `.git`. It checks the same manifest after measurement. A corpus name or checkout path alone is never treated as content identity.

The optional structural worker is not trusted by path. The caller must supply its expected digest. When omitted, the receipt explicitly records native hotspot compatibility as untested.

