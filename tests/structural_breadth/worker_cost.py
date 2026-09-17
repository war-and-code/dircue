#!/usr/bin/env python3
"""Measure worker artifact size and warm one-file process cost on this host."""

import argparse
import hashlib
import json
import platform
from pathlib import Path
import re
import statistics
import subprocess
import time


def checksum(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def invoke(binary, request):
    started = time.perf_counter_ns()
    process = subprocess.run(
        [str(binary)], input=request, capture_output=True, check=True, timeout=30
    )
    elapsed_ms = (time.perf_counter_ns() - started) / 1_000_000
    response = json.loads(process.stdout)
    if response.get("status") != "complete" or response.get("parse_count") != 1:
        raise RuntimeError(f"{binary}: unexpected response: {response}")
    return elapsed_ms, response


def peak_rss(binary, request):
    system = platform.system()
    if system == "Darwin":
        command = ["/usr/bin/time", "-l", str(binary)]
        pattern = r"(\d+)\s+maximum resident set size"
        multiplier = 1
    elif system == "Linux":
        command = ["/usr/bin/time", "-f", "peak_rss_kib=%M", str(binary)]
        pattern = r"peak_rss_kib=(\d+)"
        multiplier = 1024
    else:
        return {"available": False, "reason": "measurement supports macOS/Linux"}
    process = subprocess.run(
        command, input=request, capture_output=True, check=True, timeout=30
    )
    match = re.search(pattern, process.stderr.decode("utf-8", errors="replace"))
    if not match:
        raise RuntimeError("could not read /usr/bin/time peak RSS")
    return {"available": True, "bytes": int(match.group(1)) * multiplier}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--before", required=True, type=Path)
    parser.add_argument("--candidate", required=True, type=Path)
    parser.add_argument("--output", required=True, type=Path)
    parser.add_argument("--runs", type=int, default=25)
    args = parser.parse_args()
    if args.runs < 1:
        parser.error("--runs must be positive")
    binaries = {"before": args.before.resolve(), "candidate": args.candidate.resolve()}
    source = "class Choice { int choose(int x) { if (x > 0) return x; return 0; } }\n"
    request = json.dumps(
        {"language": "Java", "path": "Choice.java", "source": source, "mode": "combined"}
    ).encode()
    outputs = {}
    for label, binary in binaries.items():
        _, response = invoke(binary, request)
        response.pop("timings_ns", None)
        outputs[label] = response
    if outputs["before"] != outputs["candidate"]:
        raise RuntimeError("worker outputs changed for the Java comparison fixture")
    timings = {label: [] for label in binaries}
    for index in range(args.runs):
        order = list(binaries.items())
        if index % 2:
            order.reverse()
        for label, binary in order:
            elapsed_ms, _ = invoke(binary, request)
            timings[label].append(elapsed_ms)
    result = {
        "description": "Diagnostic warm one-file process latency; not a repository benchmark or a general regression claim.",
        "limitations": [
            "A single 70-byte Java file does not represent large-file parse cost.",
            "One warm-up per binary is excluded; subsequent pairs alternate order.",
            "Peak RSS is one separate invocation per binary, not a hard memory bound.",
            "Artifact hashes identify inputs; their original compiler/build conditions are not inferred.",
        ],
        "platform": platform.platform(),
        "source": source,
        "source_bytes": len(source.encode()),
        "runs_per_binary": args.runs,
        "outputs_equal_excluding_timings": True,
        "workers": {},
    }
    for label, binary in binaries.items():
        values = timings[label]
        result["workers"][label] = {
            "path": str(binary),
            "sha256": checksum(binary),
            "bytes": binary.stat().st_size,
            "elapsed_ms": values,
            "median_ms": statistics.median(values),
            "min_ms": min(values),
            "max_ms": max(values),
            "peak_rss": peak_rss(binary, request),
        }
    args.output.parent.mkdir(parents=True, exist_ok=True)
    args.output.write_text(json.dumps(result, indent=2) + "\n")
    print(json.dumps({key: {field: data[field] for field in ("bytes", "median_ms", "peak_rss")} for key, data in result["workers"].items()}, indent=2))


if __name__ == "__main__":
    main()
