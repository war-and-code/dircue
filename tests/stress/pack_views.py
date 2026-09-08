#!/usr/bin/env python3
"""Add standalone packed-object views without copying large checkout payloads."""
import argparse
import copy
import hashlib
import json
import os
from pathlib import Path
import shutil
import time

from generate import git, inventory


def file_hash(path):
    with path.open("rb") as stream:
        return hashlib.file_digest(stream, "sha256").hexdigest()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--fixtures", type=Path, required=True)
    args = parser.parse_args()
    root = args.fixtures.resolve()
    manifest_path = root/"manifest.json"
    original = manifest_path.read_bytes()
    manifest = json.loads(original)
    if not manifest.get("finished_at_utc") or manifest.get("packed_views"):
        parser.error("requires a completed, not yet extended fixture manifest")
    names = ["talend", "xml-log"]
    fixtures = {fixture["name"]: fixture for fixture in manifest["fixtures"]}
    if any(name not in fixtures for name in names):
        parser.error("Talend and XML-log base fixtures are required")
    if any((root/(name+"-packed")).exists() for name in names):
        parser.error("packed destinations already exist; never overwrite them")
    needed = 2*sum(fixtures[name]["git_storage"]["logical_bytes"] for name in names)
    if shutil.disk_usage(root).free < needed:
        parser.error(f"need conservative {needed} free bytes for packed Git views")
    started = time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime())
    for name in names:
        source_fixture = fixtures[name]
        source = root/source_fixture["git"]
        destination = root/(name+"-packed")/"git"
        destination.parent.mkdir(parents=True)
        # No checkout means no source filters, hooks, builds or scripts execute.
        # Shared objects are temporary: repack copies every reachable object and
        # alternates are then removed before a standalone fsck.
        git(root, "clone", "--quiet", "--no-checkout", "--shared", "--", str(source), str(destination))
        latest = source_fixture["variants"][-1]
        for item in latest["files"]:
            target = destination/item["path"]
            target.parent.mkdir(parents=True, exist_ok=True)
            if item["path"] == ".gitattributes":
                shutil.copyfile(source/item["path"], target)
            else:
                os.link(source/item["path"], target)
        git(destination, "-c", "pack.threads=1", "repack", "-a", "-d", "--window=0", "--depth=0")
        (destination/".git/objects/info/alternates").unlink()
        git(destination, "fsck", "--full", "--strict")
        fixture = copy.deepcopy(source_fixture)
        fixture["name"] = name+"-packed"
        fixture["git"] = f"{name}-packed/git"
        fixture["git_storage"] = inventory(destination/".git")
        fixture["packed_backend"] = {
            "source_fixture": name, "delta_window": 0, "delta_depth": 0,
            "standalone_fsck_passed": True, "alternates_removed": True,
            "payload_views": "hardlinked current checkout; existing immutable flat variants reused",
            "packs": [{"name": path.name, "bytes": path.stat().st_size,
                       "sha256": file_hash(path)}
                      for path in sorted((destination/".git/objects/pack").iterdir())]}
        manifest["fixtures"].append(fixture)
        print(json.dumps({"packed": name, "git_storage": fixture["git_storage"]}), flush=True)
    seen = set()
    allocated = 0
    for current, _, files in os.walk(root):
        for name in files:
            info = (Path(current)/name).stat()
            key = info.st_dev, info.st_ino
            if key not in seen:
                seen.add(key)
                allocated += info.st_blocks*512
    manifest["unique_allocated_bytes"] = allocated
    manifest["disk_free_after"] = shutil.disk_usage(root).free
    manifest["packed_views"] = {"script_sha256": hashlib.sha256(Path(__file__).read_bytes()).hexdigest(),
                                "base_manifest_sha256": hashlib.sha256(original).hexdigest(),
                                "started_at_utc": started,
                                "finished_at_utc": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime())}
    temporary = manifest_path.with_suffix(".json.tmp")
    temporary.write_text(json.dumps(manifest, indent=2)+"\n")
    temporary.replace(manifest_path)

if __name__ == "__main__":
    main()
