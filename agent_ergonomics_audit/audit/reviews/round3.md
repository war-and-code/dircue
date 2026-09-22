# Round 3 independent CLI and schema review

Reviewed the frozen `84e96f8` source and the integrated candidate described by
`.cache/v100/integrated-final/build.json` (candidate SHA-256
`aa232134a07211f5755f8b8ec5862e71ed785e4e37fc55738332b48b2e4ff25a`). The
review compared CLI, schema, and documentation changes with `121027f`. Scanner
performance and packed-object work were excluded. I did not read earlier review
findings before completing this pass.

## Finding

### [P2] Escape terminal controls in returned source-path errors

`missingPathCommandHint` deliberately declines to add a suggestion when a path
contains terminal-unsafe characters, but it then returns the original error
unchanged (`internal/cli/diagnostics.go:133-141`). Scanner errors include the
source path, and `Execute` returns them directly (`internal/cli/cli.go:167`). The
executable prints that string verbatim (`main.go:16-17`). A caller-controlled
missing path can therefore inject ANSI control bytes or bidi-format characters
into stderr even though the new flag diagnostics escape those same characters.

Reproduction against the frozen integrated candidate:

```sh
cd .cache/v100/integrated-final
arg=$'missing\033[31m\u202e'
./dircue "$arg" >/tmp/out 2>/tmp/err
od -An -tx1 /tmp/err
```

The command exits 1 with empty stdout, but `/tmp/err` contains the literal byte
sequence `1b 5b 33 31 6d` and UTF-8 `e2 80 ae` inside the reported path. The same
result occurs through `analyze <profiler> PATH`; those commands do not call
`missingPathCommandHint` at all. This can alter terminal rendering or disguise
the surrounding diagnostic when an automation passes an untrusted pathname.

The narrow general fix is to sanitize the final error text at the `Execute`
boundary before returning it, while retaining the original error as the unwrap
cause so `errors.Is` and cancellation checks keep working. Applying
`terminalValue` only when the returned message contains control or `Cf`
characters preserves every ordinary legacy diagnostic byte-for-byte. Add a
test that invokes both the root language path and an `analyze` path containing
ESC, CR/LF, C1, and bidi-format characters, checks for exit/error behavior and
empty stdout, and asserts that none of those raw characters survive in the
returned message.

## Clean scope and limits

- The live Cobra catalog contained all 24 commands, the generated `--version`
  flag, stable defaults, output-contract mappings, and the intended rejected
  inherited flags for `capabilities`, `plan`, and `compare`. The guide's
  profiler reference matched current command help.
- Unknown long and short flags, invalid typed values, ambiguous spelling hints,
  and explicit false capability selectors failed without executing a guess.
  Assigned private values were absent from diagnostics, stdout stayed empty,
  and the candidate exited 1.
- Plain plan argv rendering escaped C0, C1, and `Cf` characters while remaining
  valid round-trippable JSON. The new CLI, guide, schema-export, and plan writers
  all propagate ordinary and short-write failures in their explicit output
  paths.
- The exported profile document contained the expected transitive component
  resources with absolute identifiers, and source review found no filesystem or
  network lookup path in `schema.Export`. Export allowlisting, reference scope,
  deterministic ownership, and whole-output versus component labeling were
  consistent with the CLI catalog and documentation.
- I did not rebuild or rerun the heavy race, compatibility, or performance
  suites, as requested. The supplied final build manifest binds the candidate
  to the reviewed files. A second independent Draft 2020-12 implementation was
  not installed locally, so schema interoperability beyond the repository's
  existing offline compiler coverage remains an explicit limit of this pass.
