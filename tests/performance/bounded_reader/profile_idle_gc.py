#!/usr/bin/env python3
"""Run a separate two-GC idle-retention diagnostic with the allocation harness.

This changes only the temporary instrumentation main. Production runtime and
existing one-GC results remain untouched. Accepts profile_allocations.py flags.
"""
import hashlib
import json
from pathlib import Path
import sys
import tempfile

import profile_allocations as harness


def main():
    initial_script = harness.sha(Path(__file__))
    original = (harness.HERE / "profile_main.go.txt").read_text()
    marker = "\truntime.ReadMemStats(&live)\n"
    assert original.count(marker) == 1
    changed = original.replace("var before, after, live runtime.MemStats", "var before, after, live, second runtime.MemStats")
    changed = changed.replace(marker, marker + "\truntime.GC()\n\truntime.ReadMemStats(&second)\n")
    marker = '\t\t"post_gc_live_heap_bytes": live.HeapAlloc,\n'
    assert changed.count(marker) == 1
    changed = changed.replace(marker, marker + '\t\t"post_second_gc_live_heap_bytes": second.HeapAlloc,\n'
                              '\t\t"post_second_gc_heap_inuse_bytes": second.HeapInuse,\n'
                              '\t\t"post_second_gc_heap_objects": second.HeapObjects,\n'
                              '\t\t"heap_profile_forced_gc_rounds": 2,\n')
    with tempfile.TemporaryDirectory(prefix="dircue-idle-gc-") as directory:
        harness.HERE = Path(directory)
        (harness.HERE / "profile_main.go.txt").write_text(changed)
        harness.main()
    # The base harness preserves the original numeric samples. Identify this
    # separately so its second-GC heap profiles cannot be mistaken for round one.
    position = sys.argv.index("--public-output") + 1
    destination = Path(sys.argv[position]) / "allocation-results.json"
    report = json.loads(destination.read_text())
    assert initial_script == harness.sha(Path(__file__))
    report["idle_retention_diagnostic"] = {
        "diagnostic_runner_sha256": initial_script,
        "original_template_sha256": hashlib.sha256(original.encode()).hexdigest(),
        "modified_template_sha256": hashlib.sha256(changed.encode()).hexdigest(),
        "forced_gc_rounds_before_profiles": 2,
        "description": "Two consecutive explicit GCs after CLI execution; no intervening profile writing. First-GC and second-GC counters are retained separately. Allocation/heap profile tables are captured after round two.",
        "production_behavior_changed": False,
    }
    report["limits"].append("This is an idle-cache diagnostic, not a proposal to force GC in production; it cannot establish the cause of production peak RSS.")
    harness.validate_portable(report)
    harness.write_json(destination, report)


if __name__ == "__main__":
    main()
