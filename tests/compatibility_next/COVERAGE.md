# Coverage

| Group | Cases | What is exercised |
| --- | ---: | --- |
| Retained CLI matrix | 118 | The unchanged fixture generator from `tests/compatibility_v030/run.py`: legacy JSON, text and breakdown; languages, ecosystems, frameworks, all and metrics; Git and directory sources; revisions, staged/unstaged/untracked files; empty and unborn repositories; limits, invalid arguments and command-like directory names. |
| 0.3 project analysis | 55 | Text and JSON; combined projects/metrics; directory and committed snapshots; worker counts; tree and file bounds; empty/unborn sources; malformed and oversized declarations; invalid revisions and file targets. |
| Structural argument validation | 6 | Missing explicit worker, missing worker file, invalid size bounds and timeout handling. These check actual released behavior, including validation order. |
| Native structural analysis | 30 | Twenty supported languages in 21 fixtures, including JSX; individual languages and combined reports; serial and concurrent scanning; size/tree limits; parser recovery, unsupported Swift, generated-code exclusion, and committed versus dirty source. |

The project fixtures include .NET project cycles, unresolved/conditional references, imports, shared build properties, shared package declarations, SDK selection, NuGet configuration and solution declarations. Java fixtures include Maven modules/parents and Gradle settings/toolchain/wrapper declarations. Their presence tests the released parser's behavior; it does not assert complete build resolution.

The malformed fixture includes a declaration larger than 1 MiB, malformed XML/JSON and an external XML entity declaration. No external resource is fetched. Independent assertions require the reference to report partial project coverage, discover multiple real projects, parse every supported structural fixture once, and qualify recovered/unsupported structural input.

The six harness self-tests cover byte-preserving captures, newline/status/stderr differences, digest corruption, ambiguous encodings, false-empty structural success, and isolation of pristine fixtures from the dirty Git copy.

Not covered: other operating systems in the checked-in receipt, every declaration dialect or language syntax, arbitrary permission failures, concurrent filesystem mutation, worker timeouts/crashes, malicious worker output, and performance. Dedicated scanner, worker, schema and broader fixture suites cover additional boundaries; this matrix does not replace them.
