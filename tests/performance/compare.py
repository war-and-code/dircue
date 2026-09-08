#!/usr/bin/env python3
"""Run inside the pinned reference container; compare correctness before timing."""
# Result keys retain the original evidence format across the project rename.
import argparse
import hashlib
import json
import math
import os
from pathlib import Path
import platform
import signal
import statistics
import subprocess
import tempfile
import threading
import time

def command_output(command):
    result = subprocess.run(command, capture_output=True, text=True, timeout=300)
    if result.returncode:
        raise RuntimeError(f"{command}: exit {result.returncode}: {result.stderr[:2000]}")
    return result.stdout, result.stderr

def normalized(value):
    return {language: {**details, "files": sorted(details.get("files", []))} for language, details in value.items()}

def differences(expected, actual):
    expected_files = {file: language for language, details in expected.items() for file in details.get("files", [])}
    actual_files = {file: language for language, details in actual.items() for file in details.get("files", [])}
    paths = sorted(set(expected_files) | set(actual_files))
    changed = [{"path": path, "linguist": expected_files.get(path), "auragaze": actual_files.get(path)}
               for path in paths if expected_files.get(path) != actual_files.get(path)]
    return {"file_differences": changed,
            "language_totals": {language: {"linguist": expected.get(language, {}).get("size", 0),
                                           "auragaze": actual.get(language, {}).get("size", 0)}
                                for language in sorted(set(expected) | set(actual))
                                if expected.get(language, {}).get("size", 0) != actual.get(language, {}).get("size", 0)}}

def measured(command):
    # A forked Python child's high-water RSS can include the parent's pre-exec
    # footprint. GNU time measures its own small child, avoiding that floor.
    # Include the same launcher's overhead in both precise wall measurements.
    with tempfile.TemporaryFile() as stderr, tempfile.NamedTemporaryFile() as stats:
        started = time.perf_counter()
        process = subprocess.Popen(["/usr/bin/time", "-f", "%U %S %M", "-o", stats.name, *command],
                                   stdout=subprocess.DEVNULL, stderr=stderr, start_new_session=True)
        expired = threading.Event()
        def stop():
            expired.set()
            try:
                os.killpg(process.pid, signal.SIGKILL)
            except ProcessLookupError:
                pass
        watchdog = threading.Timer(1800, stop)
        watchdog.daemon = True
        watchdog.start()
        try:
            process.wait()
        finally:
            watchdog.cancel()
        elapsed = time.perf_counter() - started
        stderr.seek(0)
        diagnostic = stderr.read().decode(errors="replace")
        if expired.is_set() or process.returncode:
            raise RuntimeError(f"timed process failed {command}: {diagnostic[:2000]}")
        stats.seek(0)
        user_seconds, system_seconds, rss = stats.read().decode().split()
        user_seconds, system_seconds = float(user_seconds), float(system_seconds)
        return {"seconds": elapsed, "user_seconds": user_seconds, "system_seconds": system_seconds,
                "max_rss_kib": int(rss), "cpu_percent": 100 * (user_seconds + system_seconds) / elapsed,
                "stderr_bytes": len(diagnostic)}

def summary(samples):
    values = sorted(sample["seconds"] for sample in samples)
    def percentile(p):
        return values[min(len(values) - 1, math.ceil(p * len(values)) - 1)]
    median = statistics.median(values)
    return {"runs": len(samples), "median_seconds": median,
            "p95_seconds": percentile(.95), "p99_seconds": percentile(.99),
            "p999_seconds": percentile(.999), "p9999_seconds": percentile(.9999),
            "max_seconds": max(values), "min_seconds": min(values),
            "mean_seconds": statistics.mean(values),
            "coefficient_of_variation": statistics.pstdev(values) / statistics.mean(values),
            "peak_rss_kib": max(sample["max_rss_kib"] for sample in samples),
            "mean_cpu_percent": statistics.mean(sample["cpu_percent"] for sample in samples),
            "operations_per_second": 1 / median}

def read_optional(path):
    try:
        return Path(path).read_text().strip()
    except OSError:
        return None

def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--corpus", type=Path, default=Path("/corpus"))
    parser.add_argument("--candidate", default="/candidate/dircue")
    parser.add_argument("--output", type=Path, default=Path("/results/comparison.json"))
    parser.add_argument("--runs", type=int, default=20)
    parser.add_argument("--warmup", type=int, default=3)
    parser.add_argument("--project", action="append", default=[])
    args = parser.parse_args()
    if platform.system() != "Linux":
        parser.error("run inside the pinned Linux reference container (GNU time and Linux RSS units)")
    if args.runs < 0 or args.warmup < 0:
        parser.error("runs and warmup must be nonnegative")
    projects = json.loads((args.corpus / "provenance.json").read_text())
    if args.project:
        unknown = set(args.project) - {project["name"] for project in projects}
        if unknown:
            parser.error(f"unknown projects: {', '.join(sorted(unknown))}")
        projects = [project for project in projects if project["name"] in args.project]
    args.output.parent.mkdir(parents=True, exist_ok=True)
    artifact_dir = args.output.parent / (args.output.stem + "-details")
    artifact_dir.mkdir(exist_ok=True)
    version, _ = command_output(["github-linguist", "--version"])
    report = {
        "schema_version": "1.0.0", "started_at_utc": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
        "reference_version": version.strip(),
        "candidate_sha256": hashlib.sha256(Path(args.candidate).read_bytes()).hexdigest(),
        "harness_sha256": hashlib.sha256(Path(__file__).read_bytes()).hexdigest(),
        "environment": {"platform": platform.platform(), "machine": platform.machine(), "cpu_count": os.cpu_count(),
                        "affinity": sorted(os.sched_getaffinity(0)), "cpuinfo": read_optional("/proc/cpuinfo"),
                        "meminfo": read_optional("/proc/meminfo"), "mounts": read_optional("/proc/mounts"),
                        "cpu_max": read_optional("/sys/fs/cgroup/cpu.max"),
                        "memory_max": read_optional("/sys/fs/cgroup/memory.max"),
                        "gem_versions": read_optional("/reference-gems.txt")},
        "methodology": {"runs": args.runs, "warmup": args.warmup, "cache": "warm after explicit warmups; no cache flush",
                        "workload": "full process --breakdown --json, committed HEAD, same container and corpus",
                        "order": "alternating tools per iteration", "timing": "perf_counter around process launch/completion, includes identical GNU time launcher overhead",
                        "rss": "GNU time target-child peak RSS in KiB; avoids Python pre-exec RSS floor",
                        "cpu": "GNU time target-child user/system seconds, 0.01-second precision",
                        "tail_note": "p99+ conservative observed order statistics; 20 runs cannot estimate rare tails",
                        "correctness_gate": "exact language names, byte totals, percentage strings and per-language file sets; timing withheld on mismatch"},
        "projects": []}
    for project in projects:
        path = str(args.corpus / project["name"])
        commands = {"linguist": ["github-linguist", "--json", "--breakdown", path],
                    "auragaze": [args.candidate, "--json", "--breakdown", path]}
        parsed = {}
        stderr = {}
        for tool, command in commands.items():
            output, diagnostic = command_output(command)
            parsed[tool] = normalized(json.loads(output))
            stderr[tool] = diagnostic
            (artifact_dir / f"{project['name']}-{tool}.json").write_text(output)
        match = parsed["linguist"] == parsed["auragaze"]
        item = {**project, "match": match, "commands": commands, "stderr": stderr,
                "output_sha256": {tool: hashlib.sha256((artifact_dir / f"{project['name']}-{tool}.json").read_bytes()).hexdigest() for tool in commands},
                "language_bytes": sum(value["size"] for value in parsed["linguist"].values()),
                "counted_files": sum(len(value["files"]) for value in parsed["linguist"].values()),
                "differences": differences(parsed["linguist"], parsed["auragaze"])}
        if match and args.runs:
            for _ in range(args.warmup):
                for command in commands.values():
                    measured(command)
            samples = {tool: [] for tool in commands}
            for iteration in range(args.runs):
                for tool in (["linguist", "auragaze"] if iteration % 2 == 0 else ["auragaze", "linguist"]):
                    samples[tool].append(measured(commands[tool]))
            item["samples"] = samples
            item["timings"] = {tool: summary(values) for tool, values in samples.items()}
            for values in item["timings"].values():
                values["language_bytes_per_second"] = item["language_bytes"] / values["median_seconds"]
            item["median_speedup"] = item["timings"]["linguist"]["median_seconds"] / item["timings"]["auragaze"]["median_seconds"]
        report["projects"].append(item)
        args.output.write_text(json.dumps(report, indent=2) + "\n")
        print(json.dumps({"project": project["name"], "match": match,
                          "file_differences": len(item["differences"]["file_differences"]),
                          "speedup": item.get("median_speedup")}), flush=True)
    report["finished_at_utc"] = time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime())
    report["candidate_sha256_after"] = hashlib.sha256(Path(args.candidate).read_bytes()).hexdigest()
    report["passed"] = (bool(projects) and report["candidate_sha256"] == report["candidate_sha256_after"]
                        and all(project["match"] for project in report["projects"]))
    report["performance_passed"] = (all(project.get("median_speedup", 0) >= 1 for project in report["projects"])
                                    if args.runs and report["passed"] else None)
    args.output.write_text(json.dumps(report, indent=2) + "\n")
    raise SystemExit(0 if report["passed"] and report["performance_passed"] is not False else 1)

if __name__ == "__main__":
    main()
