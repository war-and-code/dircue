#!/usr/bin/env python3
"""Build the optional structural worker and package its pinned dependency sources.

This writes local archives only; it never creates tags or publishes a release.
"""
from __future__ import annotations

import argparse
import hashlib
import json
import os
import re
from pathlib import Path
import shutil
import subprocess
import tarfile
import tempfile
import tomllib
import zipfile

ROOT = Path(__file__).resolve().parents[1]
WORKER = ROOT / "prototypes/structural/worker"
TOOLCHAIN = "1.94.0"
SMOKE_FIXTURES = ROOT / "tests/structural_breadth/fixtures.json"
TARGETS = {
    "darwin-arm64": "aarch64-apple-darwin",
    "darwin-amd64": "x86_64-apple-darwin",
    "linux-amd64": "x86_64-unknown-linux-gnu",
    "linux-arm64": "aarch64-unknown-linux-gnu",
    "windows-amd64": "x86_64-pc-windows-msvc",
}


def run(*args: str, env: dict[str, str] | None = None) -> str:
    return subprocess.check_output(args, cwd=ROOT, text=True, env=env)


def digest(path: Path) -> str:
    return hashlib.sha256(path.read_bytes()).hexdigest()


def source_hashes() -> dict[str, str]:
    inputs = [WORKER / "Cargo.toml", WORKER / "Cargo.lock", ROOT / "LICENSE",
              ROOT / "docs/STRUCTURE.md", ROOT / "scripts/structural_worker_release.py",
              ROOT / ".github/workflows/structural-worker.yml"]
    inputs.extend(path for path in (WORKER / "src").rglob("*") if path.is_file())
    inputs.append(SMOKE_FIXTURES)
    inputs.extend(path for path in (SMOKE_FIXTURES.parent / "testdata").rglob("*") if path.is_file())
    inputs.extend(path for path in [ROOT / ".cargo/config.toml", WORKER / ".cargo/config.toml"] if path.is_file())
    return {path.relative_to(ROOT).as_posix(): digest(path) for path in sorted(inputs)}


def artifact_paths(output: Path, version: str, platform: str) -> tuple[Path, Path]:
    suffix = ".zip" if platform.startswith("windows") else ".tar.gz"
    archive = output / f"dircue-structural-worker_{version}_{platform}{suffix}"
    checksum = output / f"{archive.name}.sha256"
    for candidate in (archive, checksum):
        if candidate.exists() or candidate.is_symlink():
            raise FileExistsError(f"refusing to replace existing release artifact: {candidate}")
    return archive, checksum


def verify_staged_sources(stage: Path, expected: dict[str, str]) -> None:
    if source_hashes() != expected:
        raise RuntimeError("source inputs changed while staging the worker package")
    prefix = WORKER.relative_to(ROOT).as_posix() + "/"
    for name, checksum in expected.items():
        if name.startswith(prefix):
            staged = stage / "source" / name.removeprefix(prefix)
        elif name == "LICENSE":
            staged = stage / "LICENSE"
        elif name == "docs/STRUCTURE.md":
            staged = stage / "README.md"
        else:
            continue
        if digest(staged) != checksum:
            raise RuntimeError(f"packaged source differs from build input: {name}")


def runtime_requirements(binary: Path, platform: str) -> dict[str, object]:
    if platform.startswith("darwin"):
        linkage = run("otool", "-L", str(binary))
        load_commands = run("otool", "-l", str(binary))
        minimum = re.search(r"^\s*(?:minos|version) (11\.0(?:\.0)?)$", load_commands, re.MULTILINE)
        if minimum is None:
            raise RuntimeError("worker is missing the pinned macOS 11.0 deployment target")
        libraries = [line.strip().split(" (", 1)[0] for line in linkage.splitlines()[1:]]
        if any(not path.startswith(("/usr/lib/", "/System/Library/")) for path in libraries):
            raise RuntimeError(f"non-system macOS library dependency: {libraries}")
        return {"minimum_macos": "11.0", "dynamic_libraries": libraries}
    if platform.startswith("linux"):
        symbols = run("readelf", "--version-info", str(binary))
        versions = set(re.findall(r"GLIBC_([0-9]+(?:\.[0-9]+)+)", symbols))
        if not versions:
            raise RuntimeError("could not determine worker glibc symbol requirement")
        minimum = max(versions, key=lambda v: tuple(map(int, v.split("."))))
        linkage = run("readelf", "--dynamic", str(binary))
        libraries = re.findall(r"Shared library: \[([^]]+)\]", linkage)
        return {"minimum_glibc_symbols": minimum, "dynamic_libraries": libraries,
                "glibc_version_requirements": sorted(set(re.findall(r"GLIBC_[A-Za-z0-9_.]+", symbols))),
                "musl_supported": False}
    vswhere = Path(os.environ.get("ProgramFiles(x86)", r"C:\Program Files (x86)")) / "Microsoft Visual Studio/Installer/vswhere.exe"
    installation = run(str(vswhere), "-latest", "-products", "*", "-property", "installationPath").strip()
    tools = sorted((Path(installation) / "VC/Tools/MSVC").glob("*/bin/Hostx64/x64/dumpbin.exe"))
    if not tools:
        raise RuntimeError("dumpbin is required to verify Windows runtime dependencies")
    linkage = run(str(tools[-1]), "/DEPENDENTS", str(binary))
    libraries = sorted(set(re.findall(r"^\s+([A-Za-z0-9_.-]+\.dll)\s*$", linkage, re.MULTILINE | re.IGNORECASE)))
    if not libraries:
        raise RuntimeError("could not inspect Windows DLL dependencies")
    if any(name.upper().startswith(("VCRUNTIME", "MSVCP")) for name in libraries):
        raise RuntimeError(f"unexpected Visual C++ Redistributable dependency: {libraries}")
    return {"msvc_crt": "static", "dynamic_libraries": libraries,
            "minimum_windows": "Windows 10 or Windows Server 2016 (Rust target baseline)"}


def verify_archive(archive: Path, executable: str) -> None:
    with tempfile.TemporaryDirectory(prefix="dircue-worker-verify-") as temp:
        root = Path(temp)
        if archive.suffix == ".zip":
            with zipfile.ZipFile(archive) as handle:
                handle.extractall(root)
        else:
            with tarfile.open(archive) as handle:
                handle.extractall(root, filter="data")
        for line in (root / "SHA256SUMS").read_text().splitlines():
            expected, name = line.split("  ", 1)
            if digest(root / name) != expected:
                raise RuntimeError(f"packaged file checksum mismatch: {name}")
        if not (root / "source/crates/big-code-analysis-2.2.0.crate").is_file():
            raise RuntimeError("BCA source archive missing from package")
        fixtures = json.loads(SMOKE_FIXTURES.read_text())
        for fixture in fixtures:
            source = (SMOKE_FIXTURES.parent / "testdata" / fixture["path"]).read_text()
            request = json.dumps(dict(path=fixture["path"], language=fixture["language"],
                                      source=source, mode="combined"))
            proc = subprocess.run([str(root / executable)], input=request, capture_output=True,
                                  text=True, check=True, timeout=10)
            result = json.loads(proc.stdout)
            if (result["parse_count"] != 1 or result["status"] != fixture["status"]
                    or result["language"] != fixture["language"]
                    or result["source_bytes"] != len(source.encode())
                    or result["observations"]["syntax_nodes"] == 0
                    or not result.get("metrics")):
                raise RuntimeError(f"packaged worker smoke test failed for {fixture['language']}")



def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--platform", choices=TARGETS, required=True)
    parser.add_argument("--version", default="0.4.0")
    parser.add_argument("--smoke-test", action="store_true", help="extract and execute the archive on this host")
    parser.add_argument("--output", type=Path, default=ROOT / "dist/structural-worker")
    args = parser.parse_args()
    if not args.version or any(c not in "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789.-" for c in args.version):
        parser.error("version must contain only letters, digits, dots, and hyphens")
    archive, archive_checksum = artifact_paths(args.output, args.version, args.platform)
    target = TARGETS[args.platform]
    manifest = str(WORKER / "Cargo.toml")
    cargo = ("rustup", "run", TOOLCHAIN, "cargo")
    # Native package managers can put an unrelated rustc ahead of rustup proxies.
    # Pin the compiler itself as well as Cargo for reproducible toolchain selection.
    build_env = dict(os.environ)
    build_env["RUSTC"] = run("rustup", "which", "--toolchain", TOOLCHAIN, "rustc").strip()
    build_env["RUSTDOC"] = run("rustup", "which", "--toolchain", TOOLCHAIN, "rustdoc").strip()
    build_env.pop("CARGO_ENCODED_RUSTFLAGS", None)
    build_env["RUSTFLAGS"] = "-C target-feature=+crt-static" if args.platform.startswith("windows") else ""
    if args.platform.startswith("darwin"):
        build_env["MACOSX_DEPLOYMENT_TARGET"] = "11.0"
    initial_sources = source_hashes()
    subprocess.run([*cargo, "fetch", "--locked", "--manifest-path", manifest], cwd=ROOT, check=True, env=build_env)
    subprocess.run([*cargo, "build", "--locked", "--release", "--target", target, "--manifest-path", manifest], cwd=ROOT, check=True, env=build_env)
    metadata = json.loads(run(*cargo, "metadata", "--locked", "--format-version", "1", "--manifest-path", manifest, env=build_env))
    suffix = ".exe" if args.platform.startswith("windows") else ""
    binary = Path(metadata["target_directory"]) / target / "release" / f"dircue-structural-worker{suffix}"
    runtime = runtime_requirements(binary, args.platform)
    if source_hashes() != initial_sources:
        raise RuntimeError("source inputs changed during worker build")
    lock = tomllib.loads((WORKER / "Cargo.lock").read_text())
    checksums = {(p["name"], p["version"]): p.get("checksum") for p in lock["package"]}
    stem = f"dircue-structural-worker_{args.version}_{args.platform}"
    args.output.mkdir(parents=True, exist_ok=True)
    with tempfile.TemporaryDirectory(prefix="dircue-worker-") as temp:
        stage = Path(temp) / stem
        stage.mkdir()
        shutil.copy2(binary, stage / binary.name)
        shutil.copy2(ROOT / "LICENSE", stage / "LICENSE")
        shutil.copy2(ROOT / "docs/STRUCTURE.md", stage / "README.md")
        source = stage / "source"
        source.mkdir()
        shutil.copy2(WORKER / "Cargo.toml", source / "Cargo.toml")
        shutil.copy2(WORKER / "Cargo.lock", source / "Cargo.lock")
        shutil.copytree(WORKER / "src", source / "src")
        archives = source / "crates"
        archives.mkdir()
        packages = []
        for package in sorted(metadata["packages"], key=lambda p: (p["name"], p["version"])):
            if package["source"] is None:
                continue
            if not package["source"].startswith("registry+"):
                raise RuntimeError(f"unhandled non-registry dependency: {package['name']}")
            package_root = Path(package["manifest_path"]).parent
            # Cargo's registry source and cache paths share the same index directory.
            crate = package_root.parent.parent.parent / "cache" / package_root.parent.name / f"{package['name']}-{package['version']}.crate"
            expected = checksums[(package["name"], package["version"])]
            if not expected or digest(crate) != expected:
                raise RuntimeError(f"crate archive checksum mismatch: {crate}")
            shutil.copy2(crate, archives / crate.name)
            packages.append({"name": package["name"], "version": package["version"], "license": package["license"], "source_archive": f"source/crates/{crate.name}", "sha256": expected})
        notice = """Optional dircue structural worker

The worker's own source is MIT-licensed. big-code-analysis 2.2.0 is MPL-2.0;
its complete, unmodified Corresponding Source, license, and notices are included
in source/crates/big-code-analysis-2.2.0.crate (a gzip-compressed tar archive).
Every resolved Cargo dependency's complete crate source, including its license
and notices, is included in source/crates. See provenance.json for licenses and
archive SHA-256 values. Dependency licenses remain applicable independently of
dircue's MIT license. No separate download is needed to obtain these sources.

source/Cargo.toml, source/Cargo.lock, and source/src contain this worker's build
inputs. Build with Rust 1.94.0 and cargo build --locked --release. Initial Cargo
setup may require a configured registry; the built executable runs offline.
"""
        (stage / "THIRD_PARTY_NOTICES.txt").write_text(notice)
        verify_staged_sources(stage, initial_sources)
        provenance = {
            "version": args.version,
            "platform": args.platform,
            "target": target,
            "rust_toolchain": TOOLCHAIN,
            "commit": run("git", "rev-parse", "HEAD").strip(),
            "source_dirty": bool(run("git", "status", "--porcelain").strip()),
            "source_sha256": initial_sources,
            "runtime_requirements": runtime,
            "build_settings": {
                "rustflags": build_env["RUSTFLAGS"],
                "macos_deployment_target": build_env.get("MACOSX_DEPLOYMENT_TARGET"),
            },
            "binary_sha256": digest(binary),
            "dependencies": packages,
        }
        (stage / "provenance.json").write_text(json.dumps(provenance, indent=2) + "\n")
        checks = []
        for path in sorted(stage.rglob("*")):
            if path.is_file():
                checks.append(f"{digest(path)}  {path.relative_to(stage).as_posix()}\n")
        (stage / "SHA256SUMS").write_text("".join(checks))
        if suffix:
            with zipfile.ZipFile(archive, "x", zipfile.ZIP_DEFLATED) as handle:
                for path in sorted(stage.rglob("*")):
                    if path.is_file():
                        handle.write(path, path.relative_to(stage).as_posix())
        else:
            with archive.open("xb") as output:
                with tarfile.open(fileobj=output, mode="w:gz") as handle:
                    for path in sorted(stage.rglob("*")):
                        if path.is_file():
                            handle.add(path, arcname=path.relative_to(stage).as_posix())
        with archive_checksum.open("x") as output:
            output.write(f"{digest(archive)}  {archive.name}\n")
        if args.smoke_test:
            verify_archive(archive, binary.name)
        print(archive)


if __name__ == "__main__":
    main()
