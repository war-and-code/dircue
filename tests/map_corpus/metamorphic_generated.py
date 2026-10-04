#!/usr/bin/env python3
"""Seeded map metamorphic checks over generated repositories.

Run with ``--tier ci`` for three fixed seeds or ``--tier manual`` for twelve.
All generated inputs and Git repositories live below one temporary directory.
"""

from __future__ import annotations

import argparse
import copy
import json
import os
import shutil
import tempfile
import sys
from pathlib import Path

from generated_trees import CI_SEEDS, MANUAL_SEEDS, create_tree
sys.path.insert(0, str(Path(__file__).resolve().parents[2] / "scripts"))
import smoke_process


def invoke(binary: Path, *args) -> dict:
    result = smoke_process.run([str(binary), *map(str, args)], capture_output=True,
                            text=True, encoding="utf-8", timeout=180)
    if result.returncode != 0:
        raise AssertionError(f"{' '.join(map(str, args))}: exit={result.returncode}; stderr={result.stderr}")
    try:
        return json.loads(result.stdout)
    except json.JSONDecodeError as exc:
        raise AssertionError(f"dircue returned invalid JSON for {args}: {exc}\n{result.stdout[:1000]}") from exc


def git(directory: Path, *args, cwd: Path | None = None) -> str:
    # Ambient Git settings must not redirect writes out of the temporary tree
    # or override the local repository configuration used by this test.
    env = {key: value for key, value in os.environ.items() if not key.upper().startswith("GIT_")}
    env.update({
        "GIT_CONFIG_NOSYSTEM": "1",
        "GIT_CONFIG_GLOBAL": os.devnull,
        "GIT_AUTHOR_DATE": "2000-01-01T00:00:00Z",
        "GIT_COMMITTER_DATE": "2000-01-01T00:00:00Z",
    })
    result = smoke_process.run(["git", "-c", "core.longpaths=true", *args], cwd=cwd or directory, capture_output=True,
                            text=True, encoding="utf-8", timeout=120, env=env)
    if result.returncode != 0:
        raise AssertionError(f"git {' '.join(args)} failed in {cwd or directory}: {result.stderr}")
    return result.stdout.strip()


def reverse_copy(source: Path, target: Path) -> None:
    target.mkdir()
    for item in sorted(source.rglob("*"), reverse=True):
        destination = target / item.relative_to(source)
        if item.is_symlink():
            destination.parent.mkdir(parents=True, exist_ok=True)
            destination.symlink_to(item.readlink(), target_is_directory=item.is_dir())
        elif item.is_dir():
            destination.mkdir(parents=True, exist_ok=True)
        else:
            destination.parent.mkdir(parents=True, exist_ok=True)
            shutil.copyfile(item, destination)


def assert_no_absolute_strings(value, roots: tuple[Path, ...]) -> None:
    if isinstance(value, dict):
        for child in value.values():
            assert_no_absolute_strings(child, roots)
    elif isinstance(value, list):
        for child in value:
            assert_no_absolute_strings(child, roots)
    elif isinstance(value, str):
        for root in roots:
            if str(root).replace("\\", "/") in value.replace("\\", "/"):
                raise AssertionError(f"absolute generated source path leaked: {value}")


def non_documentation_projection(document: dict) -> dict:
    excluded = {
        node["id"] for node in document["nodes"]
        if node.get("kind") == "content" and node.get("properties", {}).get("role") == "documentation"
    }
    return {
        "nodes": [node for node in document["nodes"] if node["id"] not in excluded],
        "edges": [edge for edge in document["edges"] if edge["from"] not in excluded and edge["to"] not in excluded],
        "coverage": document["coverage"],
    }


def _git_init(directory: Path) -> None:
    git(directory, "init", "-q")
    hooks = Path(git(directory, "rev-parse", "--absolute-git-dir")) / "metamorphic-empty-hooks"
    hooks.mkdir(parents=True, exist_ok=True)
    git(directory, "config", "core.hooksPath", str(hooks))
    git(directory, "config", "core.autocrlf", "false")
    git(directory, "config", "gc.auto", "0")
    git(directory, "config", "commit.gpgsign", "false")
    git(directory, "config", "user.email", "test@metamorphic.test")
    git(directory, "config", "user.name", "Generated map test")
    git(directory, "add", "-A")
    git(directory, "commit", "-q", "-m", "generated metamorphic seed")


def _loose_object_count(directory: Path) -> int:
    fields = dict(line.split(": ", 1) for line in git(directory, "count-objects", "-v").splitlines() if ": " in line)
    if "count" not in fields:
        raise AssertionError(f"git count-objects omitted loose-object count: {fields}")
    return int(fields["count"])


def _assert_generated_evidence(document: dict, evidence: dict) -> None:
    """Guard against vacuous generated runs that accidentally contain no source."""
    represented_paths = {
        path
        for node in document.get("nodes", [])
        for path in node.get("paths", [])
    }
    for ecosystem in evidence["ecosystem_pair"].split("-"):
        manifest = {
            "npm": "package.json", "go": "go.mod", "python": "pyproject.toml",
            "cargo": "Cargo.toml", "dotnet": ".csproj", "maven": "pom.xml",
        }[ecosystem]
        if not any(path.endswith(manifest) for path in represented_paths):
            raise AssertionError(f"generated {ecosystem} manifest was not represented in the map")
    missing_manifests = set(evidence["expected_manifest_paths"]) - represented_paths
    if missing_manifests:
        raise AssertionError(f"generated ecosystem manifest path(s) were not represented exactly: {sorted(missing_manifests)}")
    if not document.get("nodes"):
        raise AssertionError("generated tree produced an empty map")
    components = [node for node in document["nodes"] if node.get("kind") == "component"]
    if len(components) < 2:
        raise AssertionError(f"generated tree expected two ecosystem components, found {len(components)}")
    component_indexes = []
    for manifest_path in evidence["expected_manifest_paths"]:
        owners = [index for index, node in enumerate(components)
                  if manifest_path in node.get("paths", [])]
        if len(owners) != 1:
            raise AssertionError(
                f"generated manifest {manifest_path!r} must belong to exactly one component node; found {len(owners)}"
            )
        component_indexes.append(owners[0])
    if len(set(component_indexes)) != len(evidence["expected_manifest_paths"]):
        raise AssertionError("generated ecosystem manifests were not assigned to distinct component nodes")
    observed_languages = {
        node.get("properties", {}).get("language") or node.get("name")
        for node in document["nodes"]
        if node.get("kind") == "content" and node.get("properties", {}).get("role") == "language_population"
    }
    missing_languages = set(evidence["expected_language_markers"]) - observed_languages
    if missing_languages:
        raise AssertionError(f"generated tree has no language-population evidence for {sorted(missing_languages)}")
    language_files = {
        node.get("properties", {}).get("language") or node.get("name"): int(node["properties"].get("files", "0"))
        for node in document["nodes"]
        if node.get("kind") == "content" and node.get("properties", {}).get("role") == "language_population"
    }
    if language_files != evidence["expected_language_file_counts"]:
        raise AssertionError(
            f"generated deep/Unicode/control source inventory differs: "
            f"expected={evidence['expected_language_file_counts']} actual={language_files}"
        )
    if any(link["created"] for link in evidence["symlinks"]):
        coverage = next((item for item in document.get("coverage", []) if item.get("question") == "content"), {})
        if coverage.get("status") != "partial" or "non_regular_file" not in coverage.get("reasons", []):
            raise AssertionError("created symlinks were not reported through partial/non_regular_file content coverage")


def _content_coverage(document: dict) -> dict:
    return next((item for item in document.get("coverage", []) if item.get("question") == "content"), {})


def _language_population_files(document: dict) -> int:
    return sum(
        int(node.get("properties", {}).get("files", "0"))
        for node in document.get("nodes", [])
        if node.get("kind") == "content" and node.get("properties", {}).get("role") == "language_population"
    )


def _assert_limit_effect(baseline: dict, limited: dict, reason: str, label: str,
                         *, require_lower_language_population: bool = False) -> None:
    coverage = _content_coverage(limited)
    if limited == baseline:
        raise AssertionError(f"{label}: expected {reason!r}; limit had no observable effect on the generated map")
    if (limited.get("status") != "partial" or coverage.get("status") != "partial"
            or reason not in coverage.get("reasons", [])):
        raise AssertionError(f"{label}: expected partial content coverage with {reason!r}, got {coverage}")
    if require_lower_language_population and _language_population_files(limited) >= _language_population_files(baseline):
        raise AssertionError(f"{label}: expected a lower language file population after the inventory limit")


def _assert_generated_language_paths(report: dict, evidence: dict) -> None:
    for language, expected_paths in evidence["expected_language_paths"].items():
        entry = report.get(language)
        if not isinstance(entry, dict):
            raise AssertionError(f"legacy language report omitted expected language {language!r}")
        actual_paths = {str(path).replace("\\", "/") for path in entry.get("files", [])}
        missing = set(expected_paths) - actual_paths
        if missing:
            raise AssertionError(f"legacy language breakdown omitted {language} path(s): {sorted(missing)!r}")


def validate_output_guard_mutants(document: dict, evidence: dict, language_report: dict) -> list[str]:
    """Prove generated-tree guard sensitivity with explicitly scoped output mutants.

    These mutants corrupt the real CLI's JSON result after invocation. They
    validate the harness guards, not production-source mutation kill rate.
    """
    mutants = []
    for language, path, label in (
        ("Python", evidence["deepest_file"], "deep-python-path-omitted"),
        ("Go", evidence["unicode_path"], "unicode-go-path-omitted"),
    ):
        mutated = copy.deepcopy(language_report)
        mutated[language]["files"] = [
            value for value in mutated[language].get("files", [])
            if str(value).replace("\\", "/") != path
        ]
        try:
            _assert_generated_language_paths(mutated, evidence)
        except AssertionError:
            mutants.append(label)
        else:
            raise AssertionError(f"generated evidence guard survived projected mutant {label}")

    if evidence.get("control_path"):
        mutated = copy.deepcopy(language_report)
        mutated["Python"]["files"] = [
            value for value in mutated["Python"].get("files", [])
            if str(value).replace("\\", "/") != evidence["control_path"]
        ]
        try:
            _assert_generated_language_paths(mutated, evidence)
        except AssertionError:
            mutants.append("control-python-path-omitted")
        else:
            raise AssertionError("generated evidence guard survived projected mutant control-python-path-omitted")

    mutated = copy.deepcopy(document)
    component = next(n for n in mutated["nodes"] if n.get("kind") == "component")
    mutated["nodes"].remove(component)
    try:
        _assert_generated_evidence(mutated, evidence)
    except AssertionError:
        mutants.append("ecosystem-component-omitted")
    else:
        raise AssertionError("generated evidence guard survived projected mutant ecosystem-component-omitted")

    mutated = copy.deepcopy(document)
    expected_manifest = evidence["expected_manifest_paths"][0]
    for node in mutated["nodes"]:
        if expected_manifest in node.get("paths", []):
            node["paths"].remove(expected_manifest)
    manifest_node = next(node for node in mutated["nodes"] if node.get("kind") == "component")
    manifest_node["paths"].append("misleading/relocated/" + expected_manifest.rsplit("/", 1)[-1])
    try:
        _assert_generated_evidence(mutated, evidence)
    except AssertionError:
        mutants.append("exact-ecosystem-manifest-path-omitted")
    else:
        raise AssertionError("generated evidence guard survived projected mutant exact-ecosystem-manifest-path-omitted")

    mutated = copy.deepcopy(document)
    expected_manifest = evidence["expected_manifest_paths"][0]
    for node in mutated["nodes"]:
        if node.get("kind") == "component" and expected_manifest in node.get("paths", []):
            node["paths"].remove(expected_manifest)
    mutated["nodes"].append({"kind": "interface", "paths": [expected_manifest]})
    try:
        _assert_generated_evidence(mutated, evidence)
    except AssertionError:
        mutants.append("manifest-on-wrong-kind-node-rejected")
    else:
        raise AssertionError("generated evidence guard survived projected mutant manifest-on-wrong-kind-node-rejected")

    if any(link["created"] for link in evidence["symlinks"]):
        mutated = copy.deepcopy(document)
        coverage = next(item for item in mutated["coverage"] if item.get("question") == "content")
        coverage["reasons"] = [reason for reason in coverage.get("reasons", []) if reason != "non_regular_file"]
        try:
            _assert_generated_evidence(mutated, evidence)
        except AssertionError:
            mutants.append("symlink-omission-status-hidden")
        else:
            raise AssertionError("generated evidence guard survived projected mutant symlink-omission-status-hidden")

    for reason, label in (("file_too_large", "ignored-file-byte-limit"),
                          ("tree_size_limit", "ignored-inventory-budget")):
        try:
            # This models the CLI silently ignoring the requested limit and
            # returning the already-partial symlink baseline unchanged.
            _assert_limit_effect(document, document, reason, label,
                                 require_lower_language_population=(reason == "tree_size_limit"))
        except AssertionError:
            mutants.append(label)
        else:
            raise AssertionError(f"resource-limit guard survived projected mutant {label}")
    return mutants


def _assert_non_doc_locality(binary: Path, root: Path, baseline: dict, workspace: Path) -> None:
    extra = workspace / f"with-extra-readme-{root.name}"
    shutil.copytree(root, extra, symlinks=True)
    unrelated = extra / "extra-unrelated-docs"
    unrelated.mkdir()
    (unrelated / "README.md").write_text("# Separate notes\n", encoding="utf-8")
    changed = invoke(binary, "map", "--source", "directory", "--workers", "1", "--json", extra)
    if non_documentation_projection(baseline) != non_documentation_projection(changed):
        raise AssertionError("adding an unrelated README changed non-documentation nodes, edges, or coverage")


def run_directory_properties(binary: Path, seed: int, pair, workspace: Path) -> tuple[int, dict, list[str]]:
    source = workspace / f"tree-{seed}"
    evidence = create_tree(source, seed, pair)
    reverse = workspace / f"tree-{seed}-reverse"
    reverse_copy(source, reverse)

    baseline = invoke(binary, "map", "--source", "directory", "--workers", "1", "--json", source)
    _assert_generated_evidence(baseline, evidence)
    language_report = invoke(binary, "analyze", "languages", "--source", "directory", "--breakdown", "--json", source)
    _assert_generated_language_paths(language_report, evidence)
    guard_mutants = validate_output_guard_mutants(baseline, evidence, language_report)
    relation_cases = 1  # Literal path membership in the legacy language breakdown.

    parallel = invoke(binary, "map", "--source", "directory", "--workers", "4", "--json", source)
    if baseline != parallel:
        raise AssertionError(f"seed {seed}: worker count changed map semantics")
    relation_cases += 1

    relocated = invoke(binary, "map", "--source", "directory", "--workers", "1", "--json", reverse)
    if baseline != relocated:
        raise AssertionError(f"seed {seed}: relocation or creation order changed portable output")
    assert_no_absolute_strings(baseline, (source, reverse, workspace))
    relation_cases += 2

    for controls in (("--preset", "low-memory"), ("--set", "workers=3"),
                     ("--set", "workers=16", "--set", "git.object_cache_bytes=128MiB")):
        controlled = invoke(binary, "map", "--source", "directory", *controls, "--json", source)
        if baseline != controlled:
            raise AssertionError(f"seed {seed}: answer-preserving control changed output: {controls}")
        relation_cases += 1

    readme = source / "README.md"
    original_readme = readme.read_text(encoding="utf-8")
    readme.write_text(original_readme + "\nClaims: synthetic documentation mutation.\n", encoding="utf-8")
    documentation = invoke(binary, "map", "--source", "directory", "--json", source)
    if non_documentation_projection(baseline) != non_documentation_projection(documentation):
        raise AssertionError(f"seed {seed}: documentation mutation changed non-documentation facts")
    readme.write_text(original_readme, encoding="utf-8")
    relation_cases += 1

    limited = invoke(binary, "map", "--source", "directory", "--max-file-bytes", "1", "--json", reverse)
    _assert_limit_effect(baseline, limited, "file_too_large", f"seed {seed} file-byte limit")
    relation_cases += 1

    budgeted = invoke(binary, "map", "--source", "directory", "--budget-files", "1", "--json", reverse)
    _assert_limit_effect(baseline, budgeted, "tree_size_limit", f"seed {seed} inventory budget",
                         require_lower_language_population=True)
    relation_cases += 1

    before, after = workspace / f"before-{seed}.json", workspace / f"after-{seed}.json"
    before.write_text(json.dumps(baseline), encoding="utf-8")
    after.write_text(json.dumps(parallel), encoding="utf-8")
    unchanged = invoke(binary, "map", "compare", "--json", before, after)
    if unchanged.get("status") != "unchanged" or unchanged.get("counts", {}).get("material") != 0:
        raise AssertionError(f"seed {seed}: identical generated maps did not compare unchanged")
    relation_cases += 1

    # Compare symmetry is meaningful only when the maps contain material change.
    changed_file = source / "services" / f"{pair.first}-service"
    code = next((p for p in changed_file.rglob("*") if p.is_file() and p.suffix in {".go", ".js", ".py", ".rs", ".cs", ".java"}), None)
    if code is None:
        raise AssertionError(f"seed {seed}: missing expected primary source for {pair.first}")
    mutation = {
        "npm": "\n// generated semantic change\n",
        "go": "\nconst MetamorphicDelta = 1\n",
        "python": "\nMETAMORPHIC_DELTA = 1\n",
        "cargo": "\nconst METAMORPHIC_DELTA: u8 = 1;\n",
        "dotnet": "\n// generated semantic change\n",
        "maven": "\n// generated semantic change\n",
    }[pair.first]
    code.write_text(code.read_text(encoding="utf-8") + mutation, encoding="utf-8")
    changed = invoke(binary, "map", "--source", "directory", "--json", source)
    change_file = workspace / f"changed-{seed}.json"
    change_file.write_text(json.dumps(changed), encoding="utf-8")
    forward = invoke(binary, "map", "compare", "--json", before, change_file)
    backward = invoke(binary, "map", "compare", "--json", change_file, before)
    if forward.get("counts", {}).get("material", 0) == 0:
        raise AssertionError(f"seed {seed}: source mutation did not produce material map change")
    if forward.get("counts", {}).get("material") != backward.get("counts", {}).get("material"):
        raise AssertionError(f"seed {seed}: comparison material count is not symmetric")
    relation_cases += 1

    renamed = workspace / f"renamed-root-{seed}"
    shutil.copytree(reverse, renamed, symlinks=True)
    renamed_map = invoke(binary, "map", "--source", "directory", "--workers", "1", "--json", renamed)
    if baseline != renamed_map:
        raise AssertionError(f"seed {seed}: root directory name changed portable output")
    relation_cases += 1

    _assert_non_doc_locality(binary, reverse, baseline, workspace)
    relation_cases += 1

    git_repo = workspace / f"git-source-{seed}"
    shutil.copytree(reverse, git_repo, symlinks=True)
    _git_init(git_repo)
    git_map = invoke(binary, "map", "--source", "git", "--workers", "1", "--json", git_repo)
    dir_map = invoke(binary, "map", "--source", "directory", "--workers", "1", "--json", git_repo)
    # The source object is the one intentional source-identity exception:
    # Git reports commit/tree/revision, while directory mode reports a digest.
    _validate_source_identity(git_map, "git")
    _validate_source_identity(dir_map, "directory")
    if {k: v for k, v in git_map.items() if k != "source"} != {k: v for k, v in dir_map.items() if k != "source"}:
        raise AssertionError(f"seed {seed}: Git and directory mode differ outside the source identity object")
    relation_cases += 1
    return relation_cases, evidence, guard_mutants


def _validate_source_identity(document: dict, mode: str) -> None:
    """Pin the precise source schema fields intentionally excluded in cross-mode comparison."""
    source = document.get("source")
    if not isinstance(source, dict) or source.get("mode") != mode:
        raise AssertionError(f"expected source.mode={mode!r}, got {source!r}")
    if mode == "git":
        allowed = {"mode", "revision", "commit", "tree"}
        required = {"mode", "commit", "tree"}
        if not required <= source.keys() or not source.keys() <= allowed:
            raise AssertionError(f"unexpected Git source identity fields: {sorted(source)}")
        if not all(isinstance(source.get(key), str) and source[key] for key in required - {"mode"}):
            raise AssertionError(f"incomplete Git source identity: {source!r}")
        for key in ("commit", "tree"):
            value = source[key]
            if len(value) not in (40, 64) or any(ch not in "0123456789abcdef" for ch in value.lower()):
                raise AssertionError(f"invalid Git {key} identity: {value!r}")
        if "revision" in source and (not isinstance(source["revision"], str) or not source["revision"]):
            raise AssertionError(f"invalid Git revision identity: {source!r}")
        return
    if mode == "directory":
        if not source.keys() <= {"mode", "digest"}:
            raise AssertionError(f"unexpected directory source identity fields: {sorted(source)}")
        digest = source.get("digest")
        if digest is not None:
            if not isinstance(digest, dict) or not {"algorithm", "scope", "value"} <= digest.keys():
                raise AssertionError(f"invalid directory digest identity: {digest!r}")
            if not digest.keys() <= {"algorithm", "scope", "normalization", "value"}:
                raise AssertionError(f"unexpected directory digest fields: {sorted(digest)}")
            if not all(isinstance(digest[key], str) and digest[key] for key in ("algorithm", "scope", "value")):
                raise AssertionError(f"incomplete directory digest identity: {digest!r}")
        return
    raise AssertionError(f"unsupported source mode in projected comparison: {mode!r}")


def _delta_count(repo: Path) -> int:
    packs = list((repo / ".git" / "objects" / "pack").glob("*.idx"))
    count = 0
    for index in packs:
        output = git(repo, "verify-pack", "-v", str(index))
        for line in output.splitlines():
            fields = line.split()
            if len(fields) >= 7 and fields[0] and fields[1].isalnum() and fields[2].isdigit() and fields[3].isdigit():
                try:
                    if int(fields[5]) > 0:
                        count += 1
                except ValueError:
                    continue
    return count


def run_git_storage_property(binary: Path, workspace: Path, seed: int, pair) -> tuple[int, list[str]]:
    """Compare full Git-source documents as object storage/layout changes."""
    origin = workspace / f"storage-origin-{seed}"
    evidence = create_tree(origin, seed, pair)
    _git_init(origin)
    expected_head = git(origin, "rev-parse", "HEAD")
    expected_tree = git(origin, "rev-parse", "HEAD^{tree}")
    loose_count = _loose_object_count(origin)
    if loose_count <= 0:
        raise AssertionError(f"seed {seed}: baseline Git repository was not loose-object storage")

    variants: list[tuple[str, Path]] = [("loose", origin)]
    loose_map = invoke(binary, "map", "--source", "git", "--workers", "1", "--json", origin)
    _assert_generated_evidence(loose_map, evidence)

    git(origin, "gc", "--prune=now")
    packed_count = _loose_object_count(origin)
    if packed_count != 0 or not list((origin / ".git" / "objects" / "pack").glob("*.pack")):
        raise AssertionError(f"seed {seed}: git gc did not produce packed storage")
    variants.append(("packed", origin))
    packed_map = invoke(binary, "map", "--source", "git", "--workers", "1", "--json", origin)
    if packed_map != loose_map:
        raise AssertionError(f"seed {seed}: map changed after git gc packed the objects")

    git(origin, "repack", "-adf", "--window=250", "--depth=250")
    delta_count = _delta_count(origin)
    if delta_count == 0:
        raise AssertionError(f"seed {seed}: aggressive repack produced no verified delta objects")
    variants.append(("aggressive-delta", origin))
    repacked_map = invoke(binary, "map", "--source", "git", "--workers", "1", "--json", origin)
    if repacked_map != loose_map:
        raise AssertionError(f"seed {seed}: map changed after aggressive delta repack (deltas={delta_count})")

    shallow = workspace / f"storage-shallow-{seed}"
    file_uri = origin.resolve().as_uri()
    git(shallow, "-c", "protocol.file.allow=always", "clone", "-q", "--depth=1", file_uri, str(shallow.parent / (shallow.name + "-clone")), cwd=workspace)
    shallow = workspace / (shallow.name + "-clone")
    if git(shallow, "rev-parse", "HEAD") != expected_head or git(shallow, "rev-parse", "HEAD^{tree}") != expected_tree:
        raise AssertionError(f"seed {seed}: shallow clone does not identify the same commit/tree")
    shallow_state = git(shallow, "rev-parse", "--is-shallow-repository")
    if shallow_state != "true":
        raise AssertionError(f"seed {seed}: requested depth-1 clone is not shallow")
    variants.append(("shallow", shallow))
    shallow_map = invoke(binary, "map", "--source", "git", "--workers", "1", "--json", shallow)
    if shallow_map != loose_map:
        raise AssertionError(f"seed {seed}: map changed for a shallow checkout of the same commit/tree")

    worktree = workspace / f"storage-worktree-{seed}"
    git(origin, "worktree", "add", "--detach", str(worktree), expected_head)
    if git(worktree, "rev-parse", "HEAD") != expected_head or git(worktree, "rev-parse", "HEAD^{tree}") != expected_tree:
        raise AssertionError(f"seed {seed}: linked worktree does not identify the same commit/tree")
    variants.append(("linked-worktree", worktree))
    worktree_map = invoke(binary, "map", "--source", "git", "--workers", "1", "--json", worktree)
    if worktree_map != loose_map:
        raise AssertionError(f"seed {seed}: map changed in a linked worktree of the same commit/tree")

    return len(variants) - 1, [f"verified_deltas={delta_count}", f"loose_objects={loose_count}", "variants=loose,packed,aggressive-delta,shallow,linked-worktree"]


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--binary", required=True, type=Path)
    parser.add_argument("--tier", choices=("ci", "manual"), default="ci")
    parser.add_argument("--skip-storage", action="store_true", help="Run generated directory relations only")
    args = parser.parse_args()
    binary = args.binary.resolve()
    seeds = CI_SEEDS if args.tier == "ci" else MANUAL_SEEDS
    git_available = shutil.which("git") is not None
    if not args.skip_storage and not git_available:
        raise RuntimeError("Git storage metamorphic checks require Git; pass --skip-storage to report them as skipped")

    checks = 0
    storage_evidence = []
    generated_evidence = []
    guard_mutants = []
    with tempfile.TemporaryDirectory(prefix="dircue-generated-metamorphic-") as temporary:
        workspace = Path(temporary)
        for seed, pair in seeds:
            count, evidence, tree_mutants = run_directory_properties(binary, seed, pair, workspace)
            checks += count
            if not guard_mutants:
                guard_mutants = tree_mutants
            generated_evidence.append({
                "seed": seed,
                "ecosystem_pair": evidence["ecosystem_pair"],
                "deep_directory_count": evidence["deep_directory_count"],
                "unicode_path": evidence["unicode_path"],
                "expected_language_paths": evidence["expected_language_paths"],
                "control_path_applicability": evidence["control_path_applicability"],
                "symlinks": evidence["symlinks"],
            })
        if not args.skip_storage:
            storage_seeds = seeds[:: max(1, len(seeds) // 3)]
            for seed, pair in storage_seeds:
                # Isolate storage workspace from the directory-property source path.
                storage_root = workspace / f"storage-check-{seed}"
                storage_root.mkdir()
                count, evidence = run_git_storage_property(binary, storage_root, seed, pair)
                checks += count
                storage_evidence.append({"seed": seed, "evidence": evidence})

    print(json.dumps({"gate": "map-generated-metamorphic", "tier": args.tier,
                      "generated_trees": len(seeds),
                      "relation_cases": checks,
                      "relation_case_scope": "explicitly counted source-path, portability, coverage, input-transformation and storage cases; additional setup/evidence guards are not counted separately; not a count of independent properties",
                      "generated_evidence": generated_evidence,
                      "guard_mutants": {"scope": "real CLI JSON with synthetic output corruption; harness guards only",
                                        "caught": guard_mutants, "count": len(guard_mutants)},
                      "storage_status": "skipped_by_request" if args.skip_storage else "passed",
                      "storage_cases": len(storage_evidence), "storage_evidence": storage_evidence,
                      "git_available": git_available, "status": "passed"}, indent=2))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
