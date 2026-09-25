#!/usr/bin/env python3
"""Compare deterministic map cost counters between two dircue binaries.

Each fixture directory is mapped with --stats-json by the base and head
binaries. Only the deterministic_costs section is compared; wall time, heap
and GC counts vary between runs and are reported for context only. The
report is written as Markdown to stdout (and appended to GITHUB_STEP_SUMMARY
when set). The exit status is 0 unless a binary fails, so the comparison is
report-only.
"""
from __future__ import annotations

import argparse
import json
import os
import subprocess
import sys
import tempfile
from pathlib import Path


def flatten(prefix: str, value, out: dict) -> None:
    if isinstance(value, dict):
        for key in sorted(value):
            flatten(f"{prefix}.{key}" if prefix else key, value[key], out)
    else:
        out[prefix] = value


def counters(binary: str, fixture: Path, scratch: Path) -> dict | None:
    stats = scratch / "stats.json"
    if stats.exists():
        stats.unlink()
    result = subprocess.run(
        [binary, "map", "--source", "directory", "--json", "--stats-json", str(stats), str(fixture)],
        stdout=subprocess.DEVNULL,
        stderr=subprocess.PIPE,
        text=True,
    )
    if result.returncode != 0:
        if "unknown flag: --stats-json" in result.stderr:
            return None
        raise SystemExit(f"{binary} failed on {fixture}: {result.stderr.strip()}")
    flat: dict = {}
    flatten("", json.loads(stats.read_text())["deterministic_costs"], flat)
    return flat


def render(rows: list[tuple[str, str, object, object]]) -> str:
    lines = [
        "## Deterministic map cost counters (base → head)",
        "",
        "| Fixture | Counter | Base | Head | Change |",
        "| --- | --- | ---: | ---: | ---: |",
    ]
    for fixture, name, base, head in rows:
        if base is None:
            change = "new"
        elif base == head:
            change = "="
        else:
            change = f"{head - base:+d}" if isinstance(base, int) and isinstance(head, int) else "changed"
        lines.append(f"| `{fixture}` | `{name}` | {'—' if base is None else base} | {head} | {change} |")
    lines.append("")
    lines.append("_Counters are identical for the same input and settings at any worker count. Timings are not compared._")
    return "\n".join(lines) + "\n"


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--base", required=True, help="base dircue binary")
    parser.add_argument("--head", required=True, help="head dircue binary")
    parser.add_argument("fixtures", nargs="+", type=Path)
    args = parser.parse_args()
    rows = []
    with tempfile.TemporaryDirectory() as tmp:
        scratch = Path(tmp)
        for fixture in args.fixtures:
            head = counters(args.head, fixture, scratch)
            if head is None:
                raise SystemExit(f"head binary does not support --stats-json")
            base = counters(args.base, fixture, scratch) or {}
            for name in sorted(head):
                rows.append((fixture.name, name, base.get(name), head[name]))
    report = render(rows)
    sys.stdout.write(report)
    summary = os.environ.get("GITHUB_STEP_SUMMARY")
    if summary:
        with open(summary, "a", encoding="utf-8") as handle:
            handle.write(report)
    return 0


if __name__ == "__main__":
    sys.exit(main())
