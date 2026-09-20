# Private JSON Schema runtime

This directory contains the root production files and Apache-2.0 license from
`github.com/santhosh-tekuri/jsonschema/v5` v5.3.1. It is private to dircue and is
not a replacement module or a published JSON Schema library. The upstream
dependency remains unmodified for independent validation tests.

The patch defers compilation of the five bundled draft metaschemas until the
first schema compilation. This keeps report comparison's validation work off
the language-only and other profiling paths. The inexpensive draft traversal
metadata remains initialized normally. A private bootstrap flag prevents
recursive initialization, and `sync.OnceFunc` publishes the initialized schemas
to concurrent callers and repeats any initialization panic. Bootstrap uses the
original builtin format callbacks, independently of later registry changes.

Validation algorithms, draft order, regexes, numerical operations and bundled
schemas are unchanged. One Go vet adaptation renders cached enum-error strings
with a literal `%s` format, so percent signs in enum values cannot become format
directives. This changes only that error's presentation; acceptance is unchanged
and report imports return their existing generic validation error. The first
comparison still pays the initialization cost.
Dircue uses supported draft pointers directly, sets a compiler-local denying
resource loader, and does not copy Draft values or replace global registries.
This private snapshot does not promise compatibility for callers that copy
Draft values or replace exported Draft globals before first compilation.

Verify the retained runtime bytes or regenerate into a fresh directory:

```sh
python3 third_party/update_jsonschema.py --check
python3 third_party/update_jsonschema.py --output .cache/jsonschema-regenerated
go test -race ./internal/jsonschema ./schema ./pkg/reportdiff
```

`PROVENANCE.json` records the upstream commit, Go module checksums, archive hash,
retained upstream and patched file hashes, patch hash and generator hash.
Project-owned tests and this README are maintained separately. The patch is
`third_party/patches/jsonschema-lazy-metaschemas.patch`. Regeneration retrieves
only the pinned module through Go's checksum mechanism and applies that patch.
The optional upstream HTTP-loader package is not included.

The startup change was motivated by alternating process measurements identifying
metaschema initialization as a top-three startup cost. Its opportunity score was
3 impact × 5 confidence / 2 effort = 7.5. Behavioral checks use fresh processes
for first-use/concurrency and compare accepted/rejected schemas and values
against unmodified upstream v5.3.1. Ordering and floating-point behavior are
unchanged; no random behavior is involved. Any speed claim requires the separate
candidate-versus-baseline measurements, rather than this source-level argument.

On upstream updates, inspect initialization and remove this snapshot if the
upstream implementation can avoid eager work. Otherwise regenerate the minimal
patch, rerun the differential and report-import suites, and repeat startup and
language-path measurements. No additional schema behavior belongs in this fork.
