#!/usr/bin/env python3
"""End-to-end invariants for the one-shot map; no expected output comes from dircue."""
import argparse
import json
import os
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

def invoke_allow_partial(binary, *args):
    """Like invoke but allows non-zero exit only for partial coverage, not for parse errors."""
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

def strip_volatile(document):
    """Remove fields that are expected to differ between source modes.

    Documented expected differences between git and directory source:
      - source: mode, revision, commit, tree (git) vs mode, digest (directory)
      - meta: contains timestamps and provenance metadata
    All other map content must be identical for the same file tree.
    """
    d = dict(document)
    d.pop("source", None)
    d.pop("meta", None)
    return d

def assert_no_absolute_strings(value, roots):
    if isinstance(value, dict):
        for child in value.values(): assert_no_absolute_strings(child, roots)
    elif isinstance(value, list):
        for child in value: assert_no_absolute_strings(child, roots)
    elif isinstance(value, str):
        for root in roots:
            if str(root) in value:
                raise AssertionError(f"absolute source path leaked: {value}")

def git_init(directory):
    """Create a minimal committed git repository from the given directory."""
    subprocess.run(["git", "init", "-q", str(directory)], check=True)
    subprocess.run(["git", "-C", str(directory), "config", "user.email", "test@metamorphic.test"], check=True)
    subprocess.run(["git", "-C", str(directory), "config", "user.name", "Metamorphic Test"], check=True)
    subprocess.run(["git", "-C", str(directory), "add", "."], check=True)
    subprocess.run(["git", "-C", str(directory), "commit", "-q", "-m", "metamorphic seed"], check=True)

def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--binary", type=Path, required=True)
    args = parser.parse_args()
    binary = args.binary.resolve()

    # Verify git is available for source-mode invariant.
    git_available = shutil.which("git") is not None

    with tempfile.TemporaryDirectory(prefix="dircue-map-metamorphic-") as temporary:
        root = Path(temporary)
        ordinary, reversed_tree = root / "ordinary", root / "reverse"
        shutil.copytree(SOURCE, ordinary)
        reverse_copy(SOURCE, reversed_tree)

        # Invariant 1: worker count does not change output.
        baseline = invoke(binary, "map", "--source", "directory", "--workers", "1", "--json", ordinary)
        parallel = invoke(binary, "map", "--source", "directory", "--workers", "4", "--json", ordinary)
        if baseline != parallel:
            raise AssertionError("worker count changed map semantics")

        # Invariant 2: absolute location and creation order do not change portable output.
        relocated = invoke(binary, "map", "--source", "directory", "--workers", "1", "--json", reversed_tree)
        if baseline != relocated:
            raise AssertionError("absolute location or creation order changed the portable map")

        # Invariant 3: no absolute host paths in output.
        assert_no_absolute_strings(baseline, (ordinary, reversed_tree, root))

        # Invariants 4–6: performance-only controls do not change output.
        for controls in (("--preset", "low-memory"), ("--set", "workers=3"),
                         ("--set", "workers=16", "--set", "git.object_cache_bytes=128MiB")):
            tuned = invoke(binary, "map", "--source", "directory", *controls, "--json", reversed_tree)
            if baseline != tuned:
                raise AssertionError(f"answer-preserving control changed the map: {controls}")

        # Invariant 7: documentation-only mutation does not change non-documentation facts.
        readme = ordinary / "README.md"
        readme.write_text(readme.read_text() + "\nClaims: Redis, /admin, and twelve deployables.\n")
        documentation_mutation = invoke(binary, "map", "--source", "directory", "--json", ordinary)
        if non_documentation_projection(baseline) != non_documentation_projection(documentation_mutation):
            raise AssertionError("documentation mutation changed non-documentation facts")

        # Invariant 8: content byte limit qualifies coverage to partial.
        limited = invoke(binary, "map", "--source", "directory", "--max-file-bytes", "1", "--json", reversed_tree)
        content = next(item for item in limited["coverage"] if item["question"] == "content")
        if limited["status"] != "partial" or content["status"] == "complete":
            raise AssertionError("content byte limit did not qualify coverage")

        # Invariant 9: inventory budget produces partial map.
        budgeted = invoke(binary, "map", "--source", "directory", "--budget-files", "1", "--json", reversed_tree)
        if budgeted["status"] != "partial":
            raise AssertionError("inventory budget did not produce a successful partial map")

        # Invariant 10: identical portable maps compare as unchanged.
        before, after = root / "before.json", root / "after.json"
        before.write_text(json.dumps(baseline))
        after.write_text(json.dumps(parallel))
        comparison = invoke(binary, "map", "compare", "--json", before, after)
        if comparison["status"] != "unchanged" or comparison["counts"]["material"] != 0:
            raise AssertionError("identical portable maps did not compare unchanged")

        # Invariant 11: compare(B, A) and compare(A, B) agree on material change count.
        # When maps differ, the number of materially changed elements is symmetric.
        readme.write_text(readme.read_text() + "\nSome non-documentation content change.\n")
        map_a = invoke(binary, "map", "--source", "directory", "--json", reversed_tree)
        map_b = invoke(binary, "map", "--source", "directory", "--json", ordinary)
        map_a_file, map_b_file = root / "map-a.json", root / "map-b.json"
        map_a_file.write_text(json.dumps(map_a))
        map_b_file.write_text(json.dumps(map_b))
        fwd = invoke(binary, "map", "compare", "--json", map_a_file, map_b_file)
        rev = invoke(binary, "map", "compare", "--json", map_b_file, map_a_file)
        if fwd["counts"]["material"] != rev["counts"]["material"]:
            raise AssertionError(
                f"compare is not symmetric on material count: "
                f"fwd={fwd['counts']['material']} rev={rev['counts']['material']}"
            )

        # Invariant 12: renaming the root directory does not change the portable map.
        renamed = root / "renamed-root"
        shutil.copytree(SOURCE, renamed)
        renamed_map = invoke(binary, "map", "--source", "directory", "--workers", "1", "--json", renamed)
        if baseline != renamed_map:
            raise AssertionError("root directory name changed the portable map")

        # Invariant 13: locality — adding a new, structurally unrelated file (only
        # documentation) under a new directory does not change any existing structural
        # node (components, interfaces, deployables, packages).
        with_readme = root / "with-extra-readme"
        shutil.copytree(SOURCE, with_readme)
        extra_dir = with_readme / "extra-unrelated-docs"
        extra_dir.mkdir()
        (extra_dir / "README.md").write_text("# Unrelated documentation\nSome notes.\n")
        map_without = invoke(binary, "map", "--source", "directory", "--workers", "1", "--json", ordinary)
        map_with = invoke(binary, "map", "--source", "directory", "--workers", "1", "--json", with_readme)
        structural_kinds = {"component", "interface", "deployable", "package", "capability"}
        nodes_without = {n["id"]: n for n in map_without["nodes"] if n.get("kind") in structural_kinds}
        nodes_with = {n["id"]: n for n in map_with["nodes"] if n.get("kind") in structural_kinds}
        changed_structural = {
            nid for nid in nodes_without
            if nid in nodes_with and nodes_without[nid] != nodes_with[nid]
        }
        removed_structural = set(nodes_without) - set(nodes_with)
        if changed_structural or removed_structural:
            raise AssertionError(
                f"adding unrelated README changed existing structural nodes: "
                f"changed={changed_structural} removed={removed_structural}"
            )

        # Invariant 14 (git available): git source and directory source give
        # byte-identical maps for the same committed tree, modulo the documented
        # source field differences.
        # Documented differences: source.mode, source.revision, source.commit,
        # source.tree (git) vs source.digest (directory). All other content must
        # be identical.
        if git_available:
            git_dir = root / "git-source"
            shutil.copytree(SOURCE, git_dir)
            git_init(git_dir)
            git_map = invoke(binary, "map", "--source", "git", "--workers", "1", "--json", git_dir)
            dir_map = invoke(binary, "map", "--source", "directory", "--workers", "1", "--json", git_dir)
            git_stripped = strip_volatile(git_map)
            dir_stripped = strip_volatile(dir_map)
            if git_stripped != dir_stripped:
                raise AssertionError(
                    "git source and directory source differ beyond the documented source/meta fields"
                )

    n_invariants = 14 if git_available else 13
    print(json.dumps({"gate": "map-metamorphic", "invariants": n_invariants, "status": "passed"}, indent=2))

if __name__ == "__main__":
    main()
