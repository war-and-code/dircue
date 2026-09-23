#!/usr/bin/env python3
"""Accuracy card generator for dircue map questions.

Derives per-question precision and recall from the hand-labeled expectations in
tests/map_corpus/public_quality_expectations.json (run verify_public_quality.py
for the actual scores) and writes:
  - docs/ACCURACY.md
  - internal/atlas/accuracy_data.json (embedded in the binary)

Honesty notes baked in:
  - The labels in public_quality_expectations.json are path-scoped
    (evaluated_kinds × oracle_files). Only nodes whose evidence paths overlap
    the oracle_files set are evaluated; other map output is outside the
    evaluated universe.
  - Precision and recall are computed only over the evaluated_kinds per repo.
  - A zero-label count yields "no labels" rather than a made-up score.
  - These cards do not make claims about repos outside the evaluated set.
"""

import argparse
import json
import math
import subprocess
import time
from collections import defaultdict
from pathlib import Path

HERE = Path(__file__).resolve().parent
ROOT = HERE.parent.parent
QUALITY_EXPECTATIONS = ROOT / "tests" / "map_corpus" / "public_quality_expectations.json"
ACCURACY_MD = ROOT / "docs" / "ACCURACY.md"
ACCURACY_DATA = ROOT / "internal" / "atlas" / "accuracy_data.json"
SCHEMA_VERSION = "dircue-accuracy-cards-0.2"
SUFFICIENT_LABEL_THRESHOLD = 30  # minimum labels for a kind to be considered "sufficient"


def wilson_ci(successes: int, total: int, z: float = 1.96) -> tuple[float, float]:
    """Wilson score confidence interval for a proportion.

    Returns (lower, upper) in [0, 1]. Returns (0, 1) when total == 0.
    """
    if total == 0:
        return 0.0, 1.0
    p = successes / total
    denominator = 1 + z * z / total
    centre = (p + z * z / (2 * total)) / denominator
    spread = z * math.sqrt(p * (1 - p) / total + z * z / (4 * total * total)) / denominator
    return max(0.0, centre - spread), min(1.0, centre + spread)


def evidence_paths(item: dict) -> list[str]:
    return sorted({e.get("path") for e in item.get("evidence", []) if e.get("path")})


def normalize_node(node: dict) -> dict:
    return {
        "kind": node["kind"],
        "name": node.get("name"),
        "paths": node.get("paths", []),
        "properties": node.get("properties", {}),
    }


def node_key(node: dict) -> str:
    return json.dumps(normalize_node(node), sort_keys=True, separators=(",", ":"))


def run_dircue_map(binary: str, source: Path, source_type: str) -> dict:
    cmd = [binary, "map", "--json", str(source)]
    if source_type == "git":
        cmd[2:2] = ["--source", "git"]
    result = subprocess.run(cmd, check=True, capture_output=True, text=True)
    return json.loads(result.stdout)


def score_repo(expected: dict, document: dict) -> dict[str, dict]:
    """Score one repo against its expected nodes, per kind.

    Returns {kind: {tp, fp, fn, precision, recall, ci_lower, ci_upper}}.
    """
    # Build evaluated_paths and evaluated_kinds
    evaluated_kinds = set(expected.get("evaluated_kinds", []))
    evaluated_paths = {o["path"] for o in expected.get("oracle_files", [])}

    nodes_by_id = {n["id"]: n for n in document["nodes"]}

    def paths_of_node(node: dict) -> set[str]:
        paths = set(node.get("paths", []))
        for ev in node.get("evidence", []):
            if ev.get("path"):
                paths.add(ev["path"])
        return paths

    # Actual nodes: those in evaluated_kinds whose paths overlap oracle_files
    actual_nodes = [
        normalize_node(n)
        for n in document["nodes"]
        if n["kind"] in evaluated_kinds and evaluated_paths.intersection(paths_of_node(n))
    ]

    # Expected nodes
    expected_nodes = [
        normalize_node(n)
        for n in expected.get("expected_nodes", [])
        if n["kind"] in evaluated_kinds
    ]

    scores = {}
    for kind in sorted(evaluated_kinds):
        exp = [n for n in expected_nodes if n["kind"] == kind]
        act = [n for n in actual_nodes if n["kind"] == kind]
        exp_keys = {node_key(n) for n in exp}
        act_keys = {node_key(n) for n in act}
        tp = len(exp_keys & act_keys)
        fp = len(act_keys - exp_keys)
        fn = len(exp_keys - act_keys)
        precision = tp / (tp + fp) if (tp + fp) > 0 else None
        recall = tp / (tp + fn) if (tp + fn) > 0 else None
        p_lo, p_hi = wilson_ci(tp, tp + fp) if (tp + fp) > 0 else (None, None)
        r_lo, r_hi = wilson_ci(tp, tp + fn) if (tp + fn) > 0 else (None, None)
        ci_lower = round(p_lo, 4) if p_lo is not None else None
        sufficiency = "sufficient" if len(exp) >= SUFFICIENT_LABEL_THRESHOLD else "insufficient_labels"
        scores[kind] = {
            "tp": tp,
            "fp": fp,
            "fn": fn,
            "precision": round(precision, 4) if precision is not None else None,
            "recall": round(recall, 4) if recall is not None else None,
            "precision_ci_95": [round(p_lo, 4), round(p_hi, 4)] if p_lo is not None else None,
            "recall_ci_95": [round(r_lo, 4), round(r_hi, 4)] if r_lo is not None else None,
            "ci_lower": ci_lower,
            "sufficiency": sufficiency,
            "label_count": len(exp),
            "note": None,
        }
        if len(exp) == 0:
            scores[kind]["note"] = "no labels for this kind in this repo; precision and recall not computed"
        elif precision is None:
            scores[kind]["note"] = "no actual nodes for this kind; precision not computable"

    return scores


def aggregate_kind_scores(kind_scores_list: list[dict[str, dict]]) -> dict[str, dict]:
    """Aggregate per-repo kind scores into overall kind summaries.

    For each kind: sum TP/FP/FN across all repos that have labels.
    """
    totals: dict[str, dict] = defaultdict(lambda: {"tp": 0, "fp": 0, "fn": 0, "label_count": 0, "repo_count": 0})
    for kind_scores in kind_scores_list:
        for kind, s in kind_scores.items():
            totals[kind]["tp"] += s["tp"]
            totals[kind]["fp"] += s["fp"]
            totals[kind]["fn"] += s["fn"]
            totals[kind]["label_count"] += s["label_count"]
            if s["label_count"] > 0:
                totals[kind]["repo_count"] += 1

    result = {}
    for kind, t in sorted(totals.items()):
        tp, fp, fn = t["tp"], t["fp"], t["fn"]
        precision = tp / (tp + fp) if (tp + fp) > 0 else None
        recall = tp / (tp + fn) if (tp + fn) > 0 else None
        p_lo, p_hi = wilson_ci(tp, tp + fp) if (tp + fp) > 0 else (None, None)
        r_lo, r_hi = wilson_ci(tp, tp + fn) if (tp + fn) > 0 else (None, None)
        ci_lower = round(p_lo, 4) if p_lo is not None else None
        label_count = t["label_count"]
        sufficiency = "sufficient" if label_count >= SUFFICIENT_LABEL_THRESHOLD else "insufficient_labels"
        result[kind] = {
            "tp": tp,
            "fp": fp,
            "fn": fn,
            "precision": round(precision, 4) if precision is not None else None,
            "recall": round(recall, 4) if recall is not None else None,
            "precision_ci_95": [round(p_lo, 4), round(p_hi, 4)] if p_lo is not None else None,
            "recall_ci_95": [round(r_lo, 4), round(r_hi, 4)] if r_lo is not None else None,
            "ci_lower": ci_lower,
            "sufficiency": sufficiency,
            "label_count": label_count,
            "repo_count": t["repo_count"],
        }
    return result


def write_accuracy_md(cards: dict, output_path: Path) -> None:
    """Write docs/ACCURACY.md from accuracy cards."""
    lines = [
        "# dircue Map Accuracy",
        "",
        "> Generated by `make accuracy-cards` from hand-labeled ground truth in",
        "> `tests/map_corpus/public_quality_expectations.json`.",
        "> Do not edit by hand.",
        "",
        "## Scope and honesty",
        "",
        "These cards measure precision and recall for map questions over a",
        "**bounded, path-scoped subset** of the corpus in `public_quality_expectations.json`.",
        "For each repository, only nodes whose evidence paths overlap the `oracle_files`",
        "set are evaluated. Other map output — nodes evidenced by files outside that set —",
        "is outside the evaluated universe and is neither a true positive nor a false positive.",
        "",
        "**Precision** = TP / (TP + FP): of the nodes dircue reported in the evaluated set,",
        "what fraction was expected?",
        "",
        "**Recall** = TP / (TP + FN): of the nodes in the expected set, what fraction did",
        "dircue find?",
        "",
        "Confidence intervals use the Wilson score at 95%. A question with zero labels",
        "reports N/A — not a score of zero or one.",
        "",
        f"A kind is marked **`insufficient_labels`** when it has fewer than {SUFFICIENT_LABEL_THRESHOLD} labels.",
        "These results are directional only; the confidence intervals are wide.",
        "Broader hand-labeling is tracked in issue #75.",
        "",
        "## Per-question accuracy",
        "",
    ]

    overall = cards.get("overall", {})
    per_repo = cards.get("per_repo", [])
    evaluated_repos = cards.get("evaluated_repos", 0)

    lines += [
        f"Evaluated repositories: {evaluated_repos}",
        "",
        "| Question (kind) | Labels | 95% CI lower | Precision | Recall | Repos | Sufficiency |",
        "|-----------------|--------|--------------|-----------|--------|-------|-------------|",
    ]

    for kind, s in sorted(overall.items()):
        p = f"{s['precision']:.3f}" if s["precision"] is not None else "N/A"
        r = f"{s['recall']:.3f}" if s["recall"] is not None else "N/A"
        ci_lower = f"{s['ci_lower']:.2f}" if s.get("ci_lower") is not None else "N/A"
        sufficiency = s.get("sufficiency", "insufficient_labels")
        suf_label = f"**{sufficiency}**" if sufficiency == "insufficient_labels" else sufficiency
        lines.append(
            f"| {kind} | {s['label_count']} | {ci_lower} | {p} | {r} | {s['repo_count']} | {suf_label} |"
        )

    lines += [
        "",
        "## Per-repository breakdown",
        "",
    ]

    for repo_entry in per_repo:
        repo_id = repo_entry["repo_id"]
        commit = repo_entry["commit"][:12]
        lines += [
            f"### {repo_id} ({commit})",
            "",
            "| Kind | Precision | Recall | TP | FP | FN | Labels |",
            "|------|-----------|--------|----|----|----|----|",
        ]
        for kind, s in sorted(repo_entry["scores"].items()):
            p = f"{s['precision']:.3f}" if s["precision"] is not None else "N/A"
            r = f"{s['recall']:.3f}" if s["recall"] is not None else "N/A"
            lines.append(
                f"| {kind} | {p} | {r} | {s['tp']} | {s['fp']} | {s['fn']} | {s['label_count']} |"
            )
        if repo_entry.get("note"):
            lines += ["", f"*Note: {repo_entry['note']}*"]
        lines.append("")

    lines += [
        "## Reproduction",
        "",
        "```",
        "make accuracy-cards",
        "```",
        "",
        "Or directly:",
        "",
        "```",
        "python3 tests/atlas/accuracy.py \\",
        "    --binary bin/dircue \\",
        "    --corpus-root .cache/map-corpus \\",
        "    --output docs/ACCURACY.md \\",
        "    --data internal/atlas/accuracy_data.json",
        "```",
        "",
        "Labels: `tests/map_corpus/public_quality_expectations.json`",
        "",
        f"Generated: {time.strftime('%Y-%m-%dT%H:%M:%SZ', time.gmtime())}",
        "",
    ]

    output_path.parent.mkdir(parents=True, exist_ok=True)
    output_path.write_text("\n".join(lines) + "\n")


def write_accuracy_data(cards: dict, output_path: Path) -> None:
    """Write the compact JSON data file for embedding in the binary."""
    output_path.parent.mkdir(parents=True, exist_ok=True)
    output_path.write_text(json.dumps(cards, indent=2, separators=(",", ": ")) + "\n")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--binary", type=Path, required=True,
                        help="Path to the dircue binary")
    parser.add_argument("--corpus-root", type=Path, required=True,
                        help="Directory containing cloned corpus repositories")
    parser.add_argument("--output", type=Path, default=ACCURACY_MD,
                        help="Path to write docs/ACCURACY.md (default: %(default)s)")
    parser.add_argument("--data", type=Path, default=ACCURACY_DATA,
                        help="Path to write accuracy_data.json for embedding (default: %(default)s)")
    args = parser.parse_args()

    binary = str(args.binary.resolve())
    manifest = json.loads(QUALITY_EXPECTATIONS.read_text())

    per_repo_scores = []
    kind_scores_list = []
    evaluated_repos = 0
    notes = []

    for expected in manifest["repositories"]:
        source_type = expected.get("source_type", "git")
        if source_type == "git":
            source = args.corpus_root / expected["id"]
        elif source_type == "fixture":
            source = QUALITY_EXPECTATIONS.parent / expected["fixture"]
        else:
            notes.append(f"skipping {expected['id']}: unsupported source_type {source_type!r}")
            continue

        if not source.is_dir():
            notes.append(f"skipping {expected['id']}: not in corpus root {args.corpus_root}")
            continue

        if source_type == "git":
            commit = subprocess.run(
                ["git", "-C", str(source), "rev-parse", "HEAD"],
                check=True, capture_output=True, text=True,
            ).stdout.strip()
            if commit != expected["commit"]:
                raise SystemExit(
                    f"{expected['id']}: commit mismatch {commit[:12]} != {expected['commit'][:12]}"
                )
        else:
            commit = "fixture"

        print(f"Scoring {expected['id']}...", flush=True)
        document = run_dircue_map(binary, source, source_type)
        kind_scores = score_repo(expected, document)
        per_repo_scores.append({
            "repo_id": expected["id"],
            "commit": expected.get("commit", "fixture"),
            "scores": kind_scores,
            "note": None,
        })
        kind_scores_list.append(kind_scores)
        evaluated_repos += 1

    overall = aggregate_kind_scores(kind_scores_list)
    cards = {
        "schema_version": SCHEMA_VERSION,
        "generated_at_utc": time.strftime('%Y-%m-%dT%H:%M:%SZ', time.gmtime()),
        "evaluated_repos": evaluated_repos,
        "label_source": "tests/map_corpus/public_quality_expectations.json",
        "scope_note": (
            "Scores cover nodes evidenced by oracle_files paths only. "
            "Nodes outside those paths are not in the evaluated universe. "
            "Precision and recall are not computable for kinds with zero labels. "
            f"Kinds with fewer than {SUFFICIENT_LABEL_THRESHOLD} labels are marked "
            "insufficient_labels; results are directional. "
            "Broader hand-labeling is tracked in issue #75."
        ),
        "overall": overall,
        "per_repo": per_repo_scores,
    }

    if notes:
        cards["notes"] = notes

    write_accuracy_md(cards, args.output)
    write_accuracy_data(cards, args.data)
    print(f"Wrote {args.output}", flush=True)
    print(f"Wrote {args.data}", flush=True)


if __name__ == "__main__":
    main()
