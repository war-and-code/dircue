# Changelog

## 0.2.0 (unreleased)

- Add opt-in scc code metrics through `analyze metrics` and `analyze all --metrics`, with code, comment, blank, and total lines, bytes, file counts, and lexical complexity estimates.
- Report metrics by language and immediate parent directory, with optional per-file rows and explicit coverage and skip reasons.
- Count source files by default, with a broader text scope and bounded full-file reads. XML logs remain excluded from default counting unless attributes include them.
- Extend the aggregate JSON schema to 1.1.0 when metrics are requested. Existing language output and reports without metrics retain their contracts.
- Backport a go-git streaming delta fix: backward copies could reconstruct incorrect bytes from packed Git objects without reporting an error. The fix preserves bounded reads and applies to both language profiling and metrics.
- Close Git object and linked-worktree metadata files on success and error paths, preventing descriptor leaks and Windows cleanup failures.

## 0.1.0 (2026-09-08)

First release as dircue, renamed from auragaze before publication. Language profiling targets GitHub Linguist 9.7.0.

- Portable Go CLI with legacy language output, JSON/breakdown/strategy flags, single-file metadata, and Git revision selection.
- Committed Git-tree analysis by default at repository roots, plus arbitrary directories and explicit working-directory mode.
- Nested Linguist attributes, bounded concurrent classification, generated/vendor/documentation filtering, and language grouping.
- Maintained Enry compatibility fork with pinned Linguist data, centroid classification, pure-Go tokenizer, regeneration provenance, and license notices.
- Ecosystem, framework, and layout detectors with a versioned aggregate JSON schema.
- Differential checks against the actual Ruby CLI and upstream samples, pinned public-repository comparisons, cross-platform builds, and an unprivileged minimal Docker image.
- Java/.NET coverage and synthetic GiB-scale Talend/XML, interconnected project, packed-object, and 100,000-file cases, including regressions for binary-prefix detection and tree-size cutoffs.
- Local release packaging for Linux, macOS, and Windows, plus Python wheels containing the same binaries for installation with uv or pip.

Compatibility results apply to Linguist 9.7.0 and the recorded inputs. See the [conformance results](tests/conformance/results/latest.md), [classifier results](tests/conformance/results/samples.md), [final performance results](tests/performance/results/final/README.md), and [documented differences](tests/conformance/DISCREPANCIES.md). Attribute/resource limits, refusal to inspect symlink files, and arbitrary-directory support are intentional differences. Future Linguist releases require renewed comparison.

The [large-input methodology](tests/stress/README.md) distinguishes synthetic scale evidence from the real Java and .NET projects in the public corpus.
