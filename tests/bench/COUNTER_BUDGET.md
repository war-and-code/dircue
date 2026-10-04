# Deterministic counter budget

`counter_budget.json` pins the 16 local fixture directories and their exact
regular-file bytes and symlink targets. The baseline was measured with the
recorded fresh source build from the `v1.3.0` commit; the published release
binary hash is recorded separately and was not the measurement binary.

The optional `--budget` flag verifies fixture inventory and content, then gates
the head build at baseline plus the larger of 25% or a small floor: two files
for file counters and 256 logical bytes for `bytes_requested`. Limit-hit
counters must stay at their baseline value of zero. This catches large changes
in deterministic logical work while leaving room for small fixture evolution.
These ceilings do not claim to measure exhaustive filesystem I/O or CPU and
memory quotas. Wall time, heap and GC counters remain report-excluded.

To review a new baseline, build a candidate binary with the desired pinned
toolchain, run every exact fixture with `--stats-json`, review the resulting
counters and fixture digests, then update the JSON provenance, baselines and
digests in the same reviewed change. There is no automatic budget regeneration.
