#!/usr/bin/env python3
"""Materialize pinned atlas corpus repositories into a local cache directory.

This harness uses the network (git clone) so that dircue never needs to. Each
repository is shallow-cloned at the exact pinned commit into CACHE_DIR/<id>/.
An existing checkout at the right commit is left alone. Never updates an
existing checkout to a different commit — remove the directory to re-fetch.

Exit 1 on any fetch failure or commit mismatch.
"""
import argparse
import json
import subprocess
import sys
from pathlib import Path

HERE = Path(__file__).resolve().parent
DEFAULT_CACHE = Path("/private/tmp/claude-501/-Users-gingeleski-Workspace-dircue/5f190bc5-4b78-405a-873f-236759143b25/scratchpad/atlas-cache")


def run(command, **kwargs):
    return subprocess.run(command, check=True, **kwargs)


def fetch_repo(entry: dict, destination: Path) -> None:
    repo_id = entry["id"]
    url = entry["url"]
    commit = entry["commit"]
    target = destination / repo_id

    if target.exists():
        if not (target / ".git").is_dir():
            raise SystemExit(f"{repo_id}: refusing to replace non-Git path: {target}")
        head = subprocess.run(
            ["git", "-C", str(target), "rev-parse", "HEAD"],
            check=True, capture_output=True, text=True,
        ).stdout.strip()
        if head == commit:
            print(f"{repo_id}: already at {commit[:12]}", flush=True)
            return
        raise SystemExit(
            f"{repo_id}: existing checkout is at {head[:12]}, expected {commit[:12]}; "
            f"remove {target} to re-fetch"
        )

    print(f"{repo_id}: fetching {url} at {commit[:12]}...", flush=True, end=" ")
    sys.stdout.flush()
    target.mkdir(parents=True)
    try:
        run(["git", "-C", str(target), "init", "--quiet"])
        run(["git", "-C", str(target), "remote", "add", "origin", url])
        run(["git", "-C", str(target), "fetch", "--quiet", "--depth", "1", "origin", commit])
        run(["git", "-C", str(target), "checkout", "--quiet", "--detach", "FETCH_HEAD"])
        head = subprocess.run(
            ["git", "-C", str(target), "rev-parse", "HEAD"],
            check=True, capture_output=True, text=True,
        ).stdout.strip()
        if head != commit:
            raise RuntimeError(f"HEAD is {head[:12]}, expected {commit[:12]}")
    except Exception:
        print("FAILED", flush=True)
        raise

    print("done", flush=True)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument(
        "--cache", type=Path, default=DEFAULT_CACHE,
        help="Directory to clone repositories into (default: %(default)s)",
    )
    parser.add_argument(
        "--id", action="append", dest="ids",
        help="Fetch only these repository IDs (repeat; default: all smoke repos)",
    )
    parser.add_argument(
        "--all", action="store_true",
        help="Fetch all corpus entries, not just the smoke subset",
    )
    parser.add_argument(
        "--smoke", action="store_true", default=False,
        help="Fetch the smoke subset only (default when no --id or --all)",
    )
    args = parser.parse_args()

    corpus = json.loads((HERE / "corpus.json").read_text())
    all_repos = {r["id"]: r for r in corpus["repositories"]}
    smoke_ids = set(corpus.get("smoke_ids", []))

    if args.ids:
        unknown = sorted(set(args.ids) - all_repos.keys())
        if unknown:
            raise SystemExit(f"unknown repository IDs: {', '.join(unknown)}")
        to_fetch = [all_repos[i] for i in dict.fromkeys(args.ids)]
    elif args.all:
        to_fetch = list(corpus["repositories"])
    else:
        # Default: smoke subset
        to_fetch = [r for r in corpus["repositories"] if r["id"] in smoke_ids]

    args.cache.mkdir(parents=True, exist_ok=True)
    failed = []
    for entry in to_fetch:
        try:
            fetch_repo(entry, args.cache)
        except SystemExit as e:
            print(f"ERROR: {e}", flush=True)
            failed.append(entry["id"])
        except Exception as e:
            print(f"ERROR fetching {entry['id']}: {e}", flush=True)
            failed.append(entry["id"])

    if failed:
        raise SystemExit(f"fetch failed for: {', '.join(failed)}")


if __name__ == "__main__":
    main()
