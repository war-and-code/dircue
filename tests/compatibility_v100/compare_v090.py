#!/usr/bin/env python3
"""Compare the inherited CLI corpus with an actual v0.9.0 release binary."""

import argparse
from collections import Counter
from datetime import datetime, timezone
import hashlib
import json
from pathlib import Path
import sys
import tempfile


ROOT = Path(__file__).resolve().parents[2]
sys.path.insert(0, str(ROOT / "tests/context_v080"))
import broad


BASELINE_SHA256 = "7bcadb08c6ebd6494e74de6302c74a1f5b55f0787457658f348b931e1338a477"
EXPECTED_CASES = 278


def sha256(path: Path) -> str:
    return hashlib.sha256(path.read_bytes()).hexdigest()


def observation(value: tuple[int, bytes, bytes]) -> dict:
    status, stdout, stderr = value
    return {
        "exit": status,
        "stdout_sha256": hashlib.sha256(stdout).hexdigest(),
        "stderr_sha256": hashlib.sha256(stderr).hexdigest(),
        "stdout_bytes": len(stdout),
        "stderr_bytes": len(stderr),
    }


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    for name in ("baseline", "candidate", "worker", "output"):
        parser.add_argument("--" + name, type=Path, required=True)
    args = parser.parse_args()
    baseline, candidate, worker = (
        getattr(args, name).resolve() for name in ("baseline", "candidate", "worker")
    )
    baseline_sha256 = sha256(baseline)
    if baseline_sha256 != BASELINE_SHA256:
        raise SystemExit(
            "baseline SHA-256 does not identify the official v0.9.0 "
            f"darwin-arm64 binary: got {baseline_sha256}, want {BASELINE_SHA256}"
        )
    report = {
        "schema": "dircue-v090-cli-comparison-1",
        "generated_at_utc": datetime.now(timezone.utc).isoformat(),
        "baseline_release": "v0.9.0",
        "baseline_sha256": baseline_sha256,
        "candidate_sha256": sha256(candidate),
        "worker_sha256": sha256(worker),
        "harness_sha256": sha256(Path(__file__)),
        "mismatches": [],
    }
    with tempfile.TemporaryDirectory(prefix="dircue-v090-compat-") as temporary:
        base = Path(temporary)
        environment, _flat, inherited = broad.v060.previous.load_legacy().fixture(base)
        environment.pop("DIRCUE_STRUCTURAL_WORKER", None)
        environment.update(NO_COLOR="1", LC_ALL="C")
        matrix, _languages = broad.v060.previous.cases(base, environment, inherited, worker)
        matrix += broad.v060.extra_cases(base, worker)
        matrix += broad.v060.declaration_cases(base, baseline, environment)
        if len(matrix) != EXPECTED_CASES:
            raise SystemExit(
                f"inherited compatibility matrix changed: got {len(matrix)} cases, "
                f"want {EXPECTED_CASES}"
            )
        for case_id, group, cwd, command in matrix:
            old = broad.v060.previous.capture(baseline, command, cwd, environment)
            new = broad.v060.previous.capture(candidate, command, cwd, environment)
            if old == new:
                continue
            report["mismatches"].append({
                "id": case_id,
                "group": group,
                "args": command,
                "exit_equal": old[0] == new[0],
                "stdout_equal": old[1] == new[1],
                "stderr_equal": old[2] == new[2],
                "baseline": observation(old),
                "candidate": observation(new),
            })
    report["total"] = len(matrix)
    report["changed"] = len(report["mismatches"])
    report["exact"] = report["total"] - report["changed"]
    report["changed_by_group"] = dict(sorted(Counter(
        row["group"] for row in report["mismatches"]
    ).items()))
    report["exit_changes"] = sum(not row["exit_equal"] for row in report["mismatches"])
    report["stdout_changes"] = sum(not row["stdout_equal"] for row in report["mismatches"])
    report["stderr_changes"] = sum(not row["stderr_equal"] for row in report["mismatches"])
    args.output.parent.mkdir(parents=True, exist_ok=True)
    args.output.write_text(json.dumps(report, indent=2, sort_keys=True) + "\n")
    print(json.dumps({key: report[key] for key in (
        "total", "exact", "changed", "exit_changes", "stdout_changes", "stderr_changes",
    )}))


if __name__ == "__main__":
    main()
