#!/usr/bin/env python3
"""Run hand-authored facts against an already-materialized pinned public corpus."""
import argparse
import json
import subprocess
import time
from pathlib import Path

from verify import entries, matches

HERE = Path(__file__).resolve().parent

def run(command, **kwargs):
    return subprocess.run(command, check=True, capture_output=True, text=True, **kwargs)

def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--binary", type=Path, required=True)
    parser.add_argument("--corpus-root", type=Path, required=True)
    parser.add_argument("--output", type=Path)
    args = parser.parse_args()
    manifest = json.loads((HERE / "public_expectations.json").read_text())
    results, assertions = [], 0
    for expected in manifest["repositories"]:
        source = args.corpus_root / expected["id"]
        if not source.is_dir():
            raise SystemExit(f"missing pinned source: {expected['id']}")
        commit = run(["git", "-C", str(source), "rev-parse", "HEAD"]).stdout.strip()
        if commit != expected["commit"]:
            raise SystemExit(f"{expected['id']}: commit {commit}, expected {expected['commit']}")
        started = time.monotonic()
        mapped = run([str(args.binary), "map", "--source", "git", "--json", str(source)])
        elapsed = time.monotonic() - started
        document = json.loads(mapped.stdout)
        items = entries(document)
        missing = [want for want in expected["must_include"] if not any(matches(item, want) for item in items)]
        if missing:
            raise SystemExit(f"{expected['id']}: missing={missing}")
        assertions += len(expected["must_include"])
        results.append({
            "id": expected["id"], "commit": commit, "seconds": round(elapsed, 6),
            "map_status": document["status"], "nodes": len(document["nodes"]),
            "edges": len(document["edges"]), "assertions": len(expected["must_include"])
        })
    report = {"gate":"pinned-public-map-corpus", "repositories":len(results), "assertions":assertions, "results":results}
    encoded = json.dumps(report, indent=2) + "\n"
    if args.output:
        args.output.parent.mkdir(parents=True, exist_ok=True)
        args.output.write_text(encoded)
    print(encoded, end="")

if __name__ == "__main__":
    main()
