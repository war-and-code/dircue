#!/usr/bin/env python3
"""Byte-compare candidate legacy outputs with a published dircue binary."""
import argparse
import hashlib
import json
import subprocess
from pathlib import Path

HERE = Path(__file__).resolve().parent
MAX_STREAM = 256 * 1024 * 1024
MODES = [
    ("json", ["--json"]),
    ("breakdown-json", ["--breakdown", "--json"]),
    ("breakdown-strategies", ["--breakdown", "--strategies"]),
]


def invoke(binary, flags, cwd, timeout):
    result = subprocess.run([str(binary), *flags, "."], cwd=cwd, capture_output=True, timeout=timeout)
    if len(result.stdout) > MAX_STREAM or len(result.stderr) > MAX_STREAM:
        raise RuntimeError(f"output exceeds {MAX_STREAM} bytes")
    return result


def sha(data):
    return hashlib.sha256(data).hexdigest()


def first_difference(left, right):
    for index, (a, b) in enumerate(zip(left, right)):
        if a != b:
            return index
    return min(len(left), len(right)) if len(left) != len(right) else None


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--candidate", type=Path, required=True)
    parser.add_argument("--baseline", type=Path, required=True)
    parser.add_argument("--baseline-version", default="dircue 0.9.0")
    parser.add_argument("--corpus-root", type=Path, required=True)
    parser.add_argument("--timeout", type=int, default=300)
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument("--candidate-commit", required=True,
                        help="Exact Git commit used to build --candidate")
    args = parser.parse_args()
    candidate, baseline = args.candidate.resolve(), args.baseline.resolve()
    version = subprocess.run([str(baseline), "--version"], check=True, capture_output=True, text=True).stdout.strip()
    if version != args.baseline_version:
        raise SystemExit(f"baseline version {version!r}, expected {args.baseline_version!r}")
    manifest = json.loads((HERE / "public_expectations.json").read_text())
    results = []
    for expected in manifest["repositories"]:
        source = (args.corpus_root / expected["id"]).resolve()
        commit = subprocess.run(["git", "-C", str(source), "rev-parse", "HEAD"], check=True, capture_output=True, text=True).stdout.strip()
        if commit != expected["commit"]:
            raise SystemExit(f"{expected['id']}: commit {commit}, expected {expected['commit']}")
        for mode, flags in MODES:
            old = invoke(baseline, flags, source, args.timeout)
            new = invoke(candidate, flags, source, args.timeout)
            passed = old.returncode == new.returncode and old.stdout == new.stdout and old.stderr == new.stderr
            row = {
                "id": expected["id"], "commit": commit, "mode": mode,
                "status": "passed" if passed else "failed",
                "baseline": {"exit": old.returncode, "stdout_bytes": len(old.stdout), "stdout_sha256": sha(old.stdout), "stderr_sha256": sha(old.stderr)},
                "candidate": {"exit": new.returncode, "stdout_bytes": len(new.stdout), "stdout_sha256": sha(new.stdout), "stderr_sha256": sha(new.stderr)},
            }
            if not passed:
                row["first_stdout_difference"] = first_difference(old.stdout, new.stdout)
                row["first_stderr_difference"] = first_difference(old.stderr, new.stderr)
            results.append(row)
        print(f"{expected['id']}: {sum(x['status'] == 'passed' for x in results[-3:])}/3 byte-identical", flush=True)
    report = {
        "gate": "pinned-public-legacy-regression", "baseline_version": version,
        "candidate_version": subprocess.run([str(candidate), "--version"], check=True, capture_output=True, text=True).stdout.strip(),
        "candidate_commit": args.candidate_commit,
        "candidate_sha256": sha(candidate.read_bytes()),
        "baseline_sha256": sha(baseline.read_bytes()),
        "repositories": len(manifest["repositories"]), "comparisons": len(results),
        "passed": sum(x["status"] == "passed" for x in results),
        "failed": sum(x["status"] == "failed" for x in results),
        "scope": "Byte-for-byte stdout, stderr, and exit status for legacy json, breakdown-json, and breakdown-strategies modes.",
        "results": results,
    }
    args.output.parent.mkdir(parents=True, exist_ok=True)
    args.output.write_text(json.dumps(report, indent=2) + "\n")
    print(json.dumps({k: report[k] for k in ("comparisons", "passed", "failed")}, indent=2))
    return 1 if report["failed"] else 0


if __name__ == "__main__":
    raise SystemExit(main())
