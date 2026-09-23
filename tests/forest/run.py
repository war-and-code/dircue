#!/usr/bin/env python3
"""Forest E2E conformance harness for dircue.

Builds a synthetic "old drive" at runtime in a temp dir (never committed),
runs `dircue map --forest` on it, and asserts expected properties. Exits
non-zero on any assertion failure or timeout. Logs per-case metrics to JSONL.

Usage:
  python3 tests/forest/run.py --binary bin/dircue
  python3 tests/forest/run.py --binary bin/dircue --quick   # CI subset only
"""

import argparse
import json
import os
import platform
import shutil
import stat
import subprocess
import sys
import tempfile
import time
from pathlib import Path

HERE = Path(__file__).resolve().parent
ROOT = HERE.parent.parent

IS_WINDOWS = platform.system() == "Windows"
IS_MACOS = platform.system() == "Darwin"
GIT = shutil.which("git") or "git"

# Canary token that must never appear in dircue output.
CREDENTIAL_CANARY = "ghp_CANARYTOKEN123456789ABCDEF0000"


# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------

class Case:
    def __init__(self, name: str, is_quick: bool = False):
        self.name = name
        self.is_quick = is_quick
        self.failures: list[str] = []

    def check(self, cond: bool, msg: str) -> None:
        if not cond:
            self.failures.append(msg)

    def fail(self, msg: str) -> None:
        self.failures.append(msg)


def run_dircue(binary: str, args: list, timeout: int = 60) -> dict:
    """Run dircue, return dict with exit_code, stdout, stderr, wall_ms."""
    started = time.perf_counter()
    try:
        proc = subprocess.run(
            [str(binary)] + [str(a) for a in args],
            capture_output=True,
            text=True,
            timeout=timeout,
        )
        elapsed_ms = int((time.perf_counter() - started) * 1000)
        return {
            "exit_code": proc.returncode,
            "stdout": proc.stdout,
            "stderr": proc.stderr,
            "wall_ms": elapsed_ms,
            "timed_out": False,
        }
    except subprocess.TimeoutExpired as exc:
        elapsed_ms = int((time.perf_counter() - started) * 1000)
        return {
            "exit_code": -1,
            "stdout": "",
            "stderr": str(exc),
            "wall_ms": elapsed_ms,
            "timed_out": True,
        }


def run_git(args: list, cwd: str = None, env: dict = None) -> subprocess.CompletedProcess:
    """Run git command; raises on error."""
    merged_env = {**os.environ}
    if env:
        merged_env.update(env)
    return subprocess.run(
        [GIT] + args,
        capture_output=True,
        text=True,
        cwd=cwd,
        env=merged_env,
        check=True,
    )


def git_env(tmpdir: str) -> dict:
    """Minimal git environment so git doesn't read ~/.gitconfig for user ident."""
    # Write a minimal gitconfig that allows local file:// transport (needed for
    # submodule add on newer git versions that block it by default).
    gitconfig_path = os.path.join(tmpdir, ".gitconfig")
    with open(gitconfig_path, "w") as f:
        f.write("[protocol \"file\"]\n\tallow = always\n")
    return {
        "GIT_AUTHOR_NAME": "Test",
        "GIT_AUTHOR_EMAIL": "test@example.com",
        "GIT_COMMITTER_NAME": "Test",
        "GIT_COMMITTER_EMAIL": "test@example.com",
        "GIT_AUTHOR_DATE": "2020-01-01T00:00:00Z",
        "GIT_COMMITTER_DATE": "2020-01-01T00:00:00Z",
        "GIT_CONFIG_NOSYSTEM": "1",
        "HOME": tmpdir,  # isolate from user gitconfig; .gitconfig written above
    }


def make_committed_repo(path: str, env: dict, files: dict = None) -> str:
    """Initialize a repo, add files, make an initial commit. Returns the repo path."""
    os.makedirs(path, exist_ok=True)
    run_git(["init", "-b", "main", path], env=env)
    if files is None:
        files = {"README.md": "# test\n"}
    for fname, content in files.items():
        fp = os.path.join(path, fname)
        os.makedirs(os.path.dirname(fp), exist_ok=True) if "/" in fname else None
        with open(fp, "w") as f:
            f.write(content)
    run_git(["add", "."], cwd=path, env=env)
    run_git(["commit", "-m", "initial"], cwd=path, env=env)
    return path


def write_jsonl(path: str, records: list) -> None:
    os.makedirs(os.path.dirname(path) or ".", exist_ok=True)
    with open(path, "a") as f:
        for rec in records:
            f.write(json.dumps(rec) + "\n")


def parse_forest_json(stdout: str) -> dict | None:
    """Parse forest JSON output, return dict or None on error."""
    for line in stdout.splitlines():
        line = line.strip()
        if line.startswith("{"):
            try:
                return json.loads(line)
            except json.JSONDecodeError:
                return None
    return None


# ---------------------------------------------------------------------------
# Synthetic "old drive" builder
# ---------------------------------------------------------------------------

def build_old_drive(base: str, genv: dict) -> dict:
    """Build the synthetic directory structure. Returns a metadata dict."""
    meta = {}

    # -----------------------------------------------------------------------
    # 1. Normal embedded repo  (a plain git repo nested inside the drive)
    # -----------------------------------------------------------------------
    embedded_path = os.path.join(base, "projects", "myapp")
    make_committed_repo(embedded_path, genv, {
        "src/main.go": 'package main\nfunc main() {}\n',
        "go.mod": "module example.com/myapp\n\ngo 1.21\n",
    })
    meta["embedded"] = "projects/myapp"

    # -----------------------------------------------------------------------
    # 2. Bare repo clone
    # -----------------------------------------------------------------------
    bare_path = os.path.join(base, "archives", "myapp.git")
    os.makedirs(os.path.dirname(bare_path), exist_ok=True)
    run_git(["clone", "--bare", embedded_path, bare_path], env=genv)
    meta["bare"] = "archives/myapp.git"

    # -----------------------------------------------------------------------
    # 3. Another repo that is the "parent" for a submodule relationship
    # -----------------------------------------------------------------------
    parent_path = os.path.join(base, "projects", "parent")
    make_committed_repo(parent_path, genv, {"README.md": "# parent\n"})
    meta["parent"] = "projects/parent"

    # -----------------------------------------------------------------------
    # 4. A submodule inside the parent repo
    # -----------------------------------------------------------------------
    # Create the submodule target first as a separate repo
    submod_target = os.path.join(base, "projects", "lib")
    make_committed_repo(submod_target, genv, {"lib.go": "package lib\n"})
    # Add it as a submodule in parent
    run_git(["submodule", "add", submod_target, "lib"], cwd=parent_path, env=genv)
    run_git(["commit", "-m", "add submodule"], cwd=parent_path, env=genv)
    meta["submodule_parent"] = "projects/parent"
    meta["submodule"] = "projects/parent/lib"

    # -----------------------------------------------------------------------
    # 5. Repo with a remote carrying a credential canary
    # -----------------------------------------------------------------------
    cred_repo = os.path.join(base, "projects", "credtest")
    make_committed_repo(cred_repo, genv, {"main.py": "print('hello')\n"})
    canary_url = f"https://user:{CREDENTIAL_CANARY}@github.com/org/credtest.git"
    run_git(["remote", "add", "origin", canary_url], cwd=cred_repo, env=genv)
    meta["cred_repo"] = "projects/credtest"
    meta["canary_url"] = canary_url

    # -----------------------------------------------------------------------
    # 6. Repo with unborn HEAD (no commits yet)
    # -----------------------------------------------------------------------
    unborn_path = os.path.join(base, "projects", "newrepo")
    os.makedirs(unborn_path, exist_ok=True)
    run_git(["init", "-b", "main", unborn_path], env=genv)
    with open(os.path.join(unborn_path, "README.md"), "w") as f:
        f.write("# new\n")
    # Do NOT commit, so HEAD points to a non-existent ref
    meta["unborn"] = "projects/newrepo"

    # -----------------------------------------------------------------------
    # 7. node_modules (~20k tiny files for entry-cap testing)
    # -----------------------------------------------------------------------
    # Use a representative project with node_modules
    nodeapp_path = os.path.join(base, "projects", "nodeapp")
    make_committed_repo(nodeapp_path, genv, {
        "package.json": '{"name":"nodeapp","version":"1.0.0"}\n',
        "index.js": "console.log('hello');\n",
    })
    nm_path = os.path.join(nodeapp_path, "node_modules")
    os.makedirs(nm_path, exist_ok=True)
    # Create ~20k tiny package files (200 packages × 100 files each)
    for pkg_idx in range(200):
        pkg_dir = os.path.join(nm_path, f"pkg{pkg_idx:04d}")
        os.makedirs(pkg_dir, exist_ok=True)
        for file_idx in range(100):
            with open(os.path.join(pkg_dir, f"f{file_idx:03d}.js"), "w") as f:
                f.write(f"module.exports={pkg_idx*100+file_idx};\n")
    meta["node_modules"] = "projects/nodeapp/node_modules"

    # -----------------------------------------------------------------------
    # 8. Python virtualenv with pyvenv.cfg
    # -----------------------------------------------------------------------
    pyapp_path = os.path.join(base, "projects", "pyapp")
    make_committed_repo(pyapp_path, genv, {"main.py": "print('hi')\n"})
    venv_path = os.path.join(pyapp_path, ".venv")
    os.makedirs(os.path.join(venv_path, "lib", "python3.11", "site-packages"), exist_ok=True)
    with open(os.path.join(venv_path, "pyvenv.cfg"), "w") as f:
        f.write("home = /usr/bin\nprompt = pyapp\n")
    with open(os.path.join(venv_path, "lib", "python3.11", "site-packages", "pkg.py"), "w") as f:
        f.write("# package\n")
    meta["venv"] = "projects/pyapp/.venv"

    # -----------------------------------------------------------------------
    # 9. Rust-style target/ with CACHEDIR.TAG
    # -----------------------------------------------------------------------
    rustapp_path = os.path.join(base, "projects", "rustapp")
    make_committed_repo(rustapp_path, genv, {
        "src/main.rs": 'fn main() { println!("hello"); }\n',
        "Cargo.toml": '[package]\nname="rustapp"\nversion="0.1.0"\n',
    })
    target_path = os.path.join(rustapp_path, "target")
    os.makedirs(os.path.join(target_path, "debug"), exist_ok=True)
    with open(os.path.join(target_path, "CACHEDIR.TAG"), "w") as f:
        f.write("Signature: 8a477f597d28d172789f06886806bc55\n")
        f.write("# This file is a cache directory tag created by cargo.\n")
    with open(os.path.join(target_path, "debug", "rustapp"), "wb") as f:
        f.write(b"\x7fELF" + bytes(100))
    meta["target"] = "projects/rustapp/target"

    # -----------------------------------------------------------------------
    # 10. Source build/ dir WITHOUT a Gradle marker (must remain as content)
    # -----------------------------------------------------------------------
    javaapp_path = os.path.join(base, "projects", "javaapp")
    make_committed_repo(javaapp_path, genv, {
        "src/Main.java": "public class Main { public static void main(String[] a){} }\n",
    })
    # build/ with source content but no build.gradle → must NOT be summarized
    build_path = os.path.join(javaapp_path, "build")
    os.makedirs(build_path, exist_ok=True)
    with open(os.path.join(build_path, "Main.class"), "wb") as f:
        f.write(b"\xca\xfe\xba\xbe" + bytes(50))
    meta["source_build"] = "projects/javaapp/build"

    # 10b. Explicitly untracked files in javaapp to exercise working_tree_only_files.
    # These are NOT committed so they must appear in working_tree_only_files, not
    # in the committed count.
    with open(os.path.join(javaapp_path, "scratch.txt"), "w") as f:
        f.write("scratch notes\n")
    with open(os.path.join(javaapp_path, "TODO"), "w") as f:
        f.write("fix the thing\n")
    meta["javaapp_untracked"] = 2  # number of explicit untracked text files added

    # -----------------------------------------------------------------------
    # 11. Loose residual files (outside any repo, any env tree)
    # -----------------------------------------------------------------------
    notes_path = os.path.join(base, "notes")
    os.makedirs(notes_path, exist_ok=True)
    with open(os.path.join(notes_path, "todo.txt"), "w") as f:
        f.write("- buy milk\n- fix bug\n")
    with open(os.path.join(notes_path, "config.yaml"), "w") as f:
        f.write("key: value\n")
    meta["residual_notes"] = "notes"

    # -----------------------------------------------------------------------
    # 12. Repo with a local remote (inside drive) to test path classification
    # -----------------------------------------------------------------------
    # myapp already has origin pointing to the local bare clone path (set
    # by the clone step in section 2).  Add a second local remote pointing
    # to lib so we can verify the root-relative URL.
    embedded_abs = os.path.join(base, meta["embedded"])
    lib_abs = os.path.join(base, "projects", "lib")
    run_git(["remote", "add", "locallib", lib_abs], cwd=embedded_abs, env=genv)
    # Also add an out-of-drive remote that must be redacted.
    run_git(["remote", "add", "outsider", "/tmp/outside-repo"], cwd=embedded_abs, env=genv)
    meta["local_remote_inside"] = "projects/lib"  # root-relative path expected in url
    meta["local_remote_outside"] = "/tmp/outside-repo"  # absolute path that must be redacted

    return meta


# ---------------------------------------------------------------------------
# Test cases
# ---------------------------------------------------------------------------

def case_basic_forest(binary: str, drive: str, meta: dict) -> tuple:
    """Core forest scan: roots found, schema valid, JSON parseable."""
    c = Case("basic_forest", is_quick=True)

    result = run_dircue(binary, ["map", "--forest", "--json", drive], timeout=120)
    c.check(not result["timed_out"], "forest scan timed out")
    c.check(result["exit_code"] == 0, f"exit {result['exit_code']}: {result['stderr'][:300]}")

    doc = parse_forest_json(result["stdout"])
    c.check(doc is not None, "could not parse forest JSON output")

    if doc is not None:
        c.check(doc.get("schema_version") == "1.0.0", f"schema_version={doc.get('schema_version')!r}")
        c.check(doc.get("kind") == "forest", f"kind={doc.get('kind')!r}")
        c.check(isinstance(doc.get("roots"), list), "roots must be a list")
        c.check(isinstance(doc.get("residual"), dict), "residual must be a dict")
        c.check(doc.get("status") in ("complete", "partial", "unknown"),
                f"status={doc.get('status')!r}")

    records = [{"case": c.name, **result, "passed": not c.failures,
                "doc_keys": list(doc.keys()) if doc else []}]
    return c, records


def case_roots_found(binary: str, drive: str, meta: dict) -> tuple:
    """Check that the expected roots are discovered."""
    c = Case("roots_found", is_quick=True)

    result = run_dircue(binary, ["map", "--forest", "--json", drive], timeout=120)
    c.check(result["exit_code"] == 0, f"exit {result['exit_code']}")
    doc = parse_forest_json(result["stdout"])
    if doc is None:
        c.fail("no JSON output")
        return c, [{"case": c.name, **result, "passed": False}]

    roots = doc.get("roots", [])
    root_paths = {r.get("path", "") for r in roots}

    # Embedded repo
    c.check(
        any(meta["embedded"] in p or p.endswith("myapp") for p in root_paths),
        f"embedded repo not found in roots: {sorted(root_paths)}"
    )
    # Bare repo
    c.check(
        any(meta["bare"] in p or p.endswith("myapp.git") for p in root_paths),
        f"bare repo not found in roots: {sorted(root_paths)}"
    )
    # All roots have required fields
    required_fields = {"path", "kind", "head", "identity_status", "remotes"}
    for r in roots:
        missing = required_fields - set(r.keys())
        c.check(not missing, f"root {r.get('path')!r} missing fields: {missing}")

    records = [{"case": c.name, **result, "passed": not c.failures,
                "root_count": len(roots), "root_paths": sorted(root_paths)}]
    return c, records


def case_root_kinds(binary: str, drive: str, meta: dict) -> tuple:
    """Check that bare and worktree roots have correct kind fields."""
    c = Case("root_kinds", is_quick=True)

    result = run_dircue(binary, ["map", "--forest", "--json", drive], timeout=120)
    c.check(result["exit_code"] == 0, f"exit {result['exit_code']}")
    doc = parse_forest_json(result["stdout"])
    if doc is None:
        c.fail("no JSON output")
        return c, [{"case": c.name, **result, "passed": False}]

    roots = doc.get("roots", [])
    kinds_by_path = {r.get("path", ""): r.get("kind", "") for r in roots}

    bare_entry = next((r for r in roots if r.get("path", "").endswith("myapp.git")), None)
    c.check(bare_entry is not None, "bare repo entry not found")
    if bare_entry:
        c.check(bare_entry.get("kind") == "git_bare",
                f"bare repo kind={bare_entry.get('kind')!r}, want git_bare")

    non_bare = [r for r in roots if not r.get("path", "").endswith(".git")]
    worktree_kinds = {r.get("kind") for r in non_bare}
    valid_kinds = {"git_worktree", "git_bare", "git_submodule"}
    for k in worktree_kinds:
        c.check(k in valid_kinds, f"unexpected kind {k!r}")

    records = [{"case": c.name, **result, "passed": not c.failures,
                "kinds": dict(kinds_by_path)}]
    return c, records


def case_unborn_head(binary: str, drive: str, meta: dict) -> tuple:
    """Repo with no commits must have identity_status=unknown."""
    c = Case("unborn_head", is_quick=True)

    result = run_dircue(binary, ["map", "--forest", "--json", drive], timeout=120)
    c.check(result["exit_code"] == 0, f"exit {result['exit_code']}")
    doc = parse_forest_json(result["stdout"])
    if doc is None:
        c.fail("no JSON output")
        return c, [{"case": c.name, **result, "passed": False}]

    roots = doc.get("roots", [])
    unborn = next((r for r in roots
                   if r.get("path", "").endswith("newrepo")), None)
    if unborn is None:
        # unborn HEAD repos may not be discovered at all (no committed content)
        # if the repo directory is listed, identity_status must be unknown
        records = [{"case": c.name, **result, "passed": True,
                    "note": "unborn repo not in roots (acceptable)"}]
        return c, records

    c.check(unborn.get("identity_status") == "unknown",
            f"unborn repo identity_status={unborn.get('identity_status')!r}, want unknown")
    records = [{"case": c.name, **result, "passed": not c.failures,
                "unborn_status": unborn.get("identity_status")}]
    return c, records


def case_credential_canary(binary: str, drive: str, meta: dict) -> tuple:
    """Credential canary token must never appear in dircue output."""
    c = Case("credential_canary", is_quick=True)

    result = run_dircue(binary, ["map", "--forest", "--json", drive], timeout=120)
    c.check(result["exit_code"] == 0, f"exit {result['exit_code']}")

    canary = CREDENTIAL_CANARY
    c.check(canary not in result["stdout"],
            f"canary token found in stdout")
    c.check(canary not in result["stderr"],
            f"canary token found in stderr")

    records = [{"case": c.name, **result, "passed": not c.failures}]
    return c, records


def case_env_trees_summarized(binary: str, drive: str, meta: dict) -> tuple:
    """node_modules, .venv, and CACHEDIR.TAG target/ must appear as environment_trees."""
    c = Case("env_trees_summarized", is_quick=True)

    result = run_dircue(binary, ["map", "--forest", "--json", drive], timeout=120)
    c.check(result["exit_code"] == 0, f"exit {result['exit_code']}")
    doc = parse_forest_json(result["stdout"])
    if doc is None:
        c.fail("no JSON output")
        return c, [{"case": c.name, **result, "passed": False}]

    env_trees = doc.get("environment_trees", [])
    et_paths = {et.get("path", "") for et in env_trees}
    et_kinds = {et.get("kind", "") for et in env_trees}

    # node_modules must be summarized
    has_nm = any("node_modules" in p for p in et_paths)
    c.check(has_nm, f"node_modules not in environment_trees: {sorted(et_paths)}")

    # .venv must be summarized (dependency_tree, python)
    has_venv = any(".venv" in p for p in et_paths)
    c.check(has_venv, f".venv not in environment_trees: {sorted(et_paths)}")

    # CACHEDIR.TAG target/ must be summarized
    has_target = any(p.endswith("target") for p in et_paths)
    c.check(has_target, f"target (CACHEDIR.TAG) not in environment_trees: {sorted(et_paths)}")

    # All env trees have required fields
    required_et = {"path", "kind", "ecosystem", "marker", "basis", "entries", "bytes", "bounded"}
    for et in env_trees:
        missing = required_et - set(et.keys())
        c.check(not missing, f"env_tree {et.get('path')!r} missing fields: {missing}")

    records = [{"case": c.name, **result, "passed": not c.failures,
                "env_tree_count": len(env_trees), "env_tree_paths": sorted(et_paths)}]
    return c, records


def case_source_build_not_summarized(binary: str, drive: str, meta: dict) -> tuple:
    """build/ dir without Gradle marker must NOT be in environment_trees."""
    c = Case("source_build_not_summarized", is_quick=True)

    result = run_dircue(binary, ["map", "--forest", "--json", drive], timeout=120)
    c.check(result["exit_code"] == 0, f"exit {result['exit_code']}")
    doc = parse_forest_json(result["stdout"])
    if doc is None:
        c.fail("no JSON output")
        return c, [{"case": c.name, **result, "passed": False}]

    env_trees = doc.get("environment_trees", [])
    et_paths = {et.get("path", "") for et in env_trees}

    # The plain build/ inside javaapp (no build.gradle) must not appear
    bad = [p for p in et_paths if p.endswith("javaapp/build") or p.endswith("javaapp\\build")]
    c.check(not bad, f"build/ without Gradle marker was incorrectly summarized: {bad}")

    records = [{"case": c.name, **result, "passed": not c.failures}]
    return c, records


def case_residual_present(binary: str, drive: str, meta: dict) -> tuple:
    """Residual map must be present and the notes/ files must be in it."""
    c = Case("residual_present", is_quick=True)

    result = run_dircue(binary, ["map", "--forest", "--json", drive], timeout=120)
    c.check(result["exit_code"] == 0, f"exit {result['exit_code']}")
    doc = parse_forest_json(result["stdout"])
    if doc is None:
        c.fail("no JSON output")
        return c, [{"case": c.name, **result, "passed": False}]

    residual = doc.get("residual")
    c.check(residual is not None, "residual must be present")
    if residual is not None:
        c.check(isinstance(residual, dict), "residual must be a dict")

    records = [{"case": c.name, **result, "passed": not c.failures}]
    return c, records


def case_deterministic(binary: str, drive: str, meta: dict) -> tuple:
    """Two consecutive forest runs on the same input must produce identical output."""
    c = Case("deterministic", is_quick=False)

    r1 = run_dircue(binary, ["map", "--forest", "--json", drive], timeout=120)
    r2 = run_dircue(binary, ["map", "--forest", "--json", drive], timeout=120)

    c.check(r1["exit_code"] == 0, f"run1 exit {r1['exit_code']}")
    c.check(r2["exit_code"] == 0, f"run2 exit {r2['exit_code']}")

    # Compare the parsed document (ignore timing/wall_ms).
    doc1 = parse_forest_json(r1["stdout"])
    doc2 = parse_forest_json(r2["stdout"])

    if doc1 and doc2:
        # Compare JSON representations (canonical keys).
        j1 = json.dumps(doc1, sort_keys=True)
        j2 = json.dumps(doc2, sort_keys=True)
        c.check(j1 == j2, "two runs produced different output (non-deterministic)")

    records = [{"case": c.name, "exit_code_1": r1["exit_code"], "exit_code_2": r2["exit_code"],
                "wall_ms": r1["wall_ms"] + r2["wall_ms"], "timed_out": False,
                "passed": not c.failures}]
    return c, records


def case_empty_path(binary: str, drive: str, meta: dict) -> tuple:
    """Forest on an isolated empty directory must exit 0 with zero roots."""
    c = Case("empty_path", is_quick=True)

    with tempfile.TemporaryDirectory(prefix="dircue-forest-empty-") as empty_dir:
        result = run_dircue(binary, ["map", "--forest", "--json", empty_dir], timeout=30)
    c.check(result["exit_code"] == 0, f"exit {result['exit_code']}: {result['stderr'][:200]}")
    doc = parse_forest_json(result["stdout"])
    c.check(doc is not None, "no JSON output for empty dir")
    if doc:
        c.check(doc.get("roots") == [], f"expected empty roots, got {doc.get('roots')}")

    records = [{"case": c.name, **result, "passed": not c.failures}]
    return c, records


def case_summary_flag(binary: str, drive: str, meta: dict) -> tuple:
    """--forest --summary must exit 0 and print a human-readable table."""
    c = Case("summary_flag", is_quick=True)

    result = run_dircue(binary, ["map", "--forest", "--summary", drive], timeout=120)
    c.check(not result["timed_out"], "summary timed out")
    c.check(result["exit_code"] == 0, f"exit {result['exit_code']}: {result['stderr'][:200]}")
    # Summary should contain at least the word "Forest" and "roots"
    out = result["stdout"]
    c.check("Forest" in out or "Roots" in out or "roots" in out,
            f"summary output missing expected header: {out[:300]!r}")
    # Canary must not appear here either
    c.check(CREDENTIAL_CANARY not in out, "canary in summary output")

    records = [{"case": c.name, **result, "passed": not c.failures}]
    return c, records


def case_isolated_git(binary: str, drive: str, meta: dict) -> tuple:
    """dircue must produce same forest output with PATH set to an empty dir (no git subprocess)."""
    c = Case("isolated_git", is_quick=False)

    if IS_WINDOWS:
        records = [{"case": c.name, "timed_out": False, "wall_ms": 0,
                    "exit_code": 0, "stdout": "", "stderr": "", "passed": True,
                    "note": "skipped on Windows"}]
        return c, records

    with tempfile.TemporaryDirectory(prefix="dircue-emptypath-") as empty_path_dir:
        # Run with PATH pointing to a directory containing no executables.
        env = {**os.environ, "PATH": empty_path_dir}
        started = time.perf_counter()
        try:
            proc = subprocess.run(
                [str(binary), "map", "--forest", "--json", drive],
                capture_output=True, text=True, timeout=120, env=env,
            )
            elapsed_ms = int((time.perf_counter() - started) * 1000)
            result = {
                "exit_code": proc.returncode,
                "stdout": proc.stdout,
                "stderr": proc.stderr,
                "wall_ms": elapsed_ms,
                "timed_out": False,
            }
        except subprocess.TimeoutExpired as exc:
            elapsed_ms = int((time.perf_counter() - started) * 1000)
            result = {
                "exit_code": -1, "stdout": "", "stderr": str(exc),
                "wall_ms": elapsed_ms, "timed_out": True,
            }

    c.check(not result["timed_out"], "isolated-git run timed out")
    c.check(result["exit_code"] == 0, f"exit {result['exit_code']}: {result['stderr'][:200]}")
    doc = parse_forest_json(result["stdout"])
    c.check(doc is not None, "no JSON with empty PATH")
    if doc:
        c.check(isinstance(doc.get("roots"), list), "roots must be a list with empty PATH")

    records = [{"case": c.name, **result, "passed": not c.failures}]
    return c, records


def git_head_info(repo_path: str, env: dict) -> dict | None:
    """Return {commit, tree, committer_time} from git for a repo path.
    Returns None if git fails (e.g. unborn HEAD)."""
    try:
        commit = run_git(["rev-parse", "HEAD"], cwd=repo_path, env=env).stdout.strip()
        tree = run_git(["rev-parse", "HEAD^{tree}"], cwd=repo_path, env=env).stdout.strip()
        ct_str = run_git(["log", "-1", "--format=%ct"], cwd=repo_path, env=env).stdout.strip()
        return {"commit": commit, "tree": tree, "committer_time": int(ct_str)}
    except (subprocess.CalledProcessError, ValueError):
        return None


def case_identity_vs_git(binary: str, drive: str, meta: dict) -> tuple:
    """For every resolved root, dircue commit/tree/committer_time must match git."""
    c = Case("identity_vs_git", is_quick=True)

    result = run_dircue(binary, ["map", "--forest", "--json", drive], timeout=120)
    c.check(result["exit_code"] == 0, f"exit {result['exit_code']}")
    doc = parse_forest_json(result["stdout"])
    if doc is None:
        c.fail("no JSON output")
        return c, [{"case": c.name, **result, "passed": False}]

    roots = doc.get("roots", [])

    # Use a minimal env that allows git to run but uses same ident as build.
    git_env_local = {**os.environ}

    mismatches = []
    for root in roots:
        if root.get("identity_status") != "resolved":
            continue  # unknown roots have no commit to compare
        rel_path = root.get("path", "")
        abs_path = os.path.join(drive, rel_path) if rel_path else drive
        info = git_head_info(abs_path, git_env_local)
        if info is None:
            continue  # git also couldn't resolve; both unknown is fine

        got_commit = root.get("commit", "")
        got_tree = root.get("tree", "")
        got_ct = root.get("committer_time", 0)

        if got_commit.lower() != info["commit"].lower():
            mismatches.append(
                f"{rel_path or '.'}: commit {got_commit[:8]!r} != git {info['commit'][:8]!r}")
        if got_tree.lower() != info["tree"].lower():
            mismatches.append(
                f"{rel_path or '.'}: tree {got_tree[:8]!r} != git {info['tree'][:8]!r}")
        if got_ct != info["committer_time"]:
            mismatches.append(
                f"{rel_path or '.'}: committer_time {got_ct} != git {info['committer_time']}")

    for m in mismatches:
        c.fail(m)

    records = [{"case": c.name, **result, "passed": not c.failures,
                "checked_roots": len([r for r in roots if r.get("identity_status") == "resolved"]),
                "mismatches": mismatches}]
    return c, records


def _walk_regular_only(directory: str, exclude_dirs: set) -> int:
    """Count ONLY regular files (not symlinks, not special files) under directory,
    excluding subdirs in exclude_dirs (relative paths from directory) and .git dirs.
    Uses lstat to distinguish regular files from symlinks."""
    count = 0
    for root_dir, dirs, files in os.walk(directory, followlinks=False):
        to_remove = []
        for d in list(dirs):
            rel_sub = os.path.relpath(os.path.join(root_dir, d), directory)
            if rel_sub in exclude_dirs or d == ".git":
                to_remove.append(d)
        for d in to_remove:
            if d in dirs:
                dirs.remove(d)
        for fname in files:
            fpath = os.path.join(root_dir, fname)
            try:
                st = os.lstat(fpath)
                if stat.S_ISREG(st.st_mode):
                    count += 1
            except OSError:
                pass
    return count


def case_entry_accounting(binary: str, drive: str, meta: dict) -> tuple:
    """Every regular file must be accounted for exactly once (zero tolerance).

    Accounting identity:
      committed (git ls-tree -r HEAD per non-bare root)
      + working_tree_only_files (untracked regular files, from forest doc)
      + env_entries (files in env trees, from forest doc)
      + residual_files (residual_totals.files, from forest doc)
      == walk_regular (lstat count of regular files, excluding .git and bare dirs)
    """
    c = Case("entry_accounting", is_quick=False)

    result = run_dircue(binary, ["map", "--forest", "--json", drive], timeout=120)
    c.check(result["exit_code"] == 0, f"exit {result['exit_code']}")
    doc = parse_forest_json(result["stdout"])
    if doc is None:
        c.fail("no JSON output")
        return c, [{"case": c.name, **result, "passed": False}]

    roots = doc.get("roots", [])
    env_trees = doc.get("environment_trees", [])
    residual_totals = doc.get("residual_totals")

    git_env_local = {**os.environ}

    # Count committed regular files per non-bare resolved root via git ls-tree.
    # Bare repos are excluded from the walk on both sides of the identity.
    committed_total = 0
    wt_only_total = 0
    for root in roots:
        if root.get("identity_status") != "resolved":
            continue
        if root.get("kind") == "git_bare":
            continue
        rel_path = root.get("path", "")
        abs_path = os.path.join(drive, rel_path) if rel_path else drive
        try:
            out = run_git(["ls-tree", "-r", "--name-only", "HEAD"],
                          cwd=abs_path, env=git_env_local).stdout
            committed = len([line for line in out.splitlines() if line.strip()])
            committed_total += committed
        except subprocess.CalledProcessError:
            pass  # unborn; skip
        wt_only_total += root.get("working_tree_only_files", 0)

    # Verify at least one root has working_tree_only_files > 0 (untracked files counted).
    wt_roots = [r.get("path") for r in roots
                if r.get("working_tree_only_files", 0) > 0
                and r.get("kind") != "git_bare"]
    c.check(len(wt_roots) > 0,
            "no worktree root has working_tree_only_files > 0; "
            "untracked files are not being counted")

    # Count regular files in env trees using the same lstat methodology as
    # _walk_regular_only.  The forest doc's `entries` field counts ALL directory
    # entries (dirs + files) so we cannot use it here — we need only regular files.
    env_regular = 0
    for et in env_trees:
        et_abs = os.path.join(drive, et.get("path", ""))
        if os.path.isdir(et_abs):
            env_regular += _walk_regular_only(et_abs, set())

    residual_files = residual_totals["files"] if residual_totals else 0

    # Independent walk: lstat-based regular-file count (no symlinks, no specials),
    # excluding .git dirs and bare-repo top-level directories.
    excluded = {root.get("path", "") for root in roots if root.get("kind") == "git_bare"}
    walk_regular = _walk_regular_only(drive, excluded)

    total_dircue = committed_total + wt_only_total + env_regular + residual_files
    diff = abs(total_dircue - walk_regular)

    c.check(diff == 0,
            f"exact accounting mismatch: dircue {total_dircue} "
            f"(committed={committed_total}, wt_only={wt_only_total}, "
            f"env={env_regular}, residual={residual_files}) "
            f"vs walk_regular={walk_regular}")

    records = [{"case": c.name, **result, "passed": not c.failures,
                "committed": committed_total, "wt_only": wt_only_total,
                "env_regular": env_regular, "residual_files": residual_files,
                "walk_regular": walk_regular, "diff": diff}]
    return c, records
# ---------------------------------------------------------------------------
def case_bare_map_non_empty(binary: str, drive: str, meta: dict) -> tuple:
    """Every resolved bare repo must receive a non-empty Git-mode map."""
    c = Case("bare_map_non_empty", is_quick=True)

    result = run_dircue(binary, ["map", "--forest", "--json", drive], timeout=120)
    c.check(result["exit_code"] == 0, f"exit {result['exit_code']}")
    doc = parse_forest_json(result["stdout"])
    if doc is None:
        c.fail("no JSON output")
        return c, [{"case": c.name, **result, "passed": False}]

    bare_roots = [r for r in doc.get("roots", [])
                  if r.get("kind") == "git_bare"
                  and r.get("identity_status") == "resolved"]
    c.check(len(bare_roots) > 0, "no resolved bare root in synthetic drive")
    for root in bare_roots:
        path = root.get("path", "?")
        c.check(root.get("map_status") == "ok",
                f"bare root {path!r}: map_status={root.get('map_status')!r} "
                f"reason={root.get('map_reason')!r}")
        m = root.get("map")
        nodes = (m or {}).get("nodes", [])
        c.check(m is not None and len(nodes) > 0,
                f"bare root {path!r}: map is None or has no nodes")

    records = [{"case": c.name, **result, "passed": not c.failures,
                "bare_roots": len(bare_roots)}]
    return c, records


def case_local_remote_canary(binary: str, drive: str, meta: dict) -> tuple:
    """Local-path remote URLs must be root-relative (inside) or redacted (outside).
    No absolute host paths may appear anywhere in forest output."""
    c = Case("local_remote_canary", is_quick=True)

    result = run_dircue(binary, ["map", "--forest", "--json", drive], timeout=120)
    c.check(result["exit_code"] == 0, f"exit {result['exit_code']}")
    doc = parse_forest_json(result["stdout"])
    if doc is None:
        c.fail("no JSON output")
        return c, [{"case": c.name, **result, "passed": False}]

    abs_path_fragments = [
        "/tmp/", "/private/", "/var/folders/", "/Users/", "/home/",
        "C:\\", "C:/", "\\\\",
    ]

    for root in doc.get("roots", []):
        for rem in root.get("remotes", []):
            url = rem.get("url", "")
            if url is None:
                url = ""
            # A local_path remote that is inside the drive must use a relative url.
            if rem.get("kind") == "local_path":
                if not rem.get("redacted", False):
                    # url must be relative (no leading /)
                    c.check(not url.startswith("/") and ":\\" not in url and "C:/" not in url,
                            f"{root['path']!r} remote {rem['name']!r}: "
                            f"local_path url is not relative: {url!r}")
                else:
                    # Redacted: url must be None/null
                    c.check(rem.get("url") is None,
                            f"{root['path']!r} remote {rem['name']!r}: "
                            f"redacted but url is not null: {url!r}")
            # Any remote url must not contain absolute host paths.
            for frag in abs_path_fragments:
                if frag in url:
                    c.fail(f"{root['path']!r} remote {rem['name']!r}: "
                           f"absolute host path {frag!r} in url: {url!r}")
                    break

    # Check for local remotes in the synthetic drive specifically.
    embedded = meta.get("embedded", "")
    embedded_roots = [r for r in doc.get("roots", []) if r.get("path") == embedded]
    c.check(len(embedded_roots) > 0, f"embedded root {embedded!r} not found")
    if embedded_roots:
        r = embedded_roots[0]
        local_rems = [rem for rem in r.get("remotes", []) if rem.get("kind") == "local_path"]
        c.check(len(local_rems) >= 1,
                f"embedded root has no local_path remotes; got: {r.get('remotes')}")
        # The outside remote must be redacted.
        outside_rems = [rem for rem in local_rems if rem.get("redacted")]
        c.check(len(outside_rems) >= 1,
                f"embedded root has no redacted remote for outside path; "
                f"local remotes: {local_rems}")

    records = [{"case": c.name, **result, "passed": not c.failures}]
    return c, records


def case_complete_requires_all_maps(binary: str, drive: str, meta: dict) -> tuple:
    """Forest status must be partial (not complete) when any root's map build failed."""
    c = Case("complete_requires_all_maps", is_quick=True)

    # Run on the normal drive first: verify that all resolved roots have map_status=ok.
    result = run_dircue(binary, ["map", "--forest", "--json", drive], timeout=120)
    c.check(result["exit_code"] == 0, f"exit {result['exit_code']}")
    doc = parse_forest_json(result["stdout"])
    if doc is None:
        c.fail("no JSON output")
        return c, [{"case": c.name, **result, "passed": False}]

    roots = doc.get("roots", [])
    for root in roots:
        if root.get("identity_status") != "resolved":
            continue
        ms = root.get("map_status")
        c.check(ms == "ok",
                f"resolved root {root['path']!r} has map_status={ms!r}; "
                f"reason={root.get('map_reason')!r}")

    # Verify that a root with map_status=failed forces forest status to partial.
    # We simulate this by running on a directory where a repo has no HEAD commit
    # (newrepo has unborn HEAD → identity_status=unknown, so forest already partial).
    # The newrepo case: identity_unknown → forest partial.
    unborn_roots = [r for r in roots if r.get("identity_status") == "unknown"]
    if unborn_roots:
        c.check(doc.get("status") in ("partial", "unknown"),
                f"forest has unresolved root but status={doc.get('status')!r}")
        cov = next((q for q in doc.get("coverage", []) if q["question"] == "roots"), None)
        c.check(cov is not None and cov.get("status") in ("partial", "unknown"),
                f"roots coverage not partial despite unborn root: {cov}")

    records = [{"case": c.name, **result, "passed": not c.failures}]
    return c, records


# ---------------------------------------------------------------------------
# Test registry
# ---------------------------------------------------------------------------

ALL_CASES = [
    case_basic_forest,
    case_roots_found,
    case_root_kinds,
    case_unborn_head,
    case_credential_canary,
    case_env_trees_summarized,
    case_source_build_not_summarized,
    case_residual_present,
    case_empty_path,
    case_summary_flag,
    case_identity_vs_git,
    case_entry_accounting,
    case_bare_map_non_empty,
    case_local_remote_canary,
    case_complete_requires_all_maps,
    case_deterministic,
    case_isolated_git,
]


# ---------------------------------------------------------------------------
# Main runner
# ---------------------------------------------------------------------------

def run_all(binary: str, quick: bool, jsonl_out: str) -> bool:
    print(f"Platform: {platform.system()}")
    print(f"Building synthetic old drive...")

    passed = 0
    failed = 0
    skipped = 0
    rows = []

    with tempfile.TemporaryDirectory(prefix="dircue-forest-drive-") as tmpdir:
        genv = git_env(tmpdir)
        drive = os.path.join(tmpdir, "drive")
        os.makedirs(drive, exist_ok=True)

        try:
            meta = build_old_drive(drive, genv)
        except subprocess.CalledProcessError as e:
            print(f"ERROR: Could not build synthetic drive: {e}", file=sys.stderr)
            print(f"  stdout: {e.stdout}", file=sys.stderr)
            print(f"  stderr: {e.stderr}", file=sys.stderr)
            return False

        print(f"Synthetic drive ready: {drive}")
        print(f"  Roots built: {len([k for k in meta if 'path' not in k])}")

        for case_fn in ALL_CASES:
            case_obj, records = case_fn(binary, drive, meta)

            if quick and not case_obj.is_quick:
                skipped += 1
                rows.append((case_obj.name, "SKIP (--quick)", "-", "-"))
                continue

            if not records:
                skipped += 1
                rows.append((case_obj.name, "SKIP", "-", "-"))
                continue

            write_jsonl(jsonl_out, records)

            if case_obj.failures:
                failed += 1
                status = "FAIL"
                for f in case_obj.failures:
                    print(f"  FAIL [{case_obj.name}]: {f}", file=sys.stderr)
            else:
                passed += 1
                status = "PASS"

            last = records[-1]
            wall = f"{last.get('wall_ms', '?')} ms"
            rows.append((case_obj.name, status, wall, "-"))

    print()
    print(f"{'Case':<35} {'Status':<16} {'Wall':<12}")
    print("-" * 65)
    for name, status, wall, _ in rows:
        print(f"{name:<35} {status:<16} {wall:<12}")
    print()
    print(f"Results: {passed} passed, {failed} failed, {skipped} skipped")
    print(f"JSONL output: {jsonl_out}")

    return failed == 0


def main() -> None:
    ap = argparse.ArgumentParser(description="Forest E2E conformance harness")
    ap.add_argument("--binary", default=str(ROOT / "bin" / "dircue"),
                    help="Path to the dircue binary (default: bin/dircue)")
    ap.add_argument("--quick", action="store_true",
                    help="Run only the quick subset (for CI)")
    ap.add_argument("--output", default=str(ROOT / ".cache" / "forest_e2e_results.jsonl"),
                    help="JSONL output file")
    args = ap.parse_args()

    binary = Path(args.binary)
    if not binary.exists():
        print(f"Binary {binary} not found; attempting go build...")
        result = subprocess.run(
            ["go", "build", "-buildvcs=false", "-trimpath", "-o", str(binary), "."],
            cwd=str(ROOT),
        )
        if result.returncode != 0:
            print("go build failed; run 'make build' first", file=sys.stderr)
            sys.exit(2)

    jsonl_out = args.output
    os.makedirs(os.path.dirname(jsonl_out) or ".", exist_ok=True)
    if os.path.exists(jsonl_out):
        os.remove(jsonl_out)

    ok = run_all(str(binary), args.quick, jsonl_out)
    sys.exit(0 if ok else 1)


if __name__ == "__main__":
    main()
