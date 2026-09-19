# Optional function-space metrics

The development branch after 0.3.0 can report bounded function-space metrics
from [big-code-analysis](https://github.com/dekobon/big-code-analysis) (BCA).
This option needs a worker built from the matching development source. The
downloadable 0.3.0 worker does not provide it.

```sh
dircue analyze structure --functions --json \
  --structural-worker /opt/dircue/dircue-structural-worker /checkout

dircue analyze all --structure --functions --json \
  --structural-worker /opt/dircue/dircue-structural-worker /checkout
```

`--functions` is independent of `--files`. It retains function observations
without requesting all per-file structural records. On `analyze all`, it
requires `--structure`. Existing commands without this option retain their
output contracts and do not collect function entries or hash source for them.

## What an entry means

An entry represents a space BCA labels `Function`. It includes its source path,
language, complete-source SHA-256, provider index, and one-based inclusive line
span. The optional name is a syntax-derived label. Anonymous or unnamed spaces
can have an unavailable name; names containing control characters or exceeding
256 UTF-8 bytes are omitted with an explicit status.

This is not a compiler's inventory of callable methods. Parser support and
language constructs affect which spaces exist. Inheritance, overload resolution,
runtime dispatch, and cross-file calls are not resolved. Source hashes identify
the bytes analyzed; a path alone does not establish an unchanged source file.

BCA 2.2.0 supplies ten metric groups for these spaces: `abc`, `cognitive`,
`cyclomatic`, `halstead`, `loc`, `mi`, `nargs`, `nexits`, `nom`, and `tokens`.
Their nested values retain BCA's definitions and null values for unavailable
measurements. These groups differ from the file-level metric inventory.
Dircue does not fill missing values with zero or combine them into a grade.

**Metrics include nested spaces.** A containing function and its nested
function can describe overlapping work. Adding their metrics double counts
that work. Compare the same measure, provider, language, and unit of analysis;
lower complexity is not evidence that code is correct or better maintained.

The worker parses each selected file once. Function evidence uses the same BCA
metrics tree as per-file analysis; it does not start a second parser.

## Bounds and coverage

The report keeps at most 128 valid entries per file and 1,024 entries overall.
It selects entries by source path and then provider traversal order, independent
of scan concurrency. **This is a bounded sample, not a top-complexity ranking.**
Later entries can have larger measurements than retained ones.

`structure.functions` includes the provider, rule/version, metric scope, limits,
selection order, file coverage, and counts:

- `total_spaces`: all observed function spaces in successfully processed files.
- `invalid_span_spaces`: spaces omitted because their spans did not fit the source.
- `per_file_omitted_spaces`: otherwise valid spaces beyond the per-file limit.
- `report_omitted_spaces`: retained per-file entries excluded by the report limit.
- `omitted_spaces`: the sum of those two limit-related counts.

`total_spaces` equals the retained entry count plus `omitted_spaces` plus
`invalid_span_spaces`. It does not estimate functions in unsupported, skipped,
or unprocessed files. Check `status`, `parent_status`, `omissions`, and partial
file counts before using the measured population. Syntax recovery, invalid
spans, omitted names, and entry caps prevent a complete claim. An empty list
does not prove there are no functions in the directory.

The aggregate owns these entries. Requesting `--files` does not duplicate
function lists under every file. Function-enabled reports use schema `1.3.0`;
older structural invocations keep schema `1.2.0`.

The existing [structural input selection, deadlines, and size bounds](STRUCTURE.md)
still apply. This feature supplies evidence for future review views and report
comparisons; it does not rank files, set quality thresholds, or recommend
skipping other analysis.
