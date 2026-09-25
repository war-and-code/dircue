#!/usr/bin/env python3
"""Opt-in bounded check of public repositories with checked-in local binaries.

This is a source-slice validation, not a whole-repository corpus gate. It fetches
bounded declaration files and two binary files at fixed commits into a temporary
directory, verifies their references and SHA-256 digests, and optionally asks
dircue to map those slices. No fetched content is written into the checkout.
"""
from __future__ import annotations

import argparse
import hashlib
import json
import posixpath
import re
import subprocess
import tempfile
import urllib.error
import urllib.request
import xml.etree.ElementTree as ET
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
        "target_framework": "net48",
        "reference_include": "pyRevitLabs.Json",
        "reference_hint_path": "$(PyRevitDevLibsDir)\\pyRevitLabs.Json.dll",
        "build_targets": "dev/Directory.Build.targets",
        "build_targets_properties": {
            "NetFolder": ("'$(TargetFramework)' == 'net48'", "netfx"),
            "PyRevitRootDir": (None, "$(MSBuildThisFileDirectory).."),
            "PyRevitDevDir": (None, "$(PyRevitRootDir)\\dev"),
            "PyRevitDevLibsDir": (None, "$(PyRevitDevDir)\\libs\\$(NetFolder)"),
        },
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


def local_name(tag: str) -> str:
    return re.sub(r"^\{[^}]+\}", "", tag)


def validate_slice(spec: dict[str, Any], scratch: Path, binary: Path | None) -> dict[str, Any]:
    base = f"https://raw.githubusercontent.com/{spec['repo']}/{spec['commit']}/"
    manifest_bytes = fetch(base + spec["manifest"], MAX_SOURCE_BYTES)
    manifest_text = manifest_bytes.decode("utf-8")
    build_targets_path = None
    resolution = None
    build_targets_bytes = None
    source_hashes = {"manifest_sha256": hashlib.sha256(manifest_bytes).hexdigest()}
    if "build_targets" in spec:
        build_targets_bytes = fetch(base + spec["build_targets"], MAX_SOURCE_BYTES)
        try:
            manifest_root = ET.fromstring(manifest_bytes)
            build_targets_root = ET.fromstring(build_targets_bytes)
        except ET.ParseError as exc:
            raise ValueError(f"{spec['id']}: pinned project or build targets are not valid XML: {exc}") from exc
        target_frameworks = [
            (element.text or "").strip()
            for element in manifest_root.iter()
            if local_name(element.tag) == "TargetFrameworks"
        ]
        if not any(spec["target_framework"] in value.split(";") for value in target_frameworks):
            raise ValueError(f"{spec['id']}: project does not target {spec['target_framework']}: {target_frameworks!r}")
        references = [element for element in manifest_root.iter() if local_name(element.tag) == "Reference"]
        matched_reference = next(
            (element for element in references if element.attrib.get("Include") == spec["reference_include"]),
            None,
        )
        if matched_reference is None or matched_reference.attrib.get("HintPath") != spec["reference_hint_path"]:
            actual = matched_reference.attrib.get("HintPath") if matched_reference is not None else None
            raise ValueError(f"{spec['id']}: project reference HintPath mismatch: {actual!r}")
        properties: dict[str, list[tuple[str | None, str]]] = {}
        for element in build_targets_root.iter():
            name = local_name(element.tag)
            if name in spec["build_targets_properties"]:
                properties.setdefault(name, []).append((element.attrib.get("Condition"), (element.text or "").strip()))
        for name, (expected_condition, expected_value) in spec["build_targets_properties"].items():
            actual = properties.get(name, [])
            if (expected_condition, expected_value) not in actual:
                raise ValueError(f"{spec['id']}: {spec['build_targets']} {name} property mismatch: {actual!r}")
        # Resolve these declarations symbolically for the pinned project location;
        # do not invoke MSBuild or execute repository build logic.
        netfolder_value = next(
            value
            for condition, value in properties["NetFolder"]
            if condition == ("'$(TargetFramework)' == '" + spec["target_framework"] + "'")
        )
        build_targets_directory = posixpath.dirname(spec["build_targets"])
        properties_for_resolution = {
            "TargetFramework": spec["target_framework"],
            "NetFolder": netfolder_value,
            "MSBuildThisFileDirectory": f"<repository-root>/{build_targets_directory}/",
        }
        properties_for_resolution.update({
            name: next(value for condition, value in properties[name] if condition == expected_condition)
            for name, (expected_condition, _) in spec["build_targets_properties"].items()
            if name != "NetFolder"
        })

        def substitute(value: str) -> str:
            for _ in range(len(properties_for_resolution) + 1):
                updated = re.sub(
                    r"\$\(([^)]+)\)",
                    lambda match: properties_for_resolution.get(match.group(1), match.group(0)),
                    value,
                )
                if updated == value:
                    return value
                value = updated
            raise ValueError(f"{spec['id']}: cyclic property expansion in {value!r}")

        resolved_lib_dir = posixpath.normpath(substitute(properties_for_resolution["PyRevitDevLibsDir"]).replace("\\", "/"))
        resolved_reference = posixpath.normpath(substitute(matched_reference.attrib["HintPath"]).replace("\\", "/"))
        binary_relative_path = spec["binary"].replace("\\", "/")
        declared_binary = f"<repository-root>/{binary_relative_path}"
        if resolved_reference != declared_binary:
            raise ValueError(f"{spec['id']}: property resolution selected {resolved_reference}, expected {declared_binary}")
        build_targets_path = Path(spec["build_targets"])
        resolution = {
            "method": "static_property_substitution",
            "build_logic_executed": False,
            "target_framework": spec["target_framework"],
            "NetFolder": properties_for_resolution["NetFolder"],
            "PyRevitDevLibsDir": resolved_lib_dir,
            "resolved_reference": resolved_reference,
        }
        source_hashes["build_targets_sha256"] = hashlib.sha256(build_targets_bytes).hexdigest()
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
    if build_targets_path is not None:
        targets_path = root / build_targets_path
        targets_path.parent.mkdir(parents=True, exist_ok=True)
        assert build_targets_bytes is not None
        targets_path.write_bytes(build_targets_bytes)
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
    result.update(source_hashes)
    if resolution is not None:
        result["build_targets"] = spec["build_targets"]
        result["build_variable_resolution"] = resolution
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
                    "repository_population": "not measured; only the referenced binary and its bounded declaration files per pinned repository are included",
                    "package_identity": "not inferred from the binary file role; any package nodes are reported as tool observations only",
                },
            }
        print(json.dumps(report, indent=2, sort_keys=True))
    except (OSError, urllib.error.URLError, UnicodeError, ValueError, subprocess.SubprocessError, json.JSONDecodeError) as exc:
        parser.exit(1, f"unmanaged binary slice validation failed: {exc}\n")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
