#!/usr/bin/env python3
"""Check integrated CLI overhead separately from the isolated cache experiment.

Run only in a quiet measurement window. This compares the cache-only candidate
with the full preparation candidate, both carrying comparison version 0.8.0.
It is not an additional attribution experiment for the packfile optimization.
"""

import argparse
import gzip
import json
import os
from pathlib import Path
import statistics
import sys

ROOT = Path(__file__).resolve().parents[2]
sys.path.insert(0, str(ROOT / "tests/performance/v100_preparation"))
import benchmark
sys.path.insert(0, str(ROOT / "tests/context_v080"))
import common


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    for name in ("candidate", "build-receipt", "output"):
        parser.add_argument("--" + name, type=Path, required=True)
    args = parser.parse_args()
    if args.output.exists():
        raise AssertionError("refusing to replace a timing receipt")
    receipt, receipt_hash = common.load_build_receipt(args.build_receipt, args.candidate)
    pure_receipt = json.loads((benchmark.OUT / "candidate-build.json").read_text())
    controls = {key: os.getenv(key) for key in pure_receipt["runtime_controls"]}
    if controls != pure_receipt["runtime_controls"]:
        raise AssertionError("runtime controls differ from the isolated experiment")
    pure = Path(pure_receipt["binary_path"])
    build_settings = lambda info: {line for line in info.splitlines() if line.startswith("\tbuild\t")}
    if build_settings(receipt["go_build_info"]) != build_settings(pure_receipt["build_info"]):
        raise AssertionError("build settings differ; match the isolated build, including CGO_ENABLED")
    if common.sha256(pure) != pure_receipt["candidate_sha256"]:
        raise AssertionError("cache-only candidate differs")
    candidate_hash = common.sha256(args.candidate)
    cases = json.loads((benchmark.OUT / "scenarios.json").read_text())
    result = {"schema": "dircue-v100-integrated-overhead-1", "started_at": benchmark.utc(),
              "cache_only_sha256": common.sha256(pure), "candidate_sha256": candidate_hash,
              "build_receipt": receipt, "build_receipt_sha256": receipt_hash,
              "harness_sha256": common.sha256(Path(__file__)), "scenarios": [],
              "runtime_controls": controls,
              "method": "Three warmup pairs then 20 measured adjacent AB/BA pairs; all outputs exact, exit 0, empty stderr. Same-host warm-cache evidence, not a production SLA. No concurrent agent builds/tests."}
    for corpus, lane in (("spring-framework", "languages"), ("xml-2gib", "languages"), ("xml-2gib", "optional")):
        case = next(c for c in cases if c["name"] == corpus)
        before = benchmark.inventory(Path(case["path"]), case["source"])
        if before["manifest_sha256"] != case["input"]["manifest_sha256"]:
            raise AssertionError("input corpus differs")
        command = benchmark.command(case, lane)[1:]
        golden = gzip.decompress((benchmark.CACHE / "perf" / f"golden-{corpus}-{lane}.json.gz").read_bytes())
        samples = {"cache_only": [], "integrated": []}
        warmups = {"cache_only": [], "integrated": []}
        for pair in range(23):
            for key, binary in [("cache_only", pure), ("integrated", args.candidate)][::1 if pair % 2 == 0 else -1]:
                payload, sample = benchmark.measure([str(binary.resolve()), *command], benchmark.CACHE / "perf/integration-time.txt")
                if payload != golden:
                    raise AssertionError((corpus, lane, key, "output differs"))
                sample["pair"] = pair - 2
                (warmups if pair < 3 else samples)[key].append(sample)
        if benchmark.inventory(Path(case["path"]), case["source"]) != before:
            raise AssertionError("input corpus changed")
        ratios = [b["seconds"] / a["seconds"] for a, b in zip(samples["cache_only"], samples["integrated"])]
        result["scenarios"].append({"corpus": corpus, "lane": lane, "command_tail": command,
                                    "samples": samples, "warmups": warmups,
                                    "summary": {k: benchmark.summary(v, case["input"]["bytes"]) for k, v in samples.items()},
                                    "paired_integrated_over_cache_only": ratios,
                                    "paired_median_ratio": statistics.median(ratios)})
        print(corpus, lane, statistics.median(ratios), flush=True)
    if common.sha256(pure) != result["cache_only_sha256"] or common.sha256(args.candidate) != candidate_hash:
        raise AssertionError("executable changed")
    result["finished_at"] = benchmark.utc()
    common.write_json(args.output, result)


if __name__ == "__main__":
    main()
