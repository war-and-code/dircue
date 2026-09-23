#!/usr/bin/env python3
"""Compare legacy language JSON with pinned Linguist on the local public corpus.

The harness is on-demand, requires a prebuilt local reference image, disables
container networking, and never fetches or updates corpus repositories.
"""
import argparse
import hashlib
import json
import subprocess
from pathlib import Path

HERE = Path(__file__).resolve().parent
MAX_OUTPUT = 64 * 1024 * 1024


def capture(command, timeout):
    result = subprocess.run(command, capture_output=True, timeout=timeout)
    if result.returncode != 0:
        raise RuntimeError(f"exit {result.returncode}: {result.stderr[-4000:].decode(errors='replace')}")
    if len(result.stdout) > MAX_OUTPUT:
        raise RuntimeError(f"stdout exceeds {MAX_OUTPUT} bytes")
    return json.loads(result.stdout)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--binary", type=Path, required=True)
    parser.add_argument("--corpus-root", type=Path, required=True)
    parser.add_argument("--image", default="dircue-linguist:9.7.0")
    parser.add_argument("--timeout", type=int, default=300, help="Seconds per tool invocation")
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument("--candidate-commit", required=True,
                        help="Exact Git commit used to build --binary")
    args = parser.parse_args()
    binary = args.binary.resolve()
    version = subprocess.run(
        ["docker", "run", "--rm", "--network=none", args.image, "github-linguist", "--version"],
        check=True, capture_output=True, text=True,
    ).stdout.strip()
    if version != "github-linguist 9.7.0":
        raise SystemExit(f"reference must be github-linguist 9.7.0, got {version!r}")
    manifest = json.loads((HERE / "public_expectations.json").read_text())
    results = []
    for expected in manifest["repositories"]:
        source = (args.corpus_root / expected["id"]).resolve()
        commit = subprocess.run(
            ["git", "-C", str(source), "rev-parse", "HEAD"], check=True,
            capture_output=True, text=True,
        ).stdout.strip()
        if commit != expected["commit"]:
            raise SystemExit(f"{expected['id']}: commit {commit}, expected {expected['commit']}")
        actual = capture([str(binary), "--json", str(source)], args.timeout)
        reference = capture([
            "docker", "run", "--rm", "--network=none", "-v", f"{source}:/source:ro",
            args.image, "github-linguist", "--json", "/source",
        ], args.timeout)
        passed = actual == reference
        result = {"id": expected["id"], "commit": commit, "status": "passed" if passed else "failed"}
        if not passed:
            result["actual"] = actual
            result["reference"] = reference
        results.append(result)
        print(f"{result['status']} {expected['id']}")
    report = {
        "gate": "pinned-public-linguist-parity", "reference": version,
        "image": args.image, "repositories": len(results),
        "candidate": {
            "commit": args.candidate_commit,
            "sha256": hashlib.sha256(binary.read_bytes()).hexdigest(),
            "version": subprocess.run([str(binary), "--version"], check=True, capture_output=True, text=True).stdout.strip(),
        },
        "passed": sum(x["status"] == "passed" for x in results),
        "failed": sum(x["status"] == "failed" for x in results),
        "scope": "Exact legacy --json language aggregation only; map semantics are outside this differential.",
        "results": results,
    }
    args.output.parent.mkdir(parents=True, exist_ok=True)
    args.output.write_text(json.dumps(report, indent=2) + "\n")
    print(json.dumps({k: report[k] for k in ("repositories", "passed", "failed")}, indent=2))
    return 1 if report["failed"] else 0


if __name__ == "__main__":
    raise SystemExit(main())
