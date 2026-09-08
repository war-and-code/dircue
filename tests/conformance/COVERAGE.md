# Compatibility scope and coverage

The harness uses the reference implementation to define the requirements below. Each generated case is tagged `MUST` within this scope. The report counts test invocations; its score applies only to the tested behavior, languages, and repositories.

| Category | Required observed behavior | Fixtures |
|---|---|---|
| Classification | Same language labels, grouping, byte counts, percentages and source lists | basic, grouped, shebang |
| Java | Modern records and modules, Maven and Gradle multi-project metadata, generated and vendor overrides | java-enterprise |
| .NET | Modern and legacy projects, project references, centralized build/package props, C# records/raw strings/file-scoped namespaces, XAML and Razor, generated and vendor overrides | dotnet-enterprise |
| Selection | Same inclusion for empty files, hidden paths, prose/data, binary, large source, vendor and generated files | empty, empty-source, hidden, data-prose, binary, large-source, lfs, vendor, generated |
| Encoding | Same source handling for Latin-1, UTF-16/32 and UTF-8 BOM | encoding |
| Attributes | Language aliases, generated/vendor/detectable overrides, nested precedence, macros/token precedence, quoting, unset and glob matching | attrs-* |
| Git | Analyze committed state despite dirty/untracked content; ignore links and gitlinks; subdirectory invocation semantics | git-dirty-head, symlink, submodule, subdirectory |
| Single files | Exact JSON metadata/text for Go, Python, JavaScript, Markdown, empty files, CRLF, PNG and late-NUL RTF; committed contents in dirty trees | single-files, git-dirty-head/file-json |
| Revision/options | Historical revision, invalid revision, tree-size cap, strategies, trailing flags | revisions, basic extra modes |
| Paths | Preserve Unicode, spaces and embedded newlines | unusual-paths |
| CLI output | JSON, JSON breakdown, text, text breakdown, combined short flags | Five modes per repository fixture |
| Single-file read scope | Full-file versus Git prefix binary/line inspection at 300 KiB, exactly 1 MiB and above 1 MiB; ASCII/UTF-16 NUL at 200 KiB; verified bounded-policy XFAIL against independent Git oracle | single-file-limits, single-file-limits-flat |
| Binary signatures and MIME | Actual PostScript with NUL, PNG/GIF/PDF/JPEG/WebP bytes, uppercase image extensions, text-bearing image names, UTF-16/32 BOMs; same Git and flat-file oracles | single-file-magic, single-file-magic-flat |
| Flat extension | Match the same committed reference contents without Git metadata | Every non-Git fixture copied into a flat tree |

The report generator creates a category × tested/passing/failing/score matrix. Any unexpected failure causes a nonzero exit. Deliberate exceptions belong in DISCREPANCIES.md and must remain visible as expected failures rather than being skipped.

The CLI matrix does not comprehensively cover upstream sample classification, every generated/vendor regex, Git LFS, sparse/partial clones, platform-specific path rules, filesystem races and permission errors, Git attribute edge cases, unlimited repository sizes, all single-file encodings/MIME types, or every invalid or combined option invocation. Separate Go tests cover local security and scanner invariants. The performance harness tests a broader open-source corpus.

## Exploratory upstream classification

`samples.py` independently compares all 3,388 regular files in the pinned upstream Linguist 9.7.0 `samples/` tree (765 top-level sample directories). All files receive reference and candidate results; exceptions are explicit errors. The generated `results/samples.json` and `.md` list exact labels and every discrepancy, with programming/markup counts distinguished from data/prose. These samples expand classification evidence, but do not exhaust every possible program or override combination and do not measure repository aggregation.
