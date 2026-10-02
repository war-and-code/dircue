#!/usr/bin/env python3
"""Replay the inherited CLI matrix against an explicitly pinned release binary."""

import argparse
from collections import Counter
from datetime import datetime, timezone
import importlib.util
import json
import os
from pathlib import Path
import platform
import tempfile

ROOT = Path(__file__).resolve().parents[2]
HELPER = ROOT / "tests/compatibility_v060/run.py"
spec = importlib.util.spec_from_file_location("minor_compatibility_helpers", HELPER)
helpers = importlib.util.module_from_spec(spec)
spec.loader.exec_module(helpers)
previous = helpers.previous
INPUT_FILES = (HELPER, helpers.PREVIOUS, previous.LEGACY_HARNESS,
               previous.BREADTH / "fixtures.json", Path(__file__).resolve(),
               ROOT / "scripts/declarations_release_smoke.py",
               ROOT / "tests/packageevidence/fixtures/syft-1.52.0.json")


def input_hashes():
    return {str(p.relative_to(ROOT)): previous.sha(p) for p in INPUT_FILES}


def check_baseline(binary, expected_sha256, expected_version):
    if previous.sha(binary) != expected_sha256:
        raise ValueError("baseline differs from its pinned SHA-256")
    result = previous.capture(binary, ["--version"], ROOT, os.environ.copy())
    if result != (0, f"dircue {expected_version}\n".encode(), b""):
        raise ValueError("baseline does not report the specified release version")


def run(baseline, candidate, worker):
    report = {
        "schema": "dircue-minor-compatibility-1",
        "started_at_utc": datetime.now(timezone.utc).isoformat(),
        "platform": platform.platform(),
        "baseline_sha256": previous.sha(baseline),
        "candidate_sha256": previous.sha(candidate),
        "worker_sha256": previous.sha(worker) if worker else None,
        "helper_and_external_input_sha256": input_hashes(),
        "untested": [] if worker else ["optional native structural-worker execution"],
        "method": "Exact exit status, stdout and stderr; no normalization. Help, version, newer map/focus/context interfaces, and newly supported evidence require separate checks.",
        "cases": [],
    }
    with tempfile.TemporaryDirectory(prefix="dircue-minor-compatibility-") as temporary:
        base = Path(temporary)
        legacy = previous.load_legacy()
        env, _flat, inherited = legacy.fixture(base)
        env.pop("DIRCUE_STRUCTURAL_WORKER", None)
        env.update(NO_COLOR="1", LC_ALL="C")
        matrix, languages = previous.cases(base, env, inherited, worker)
        matrix += helpers.extra_cases(base, worker)
        matrix += helpers.declaration_cases(base, baseline, env)
        expected = 278 if worker else 245
        if len(matrix) != expected or len({row[0] for row in matrix}) != expected:
            raise AssertionError("inherited case count changed; review the matrix")
        before = previous.fixture_manifest(base)
        for case_id, group, cwd, options in matrix:
            old = previous.capture(baseline, options, cwd, env)
            new = previous.capture(candidate, options, cwd, env)
            previous.check_reference(case_id, old, languages)
            report["cases"].append({"id": case_id, "group": group, "args": options,
                                    "cwd": str(cwd.relative_to(base)), "equal": old == new,
                                    "baseline": previous.recorded(old), "candidate": previous.recorded(new)})
        if previous.fixture_manifest(base) != before:
            raise AssertionError("a command changed the fixture files")
        report["fixtures_sha256"] = before
    if previous.sha(baseline) != report["baseline_sha256"] or previous.sha(candidate) != report["candidate_sha256"]:
        raise AssertionError("an executable changed during verification")
    if input_hashes() != report["helper_and_external_input_sha256"]:
        raise AssertionError("a helper or external input changed during verification")
    if worker and previous.sha(worker) != report["worker_sha256"]:
        raise AssertionError("the worker changed during verification")
    report["total"] = len(report["cases"])
    report["exact_matches"] = sum(row["equal"] for row in report["cases"])
    report["groups"] = dict(Counter(row["group"] for row in report["cases"]))
    report["passed"] = report["total"] == report["exact_matches"]
    return report


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--baseline", type=Path, required=True)
    parser.add_argument("--baseline-sha256", required=True)
    parser.add_argument("--baseline-version", required=True)
    parser.add_argument("--candidate", type=Path, required=True)
    parser.add_argument("--worker", type=Path)
    parser.add_argument("--worker-sha256")
    parser.add_argument("--require-worker", action="store_true")
    parser.add_argument("--output", type=Path, required=True)
    args = parser.parse_args()
    if args.output.exists():
        parser.error("refusing to overwrite a receipt")
    if bool(args.worker) != bool(args.worker_sha256) or (args.require_worker and not args.worker):
        parser.error("worker execution requires a worker and its pinned SHA-256")
    baseline, candidate = args.baseline.resolve(), args.candidate.resolve()
    worker = args.worker.resolve() if args.worker else None
    check_baseline(baseline, args.baseline_sha256, args.baseline_version)
    if worker and previous.sha(worker) != args.worker_sha256:
        parser.error("worker differs from its pinned SHA-256")
    report = run(baseline, candidate, worker)
    report["baseline_release"] = args.baseline_version
    args.output.parent.mkdir(parents=True, exist_ok=True)
    with args.output.open("x") as output:
        json.dump(report, output, indent=2)
        output.write("\n")
    print(f"{report['exact_matches']}/{report['total']} exact CLI matches; receipt: {args.output}")
    raise SystemExit(0 if report["passed"] else 1)


if __name__ == "__main__":
    main()
