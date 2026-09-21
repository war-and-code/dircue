#!/usr/bin/env python3
"""Measure honest 0.7/0.8 context workflows without asserting unlike outcomes are equivalent."""

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
from typing import Any, Callable

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
    encoded = json.dumps(rows, separators=(",", ":"), ensure_ascii=False).encode()
    return {"files": sum(row[1] == "file" for row in rows), "symlinks": sum(row[1] == "symlink" for row in rows),
            "content_bytes": total, "manifest_sha256": hashlib.sha256(encoded).hexdigest(),
            "selection": "all regular-file content hashes and symlink targets, excluding .git"}


def authored_fixtures(parent: Path) -> dict[str, Path]:
    small = parent / "small"
    xml = parent / "large-xml"
    for root in (small, xml):
        root.mkdir()
    files = {
        small / "global.json": b'{"sdk":{"version":"8.0.300"}}\n',
        small / "app/App.csproj": b'<Project Sdk="Microsoft.NET.Sdk"><PropertyGroup><TargetFramework>net8.0</TargetFramework></PropertyGroup></Project>\n',
        small / "app/Program.cs": b'class Program { static void Main() {} }\n',
        small / "py/pyproject.toml": b'[project]\nname="fixture"\nversion="1"\nrequires-python=">=3.12"\n',
        small / "py/main.py": b'print("fixture")\n',
        xml / "tiny/Tiny.csproj": b'<Project Sdk="Microsoft.NET.Sdk"><PropertyGroup><TargetFramework>net8.0</TargetFramework></PropertyGroup></Project>\n',
        xml / "tiny/Tiny.cs": b'class Tiny {}\n',
    }
    for path, content in files.items():
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_bytes(content)
    # Materialized, valid XML: 2,048 files x exactly 1 MiB = 2 GiB. Files are
    # written normally (not truncated/sparse), and creation is outside timing.
    prefix, suffix = b'<root>', b'</root>\n'
    xml_payload = prefix + (b'x' * ((1 << 20) - len(prefix) - len(suffix))) + suffix
    if len(xml_payload) != 1 << 20:
        raise AssertionError("authored XML payload size differs")
    xml_dir = xml / "documents"
    xml_dir.mkdir()
    for index in range(2048):
        (xml_dir / f"document-{index:04d}.xml").write_bytes(xml_payload)
    return {"small": small, "large_xml": xml}


def timed(command: list[str], timeout: int, cwd: Path) -> tuple[bytes, dict[str, Any]]:
    system = platform.system()
    if system not in {"Darwin", "Linux"}:
        raise AssertionError("peak RSS sampling supports macOS or Linux")
    with tempfile.TemporaryDirectory(prefix="dircue-context-time-") as temporary:
        usage = Path(temporary) / "usage"
        wrapper = ["/usr/bin/time", "-l", "-o", str(usage)] if system == "Darwin" else ["/usr/bin/time", "-f", "%M", "-o", str(usage)]
        started = time.perf_counter()
        child = subprocess.Popen([*wrapper, *command], cwd=cwd, stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                                 env={"PATH": "/usr/bin:/bin:/usr/sbin:/sbin", "NO_COLOR": "1", "LC_ALL": "C"},
                                 start_new_session=True)
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
    return stdout, {"seconds": seconds, "peak_rss_bytes": rss, "exit": child.returncode,
                    "stdout_bytes": len(stdout), "stdout_sha256": hashlib.sha256(stdout).hexdigest(),
                    "stderr_sha256": hashlib.sha256(stderr).hexdigest()}


def single(command: list[str], timeout: int, cwd: Path) -> tuple[bytes, dict[str, Any]]:
    return timed(command, timeout, cwd)


def staged(candidate: Path, root: Path, shared: list[str], timeout: int, cwd: Path) -> tuple[bytes, dict[str, Any]]:
    with tempfile.TemporaryDirectory(prefix="dircue-context-plan-") as temporary:
        saved = Path(temporary) / "discovery.json"
        commands = [
            [str(candidate), "analyze", "discovery", *shared, str(root)],
            [str(candidate), "plan", str(saved), "--module", "declarations", "--json"],
            [str(candidate), "analyze", "declarations", *shared, str(root)],
        ]
        outputs, stages = [], []
        for index, command in enumerate(commands):
            payload, sample = timed(command, timeout, cwd)
            outputs.append(payload)
            stages.append(sample)
            if index == 0:
                saved.write_bytes(payload)
        joined = b"".join(len(value).to_bytes(8, "big") + value for value in outputs)
        return joined, {"seconds": sum(row["seconds"] for row in stages),
                        "peak_rss_bytes": max(row["peak_rss_bytes"] for row in stages),
                        "exit": 0, "stdout_bytes": sum(row["stdout_bytes"] for row in stages),
                        "stdout_sha256": hashlib.sha256(joined).hexdigest(),
                        "stderr_sha256": hashlib.sha256(b"").hexdigest(), "stages": stages}


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--baseline", type=Path, default=common.ROOT / ".cache/release070/installed/dircue")
    parser.add_argument("--baseline-sha256", default=common.BASELINE_SHA256)
    parser.add_argument("--candidate", type=Path, required=True)
    parser.add_argument("--build-receipt", type=Path, required=True)
    parser.add_argument("--corpus", type=Path, default=common.ROOT / ".cache/corpus/aspnetcore")
    parser.add_argument("--repetitions", type=int, default=5)
    parser.add_argument("--warmups", type=int, default=1)
    parser.add_argument("--workers", type=int, default=8)
    parser.add_argument("--tree-size", type=int, default=200000)
    parser.add_argument("--timeout", type=int, default=900)
    parser.add_argument("--environment-note", required=True)
    parser.add_argument("--output", type=Path, required=True)
    args = parser.parse_args()
    if args.repetitions < 5 or args.warmups < 1:
        raise AssertionError("use at least five repetitions and one warmup")
    baseline, candidate, corpus = args.baseline.resolve(), args.candidate.resolve(), args.corpus.resolve()
    if common.sha256(baseline) != args.baseline_sha256:
        raise AssertionError("unverified 0.7.0 baseline")
    if not corpus.is_dir():
        raise AssertionError("ASP.NET Core corpus is missing")
    build_receipt, build_receipt_sha = common.load_build_receipt(args.build_receipt, candidate)
    started = datetime.now(timezone.utc).isoformat()
    with tempfile.TemporaryDirectory(prefix="dircue-context-performance-") as temporary:
        fixtures = authored_fixtures(Path(temporary))
        inputs = {"aspnetcore": inventory(corpus), **{name: inventory(root) for name, root in fixtures.items()}}
        shared = ["--source", "directory", "--workers", str(args.workers), "--tree-size", str(args.tree_size), "--json"]
        command_receipts: dict[str, Any] = {
            "small_staged": [[str(candidate), "analyze", "discovery", *shared, str(fixtures["small"])],
                             [str(candidate), "plan", "{saved_report}", "--module", "declarations", "--json"],
                             [str(candidate), "analyze", "declarations", *shared, str(fixtures["small"])]],
            "xml_staged": [[str(candidate), "analyze", "discovery", *shared, str(fixtures["large_xml"])],
                           [str(candidate), "plan", "{saved_report}", "--module", "declarations", "--json"],
                           [str(candidate), "analyze", "declarations", *shared, str(fixtures["large_xml"])]],
        }
        lane_commands = {
            "small_full": [str(candidate), "analyze", "all", "--declarations", "--metrics", *shared, str(fixtures["small"])],
            "xml_full": [str(candidate), "analyze", "all", "--declarations", "--metrics", *shared, str(fixtures["large_xml"])],
            "baseline_corpus_languages": [str(baseline), "analyze", "languages", *shared, str(corpus)],
            "candidate_corpus_languages": [str(candidate), "analyze", "languages", *shared, str(corpus)],
            "baseline_xml_languages": [str(baseline), "analyze", "languages", *shared, str(fixtures["large_xml"])],
            "candidate_xml_languages": [str(candidate), "analyze", "languages", *shared, str(fixtures["large_xml"])],
            "candidate_small_declarations": [str(candidate), "analyze", "declarations", *shared, str(fixtures["small"])],
            "candidate_small_environments": [str(candidate), "analyze", "environments", *shared, str(fixtures["small"])],
        }
        command_receipts.update(lane_commands)
        runners: dict[str, Callable[[], tuple[bytes, dict[str, Any]]]] = {
            "small_staged": lambda: staged(candidate, fixtures["small"], shared, args.timeout, common.ROOT),
            "xml_staged": lambda: staged(candidate, fixtures["large_xml"], shared, args.timeout, common.ROOT),
            **{name: (lambda command=command: single(command, args.timeout, common.ROOT)) for name, command in lane_commands.items()},
        }
        expected: dict[str, bytes] = {}
        warmups: dict[str, list[dict[str, Any]]] = {name: [] for name in runners}
        for _ in range(args.warmups):
            for name, runner in runners.items():
                payload, sample = runner()
                if name in expected and expected[name] != payload:
                    raise AssertionError(f"nondeterministic warmup output for {name}")
                expected[name] = payload
                warmups[name].append(sample)
        equality = {
            "corpus_languages": expected["baseline_corpus_languages"] == expected["candidate_corpus_languages"],
            "xml_languages": expected["baseline_xml_languages"] == expected["candidate_xml_languages"],
        }
        if not all(equality.values()):
            raise AssertionError("candidate changed inherited language-only output")
        samples: dict[str, list[dict[str, Any]]] = {name: [] for name in runners}
        order: list[list[str]] = []
        names = list(runners)
        for repetition in range(args.repetitions):
            lanes = names if repetition % 2 == 0 else list(reversed(names))
            order.append(lanes)
            for name in lanes:
                payload, sample = runners[name]()
                if payload != expected[name]:
                    raise AssertionError(f"nondeterministic measured output for {name}")
                samples[name].append(sample)
        after = {"aspnetcore": inventory(corpus), **{name: inventory(root) for name, root in fixtures.items()}}
        if inputs != after:
            raise AssertionError("benchmark input changed during measurement")
        summaries = {name: common.sample_summary(rows) for name, rows in samples.items()}
        report = {
            "schema": "dircue-context-v080-performance-1", "started_at_utc": started,
            "finished_at_utc": datetime.now(timezone.utc).isoformat(), "platform": platform.platform(),
            "cpu_count": os.cpu_count(), "environment_note": args.environment_note,
            "baseline_release": "v0.7.0", "baseline_sha256": common.sha256(baseline),
            "candidate_sha256": common.sha256(candidate), "build_receipt": build_receipt,
            "build_receipt_sha256": build_receipt_sha,
            "driver_sha256": common.sha256(Path(__file__).resolve()), "shared_helper_sha256": common.sha256(Path(common.__file__).resolve()),
            "inputs": inputs, "commands": command_receipts, "expected_output_sha256": {name: hashlib.sha256(value).hexdigest() for name, value in expected.items()},
            "method": {"repetitions": args.repetitions, "warmups": args.warmups, "workers": args.workers,
                       "tree_size": args.tree_size, "timeout_seconds": args.timeout, "source": "directory",
                       "cache": "warm after explicit per-lane warmup", "order": "all lanes reverse on alternating repetitions",
                       "wall": "complete process wall time; staged lanes sum discovery, offline plan, and declarations processes",
                       "rss": "single-process peak RSS; staged lanes report the maximum stage peak rather than an invalid sum",
                       "claim_limit": "staged and full workflows answer different questions; ratios are recorded observations, not equivalent-output savings"},
            "warmups": warmups, "order": order, "summaries": summaries, "identical_output": equality,
            "comparisons": {
                "small_staged_vs_full_wall_ratio": summaries["small_staged"]["median_seconds"] / summaries["small_full"]["median_seconds"],
                "xml_staged_vs_full_wall_ratio": summaries["xml_staged"]["median_seconds"] / summaries["xml_full"]["median_seconds"],
                "small_environments_vs_declarations_wall_ratio": summaries["candidate_small_environments"]["median_seconds"] / summaries["candidate_small_declarations"]["median_seconds"],
            }, "passed": True,
        }
    common.write_json(args.output, report)
    print(json.dumps({"passed": True, "comparisons": report["comparisons"]}, sort_keys=True))


if __name__ == "__main__":
    main()
