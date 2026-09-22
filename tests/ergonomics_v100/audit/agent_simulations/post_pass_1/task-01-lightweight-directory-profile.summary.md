# Task 01: Lightweight machine-readable directory profile without optional worker/heavy analysis

**Status.** COMPLETE

**CLI calls.** 2 total: 1 discovery/help call and 1 intended-operation call.

**First intended operation success?** YES (step 2).

**What worked.** Global help directly explained that bare profiling plus `--json` emits the lightweight language map and that `--source directory` selects current files. The first operational command returned valid JSON with Go and JavaScript totals and no diagnostics.

**What was confusing.** Nothing blocked the task. The help clearly distinguished the legacy language map from aggregate analyzer profiles.

**Round-trips to completion.** 2, including discovery.
