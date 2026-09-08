# Conformance discrepancies

Review date: September 7, 2026.

## DISC-001: Flat directory extension

- Reference: repository statistics require a Git repository and revision.
- Dircue: also supports arbitrary local directories without Git metadata.
- Impact: users can profile exported/untracked checkouts. Flat scans necessarily use the available files rather than a committed Git tree.
- Resolution: ACCEPTED. Git-free input is a supported Dircue mode. The `*/flat-extension` checks compare a flat copy to identical committed reference contents and must pass.

## DISC-002: Diagnostic error wording

- Reference: Ruby exceptions and CLI-specific messages for rejected invocations.
- Dircue: Go/Cobra diagnostic messages.
- Impact: error text is not interchangeable; callers should rely on nonzero exit status.
- Resolution: ACCEPTED. Error exit status is compared; error wording is outside the measured compatibility contract. This applies to error-producing fixtures such as `subdirectory` when the reference rejects them.

Generated JSON and Markdown reports retain each unresolved mismatch. An unresolved failure must remain visible regardless of its effect on the matrix score. The harness covers the cases listed in [COVERAGE.md](COVERAGE.md).

## DISC-003: Requested subdirectory extension

- Reference: passing a directory below the Git repository root fails to open a repository.
- Dircue: auto mode profiles that requested subtree as an ordinary directory.
- Impact: successful output for a path Ruby rejects; only requested files are scanned, not the parent repository.
- Resolution: ACCEPTED as part of arbitrary-directory support. `subdirectory/*` comparisons remain XFAIL against Ruby rejection and must additionally match Ruby run on a separate committed copy of the subtree (`subdirectory-oracle`). A wrong subtree result is FAIL, not XFAIL.

## DISC-004: Quoted attribute patterns in flat mode

- Reference: the pinned Ruby/Rugged combination does not apply the quoted filename patterns in `attrs-quoted`.
- Dircue: Git source matches that behavior (with a diagnostic warning). Flat source supports Git-standard quoted patterns for paths containing whitespace.
- Impact: flat-directory language inclusion can differ when these quoted patterns are present.
- Resolution: ACCEPTED extension for flat mode only. `attrs-quoted/flat-extension` remains XFAIL against the direct reference and must also match the actual reference run on equivalent unquoted wildcard patterns (`attrs-quoted-portable-patterns`). Git source must match the direct reference exactly.

## DISC-005: Enry versus current Linguist classification data and heuristics

- Reference: Ruby Linguist 9.7.0 language definitions, heuristics, Flex tokenizer, and centroid classifier.
- Dircue: an isolated Enry source fork refreshed to Linguist 9.7.0, with the reference centroid model, a pure-Go port of the Flex tokenizer, and source-backed syntax corrections.
- Impact: the unmodified Enry baseline differs on some language names, newer formats and ambiguous files. Classification differences can affect default programming/markup statistics and downstream detectors for any language type.
- Resolution: RESOLVED for the pinned sample corpus. `results/samples.json` and `.md` record 3,388/3,388 exact candidate labels and ordered token sequences; the independent original Enry baseline remains 3,241/3,388. The update includes classifier/tokenizer changes, not only language-data regeneration. Unsupported regex branches outside the sampled inputs remain documented in the maintained fork; this bounded evidence is not universal input equivalence.

## DISC-006: Single-file symlink refusal

- Reference: an explicit symlink filename can resolve to another file and return that file's metadata.
- Dircue: explicitly requested symlink files are refused; repository walks also exclude symlinks.
- Impact: this single-file diagnostic invocation fails where Ruby may succeed.
- Resolution: ACCEPTED safety boundary for v0.1. `symlink/file-json` is XFAIL only when the candidate exits 1, emits no stdout, and identifies the non-regular Git file; the regular target is independently covered. Unexpected success or a different failure is not masked.

## DISC-007: Bounded inspection of large Git-free single files

- Reference: direct single-file inspection outside Git uses `FileBlob`, which loads the whole file for language/generated detection; its Charlock Holmes binary probe scans at most 1 MiB and treats UTF-16/UTF-32 BOMs as text. Repository statistics and committed single-file inspection use `LazyBlob` with a 128 KiB content view.
- Dircue: directory statistics and committed single-file inspection retain the 128 KiB repository view. A directly inspected Git-free regular file up to and including 1 MiB is read completely; larger single files retain only 128 KiB for classification and generated-code detection. Full file size remains available and large-file LOC/SLOC are suppressed, as in the reference.
- Impact: a NUL or language/generated marker beyond the retained prefix of a Git-free file larger than 1 MiB can produce different diagnostic metadata. This is a scoped resource policy, not full `FileBlob` equivalence.
- Resolution: ACCEPTED bounded-resource difference for v0.1. `single-file-limits-flat/over-limit-late-nul.cs/*` and `over-limit-nul-edge.cs/*`, plus `over-limit-generated.js/*` remain XFAIL against actual full-file Ruby inspection. They must independently match the actual Git `LazyBlob` reference for identical file bytes. Files at 300 KiB and exactly 1 MiB, including a NUL at 200 KiB, must match full-file Ruby exactly. A 2 MiB ASCII file with its first NUL beyond 1 MiB must match the reference text result. Git and directory scans must independently match the prefix reference. An arbitrary mismatch never qualifies as this XFAIL.
