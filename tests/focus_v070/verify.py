#!/usr/bin/env python3
"""Independently verify a focus conformance receipt's captures and claims."""

from __future__ import annotations

import argparse
from collections import Counter
import importlib.util
import json
from pathlib import Path

import common
import fixture
import run as focus_run


def require_exact_ids(rows: list[dict], expected: set[str], label: str) -> None:
    observed = [row["id"] for row in rows]
    if len(observed) != len(expected) or set(observed) != expected:
        raise AssertionError(f"{label} case matrix differs")


def verify_build_binding(report: dict) -> None:
    build = report["build_receipt"]
    source = build.get("source_at_build")
    if (
        report["build_receipt_sha256"] != common.canonical_json_sha256(build)
        or build.get("schema") != "dircue-focus-v070-build-1"
        or build.get("candidate_sha256") != report["candidate_sha256"]
        or not isinstance(build.get("files"), dict)
        or not build["files"]
        or not isinstance(source, dict)
        or not {"commit", "head_tree", "status_sha256", "diff_sha256", "dirty"}.issubset(source)
    ):
        raise AssertionError("candidate is not bound to build receipt")


def verify_conformance(report: dict) -> dict:
    if report["schema"] != "dircue-focus-v070-conformance-1":
        raise AssertionError("unexpected receipt schema")
    if report["baseline_release"] != "v0.6.1" or report["baseline_sha256"] != common.BASELINE_SHA256:
        raise AssertionError("unexpected baseline identity")
    verify_build_binding(report)
    if report["harness_sha256"] != common.script_hashes():
        raise AssertionError("receipt was produced by different harness source")
    expected_specs = fixture.case_specs()
    if report["fixture_expectations"] != expected_specs:
        raise AssertionError("fixture expectation matrix differs from authored source")
    per_fixture = 6 if report.get("worker_sha256") else 5
    expected_legacy_ids = {f"{name}-{index:02}" for name in expected_specs for index in range(1, per_fixture + 1)}
    require_exact_ids(report["legacy_cases"], expected_legacy_ids, "selected legacy")
    exact = 0
    identifiers = set()
    for row in report["legacy_cases"]:
        if row["id"] in identifiers:
            raise AssertionError("duplicate legacy case ID")
        identifiers.add(row["id"])
        baseline = common.decode(row["baseline"])
        candidate = baseline if row["candidate"] == {"identical_to_baseline": True} else common.decode(row["candidate"])
        equal = baseline == candidate
        if row["equal"] is not equal:
            raise AssertionError("legacy equality claim differs from raw captures")
        exact += equal
    require_exact_ids(report["focus_cases"], set(expected_specs), "focus")
    for case in report["focus_cases"]:
        if not case["passed"]:
            raise AssertionError("focus case was not marked passed")
        require_exact_ids(case["captures"], {"full", "primary", "related", "affected"}, "focus capture")
        parsed = {}
        for row in case["captures"]:
            raw = common.decode(row["capture"])
            if raw[0] != 0 or raw[2]:
                raise AssertionError("focus capture did not succeed cleanly")
            parsed[row["id"]] = json.loads(raw[1])
        expected = expected_specs[case["id"]]
        recomputed = focus_run.evaluate_focus(expected, parsed)
        if recomputed != case["facts"]:
            raise AssertionError("focus facts differ from raw captured reports")
        if case["facts"]["primary_population"] != sorted(expected["primary"]):
            raise AssertionError("primary population is not the authored expectation")
        if case["facts"]["related_population"] != sorted(expected["related_files"]):
            raise AssertionError("related population is not the authored expectation")
        if case["facts"]["metric_equivalence"]["expected_paths"] != sorted(expected["primary"]):
            raise AssertionError("metric equivalence used a derived path set")
    passed = exact == len(report["legacy_cases"]) and len(report["focus_cases"]) == 2
    if report["legacy_total"] != len(report["legacy_cases"]) or report["legacy_exact_matches"] != exact or report["passed"] is not passed:
        raise AssertionError("receipt summary differs from verified evidence")
    return {"passed": passed, "legacy_total": len(report["legacy_cases"]), "legacy_exact_matches": exact, "focus_cases": len(report["focus_cases"])}


def verify_performance(report: dict) -> dict:
    if report["schema"] != "dircue-focus-v070-performance-1":
        raise AssertionError("unexpected performance receipt schema")
    if report["baseline_sha256"] != common.BASELINE_SHA256:
        raise AssertionError("unexpected baseline identity")
    verify_build_binding(report)
    if report["harness_sha256"] != common.script_hashes():
        raise AssertionError("receipt was produced by different harness source")
    method = report["method"]
    expected_lanes = {"baseline_default", "candidate_default", "focus_plan", "focus_metrics", "full_metrics"}
    if set(report["summaries"]) != expected_lanes or set(report["commands"]) != expected_lanes:
        raise AssertionError("performance lanes differ")
    output_hashes = {}
    for lane in expected_lanes:
        summary = report["summaries"][lane]
        samples = summary["samples"]
        if len(samples) != method["repetitions"] or summary != common.sample_summary(samples):
            raise AssertionError(f"sample summary differs for {lane}")
        hashes = {row["stdout_sha256"] for row in samples}
        if len(hashes) != 1 or any(row["exit"] != 0 or row["stderr_sha256"] != "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855" for row in samples):
            raise AssertionError(f"unstable or unsuccessful samples for {lane}")
        output_hashes[lane] = next(iter(hashes))
    if output_hashes["baseline_default"] != output_hashes["candidate_default"]:
        raise AssertionError("default outputs differ")
    observed = report["observations"]
    recalculated = {
        "default_candidate_vs_baseline_percent": 100 * (report["summaries"]["candidate_default"]["median_seconds"] / report["summaries"]["baseline_default"]["median_seconds"] - 1),
        "focus_plan_vs_candidate_default_percent": 100 * (report["summaries"]["focus_plan"]["median_seconds"] / report["summaries"]["candidate_default"]["median_seconds"] - 1),
        "focus_metrics_vs_full_metrics_percent": 100 * (report["summaries"]["focus_metrics"]["median_seconds"] / report["summaries"]["full_metrics"]["median_seconds"] - 1),
    }
    for key, value in recalculated.items():
        if observed[key] != value:
            raise AssertionError(f"derived observation differs: {key}")
    if not report["passed"] or report["input"]["files"] <= 0 or report["input"]["content_bytes"] <= 0:
        raise AssertionError("performance receipt lacks successful, nonempty input evidence")
    return {"passed": True, "samples_per_lane": method["repetitions"], "input_files": report["input"]["files"], "observations": recalculated}


def verify_broad(report: dict) -> dict:
    expected = {"v020-retained": 118, "v030-projects": 55, "v030-structure-validation": 6, "v030-structure-native": 30, "v040-modules": 32, "v050-declarations": 37}
    if report["schema"] != "dircue-focus-v070-broad-compatibility-1" or report["baseline_release"] != "v0.6.1" or report["baseline_sha256"] != common.BASELINE_SHA256:
        raise AssertionError("unexpected broad compatibility identity")
    verify_build_binding(report)
    if report["harness_sha256"] != common.script_hashes():
        raise AssertionError("receipt was produced by different harness source")
    compatibility = common.ROOT / "tests/compatibility_v060/run.py"
    module_spec = importlib.util.spec_from_file_location("compatibility_v060_verify", compatibility)
    module = importlib.util.module_from_spec(module_spec)
    assert module_spec.loader is not None
    module_spec.loader.exec_module(module)
    helper_paths = [compatibility, module.PREVIOUS, module.previous.LEGACY_HARNESS, module.previous.BREADTH / "fixtures.json"]
    expected_helpers = {str(path.relative_to(common.ROOT)): common.sha256(path) for path in helper_paths}
    if report["fixture_helper_sha256"] != expected_helpers:
        raise AssertionError("broad fixture helper source differs")
    expected_languages = json.loads((module.previous.BREADTH / "fixtures.json").read_text())
    if report.get("reference_languages") != expected_languages:
        raise AssertionError("broad structural reference expectations differ")
    identifiers = set()
    exact = 0
    for row in report["cases"]:
        if row["id"] in identifiers:
            raise AssertionError("duplicate broad case ID")
        identifiers.add(row["id"])
        baseline = common.decode(row["baseline"])
        module.previous.check_reference(row["id"], baseline, expected_languages)
        candidate = baseline if row["candidate"] == {"identical_to_baseline": True} else common.decode(row["candidate"])
        equal = baseline == candidate
        if row["equal"] is not equal:
            raise AssertionError("broad equality claim differs from raw captures")
        exact += equal
    coverage = dict(Counter(row["group"] for row in report["cases"]))
    passed = coverage == expected and len(report["cases"]) == exact == 278
    if report["coverage"] != dict(sorted(expected.items())) or report["total"] != 278 or report["exact_matches"] != exact or report["passed"] is not passed:
        raise AssertionError("broad receipt summary differs")
    return {"passed": passed, "total": 278, "exact_matches": exact, "coverage": coverage}


def verify(report: dict) -> dict:
    if report.get("schema") == "dircue-focus-v070-conformance-1":
        return verify_conformance(report)
    if report.get("schema") == "dircue-focus-v070-performance-1":
        return verify_performance(report)
    if report.get("schema") == "dircue-focus-v070-broad-compatibility-1":
        return verify_broad(report)
    raise AssertionError("unsupported receipt schema")


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("receipt", type=Path)
    args = parser.parse_args()
    result = verify(common.read_json(args.receipt))
    print(json.dumps(result))
    raise SystemExit(0 if result["passed"] else 1)


if __name__ == "__main__":
    main()
