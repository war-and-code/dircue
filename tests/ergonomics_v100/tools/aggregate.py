#!/usr/bin/env python3
"""Aggregate independent rubric assessments without mixing baseline and candidate.

Each input is JSONL with surface_id, scorer_id, pass, rubric_version, scores,
and evidence. A disagreement of at least 300 points requires a separate
tiebreaker assessment; this tool does not invent one.
"""

import argparse
from collections import defaultdict
from datetime import datetime, timezone
import json
from pathlib import Path
from statistics import median


DIMENSIONS = (
    "agent_intuitiveness", "agent_ergonomics", "agent_ease_of_use",
    "output_parseability", "error_pedagogy", "intent_inference",
    "safety_with_recovery", "determinism_and_reproducibility",
    "self_documentation", "composability", "regression_resistance",
)


def aggregate(paths):
    groups = defaultdict(list)
    for path in paths:
        for line in path.read_text().splitlines():
            if line.strip():
                row = json.loads(line)
                groups[row["surface_id"]].append(row)
    if not groups:
        raise ValueError("no assessments supplied")
    versions = {(r["pass"], r["rubric_version"], r.get("target_sha"))
                for rows in groups.values() for r in rows}
    if len(versions) != 1:
        raise ValueError("assessments mix passes, source identities, or rubrics")
    result = []
    for surface, rows in sorted(groups.items()):
        scorers = [r["scorer_id"] for r in rows]
        if len(rows) < 2 or len(set(scorers)) != len(rows):
            raise ValueError(f"{surface}: need distinct independent scorers")
        for row in rows:
            if set(row["scores"]) != set(DIMENSIONS):
                raise ValueError(f"{surface}: incomplete dimension set")
            for dim, value in row["scores"].items():
                if value is None and row["scorer_id"] == "tiebreaker":
                    continue
                if type(value) is not int or not 0 <= value <= 1000:
                    raise ValueError(f"{surface}: invalid {dim} score")
                if value > 700 and not row.get("evidence", {}).get(dim):
                    raise ValueError(f"{surface}: unsourced high {dim} score")
        original = [r for r in rows if r["scorer_id"] != "tiebreaker"]
        if len(original) < 2:
            raise ValueError(f"{surface}: need two independent initial assessments")
        spreads = {d: max(r["scores"][d] for r in original)
                   - min(r["scores"][d] for r in original) for d in DIMENSIONS}
        tiebroken = "tiebreaker" in scorers
        for dim, spread in spreads.items():
            if spread >= 300 and not any(r["scorer_id"] == "tiebreaker" and r["scores"][dim] is not None for r in rows):
                raise ValueError(f"{surface}: independent tiebreak required for {dim}")
        combined = dict(rows[0])
        combined.pop("scorer_id", None)
        combined["scores"] = {
            d: int(median(r["scores"][d] for r in rows if r["scores"][d] is not None)) for d in DIMENSIONS
        }
        combined["weighted_score"] = sum(combined["scores"].values()) // len(DIMENSIONS)
        combined["score_confidence"] = {
            "spread_max": max(spreads.values()), "tiebroken": tiebroken,
        }
        combined["evidence"] = {
            dim: {"assessments": [
                {"scorer_id": row["scorer_id"], "evidence": row.get("evidence", {}).get(dim)}
                for row in rows if row["scores"][dim] is not None
            ]}
            for dim in DIMENSIONS
        }
        combined["assessments"] = [
            {"scorer_id": r["scorer_id"], "scores": r["scores"],
             "evidence": r.get("evidence", {}), "notes": r.get("notes", "")}
            for r in rows
        ]
        combined["scored_at"] = datetime.now(timezone.utc).isoformat()
        result.append(combined)
    return result


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("inputs", type=Path, nargs="+")
    parser.add_argument("--output", type=Path, required=True)
    args = parser.parse_args()
    try:
        rows = aggregate(args.inputs)
    except (ValueError, KeyError, OSError) as error:
        parser.exit(1, f"Cannot aggregate: {error}\n")
    args.output.write_text("".join(json.dumps(r, sort_keys=True) + "\n" for r in rows))
    print(f"Aggregated {len(rows)} surfaces into {args.output}")


if __name__ == "__main__":
    main()
