# Task 05: Recover from deliberate `--jsno` and verify the failed guess performs no analysis

**Status.** COMPLETE

**CLI calls.** 2 intended-operation calls.

**First intended operation success?** NO — corrected at step 2.

The typo exited 1 in 16 ms, emitted no stdout, and named both `--json` and the exact applicable help route. The corrected command succeeded. The observable evidence supports that the rejected guess produced no analysis output; this black-box run cannot prove unobservable internal work.

**Round-trips to completion.** 2.
