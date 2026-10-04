#!/usr/bin/env python3
"""Compare deterministic map counters and optionally enforce a fixture budget.

Timing, heap and GC observations are deliberately excluded. With ``--budget``
the committed fixture inventory and bytes are verified before binaries run, and
head counters are checked against reviewed ceilings derived from a pinned
baseline. Without it, the command remains a report-only base/head comparison.
"""
from __future__ import annotations

import argparse
import hashlib
import json
import math
import os
import re
import stat
import subprocess
import sys
import tempfile
from pathlib import Path
from typing import Any

COUNTER_KEYS = (
    "files_enumerated",
    "files_content_read",
    "bytes_requested",
    "limit_hits.file_bytes",
    "limit_hits.tree_size",
)
EXPECTED_STATS_KEYS = {
    "schema_version", "kind", "command", "source_mode",
    "deterministic_costs", "measurements",
}
EXPECTED_COST_KEYS = {
    "files_enumerated", "files_content_read", "bytes_requested", "limit_hits",
}
EXPECTED_LIMIT_KEYS = {"file_bytes", "tree_size"}
EXPECTED_MEASUREMENT_KEYS = {"wall_time_ns", "phases", "peak_heap_inuse_bytes", "gc_count"}
EXPECTED_PHASE_KEYS = {"scan_ns", "build_ns"}
TIMEOUT_SECONDS = 60


class GateError(Exception):
    """An input, CLI result, or budget failed validation."""


def fixture_digest(root: Path) -> str:
    """Hash sorted regular-file paths and bytes plus symlink paths and targets."""
    fixture_root = root
    try:
        root = root.resolve(strict=True)
        if not root.is_dir():
            raise GateError(f"fixture is not a directory: {root}")
        entries: list[tuple[str, str, bytes]] = []
        pending = [root]
        while pending:
            directory = pending.pop()
            with os.scandir(directory) as children:
                for child in children:
                    path = Path(child.path)
                    rel = path.relative_to(root).as_posix()
                    mode = child.stat(follow_symlinks=False).st_mode
                    if stat.S_ISLNK(mode):
                        target = os.readlink(path).encode("utf-8", "surrogateescape")
                        entries.append((rel, "symlink", target))
                    elif stat.S_ISREG(mode):
                        entries.append((rel, "file", path.read_bytes()))
                    elif stat.S_ISDIR(mode):
                        pending.append(path)
                    else:
                        raise GateError(f"fixture contains unsupported filesystem object: {rel}")
    except GateError:
        raise
    except OSError as exc:
        raise GateError(f"cannot hash fixture {fixture_root}: {exc}") from exc
    digest = hashlib.sha256()
    for rel, kind, content in sorted(entries):
        path_bytes = rel.encode("utf-8", "surrogateescape")
        digest.update(kind.encode("ascii") + b"\0")
        digest.update(len(path_bytes).to_bytes(8, "big") + path_bytes)
        digest.update(len(content).to_bytes(8, "big") + content)
    return digest.hexdigest()


def validate_inventory(fixtures: list[Path], budget: dict[str, Any]) -> dict[str, dict[str, Any]]:
    expected = budget.get("fixtures")
    if not isinstance(expected, list) or not expected:
        raise GateError("budget fixtures must be a non-empty list")
    by_name: dict[str, dict[str, Any]] = {}
    for item in expected:
        if not isinstance(item, dict) or not isinstance(item.get("id"), str):
            raise GateError("budget contains a fixture entry without a string id")
        name = item["id"]
        if name in by_name:
            raise GateError(f"budget contains duplicate fixture id: {name}")
        by_name[name] = item
    names = [fixture.name for fixture in fixtures]
    if len(names) != len(set(names)):
        raise GateError("fixture arguments contain duplicate directory names")
    expected_names = sorted(by_name)
    actual_names = sorted(names)
    missing = sorted(set(expected_names) - set(actual_names))
    extra = sorted(set(actual_names) - set(expected_names))
    if missing or extra:
        details = []
        if missing:
            details.append("missing fixtures: " + ", ".join(missing))
        if extra:
            details.append("unbudgeted fixtures: " + ", ".join(extra))
        raise GateError("fixture inventory mismatch: " + "; ".join(details))
    for fixture in fixtures:
        expected_hash = by_name[fixture.name].get("input_sha256")
        if not isinstance(expected_hash, str) or re.fullmatch(r"[0-9a-f]{64}", expected_hash) is None:
            raise GateError(f"{fixture.name}: budget is missing a valid input_sha256")
        actual_hash = fixture_digest(fixture)
        if actual_hash != expected_hash:
            raise GateError(
                f"{fixture.name}: fixture input digest changed; expected {expected_hash}, got {actual_hash}"
            )
    return by_name


def validate_stats(doc: Any, binary: str, fixture: Path) -> dict[str, int]:
    label = f"{binary} on {fixture.name}"
    if not isinstance(doc, dict):
        raise GateError(f"{label}: stats document must be a JSON object")
    keys = set(doc)
    missing = sorted(EXPECTED_STATS_KEYS - keys)
    extra = sorted(keys - EXPECTED_STATS_KEYS)
    if missing or extra:
        pieces = []
        if missing:
            pieces.append("missing stats fields: " + ", ".join(missing))
        if extra:
            pieces.append("unknown stats fields: " + ", ".join(extra))
        raise GateError(f"{label}: " + "; ".join(pieces))
    metadata = {
        "schema_version": "1.0.0",
        "kind": "run-stats",
        "command": "map",
        "source_mode": "directory",
    }
    for key, expected in metadata.items():
        if doc.get(key) != expected:
            raise GateError(f"{label}: {key}={doc.get(key)!r}, expected {expected!r}")
    measurements = doc["measurements"]
    if not isinstance(measurements, dict):
        raise GateError(f"{label}: measurements must be a JSON object")
    missing = sorted({"wall_time_ns"} - set(measurements))
    extra = sorted(set(measurements) - EXPECTED_MEASUREMENT_KEYS)
    if missing or extra:
        pieces = []
        if missing:
            pieces.append("missing measurement fields: " + ", ".join(missing))
        if extra:
            pieces.append("unknown measurement fields: " + ", ".join(extra))
        raise GateError(f"{label}: " + "; ".join(pieces))
    for key, value in measurements.items():
        if key == "phases":
            if not isinstance(value, dict):
                raise GateError(f"{label}: measurements.phases must be a JSON object")
            phase_extra = sorted(set(value) - EXPECTED_PHASE_KEYS)
            if phase_extra:
                raise GateError(f"{label}: unknown measurement phases: " + ", ".join(phase_extra))
            for phase, phase_value in value.items():
                if isinstance(phase_value, bool) or not isinstance(phase_value, int) or phase_value < 0:
                    raise GateError(f"{label}: measurements.phases.{phase} must be a non-negative integer")
        elif isinstance(value, bool) or not isinstance(value, int) or value < 0:
            raise GateError(f"{label}: measurements.{key} must be a non-negative integer")
    costs = doc["deterministic_costs"]
    if not isinstance(costs, dict):
        raise GateError(f"{label}: deterministic_costs must be a JSON object")
    missing = sorted(EXPECTED_COST_KEYS - set(costs))
    extra = sorted(set(costs) - EXPECTED_COST_KEYS)
    if missing or extra:
        pieces = []
        if missing:
            pieces.append("missing counters: " + ", ".join(missing))
        if extra:
            pieces.append("unknown counters: " + ", ".join(extra))
        raise GateError(f"{label}: " + "; ".join(pieces))
    limits = costs["limit_hits"]
    if not isinstance(limits, dict):
        raise GateError(f"{label}: limit_hits must be a JSON object")
    missing = sorted(EXPECTED_LIMIT_KEYS - set(limits))
    extra = sorted(set(limits) - EXPECTED_LIMIT_KEYS)
    if missing or extra:
        pieces = []
        if missing:
            pieces.append("missing counters: " + ", ".join(f"limit_hits.{k}" for k in missing))
        if extra:
            pieces.append("unknown counters: " + ", ".join(f"limit_hits.{k}" for k in extra))
        raise GateError(f"{label}: " + "; ".join(pieces))
    flat = {
        "files_enumerated": costs["files_enumerated"],
        "files_content_read": costs["files_content_read"],
        "bytes_requested": costs["bytes_requested"],
        "limit_hits.file_bytes": limits["file_bytes"],
        "limit_hits.tree_size": limits["tree_size"],
    }
    for key, value in flat.items():
        if isinstance(value, bool) or not isinstance(value, int) or value < 0:
            raise GateError(f"{label}: {key} must be a non-negative integer (got {value!r})")
    if flat["files_content_read"] > flat["files_enumerated"]:
        raise GateError(f"{label}: files_content_read exceeds files_enumerated")
    if flat["files_content_read"] == 0 and flat["bytes_requested"] != 0:
        raise GateError(f"{label}: bytes_requested is non-zero when no files were content-read")
    if flat["limit_hits.tree_size"] not in (0, 1):
        raise GateError(f"{label}: limit_hits.tree_size must be 0 or 1")
    return flat


def counters(binary: str, fixture: Path, scratch: Path) -> dict[str, int]:
    stats = scratch / "stats.json"
    stats.unlink(missing_ok=True)
    command = [binary, "map", "--source", "directory", "--json", "--stats-json", str(stats), str(fixture)]
    try:
        result = subprocess.run(command, stdout=subprocess.DEVNULL, stderr=subprocess.PIPE,
                                text=True, timeout=TIMEOUT_SECONDS, check=False)
    except subprocess.TimeoutExpired as exc:
        raise GateError(f"{binary} timed out after {TIMEOUT_SECONDS}s on {fixture}") from exc
    except OSError as exc:
        raise GateError(f"cannot run {binary}: {exc}") from exc
    if result.returncode != 0:
        detail = result.stderr.strip() or f"exit status {result.returncode}"
        raise GateError(f"{binary} failed on {fixture}: {detail}")
    if not stats.is_file():
        raise GateError(f"{binary} succeeded on {fixture} but did not write stats JSON")
    try:
        doc = json.loads(stats.read_text(encoding="utf-8"), object_pairs_hook=unique_json_object)
    except (OSError, UnicodeError, ValueError) as exc:
        raise GateError(f"{binary} wrote invalid stats JSON on {fixture}: {exc}") from exc
    return validate_stats(doc, binary, fixture)


def load_budget(path: Path) -> dict[str, Any]:
    try:
        budget = json.loads(path.read_text(encoding="utf-8"), object_pairs_hook=unique_json_object)
    except (OSError, UnicodeError, ValueError) as exc:
        raise GateError(f"cannot read budget {path}: {exc}") from exc
    if (not isinstance(budget, dict) or type(budget.get("schema_version")) is not int
            or budget.get("schema_version") != 1):
        raise GateError(f"{path}: unsupported budget schema_version")
    policy = budget.get("growth_policy")
    if (not isinstance(policy, dict) or type(policy.get("percent")) is not int
            or policy.get("percent") != 25):
        raise GateError(f"{path}: growth_policy.percent must be 25")
    if (type(policy.get("files_absolute_floor")) is not int
            or type(policy.get("bytes_absolute_floor")) is not int
            or policy.get("files_absolute_floor") != 2
            or policy.get("bytes_absolute_floor") != 256):
        raise GateError(f"{path}: growth policy floors must be files=2, bytes=256")
    if policy.get("limit_hits") != "no-increase":
        raise GateError(f"{path}: limit_hits policy must be no-increase")
    return budget


def ceilings(item: dict[str, Any], policy: dict[str, Any]) -> dict[str, int]:
    baseline = item.get("baseline")
    if not isinstance(baseline, dict) or set(baseline) != set(COUNTER_KEYS):
        raise GateError(f"{item.get('id', '<unknown>')}: baseline must contain exactly the known counters")
    result: dict[str, int] = {}
    for key in COUNTER_KEYS:
        value = baseline[key]
        if isinstance(value, bool) or not isinstance(value, int) or value < 0:
            raise GateError(f"{item['id']}: invalid baseline {key}={value!r}")
        if key.startswith("limit_hits."):
            if value != 0:
                raise GateError(f"{item['id']}: baseline {key} must be zero")
            result[key] = 0
            continue
        floor = policy["bytes_absolute_floor"] if key == "bytes_requested" else policy["files_absolute_floor"]
        result[key] = value + max(floor, math.ceil(value * policy["percent"] / 100))
    return result


def budget_violations(actual: dict[str, int], ceiling: dict[str, int]) -> list[tuple[str, int, int]]:
    return [(key, actual[key], ceiling[key]) for key in COUNTER_KEYS if actual[key] > ceiling[key]]


def unique_json_object(pairs: list[tuple[str, Any]]) -> dict[str, Any]:
    result: dict[str, Any] = {}
    for key, value in pairs:
        if key in result:
            raise ValueError(f"duplicate JSON object key: {key}")
        result[key] = value
    return result


def render(rows: list[tuple[str, str, int, int, int | None]]) -> str:
    lines = [
        "## Deterministic map cost counters (base → head)",
        "",
        "| Fixture | Counter | Base | Head | Change | Budget | Verdict |",
        "| --- | --- | ---: | ---: | ---: | ---: | --- |",
    ]
    for fixture, name, base, head, ceiling in rows:
        change = "=" if base == head else f"{head - base:+d}"
        verdict = "report" if ceiling is None else ("PASS" if head <= ceiling else "FAIL")
        lines.append(f"| `{fixture}` | `{name}` | {base} | {head} | {change} | {'—' if ceiling is None else ceiling} | {verdict} |")
    lines.extend(["", "_Only deterministic logical counters are gated. Wall time, heap and GC observations are excluded._", ""])
    return "\n".join(lines)


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--base", required=True, help="base dircue binary")
    parser.add_argument("--head", required=True, help="head dircue binary")
    parser.add_argument("--budget", type=Path, help="committed deterministic counter budget JSON; enables gate")
    parser.add_argument("fixtures", nargs="+", type=Path)
    args = parser.parse_args(argv)
    try:
        budget = load_budget(args.budget) if args.budget else None
        budget_fixtures = validate_inventory(args.fixtures, budget) if budget is not None else None
        rows: list[tuple[str, str, int, int, int | None]] = []
        failed = False
        with tempfile.TemporaryDirectory() as tmp:
            scratch = Path(tmp)
            for fixture in sorted(args.fixtures, key=lambda path: path.name):
                base = counters(args.base, fixture, scratch)
                head = counters(args.head, fixture, scratch)
                ceiling_by_key = (ceilings(budget_fixtures[fixture.name], budget["growth_policy"])
                                  if budget is not None and budget_fixtures is not None else {})
                if budget is not None and budget_violations(head, ceiling_by_key):
                    failed = True
                for name in COUNTER_KEYS:
                    ceiling = ceiling_by_key.get(name)
                    rows.append((fixture.name, name, base[name], head[name], ceiling))
        if budget is not None and budget_fixtures is not None:
            # Detect fixtures changing after the pre-run digest check while
            # either binary was reading them.
            validate_inventory(args.fixtures, budget)
        report = render(rows)
        sys.stdout.write(report)
        summary = os.environ.get("GITHUB_STEP_SUMMARY")
        if summary:
            with open(summary, "a", encoding="utf-8") as handle:
                handle.write(report)
        return 1 if failed else 0
    except GateError as exc:
        print(f"counter-regression: {exc}", file=sys.stderr)
        return 1


if __name__ == "__main__":
    sys.exit(main())
