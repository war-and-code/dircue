#!/usr/bin/env python3
"""Run source-bound 0.7 focused-profiling conformance checks."""

from __future__ import annotations

import argparse
from datetime import datetime, timezone
import hashlib
import json
from pathlib import Path
import platform
import tempfile
from typing import Any

import common
import fixture


LEGACY_COMMANDS = [
    ["analyze", "languages", "--source", "directory", "--json", "."],
    ["analyze", "metrics", "--source", "directory", "--files", "--json", "."],
    ["analyze", "declarations", "--source", "directory", "--json", "."],
    ["analyze", "formats", "--source", "directory", "--json", "."],
    ["analyze", "all", "--source", "directory", "--declarations", "--metrics", "--files", "--json", "."],
]


def metric_equivalence(full: dict[str, Any], focused: dict[str, Any], expected_paths: list[str]) -> dict[str, Any]:
    full_metrics = full["metrics"]
    primary = focused["focused_metrics"]["primary"]
    full_files = {item["path"]: item for item in full_metrics["files"]}
    focused_files = {item["path"]: item for item in primary["files"]}
    expected = sorted(expected_paths)
    if sorted(focused_files) != expected:
        raise AssertionError(f"focused metrics paths differ: {sorted(focused_files)} != {expected}")
    missing = [name for name in expected if name not in full_files]
    if missing:
        raise AssertionError(f"full metrics omitted expected paths: {missing}")
    for name in expected:
        if focused_files[name] != full_files[name]:
            raise AssertionError(f"per-file metrics differ for {name}")
    recomputed = common.aggregate_file_metrics(list(focused_files.values()))
    if recomputed != primary["totals"]:
        raise AssertionError("focused totals differ from independently recomputed file totals")
    return {
        "expected_paths": expected,
        "per_file_equal_to_full_subset": True,
        "recomputed_totals": recomputed,
        "full_metrics_sha256": hashlib.sha256(json.dumps(full_metrics, sort_keys=True, separators=(",", ":")).encode()).hexdigest(),
    }


def evaluate_focus(case: dict[str, object], parsed: dict[str, dict[str, Any]]) -> dict[str, Any]:
    project = str(case["project"])
    related = str(case["related"])
    primary = parsed["primary"]["focus"]
    if primary["status"] != case["expected_status"] or primary["scope"]["role"] != "project":
        raise AssertionError("primary focus status or role differs from the authored expectation")
    observed_paths = sorted(item["path"] for item in primary["primary"])
    if observed_paths != sorted(case["primary"]):
        raise AssertionError(f"planner population differs: {observed_paths}")
    contexts = {item["path"] for item in primary["context"]}
    if not set(case["required_context"]).issubset(contexts):
        raise AssertionError(f"missing qualified contexts: {set(case['required_context']) - contexts}")
    if "shared/Linked.cs" in observed_paths:
        raise AssertionError("declared linked input was inferred as contained source")

    equivalence = metric_equivalence(parsed["full"], parsed["primary"], list(case["primary"]))
    if case["expected_status"] == "partial":
        skips = parsed["primary"]["focused_metrics"]["primary"]["skipped"]
        if "focus_scope_partial" not in {item["reason"] for item in skips}:
            raise AssertionError("partial selection was not carried into focused metrics")

    related_report = parsed["related"]
    related_focus = related_report["focus"]
    if related_focus["scope"]["related_projects"] != [related]:
        raise AssertionError("related project scope is not explicit and canonical")
    related_population = related_focus["related"]
    if len(related_population) != 1:
        raise AssertionError("related project population missing")
    related_paths = sorted(item["path"] for item in related_population[0]["files"])
    if related_paths != sorted(case["related_files"]):
        raise AssertionError(f"related population differs: {related_paths}")
    focused_related = related_report["focused_metrics"]["related"]
    if len(focused_related) != 1 or focused_related[0]["project"] != related:
        raise AssertionError("related metrics denominator missing")
    full_files = {item["path"]: item for item in parsed["full"]["metrics"]["files"]}
    actual_related = {item["path"]: item for item in focused_related[0]["metrics"]["files"]}
    if actual_related != {name: full_files[name] for name in sorted(case["related_files"])}:
        raise AssertionError("related per-file metrics differ from full-root subset")

    affected = parsed["affected"]["focus"]
    if affected["scope"]["role"] != "affected-by" or affected.get("primary_project") is not None:
        raise AssertionError("affected-by query incorrectly has a primary project")
    projects = [item["project_id"] for item in affected["affected_projects"]["projects"]]
    if project not in projects:
        raise AssertionError("affected-by query omitted the authored project")

    facts = {
        "project": project,
        "primary_population": observed_paths,
        "contexts": sorted(contexts),
        "related_project": related,
        "related_population": related_paths,
        "affected_by": case["affected_by"],
        "affected_projects": projects,
        "metric_equivalence": equivalence,
        "scope_ids": {key: value["focus"]["scope"]["id"] for key, value in parsed.items() if key != "full"},
    }
    return facts


def check_focus(case: dict[str, object], candidate: Path) -> tuple[list[dict[str, Any]], dict[str, Any]]:
    root = case["root"]
    assert isinstance(root, Path)
    project = str(case["project"])
    related = str(case["related"])
    base = ["--source", "directory", "--json", str(root)]
    commands = {
        "full": ["analyze", "metrics", "--files", *base],
        "primary": ["analyze", "focus", "--project", project, "--metrics", "--files", *base],
        "related": ["analyze", "focus", "--project", project, "--related-project", related, "--metrics", "--files", *base],
        "affected": ["analyze", "focus", "--affected-by", str(case["affected_by"]), *base],
    }
    captures: list[dict[str, Any]] = []
    parsed: dict[str, dict[str, Any]] = {}
    for name, args in commands.items():
        raw = common.capture(candidate, args, root)
        parsed[name] = common.parse_json(raw, name)
        captures.append({"id": name, "args": args[:-1] + ["$FIXTURE"], "capture": common.recorded(raw)})
    return captures, evaluate_focus(case, parsed)


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--baseline", required=True, type=Path)
    parser.add_argument("--baseline-sha256", default=common.BASELINE_SHA256)
    parser.add_argument("--candidate", required=True, type=Path)
    parser.add_argument("--build-receipt", required=True, type=Path)
    parser.add_argument("--worker", type=Path, help="Optional verified structural worker for inherited hotspot coverage")
    parser.add_argument("--worker-sha256")
    parser.add_argument("--output", required=True, type=Path)
    args = parser.parse_args()

    baseline = args.baseline.resolve()
    candidate = args.candidate.resolve()
    if common.sha256(baseline) != args.baseline_sha256:
        raise AssertionError("unverified 0.6.1 baseline")
    build_receipt, build_receipt_sha = common.load_build_receipt(args.build_receipt, candidate)
    if args.worker:
        worker = args.worker.resolve()
        if not args.worker_sha256 or common.sha256(worker) != args.worker_sha256:
            raise AssertionError("unverified structural worker")
    else:
        worker = None

    report: dict[str, Any] = {
        "schema": "dircue-focus-v070-conformance-1",
        "started_at_utc": datetime.now(timezone.utc).isoformat(),
        "platform": platform.platform(),
        "baseline_release": "v0.6.1",
        "baseline_sha256": common.sha256(baseline),
        "candidate_sha256": common.sha256(candidate),
        "build_receipt": build_receipt,
        "build_receipt_sha256": build_receipt_sha,
        "harness_sha256": common.script_hashes(),
        "legacy_cases": [],
        "focus_cases": [],
        "untested": [] if worker else ["native structural hotspot compatibility: no verified worker supplied"],
        "method": "Released 0.6.1 and candidate run on identical authored bytes for inherited commands. Focus populations come from authored expectations; focused scc files are compared to the same paths in a full-root candidate report.",
    }
    if worker:
        report["worker_sha256"] = common.sha256(worker)

    with tempfile.TemporaryDirectory(prefix="dircue-focus-v070-") as temporary:
        base = Path(temporary)
        cases = fixture.prepare(base)
        report["fixture_manifest"] = fixture.manifest(base)
        report["fixture_expectations"] = {
            name: {key: value for key, value in case.items() if key != "root"}
            for name, case in cases.items()
        }
        for fixture_name, case in cases.items():
            root = case["root"]
            assert isinstance(root, Path)
            commands = list(LEGACY_COMMANDS)
            if worker:
                commands.append(["analyze", "structure", "--source", "directory", "--hotspots", "--functions", "--files", "--structural-worker", str(worker), "--json", "."])
            for index, command in enumerate(commands, 1):
                old = common.capture(baseline, command, root)
                new = common.capture(candidate, command, root)
                report["legacy_cases"].append({
                    "id": f"{fixture_name}-{index:02}", "fixture": fixture_name, "args": command,
                    "equal": old == new, "baseline": common.recorded(old),
                    "candidate": {"identical_to_baseline": True} if old == new else common.recorded(new),
                })
            captures, facts = check_focus(case, candidate)
            report["focus_cases"].append({"id": fixture_name, "captures": captures, "facts": facts, "passed": True})
        if fixture.manifest(base) != report["fixture_manifest"]:
            raise AssertionError("fixture bytes changed during conformance execution")

    report["legacy_exact_matches"] = sum(row["equal"] for row in report["legacy_cases"])
    report["legacy_total"] = len(report["legacy_cases"])
    report["passed"] = report["legacy_exact_matches"] == report["legacy_total"] and all(row["passed"] for row in report["focus_cases"])
    report["finished_at_utc"] = datetime.now(timezone.utc).isoformat()
    if common.sha256(baseline) != report["baseline_sha256"] or common.sha256(candidate) != report["candidate_sha256"]:
        raise AssertionError("executable changed during conformance execution")
    common.write_json(args.output, report)
    print(json.dumps({key: report[key] for key in ("passed", "legacy_total", "legacy_exact_matches", "untested")}))
    raise SystemExit(0 if report["passed"] else 1)


if __name__ == "__main__":
    main()
