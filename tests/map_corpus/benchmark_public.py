#!/usr/bin/env python3
"""Measure map resource use on an already-materialized pinned public corpus.

This on-demand harness does not fetch repositories and defines no release
thresholds. It also checks that execution-only presets produce byte-identical
map documents for each pinned source.
"""
import argparse
import hashlib
import json
import platform
import re
import subprocess
import sys
import tempfile
import time
from pathlib import Path

HERE = Path(__file__).resolve().parent


def run_text(command):
    return subprocess.run(command, check=True, capture_output=True, text=True).stdout.strip()


def digest_file(path):
    digest = hashlib.sha256()
    with path.open("rb") as stream:
        for block in iter(lambda: stream.read(1024 * 1024), b""):
            digest.update(block)
    return digest.hexdigest()


def semantic_digest(document):
    """Hash the map answer while excluding only execution provenance."""
    answer = dict(document)
    answer.pop("execution", None)
    encoded = json.dumps(answer, sort_keys=True, separators=(",", ":")).encode()
    return hashlib.sha256(encoded).hexdigest()


def timed_command(binary, source, preset, output, diagnostics):
    command = [str(binary), "map", "--preset", preset, "--source", "git", "--json", str(source)]
    if sys.platform == "darwin":
        wrapped = ["/usr/bin/time", "-l", *command]
    elif sys.platform.startswith("linux"):
        wrapped = ["/usr/bin/time", "-v", *command]
    else:
        raise SystemExit("resource benchmark currently requires macOS or Linux /usr/bin/time")
    started = time.monotonic()
    with output.open("wb") as stdout, diagnostics.open("wb") as stderr:
        result = subprocess.run(wrapped, stdout=stdout, stderr=stderr)
    wall = time.monotonic() - started
    if result.returncode != 0:
        detail = diagnostics.read_text(errors="replace")[-4000:]
        raise SystemExit(f"map failed for {source.name}/{preset} (exit {result.returncode}):\n{detail}")
    return wall, diagnostics.read_text(errors="replace")


def parse_usage(text):
    if sys.platform == "darwin":
        timing = re.search(r"([0-9.]+) real\s+([0-9.]+) user\s+([0-9.]+) sys", text)
        rss = re.search(r"(\d+)\s+maximum resident set size", text)
        if not timing or not rss:
            raise SystemExit(f"cannot parse BSD time output:\n{text[-2000:]}")
        return float(timing.group(2)), float(timing.group(3)), int(rss.group(1))
    user = re.search(r"User time \(seconds\):\s*([0-9.]+)", text)
    system = re.search(r"System time \(seconds\):\s*([0-9.]+)", text)
    rss = re.search(r"Maximum resident set size \(kbytes\):\s*(\d+)", text)
    if not user or not system or not rss:
        raise SystemExit(f"cannot parse GNU time output:\n{text[-2000:]}")
    return float(user.group(1)), float(system.group(1)), int(rss.group(1)) * 1024


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--binary", type=Path, required=True)
    parser.add_argument("--corpus-root", type=Path, required=True)
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument("--candidate-commit", required=True,
                        help="Exact Git commit used to build --binary")
    parser.add_argument("--preset", action="append", choices=["balanced", "fast", "low-memory"],
                        help="Execution-only preset to measure; defaults to all three")
    parser.add_argument("--max-output-bytes", type=int, default=256 * 1024 * 1024)
    args = parser.parse_args()
    binary = args.binary.resolve()
    if not binary.is_file():
        raise SystemExit(f"binary not found: {binary}")
    presets = args.preset or ["balanced", "fast", "low-memory"]
    manifest = json.loads((HERE / "public_expectations.json").read_text())
    results = []
    with tempfile.TemporaryDirectory(prefix="dircue-map-bench-") as temporary:
        temporary = Path(temporary)
        for expected in manifest["repositories"]:
            source = (args.corpus_root / expected["id"]).resolve()
            if not source.is_dir():
                raise SystemExit(f"missing pinned source: {expected['id']}")
            commit = run_text(["git", "-C", str(source), "rev-parse", "HEAD"])
            if commit != expected["commit"]:
                raise SystemExit(f"{expected['id']}: commit {commit}, expected {expected['commit']}")
            runs = []
            for preset in presets:
                output = temporary / f"{expected['id']}-{preset}.json"
                diagnostics = temporary / f"{expected['id']}-{preset}.time"
                wall, raw_usage = timed_command(binary, source, preset, output, diagnostics)
                size = output.stat().st_size
                if size > args.max_output_bytes:
                    raise SystemExit(f"{expected['id']}/{preset}: {size} output bytes exceeds limit {args.max_output_bytes}")
                user, system, rss = parse_usage(raw_usage)
                document = json.loads(output.read_bytes())
                runs.append({
                    "preset": preset, "wall_seconds": round(wall, 6),
                    "user_cpu_seconds": user, "system_cpu_seconds": system,
                    "peak_rss_bytes": rss, "output_bytes": size,
                    "raw_output_sha256": digest_file(output),
                    "semantic_output_sha256": semantic_digest(document),
                    "map_status": document.get("status"),
                    "nodes": len(document.get("nodes", [])), "edges": len(document.get("edges", [])),
                })
            semantic_hashes = {run["semantic_output_sha256"] for run in runs}
            if len(semantic_hashes) != 1:
                raise SystemExit(f"{expected['id']}: execution-only presets changed map answers")
            raw_hashes = {run["raw_output_sha256"] for run in runs}
            results.append({
                "id": expected["id"], "commit": commit,
                "preset_answers_equal": True, "raw_outputs_equal": len(raw_hashes) == 1,
                "semantic_hash_exclusion": ["execution"], "runs": runs,
            })
            print(f"{expected['id']}: {len(runs)} presets, identical answers", file=sys.stderr)
    report = {
        "schema": "dircue-public-map-benchmark-0.1", "platform": platform.platform(),
        "python": platform.python_version(), "presets": presets,
        "candidate": {
            "commit": args.candidate_commit, "sha256": digest_file(binary),
            "version": run_text([str(binary), "--version"]),
        },
        "repositories": len(results), "max_output_bytes": args.max_output_bytes,
        "qualification": "observational single-run measurements; no release threshold or cross-host comparison",
        "results": results,
    }
    args.output.parent.mkdir(parents=True, exist_ok=True)
    args.output.write_text(json.dumps(report, indent=2) + "\n")
    print(json.dumps({"repositories": len(results), "presets": presets, "status": "passed"}, indent=2))


if __name__ == "__main__":
    main()
