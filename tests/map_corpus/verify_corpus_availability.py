#!/usr/bin/env python3
"""Check that dircue map succeeds (exit 0, valid JSON) on every repo in a corpus root.

Run against a directory whose immediate subdirectories are repository clones:

    python3 tests/map_corpus/verify_corpus_availability.py \
        --binary bin/dircue \
        --root .cache/corpus \
        --output /tmp/availability.json

Each subdirectory is mapped once.  The script writes a JSON summary and exits
non-zero if any repo fails.  It never touches the network.
"""

import argparse
import json
import subprocess
import time
from pathlib import Path

SCHEMA_VERSION = "1.0.0"


def run_map(binary: Path, repo_dir: Path, timeout: int = 120):
    """Run `binary map --json repo_dir` and return a result dict.

    Returns a dict with keys:
        repo          (str)  – directory basename
        path          (str)  – absolute path
        exit_code     (int)  – process exit code
        duration_s    (float) – wall-clock seconds
        first_stderr  (str)  – first non-empty line of stderr, or ""
        schema_ok     (bool) – True if stdout is valid JSON with the expected schema_version
        error         (str)  – human-readable failure reason, or "" on success
    """
    repo = repo_dir.name
    started = time.monotonic()
    try:
        proc = subprocess.run(
            [str(binary), "map", "--json", str(repo_dir)],
            capture_output=True,
            text=True,
            timeout=timeout,
        )
    except subprocess.TimeoutExpired:
        elapsed = time.monotonic() - started
        return {
            "repo": repo,
            "path": str(repo_dir),
            "exit_code": -1,
            "duration_s": round(elapsed, 3),
            "first_stderr": "timeout",
            "schema_ok": False,
            "error": f"timed out after {timeout}s",
        }
    elapsed = time.monotonic() - started

    stderr_lines = [ln.strip() for ln in proc.stderr.splitlines() if ln.strip()]
    first_stderr = stderr_lines[0] if stderr_lines else ""

    schema_ok = False
    error = ""
    if proc.returncode != 0:
        error = f"exit {proc.returncode}: {first_stderr}"
    else:
        try:
            doc = json.loads(proc.stdout)
        except json.JSONDecodeError as exc:
            error = f"stdout is not valid JSON: {exc}"
        else:
            if doc.get("schema_version") != SCHEMA_VERSION:
                error = (
                    f"unexpected schema_version {doc.get('schema_version')!r}, "
                    f"want {SCHEMA_VERSION!r}"
                )
            else:
                schema_ok = True

    return {
        "repo": repo,
        "path": str(repo_dir),
        "exit_code": proc.returncode,
        "duration_s": round(elapsed, 3),
        "first_stderr": first_stderr,
        "schema_ok": schema_ok,
        "error": error,
    }


def check_corpus_root(binary: Path, root: Path, timeout: int = 120):
    """Return a list of result dicts, one per immediate subdirectory of root."""
    repos = sorted(p for p in root.iterdir() if p.is_dir())
    return [run_map(binary, repo, timeout=timeout) for repo in repos]


def summarise(results):
    """Return (passed, failed, summary_dict) from a list of result dicts."""
    passed = [r for r in results if r["schema_ok"] and r["exit_code"] == 0]
    failed = [r for r in results if not (r["schema_ok"] and r["exit_code"] == 0)]
    summary = {
        "total": len(results),
        "passed": len(passed),
        "failed": len(failed),
        "results": results,
    }
    return passed, failed, summary


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--binary", type=Path, required=True, help="Path to dircue binary")
    parser.add_argument(
        "--root",
        type=Path,
        action="append",
        dest="roots",
        metavar="DIR",
        required=True,
        help="Corpus root directory (repeatable)",
    )
    parser.add_argument("--output", type=Path, help="Write JSON summary to this file")
    parser.add_argument(
        "--timeout", type=int, default=120, help="Per-repo timeout in seconds (default: 120)"
    )
    args = parser.parse_args()

    binary = args.binary.resolve()
    if not binary.is_file():
        raise SystemExit(f"binary not found: {binary}")

    all_results = []
    for root in args.roots:
        root = root.resolve()
        if not root.is_dir():
            raise SystemExit(f"corpus root not found: {root}")
        results = check_corpus_root(binary, root, timeout=args.timeout)
        all_results.extend(results)

    passed, failed, summary = summarise(all_results)
    output_json = json.dumps(summary, indent=2)

    if args.output:
        args.output.write_text(output_json + "\n")

    # Always print a one-line status to stdout.
    status = "passed" if not failed else "failed"
    short = {
        "gate": "corpus-availability",
        "total": summary["total"],
        "passed": summary["passed"],
        "failed": summary["failed"],
        "status": status,
    }
    if failed:
        short["failures"] = [{"repo": r["repo"], "error": r["error"]} for r in failed]
    print(json.dumps(short))

    if failed:
        raise SystemExit(1)


if __name__ == "__main__":
    main()
