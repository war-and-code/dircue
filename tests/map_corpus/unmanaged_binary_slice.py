#!/usr/bin/env python3
"""Opt-in bounded check of public repositories with checked-in local binaries.

This is a source-slice validation, not a whole-repository corpus gate. It fetches
two project files and two binary files at fixed commits into a temporary
directory, verifies their references and SHA-256 digests, and optionally asks
dircue to map those slices. No fetched content is written into the checkout.
"""
from __future__ import annotations

import argparse
import hashlib
import json
import subprocess
import tempfile
import urllib.error
import urllib.request
from pathlib import Path
from typing import Any


MAX_SOURCE_BYTES = 250_000
MAX_BINARY_BYTES = 2_000_000

SLICES = (
    {
        "id": "zdh-web-local-jar",
        "repo": "zhaoyachao/zdh_web",
        "commit": "1e420dcb3ec748011958a34e1d317ad58830c636",
        "manifest": "pom.xml",
        "manifest_contains": "<systemPath>${basedir}/lib/zdh_generator.jar</systemPath>",
        "binary": "lib/zdh_generator.jar",
        "binary_size": 14276,
        "binary_sha256": "7bfbff05cbb57dd10026a54f8c54c77d205547547ab530e0100fc33d5eea7264",
        "role": "Maven system-scope local JAR reference",
        "expected_content_role": "archive",
    },
    {
        "id": "pyrevit-local-dll",
        "repo": "pyrevitlabs/pyRevit",
        "commit": "6294cf9c477130eadd73b9d156784f7a5553b4cd",
        "manifest": "dev/pyRevitLabs/pyRevitLabs.Common/pyRevitLabs.Common.csproj",
        "manifest_contains": '<Reference Include="pyRevitLabs.Json" HintPath="$(PyRevitDevLibsDir)\\pyRevitLabs.Json.dll"',
        "binary": "dev/libs/netfx/pyRevitLabs.Json.dll",
        "binary_size": 699392,
        "binary_sha256": "8eedc650cdb204ae8b1cf7f1c3b47e29d2617b875c10b48140b0e34381dfbeb3",
        "role": ".NET HintPath reference to checked-in managed DLL",
        "expected_content_role": "binary",
    },
)


def fetch(url: str, limit: int) -> bytes:
    request = urllib.request.Request(url, headers={"User-Agent": "dircue-unmanaged-binary-slice/1"})
    with urllib.request.urlopen(request, timeout=30) as response:
        length = response.headers.get("Content-Length")
        if length is not None and int(length) > limit:
            raise ValueError(f"{url}: Content-Length {length} exceeds {limit} byte limit")
        data = response.read(limit + 1)
    if len(data) > limit:
        raise ValueError(f"{url}: response exceeds {limit} byte limit")
    return data


def map_slice(binary: Path, root: Path) -> dict[str, Any]:
    completed = subprocess.run(
        [str(binary), "map", "--json", str(root)],
        check=True,
        capture_output=True,
        text=True,
        timeout=120,
    )
    document = json.loads(completed.stdout)
    roles: dict[str, int] = {}
    counts: dict[str, int] = {}
    package_names: list[str] = []
    for node in document.get("nodes", []):
        kind = node.get("kind", "unknown")
        counts[kind] = counts.get(kind, 0) + 1
        if kind == "content":
            role = node.get("properties", {}).get("role", "unknown")
            roles[role] = roles.get(role, 0) + 1
        elif kind == "package":
            package_names.append(node.get("name") or "(unnamed)")
    return {
        "status": document.get("status"),
        "node_counts": counts,
        "content_roles": roles,
        "package_nodes": len(package_names),
        "package_names": package_names,
        "coverage": document.get("coverage", []),
    }


def validate_slice(spec: dict[str, Any], scratch: Path, binary: Path | None) -> dict[str, Any]:
    base = f"https://raw.githubusercontent.com/{spec['repo']}/{spec['commit']}/"
    manifest_bytes = fetch(base + spec["manifest"], MAX_SOURCE_BYTES)
    manifest_text = manifest_bytes.decode("utf-8")
    if spec["manifest_contains"] not in manifest_text:
        raise ValueError(f"{spec['id']}: pinned manifest no longer contains its binary reference")
    binary_bytes = fetch(base + spec["binary"], MAX_BINARY_BYTES)
    digest = hashlib.sha256(binary_bytes).hexdigest()
    if len(binary_bytes) != spec["binary_size"]:
        raise ValueError(f"{spec['id']}: size mismatch, expected {spec['binary_size']}, got {len(binary_bytes)}")
    if digest != spec["binary_sha256"]:
        raise ValueError(f"{spec['id']}: SHA-256 mismatch, expected {spec['binary_sha256']}, got {digest}")

    root = scratch / spec["id"]
    manifest_path = root / spec["manifest"]
    binary_path = root / spec["binary"]
    manifest_path.parent.mkdir(parents=True, exist_ok=True)
    binary_path.parent.mkdir(parents=True, exist_ok=True)
    manifest_path.write_bytes(manifest_bytes)
    binary_path.write_bytes(binary_bytes)

    result: dict[str, Any] = {
        "id": spec["id"],
        "repo": spec["repo"],
        "commit": spec["commit"],
        "manifest": spec["manifest"],
        "binary": spec["binary"],
        "role": spec["role"],
        "binary_size": len(binary_bytes),
        "binary_sha256": digest,
        "manifest_reference_verified": True,
        "whole_repository_coverage": False,
        "package_identity_claimed": False,
    }
    if binary is not None:
        observed = map_slice(binary, root)
        if observed["content_roles"].get(spec["expected_content_role"], 0) < 1:
            raise ValueError(f"{spec['id']}: expected {spec['expected_content_role']} content role")
        if observed["package_nodes"]:
            raise ValueError(f"{spec['id']}: package identity inferred without a provider report")
        result["dircue_map"] = observed
    return result


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--binary", type=Path, help="optional dircue executable; defaults to bin/dircue when present")
    args = parser.parse_args()
    binary = args.binary.resolve() if args.binary else Path(__file__).resolve().parents[2] / "bin" / "dircue"
    if not binary.is_file():
        if args.binary:
            parser.error(f"binary does not exist: {binary}")
        binary = None
    try:
        with tempfile.TemporaryDirectory(prefix="dircue-unmanaged-binary-slice-") as temp:
            report = {
                "schema": "dircue-unmanaged-binary-source-slice-0.1",
                "opt_in": True,
                "bounded_fetch_bytes": {"source_per_file": MAX_SOURCE_BYTES, "binary_per_file": MAX_BINARY_BYTES},
                "results": [validate_slice(spec, Path(temp), binary) for spec in SLICES],
                "dircue_binary_sha256": hashlib.sha256(binary.read_bytes()).hexdigest() if binary else None,
                "claims": {
                    "repository_population": "not measured; only one manifest/project file and one referenced binary per pinned repository are included",
                    "package_identity": "not inferred from the binary file role; any package nodes are reported as tool observations only",
                },
            }
        print(json.dumps(report, indent=2, sort_keys=True))
    except (OSError, urllib.error.URLError, UnicodeError, ValueError, subprocess.SubprocessError, json.JSONDecodeError) as exc:
        parser.exit(1, f"unmanaged binary slice validation failed: {exc}\n")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
