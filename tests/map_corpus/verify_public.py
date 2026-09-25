#!/usr/bin/env python3
"""Run hand-authored facts against an already-materialized pinned public corpus."""
import argparse
import json
import subprocess
import time
from pathlib import Path, PurePosixPath

from verify import entries, matches

HERE = Path(__file__).resolve().parent

def run(command, **kwargs):
    return subprocess.run(command, check=True, capture_output=True, text=True, **kwargs)

def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--binary", type=Path, required=True)
    parser.add_argument("--corpus-root", type=Path, required=True)
    parser.add_argument("--output", type=Path)
    args = parser.parse_args()
    manifest = json.loads((HERE / "public_expectations.json").read_text())
    results, assertions, asserted_questions, unknown_questions = [], 0, 0, 0
    for expected in manifest["repositories"]:
        source = args.corpus_root / expected["id"]
        if not source.is_dir():
            raise SystemExit(f"missing pinned source: {expected['id']}")
        commit = run(["git", "-C", str(source), "rev-parse", "HEAD"]).stdout.strip()
        if commit != expected["commit"]:
            raise SystemExit(f"{expected['id']}: commit {commit}, expected {expected['commit']}")
        source_files = run(["git", "-C", str(source), "ls-tree", "-r", "--name-only", "HEAD"]).stdout.splitlines()
        started = time.monotonic()
        mapped = run([str(args.binary), "map", "--source", "git", "--json", str(source)])
        elapsed = time.monotonic() - started
        document = json.loads(mapped.stdout)
        items = entries(document)
        question_results = []
        for question in expected["questions"]:
            disposition = question.get("disposition")
            oracle = question.get("oracle", {})
            if not oracle.get("source") or not oracle.get("url"):
                raise SystemExit(f"{expected['id']}/{question['id']}: incomplete oracle metadata")
            if expected["commit"] not in oracle["url"]:
                raise SystemExit(f"{expected['id']}/{question['id']}: oracle URL is not commit-pinned")
            source_path = oracle.get("path", "")
            if source_path:
                present = subprocess.run(
                    ["git", "-C", str(source), "cat-file", "-e", f"HEAD:{source_path}"],
                    capture_output=True,
                )
                if present.returncode != 0:
                    raise SystemExit(f"{expected['id']}/{question['id']}: oracle path absent: {source_path}")
            for basename in oracle.get("absent_basenames", []):
                found = [name for name in source_files if PurePosixPath(name).name == basename]
                if found:
                    raise SystemExit(f"{expected['id']}/{question['id']}: negative oracle contradicted by {found[:3]}")
            if disposition == "unknown":
                if not question.get("reason"):
                    raise SystemExit(f"{expected['id']}/{question['id']}: unknown question requires a reason")
                unknown_questions += 1
                question_results.append({
                    "id": question["id"], "status": "unknown", "assertions": 0,
                    "reason": question["reason"], "oracle": oracle["url"],
                    "recall": None, "precision": None,
                })
                continue
            if disposition != "asserted":
                raise SystemExit(f"{expected['id']}/{question['id']}: invalid disposition {disposition!r}")
            must_include = question.get("must_include", [])
            must_not_include = question.get("must_not_include", [])
            if not must_include and not must_not_include:
                raise SystemExit(f"{expected['id']}/{question['id']}: asserted question has no expectations")
            missing = [want for want in must_include if not any(matches(item, want) for item in items)]
            forbidden = [want for want in must_not_include if any(matches(item, want) for item in items)]
            if missing or forbidden:
                raise SystemExit(f"{expected['id']}/{question['id']}: missing={missing} forbidden={forbidden}")
            count = len(must_include) + len(must_not_include)
            assertions += count
            asserted_questions += 1
            question_results.append({
                "id": question["id"], "status": "passed", "assertions": count,
                "positive": len(must_include), "negative": len(must_not_include),
                "recall": 1.0 if must_include else None,
                "precision": None,
                "precision_reason": "not measured: expectations do not exhaustively label every emitted observation",
                "oracle": oracle["url"],
            })
        results.append({
            "id": expected["id"], "commit": commit, "seconds": round(elapsed, 6),
            "map_status": document["status"], "nodes": len(document["nodes"]),
            "edges": len(document["edges"]),
            "questions": question_results,
        })
    report = {
        "gate": "pinned-public-map-corpus", "repositories": len(results),
        "assertions": assertions, "asserted_questions": asserted_questions,
        "unknown_questions": unknown_questions, "results": results,
        "metric_scope": "Recall covers only cited positive facts. Precision is explicitly unmeasured because output labels are not exhaustive; negative assertions are targeted false-positive checks.",
    }
    encoded = json.dumps(report, indent=2) + "\n"
    if args.output:
        args.output.parent.mkdir(parents=True, exist_ok=True)
        args.output.write_text(encoded)
    print(encoded, end="")

if __name__ == "__main__":
    main()
