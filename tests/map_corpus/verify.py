#!/usr/bin/env python3
"""Verify hand-written facts against candidate map documents."""
import argparse
import hashlib
import json
from pathlib import Path

HERE = Path(__file__).resolve().parent
FIXTURES = HERE / "fixtures"

def entries(doc):
    out = []
    for n in doc.get("nodes", []):
        out.append({"kind": n.get("kind"), "name": n.get("name", ""), "paths": n.get("paths", []), "properties": n.get("properties", {})})
        for f in n.get("facts", []):
            out.append({"kind": "fact", "name": f.get("name", ""), "value": f.get("value", ""), "properties": f.get("properties", {}), "paths": n.get("paths", [])})
    for e in doc.get("edges", []):
        out.append({"kind": "edge", "type": e.get("type"), "properties": e.get("properties", {})})
    return out

def matches(item, want):
    if item.get("kind") != want.get("kind"): return False
    for key in ("name", "value", "type"):
        if key in want and item.get(key) != want[key]: return False
    if "property" in want and item.get("properties", {}).get(want["property"]) != want.get("value"): return False
    if "path" in want and want["path"] not in item.get("paths", []): return False
    return True

def check_document(path):
    doc = json.loads(path.read_text())
    if doc.get("kind") != "map" or doc.get("schema_version") != "1.0.0": raise AssertionError("wrong map identity")
    if doc.get("status") not in ("complete", "partial", "unknown"): raise AssertionError("invalid status")
    raw = json.dumps(doc, sort_keys=True, separators=(",", ":")).encode()
    if hashlib.sha256(raw).hexdigest() == "": raise AssertionError("unreachable digest check")
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
            total += len(question["must_include"]) + len(question["must_not_include"])
            missing = [x for x in question["must_include"] if not any(matches(i, x) for i in items)]
            forbidden = [x for x in question["must_not_include"] if any(matches(i, x) for i in items)]
            if missing or forbidden: raise SystemExit(f"{entry['id']}/{question['id']}: missing={missing} forbidden={forbidden}")
            passed += len(question["must_include"]) + len(question["must_not_include"])
    print(json.dumps({"gate":"initial-map-corpus","passed":passed,"assertions":total,"precision":1.0,"recall":1.0,"scope":"three compact hand-written fixtures; not full #75 corpus"}, indent=2))

if __name__ == "__main__": main()
