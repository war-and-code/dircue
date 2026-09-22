# Task 04: Code metrics without installing a worker

**Status.** COMPLETE

**CLI calls.** 1 intended-operation call.

**First intended operation success?** YES.

`analyze metrics --json .` returned a complete `scc` metrics module for committed HEAD, including totals, language, directory, source mode, and tree identity. No structural worker was requested or installed.

**Round-trips to completion.** 1.
