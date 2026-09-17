# Code metrics

Dircue includes opt-in code metrics (introduced in 0.2) through the Go library from
[scc](https://github.com/boyter/scc). It counts complete files selected by dircue's
existing directory or Git scanner. Language-only commands keep their existing
output and bounded classification reads.

```sh
dircue analyze metrics /path/to/checkout
dircue analyze metrics --json --files /path/to/checkout
dircue analyze all --metrics --json /path/to/checkout
```

`analyze metrics` runs language classification and counting, with ecosystem,
framework, and layout detectors disabled. Its JSON output uses the full profile
envelope; those finding arrays are empty. `analyze all --metrics` also runs the
existing detectors. Plain `analyze all` does not run scc.

## Scope and limits

The default `--metrics-scope source` counts files included in language
statistics. Generated, vendored, documentation, and data-language exclusions
apply unless `.gitattributes` overrides them. XML logs are therefore excluded
by default. An explicit `*.xml linguist-detectable=true` override can include
XML files.

Use `--metrics-scope text` to count detected textual languages beyond the source
selection, including XML, documentation, and generated or vendored files.
Binary files, symlinks, and special files are excluded. Files with no supported
language grammar remain uncounted and receive a reason in the report. Text scope
is a counting option; it does not change the language statistics or detector
selection.

```sh
dircue analyze metrics --metrics-scope text --files --json /exported-directory
```

Counting requires valid UTF-8 content; dircue does not transcode other encodings.
A file can contribute to language statistics but remain uncounted with
`unsupported_encoding`.

Metrics require complete file contents. The default `--metrics-max-file-bytes`
is 16 MiB (16,777,216 bytes). The permitted range is 1 through 268,435,456 bytes
(256 MiB). Larger files retain their ordinary language-statistics treatment but
are skipped for counting. Dircue never presents a prefix count as a full-file
count. The existing `--max-file-bytes` and `--tree-size` scan limits still apply.

This per-file bound is not a process memory limit. Concurrent buffers, scc's
language data, Git object reconstruction, and report rows use additional memory.
Use `--workers` to reduce concurrent work and operating-system or container
limits when a hard resource limit is required. Per-file output also increases
report size and retained memory.

`--metrics-scope` and `--metrics-max-file-bytes` are available on
`analyze metrics` and `analyze all`; on `all` they require `--metrics`. `--files`
also serves structural analysis. On `analyze all`, it applies to both counting
and structure when both are requested.
`--breakdown` retains its separate meaning: include paths in language statistics.

## Reading the report

Metrics reports use `schema_version: "1.1.0"` when project mapping and structural
analysis are absent. Adding either module uses `"1.2.0"`, with the same metrics
object. Reports without these optional modules retain `"1.0.0"`. The
[JSON schema](../schema/profile.schema.json) describes all three versions. Legacy
`dircue --json` is unchanged.

The `metrics` object records the engine and its version, selection scope,
per-file limit, and content source. Git reports identify the selected tree.
Keep these fields when comparing reports: counts from different scopes, engine
versions, or input revisions may not be comparable.

A Git tree is an immutable input. Directory mode reads live files and cannot
provide an atomic snapshot of a changing filesystem. It detects some changes
between classification and counting and reports `input_changed`; use a stable
directory or committed Git tree when repeatable input matters.

| Field | Meaning |
| --- | --- |
| `totals` | Files, bytes, lines, code lines, comment lines, blank lines, and lexical complexity for counted files. |
| `languages` | Counts grouped by dircue's language name and the scc grammar used. |
| `directories` | Counts grouped by each file's immediate parent directory, without descendant totals or inferred project boundaries. |
| `skipped` | File counts grouped by exclusion or failure reason. |
| `files` | Optional per-file counted or skipped rows, enabled with `--files`. |

Each file is counted completely or skipped. Totals contain only counted files;
line counts are never extrapolated. A `counted` file row contains its language,
grammar, and counters. A `skipped` row contains a reason instead of counters.
Paths are relative to the selected root. Empty collections are arrays, and
ordering is deterministic across worker counts.

Coverage has three states:

- `complete`: counting finished for the selected scope; expected exclusions may
  still appear, such as `outside_scope`, `binary`, or `non_regular_file`.
- `partial`: some intended inputs could not be counted, for example because of
  `file_too_large`, `unsupported_language`, `unsupported_encoding`,
  `input_changed`, or `lfs_pointer`. Totals cover the files counted in full.
- `skipped`: the scan could not provide metrics, such as when the tree-size limit
  was reached. Check the reason and warnings before using the totals. For
  `tree_size_limit`, the skip entry omits `files`: enumeration stopped before
  the number of affected files was known. Text output shows `Skipped: unknown`
  for that case.

A partial report can be a successful invocation with exit status 0. Consumers
that require complete counts must check `metrics.status`, as well as the exit
status. Fatal scan errors still return a nonzero exit status.

## What scc contributes

| Capability | Integration |
| --- | --- |
| Code, comment, blank, and total line counts | Included for supported selected languages. |
| Byte and file totals | Included alongside line counts. |
| Lexical complexity estimate | Included using scc's grammar rules. |
| Per-file, language, and parent-directory summaries | Included in the versioned report. |
| scc's own file walker and language-selection policy | Not used; dircue selects the source and language. |
| COCOMO cost estimates, duplicate detection, ULOC, or scc's output formats | Not integrated. |
| Tree-sitter/BCA metrics | Separate optional [structural analysis](STRUCTURE.md), with per-file BCA aggregates for 20 supported languages. |

Complexity in the `metrics` object is scc's lexical estimate, based on
language-specific tokens. It is not an AST-derived measure, a defect count, or
proof of code quality. The optional `structure` object retains BCA measurements
separately; values from the two engines should not be treated as interchangeable.

Counts are useful for sizing collections and comparing their composition;
interpret them with the selected grammar and coverage information. In text
scope, scc may label XML or plain-text lines as `code`; that counter does not
mean the file contains a programming language.

The pinned scc version has known lexical limitations with Java text blocks and
C# raw strings. Some valid multiline strings containing quote and comment-like
lines can be counted as code plus a comment instead of code throughout. Dircue
preserves scc's results; support for a language does not guarantee correct
handling of every syntax construct. The [scc dependency notes](SCC_UPSTREAM.md)
record the pinned version, integration choices, and reference checks.

The integration pins scc as a dependency. Scans do not fetch language definitions
or execute the files being inspected. Dependency updates should rerun the
adapter's reference comparisons and the scanner's scope and limit tests.
