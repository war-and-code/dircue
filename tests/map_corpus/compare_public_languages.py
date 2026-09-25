#!/usr/bin/env python3
"""Compare legacy language modes with pinned Linguist on the local public corpus.

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
MODES = (
    ("json", ("--json",), True),
    ("breakdown-json", ("--breakdown", "--json"), True),
    ("breakdown-strategies", ("--breakdown", "--strategies"), False),
)
KNOWN_TEXT_DIFFERENCES = {
    ("aspnetcore", "breakdown-strategies"): (
        (
            "0.00%   12         SCSS\n0.00%   12         Less\n",
            "0.00%   12         Less\n0.00%   12         SCSS\n",
        ),
        (
            "  src/Components/test/testassets/TestContentPackage/MûltibyteÇharacterCompoñent.razor [Extension]\n",
            "  src/Components/test/testassets/TestContentPackage/MûltibyteÇharacterCompoñent.razor\n",
        ),
        (
            "  src/Middleware/StaticFiles/test/UnitTests/SubFolder/你好/default.html [Heuristics]\n",
            "  src/Middleware/StaticFiles/test/UnitTests/SubFolder/你好/default.html\n",
        ),
        (
            "  src/Middleware/StaticFiles/test/UnitTests/SubFolder/你好/世界/default.html [Heuristics]\n",
            "  src/Middleware/StaticFiles/test/UnitTests/SubFolder/你好/世界/default.html\n",
        ),
    ),
}


def known_text_difference(repo_id, mode, actual, reference):
    """Accept only the exact, pinned DISC-009 text differences."""
    changes = KNOWN_TEXT_DIFFERENCES.get((repo_id, mode))
    if not changes:
        return False
    normalized = actual
    for old, new in changes:
        old, new = old.encode(), new.encode()
        if normalized.count(old) != 1 or reference.count(new) != 1:
            return False
        normalized = normalized.replace(old, new, 1)
    return normalized == reference


def capture(command, timeout):
    result = subprocess.run(command, capture_output=True, timeout=timeout)
    if result.returncode != 0:
        raise RuntimeError(f"exit {result.returncode}: {result.stderr[-4000:].decode(errors='replace')}")
    if len(result.stdout) > MAX_OUTPUT:
        raise RuntimeError(f"stdout exceeds {MAX_OUTPUT} bytes")
    return result.stdout


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--binary", type=Path, required=True)
    parser.add_argument("--corpus-root", type=Path, required=True)
    parser.add_argument("--image", default="dircue-linguist:9.7.0")
    parser.add_argument("--id", action="append", default=[],
                        help="Compare only this pinned repository ID (repeatable; default: all)")
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
    all_repositories = manifest["repositories"]
    unknown_ids = set(args.id) - {entry["id"] for entry in all_repositories}
    if unknown_ids:
        raise SystemExit(f"unknown repository IDs: {', '.join(sorted(unknown_ids))}")
    repositories = [entry for entry in all_repositories if not args.id or entry["id"] in args.id]
    results = []
    for expected in repositories:
        source = (args.corpus_root / expected["id"]).resolve()
        commit = subprocess.run(
            ["git", "-C", str(source), "rev-parse", "HEAD"], check=True,
            capture_output=True, text=True,
        ).stdout.strip()
        if commit != expected["commit"]:
            raise SystemExit(f"{expected['id']}: commit {commit}, expected {expected['commit']}")
        for mode, flags, is_json in MODES:
            actual_bytes = capture([str(binary), *flags, str(source)], args.timeout)
            reference_bytes = capture([
                "docker", "run", "--rm", "--network=none", "-v", f"{source}:/source:ro",
                args.image, "github-linguist", *flags, "/source",
            ], args.timeout)
            # JSON object key order is not a language-detection difference; the
            # strategy breakdown is a human-facing, line-oriented contract.
            actual = json.loads(actual_bytes) if is_json else actual_bytes
            reference = json.loads(reference_bytes) if is_json else reference_bytes
            passed = actual == reference
            known = not passed and not is_json and known_text_difference(
                expected["id"], mode, actual_bytes, reference_bytes
            )
            status = "passed" if passed else "known-divergence" if known else "failed"
            result = {
                "id": expected["id"], "commit": commit, "mode": mode,
                "status": status,
                "actual_sha256": hashlib.sha256(actual_bytes).hexdigest(),
                "reference_sha256": hashlib.sha256(reference_bytes).hexdigest(),
            }
            if known:
                result["discrepancy"] = "DISC-009"
            elif not passed:
                result["actual"] = actual if is_json else actual_bytes.decode(errors="replace")
                result["reference"] = reference if is_json else reference_bytes.decode(errors="replace")
            results.append(result)
            print(f"{result['status']} {expected['id']} {mode}", flush=True)
    report = {
        "gate": "pinned-public-linguist-parity", "reference": version,
        "image": args.image, "repositories": len(repositories),
        "candidate": {
            "commit": args.candidate_commit,
            "sha256": hashlib.sha256(binary.read_bytes()).hexdigest(),
            "version": subprocess.run([str(binary), "--version"], check=True, capture_output=True, text=True).stdout.strip(),
        },
        "comparisons": len(results),
        "passed": sum(x["status"] == "passed" for x in results),
        "known_divergences": sum(x["status"] == "known-divergence" for x in results),
        "failed": sum(x["status"] == "failed" for x in results),
        "scope": "Legacy JSON aggregation, JSON breakdown, and text breakdown with strategy labels. Exact known text differences are named; map semantics are outside this differential.",
        "results": results,
    }
    args.output.parent.mkdir(parents=True, exist_ok=True)
    args.output.write_text(json.dumps(report, indent=2) + "\n")
    print(json.dumps({k: report[k] for k in ("repositories", "passed", "known_divergences", "failed")}, indent=2))
    return 1 if report["failed"] else 0


if __name__ == "__main__":
    raise SystemExit(main())
