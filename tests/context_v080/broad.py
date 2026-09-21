#!/usr/bin/env python3
"""Run the 278 inherited raw CLI cases against the actual 0.7.0 release."""

from __future__ import annotations

import argparse
from collections import Counter
from datetime import datetime, timezone
import importlib.util
import json
from pathlib import Path
import platform
import tempfile
from typing import Any

import common


COMPATIBILITY_V060 = common.ROOT / "tests/compatibility_v060/run.py"
spec = importlib.util.spec_from_file_location("compatibility_v060_for_focus", COMPATIBILITY_V060)
v060 = importlib.util.module_from_spec(spec)
assert spec.loader is not None
spec.loader.exec_module(v060)

EXPECTED_GROUPS = {
    "v020-retained": 118,
    "v030-projects": 55,
    "v030-structure-validation": 6,
    "v030-structure-native": 30,
    "v040-modules": 32,
    "v050-declarations": 37,
}


def source_hashes() -> dict[str, str]:
    paths = [COMPATIBILITY_V060, v060.PREVIOUS, v060.previous.LEGACY_HARNESS, v060.previous.BREADTH / "fixtures.json"]
    return {str(path.relative_to(common.ROOT)): common.sha256(path) for path in paths}


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--baseline", required=True, type=Path)
    parser.add_argument("--baseline-sha256", default=common.BASELINE_SHA256)
    parser.add_argument("--candidate", required=True, type=Path)
    parser.add_argument("--build-receipt", required=True, type=Path)
    parser.add_argument("--worker", required=True, type=Path)
    parser.add_argument("--worker-sha256", required=True)
    parser.add_argument("--output", required=True, type=Path)
    args = parser.parse_args()
    baseline = args.baseline.resolve()
    candidate = args.candidate.resolve()
    worker = args.worker.resolve()
    if common.sha256(baseline) != args.baseline_sha256:
        raise AssertionError("unverified 0.7.0 baseline")
    if common.sha256(worker) != args.worker_sha256:
        raise AssertionError("unverified structural worker")
    build_receipt, build_receipt_sha = common.load_build_receipt(args.build_receipt, candidate)
    report: dict[str, Any] = {
        "schema": "dircue-context-v080-broad-compatibility-1",
        "started_at_utc": datetime.now(timezone.utc).isoformat(),
        "platform": platform.platform(),
        "baseline_release": "v0.7.0",
        "baseline_sha256": common.sha256(baseline),
        "candidate_sha256": common.sha256(candidate),
        "worker_sha256": common.sha256(worker),
        "build_receipt": build_receipt,
        "build_receipt_sha256": build_receipt_sha,
        "harness_sha256": common.script_hashes(),
        "fixture_helper_sha256": source_hashes(),
        "cases": [],
        "method": "The actual pinned 0.7.0 release and source-bound 0.8 candidate run the inherited 278-case matrix on identical temporary paths and environment. Exit code and raw stdout/stderr must match without normalization. Help/version and new focus interfaces are outside this preservation matrix.",
    }
    with tempfile.TemporaryDirectory(prefix="dircue-v080-broad-") as temporary:
        base = Path(temporary)
        legacy = v060.previous.load_legacy()
        env, _flat, inherited = legacy.fixture(base)
        env.pop("DIRCUE_STRUCTURAL_WORKER", None)
        env.update(NO_COLOR="1", LC_ALL="C")
        matrix, languages = v060.previous.cases(base, env, inherited, worker)
        matrix += v060.extra_cases(base, worker)
        matrix += v060.declaration_cases(base, baseline, env)
        groups = Counter(row[1] for row in matrix)
        if dict(groups) != EXPECTED_GROUPS or len(matrix) != 278:
            raise AssertionError(f"inherited matrix changed: {dict(groups)}")
        report["fixtures_sha256"] = v060.previous.fixture_manifest(base)
        report["reference_languages"] = languages
        for case_id, group, cwd, options in matrix:
            old = v060.previous.capture(baseline, options, cwd, env)
            new = v060.previous.capture(candidate, options, cwd, env)
            v060.previous.check_reference(case_id, old, languages)
            report["cases"].append({
                "id": case_id,
                "group": group,
                "args": options,
                "cwd": str(cwd.relative_to(base)),
                "equal": old == new,
                "baseline": common.recorded(old),
                "candidate": common.recorded(new),
            })
        if v060.previous.fixture_manifest(base) != report["fixtures_sha256"]:
            raise AssertionError("inherited fixtures changed during execution")
    report["coverage"] = dict(sorted(Counter(row["group"] for row in report["cases"]).items()))
    report["total"] = len(report["cases"])
    report["exact_matches"] = sum(row["equal"] for row in report["cases"])
    report["passed"] = report["exact_matches"] == report["total"] == 278
    report["finished_at_utc"] = datetime.now(timezone.utc).isoformat()
    if common.sha256(baseline) != report["baseline_sha256"] or common.sha256(candidate) != report["candidate_sha256"]:
        raise AssertionError("executable changed during broad compatibility execution")
    common.write_json(args.output, report)
    print(json.dumps({key: report[key] for key in ("passed", "total", "exact_matches", "coverage")}, sort_keys=True))
    raise SystemExit(0 if report["passed"] else 1)


if __name__ == "__main__":
    main()
