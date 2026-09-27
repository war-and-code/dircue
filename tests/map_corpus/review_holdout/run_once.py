#!/usr/bin/env python3
"""Run the frozen review holdout exactly once per pinned Git checkout.

Refuses to run if its receipt directory already exists. The map command uses
only committed Git trees; it does not use or inspect working-tree output.
"""
from __future__ import annotations

import argparse
import datetime as dt
import hashlib
import json
import subprocess
import sys
from types import SimpleNamespace
from pathlib import Path

EXPECTED = {
    "chi": "3d1777a1ef8881f7d1da0b02c76ca8f0a29cd2bc",
    "fastapi": "192b12197eb04c2b4a691cce7d87261b21716714",
    "changedetection": "d789fe3ea5809eef0134943917ef50f47259121b",
    "umami": "ec0ff50388c264ed8ce46f00967e92f7e71476ae",
}
LABEL_COMMIT = "0fb62770cc7faa80ab04c195dfc0e3d6f473bbaf"


def sha256(path: Path) -> str:
    h = hashlib.sha256()
    with path.open("rb") as f:
        for chunk in iter(lambda: f.read(1024 * 1024), b""):
            h.update(chunk)
    return h.hexdigest()


def git(cwd: Path, *args: str) -> str:
    return subprocess.run(["git", *args], cwd=cwd, check=True, text=True,
                          stdout=subprocess.PIPE, stderr=subprocess.PIPE).stdout.strip()


def now() -> str:
    return dt.datetime.now(dt.timezone.utc).isoformat()


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--binary", required=True, type=Path)
    parser.add_argument("--repos", required=True, type=Path)
    parser.add_argument("--receipts", required=True, type=Path)
    parser.add_argument("--build-command", required=True)
    parser.add_argument("--build-source", required=True)
    parser.add_argument("--go-version", required=True)
    args = parser.parse_args()
    binary = args.binary.resolve()
    repos = args.repos.resolve()
    receipts = args.receipts.resolve()
    if receipts.exists():
        raise SystemExit(f"refusing to overwrite existing receipt directory: {receipts}")
    for repo, commit in EXPECTED.items():
        root = repos / repo
        if git(root, "rev-parse", "HEAD") != commit:
            raise SystemExit(f"{repo}: checkout HEAD does not match frozen pin {commit}")
        if git(root, "status", "--porcelain"):
            raise SystemExit(f"{repo}: working tree is dirty; refusing non-frozen input")
    receipts.mkdir(parents=True)
    raw_dir = receipts / "raw"
    raw_dir.mkdir()
    binary_hash = sha256(binary)
    build = {
        "command": args.build_command,
        "source_commit": args.build_source,
        "go_version": args.go_version,
        "binary_path": "$HOLDOUT/dircue",
        "binary_sha256": binary_hash,
        "label_commit": LABEL_COMMIT,
        "map_invocations": 4,
        "note": "One invocation per pinned repository. No retries or reruns are performed.",
    }
    (receipts / "build.json").write_text(json.dumps(build, indent=2) + "\n")
    failures = 0
    for repo, expected_commit in EXPECTED.items():
        root = repos / repo
        source_commit = git(root, "rev-parse", "HEAD")
        source_tree = git(root, "rev-parse", "HEAD^{tree}")
        exec_argv = [str(binary), "map", "--source", "git", "--rev", "HEAD", "--json", "."]
        receipt_argv = ["$BINARY", "map", "--source", "git", "--rev", "HEAD", "--json", "."]
        started = now()
        timed_out = False
        try:
            proc = subprocess.run(exec_argv, cwd=root, stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=300)
        except subprocess.TimeoutExpired as exc:
            timed_out = True
            proc = SimpleNamespace(returncode=124, stdout=exc.stdout or b"", stderr=(exc.stderr or b"") + b"\nmap invocation exceeded 300-second timeout\n")
        ended = now()
        raw_path = raw_dir / f"{repo}.json"
        stderr_path = raw_dir / f"{repo}.stderr"
        raw_path.write_bytes(proc.stdout)
        stderr_path.write_bytes(proc.stderr)
        receipt = {
            "repo": repo,
            "source_commit": source_commit,
            "expected_commit": expected_commit,
            "source_tree": source_tree,
            "source_dirty": False,
            "binary_sha256": binary_hash,
            "argv": receipt_argv,
            "cwd": f"$REPOS/{repo}",
            "started_utc": started,
            "ended_utc": ended,
            "exit_status": proc.returncode,
            "timed_out": timed_out,
            "timeout_seconds": 300,
            "stdout_file": str(raw_path.relative_to(receipts)),
            "stdout_bytes": len(proc.stdout),
            "stdout_sha256": hashlib.sha256(proc.stdout).hexdigest(),
            "stderr_file": str(stderr_path.relative_to(receipts)),
            "stderr_bytes": len(proc.stderr),
            "stderr_sha256": hashlib.sha256(proc.stderr).hexdigest(),
        }
        (receipts / f"{repo}.receipt.json").write_text(json.dumps(receipt, indent=2) + "\n")
        try:
            parsed = json.loads(proc.stdout)
            receipt["stdout_json_valid"] = isinstance(parsed, dict)
        except (json.JSONDecodeError, UnicodeDecodeError):
            receipt["stdout_json_valid"] = False
        (receipts / f"{repo}.receipt.json").write_text(json.dumps(receipt, indent=2) + "\n")
        print(f"{repo}: exit={proc.returncode} bytes={len(proc.stdout)} sha256={receipt['stdout_sha256']}")
        failures += proc.returncode != 0 or not receipt["stdout_json_valid"]
    return 1 if failures else 0


if __name__ == "__main__":
    sys.exit(main())
