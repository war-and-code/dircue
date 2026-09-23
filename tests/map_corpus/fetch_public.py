#!/usr/bin/env python3
"""Manually materialize selected commit-pinned public corpus repositories.

This helper is intentionally not called by CI. It never updates an existing
checkout and requires each repository ID explicitly.
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
    args = parser.parse_args()
    manifests = [
        json.loads((HERE / "public_expectations.json").read_text()),
        json.loads((HERE / "public_quality_expectations.json").read_text()),
    ]
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
            run(["git", "-C", str(target), "fetch", "--quiet", "--depth", "1", "--filter=blob:limit=2m", "origin", entry["commit"]])
            run(["git", "-C", str(target), "checkout", "--quiet", "--detach", "FETCH_HEAD"])
        except Exception:
            print(f"{repo_id}: fetch failed; remove incomplete directory before retrying: {target}")
            raise
        print(f"{repo_id}: pinned at {entry['commit']}")


if __name__ == "__main__":
    main()
