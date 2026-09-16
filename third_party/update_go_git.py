#!/usr/bin/env python3
"""Reproduce the maintained go-git snapshot from a pinned module archive."""
import argparse
import hashlib
import json
import os
from pathlib import Path, PurePosixPath
import shutil
import stat
import subprocess
import tempfile
import zipfile

HERE = Path(__file__).resolve().parent
MODULE = "github.com/go-git/go-git/v5"
VERSION = "v5.19.2"
MODULE_SUM = "h1:wkfn7vOlUBu8ivAWKBWisTiwJK4jYHzTF8Ndv1LyGqY="
MOD_SUM = "h1:QqCBE1EFN5ddFmrliLQ3/ntRCUjZU3EJuwuB/jWEHjk="
ZIP_SHA256 = "8912b30dd8f38491ea74ab818cacc50b0083fafb4c5c896b2824e36090dab133"
PATCH = HERE / "patches/go-git-reader-delta.patch"
INCLUDED_FILES = {"LICENSE", "go.mod", "go.sum", "plumbing/format/packfile/patch_delta_test.go"}


def digest(content):
    return hashlib.sha256(content).hexdigest()


def selected(name):
    path = PurePosixPath(name)
    return name in INCLUDED_FILES or (
        path.suffix == ".go" and not path.name.endswith("_test.go")
        and "_examples" not in path.parts and "test" not in path.parts)


def tree_files(root):
    files = {}
    for path in sorted(root.rglob("*")):
        if path.is_symlink():
            raise RuntimeError(f"unexpected symlink: {path}")
        if path.is_file():
            files[path.relative_to(root).as_posix()] = digest(path.read_bytes())
    return files


def regenerate(stage):
    environment = {**os.environ, "GOWORK": "off", "GOFLAGS": "", "GOENV": "off",
                   "GOPROXY": "https://proxy.golang.org", "GOSUMDB": "sum.golang.org",
                   "GOPRIVATE": "", "GONOPROXY": "", "GONOSUMDB": "", "GOINSECURE": ""}
    module = json.loads(subprocess.check_output(
        ["go", "mod", "download", "-json", f"{MODULE}@{VERSION}"],
        cwd=stage, env=environment, text=True))
    if module.get("Sum") != MODULE_SUM or module.get("GoModSum") != MOD_SUM:
        raise RuntimeError("upstream Go module checksums differ")
    archive = Path(module["Zip"])
    if digest(archive.read_bytes()) != ZIP_SHA256:
        raise RuntimeError("upstream archive SHA-256 differs")
    prefix = f"{MODULE}@{VERSION}/"
    with zipfile.ZipFile(archive) as source:
        for entry in source.infolist():
            if entry.is_dir():
                continue
            if not entry.filename.startswith(prefix):
                raise RuntimeError("unexpected archive prefix")
            name = entry.filename[len(prefix):]
            path = PurePosixPath(name)
            if (path.is_absolute() or ".." in path.parts or "\\" in name
                    or path.as_posix() != name or stat.S_ISLNK(entry.external_attr >> 16)):
                raise RuntimeError(f"unsafe archive path: {name}")
            if selected(name):
                target = stage.joinpath(*path.parts)
                target.parent.mkdir(parents=True, exist_ok=True)
                target.write_bytes(source.read(entry))
    upstream_files = tree_files(stage)
    subprocess.run(["git", "apply", "--check", str(PATCH)], cwd=stage, check=True)
    subprocess.run(["git", "apply", str(PATCH)], cwd=stage, check=True)
    provenance = {
        "module": MODULE, "version": VERSION, "module_sum": MODULE_SUM,
        "go_mod_sum": MOD_SUM, "archive_sha256": ZIP_SHA256,
        "selection": "production Go files excluding _examples and test directories; LICENSE, go.mod, go.sum, patch_delta_test.go",
        "patch": "third_party/patches/go-git-reader-delta.patch",
        "patch_sha256": digest(PATCH.read_bytes()),
        "generator_script": "third_party/update_go_git.py",
        "generator_script_sha256": digest(Path(__file__).read_bytes()),
        "upstream_files": upstream_files, "files": tree_files(stage),
    }
    (stage / "PROVENANCE.json").write_text(json.dumps(provenance, indent=2, sort_keys=True) + "\n")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--output", type=Path, default=HERE / "go-git")
    parser.add_argument("--check", action="store_true", help="regenerate separately and compare every file")
    args = parser.parse_args()
    output = args.output.absolute()
    if output.is_symlink():
        parser.error("output must not be a symlink")
    if args.check and not output.is_dir():
        parser.error("check requires an existing output directory")
    if not args.check and output.exists() and (not output.is_dir() or any(output.iterdir())):
        parser.error("output must be absent or empty; use --check to verify an existing snapshot")
    with tempfile.TemporaryDirectory(prefix="dircue-go-git-") as temporary:
        stage = Path(temporary)
        regenerate(stage)
        expected = tree_files(stage)
        if args.check:
            actual = tree_files(output)
            different = sorted(name for name in expected.keys() | actual.keys()
                               if expected.get(name) != actual.get(name))
            if different:
                raise RuntimeError("snapshot differs: " + ", ".join(different))
            print(f"verified {len(expected)} files against pinned upstream and patch")
        else:
            output.parent.mkdir(parents=True, exist_ok=True)
            shutil.copytree(stage, output, dirs_exist_ok=True)
            print(f"generated {len(expected)} files in {output}")


if __name__ == "__main__":
    main()
