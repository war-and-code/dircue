#!/usr/bin/env python3
"""End-to-end invariants for the one-shot map; no expected output comes from dircue."""
import argparse
import json
import shutil
import subprocess
import tempfile
from pathlib import Path

HERE = Path(__file__).resolve().parent
SOURCE = HERE / "fixtures" / "polyglot-deployable"

def invoke(binary, *args):
    result = subprocess.run([str(binary), *map(str, args)], capture_output=True, text=True)
    if result.returncode != 0:
        raise AssertionError(f"{' '.join(map(str,args))}: exit={result.returncode} stderr={result.stderr}")
    return json.loads(result.stdout)

def reverse_copy(source, target):
    target.mkdir()
    paths = sorted(source.rglob("*"), reverse=True)
    for item in paths:
        relative = item.relative_to(source)
        destination = target / relative
        if item.is_dir():
            destination.mkdir(parents=True, exist_ok=True)
        else:
            destination.parent.mkdir(parents=True, exist_ok=True)
            shutil.copyfile(item, destination)

def non_documentation_projection(document):
    excluded = {
        node["id"] for node in document["nodes"]
        if node.get("kind") == "content" and node.get("properties", {}).get("role") == "documentation"
    }
    return {
        "nodes": [node for node in document["nodes"] if node["id"] not in excluded],
        "edges": [edge for edge in document["edges"] if edge["from"] not in excluded and edge["to"] not in excluded],
        "coverage": document["coverage"],
    }

def assert_no_absolute_strings(value, roots):
    if isinstance(value, dict):
        for child in value.values(): assert_no_absolute_strings(child, roots)
    elif isinstance(value, list):
        for child in value: assert_no_absolute_strings(child, roots)
    elif isinstance(value, str):
        for root in roots:
            if str(root) in value:
                raise AssertionError(f"absolute source path leaked: {value}")

def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--binary", type=Path, required=True)
    args = parser.parse_args()
    binary = args.binary.resolve()
    with tempfile.TemporaryDirectory(prefix="dircue-map-metamorphic-") as temporary:
        root = Path(temporary)
        ordinary, reversed_tree = root / "ordinary", root / "reverse"
        shutil.copytree(SOURCE, ordinary)
        reverse_copy(SOURCE, reversed_tree)

        baseline = invoke(binary, "map", "--source", "directory", "--workers", "1", "--json", ordinary)
        parallel = invoke(binary, "map", "--source", "directory", "--workers", "4", "--json", ordinary)
        relocated = invoke(binary, "map", "--source", "directory", "--workers", "1", "--json", reversed_tree)
        if baseline != parallel:
            raise AssertionError("worker count changed map semantics")
        if baseline != relocated:
            raise AssertionError("absolute location or creation order changed the portable map")
        assert_no_absolute_strings(baseline, (ordinary, reversed_tree, root))

        for controls in (("--preset", "fast"), ("--preset", "low-memory"), ("--set", "workers=3")):
            tuned = invoke(binary, "map", "--source", "directory", *controls, "--json", reversed_tree)
            if baseline != tuned:
                raise AssertionError(f"answer-preserving control changed the map: {controls}")

        readme = ordinary / "README.md"
        readme.write_text(readme.read_text() + "\nClaims: Redis, /admin, and twelve deployables.\n")
        documentation_mutation = invoke(binary, "map", "--source", "directory", "--json", ordinary)
        if non_documentation_projection(baseline) != non_documentation_projection(documentation_mutation):
            raise AssertionError("documentation mutation changed non-documentation facts")

        limited = invoke(binary, "map", "--source", "directory", "--max-file-bytes", "1", "--json", reversed_tree)
        content = next(item for item in limited["coverage"] if item["question"] == "content")
        if limited["status"] != "partial" or content["status"] == "complete":
            raise AssertionError("content byte limit did not qualify coverage")

        budgeted = invoke(binary, "map", "--source", "directory", "--budget-files", "1", "--json", reversed_tree)
        if budgeted["status"] != "partial":
            raise AssertionError("inventory budget did not produce a successful partial map")

        before, after = root / "before.json", root / "after.json"
        before.write_text(json.dumps(baseline))
        after.write_text(json.dumps(parallel))
        comparison = invoke(binary, "map", "compare", "--json", before, after)
        if comparison["status"] != "unchanged" or comparison["counts"]["material"] != 0:
            raise AssertionError("identical portable maps did not compare unchanged")

    print(json.dumps({"gate":"map-metamorphic", "invariants":10, "status":"passed"}, indent=2))

if __name__ == "__main__":
    main()
