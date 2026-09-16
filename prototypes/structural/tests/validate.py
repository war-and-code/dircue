#!/usr/bin/env python3
"""Exercise the prototype worker and driver without third-party test packages."""

import argparse
import hashlib
import json
from pathlib import Path
import platform
import signal
import subprocess
import tempfile
import time

HERE = Path(__file__).resolve().parent


def worker_call(worker, source, language="Java", mode="combined", **overrides):
    request = dict(path="fixture", language=language, source=source, mode=mode)
    request.update(overrides)
    run = subprocess.run([str(worker)], input=json.dumps(request), text=True,
                         capture_output=True, timeout=30)
    return run, json.loads(run.stdout)


def driver_call(driver, worker, root, *args):
    run = subprocess.run([str(driver), "--worker", str(worker), *args, str(root)],
                         text=True, capture_output=True, timeout=60)
    return run, [json.loads(line) for line in run.stdout.splitlines()]


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--worker", type=Path, required=True)
    parser.add_argument("--driver", type=Path, required=True)
    parser.add_argument("--output", type=Path)
    args = parser.parse_args()
    worker, driver = args.worker.resolve(), args.driver.resolve()
    checks = []

    def check(name, condition):
        if not condition:
            raise AssertionError(name)
        checks.append(name)

    fixtures = [("java/Shapes.java", "Java"), ("csharp/Shapes.cs", "C#")]
    observations = {}
    for fixture, language in fixtures:
        source = (HERE / "fixtures" / fixture).read_text()
        outputs = {}
        for mode in ["combined", "structure", "metrics"]:
            run, result = worker_call(worker, source, language, mode)
            check(f"{language} {mode}: successful", run.returncode == 0 and result["status"] == "complete")
            check(f"{language} {mode}: one parse", result["parse_count"] == 1)
            check(f"{language} {mode}: exact byte count", result["source_bytes"] == len(source.encode()))
            outputs[mode] = result
        combined = outputs["combined"]
        check(f"{language}: same observations from shared tree", combined["observations"] == outputs["structure"]["observations"])
        check(f"{language}: same metrics from shared tree", combined["metrics"] == outputs["metrics"]["metrics"])
        check(f"{language}: structure mode omits metrics", "metrics" not in outputs["structure"])
        check(f"{language}: metrics mode omits observations", "observations" not in outputs["metrics"])
        counts = combined["observations"]
        for kind, expected in {"classes": 2, "interfaces": 1, "records": 1,
                               "methods": 4, "imports": 2 if language == "Java" else 3,
                               "lambdas": 1 if language == "Java" else 2}.items():
            check(f"{language}: {kind}={expected}", counts[kind] == expected)
        observations[language] = counts

    for fixture, language in [("malformed/Broken.java", "Java"), ("malformed/Broken.cs", "C#")]:
        run, result = worker_call(worker, (HERE / "fixtures" / fixture).read_text(), language)
        check(f"{language}: malformed is partial", run.returncode == 0 and result["status"] == "partial" and result["syntax_errors"])
        check(f"{language}: malformed parsed once", result["parse_count"] == 1)

    for name, source, overrides in [
        ("unsupported language", "print('hello')", {"language": "Python"}),
        ("unknown mode", "class X {}", {"mode": "unknown"}),
        ("oversized source", " " * (8 * 1024 * 1024 + 1), {}),
    ]:
        run, result = worker_call(worker, source, **overrides)
        check(name + ": rejected before parsing", run.returncode != 0 and result["status"] == "error" and result["parse_count"] == 0)

    run, rows = driver_call(driver, worker, HERE / "fixtures")
    files = [row for row in rows if row["type"] == "file"]
    check("driver: four supported fixture files", len(files) == 4)
    check("driver: XML/prose not parsed", not any(row["path"].endswith((".xml", ".txt")) for row in files))
    check("driver: summary emitted", rows[-1]["type"] == "summary")

    with tempfile.TemporaryDirectory(prefix="dircue-structural-tests-") as temp:
        root = Path(temp)
        (root / "Small.java").write_text("class Small {}")
        (root / "Large.java").write_text("class Large {}" + " " * 1024)
        (root / "Invalid.cs").write_bytes(b"class Invalid {}\xff")
        (root / "Link.java").symlink_to(root / "Small.java")
        (root / ".git").mkdir()
        (root / ".git" / "Hidden.java").write_text("class Hidden {}")
        run, rows = driver_call(driver, worker, root, "--max-file-bytes", "512")
        by_name = {row["path"]: row for row in rows if row["type"] == "file"}
        check("driver: small source observed", by_name["Small.java"]["status"] == "observed")
        for filename in ["Large.java", "Invalid.cs", "Link.java"]:
            check("driver: skipped " + filename, by_name[filename]["status"] == "skipped")
        check("driver: metadata directory excluded", not any("Hidden.java" in row.get("path", "") for row in rows))
        check("driver: policy skips successful partial run", run.returncode == 0 and rows[-1]["status"] == "partial")
        run, rows = driver_call(driver, worker, root, "--timeout", "1ns")
        check("driver: timeout reported as error", run.returncode != 0 and rows[-1]["errors"] > 0)

    # A sleeping local helper makes cancellation deterministic. It does not
    # substitute for the real worker tests above.
    with tempfile.TemporaryDirectory(prefix="dircue-structural-cancel-") as temp:
        root = Path(temp)
        (root / "File.java").write_text("class File {}")
        helper = root / "wait-worker"
        helper.write_text("#!/bin/sh\nexec sleep 30\n")
        helper.chmod(0o755)
        started = time.monotonic()
        process = subprocess.Popen([str(driver), "--worker", str(helper), str(root)],
                                   stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True)
        time.sleep(0.2)
        process.send_signal(signal.SIGINT)
        stdout, stderr = process.communicate(timeout=5)
        check("driver: interrupt stops worker promptly", process.returncode != 0 and time.monotonic() - started < 5)

    report = {"checks_passed": len(checks), "checks": checks, "fixture_observations": observations,
              "platform": platform.platform(),
              "worker_sha256": hashlib.sha256(worker.read_bytes()).hexdigest(),
              "driver_sha256": hashlib.sha256(driver.read_bytes()).hexdigest()}
    rendered = json.dumps(report, indent=2) + "\n"
    if args.output:
        args.output.parent.mkdir(parents=True, exist_ok=True)
        args.output.write_text(rendered)
    print(rendered, end="")


if __name__ == "__main__":
    main()
