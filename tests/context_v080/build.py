#!/usr/bin/env python3
"""Build one source-bound 0.8 candidate at fresh paths."""

from __future__ import annotations

import argparse
from datetime import datetime, timezone
import hashlib
import json
import os
from pathlib import Path
import subprocess

import common


def source_state() -> dict[str, object]:
    def git(*args: str) -> bytes:
        return subprocess.check_output(["git", *args], cwd=common.ROOT)

    status = git("status", "--porcelain=v1", "-z", "--untracked-files=all")
    return {
        "commit": git("rev-parse", "HEAD").decode().strip(),
        "head_tree": git("rev-parse", "HEAD^{tree}").decode().strip(),
        "status_sha256": hashlib.sha256(status).hexdigest(),
        "diff_sha256": hashlib.sha256(git("diff", "HEAD", "--binary")).hexdigest(),
        "dirty": bool(status),
    }


def build_inputs(env: dict[str, str]) -> dict[str, str]:
    raw = subprocess.check_output(["go", "list", "-mod=readonly", "-deps", "-json", "."], cwd=common.ROOT, env=env).decode()
    decoder = json.JSONDecoder()
    files = {common.ROOT / "go.mod", common.ROOT / "go.sum"}
    while raw.strip():
        package, end = decoder.raw_decode(raw.lstrip())
        raw = raw.lstrip()[end:]
        directory = Path(package.get("Dir", "/"))
        if directory.is_relative_to(common.ROOT):
            for field in ("GoFiles", "CgoFiles", "CFiles", "CXXFiles", "HFiles", "SFiles", "SysoFiles", "EmbedFiles"):
                files.update(directory / name for name in package.get(field, []))
            module_file = package.get("Module", {}).get("GoMod")
            if module_file:
                files.add(Path(module_file))
    return {str(item.relative_to(common.ROOT)): common.sha256(item) for item in sorted(files) if item.is_relative_to(common.ROOT)}


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--candidate", required=True, type=Path)
    parser.add_argument("--build-receipt", required=True, type=Path)
    args = parser.parse_args()
    output = args.candidate.resolve()
    receipt = args.build_receipt.resolve()
    if output.exists() or receipt.exists():
        raise AssertionError("candidate and build receipt must use fresh paths")
    output.parent.mkdir(parents=True, exist_ok=True)
    env = dict(os.environ, CGO_ENABLED="0", GOWORK="off", GOFLAGS="")
    inputs = build_inputs(env)
    state = source_state()
    command = [
        "go", "build", "-mod=readonly", "-buildvcs=false", "-trimpath", "-ldflags",
        "-s -w -X dircue/internal/cli.Version=0.8.0-dev", "-o", str(output), ".",
    ]
    started = datetime.now(timezone.utc).isoformat()
    subprocess.run(command, cwd=common.ROOT, env=env, check=True)
    if inputs != build_inputs(env) or state != source_state():
        output.unlink(missing_ok=True)
        raise AssertionError("source or compilation inputs changed during build; retry on a quiet worktree")
    value = {
        "schema": "dircue-context-v080-build-1",
        "started_at_utc": started,
        "finished_at_utc": datetime.now(timezone.utc).isoformat(),
        "source_at_build": state,
        "files": inputs,
        "candidate_sha256": common.sha256(output),
        "command": command,
        "environment": {"CGO_ENABLED": "0", "GOWORK": "off", "GOFLAGS": ""},
        "go_version": subprocess.check_output(["go", "version"], env=env).decode().strip(),
        "go_build_info": subprocess.check_output(["go", "version", "-m", str(output)], env=env).decode().strip(),
    }
    common.write_json(receipt, value)
    print(receipt)


if __name__ == "__main__":
    main()
