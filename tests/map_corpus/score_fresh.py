#!/usr/bin/env python3
"""Score dircue maps against the fresh blind labels.

The labels in fresh_labels/ were written from source before any #143 map
change was run on these repositories, and frozen in FREEZE_COMMIT. Later
source-checked corrections are recorded in each file's `corrections`. By
default this scores the current labels; `--frozen` scores the labels exactly
as frozen, which is the unseen measurement.

Fetch the pinned checkouts with fetch_fresh.py. The scorer never uses the
network.
"""

from __future__ import annotations

import argparse
import json
from pathlib import Path
import subprocess
import sys
import tempfile

HERE = Path(__file__).resolve().parent
LABEL_DIR = HERE / "fresh_labels"
FREEZE_COMMIT = "c976461048b949a4e3b29f965d12ea0dabb32cc5"


def current_labels() -> list[dict]:
    return [json.loads(path.read_text()) for path in sorted(LABEL_DIR.glob("*.json"))]


def frozen_labels() -> list[dict]:
    root = HERE.parents[1]
    relative = LABEL_DIR.relative_to(root).as_posix()
    names = subprocess.run(
        ["git", "-C", str(root), "ls-tree", "--name-only", FREEZE_COMMIT, relative + "/"],
        capture_output=True, text=True, check=True,
    ).stdout.split()
    labels = [
        json.loads(subprocess.run(
            ["git", "-C", str(root), "show", f"{FREEZE_COMMIT}:{name}"],
            capture_output=True, text=True, check=True,
        ).stdout)
        for name in sorted(names) if name.endswith(".json")
    ]
    if not labels:
        raise SystemExit(f"no frozen labels found at {FREEZE_COMMIT}")
    return labels


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--binary", type=Path, required=True)
    parser.add_argument("--repos", type=Path, required=True, help="Directory of pinned checkouts from fetch_fresh.py")
    parser.add_argument("--frozen", action="store_true", help=f"Score the labels as frozen in {FREEZE_COMMIT}")
    parser.add_argument("--output", type=Path)
    args = parser.parse_args()

    labels = frozen_labels() if args.frozen else current_labels()
    with tempfile.TemporaryDirectory(prefix="dircue-fresh-") as temporary:
        combined = Path(temporary) / "labels.json"
        combined.write_text(json.dumps({
            "labeled_by": "fresh blind labels (tests/map_corpus/fresh_labels)",
            "description": "frozen" if args.frozen else "current, with recorded corrections",
            "repos": labels,
        }))
        command = [
            sys.executable, str(HERE / "verify_golden.py"),
            "--labels", str(combined), "--binary", str(args.binary),
            "--repos", str(args.repos), "--no-gate",
        ]
        if args.output:
            command += ["--output", str(args.output)]
        return subprocess.run(command).returncode


if __name__ == "__main__":
    sys.exit(main())
