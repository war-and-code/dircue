#!/usr/bin/env python3
"""Measure exact precision and recall over a bounded, source-verified public corpus."""
import argparse
import hashlib
import json
import subprocess
import time
from collections import Counter
from pathlib import Path

HERE = Path(__file__).resolve().parent


def run(command):
    return subprocess.run(command, check=True, capture_output=True, text=True)


def evidence_paths(item):
    return sorted({entry.get("path") for entry in item.get("evidence", []) if entry.get("path")})


def normalize_node(node):
    return {
        "kind": node["kind"],
        "name": node.get("name"),
        "paths": node.get("paths", []),
        "properties": node.get("properties", {}),
    }


def normalize_edge(edge, nodes):
    return {
        "type": edge["type"],
        "from": normalize_node(nodes[edge["from"]]),
        "to": normalize_node(nodes[edge["to"]]),
        "properties": edge.get("properties", {}),
        "evidence_paths": evidence_paths(edge),
    }


def key(value):
    return json.dumps(value, sort_keys=True, separators=(",", ":"))


def score(expected, actual):
    expected_counts, actual_counts = Counter(map(key, expected)), Counter(map(key, actual))
    true_positive = sum((expected_counts & actual_counts).values())
    false_positive = sum((actual_counts - expected_counts).values())
    false_negative = sum((expected_counts - actual_counts).values())
    precision = true_positive / (true_positive + false_positive) if true_positive + false_positive else 1.0
    recall = true_positive / (true_positive + false_negative) if true_positive + false_negative else 1.0
    return true_positive, false_positive, false_negative, precision, recall


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--binary", type=Path, required=True)
    parser.add_argument("--corpus-root", type=Path, required=True)
    parser.add_argument("--output", type=Path)
    args = parser.parse_args()
    manifest = json.loads((HERE / "public_quality_expectations.json").read_text())
    binary = args.binary.resolve()
    totals = [0, 0, 0]
    type_totals = {record: [0, 0, 0] for record in ("node", "edge", "coverage")}
    results = []
    for expected in manifest["repositories"]:
        if expected["source_type"] == "git":
            source = (args.corpus_root / expected["id"]).resolve()
        elif expected["source_type"] == "fixture":
            source = (HERE / expected["fixture"]).resolve()
        else:
            raise SystemExit(f"{expected['id']}: invalid source_type {expected['source_type']!r}")
        if not source.is_dir():
            raise SystemExit(f"missing pinned source: {expected['id']}")
        commit = None
        if expected["source_type"] == "git":
            commit = run(["git", "-C", str(source), "rev-parse", "HEAD"]).stdout.strip()
            if commit != expected["commit"]:
                raise SystemExit(f"{expected['id']}: commit {commit}, expected {expected['commit']}")
        evaluated_paths = set()
        for oracle in expected["oracle_files"]:
            if expected["source_type"] == "git":
                content = subprocess.run(
                    ["git", "-C", str(source), "show", f"HEAD:{oracle['path']}"],
                    check=True, capture_output=True,
                ).stdout
            else:
                content = (source / oracle["path"]).read_bytes()
            digest = hashlib.sha256(content).hexdigest()
            if digest != oracle["sha256"]:
                raise SystemExit(f"{expected['id']}: oracle digest mismatch for {oracle['path']}")
            if commit and commit not in oracle["url"]:
                raise SystemExit(f"{expected['id']}: oracle URL is not commit-pinned: {oracle['url']}")
            evaluated_paths.add(oracle["path"])
        started = time.monotonic()
        command = [str(binary), "map", "--json", str(source)]
        if expected["source_type"] == "git":
            command[2:2] = ["--source", "git"]
        mapped = run(command)
        elapsed = time.monotonic() - started
        document = json.loads(mapped.stdout)
        if document.get("kind") != "map" or document.get("schema_version") != "1.0.0":
            raise SystemExit(f"{expected['id']}: wrong map identity")
        if document.get("status") not in {"complete", "partial", "unknown"}:
            raise SystemExit(f"{expected['id']}: invalid map status")
        mapped_source = document.get("source", {})
        if commit and (mapped_source.get("mode") != "git" or mapped_source.get("commit") != commit):
            raise SystemExit(f"{expected['id']}: map is not bound to the pinned commit")
        if not commit and mapped_source.get("mode") != "directory":
            raise SystemExit(f"{expected['id']}: fixture map source is not directory mode")
        nodes = {node["id"]: node for node in document["nodes"]}
        actual_nodes = [
            normalize_node(node) for node in document["nodes"]
            if node["kind"] in set(expected["evaluated_kinds"]) and evaluated_paths.intersection(evidence_paths(node))
        ]
        actual_edges = [
            normalize_edge(edge, nodes) for edge in document["edges"]
            if evaluated_paths.intersection(evidence_paths(edge))
        ]
        actual_coverage = [
            {"question": entry["question"], "status": entry["status"]}
            for entry in document["coverage"]
        ]
        expected_items = ([{"record": "node", **item} for item in expected["expected_nodes"]]
                          + [{"record": "edge", **item} for item in expected["expected_edges"]]
                          + [{"record": "coverage", **item} for item in expected["expected_coverage"]])
        actual_items = ([{"record": "node", **item} for item in actual_nodes]
                        + [{"record": "edge", **item} for item in actual_edges]
                        + [{"record": "coverage", **item} for item in actual_coverage])
        tp, fp, fn, precision, recall = score(expected_items, actual_items)
        totals[0] += tp; totals[1] += fp; totals[2] += fn
        breakdown = {}
        for record in type_totals:
            values = score(
                [item for item in expected_items if item["record"] == record],
                [item for item in actual_items if item["record"] == record],
            )
            for index in range(3):
                type_totals[record][index] += values[index]
            breakdown[record] = {
                "true_positive": values[0], "false_positive": values[1],
                "false_negative": values[2], "precision": values[3], "recall": values[4],
            }
        results.append({
            "id": expected["id"], "commit": commit, "seconds": round(elapsed, 6),
            "evaluated_paths": len(evaluated_paths), "expected_records": len(expected_items),
            "true_positive": tp, "false_positive": fp, "false_negative": fn,
            "precision": precision, "recall": recall, "record_types": breakdown,
        })
    tp, fp, fn = totals
    report = {
        "gate": "bounded-public-map-quality",
        "repositories": len(results), "evaluated_records": tp + fn,
        "true_positive": tp, "false_positive": fp, "false_negative": fn,
        "precision": tp / (tp + fp) if tp + fp else 1.0,
        "recall": tp / (tp + fn) if tp + fn else 1.0,
        "record_types": {
            record: {
                "true_positive": values[0], "false_positive": values[1],
                "false_negative": values[2],
                "precision": values[0] / (values[0] + values[1]) if values[0] + values[1] else 1.0,
                "recall": values[0] / (values[0] + values[2]) if values[0] + values[2] else 1.0,
            }
            for record, values in type_totals.items()
        },
        "metric_scope": manifest["metric_scope"], "results": results,
    }
    encoded = json.dumps(report, indent=2) + "\n"
    if args.output:
        args.output.parent.mkdir(parents=True, exist_ok=True)
        args.output.write_text(encoded)
    print(encoded, end="")
    if fp or fn:
        raise SystemExit(1)


if __name__ == "__main__":
    main()
