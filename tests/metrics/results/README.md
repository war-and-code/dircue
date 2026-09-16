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

## Performance

The final measurements use clean source commit
`bf315bb5fb58e7a45ba7cd66451c08590770de7b` and compare it with the 0.1 baseline
`c3444a4e899678c798bccdbaec7f87de4c3d7bbb`. Both dircue executables were built
with Go 1.26.6, `CGO_ENABLED=0`, and `go build -trimpath`, on the same macOS arm64
host. Each command had three warmups and 20 samples, with alternating command
order and warm filesystem caches. No power or kernel settings were changed.

Language-only JSON matched exactly. All five scenarios stayed within the stated
10% p95 regression envelope. The measurements do not establish identical cost
or guarantee performance on other hosts or repositories.

| Input | 0.1 language median | 0.2 language median | Language p95 change | Metrics median | Added counting time | Metrics peak RSS |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| Spring Framework | 1.654 s | 1.650 s | +4.9% | 1.665 s | +0.9% | 174.0 MiB |
| Roslyn | 6.961 s | 6.604 s | -6.3% | 8.865 s | +34.2% | 365.5 MiB |
| ASP.NET Core | 2.063 s | 2.060 s | -0.3% | 2.092 s | +1.6% | 230.5 MiB |

Added counting time compares metrics with the candidate's language-only median.
Metrics reads eligible files in full, while language classification uses bounded
content. Roslyn includes many large source files, so the additional work is more
substantial there. Unsupported grammars and encodings remain explicitly skipped;
these measurements do not imply complete coverage of every source file.

On empty and tiny directories, language p95 changed by +4.8% and +7.3% respectively.
The tiny Java metrics command took 19.8 ms median with 25.2 MiB peak RSS.

On the same 11 selected source fixtures, dircue metrics took 28.9 ms median and
41.6 MiB peak RSS; standalone scc took 15.3 ms and 21.1 MiB. Dircue also classifies
files and reports counting coverage. This result is not a speed win over scc.

Peak RSS is measured by the system `time` launcher, not inferred from Go heap
allocation. CPU timings have hundredth-second resolution and are too coarse for
tiny inputs. Reported p99 and higher values are observed order statistics;
20 samples cannot estimate rare tails. The host retained ordinary desktop
background activity and was not a dedicated benchmark machine.

The interrupted run affected by unrelated build interference and runs predating
later reader fixes are excluded from these conclusions. `attempts.json` explains
the retained local receipts. Final measurements are in `performance-startup.json`,
`performance-corpus.json`, and `performance-selected.json`;
`performance-gate.json` records the comparison checks.

`build.json` records the candidate binary identity, compiler metadata, and hashes
of tracked Go files plus both embedded data files. `baseline-build.json` identifies
the archived 0.1 source build. Later documentation-only commits can be checked
against this source inventory without claiming they are the measured executable.
`SHA256SUMS.json` covers the published artifacts in this directory.
