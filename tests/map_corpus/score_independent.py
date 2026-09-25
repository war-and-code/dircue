#!/usr/bin/env python3
"""Score source-first public holdouts without overstating their oracle scope.

The two label files were committed before the first map was generated. This
runner verifies their upstream commits, source hashes, and any claimed complete
file inventory before invoking dircue. It reports precision only for categories
whose labels enumerate every supported fact in the selected source boundary.
"""
from __future__ import annotations

import argparse
import hashlib
import json
import subprocess
import sys
from pathlib import Path

from verify_golden import score_repo


HERE = Path(__file__).resolve().parent
LABELS = HERE / "independent_labels"
SAMPLES = (
    ("flask-on-docker", "flask-on-docker.json", ""),
    ("gs-spring-boot-docker-complete", "gs-spring-boot-docker-complete.json", "complete"),
)
QUESTIONS = ("components", "deployables", "interfaces", "capabilities", "edges", "coverage")


def checked_output(*args: str) -> bytes:
    return subprocess.check_output(args, stderr=subprocess.PIPE, timeout=120)


def verify_source(label: dict, checkout: Path, scan_root: Path) -> None:
    actual_commit = checked_output("git", "-C", str(checkout), "rev-parse", "HEAD").decode().strip()
    if actual_commit != label["commit"]:
        raise ValueError(f"{label['repo']}: commit {actual_commit} differs from label {label['commit']}")
    oracle = {entry["path"] for entry in label["oracle_files"]}
    if len(oracle) != len(label["oracle_files"]):
        raise ValueError(f"{label['repo']}: duplicate oracle file")
    for entry in label["oracle_files"]:
        relative = Path(entry["path"])
        if relative.is_absolute() or ".." in relative.parts:
            raise ValueError(f"{label['repo']}: invalid oracle path {relative}")
        source = scan_root / relative
        if not source.is_file():
            raise ValueError(f"{label['repo']}: missing oracle file {relative}")
        digest = hashlib.sha256(source.read_bytes()).hexdigest()
        if digest != entry["sha256"]:
            raise ValueError(f"{label['repo']}: hash mismatch for {relative}")
    if any(label["evaluation_scope"][question] == "exhaustive" for question in QUESTIONS):
        tracked = set(checked_output("git", "-C", str(scan_root), "ls-files", "-z", "--cached").decode().rstrip("\0").split("\0"))
        if tracked != oracle:
            raise ValueError(f"{label['repo']}: exhaustive oracle inventory differs from tracked files: {sorted(tracked ^ oracle)}")


def scoped_question(question: object, scope: str) -> dict:
    if scope not in ("exhaustive", "targeted_recall_only"):
        raise ValueError(f"invalid evaluation scope {scope!r}")
    result = {
        "question": question.question,
        "scope": scope,
        "found": question.tp,
        "missed": question.fn,
        "targeted_recall": round(question.recall, 4) if question.recall is not None else None,
        "found_items": question.tp_items,
        "missed_items": question.fn_items,
    }
    if scope == "exhaustive" and question.question != "coverage":
        result.update({
            "false_positives": question.fp,
            "precision": round(question.precision, 4) if question.precision is not None else None,
            "false_positive_items": question.fp_items,
        })
    elif question.question != "coverage":
        result["unadjudicated_extra_observations"] = question.fp
    return result


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--binary", type=Path, required=True)
    parser.add_argument("--flask-checkout", type=Path, required=True)
    parser.add_argument("--spring-checkout", type=Path, required=True)
    parser.add_argument("--output", type=Path, required=True)
    args = parser.parse_args()
    binary = args.binary.resolve(strict=True)
    checkouts = (args.flask_checkout.resolve(strict=True), args.spring_checkout.resolve(strict=True))
    result = {
        "schema": "dircue-independent-source-holdout-0.1",
        "binary_sha256": hashlib.sha256(binary.read_bytes()).hexdigest(),
        "method": "source-first labels committed before first candidate map; precision only for exhaustive oracle categories",
        "repos": [],
    }
    for (name, label_file, subdir), checkout in zip(SAMPLES, checkouts):
        label_bytes = (LABELS / label_file).read_bytes()
        label = json.loads(label_bytes)
        if label["repo"] != name:
            raise ValueError(f"{label_file}: unexpected repository identifier")
        scan_root = checkout / subdir if subdir else checkout
        verify_source(label, checkout, scan_root)
        raw_map = checked_output(str(binary), "map", "--json", str(scan_root))
        map_doc = json.loads(raw_map)
        scores = score_repo(label, map_doc)
        questions = {q.question: scoped_question(q, label["evaluation_scope"][q.question]) for q in scores.questions}
        entry = {
            "repo": name,
            "upstream_commit": label["commit"],
            "scan_subdirectory": subdir or ".",
            "oracle_file_count": len(label["oracle_files"]),
            "label_sha256": hashlib.sha256(label_bytes).hexdigest(),
            "map_sha256": hashlib.sha256(raw_map).hexdigest(),
            "questions": questions,
        }
        result["repos"].append(entry)
        for question in QUESTIONS:
            q = questions[question]
            print(f"{name} {question}: found {q['found']}, missed {q['missed']}, "
                  f"scope {q['scope']}, precision {q.get('precision', 'not estimated')}")
    args.output.parent.mkdir(parents=True, exist_ok=True)
    args.output.write_text(json.dumps(result, indent=2, sort_keys=True) + "\n")
    return 0


if __name__ == "__main__":
    try:
        sys.exit(main())
    except (OSError, ValueError, KeyError, subprocess.SubprocessError, json.JSONDecodeError) as exc:
        sys.exit(f"independent holdout failed: {exc}")
