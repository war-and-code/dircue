# Metrics validation

These receipts compare dircue's optional counting module with standalone scc
4.1.0. Matching counters establish compatibility with that engine, not complete
language syntax support. In particular, the fixture suite records a known scc
limitation involving comment markers inside Java text blocks and C# raw strings.

The fixture suite checks Java, C#, TSX, line endings, Unicode, missing final
newlines, generated/vendor/documentation scope, aggregation, full-file limits,
and Git revision selection. It also writes a 1,100 MiB XML file: default source
counting excludes it, while explicit text counting reports incomplete coverage
when the file exceeds the counting limit.

The corpus comparison checked 910 committed files against native `git show`
bytes and standalone scc: 206 from Spring Framework, 490 from Roslyn, and 214
from ASP.NET Core. Selection includes 200 Java/C# files per project ordered by
path hash, every counted Java/C# file over 128 KiB, and the Roslyn Visual Basic
regression. Overlapping selections are counted once. Immutable Git trees have
no unexpected `input_changed` skips after the reader fix.

Reports remain partial where a detected language has no counting grammar or an
eligible file uses an unsupported encoding. Completed totals exclude those
files; they are not estimates for missing content.

The reader correction check compared all per-file outputs before and after the
Git delta-reader fix. Twenty-four Roslyn files and one ASP.NET file changed;
every corrected row matched standalone scc on native Git bytes. Spring's counters
were unchanged. These are corrected inputs, separate from the lazy grammar
initialization change. The tiny Java and mixed fixture reports remained
byte-identical through both changes.

## Receipts

- `fixture-correctness.json`: fixture assertions, source hashes, and per-file counters.
- `corpus-correctness.json.gz`: corpus commits, selected paths, source hashes, and counters.
- `reader-corrections.json`: all changed rows and native-input comparison results.
- `golden-equivalence.json`: exact report equality for the initialization change.
- `initialization.md`: allocation profiles before and after lazy grammar loading.

Each receipt identifies its tested executable by SHA-256. Local workspace/home
paths are replaced by `$WORKSPACE`/`$HOME`; source paths, counter values, and
hashes are unchanged. Original receipts remain in the ignored
`.cache/metrics-v020/` directory. To inspect the compressed corpus report:

```sh
gzip -dc tests/metrics/results/corpus-correctness.json.gz
```

Performance results are still provisional while the final candidate and a quiet
measurement window are established. A corpus timing run overlapped an unrelated
local build; its partial results are retained locally and excluded from
performance conclusions.
