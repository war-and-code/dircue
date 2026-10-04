"""Small, reproducible source trees for map metamorphic checks.

The generator uses only the standard library.  It deliberately emits more than
the ecosystem manifests: related source and data files give Git enough similar
objects to exercise real pack/delta storage as well as map traversal.
"""

from __future__ import annotations

import os
import random
import sys
from dataclasses import dataclass
from pathlib import Path


@dataclass(frozen=True)
class EcosystemPair:
    name: str
    first: str
    second: str


ECOSYSTEM_PAIRS = (
    EcosystemPair("npm-go", "npm", "go"),
    EcosystemPair("python-cargo", "python", "cargo"),
    EcosystemPair("dotnet-maven", "dotnet", "maven"),
)

# Fixed and reviewable event-CI seeds: one per ecosystem pair.  On-demand
# generation expands each family to four seeds.
CI_SEEDS = ((10101, ECOSYSTEM_PAIRS[0]), (20202, ECOSYSTEM_PAIRS[1]), (30303, ECOSYSTEM_PAIRS[2]))
MANUAL_SEEDS = tuple(
    (base + variant, pair)
    for pair, base in zip(ECOSYSTEM_PAIRS, (10101, 20202, 30303))
    for variant in range(4)
)


def _write(root: Path, relative: str, content: str) -> None:
    path = root / relative
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(content, encoding="utf-8", newline="\n")


def _add_ecosystem(root: Path, ecosystem: str, name: str) -> list[str]:
    """Write a tiny but recognizable package and return its language markers."""
    if ecosystem == "npm":
        _write(root, f"services/{name}/package.json", '{"name":"%s","version":"1.0.0","scripts":{"start":"node index.js"}}\n' % name)
        _write(root, f"services/{name}/index.js", "const port = process.env.PORT || 8080;\nmodule.exports = { port };\n")
        return ["JavaScript"]
    if ecosystem == "go":
        _write(root, f"services/{name}/go.mod", f"module example.test/{name}\n\ngo 1.22\n")
        _write(root, f"services/{name}/main.go", "package main\n\nfunc main() {}\n")
        return ["Go"]
    if ecosystem == "python":
        _write(root, f"services/{name}/pyproject.toml", f"[project]\nname = \"{name}\"\nversion = \"1.0.0\"\n")
        _write(root, f"services/{name}/app.py", "def main():\n    return 200\n")
        return ["Python"]
    if ecosystem == "cargo":
        _write(root, f"services/{name}/Cargo.toml", f"[package]\nname = \"{name}\"\nversion = \"0.1.0\"\nedition = \"2021\"\n")
        _write(root, f"services/{name}/src/main.rs", "fn main() {}\n")
        return ["Rust"]
    if ecosystem == "dotnet":
        _write(root, f"services/{name}/{name}.csproj", '<Project Sdk="Microsoft.NET.Sdk"><PropertyGroup><TargetFramework>net8.0</TargetFramework></PropertyGroup></Project>\n')
        _write(root, f"services/{name}/Program.cs", "internal static class Program { static void Main() {} }\n")
        return ["C#"]
    if ecosystem == "maven":
        class_name = "".join(part.capitalize() for part in name.split("-") if part)
        _write(root, f"services/{name}/pom.xml", f"<project><modelVersion>4.0.0</modelVersion><groupId>example.test</groupId><artifactId>{name}</artifactId><version>1.0.0</version></project>\n")
        _write(root, f"services/{name}/src/main/java/example/{class_name}.java", f"package example;\npublic class {class_name} {{}}\n")
        return ["Java"]
    raise ValueError(f"unknown ecosystem: {ecosystem}")


def _can_create_control_name() -> bool:
    # Git and POSIX permit most C0 controls in path components; Windows and
    # Python's Windows filesystem layer reject them.  NUL and slash are never
    # valid in a single component.
    return os.name != "nt" and sys.platform != "win32"


def create_tree(root: Path, seed: int, pair: EcosystemPair) -> dict:
    """Populate an empty root and return deterministic applicability evidence."""
    rng = random.Random(seed)
    root.mkdir(parents=True, exist_ok=True)
    _write(root, "README.md", f"# Generated {pair.name} repository\nSeed: {seed}\n")
    _write(root, ".gitignore", "target/\nnode_modules/\n")

    language_markers = []
    language_markers.extend(_add_ecosystem(root, pair.first, f"{pair.first}-service"))
    language_markers.extend(_add_ecosystem(root, pair.second, f"{pair.second}-service"))

    # 64 repeated-shape, slightly varied files give aggressive repack a real
    # chance to choose deltas. Contents are all tracked and byte-stable.
    for index in range(64):
        suffix = rng.randrange(1_000_000)
        _write(root, f"catalog/item-{index:03}.yaml",
               f"apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: item-{index:03}\n  labels:\n    family: generated\n    revision: {suffix}\ndata:\n  message: stable catalog entry {index:03} for seed {seed}\n")

    # More than 50 nested directories, with Unicode in names. Keep paths short
    # enough that the generated depth is usable on standard Windows builders.
    deep = root / "deep"
    deep.mkdir()
    for level in range(55):
        deep = deep / (f"d{level:02}-雪" if level == 27 else f"d{level:02}")
        deep.mkdir()
    deep_file = deep / "leaf.py"
    deep_file.write_text("VALUE = 1\n", encoding="utf-8", newline="\n")

    # Unicode is portable across supported filesystems.  A POSIX control-name
    # case is an additional probe, with explicit applicability metadata.
    unicode_name = "observability/名前-Δ.go"
    _write(root, unicode_name, "package probe\n\nconst UnicodePathProbe = 1\n")
    control_name = "control-\x01-name.py"
    control_supported = _can_create_control_name()
    if control_supported:
        _write(root, control_name, "CONTROL_PATH_PROBE = 1\n")

    source_path_by_ecosystem = {
        "npm": "services/npm-service/index.js",
        "go": "services/go-service/main.go",
        "python": "services/python-service/app.py",
        "cargo": "services/cargo-service/src/main.rs",
        "dotnet": "services/dotnet-service/Program.cs",
        "maven": "services/maven-service/src/main/java/example/MavenService.java",
    }
    language_for_ecosystem = {
        "npm": "JavaScript", "go": "Go", "python": "Python",
        "cargo": "Rust", "dotnet": "C#", "maven": "Java",
    }
    expected_language_paths = {"Go": [unicode_name], "Python": [str(deep_file.relative_to(root).as_posix())]}
    if control_supported:
        expected_language_paths["Python"].append(control_name)
    for ecosystem in (pair.first, pair.second):
        expected_language_paths.setdefault(language_for_ecosystem[ecosystem], []).append(source_path_by_ecosystem[ecosystem])

    nested_link_parent = root / "deep"
    for level in range(3):
        nested_link_parent = nested_link_parent / f"d{level:02}"
    nested_target = os.path.relpath(root / "README.md", nested_link_parent)
    symlinks = []
    for relative, target in (("links/root-link", "../README.md"),
                             ("deep-link", "deep"),
                             (str((nested_link_parent / "nested-link").relative_to(root)), nested_target)):
        path = root / relative
        path.parent.mkdir(parents=True, exist_ok=True)
        try:
            path.symlink_to(target, target_is_directory=(relative == "deep-link"))
            symlinks.append({"path": relative, "created": True, "reason": ""})
        except (OSError, NotImplementedError) as exc:
            symlinks.append({"path": relative, "created": False,
                             "reason": f"{type(exc).__name__}: {exc}"})

    return {
        "seed": seed,
        "ecosystem_pair": pair.name,
        "expected_language_markers": sorted(set(language_markers + ["Go", "Python"])),
        "expected_language_paths": expected_language_paths,
        "expected_language_file_counts": {
            "Go": 1 + int(pair.first == "go" or pair.second == "go"),
            "Python": 1 + int(pair.first == "python" or pair.second == "python") + int(control_supported),
            **{
                language: 1
                for language in language_markers
                if language not in {"Go", "Python"}
            },
        },
        "deepest_file": str(deep_file.relative_to(root).as_posix()),
        "deep_directory_count": 55,
        "unicode_path": unicode_name,
        "control_path": control_name if control_supported else None,
        "control_path_applicability": "supported" if control_supported else "unsupported-on-windows",
        "symlinks": symlinks,
        "tracked_catalog_files": 64,
    }
