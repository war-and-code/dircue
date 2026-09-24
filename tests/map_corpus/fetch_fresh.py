#!/usr/bin/env python3
"""
fetch_fresh.py: Clone and checkout the pinned repos for the fresh final-check labels.

Reads repo commit pins from the fresh_labels/ directory and clones each repo
at the pinned commit into --dest.  Skips repos already present at the correct
commit.

Usage:
    python3 tests/map_corpus/fetch_fresh.py \\
        --labels tests/map_corpus/fresh_labels \\
        --dest .cache/fresh-repos

Then score with:
    python3 tests/map_corpus/verify_golden.py \\
        --labels tests/map_corpus/fresh_labels \\
        --maps   <dir with per-repo <id>.json dircue map files> \\
        --output tests/map_corpus/fresh_results.json
"""
from __future__ import annotations

import argparse
import json
import subprocess
import sys
from pathlib import Path

# Map label-file stem → GitHub URL.
# Update if a repo moves.
REPO_URLS: dict[str, str] = {
    "openmrs-core":   "https://github.com/OpenMRS/openmrs-core",
    "flask-realworld": "https://github.com/gothinkster/flask-realworld-example-app",
    "bloc":            "https://github.com/felangel/bloc",
    "node-realworld":  "https://github.com/gothinkster/node-express-realworld-example-app",
}


def git(*args: str, cwd: Path | None = None, check: bool = True) -> subprocess.CompletedProcess:
    return subprocess.run(["git", *args], cwd=cwd, check=check,
                         capture_output=True, text=True)


def clone_at_commit(url: str, dest: Path, commit: str) -> None:
    if dest.exists():
        r = git("rev-parse", "HEAD", cwd=dest, check=False)
        if r.returncode == 0 and r.stdout.strip().startswith(commit[:12]):
            print(f"  already at {commit[:12]}, skipping")
            return
        raise SystemExit(
            f"{dest} exists but is not at {commit[:12]}; "
            "move it aside or choose another --dest (existing directories are never deleted)"
        )

    print(f"  cloning {url}")
    dest.parent.mkdir(parents=True, exist_ok=True)
    try:
        git("clone", "--depth=1", "--no-tags", url, str(dest))
        r = git("rev-parse", "HEAD", cwd=dest, check=False)
        if r.returncode == 0 and r.stdout.strip() == commit:
            return
        # Pinned commit not at tip; fetch the full history to reach it
        git("fetch", "--unshallow", cwd=dest, check=False)
        git("checkout", commit, cwd=dest)
    except subprocess.CalledProcessError as exc:
        print(f"  ERROR: {exc.stderr.strip()}", file=sys.stderr)
        raise


def load_labels(labels_dir: Path) -> list[dict]:
    """Load all per-repo label files from labels_dir."""
    entries = []
    for label_file in sorted(labels_dir.glob("*.json")):
        try:
            data = json.loads(label_file.read_text())
        except json.JSONDecodeError as exc:
            print(f"WARNING: cannot parse {label_file}: {exc}", file=sys.stderr)
            continue
        # Expect a 'repo' key and a 'commit' key
        repo = data.get("repo") or label_file.stem
        commit = data.get("commit", "")
        if not commit:
            print(f"WARNING: no commit in {label_file}, skipping", file=sys.stderr)
            continue
        entries.append({"id": label_file.stem, "repo": repo, "commit": commit})
    return entries


def main() -> int:
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument("--labels", type=Path,
                    default=Path("tests/map_corpus/fresh_labels"),
                    help="Directory containing per-repo <id>.json label files")
    ap.add_argument("--dest", type=Path, default=Path(".cache/fresh-repos"),
                    help="Destination directory for repo clones")
    args = ap.parse_args()

    entries = load_labels(args.labels)
    if not entries:
        print("No label files found.", file=sys.stderr)
        return 1

    errors = 0
    for entry in entries:
        slug = entry["id"]
        commit = entry["commit"]
        url = REPO_URLS.get(slug)
        if not url:
            print(f"WARNING: no URL registered for {slug!r}, skipping", file=sys.stderr)
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
