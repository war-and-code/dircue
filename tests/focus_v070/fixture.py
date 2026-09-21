#!/usr/bin/env python3
"""Authored focus fixtures and their independent selection expectations."""

from __future__ import annotations

import hashlib
import json
from pathlib import Path
import argparse


DOTNET_FILES: dict[str, bytes] = {
    "Directory.Build.props": b"<Project><PropertyGroup><LangVersion>preview</LangVersion></PropertyGroup></Project>\n",
    "shared.props": b"<Project><PropertyGroup><Nullable>enable</Nullable></PropertyGroup></Project>\n",
    "app/App.csproj": b"""<Project Sdk="Microsoft.NET.Sdk">
  <Import Project="../shared.props" Condition="Exists('../shared.props')" />
  <ItemGroup>
    <ProjectReference Include="../lib/Lib.csproj" />
    <Compile Include="../shared/Linked.cs" Link="Linked.cs" />
  </ItemGroup>
</Project>
""",
    "app/Program.cs": b"namespace App; class Program { static int Main() => 0; }\n",
    "app/Nested/Helper.cs": b"namespace App; static class Helper { public static int Twice(int x) => x * 2; }\n",
    "app/notes.txt": b"authored non-source file\n",
    "app/vendor/package.json": b'{"name":"boundary"}\n',
    "app/vendor/NotOwned.cs": b"namespace Vendor; class NotOwned {}\n",
    "app/broken/Broken.csproj": b"<Project><ItemGroup>\n",
    "app/broken/NotOwned.cs": b"namespace Broken; class NotOwned {}\n",
    "lib/Lib.csproj": b'<Project Sdk="Microsoft.NET.Sdk" />\n',
    "lib/Lib.cs": b"namespace Lib; public class Value {}\n",
    "shared/Linked.cs": b"namespace Shared; public class Linked {}\n",
}

DOTNET_PRIMARY = [
    "app/App.csproj",
    "app/Nested/Helper.cs",
    "app/Program.cs",
    "app/notes.txt",
]
DOTNET_RELATED = ["lib/Lib.cs", "lib/Lib.csproj"]

PYTHON_FILES: dict[str, bytes] = {
    "pyproject.toml": b"""[tool.uv.workspace]
members = ["packages/*"]
""",
    "uv.lock": b"version = 1\nrevision = 1\n",
    "packages/api/pyproject.toml": b"""[project]
name = "focus-api"
version = "0.1.0"
dependencies = ["focus-shared"]

[tool.uv.sources]
focus-shared = { workspace = true }
""",
    "packages/api/api.py": b"def twice(value: int) -> int:\n    return value * 2\n",
    "packages/api/README.md": b"# API\n",
    "packages/api/generated/Cargo.toml": b"[package]\nname = \"boundary\"\nversion = \"0.1.0\"\n",
    "packages/api/generated/not_owned.py": b"raise RuntimeError('not executed')\n",
    "packages/shared/pyproject.toml": b"""[project]
name = "focus-shared"
version = "0.1.0"
""",
    "packages/shared/shared.py": b"VALUE = 2\n",
}

PYTHON_PRIMARY = [
    "packages/api/README.md",
    "packages/api/api.py",
    "packages/api/pyproject.toml",
]
PYTHON_RELATED = [
    "packages/shared/pyproject.toml",
    "packages/shared/shared.py",
]


def write_files(root: Path, files: dict[str, bytes]) -> None:
    for name, content in files.items():
        target = root / name
        target.parent.mkdir(parents=True, exist_ok=True)
        target.write_bytes(content)


def case_specs() -> dict[str, dict[str, object]]:
    return {
        "dotnet": {
            "project": "app/App.csproj",
            "related": "lib/Lib.csproj",
            "affected_by": "Directory.Build.props",
            "primary": DOTNET_PRIMARY,
            "related_files": DOTNET_RELATED,
            "required_context": ["Directory.Build.props", "shared.props"],
            "expected_status": "partial",
        },
        "python": {
            "project": "packages/api/pyproject.toml",
            "related": "packages/shared/pyproject.toml",
            "affected_by": "uv.lock",
            "primary": PYTHON_PRIMARY,
            "related_files": PYTHON_RELATED,
            "required_context": ["pyproject.toml", "uv.lock"],
            "expected_status": "partial",
        },
    }


def prepare(base: Path) -> dict[str, dict[str, object]]:
    dotnet = base / "dotnet"
    python = base / "python"
    write_files(dotnet, DOTNET_FILES)
    write_files(python, PYTHON_FILES)
    specs = case_specs()
    specs["dotnet"]["root"] = dotnet
    specs["python"]["root"] = python
    return specs


def manifest(root: Path) -> dict[str, dict[str, object]]:
    result: dict[str, dict[str, object]] = {}
    for item in sorted(root.rglob("*")):
        if not item.is_file():
            continue
        content = item.read_bytes()
        result[item.relative_to(root).as_posix()] = {
            "bytes": len(content),
            "sha256": hashlib.sha256(content).hexdigest(),
        }
    return result


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--output", required=True, type=Path)
    args = parser.parse_args()
    if args.output.exists():
        raise AssertionError("choose a fresh fixture directory")
    cases = prepare(args.output)
    print(json.dumps({"manifest": manifest(args.output), "cases": {name: {key: value for key, value in case.items() if key != "root"} for name, case in cases.items()}}, indent=2, sort_keys=True))


if __name__ == "__main__":
    main()
