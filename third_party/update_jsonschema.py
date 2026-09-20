#!/usr/bin/env python3
"""Reproduce dircue's private JSON Schema runtime from the pinned upstream module."""
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

ROOT = Path(__file__).resolve().parents[1]
MODULE = "github.com/santhosh-tekuri/jsonschema/v5"
VERSION = "v5.3.1"
COMMIT = "16bce71af51f6a4a775f11e649a347a8803940d3"
MODULE_SUM = "h1:lZUw3E0/J3roVtGQ+SCrUrg3ON6NgVqpn3+iol9aGu4="
MOD_SUM = "h1:uToXkOrWAZ6/Oc07xWQrPOhJotwFIyu2bBVN41fcDUY="
ZIP_SHA256 = "6c953c3751cca3003d0e7f6d775c7c3b2e4b1eeb1fa2e8d68786ead53b083094"
PATCH = ROOT / "third_party/patches/jsonschema-lazy-metaschemas.patch"


def digest(content):
    return hashlib.sha256(content).hexdigest()


def selected(name):
    path = PurePosixPath(name)
    return len(path.parts) == 1 and (name == "LICENSE" or path.suffix == ".go" and not name.endswith("_test.go"))


def hashes(root):
    result = {}
    for path in sorted(root.iterdir()):
        if path.is_symlink():
            raise RuntimeError("unexpected symlink in private snapshot")
        if path.is_file() and selected(path.name):
            result[path.name] = digest(path.read_bytes())
    return result


def regenerate(stage):
    env = {**os.environ, "GOWORK": "off", "GOFLAGS": "", "GOENV": "off",
           "GOPROXY": "https://proxy.golang.org", "GOSUMDB": "sum.golang.org",
           "GOPRIVATE": "", "GONOPROXY": "", "GONOSUMDB": "", "GOINSECURE": ""}
    module = json.loads(subprocess.check_output(
        ["go", "mod", "download", "-json", MODULE + "@" + VERSION], cwd=stage, env=env, text=True))
    if module.get("Sum") != MODULE_SUM or module.get("GoModSum") != MOD_SUM:
        raise RuntimeError("upstream module checksum differs")
    archive = Path(module["Zip"])
    if digest(archive.read_bytes()) != ZIP_SHA256:
        raise RuntimeError("upstream archive checksum differs")
    prefix = MODULE + "@" + VERSION + "/"
    with zipfile.ZipFile(archive) as source:
        for item in source.infolist():
            if item.is_dir():
                continue
            if not item.filename.startswith(prefix):
                raise RuntimeError("unexpected archive prefix")
            name = item.filename[len(prefix):]
            path = PurePosixPath(name)
            if path.is_absolute() or ".." in path.parts or "\\" in name or path.as_posix() != name or stat.S_ISLNK(item.external_attr >> 16):
                raise RuntimeError("unsafe upstream archive entry")
            if selected(name):
                (stage / name).write_bytes(source.read(item))
    upstream = hashes(stage)
    subprocess.run(["git", "apply", "--check", str(PATCH)], cwd=stage, check=True)
    subprocess.run(["git", "apply", str(PATCH)], cwd=stage, check=True)
    record = {"module": MODULE, "version": VERSION, "upstream_commit": COMMIT,
              "module_sum": MODULE_SUM, "go_mod_sum": MOD_SUM, "archive_sha256": ZIP_SHA256,
              "selection": "Root production Go files and LICENSE; excludes upstream tests, module file and optional HTTP loader. Project-owned README/tests are outside the upstream snapshot.",
              "patch": str(PATCH.relative_to(ROOT)), "patch_sha256": digest(PATCH.read_bytes()),
              "generator_script": "third_party/update_jsonschema.py", "generator_script_sha256": digest(Path(__file__).read_bytes()),
              "upstream_files": upstream, "files": hashes(stage)}
    (stage / "PROVENANCE.json").write_text(json.dumps(record, indent=2, sort_keys=True) + "\n")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--output", type=Path, default=ROOT / "internal/jsonschema")
    parser.add_argument("--check", action="store_true")
    args = parser.parse_args()
    output = args.output.absolute()
    if output.is_symlink():
        parser.error("output must not be a symlink")
    if args.check and not output.is_dir():
        parser.error("check requires an existing snapshot")
    if not args.check and output.exists() and (not output.is_dir() or any(output.iterdir())):
        parser.error("output must be absent or empty; use --check for an existing snapshot")
    with tempfile.TemporaryDirectory(prefix="dircue-jsonschema-") as temp:
        stage = Path(temp)
        regenerate(stage)
        if args.check:
            if hashes(stage) != hashes(output) or (stage / "PROVENANCE.json").read_bytes() != (output / "PROVENANCE.json").read_bytes():
                raise RuntimeError("private JSON Schema snapshot differs from pinned source and patch")
            print(f"verified {len(hashes(stage))} upstream-derived files and provenance")
        else:
            output.parent.mkdir(parents=True, exist_ok=True)
            shutil.copytree(stage, output, dirs_exist_ok=True)
            print(f"generated private snapshot in {output}")


if __name__ == "__main__":
    main()
