#!/usr/bin/env python3
"""Fetch exact public Git objects, without executing any project build scripts."""
import argparse
import concurrent.futures
import json
import os
from pathlib import Path
import subprocess

def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--directory", type=Path, default=Path(".cache/corpus"))
    args = parser.parse_args()
    manifest = json.loads(Path(__file__).with_name("corpus.json").read_text())
    args.directory.mkdir(parents=True, exist_ok=True)
    env = {**os.environ, "GIT_TERMINAL_PROMPT": "0", "GIT_LFS_SKIP_SMUDGE": "1",
           "GIT_CONFIG_NOSYSTEM": "1", "GIT_CONFIG_GLOBAL": os.devnull}

    def fetch(project):
        target = args.directory / project["name"]
        def git(*arguments):
            return subprocess.check_output(
                ["git", "-c", "core.hooksPath=/dev/null", "-C", str(target), *arguments],
                env=env, stderr=subprocess.PIPE, text=True, timeout=600).strip()
        if not (target / ".git").is_dir():
            target.mkdir(exist_ok=True)
            git("init", "--quiet")
            git("remote", "add", "origin", project["url"])
        if git("remote", "get-url", "origin") != project["url"]:
            raise RuntimeError(f"refusing unexpected existing remote: {target}")
        git("fetch", "--quiet", "--depth=1", "origin", project["ref"])
        wanted = git("rev-parse", "FETCH_HEAD^{commit}")
        if wanted != project["commit"]:
            raise RuntimeError(f"fetched object does not resolve to pinned commit: {target}")
        # Never discard existing edits in a user-accessible checkout.
        if git("status", "--porcelain"):
            raise RuntimeError(f"refusing to replace modified checkout: {target}")
        git("checkout", "--quiet", "--detach", wanted)
        record = {**project, "commit": git("rev-parse", "HEAD"), "tracked_files": len(git("ls-files").splitlines())}
        print(json.dumps(record), flush=True)
        return record

    with concurrent.futures.ThreadPoolExecutor(max_workers=4) as pool:
        records = list(pool.map(fetch, manifest["projects"]))
    (args.directory / "provenance.json").write_text(json.dumps(records, indent=2) + "\n")

if __name__ == "__main__":
    main()
