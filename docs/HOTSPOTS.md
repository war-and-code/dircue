# Structural hotspots

```bash
dircue analyze structure --hotspots --json \
  --structural-worker /path/to/dircue-structural-worker /checkout
```

Hotspots identify the largest measured function spaces under two defined metrics.
They do not assign a repository grade, estimate defect probability, or recommend
that a function should be rewritten.

Use `analyze all --structure --hotspots` to include the same observations in a
combined report. `--functions` is independent: it can retain the existing detailed
function evidence alongside hotspots. Neither option changes the default language
analysis path.

## Population and coverage

The worker visits every **Function-kind space reported by BCA** in each selected,
supported source file. It calculates distributions and selects candidates before
limiting retained evidence. It does not sort the existing function sample, which
retains at most 128 entries per file and 1,024 overall.

A valid measured space has a one-based, inclusive source span within its file.
Spaces with invalid spans are counted in `invalid_span_spaces` and excluded from
both metrics. Within each group, the metric population is therefore:

```text
metric.count = total_spaces - invalid_span_spaces
```

Groups remain separate by **language, grammar and syntax cohort**:

- `clean`: the parser reported no syntax recovery for the file.
- `recovered`: the parser recovered from missing or unrecognized syntax. Its
  extracted spaces and measurements are provisional, even when their spans fit.

A clean parse does not establish that the source compiles or behaves correctly.
BCA determines what counts as a function space for each language; these are not
compiler-resolved symbols. Constructors, local functions, closures and other
language constructs can be represented differently by different grammars.

The parent structural report records selected-file coverage and omissions,
including unsupported languages, oversized files and traversal limits. An omitted
file has an **unknown** function population. It contributes neither synthetic zero
measurements nor a fabricated count of missing functions. Outside-scope files are
also disclosed. The result describes the measured selected-source population,
not every possible function in the directory.

All languages supported by the pinned structural worker use this mechanism.
Language groups are not combined into a cross-language ranking. The report names
`big-code-analysis@2.2.0`, the function-population rule version and each grammar.

## The two measurements

| Metric | Definition | Unit |
| --- | --- | --- |
| `cyclomatic_sum` | BCA's standard cyclomatic sum for a Function-kind space and its nested spaces. | Cyclomatic count |
| `span_lines` | `end_line - start_line + 1`, including blank lines, comments and nested spaces. | Physical source lines |

Both measures can include overlapping nested spaces. A parent function and its
nested function are separate observations; **adding their values would double
count shared content**. The report does not calculate a repository-wide sum of
these measures. `span_lines` is not an executable-line count, and
`cyclomatic_sum` is not the parent's exclusive branch count.

A bounds check or error handler can increase cyclomatic count while improving a
program. Table-driven dispatch can lower the count without reducing the number
of possible behaviors. Straight-line code can contain a defect. These measurements
provide inspection evidence, not a universal interpretation of software quality.

## Exact distributions, bounded evidence

Each metric has a count, minimum, maximum and an exact 65-bucket histogram:

- Bucket `0` contains value `0`.
- Bucket `i`, for `1 <= i <= 64`, contains values from `2^(i-1)` through `2^i - 1`,
  inclusive.

For example, buckets 1, 2 and 3 contain `1`, `2–3` and `4–7`. Bucket 64 ends at
`18446744073709551615`. Bucket counts sum to the measured population. Empty
populations have `null` minimum and maximum, zero bucket counts and an empty top
list. Missing measurements are not represented as zero.

The histogram counts and extrema are exact. The report does **not** provide exact
percentiles or pretend that a bucket boundary is an observed percentile value.

Each group retains the top **10** spaces for each metric, ordered by decreasing
value, then original relative path, then BCA's one-based preorder index within the
file. The worker retains each file's top ten; merging those candidates preserves
the global top ten under the same ordering. All valid spaces still contribute to
the histogram and extrema, including those absent from the top lists.

Every retained entry has its metric value, source span, provider index and source
SHA-256. Function names are included only when nonempty, control-free and at most
256 UTF-8 bytes; `name_status` distinguishes present, unavailable and omitted
names.

Relative paths are included only when control-free and at most 1,024 UTF-8 bytes.
`path_status` is `present` or `omitted`, and `path_sha256` always records the digest
of the original relative-path bytes. Oversized or control-bearing paths remain
eligible for ranking; the original path is used privately for tie ordering and
is not serialized. Source and path digests serve different purposes.

Omitted names or displayed paths do not reduce measurement coverage or make an
otherwise complete population partial. Their entry-level status fields disclose
the reduced display evidence. Invalid spans, recovered files and parent coverage
limits qualify population status separately. `file_coverage_status` records the underlying
structural file coverage before any separate `--functions` retention limits are
applied. Thus the parent report can be partial because its detailed function list
was truncated while the hotspot population remains complete.

## Execution and saved reports

`--hotspots` requires an explicit structural worker with hotspot support. Before
walking the selected source population, dircue performs a bounded
`--capabilities` check. That also rejects an older worker for an empty directory
or a directory containing only unsupported files. The check does not parse a
source file. It obeys cancellation and the configured worker timeout.

Each analyzed file still has one BCA-owned parse. File observations, file metrics,
optional detailed functions and optional hotspots reuse that parse and its
metrics tree. Histogram collection and top selection add a bounded traversal;
the capability check adds one worker process per scan. Source-size limits and
worker timeouts still apply. These limits do not constitute a process-RSS cap.

Reports containing hotspots use aggregate schema **1.5**. Legacy reports retain
their earlier schema versions when the new modules are not requested. Saved-report
comparison retains provider, rule, language, grammar, syntax cohort and coverage
distinctions. Hotspot comparisons describe observed cohort measurements; they do
not establish function identity or prove removals from a bounded ranking. A lower
value is not evidence of whole-repository improvement. Follow the comparison
report's compatibility and observed-only qualifications rather than interpreting
a missing difference as equivalence.
