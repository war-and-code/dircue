#!/usr/bin/env python3
"""Manually materialize selected commit-pinned public corpus repositories.

This helper is intentionally not called by CI. It never updates an existing
checkout and requires each repository ID explicitly. Pins come from the map
corpus manifests unless --manifest names others, such as
tests/assessment/corpus/expectations.json.
"""
import argparse
import json
import subprocess
from pathlib import Path

HERE = Path(__file__).resolve().parent


def run(command):
    subprocess.run(command, check=True)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--destination", type=Path, required=True)
    parser.add_argument("--id", action="append", required=True,
                        help="Repository ID to fetch (repeat for more; there is deliberately no --all)")
    parser.add_argument("--manifest", type=Path, action="append",
                        help="Pin manifest to read (repeatable; defaults to the map corpus manifests)")
    args = parser.parse_args()
    paths = args.manifest or [HERE / "public_expectations.json", HERE / "public_quality_expectations.json"]
    manifests = [json.loads(path.read_text()) for path in paths]
    by_id = {}
    for manifest in manifests:
        for entry in manifest["repositories"]:
            if entry.get("source_type", "git") != "git":
                continue
            previous = by_id.get(entry["id"])
            if previous and (previous["url"], previous["commit"]) != (entry["url"], entry["commit"]):
                raise SystemExit(f"conflicting pins for repository ID: {entry['id']}")
            by_id[entry["id"]] = entry
    unknown = sorted(set(args.id) - by_id.keys())
    if unknown:
        raise SystemExit(f"unknown repository IDs: {', '.join(unknown)}")
    args.destination.mkdir(parents=True, exist_ok=True)
    for repo_id in dict.fromkeys(args.id):
        entry = by_id[repo_id]
        target = args.destination / repo_id
        if target.exists():
            if not (target / ".git").is_dir():
                raise SystemExit(f"refusing to replace non-Git path: {target}")
            head = subprocess.run(
                ["git", "-C", str(target), "rev-parse", "HEAD"], check=True,
                capture_output=True, text=True,
            ).stdout.strip()
            if head != entry["commit"]:
                raise SystemExit(f"{repo_id}: existing checkout is {head}, expected {entry['commit']}")
            print(f"{repo_id}: already pinned at {head}")
            continue
        target.mkdir()
        try:
            run(["git", "-C", str(target), "init", "--quiet"])
            run(["git", "-C", str(target), "remote", "add", "origin", entry["url"]])
            # A partial clone can leave local Git objects missing. Dircue never
            # fetches them, so the fixture must be complete before profiling.
            run(["git", "-C", str(target), "fetch", "--quiet", "--depth", "1", "origin", entry["commit"]])
            run(["git", "-C", str(target), "checkout", "--quiet", "--detach", "FETCH_HEAD"])
        except Exception:
            print(f"{repo_id}: fetch failed; remove incomplete directory before retrying: {target}")
            raise
        print(f"{repo_id}: pinned at {entry['commit']}")


if __name__ == "__main__":
    main()
