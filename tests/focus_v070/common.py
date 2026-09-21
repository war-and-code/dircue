#!/usr/bin/env python3
"""Shared receipt helpers for the 0.7 focus harnesses."""

from __future__ import annotations

import base64
import gzip
import hashlib
import json
from pathlib import Path
import subprocess
import statistics
from typing import Any


ROOT = Path(__file__).resolve().parents[2]
BASELINE_SHA256 = "2cfe46f0467a422124a94ec1931cc91ccd92be95fa1d61f6ff043ba260f29145"


def sha256(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as source:
        for block in iter(lambda: source.read(1024 * 1024), b""):
            digest.update(block)
    return digest.hexdigest()


def script_hashes() -> dict[str, str]:
    names = ["broad.py", "build.py", "common.py", "fixture.py", "run.py", "verify.py", "performance.py"]
    return {f"tests/focus_v070/{name}": sha256(Path(__file__).parent / name) for name in names}


def load_build_receipt(path: Path, executable: Path) -> tuple[dict[str, Any], str]:
    receipt = json.loads(path.read_text())
    observed = sha256(executable)
    if receipt.get("schema") != "dircue-focus-v070-build-1" or receipt.get("candidate_sha256") != observed:
        raise AssertionError("candidate differs from its build receipt")
    source = receipt.get("source_at_build")
    if not isinstance(receipt.get("files"), dict) or not receipt["files"] or not isinstance(source, dict):
        raise AssertionError("build receipt lacks compilation-source binding")
    if not {"commit", "head_tree", "status_sha256", "diff_sha256", "dirty"}.issubset(source):
        raise AssertionError("build receipt source identity is incomplete")
    return receipt, sha256(path)


def capture(binary: Path, args: list[str], cwd: Path) -> tuple[int, bytes, bytes]:
    process = subprocess.run(
        [str(binary), *args], cwd=cwd, stdout=subprocess.PIPE, stderr=subprocess.PIPE,
        env={"PATH": "/usr/bin:/bin:/usr/sbin:/sbin", "NO_COLOR": "1", "LC_ALL": "C"},
        check=False,
    )
    return process.returncode, process.stdout, process.stderr


def recorded(value: tuple[int, bytes, bytes]) -> dict[str, Any]:
    code, stdout, stderr = value
    result: dict[str, Any] = {"exit": code}
    for key, content in (("stdout", stdout), ("stderr", stderr)):
        result[f"{key}_sha256"] = hashlib.sha256(content).hexdigest()
        try:
            result[key] = content.decode("utf-8")
        except UnicodeDecodeError:
            result[f"{key}_base64"] = base64.b64encode(content).decode("ascii")
    return result


def decode(record: dict[str, Any]) -> tuple[int, bytes, bytes]:
    values: list[bytes] = []
    for key in ("stdout", "stderr"):
        if (key in record) == (f"{key}_base64" in record):
            raise AssertionError(f"ambiguous {key} encoding")
        value = record[key].encode() if key in record else base64.b64decode(record[f"{key}_base64"], validate=True)
        if hashlib.sha256(value).hexdigest() != record[f"{key}_sha256"]:
            raise AssertionError(f"{key} digest mismatch")
        values.append(value)
    if type(record["exit"]) is not int:
        raise AssertionError("exit code must be an integer")
    return record["exit"], values[0], values[1]


def parse_json(value: tuple[int, bytes, bytes], label: str) -> dict[str, Any]:
    if value[0] != 0:
        raise AssertionError(f"{label} failed: {value[2].decode(errors='replace')}")
    if value[2]:
        raise AssertionError(f"{label} wrote unexpected stderr: {value[2].decode(errors='replace')}")
    parsed = json.loads(value[1])
    if not isinstance(parsed, dict):
        raise AssertionError(f"{label} did not return a JSON object")
    return parsed


def write_json(path: Path, value: dict[str, Any]) -> None:
    if path.exists():
        raise AssertionError("choose a fresh output receipt")
    path.parent.mkdir(parents=True, exist_ok=True)
    payload = (json.dumps(value, indent=2, sort_keys=True) + "\n").encode()
    path.write_bytes(gzip.compress(payload, mtime=0) if path.suffix == ".gz" else payload)


def read_json(path: Path) -> dict[str, Any]:
    payload = path.read_bytes()
    return json.loads(gzip.decompress(payload) if path.suffix == ".gz" else payload)


def canonical_json_sha256(value: Any) -> str:
    payload = (json.dumps(value, indent=2, sort_keys=True) + "\n").encode()
    return hashlib.sha256(payload).hexdigest()


def aggregate_file_metrics(files: list[dict[str, Any]]) -> dict[str, int]:
    keys = ("files", "bytes", "lines", "code", "comment", "blank", "complexity")
    totals = {key: 0 for key in keys}
    for item in files:
        if item["status"] != "counted":
            continue
        counts = item["counts"]
        for key in keys:
            totals[key] += counts[key]
    return totals


def sample_summary(samples: list[dict[str, Any]]) -> dict[str, Any]:
    return {
        "samples": samples,
        "median_seconds": statistics.median(row["seconds"] for row in samples),
        "min_seconds": min(row["seconds"] for row in samples),
        "max_seconds": max(row["seconds"] for row in samples),
        "median_peak_rss_bytes": statistics.median(row["peak_rss_bytes"] for row in samples),
        "max_peak_rss_bytes": max(row["peak_rss_bytes"] for row in samples),
    }
