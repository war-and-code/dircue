# Structural prototype driver

This standalone Go module runs the experimental Rust worker. It does not change
the `dircue` binary, commands, or report schemas.

Build the worker as described in the parent README, then run from this directory:

```sh
go run . --worker ../worker/target/release/dircue-structural-worker /path/to/source
```

The worker path is resolved against the invocation directory before scanning.
The driver never searches for, downloads, or builds a worker. Supply flags before
the target directory; omit the target to scan the current directory.

Options:

| Flag | Default | Meaning |
| --- | --- | --- |
| `--worker` | Required | Path to an existing executable |
| `--mode` | `combined` | `combined`, `structure`, or `metrics` |
| `--max-file-bytes` | `8388608` | Source byte limit, between 1 and 8 MiB |
| `--timeout` | `10s` | Positive time limit for each worker invocation |

The driver inspects live working-directory contents, including uncommitted files.
It does not implement dircue's Git HEAD selection or `.gitattributes` rules.
It selects `.java` and `.cs` extensions, case-insensitively, and prunes hidden
directories plus `node_modules`, `vendor`, `bin`, `obj`, and `target` at any depth.
Other extensions, including XML, increment `ignored_files` without reading their
contents. The traversal skips symbolic links and other non-regular candidate
files. It does not provide a filesystem snapshot or protection against concurrent
directory changes. Run against a stable checkout: the worker timeout does not
bound filesystem operations, which can block on remote storage or concurrent
replacement of files with special devices.

Each eligible file starts one worker process. Its source must be valid UTF-8,
contain no NUL byte, and fit the configured byte limit. Source reads stop after
the limit plus one byte, so file growth cannot cause an unbounded read. Workers
run sequentially and exit before the next file. This avoids retaining a whole
repository's syntax trees or results. The source cap is **not a hard RSS limit**:
JSON encoding and parsing, grammars, syntax trees, and metric calculations need
additional memory. Worker standard output is limited to 16 MiB and standard error
to 64 KiB; excess output cancels the worker. Timeout and interruption also cancel
the worker. These controls apply to the worker process, not an arbitrary process
tree; use the supplied worker, which does not spawn children.

Output is JSON Lines, in lexicographic traversal order. Each selected file emits
one record, followed by a final summary. Directory exclusions emit their own
records. Paths are relative to the input root and use `/` separators. Worker
timings vary between runs; ordering and observation fields are deterministic for
unchanged inputs and dependencies.

```json
{"type":"file","path":"Main.java","language":"Java","status":"observed","result":{"status":"complete","parse_count":1}}
{"type":"summary","scope":"working-directory","mode":"combined","status":"complete","observed":1,"skipped":0,"errors":0,"partial_files":0,"excluded_directories":0,"ignored_files":0}
```

The example shortens the worker result. `result` otherwise contains the worker's
entire JSON object. File status is `observed`, `skipped`, or `error`. A worker can
return partial observations for malformed syntax; that remains an observed file
and increments `partial_files`. Oversized, non-UTF-8, NUL-containing, and
non-regular candidate files increment `skipped`. Failed reads, worker errors,
timeouts, and invalid worker responses increment `errors`.

A summary is `partial` when any candidate was skipped, failed, or returned partial
observations. Ignored extensions and directory exclusions are intentional scope
choices and do not make the summary partial. Exit status is 0 for a completed
scan, including documented skips or partial observations; 1 for scan/worker/output
errors; and 2 for invalid arguments. Consumers should inspect the summary, not
infer full coverage from exit status alone. Interrupted or failed output may have
no usable final summary.

Run driver tests with `go test -race ./...` from this directory.
