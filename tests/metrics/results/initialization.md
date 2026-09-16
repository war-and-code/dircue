# Counting engine initialization

The first integration initialized every scc grammar when counting the first file.
An allocation profile of `TestCountKnownResults` (one C# and one Java input,
three lines each) attributed 68.78 MiB, or 95.69% of sampled allocation, to
`processor.ProcessConstants`.

| Rank | Location | Allocated bytes, expressed in MiB | Interpretation |
| --- | --- | ---: | --- |
| 1 | `processor.(*Trie).Insert` | 58.45 flat | Grammar token tries |
| 2 | `processor.(*Trie).InsertClose` | 5.28 flat | Closing-token tries |
| 3 | `processor.processLanguageFeature` | 4.77 flat; 68.69 cumulative | Feature construction |
| 4 | `processor.ProcessConstants` | 68.78 cumulative | Initializing every grammar |

Rows overlap when cumulative values include callees; do not add cumulative rows.
The profile records allocation, not peak RSS. Profiling overhead is included in
the remaining allocations. CPU sampling was too short for useful attribution.

The profile was captured from integration commit
`4b79e2f3c9b2c5e11a01b54272bc8a33bd07e558` with Go 1.26.6 on macOS arm64,
`CGO_ENABLED=0`, normal compiler optimization, and a 65,536-byte allocation sample
rate. Raw profiles and their hash receipt remain in the ignored
`.cache/metrics-v020/` directory. The checked-in `eager-alloc-top.txt` records the
ranked profile output.

Unused grammar initialization dominated small-input counting allocation. The
change uses scc's public lazy feature loader to construct only the selected
counting grammar. Counter settings remain unchanged.

The lazy-loading change was committed separately as
`a0220a5`. Repeating the same allocation profile attributed 678.68 KiB to
`codemetrics.Count`, including 521.05 KiB in feature construction. This is about
99% less sampled allocation in the counting call than the eager initialization.
The remaining profile total is mostly profiler overhead; it is not an estimate of
normal process memory. See `lazy-alloc-top.txt`. Process peak RSS and startup time
are measured separately by the CLI harness.

The tiny Java and mixed-language fixture reports remained byte-identical after
lazy initialization and the Git reader fix. Standalone comparisons also passed
for the fixture suite and all sampled Java/C# files, including every counted file
over 128 KiB and the Roslyn Visual Basic regression. Coverage and counter checks
are recorded separately from timing results.
