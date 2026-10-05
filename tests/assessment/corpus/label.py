#!/usr/bin/env python3
"""Derive npm lockfile association labels for pinned corpus checkouts without dircue.

npm decides workspace ownership: `npm prefix --offline`, run in each project
directory, prints the workspace root that owns the project, or the project
itself. The owner's package-lock.json then decides the state: observed when it
records the project, missing when there is no lock and the project declares
dependencies or workspaces, and not_applicable when it declares neither. Projects that npm's
answer cannot settle are left unlabeled: projects where npm itself fails,
invalid manifests, invalid ancestors, nested workspace roots, both lock kinds at once, and any Yarn, pnpm, Bun, or
Rush evidence. Those cases are covered by unit tests and the counts in
expectations.json.

    python3 tests/assessment/corpus/label.py --corpus-root .cache/corpus --id playwright --write
"""
import argparse
import json
import os
import subprocess
from pathlib import Path, PurePosixPath

HERE = Path(__file__).resolve().parent
EXPECTATIONS = HERE / "expectations.json"
DEPENDENCY_FIELDS = ("dependencies", "devDependencies", "optionalDependencies", "peerDependencies")
ALTERNATIVE_MARKERS = {"yarn.lock", "pnpm-lock.yaml", "bun.lock", "bun.lockb", "pnpm-workspace.yaml", "rush.json"}
LOCKS = ("npm-shrinkwrap.json", "package-lock.json")


def tracked(repo):
    out = subprocess.run(["git", "-C", str(repo), "ls-files", "-z"], check=True, capture_output=True).stdout
    return [p for p in out.decode().split("\0") if p and "node_modules" not in PurePosixPath(p).parts]


def load(path):
    try:
        value = json.loads(path.read_bytes())
    except (ValueError, UnicodeDecodeError):
        return None
    return value if isinstance(value, dict) else None


def npm_owner(repo, directory):
    env = dict(os.environ, npm_config_offline="true", npm_config_update_notifier="false")
    out = subprocess.run(["npm", "prefix"], cwd=repo / directory, env=env, capture_output=True, text=True)
    if out.returncode != 0:
        return None
    owner = Path(out.stdout.strip()).resolve()
    return PurePosixPath(owner.relative_to(repo.resolve()).as_posix())


def label(repo):
    files = tracked(repo)
    present = set(files)
    manifests = {PurePosixPath(p).parent: load(repo / p) for p in files if PurePosixPath(p).name == "package.json"}
    alternative = {PurePosixPath(p).parent for p in files if PurePosixPath(p).name in ALTERNATIVE_MARKERS}
    alternative |= {d for d, m in manifests.items() if m and isinstance(m.get("packageManager"), str)
                    and not m["packageManager"].startswith("npm@")}

    def chain(directory):
        return [directory, *directory.parents]

    def lock_at(directory):
        locks = [directory / name for name in LOCKS if str(directory / name).removeprefix("./") in present]
        return locks

    labels = {}
    for directory, manifest in sorted(manifests.items()):
        if manifest is None or any(manifests.get(d, {}) is None for d in chain(directory)):
            continue
        if any(d in alternative for d in chain(directory)):
            continue
        owner = npm_owner(repo, directory)
        if owner is None:
            continue
        own, owned = lock_at(directory), lock_at(owner)
        if len(own) > 1 or len(owned) > 1 or (owner != directory and "workspaces" in manifest):
            continue
        # A workspace root's lockfile records its members, so members count as
        # declarations.
        declares = bool(manifest.get("workspaces")) or any(
            isinstance(manifest.get(f), dict) and manifest[f] for f in DEPENDENCY_FIELDS)
        if owner != directory and own:
            state = "indeterminate"
        elif owned:
            lock = load(repo / owned[0]) or {}
            packages = lock.get("packages")
            if lock.get("lockfileVersion") not in (2, 3) or not isinstance(packages, dict) or "" not in packages:
                state = "unsupported"
            elif owner == directory:
                state = "observed"
            else:
                state = "observed" if str(directory.relative_to(owner)) in packages else "indeterminate"
        else:
            state = "missing" if declares else "not_applicable"
        labels[str(directory / "package.json").removeprefix("./")] = state
    return labels


def main():
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("--corpus-root", type=Path, required=True)
    parser.add_argument("--id", action="append", required=True)
    parser.add_argument("--write", action="store_true", help="store the labels in expectations.json")
    args = parser.parse_args()
    data = json.loads(EXPECTATIONS.read_text())
    npm_version = subprocess.run(["npm", "--version"], check=True, capture_output=True, text=True).stdout.strip()
    for entry in data["repositories"]:
        if entry["id"] not in args.id:
            continue
        repo = args.corpus_root / entry["id"]
        head = subprocess.run(["git", "-C", str(repo), "rev-parse", "HEAD"], check=True, capture_output=True, text=True).stdout.strip()
        if head != entry["commit"]:
            raise SystemExit(f"{entry['id']}: checkout is {head}, expected {entry['commit']}")
        states = label(repo)
        entry["npm_contexts"] = dict(method=f"label.py with npm {npm_version} prefix --offline", states=states)
        counts = {}
        for state in states.values():
            counts[state] = counts.get(state, 0) + 1
        print(entry["id"], len(states), "labeled", counts)
    if args.write:
        EXPECTATIONS.write_text(json.dumps(data, indent=2) + "\n")


if __name__ == "__main__":
    main()
