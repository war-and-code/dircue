#!/usr/bin/env python3
"""Compare source-first public labels with pinned local checkouts.

The labels cover selected files and positive facts. This reports recall and
coverage disagreements, not whole-repository precision. Missing facts are
recorded rather than silently turning the run into a passing accuracy gate.
"""

from __future__ import annotations

import argparse
import hashlib
import json
from pathlib import Path, PurePosixPath
import subprocess
import sys

import verify_golden


LABEL_DIR = Path(__file__).resolve().parent / "holdout_labels"
LABEL_FREEZE_COMMIT = "9fe9ceffed94598576e46b8d4c72b88951488975"
IDS = ("owasp_java", "owasp_python", "dart", "cobol")
# The first run against the frozen labels found 3, 0, 2 and 0 positives.
# After the #143 map work and the recorded label corrections every labeled
# positive is found; keep that floor so a lost fact fails the run. These four
# repositories now shape development, so they are regression checks, not an
# unseen estimate (see fresh_labels/ for that).
MIN_FOUND = {"owasp_java": 7, "owasp_python": 4, "dart": 7, "cobol": 0}
EXPECTED_POSITIVES = 18


def digest(data: bytes) -> str:
    return hashlib.sha256(data).hexdigest()


def command(args: list[str], *, timeout: int = 180) -> subprocess.CompletedProcess[bytes]:
    result = subprocess.run(args, capture_output=True, timeout=timeout)
    if result.returncode:
        raise RuntimeError(
            f"{args[0]} exited {result.returncode}: "
            f"{result.stderr.decode('utf-8', errors='replace')[:500]}"
        )
    return result


def verify_source(source: Path, label: dict) -> None:
    head = command(["git", "-C", str(source), "rev-parse", "HEAD"]).stdout.decode().strip()
    if head != label["commit"]:
        raise ValueError(f"{label['repo']}: checkout commit {head} != {label['commit']}")
    changes = command([
        "git", "-C", str(source), "status", "--porcelain=v1", "--untracked-files=all",
    ]).stdout
    if changes:
        raise ValueError(f"{label['repo']}: checkout has modified or untracked files")
    for item in label["oracle_files"]:
        path = PurePosixPath(item["path"])
        if path.is_absolute() or ".." in path.parts:
            raise ValueError(f"{label['repo']}: unsafe oracle path {path}")
        command(["git", "-C", str(source), "ls-files", "--error-unmatch", "--", str(path)])
        actual = digest((source / str(path)).read_bytes())
        if actual != item["sha256"]:
            raise ValueError(f"{label['repo']}: oracle digest mismatch: {path}")


def evaluate(name: str, source: Path, binary: Path, linguist_image: str | None) -> dict:
    label_path = LABEL_DIR / f"{name}.json"
    label_bytes = label_path.read_bytes()
    label = json.loads(label_bytes)
    verify_source(source, label)

    raw_map = command([str(binary), "map", "--json", str(source)]).stdout
    map_doc = json.loads(raw_map)
    map_source = map_doc.get("source", {})
    if map_source.get("mode") != "git" or map_source.get("commit") != label["commit"]:
        raise ValueError(f"{name}: map did not bind to the selected Git commit")

    scored = verify_golden.score_repo(label, map_doc)
    questions = {}
    map_coverage = {item.get("question"): item.get("status") for item in map_doc.get("coverage", [])}
    unknown_compatible = [
        name for name, expected in label.get("coverage", {}).items()
        if expected.get("status") == "unknown"
        and map_coverage.get(name) in ("unknown", "partial", "not_run")
    ]
    for item in scored.questions:
        if item.question == "coverage":
            questions[item.question] = {
                "matches": item.tp,
                "complete_overclaims": item.fp_items,
                "compatible_unknown": unknown_compatible,
                "other_disagreements": [
                    value for value in item.fn_items
                    if not any(value.startswith(f"question={name}:") for name in unknown_compatible)
                ],
            }
        else:
            questions[item.question] = {
                "labeled_positives": item.tp + item.fn,
                "found": item.tp,
                "missed": item.fn_items,
            }

    language = None
    if linguist_image:
        actual = command([str(binary), "--json", str(source)]).stdout
        reference = command([
            "docker", "run", "--rm", "--network", "none", "-v", f"{source}:/src:ro",
            "-w", "/src", linguist_image, "github-linguist", "--json",
        ], timeout=300).stdout
        actual_json = json.loads(actual)
        reference_json = json.loads(reference)
        language = {
            "matches_linguist": actual_json == reference_json,
            "dircue": actual_json,
            "linguist": reference_json,
        }

    return {
        "id": name,
        "repository": label["source"],
        "commit": label["commit"],
        "label_sha256": digest(label_bytes),
        "label_corrections": [item["change"] for item in label.get("corrections", [])],
        "oracle_files": [item["path"] for item in label["oracle_files"]],
        "map_sha256": digest(raw_map),
        "map_status": map_doc.get("status"),
        "questions": questions,
        "language_parity": language,
    }


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--binary", type=Path, required=True)
    parser.add_argument("--repo", action="append", default=[], metavar="ID=PATH",
                        help="Pinned checkout; provide each of owasp_java, owasp_python, dart, cobol")
    parser.add_argument("--linguist-image", help="Optional local Linguist container image")
    parser.add_argument("--output", type=Path, required=True)
    args = parser.parse_args()

    sources: dict[str, Path] = {}
    for pair in args.repo:
        name, sep, location = pair.partition("=")
        if not sep or name not in IDS or name in sources or not location:
            parser.error(f"invalid or repeated --repo {pair!r}")
        sources[name] = Path(location).resolve()
    if set(sources) != set(IDS):
        parser.error(f"--repo requires all four IDs: {', '.join(IDS)}")

    binary = args.binary.resolve()
    result = {
        "schema": "dircue-map-source-first-holdout-1",
        "label_freeze_commit": LABEL_FREEZE_COMMIT,
        "binary_sha256": digest(binary.read_bytes()),
        "interpretation": "Selected-file positive recall and coverage disagreement only; labels are not exhaustive, so precision is not estimated.",
        "repositories": [evaluate(name, sources[name], binary, args.linguist_image) for name in IDS],
    }
    result["labeled_positives"] = sum(
        question["labeled_positives"]
        for repo in result["repositories"]
        for name, question in repo["questions"].items()
        if name != "coverage"
    )
    result["positives_found"] = sum(
        question["found"]
        for repo in result["repositories"]
        for name, question in repo["questions"].items()
        if name != "coverage"
    )
    result["complete_overclaims"] = sum(
        len(repo["questions"]["coverage"]["complete_overclaims"])
        for repo in result["repositories"]
    )
    if result["labeled_positives"] != EXPECTED_POSITIVES:
        raise ValueError("source-first positive label count changed; review the oracle before scoring")
    args.output.parent.mkdir(parents=True, exist_ok=True)
    args.output.write_text(json.dumps(result, indent=2, sort_keys=True) + "\n")
    print(f"Found {result['positives_found']}/{result['labeled_positives']} labeled positives; "
          f"{result['complete_overclaims']} complete overclaims")
    for repo in result["repositories"]:
        parity = repo["language_parity"]
        if parity is not None:
            print(f"{repo['id']}: Linguist language JSON match={parity['matches_linguist']}")
    if result["complete_overclaims"]:
        return 1
    if any(
        sum(q["found"] for name, q in repo["questions"].items() if name != "coverage")
        < MIN_FOUND[repo["id"]]
        for repo in result["repositories"]
    ):
        return 1
    if any(repo["language_parity"] is not None and not repo["language_parity"]["matches_linguist"]
           for repo in result["repositories"]):
        return 1
    return 0


if __name__ == "__main__":
    try:
        sys.exit(main())
    except (OSError, ValueError, RuntimeError, subprocess.TimeoutExpired) as exc:
        print(f"holdout run failed: {exc}", file=sys.stderr)
        sys.exit(1)
