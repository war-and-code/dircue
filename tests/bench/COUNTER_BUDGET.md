# Deterministic counter budget

`counter_budget.json` pins the 16 local fixture directories and their exact regular-file bytes and symlink targets. The baseline was measured with the recorded fresh source build from the `v1.3.0` commit; the published release binary hash is recorded separately and was not the measurement binary.

The optional `--budget` flag verifies fixture inventory and content, then gates the head build at baseline plus the larger of 25% or a small floor: two files for file counters and 256 logical bytes for `bytes_requested`. Limit-hit counters must stay at their baseline value of zero. These ceilings catch large changes in logical work while allowing small implementation changes on fixed inputs. Changing fixture contents requires a reviewed digest update. Wall time, heap and GC observations are excluded; these counters are not exhaustive filesystem I/O or CPU and memory quotas.

All baseline entries are validated before either binary starts. Ceiling arithmetic stays integral even for counters beyond floating-point precision. These are instrumentation checks: a passing cost budget does not prove correct map output. The fixture, conformance and compatibility gates supply those separate checks. Timed-out commands receive process-group cleanup; this is not containment for a program that intentionally detaches itself.

To review a new baseline, build a candidate binary with the desired pinned toolchain, run every exact fixture with `--stats-json`, review the resulting counters and fixture digests, then update the JSON provenance, baselines and digests in the same reviewed change. There is no automatic budget regeneration.
