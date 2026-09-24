# Maintained dependencies

Dircue embeds pinned Enry and go-git source snapshots under its root Go module. Their imports are rewritten to the local package paths, so consumers do not need Go `replace` directives. The original module manifests are retained as `upstream.go.mod` and `upstream.go.sum`; provenance records source checksums and narrowly scoped path rewrites. The scc processor is also embedded; its pinned version and integration are documented in [the scc integration guide](../docs/SCC_UPSTREAM.md).

## Pinned Enry data refresh

Dircue uses the public Enry API from `third_party/go-enry/`, embedded from official Enry v2.9.6. Its language data comes from that version's unmodified generator run against Linguist 9.7.0, and its classifier follows the pinned Ruby reference. The package preserves Enry's Go detection interface without mutating its exported data maps at runtime.

Regenerate with:

```sh
docker build -t dircue-linguist:9.7.0 -f tests/conformance/Dockerfile tests/conformance
python3 third_party/update_enry.py
```

`--archive /path/linguist-v9.7.0.tar.gz` reuses a cached download after checking its pinned SHA-256. The script downloads Enry through Go's module/checksum system in an isolated temporary module, verifies the Linguist archive, runs the upstream generator, and applies the checked-in patch. It exports the official gem's centroid model and checks its canonical hash. A cached `--model` JSON export can replace the Docker export, with the same hash requirement.

The output contains runtime Go source/data, regression tests, source licenses, and machine-readable provenance. It omits the unused Bayesian frequency table. A synthetic `.git/HEAD` records the exact upstream Git object during generation; the runtime has no Git checkout dependency. The updater never executes sample or application code.

`go-enry/PROVENANCE.json` pins both upstream sources, hashes the downloaded upstream module tree, and hashes every embedded output file. The retained upstream manifests are byte-identical to the downloaded module manifests. `patches/enry-linguist-9.7.patch` contains project changes; only imports of Enry's own module are rewritten to the embedded package path. The project-owned compatibility test is included in the patch and output manifest.

The updater checks existing output for unmanaged files. It removes obsolete files only when the preceding manifest owns them and their bytes remain unchanged. Before replacing the output directory, it validates and tests the staged tree, rechecks for intervening edits, and creates a backup for rollback. The next invocation checks for an interrupted replacement; ambiguous states require inspection. `GENERATOR_WARNINGS.txt` retains unsupported upstream regex diagnostics for review.

The compatibility patch makes general changes backed by the pinned Ruby source:

- Emacs modelines use the first syntactically valid delimiter pair, avoiding greedy matches inside X font names. See Linguist `lib/linguist/strategy/modeline.rb`.
- A `UseVimball` marker in the first five lines suppresses modeline inference for Vimball archives, as Ruby does.
- The Adblock header grammar's named subroutine is expanded into an equivalent RE2 expression. Its possessive repetition is unnecessary for recognition because version digits/dots cannot consume the following separator. The exact source expression is matched before applying this translation.
- The shell `exec` wrapper heuristic permits only the whitespace/quote separators in Ruby `lib/linguist/shebang.rb`; an intervening argument such as `-nef` no longer changes Shell to another interpreter language.
- Filename and extension detection intersect their matches with candidates from earlier strategies while preserving candidate order. Generic extensions are generated from Linguist's pinned `generic.yml` and pass candidates through unchanged, requiring a later strategy to confirm the language. This prevents a `.pl` suffix from broadening a Perl shebang back to Prolog and Raku and avoids assigning generic suffixes from the filename alone.
- Content heuristics inspect only the first 50 KiB of raw bytes, matching Linguist 9.7. Patterns later in a LazyBlob's 128 KiB read prefix do not affect disambiguation.

- Linguist 9.7's log term-frequency/inverse-class-frequency centroid model replaces the old Bayesian classifier. Inference considers only the first 50 KiB of raw bytes, while model training used complete samples, matching the pinned classifier. The canonical exported model is converted deterministically to an embedded, versioned binary and loaded once on first classifier use; no Ruby runtime is involved. Conversion preserves vocabulary mappings and float64 values. Every regeneration compares the binary decoder's output against an independent Go decode of the canonical JSON before publishing the maintained fork.
- The generic tokenizer is a pure-Go implementation of the pinned Flex rules: longest match, declaration-order ties, comment/string states, 16-byte tokens, and the native scanner's byte cap. The broad conformance harness verifies ordered token sequences against Ruby, in addition to labels.
- Generated-file line scanning follows Ruby `Generated#lines`: split on LF, preserve carriage returns and all final empty elements. This differs from Ruby's single-file metadata line handling and matters when source-map references precede trailing blank lines.

The generator warning file lists the remaining unsupported upstream expressions. Agreement on the current sample corpus does not cover every possible input or unsupported regex branch. The differential suite records sample comparisons separately from CLI/repository conformance and real-project results.

The scanner's binary preflight independently follows [Charlock Holmes 0.7.9's raw-byte policy](https://github.com/brianmario/charlock_holmes/blob/v0.7.9/ext/charlock_holmes/encoding_detector.c), the version pinned in the Ruby reference image: PostScript and Unicode BOM exceptions, specific binary magic signatures, and a maximum 1 MiB NUL probe over the supplied data. Repository callers supply only the LazyBlob 128 KiB prefix. Its [MIT notice](CHARLOCK_LICENSE) is retained; Charlock and ICU are not runtime dependencies. Language and generated-code strategies receive the original bytes, matching Linguist rather than implicitly transcoding source.

Enry is **Apache-2.0**, preserved verbatim in `go-enry/LICENSE`. Linguist's generated data is derived from its **MIT**-licensed source; that notice is preserved in `go-enry/LINGUIST_LICENSE`. Project-specific changes are identified in the patch and provenance; this is an embedded snapshot, not an upstream Enry release. The production build remains pure Go with `CGO_ENABLED=0`; optional Enry `oniguruma`/`flex` build modes are outside the supported Dircue build configuration.

## go-git reader backports

The embedded package under `go-git/` starts from go-git v5.19.2. It backports a
correction for [upstream issue #2378](https://github.com/go-git/go-git/issues/2378):
the streaming delta reader could reconstruct incorrect bytes after a backward
copy followed by a forward copy, without returning an error. The correction
resets the tracked base position when reopening the reader and closes the latest
reader on completion. It applies to Git content used for language profiling and
metrics while retaining streaming reads for large objects.

A second correction closes the loose-object file after `EncodedObjectSize`
reads its header, including error paths. Upstream v5.19.2 closes the decompressor
but leaves the underlying file open. This can accumulate file descriptors and
prevent repository cleanup on Windows. Linked-worktree `commondir` discovery
and failed lazy-object reader construction now also close their files. The
regressions cover valid ownership transfer and cleanup after malformed input.
A bounded packfile cache also closes a newly opened packfile when eviction of
the previous entry fails; the original eviction error remains authoritative.

The snapshot does not link go-git's transport client. dircue reads only local
repositories, so `remote.go` no longer imports `plumbing/transport/client`, and
fetch, push and clone report that no network transport is available. This keeps
`net/http`, `crypto/tls` and the SSH stack out of the binary;
`scripts/check-linked-deps.sh` and `internal/buildcontract` enforce it.

[`patches/go-git-reader-delta.patch`](patches/go-git-reader-delta.patch) records the
runtime changes and regression tests. The snapshot retains upstream production
Go sources, module files, and license, plus standalone delta and file-lifecycle tests; upstream
examples and fixture-dependent test suites are omitted.
[`go-git/PROVENANCE.json`](go-git/PROVENANCE.json) identifies the pinned upstream
module, patch, and retained file hashes.

Verify the maintained snapshot, or regenerate it into a fresh directory:

```sh
python3 third_party/update_go_git.py --check
python3 third_party/update_go_git.py --output .cache/go-git-regenerated
```

The updater uses Go's module/checksum system to retrieve the pinned release and
applies the recorded patch and checks the embedded package in a temporary module
manifest derived from upstream's retained manifest. This snapshot preserves go-git's Apache-2.0 license; it
is not an upstream release. When a released go-git version contains these fixes,
review whether the embedded snapshot can be refreshed or the upstream fixes can
replace it. Repeat packed-object
regressions, Git-versus-directory comparisons, and language profiling benchmarks
before adopting that update.
