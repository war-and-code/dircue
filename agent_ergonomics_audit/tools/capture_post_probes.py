#!/usr/bin/env python3
"""Replay the retained intent corpus without changing baseline evidence."""
import argparse
from datetime import datetime, timezone
import hashlib
import json
from pathlib import Path
import subprocess

ROOT = Path(__file__).resolve().parents[2]
AUDIT = ROOT / "agent_ergonomics_audit/audit"


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("binary", type=Path)
    args = parser.parse_args()
    binary = args.binary.resolve()
    digest = hashlib.sha256(binary.read_bytes()).hexdigest()
    rows = []
    for line in (AUDIT / "intent_inference_corpus_baseline.jsonl").read_text().splitlines():
        before = json.loads(line)
        argv = before["argv"][1:]
        run = subprocess.run([str(binary), *argv], cwd=ROOT / before["cwd"],
                             input="", capture_output=True, text=True, timeout=5)
        rows.append({"corpus_id": before["corpus_id"], "reason": before["reason"],
                     "argv": argv, "cwd": before["cwd"], "exit_code": run.returncode,
                     "stdout": run.stdout.replace(str(ROOT), "${TARGET}"),
                     "stderr": run.stderr.replace(str(ROOT), "${TARGET}"),
                     "baseline_exit_code": before["exit_code"],
                     "binary_sha256": digest, "ran_at": datetime.now(timezone.utc).isoformat()})
    if hashlib.sha256(binary.read_bytes()).hexdigest() != digest:
        raise AssertionError("binary changed during capture")
    path = AUDIT / "evidence/post_intent_probes.jsonl"
    path.write_text("".join(json.dumps(row, sort_keys=True) + "\n" for row in rows))
    print(json.dumps({"probes": len(rows), "same_exit_status": sum(row["exit_code"] == row["baseline_exit_code"] for row in rows)}))


if __name__ == "__main__":
    main()
