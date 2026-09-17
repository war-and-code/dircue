#!/usr/bin/env python3
"""Measure bounded samples; preserve corpus IDs and content hashes with timings."""

import argparse
import hashlib
import json
import os
from pathlib import Path
import platform
import re
import shutil
import statistics
import subprocess
import tempfile
import time

HERE = Path(__file__).resolve().parent


def digest(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def git(root, *arguments):
    return subprocess.check_output(["git", "-C", str(root), *arguments], text=True).strip()


def sample(root, suffix, count):
    candidates = []
    for relative in git(root, "ls-files", "-z").split("\0"):
        path = root / relative
        if relative.endswith(suffix) and path.is_file() and not path.is_symlink():
            size = path.stat().st_size
            if 0 < size <= 1024 * 1024:
                try:
                    path.read_text(encoding="utf-8")
                except UnicodeError:
                    continue
                candidates.append((relative, size))
    candidates.sort()
    if len(candidates) <= count:
        selected = candidates
    else:
        # Spread the ordinary files over the path ordering and add the largest
        # two eligible inputs. This selection is deterministic, not random.
        ordinary = count - 2
        selected = [candidates[i * (len(candidates) - 1) // (ordinary - 1)] for i in range(ordinary)]
        for item in sorted(candidates, key=lambda entry: (-entry[1], entry[0])):
            if item not in selected:
                selected.append(item)
            if len(selected) == count:
                break
    return [{"path": path, "bytes": size, "sha256": digest(root / path)} for path, size in sorted(selected)]


def run_once(driver, worker, root, mode):
    with tempfile.TemporaryDirectory(prefix="dircue-structural-time-") as temp:
        timing = Path(temp) / "time.txt"
        command = [str(driver), "--worker", str(worker), "--mode", mode, str(root)]
        if platform.system() == "Darwin":
            command = ["/usr/bin/time", "-l", "-o", str(timing), *command]
        elif platform.system() == "Linux":
            command = ["/usr/bin/time", "-f", "max_rss_kib=%M", "-o", str(timing), *command]
        started = time.perf_counter()
        run = subprocess.run(command, capture_output=True, text=True, timeout=240)
        elapsed = time.perf_counter() - started
        if run.returncode:
            raise RuntimeError(f"Benchmark exited {run.returncode}: {run.stderr}\n{run.stdout[-2000:]}")
        rows = [json.loads(line) for line in run.stdout.splitlines()]
        results = [row["result"] for row in rows if row["type"] == "file" and row["status"] == "observed"]
        phase_ns = {phase: sum(result.get("timings_ns", {}).get(phase, 0) for result in results)
                    for phase in ["parse", "structure", "metrics"]}
        rss_bytes = None
        if timing.exists():
            text = timing.read_text()
            if platform.system() == "Darwin":
                found = re.search(r"(\d+)\s+maximum resident set size", text)
                rss_bytes = int(found[1]) if found else None
            else:
                found = re.search(r"max_rss_kib=(\d+)", text)
                rss_bytes = int(found[1]) * 1024 if found else None
        return {"wall_seconds": elapsed, "time_max_rss_bytes": rss_bytes,
                "parse_count": sum(result["parse_count"] for result in results),
                "syntax_error_files": sum(bool(result["syntax_errors"]) for result in results),
                "phase_ns": phase_ns, "summary": rows[-1]}


def repeated(driver, worker, root, mode, repeat):
    # Explicit warm-up; timings describe a warm filesystem cache.
    run_once(driver, worker, root, mode)
    runs = [run_once(driver, worker, root, mode) for _ in range(repeat)]
    return {"mode": mode, "runs": runs,
            "median_wall_seconds": statistics.median(run["wall_seconds"] for run in runs),
            "max_time_rss_bytes": max((run["time_max_rss_bytes"] or 0) for run in runs)}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--worker", type=Path, required=True)
    parser.add_argument("--driver", type=Path, required=True)
    parser.add_argument("--corpus-root", type=Path, required=True)
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument("--build-metadata", type=Path,
                        help="JSON build receipt to include alongside binary hashes")
    parser.add_argument("--files", type=int, default=50)
    parser.add_argument("--repeat", type=int, default=3)
    args = parser.parse_args()
    if args.files < 4 or args.repeat < 1:
        parser.error("--files must be at least 4 and --repeat must be positive")
    driver, worker = args.driver.resolve(), args.worker.resolve()
    report = {"platform": platform.platform(), "machine": platform.machine(),
              "logical_cpus": os.cpu_count(), "python": platform.python_version(),
              "driver_sha256": digest(driver), "worker_sha256": digest(worker),
              "timestamp_utc": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
              "method": {"file_selection": "evenly spaced tracked paths plus two largest eligible files",
                         "max_sample_file_bytes": 1048576, "warmups_per_mode": 1,
                         "repetitions": args.repeat,
                         "rss": "/usr/bin/time maximum RSS; not summed simultaneous driver+worker memory",
                         "scope": "sampled working files copied to an isolated directory; no repository-wide inference"},
              "corpora": [], "scaling": []}
    if args.build_metadata:
        report["build_metadata"] = json.loads(args.build_metadata.read_text())
    for name, suffix in [("spring-framework", ".java"), ("roslyn", ".cs"), ("aspnetcore", ".cs")]:
        root = args.corpus_root.resolve() / name
        if not root.exists():
            raise FileNotFoundError(root)
        files = sample(root, suffix, args.files)
        corpus = {"name": name, "commit": git(root, "rev-parse", "HEAD"),
                  "origin": git(root, "remote", "get-url", "origin"), "files": files,
                  "source_bytes": sum(file["bytes"] for file in files), "measurements": []}
        with tempfile.TemporaryDirectory(prefix="dircue-structural-corpus-") as temp:
            stage = Path(temp)
            for file in files:
                destination = stage / file["path"]
                destination.parent.mkdir(parents=True, exist_ok=True)
                shutil.copyfile(root / file["path"], destination)
            for mode in ["combined", "structure", "metrics"]:
                print(f"Measuring {name}: {mode} ({len(files)} files)", flush=True)
                corpus["measurements"].append(repeated(driver, worker, stage, mode, args.repeat))
        report["corpora"].append(corpus)
    with tempfile.TemporaryDirectory(prefix="dircue-structural-scaling-") as temp:
        stage = Path(temp)
        source = (HERE / "fixtures" / "java" / "Shapes.java").read_bytes()
        previous = 0
        for count in [10, 100, 500]:
            for index in range(previous, count):
                (stage / f"File{index:04d}.java").write_bytes(source)
            previous = count
            print(f"Measuring scaling: {count} files", flush=True)
            report["scaling"].append({"file_count": count, "source_sha256": hashlib.sha256(source).hexdigest(),
                                      "source_bytes": len(source) * count,
                                      "measurement": repeated(driver, worker, stage, "combined", args.repeat)})
    args.output.parent.mkdir(parents=True, exist_ok=True)
    args.output.write_text(json.dumps(report, indent=2) + "\n")
    print(f"Wrote {args.output}")


if __name__ == "__main__":
    main()
