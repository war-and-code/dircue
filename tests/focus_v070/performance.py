#!/usr/bin/env python3
"""Measure complete default, focus-plan, focus-metrics and full-metrics workflows."""

from __future__ import annotations

import argparse
from datetime import datetime, timezone
import hashlib
import json
import os
from pathlib import Path
import platform
import re
import subprocess
import tempfile
import time
from typing import Any

import common


def inventory(root: Path) -> dict[str, Any]:
    rows: list[list[object]] = []
    total = 0
    for directory, directories, files in os.walk(root, followlinks=False):
        directories[:] = sorted(name for name in directories if name != ".git")
        for name in sorted(files):
            item = Path(directory) / name
            relative = item.relative_to(root).as_posix()
            if item.is_symlink():
                rows.append([relative, "symlink", os.readlink(item)])
            elif item.is_file():
                size = item.stat().st_size
                total += size
                rows.append([relative, "file", size, common.sha256(item)])
            else:
                raise AssertionError(f"unsupported special input: {relative}")
    payload = json.dumps(rows, separators=(",", ":"), ensure_ascii=False).encode()
    return {
        "selection": "all regular-file content hashes and symlink targets, excluding .git",
        "files": sum(row[1] == "file" for row in rows),
        "symlinks": sum(row[1] == "symlink" for row in rows),
        "content_bytes": total,
        "manifest_sha256": hashlib.sha256(payload).hexdigest(),
    }


def measure(command: list[str], timeout: int) -> tuple[bytes, dict[str, Any]]:
    system = platform.system()
    if system not in {"Darwin", "Linux"}:
        raise AssertionError("peak RSS sampling supports macOS or Linux")
    with tempfile.TemporaryDirectory(prefix="dircue-focus-time-") as temporary:
        usage = Path(temporary) / "usage"
        wrapper = ["/usr/bin/time", "-l", "-o", str(usage)] if system == "Darwin" else ["/usr/bin/time", "-f", "%M", "-o", str(usage)]
        started = time.perf_counter()
        child = subprocess.Popen([*wrapper, *command], stdout=subprocess.PIPE, stderr=subprocess.PIPE, start_new_session=True)
        try:
            stdout, stderr = child.communicate(timeout=timeout)
        except subprocess.TimeoutExpired:
            import signal
            os.killpg(child.pid, signal.SIGKILL)
            child.communicate()
            raise AssertionError("benchmark command timed out and its process group was terminated") from None
        seconds = time.perf_counter() - started
        diagnostics = usage.read_text()
    if child.returncode != 0 or stderr:
        raise AssertionError(f"benchmark command failed: exit={child.returncode}, stderr_sha256={hashlib.sha256(stderr).hexdigest()}")
    if system == "Darwin":
        match = re.search(r"(\d+)\s+maximum resident set size", diagnostics)
        if not match:
            raise AssertionError("macOS time output omitted peak RSS")
        rss = int(match.group(1))
    else:
        if not diagnostics.strip().isdigit():
            raise AssertionError("GNU time output omitted peak RSS")
        rss = int(diagnostics.strip()) * 1024
    return stdout, {
        "seconds": seconds,
        "peak_rss_bytes": rss,
        "exit": child.returncode,
        "stdout_bytes": len(stdout),
        "stdout_sha256": hashlib.sha256(stdout).hexdigest(),
        "stderr_sha256": hashlib.sha256(stderr).hexdigest(),
    }


def summary(samples: list[dict[str, Any]]) -> dict[str, Any]:
    return common.sample_summary(samples)


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--baseline", required=True, type=Path)
    parser.add_argument("--baseline-sha256", default=common.BASELINE_SHA256)
    parser.add_argument("--candidate", required=True, type=Path)
    parser.add_argument("--build-receipt", required=True, type=Path)
    parser.add_argument("--root", required=True, type=Path, help="Meaningful local monorepo; repository content is read, never executed")
    parser.add_argument("--project", required=True, help="Root-relative parsed .NET or Python/uv manifest")
    parser.add_argument("--repetitions", type=int, default=5)
    parser.add_argument("--warmups", type=int, default=1)
    parser.add_argument("--workers", type=int, default=8)
    parser.add_argument("--tree-size", type=int, default=200000)
    parser.add_argument("--timeout", type=int, default=900)
    parser.add_argument("--environment-note", required=True)
    parser.add_argument("--output", required=True, type=Path)
    args = parser.parse_args()
    started_at = datetime.now(timezone.utc).isoformat()
    if args.repetitions < 5 or args.warmups < 1:
        raise AssertionError("use at least five repetitions and one warmup")
    root = args.root.resolve()
    baseline = args.baseline.resolve()
    candidate = args.candidate.resolve()
    if not root.is_dir() or not (root / args.project).is_file():
        raise AssertionError("monorepo root or selected project is missing")
    if common.sha256(baseline) != args.baseline_sha256:
        raise AssertionError("unverified 0.6.1 baseline")
    build_receipt, build_receipt_sha = common.load_build_receipt(args.build_receipt, candidate)
    before = inventory(root)
    shared = ["--source", "directory", "--workers", str(args.workers), "--tree-size", str(args.tree_size), "--json", str(root)]
    commands = {
        "baseline_default": [str(baseline), "analyze", "languages", *shared],
        "candidate_default": [str(candidate), "analyze", "languages", *shared],
        "focus_plan": [str(candidate), "analyze", "focus", "--project", args.project, *shared],
        "focus_metrics": [str(candidate), "analyze", "focus", "--project", args.project, "--metrics", *shared],
        "full_metrics": [str(candidate), "analyze", "metrics", *shared],
    }
    expected: dict[str, bytes] = {}
    warmups: dict[str, list[dict[str, Any]]] = {lane: [] for lane in commands}
    for _ in range(args.warmups):
        for lane, command in commands.items():
            payload, sample = measure(command, args.timeout)
            if lane in expected and expected[lane] != payload:
                raise AssertionError(f"nondeterministic warmup output for {lane}")
            expected[lane] = payload
            warmups[lane].append(sample)
    if expected["baseline_default"] != expected["candidate_default"]:
        raise AssertionError("candidate changed the inherited default JSON output")
    focus_plan = json.loads(expected["focus_plan"])
    focus_metrics = json.loads(expected["focus_metrics"])
    if focus_plan["focus"]["status"] not in {"complete", "partial"} or focus_plan["focus"]["primary_project"]["id"] != args.project:
        raise AssertionError("selected monorepo project did not produce a retained focus plan")
    if focus_metrics["focused_metrics"]["primary"]["status"] not in {"complete", "partial"}:
        raise AssertionError("focused metrics were not measured")
    if focus_metrics["focused_metrics"]["scope_id"] != focus_metrics["focus"]["scope"]["id"]:
        raise AssertionError("focused metrics are not bound to their selection scope")

    samples: dict[str, list[dict[str, Any]]] = {lane: [] for lane in commands}
    order: list[list[str]] = []
    names = list(commands)
    for repetition in range(args.repetitions):
        lanes = names if repetition % 2 == 0 else list(reversed(names))
        order.append(lanes)
        for lane in lanes:
            payload, sample = measure(commands[lane], args.timeout)
            if payload != expected[lane]:
                raise AssertionError(f"nondeterministic measured output for {lane}")
            samples[lane].append(sample)
    after = inventory(root)
    if before != after:
        raise AssertionError("monorepo input changed during measurement")
    summaries = {lane: summary(rows) for lane, rows in samples.items()}
    report = {
        "schema": "dircue-focus-v070-performance-1",
        "started_at_utc": started_at,
        "finished_at_utc": datetime.now(timezone.utc).isoformat(),
        "platform": platform.platform(),
        "cpu_count": os.cpu_count(),
        "environment_note": args.environment_note,
        "baseline_sha256": common.sha256(baseline),
        "candidate_sha256": common.sha256(candidate),
        "build_receipt": build_receipt,
        "build_receipt_sha256": build_receipt_sha,
        "harness_sha256": common.script_hashes(),
        "input": {"root_label": root.name, "project": args.project, **before},
        "method": {
            "repetitions": args.repetitions, "warmups": args.warmups, "workers": args.workers,
            "tree_size": args.tree_size, "timeout_seconds": args.timeout,
            "source": "directory", "cache": "warm after explicit per-lane warmup",
            "order": "lane order reverses on alternating repetitions",
            "wall": "complete CLI process including focus declaration prepass, metrics, and JSON serialization",
            "rss": "standalone CLI peak resident bytes from macOS time -l or GNU time",
            "claim_limit": "host-specific observations for this exact executable and content manifest; no universal latency claim",
        },
        "commands": commands,
        "warmups": warmups,
        "order": order,
        "summaries": summaries,
        "observations": {
            "default_candidate_vs_baseline_percent": 100 * (summaries["candidate_default"]["median_seconds"] / summaries["baseline_default"]["median_seconds"] - 1),
            "focus_plan_vs_candidate_default_percent": 100 * (summaries["focus_plan"]["median_seconds"] / summaries["candidate_default"]["median_seconds"] - 1),
            "focus_metrics_vs_full_metrics_percent": 100 * (summaries["focus_metrics"]["median_seconds"] / summaries["full_metrics"]["median_seconds"] - 1),
            "focused_primary_files": focus_plan["focus"]["coverage"]["primary_files"],
            "focused_primary_bytes": focus_plan["focus"]["coverage"]["primary_bytes"],
            "inventory_files": focus_plan["focus"]["coverage"]["inventory_files"],
            "inventory_bytes": focus_plan["focus"]["coverage"]["inventory_bytes"],
            "focus_status": focus_plan["focus"]["status"],
            "focus_omissions": focus_plan["focus"]["omissions"],
        },
        "passed": True,
    }
    common.write_json(args.output, report)
    print(json.dumps({"passed": True, "observations": report["observations"]}, sort_keys=True))


if __name__ == "__main__":
    main()
