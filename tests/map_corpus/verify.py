#!/usr/bin/env python3
"""Verify hand-written facts against candidate map documents."""
import argparse
import hashlib
import json
import posixpath
from pathlib import Path

HERE = Path(__file__).resolve().parent
FIXTURES = HERE / "fixtures"

def entries(doc):
    out = []
    for question in doc.get("coverage", []):
        out.append({"kind": "coverage", "name": question.get("question", ""), "properties": {
            "status": question.get("status", ""), "reasons": question.get("reasons", [])}})
    nodes = {n.get("id"): n for n in doc.get("nodes", [])}
    for n in doc.get("nodes", []):
        out.append({"kind": n.get("kind"), "name": n.get("name", ""), "paths": n.get("paths", []), "properties": n.get("properties", {}), "evidence": n.get("evidence", []), "coverage": n.get("coverage", {})})
        for f in n.get("facts", []):
            out.append({"kind": "fact", "name": f.get("name", ""), "value": f.get("value", ""), "properties": f.get("properties", {}), "paths": n.get("paths", []), "coverage": f.get("coverage", {}), "evidence": f.get("evidence", [])})
    for e in doc.get("edges", []):
        source, target = nodes.get(e.get("from"), {}), nodes.get(e.get("to"), {})
        out.append({
            "kind": "edge", "type": e.get("type"), "properties": e.get("properties", {}),
            "from_paths": source.get("paths", []), "to_paths": target.get("paths", []),
            "from_kind": source.get("kind"), "to_kind": target.get("kind"),
            "from_name": source.get("name"), "to_name": target.get("name"),
            "coverage": e.get("coverage", {}), "evidence": e.get("evidence", [])
        })
    return out

def matches(item, want):
    supported = {"kind", "name", "value", "type", "property", "properties", "path", "from_path", "to_path", "from_kind", "to_kind", "from_name", "to_name", "coverage", "evidence_paths"}
    unknown = set(want) - supported
    if unknown:
        raise ValueError(f"unknown fixture assertion fields: {sorted(unknown)}")
    if item.get("kind") != want.get("kind"): return False
    for key in ("name", "value", "type"):
        if key == "value" and "property" in want: continue
        if key in want and item.get(key) != want[key]: return False
    if "property" in want and item.get("properties", {}).get(want["property"]) != want.get("value"): return False
    for key, value in want.get("properties", {}).items():
        if item.get("properties", {}).get(key) != value: return False
    if "path" in want and want["path"] not in item.get("paths", []): return False
    if "from_path" in want and want["from_path"] not in item.get("from_paths", []): return False
    if "to_path" in want and want["to_path"] not in item.get("to_paths", []): return False
    for key in ("from_kind", "to_kind", "from_name", "to_name"):
        if key in want and item.get(key) != want[key]: return False
    for key, value in want.get("coverage", {}).items():
        if item.get("coverage", {}).get(key) != value: return False
    proof_paths = {e.get("path") for e in item.get("evidence", [])}
    if not set(want.get("evidence_paths", [])).issubset(proof_paths): return False
    return True

def stable_id(prefix, parts):
    digest = hashlib.sha256()
    for value in [prefix, *parts]:
        encoded = value.encode("utf-8")
        digest.update(str(len(encoded)).encode("ascii") + b":" + encoded)
    return prefix + ":" + digest.hexdigest()[:32]


def check_identities(doc):
    node_ids = set()
    for node in doc.get("nodes", []):
        paths = sorted({posixpath.normpath(p.replace("\\", "/")) for p in node.get("paths", [])})
        expected = stable_id(node["kind"], [*paths, node.get("discriminator", "")])
        if not paths or node.get("id") != expected or expected in node_ids:
            raise AssertionError("node identity is absent, duplicated or noncanonical")
        node_ids.add(expected)
    edge_ids = set()
    for edge in doc.get("edges", []):
        expected = stable_id("edge-" + edge["type"], [edge["from"], edge["to"], edge.get("discriminator", "")])
        if edge.get("id") != expected or expected in edge_ids:
            raise AssertionError("edge identity is duplicated or noncanonical")
        if edge["from"] not in node_ids or edge["to"] not in node_ids:
            raise AssertionError("edge endpoint has no retained node")
        edge_ids.add(expected)


def check_document(path):
    doc = json.loads(path.read_text())
    if doc.get("kind") != "map" or doc.get("schema_version") != "1.0.0": raise AssertionError("wrong map identity")
    if doc.get("status") not in ("complete", "partial", "unknown"): raise AssertionError("invalid status")
    check_identities(doc)
    all_items = entries(doc)
    for n in doc.get("nodes", []):
        for e in n.get("evidence", []) + [ev for f in n.get("facts", []) for ev in f.get("evidence", [])]:
            if e.get("path", "").startswith(("/", "\\")) or ":/" in e.get("path", ""): raise AssertionError("absolute evidence path")
            if n.get("properties", {}).get("role") != "documentation" and e.get("source_kind") in ("documentation", "comment", "docstring"): raise AssertionError("documentation used as evidence")
    for e in doc.get("edges", []):
        for ev in e.get("evidence", []):
            if ev.get("path", "").startswith(("/", "\\")) or ":/" in ev.get("path", ""): raise AssertionError("absolute edge evidence path")
    return all_items

def main():
    p = argparse.ArgumentParser(); p.add_argument("--results", type=Path, required=True); args = p.parse_args()
    manifest = json.loads((FIXTURES / "manifest.json").read_text()); total = passed = 0
    for entry in manifest["fixtures"]:
        items = check_document(args.results / f"{entry['id']}.json")
        expected = json.loads((FIXTURES / entry["expectations"]).read_text())
        for question in expected["questions"]:
            oracle = question.get("oracle", {})
            if not oracle.get("source") or not oracle.get("url") or not oracle.get("fixture_evidence"):
                raise SystemExit(f"{entry['id']}/{question['id']}: missing independent oracle metadata")
            total += len(question["must_include"]) + len(question["must_not_include"])
            missing = [x for x in question["must_include"] if not any(matches(i, x) for i in items)]
            forbidden = [x for x in question["must_not_include"] if any(matches(i, x) for i in items)]
            if missing or forbidden: raise SystemExit(f"{entry['id']}/{question['id']}: missing={missing} forbidden={forbidden}")
            passed += len(question["must_include"]) + len(question["must_not_include"])
    print(json.dumps({"gate":"initial-map-corpus","passed":passed,"assertions":total,
                      "precision":None,"recall":None,
                      "metric_scope":"Targeted positive and negative assertions are not an exhaustive output labeling; precision and recall are unmeasured.",
                      "scope":f"{len(manifest['fixtures'])} compact hand-written fixtures; not full #75 corpus"}, indent=2))

if __name__ == "__main__": main()
