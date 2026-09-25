#!/usr/bin/env python3
"""Measure exact precision and recall over a bounded, source-verified public corpus."""
import argparse
import hashlib
import json
import re
import posixpath
import subprocess
import time
from collections import Counter
from pathlib import Path

HERE = Path(__file__).resolve().parent
MANIFEST_SCHEMA = "dircue-public-map-quality-0.1"
NODE_KINDS = {"content", "component", "deployable", "interface", "capability", "package", "tool_run"}
COVERAGE_STATUSES = {"complete", "partial", "unknown"}
COVERAGE_QUESTIONS = {
    "analyzer_coverage", "capabilities", "components", "content", "deployables",
    "interfaces", "packages", "routing", "source_binding",
}
KIND_QUESTION = {
    "content": "content", "component": "components", "deployable": "deployables",
    "interface": "interfaces", "capability": "capabilities", "package": "packages",
    "tool_run": "analyzer_coverage",
}
EDGE_QUESTION = {
    "contains": "components", "member_of": "components", "depends_on_local": "components",
    "builds": "deployables", "runs": "deployables", "exposes": "interfaces",
    "declares": "interfaces", "uses_capability": "capabilities", "packaged_in": "packages",
    "analyzed_by": "analyzer_coverage",
}
EDGE_TYPES = set(EDGE_QUESTION)
SHA256_RE = re.compile(r"^[0-9a-f]{64}$")
MISMATCH_SAMPLE_LIMIT = 10
MISMATCH_VALUE_BYTES = 2048


def run(command):
    return subprocess.run(command, check=True, capture_output=True, text=True)


def evidence_paths(item):
    return sorted({entry["path"] for entry in item.get("evidence", []) if entry.get("path")})


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
    precision = true_positive / (true_positive + false_positive) if true_positive + false_positive else None
    recall = true_positive / (true_positive + false_negative) if true_positive + false_negative else None
    return true_positive, false_positive, false_negative, precision, recall


def mismatch_report(expected, actual, sample_limit=MISMATCH_SAMPLE_LIMIT, value_bytes=MISMATCH_VALUE_BYTES):
    """Return exact mismatch counts and a bounded, explicit sample."""
    expected_counts, actual_counts = Counter(map(key, expected)), Counter(map(key, actual))
    expected_values = {key(item): item for item in expected}
    actual_values = {key(item): item for item in actual}
    missing_counts = expected_counts - actual_counts
    unexpected_counts = actual_counts - expected_counts

    def sample(counts, values):
        total = sum(counts.values())
        result = []
        for encoded in sorted(counts):
            item = values[encoded]
            if len(result) < sample_limit:
                if len(encoded.encode("utf-8")) > value_bytes:
                    item = {
                        "truncated_record": True,
                        "encoded_bytes": len(encoded.encode("utf-8")),
                        "preview": encoded[:value_bytes],
                    }
                result.extend([item] * min(counts[encoded], sample_limit - len(result)))
        return total, result

    missing_count, missing = sample(missing_counts, expected_values)
    unexpected_count, unexpected = sample(unexpected_counts, actual_values)
    return {
        "missing_count": missing_count,
        "unexpected_count": unexpected_count,
        "missing_records": missing,
        "unexpected_records": unexpected,
        "missing_records_omitted": missing_count - len(missing),
        "unexpected_records_omitted": unexpected_count - len(unexpected),
    }


def valid_repo_path(value, allow_root=True):
    """Return whether a source path is a normalized repo-relative POSIX path."""
    if not isinstance(value, str) or not value or "\\" in value or value.startswith("/"):
        return False
    normalized = posixpath.normpath(value)
    if normalized != value or normalized == ".." or normalized.startswith("../"):
        return False
    return normalized != "." or allow_root


def validate_manifest(manifest):
    """Reject empty or malformed universes that could produce a vacuous score."""
    if manifest.get("schema") != MANIFEST_SCHEMA:
        raise ValueError(f"unsupported manifest schema {manifest.get('schema')!r}")
    if not isinstance(manifest.get("metric_scope"), str) or not manifest["metric_scope"].strip():
        raise ValueError("manifest must explain metric_scope")
    repositories = manifest.get("repositories")
    if not isinstance(repositories, list) or not repositories:
        raise ValueError("manifest must contain a non-empty repositories list")
    ids = set()
    for repo in repositories:
        if not isinstance(repo, dict):
            raise ValueError("repository entries must be objects")
        rid = repo.get("id")
        if not isinstance(rid, str) or not rid or rid in ids:
            raise ValueError(f"repository id is empty or duplicated: {rid!r}")
        ids.add(rid)
        required = {"source_type", "evaluated_kinds", "oracle_files", "expected_nodes", "expected_edges", "expected_coverage"}
        missing = sorted(required - repo.keys())
        if missing:
            raise ValueError(f"{rid}: missing required manifest fields: {', '.join(missing)}")
        if not isinstance(repo["expected_nodes"], list) or not isinstance(repo["expected_edges"], list):
            raise ValueError(f"{rid}: expected_nodes and expected_edges must be lists")
        source_type = repo.get("source_type")
        if source_type not in {"git", "fixture"}:
            raise ValueError(f"{rid}: invalid source_type {source_type!r}")
        if source_type == "git":
            commit = repo.get("commit")
            if not isinstance(commit, str) or not re.fullmatch(r"[0-9a-f]{40}", commit):
                raise ValueError(f"{rid}: git source requires a full lowercase commit hash")
        elif not isinstance(repo.get("fixture"), str) or not repo["fixture"]:
            raise ValueError(f"{rid}: fixture source requires a fixture path")
        elif not valid_repo_path(repo["fixture"], allow_root=False):
            raise ValueError(f"{rid}: fixture path must remain inside the corpus harness")

        kinds = repo.get("evaluated_kinds")
        if not isinstance(kinds, list) or not kinds or len(kinds) != len(set(kinds)):
            raise ValueError(f"{rid}: evaluated_kinds must be a non-empty unique list")
        if not set(kinds) <= NODE_KINDS:
            raise ValueError(f"{rid}: unsupported evaluated kind(s): {sorted(set(kinds) - NODE_KINDS)}")
        if not any(kind in {"component", "deployable", "interface", "capability", "content"} for kind in kinds):
            raise ValueError(f"{rid}: evaluated_kinds contains no supported observation kind")

        oracle_files = repo.get("oracle_files")
        if not isinstance(oracle_files, list) or not oracle_files:
            raise ValueError(f"{rid}: oracle_files must define a non-empty scored source slice")
        path_sets = {"oracle_files": set(), "supporting_files": set()}
        for field in path_sets:
            entries = repo.get(field, [])
            if not isinstance(entries, list):
                raise ValueError(f"{rid}: {field} must be a list")
            for entry in entries:
                path, digest = entry.get("path"), entry.get("sha256")
                if not valid_repo_path(path, allow_root=False):
                    raise ValueError(f"{rid}: invalid {field} path {path!r}")
                if path in path_sets[field] or any(path in paths for key, paths in path_sets.items() if key != field):
                    raise ValueError(f"{rid}: duplicate oracle/supporting path {path!r}")
                if not isinstance(digest, str) or not SHA256_RE.fullmatch(digest):
                    raise ValueError(f"{rid}: invalid SHA-256 for {path}")
                path_sets[field].add(path)
        allowed_evidence = path_sets["oracle_files"] | path_sets["supporting_files"]
        def related_to_hashed_source(paths):
            return any(
                path == oracle or path == "."
                or oracle.startswith(path.rstrip("/") + "/")
                or path.startswith(oracle.rstrip("/") + "/")
                for path in paths for oracle in allowed_evidence
            )

        for node in repo["expected_nodes"]:
            if not isinstance(node, dict):
                raise ValueError(f"{rid}: expected node entries must be objects")
            node_paths = node.get("paths", [])
            if not node_paths or not all(valid_repo_path(path) for path in node_paths):
                raise ValueError(f"{rid}: expected node has invalid or empty paths")
            if not related_to_hashed_source(node_paths):
                raise ValueError(f"{rid}: expected node is not grounded in an oracle/supporting path")
        for edge in repo.get("expected_edges", []):
            if not isinstance(edge, dict):
                raise ValueError(f"{rid}: expected edge entries must be objects")
            if edge.get("type") is None or edge.get("from") is None or edge.get("to") is None:
                raise ValueError(f"{rid}: expected edge must declare type and both endpoints")
            evidence = edge.get("evidence_paths")
            if not isinstance(evidence, list) or not evidence:
                raise ValueError(f"{rid}: expected edge lacks explicit evidence_paths")
            if not set(evidence) <= allowed_evidence:
                raise ValueError(f"{rid}: expected edge evidence is not hash-pinned: {sorted(set(evidence) - allowed_evidence)}")
            if not set(evidence) & path_sets["oracle_files"]:
                raise ValueError(f"{rid}: expected edge has no evidence in its scored oracle slice")
            for side in ("from", "to"):
                endpoint = edge[side]
                endpoint_paths = endpoint.get("paths", []) if isinstance(endpoint, dict) else []
                if not endpoint_paths or not all(valid_repo_path(path) for path in endpoint_paths):
                    raise ValueError(f"{rid}: expected edge {side} endpoint has invalid or empty paths")
                if not related_to_hashed_source(endpoint_paths):
                    raise ValueError(f"{rid}: expected edge {side} endpoint is not grounded in hashed source")

        coverage = repo.get("expected_coverage")
        if not isinstance(coverage, list):
            raise ValueError(f"{rid}: expected_coverage must be a list")
        if any(not isinstance(item, dict) for item in coverage):
            raise ValueError(f"{rid}: expected coverage entries must be objects")
        questions = [item.get("question") for item in coverage]
        if len(questions) != len(set(questions)) or set(questions) != COVERAGE_QUESTIONS:
            raise ValueError(f"{rid}: expected_coverage must list every known question exactly once")
        for item in coverage:
            if item.get("scope", ".") != ".":
                raise ValueError(f"{rid}: expected coverage scope must be repository root")
            if item.get("status") not in COVERAGE_STATUSES:
                raise ValueError(f"{rid}: invalid expected coverage status {item.get('status')!r}")

        for field in ("expected_nodes", "expected_edges"):
            records = repo.get(field)
            if not isinstance(records, list):
                raise ValueError(f"{rid}: {field} must be a list")
            if field == "expected_nodes":
                for node in records:
                    if node.get("kind") not in kinds:
                        raise ValueError(f"{rid}: expected node kind {node.get('kind')!r} is outside evaluated_kinds")
            elif any(edge["type"] not in EDGE_TYPES for edge in records):
                raise ValueError(f"{rid}: expected edge has an unsupported relationship type")
        if not any(repo.get(field) for field in ("expected_nodes", "expected_edges")):
            raise ValueError(f"{rid}: empty node/edge universe would make precision/recall vacuous")
        if rid == "microservices-demo":
            required_populations = {"component_roots", "dockerfiles", "workflows"}
            populations = repo.get("whole_repo_paths")
            if not isinstance(populations, dict) or set(populations) != required_populations:
                raise ValueError(f"{rid}: full-repository guards must enumerate roots, Dockerfiles, and workflows")
            for population, paths in populations.items():
                if not isinstance(paths, list) or not paths or len(paths) != len(set(paths)):
                    raise ValueError(f"{rid}: {population} guard must be a non-empty unique path list")
                if any(not valid_repo_path(path, allow_root=False) for path in paths):
                    raise ValueError(f"{rid}: {population} guard contains an invalid path")


def validate_document(document, repo_id):
    """Check structural invariants before using a document as a scoring oracle."""
    if not isinstance(document, dict):
        raise ValueError(f"{repo_id}: map document must be an object")
    nodes = document.get("nodes")
    edges = document.get("edges")
    coverage = document.get("coverage")
    if not isinstance(nodes, list) or not isinstance(edges, list) or not isinstance(coverage, list):
        raise ValueError(f"{repo_id}: map nodes, edges, and coverage must be arrays")
    if any(not isinstance(node, dict) for node in nodes):
        raise ValueError(f"{repo_id}: map node entries must be objects")
    node_ids = [node.get("id") for node in nodes]
    if any(not isinstance(node_id, str) or not node_id for node_id in node_ids) or len(node_ids) != len(set(node_ids)):
        raise ValueError(f"{repo_id}: map node IDs must be non-empty and unique")
    by_id = {node["id"]: node for node in nodes}
    def check_evidence(item, label):
        evidence = item.get("evidence")
        if not isinstance(evidence, list) or not evidence:
            raise ValueError(f"{repo_id}: {label} lacks evidence")
        for entry in evidence:
            if not isinstance(entry, dict) or not valid_repo_path(entry.get("path")):
                raise ValueError(f"{repo_id}: {label} has missing or invalid evidence path")
    for node in nodes:
        if node.get("kind") not in NODE_KINDS:
            raise ValueError(f"{repo_id}: node has invalid kind {node.get('kind')!r}")
        paths = node.get("paths")
        if not isinstance(paths, list) or not paths or any(not valid_repo_path(path) for path in paths):
            raise ValueError(f"{repo_id}: node {node.get('id')} has missing or invalid paths")
        check_evidence(node, f"node {node.get('id')}")
        for path in evidence_paths(node):
            if not valid_repo_path(path):
                raise ValueError(f"{repo_id}: node {node['id']} has invalid evidence path {path!r}")
    for edge in edges:
        if not isinstance(edge, dict):
            raise ValueError(f"{repo_id}: map edge entries must be objects")
        if edge.get("type") not in EDGE_TYPES:
            raise ValueError(f"{repo_id}: edge has invalid relationship type {edge.get('type')!r}")
        if edge.get("from") not in by_id or edge.get("to") not in by_id:
            raise ValueError(f"{repo_id}: edge references a missing endpoint")
        check_evidence(edge, "edge")
        for path in evidence_paths(edge):
            if not valid_repo_path(path):
                raise ValueError(f"{repo_id}: edge has invalid evidence path {path!r}")
    if any(not isinstance(item, dict) for item in coverage):
        raise ValueError(f"{repo_id}: map coverage entries must be objects")
    coverage_keys = [(item.get("question"), item.get("scope")) for item in coverage]
    if any(not question or not scope for question, scope in coverage_keys):
        raise ValueError(f"{repo_id}: coverage entries need question and scope")
    if len(coverage_keys) != len(set(coverage_keys)):
        raise ValueError(f"{repo_id}: duplicate coverage question/scope")
    if {question for question, scope in coverage_keys if scope == "."} != COVERAGE_QUESTIONS:
        raise ValueError(f"{repo_id}: root coverage questions are incomplete or unknown")
    if any(item.get("status") not in COVERAGE_STATUSES for item in coverage):
        raise ValueError(f"{repo_id}: coverage entry has invalid status")
    return by_id


def collect_scored_items(expected, document, nodes, evaluated_paths):
    """Apply the declared source slice consistently to nodes and evidence-backed edges."""
    allowed_kinds = set(expected["evaluated_kinds"])
    actual_nodes = [
        normalize_node(node) for node in document["nodes"]
        if node["kind"] in allowed_kinds and evaluated_paths.intersection(evidence_paths(node))
    ]
    actual_edges = [
        normalize_edge(edge, nodes) for edge in document["edges"]
        if evaluated_paths.intersection(evidence_paths(edge))
    ]
    actual_coverage = [
        {"question": entry["question"], "scope": entry["scope"], "status": entry["status"]}
        for entry in document["coverage"]
    ]
    expected_items = (
        [{"record": "node", **item} for item in expected["expected_nodes"]]
        + [{"record": "edge", **item} for item in expected["expected_edges"]]
        + [{"record": "coverage", "scope": item.get("scope", "."), **item}
           for item in expected["expected_coverage"]]
    )
    actual_items = (
        [{"record": "node", **item} for item in actual_nodes]
        + [{"record": "edge", **item} for item in actual_edges]
        + [{"record": "coverage", **item} for item in actual_coverage]
    )
    return expected_items, actual_items


def question_bucket(item):
    """Assign every scored fact to the map question it answers."""
    if item["record"] == "node":
        return KIND_QUESTION[item["kind"]]
    if item["record"] == "edge":
        if item["type"] == "contains":
            endpoint_kinds = {item["from"]["kind"], item["to"]["kind"]}
            if endpoint_kinds == {"content"}:
                return "content"
            if endpoint_kinds == {"component"}:
                return "components"
            raise ValueError(f"contains edge has unclassified endpoint kinds: {sorted(endpoint_kinds)}")
        return EDGE_QUESTION[item["type"]]
    raise ValueError(f"record {item.get('record')!r} is not a semantic fact")


def require_scored_universe(results, totals):
    if not results or sum(totals) == 0:
        raise ValueError("public quality corpus has no scored facts; refusing a vacuous pass")


def verify_whole_repo_paths(expected, document):
    """Check complete source-enumerated populations beyond the scored file slice."""
    required = expected.get("whole_repo_paths", {})
    if not required:
        return
    observed = {
        "component_roots": {
            node.get("properties", {}).get("root") for node in document["nodes"]
            if node["kind"] == "component"
            and node.get("properties", {}).get("root", "").startswith("src/")
            and node.get("properties", {}).get("root", "").count("/") == 1
        },
        "dockerfiles": {
            node["paths"][0] for node in document["nodes"]
            if node["kind"] == "deployable" and node.get("properties", {}).get("provider") == "dockerfile"
            and node["paths"][0].endswith("/Dockerfile")
        },
        "workflows": {
            node["paths"][0] for node in document["nodes"]
            if node["kind"] == "deployable" and node.get("properties", {}).get("provider") == "github-actions"
        },
    }
    for population, paths in required.items():
        if population not in observed:
            raise SystemExit(f"{expected['id']}: unknown whole-repo population {population}")
        missing = sorted(set(paths) - observed[population])
        unexpected = sorted(observed[population] - set(paths))
        if missing or unexpected:
            raise SystemExit(
                f"{expected['id']}: {population} differs from pinned source: "
                f"missing={missing}, unexpected={unexpected}"
            )
        if len(paths) != len(set(paths)):
            raise SystemExit(f"{expected['id']}: {population} oracle contains duplicate paths")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--binary", type=Path, required=True)
    parser.add_argument("--corpus-root", type=Path, required=True)
    parser.add_argument("--output", type=Path)
    args = parser.parse_args()
    manifest = json.loads((HERE / "public_quality_expectations.json").read_text())
    try:
        validate_manifest(manifest)
    except (TypeError, AttributeError, ValueError) as exc:
        raise SystemExit(f"invalid public quality manifest: {exc}") from exc
    binary = args.binary.resolve()
    totals = [0, 0, 0]
    type_totals = {record: [0, 0, 0] for record in ("node", "edge", "coverage")}
    question_totals = {question: [0, 0, 0] for question in sorted(COVERAGE_QUESTIONS)}
    coverage_totals = {question: [0, 0, 0] for question in sorted(COVERAGE_QUESTIONS)}
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
        for oracle in expected["oracle_files"] + expected.get("supporting_files", []):
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
            if oracle in expected["oracle_files"]:
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
        try:
            nodes = validate_document(document, expected["id"])
        except (TypeError, AttributeError, ValueError) as exc:
            raise SystemExit(str(exc)) from exc
        verify_whole_repo_paths(expected, document)
        expected_items, actual_items = collect_scored_items(expected, document, nodes, evaluated_paths)
        tp, fp, fn, precision, recall = score(expected_items, actual_items)
        mismatches = mismatch_report(expected_items, actual_items)
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
        question_breakdown = {}
        for question in question_totals:
            values = score(
                [item for item in expected_items if item["record"] != "coverage" and question_bucket(item) == question],
                [item for item in actual_items if item["record"] != "coverage" and question_bucket(item) == question],
            )
            for index in range(3):
                question_totals[question][index] += values[index]
            question_breakdown[question] = {
                "true_positive": values[0], "false_positive": values[1],
                "false_negative": values[2], "precision": values[3], "recall": values[4],
            }
            values = score(
                [item for item in expected_items if item["record"] == "coverage" and item["question"] == question],
                [item for item in actual_items if item["record"] == "coverage" and item["question"] == question],
            )
            for index in range(3):
                coverage_totals[question][index] += values[index]
        results.append({
            "id": expected["id"], "commit": commit, "seconds": round(elapsed, 6),
            "evaluated_paths": len(evaluated_paths), "expected_records": len(expected_items),
            "true_positive": tp, "false_positive": fp, "false_negative": fn,
            "precision": precision, "recall": recall, "record_types": breakdown,
            "questions": question_breakdown,
            "missing_records": mismatches["missing_records"],
            "unexpected_records": mismatches["unexpected_records"],
            "missing_records_omitted": mismatches["missing_records_omitted"],
            "unexpected_records_omitted": mismatches["unexpected_records_omitted"],
        })
    tp, fp, fn = totals
    try:
        require_scored_universe(results, totals)
    except ValueError as exc:
        raise SystemExit(str(exc)) from exc
    report = {
        "gate": "bounded-public-map-quality",
        "repositories": len(results), "evaluated_records": tp + fn,
        "true_positive": tp, "false_positive": fp, "false_negative": fn,
        "precision": tp / (tp + fp) if tp + fp else None,
        "recall": tp / (tp + fn) if tp + fn else None,
        "record_types": {
            record: {
                "true_positive": values[0], "false_positive": values[1],
                "false_negative": values[2],
                "precision": values[0] / (values[0] + values[1]) if values[0] + values[1] else None,
                "recall": values[0] / (values[0] + values[2]) if values[0] + values[2] else None,
            }
            for record, values in type_totals.items()
        },
        "questions": {
            question: {
                "true_positive": values[0], "false_positive": values[1],
                "false_negative": values[2],
                "precision": values[0] / (values[0] + values[1]) if values[0] + values[1] else None,
                "recall": values[0] / (values[0] + values[2]) if values[0] + values[2] else None,
            }
            for question, values in question_totals.items()
        },
        "coverage_questions": {
            question: {
                "true_positive": values[0], "false_positive": values[1],
                "false_negative": values[2],
                "precision": values[0] / (values[0] + values[1]) if values[0] + values[1] else None,
                "recall": values[0] / (values[0] + values[2]) if values[0] + values[2] else None,
            }
            for question, values in coverage_totals.items()
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
