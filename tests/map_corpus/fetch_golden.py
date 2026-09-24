#!/usr/bin/env python3
"""
fetch_golden.py: Clone and checkout the pinned repos for the golden gate.

Reads repo commit pins from golden_expectations.json and clones each repo
at the pinned commit into --dest.  Skips repos already present at the
correct commit.

Usage:
    python3 tests/map_corpus/fetch_golden.py \\
        --expectations tests/map_corpus/golden_expectations.json \\
        --dest .cache/golden-repos

Then run the gate:
    make golden GOLDEN_REPOS=.cache/golden-repos
"""
from __future__ import annotations

import argparse
import json
import subprocess
import sys
from pathlib import Path

# Map repo slug → GitHub URL.  Update if a repo moves.
REPO_URLS: dict[str, str] = {
    "aws-sam-java-rest":  "https://github.com/aws-samples/aws-sam-java-rest",
    "loki":               "https://github.com/grafana/loki",
    "mastodon":           "https://github.com/mastodon/mastodon",
    "ruff":               "https://github.com/astral-sh/ruff",
    "spring-petclinic":   "https://github.com/spring-projects/spring-petclinic",
    "superset":           "https://github.com/apache/superset",
    "terraform-aws-vpc":  "https://github.com/terraform-aws-modules/terraform-aws-vpc",
}


def git(*args: str, cwd: Path | None = None, check: bool = True) -> subprocess.CompletedProcess:
    return subprocess.run(["git", *args], cwd=cwd, check=check,
                         capture_output=True, text=True)


def clone_at_commit(url: str, dest: Path, commit: str) -> None:
    if dest.exists():
        # Check if already at the right commit
        r = git("rev-parse", "HEAD", cwd=dest, check=False)
        if r.returncode == 0 and r.stdout.strip().startswith(commit[:12]):
            print(f"  already at {commit[:12]}, skipping")
            return
        print(f"  wrong commit; recloning")
        import shutil
        shutil.rmtree(dest)

    print(f"  cloning {url}")
    dest.parent.mkdir(parents=True, exist_ok=True)
    # Shallow clone then fetch the exact commit
    try:
        git("clone", "--depth=1", "--no-tags", url, str(dest))
        # Try to checkout the pinned commit (may already be it on shallow clone)
        r = git("rev-parse", "HEAD", cwd=dest, check=False)
        if r.returncode == 0 and r.stdout.strip() == commit:
            return
        # Need a full fetch to reach the commit
        git("fetch", "--unshallow", cwd=dest, check=False)
        git("checkout", commit, cwd=dest)
    except subprocess.CalledProcessError as exc:
        print(f"  ERROR: {exc.stderr.strip()}", file=sys.stderr)
        raise


def main() -> int:
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument("--expectations", type=Path,
                    default=Path("tests/map_corpus/golden_expectations.json"),
                    help="Path to golden_expectations.json")
    ap.add_argument("--dest", type=Path, default=Path(".cache/golden-repos"),
                    help="Destination directory for repo clones")
    args = ap.parse_args()

    doc = json.loads(args.expectations.read_text())
    repos = doc.get("repos", [])

    errors = 0
    for entry in repos:
        slug = entry["repo"]
        commit = entry.get("commit", "")
        url = REPO_URLS.get(slug)
        if not url:
            print(f"WARNING: no URL for {slug}, skipping", file=sys.stderr)
            continue
        dest = args.dest / slug
        print(f"{slug} @ {commit[:12]}")
        try:
            clone_at_commit(url, dest, commit)
        except Exception as exc:
            print(f"  FAILED: {exc}", file=sys.stderr)
            errors += 1

    if errors:
        print(f"\n{errors} repo(s) failed to fetch.", file=sys.stderr)
        return 1
    print(f"\nAll repos ready in {args.dest}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
