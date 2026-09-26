#!/usr/bin/env python3
"""Attribute bounded-reader allocations with source-bound, instrumented CLI builds.

This does not measure production latency. It reads only the caller-selected XML
fixture, builds dircue itself, and never executes anything from the fixture.
"""
from __future__ import annotations

import argparse
import datetime
import hashlib
import json
import os
from pathlib import Path
import platform
import shutil
import statistics
import subprocess

ROOT = Path(__file__).resolve().parents[3]
HERE = Path(__file__).resolve().parent
GO_BINARY = "go"
READ_PATHS = ("pkg/scanner/scanner.go", "pkg/scanner/git.go")
SOURCE_FIELDS = ("GoFiles", "CgoFiles", "CFiles", "CXXFiles", "MFiles", "HFiles",
                 "FFiles", "SFiles", "SwigFiles", "SwigCXXFiles", "SysoFiles", "EmbedFiles")


def digest(data: bytes) -> str:
    return hashlib.sha256(data).hexdigest()


def sha(path: Path) -> str:
    checksum = hashlib.sha256()
    with path.open("rb") as stream:
        for block in iter(lambda: stream.read(1 << 20), b""):
            checksum.update(block)
    return checksum.hexdigest()


def write_json(path: Path, data: object) -> None:
    path.write_text(json.dumps(data, indent=2, sort_keys=True) + "\n")


def command(arguments: list[str], *, env: dict[str, str] | None = None,
            timeout: int = 180) -> bytes:
    if arguments[0] == "go":
        arguments = [GO_BINARY, *arguments[1:]]
    result = subprocess.run(arguments, cwd=ROOT, env=env, capture_output=True,
                            timeout=timeout, check=False)
    if result.returncode:
        raise RuntimeError(f"command failed ({result.returncode}): {arguments[0]}\n"
                           + result.stderr.decode(errors="replace"))
    return result.stdout


def json_stream(data: bytes) -> list[dict]:
    text = data.decode()
    decoder = json.JSONDecoder()
    documents = []
    index = 0
    while index < len(text):
        while index < len(text) and text[index].isspace():
            index += 1
        if index == len(text):
            break
        value, index = decoder.raw_decode(text, index)
        documents.append(value)
    return documents


def tracked_at(revision: str, name: str) -> bytes:
    return command(["git", "show", f"{revision}:{name}"])


COMPILED_SUFFIXES = {".go", ".c", ".cc", ".cpp", ".cxx", ".m", ".mm", ".h",
                     ".hpp", ".f", ".for", ".f90", ".s", ".swig", ".swigcxx", ".syso"}


def release_entries(revision: str) -> list[dict]:
    """Read the release tree independently of the current Go package selection."""
    entries = []
    for record in command(["git", "ls-tree", "-r", "-z", revision]).split(b"\0"):
        if not record:
            continue
        header, name = record.split(b"\t", 1)
        mode, kind, oid = header.decode().split()
        if kind != "blob":
            raise AssertionError("release tree contains an unsupported non-blob entry")
        entries.append({"path": os.fsdecode(name), "mode": mode, "oid": oid})
    if not entries:
        raise AssertionError("release tree is empty")
    return entries


def verify_release_tree(root: Path, entries: list[dict], replacements: dict[str, str],
                        instrumentation_main_sha256: str | None = None) -> dict:
    """Require complete release-file presence and unchanged compiled source.

    Build tags cannot hide a changed or removed init-only file from this check.
    Non-source blobs must remain present; selected embedded-file bytes are also
    checked by source_records after Go resolves the package graph.
    """
    allowed = set(READ_PATHS) | {"pkg/scanner/read.go"}
    if instrumentation_main_sha256 is not None:
        allowed.add("main.go")
    for original, replacement in replacements.items():
        try:
            relative = Path(original).relative_to(root).as_posix()
        except ValueError:
            raise AssertionError("baseline overlay points outside the repository") from None
        if relative not in allowed:
            raise AssertionError(f"unexpected baseline overlay: {relative}")
        if not replacement and relative != "pkg/scanner/read.go":
            raise AssertionError(f"baseline overlay removes a release source: {relative}")
    checked_source = 0
    for entry in entries:
        name = entry["path"]
        relative = Path(name)
        if relative.is_absolute() or ".." in relative.parts:
            raise AssertionError("invalid release-tree path")
        original = root / relative
        replacement = replacements.get(str(original), str(original))
        if not replacement:
            raise AssertionError(f"release file removed by overlay: {name}")
        effective = Path(replacement)
        # Test fixtures may intentionally contain dangling tracked symlinks.
        if entry["mode"] == "120000":
            present = effective.is_symlink()
        else:
            present = effective.is_file() and not effective.is_symlink()
        if not present:
            raise AssertionError(f"release file missing or changed kind: {name}")
        compiled = (relative.suffix.lower() in COMPILED_SUFFIXES
                    and not name.endswith("_test.go"))
        module_metadata = relative.name in {"go.mod", "go.sum"}
        if not compiled and not module_metadata:
            continue
        contents = (os.fsencode(os.readlink(effective)) if effective.is_symlink()
                    else effective.read_bytes())
        if name == "main.go" and instrumentation_main_sha256 is not None:
            if str(original) not in replacements or digest(contents) != instrumentation_main_sha256:
                raise AssertionError("instrumentation main does not match its explicit expected hash")
        else:
            algorithm = {40: "sha1", 64: "sha256"}.get(len(entry["oid"]))
            if algorithm is None:
                raise AssertionError("unsupported Git object identity")
            blob = b"blob " + str(len(contents)).encode() + b"\0" + contents
            if hashlib.new(algorithm, blob).hexdigest() != entry["oid"]:
                raise AssertionError(f"release compiled source differs: {name}")
        checked_source += 1
    return {"tracked_blob_count": len(entries), "compiled_source_and_module_count": checked_source,
            "tree_entries_sha256": digest(json.dumps(entries, sort_keys=True).encode()),
            "instrumentation_main_exception": instrumentation_main_sha256 is not None}


def source_records(overlay: Path, env: dict[str, str], revision: str,
                   baseline: bool, *, instrumentation_main_sha256: str | None = None) -> dict:
    """Hash the effective files, including overlays, chosen by Go's build graph."""
    replacements = json.loads(overlay.read_text())["Replace"]
    release_guard = (verify_release_tree(ROOT, release_entries(revision), replacements,
                                        instrumentation_main_sha256) if baseline else None)
    packages = json_stream(command(["go", "list", "-mod=readonly", "-deps", "-json",
                                    f"-overlay={overlay}", "."], env=env))
    records = {}
    modules = {}
    for package in packages:
        directory = Path(package["Dir"])
        module = package.get("Module")
        if module:
            module_key = module["Path"] + "@" + module.get("Version", "main")
            modules[module_key] = {
                "path": module["Path"], "version": module.get("Version"),
                "replacement": {key: module.get("Replace", {}).get(key)
                                for key in ("Path", "Version")
                                if module.get("Replace", {}).get(key)
                                and not os.path.isabs(module["Replace"][key])},
            }
        for field in SOURCE_FIELDS:
            for name in package.get(field, []):
                original = directory / name
                replacement = replacements.get(str(original), str(original))
                if not replacement:
                    continue
                effective = Path(replacement)
                try:
                    relative = original.relative_to(ROOT).as_posix()
                    key = "repo:" + relative
                except ValueError:
                    relative = None
                    if package.get("Standard"):
                        key = "stdlib:" + package["ImportPath"] + "/" + name
                    else:
                        key = "module:" + module_key + "/" + package["ImportPath"] + "/" + name
                value = sha(effective)
                if key in records and records[key]["sha256"] != value:
                    raise AssertionError(f"inconsistent build input: {key}")
                records[key] = {"sha256": value, "bytes": effective.stat().st_size}
                if baseline and relative and not (relative == "main.go" and instrumentation_main_sha256 is not None):
                    if value != digest(tracked_at(revision, relative)):
                        raise AssertionError(f"baseline differs from selected revision: {relative}")
    # Pin module selection separately from compilation units.
    for name in ("go.mod", "go.sum"):
        value = sha(ROOT / name)
        if baseline and value != digest(tracked_at(revision, name)):
            raise AssertionError(f"baseline module selection changed: {name}")
        records["repo:" + name] = {"sha256": value, "bytes": (ROOT / name).stat().st_size}
    for package in packages:
        module = package.get("Module", {})
        for metadata in (module, module.get("Replace", {})):
            filename = metadata.get("GoMod")
            if not filename:
                continue
            file = Path(filename)
            if file.exists():
                if baseline:
                    try:
                        relative = file.relative_to(ROOT).as_posix()
                    except ValueError:
                        relative = None
                    if relative and sha(file) != digest(tracked_at(revision, relative)):
                        raise AssertionError(f"baseline module metadata changed: {relative}")
                key = "module-metadata:" + module.get("Path", "") + "@" + module.get("Version", "main")
                if metadata is not module:
                    key += ":replacement"
                records[key] = {"sha256": sha(file), "bytes": file.stat().st_size}
    return {"files": records, "modules": modules, "packages": len(packages),
            "release_tree_precondition": release_guard}


def inventory(directory: Path) -> dict:
    if not directory.is_dir() or directory.is_symlink():
        raise ValueError("XML fixture must be a real directory")
    files = {}
    total = 0
    for base, dirs, names in os.walk(directory, followlinks=False):
        base = Path(base)
        for name in dirs:
            if (base / name).is_symlink():
                raise ValueError("XML fixture may not contain symlink directories")
        for name in sorted(names):
            path = base / name
            if path.is_symlink() or not path.is_file() or path.suffix.lower() != ".xml":
                raise ValueError("XML fixture must contain only regular XML files")
            size = path.stat().st_size
            files[path.relative_to(directory).as_posix()] = {"sha256": sha(path), "bytes": size}
            total += size
    if not files or total < (1 << 30):
        raise ValueError("XML fixture must contain at least 1 GiB of regular XML content")
    return {"files": dict(sorted(files.items())), "file_count": len(files), "bytes": total}


def validate_portable(value: object) -> None:
    text = json.dumps(value)
    for forbidden in (str(ROOT), str(Path.home()), "/Users/", "/home/", "\\\\Users\\\\"):
        if forbidden and forbidden in text:
            raise AssertionError("public evidence contains a host path")


def main() -> None:
    global GO_BINARY
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--go-binary", default="go", help="installed Go binary; automatic toolchain download is disabled")
    parser.add_argument("--xml-root", type=Path, required=True)
    parser.add_argument("--output", type=Path, required=True, help="fresh ignored raw-artifact directory")
    parser.add_argument("--public-output", type=Path, required=True, help="fresh portable-export directory")
    parser.add_argument("--baseline-ref", default="aa0492135186b84d3d34c57f0f27908dc73cfc24")
    parser.add_argument("--workers", type=int, default=8)
    parser.add_argument("--environment-note", required=True)
    args = parser.parse_args()
    GO_BINARY = args.go_binary
    if args.workers < 1:
        parser.error("workers must be positive")
    raw = args.output.resolve()
    public = args.public_output.resolve()
    raw.mkdir(parents=True, exist_ok=False)
    public.mkdir(parents=True, exist_ok=False)
    xml = args.xml_root.resolve()
    baseline_revision = command(["git", "rev-parse", f"{args.baseline_ref}^{{commit}}"]).decode().strip()
    head = command(["git", "rev-parse", "HEAD"]).decode().strip()
    harness_start = {"runner_sha256": sha(Path(__file__)),
                     "instrumentation_sha256": sha(HERE / "profile_main.go.txt")}
    env = dict(os.environ, CGO_ENABLED="0", GOWORK="off", GOFLAGS="",
               GOPROXY="off", GOTOOLCHAIN="local", GOSUMDB="off")
    selected_env = json.loads(command(["go", "env", "-json", "GOVERSION", "GOOS", "GOARCH",
                                      "GOROOT", "GOTOOLDIR", "GOEXPERIMENT", "GOAMD64", "GOARM64"], env=env))
    tool_dir = Path(selected_env.pop("GOTOOLDIR"))
    selected_env.pop("GOROOT")
    executable_suffix = ".exe" if os.name == "nt" else ""
    tool_paths = {name: tool_dir / (name + executable_suffix) for name in ("compile", "link", "asm")}
    go_binary = shutil.which(GO_BINARY)
    if go_binary:
        tool_paths["go"] = Path(go_binary).resolve()
    tool_hashes = {name: sha(path) for name, path in tool_paths.items()}
    source = raw / "source"
    source.mkdir()
    instrumentation = source / "main.go"
    instrumentation.write_bytes((HERE / "profile_main.go.txt").read_bytes())
    if sha(instrumentation) != harness_start["instrumentation_sha256"]:
        raise AssertionError("instrumentation changed before source copy")
    before_input = inventory(xml)
    builds = {}
    for lane in ("baseline", "candidate"):
        replacements = {str(ROOT / "main.go"): str(instrumentation)}
        if lane == "baseline":
            for name in READ_PATHS:
                destination = source / "baseline" / name
                destination.parent.mkdir(parents=True, exist_ok=True)
                destination.write_bytes(tracked_at(baseline_revision, name))
                replacements[str(ROOT / name)] = str(destination)
            replacements[str(ROOT / "pkg/scanner/read.go")] = ""
        overlay = raw / f"{lane}-overlay.json"
        write_json(overlay, {"Replace": replacements})
        before = source_records(overlay, env, baseline_revision, lane == "baseline",
                                instrumentation_main_sha256=harness_start["instrumentation_sha256"])
        binary = raw / f"{lane}-profile{executable_suffix}"
        arguments = ["go", "build", "-mod=readonly", f"-overlay={overlay}", "-trimpath", "-buildvcs=false",
                     "-ldflags=-X dircue/internal/cli.Version=0.6.0", "-o", str(binary), "."]
        command(arguments, env=env, timeout=300)
        after = source_records(overlay, env, baseline_revision, lane == "baseline",
                                instrumentation_main_sha256=harness_start["instrumentation_sha256"])
        if before != after:
            raise AssertionError("build inputs changed during instrumentation build")
        builds[lane] = {"binary_sha256": sha(binary), "inputs": before,
                        "release_binary": False, "reported_version": "0.6.0"}
    base_files = builds["baseline"]["inputs"]["files"]
    candidate_files = builds["candidate"]["inputs"]["files"]
    differences = [name for name in sorted(base_files.keys() | candidate_files.keys())
                   if base_files.get(name) != candidate_files.get(name)]
    expected = {"repo:pkg/scanner/read.go", "repo:pkg/scanner/scanner.go", "repo:pkg/scanner/git.go"}
    if set(differences) != expected:
        raise AssertionError(f"unexpected effective input differences: {differences}")
    common = ["analyze", "all", "--json", "--source", "directory", "--workers", str(args.workers),
              "--tree-size", "1000000", str(xml)]
    samples = {"baseline": [], "candidate": []}
    golden = None
    raw_artifacts = {}
    for repetition in range(3):
        order = ("baseline", "candidate") if repetition % 2 == 0 else ("candidate", "baseline")
        for lane in order:
            destination = raw / f"{lane}-{repetition}"
            destination.mkdir()
            binary = raw / f"{lane}-profile{executable_suffix}"
            process = subprocess.run([str(binary), *common], cwd=ROOT,
                                     env=dict(env, DIRCUE_PROFILE_DIR=str(destination)),
                                     capture_output=True, timeout=180, check=False)
            (destination / "stdout.json").write_bytes(process.stdout)
            (destination / "stderr.txt").write_bytes(process.stderr)
            current = (process.stdout, process.stderr, process.returncode)
            if process.returncode != 0:
                raise AssertionError(f"instrumented CLI failed: {lane}/{repetition}")
            if golden is None:
                golden = current
            if current != golden:
                raise AssertionError("instrumentation/lane/repetition changed exact CLI output or exit")
            stats = json.loads((destination / "stats.json").read_text())
            stats.update(capture=destination.name, stdout_sha256=digest(process.stdout),
                         stderr_sha256=digest(process.stderr), exit_code=process.returncode)
            samples[lane].append(stats)
            for kind, profile_name in (("alloc_space", "allocs.pprof"), ("inuse_space", "heap.pprof")):
                top = command(["go", "tool", "pprof", "-top", "-nodecount=30", "-unit=bytes",
                               f"-sample_index={kind}", str(binary), str(destination / profile_name)], env=env)
                text = top.decode()
                validate_portable(text)
                name = f"{lane}-{repetition}-{kind}-top.txt"
                (destination / (kind + "-top.txt")).write_text(text)
                (public / name).write_text(text)
            for artifact in destination.iterdir():
                raw_artifacts[artifact.relative_to(raw).as_posix()] = sha(artifact)
    if before_input != inventory(xml):
        raise AssertionError("XML fixture changed during attribution run")
    # Recheck after all samples so another writer cannot silently invalidate the comparison.
    for lane in builds:
        if builds[lane]["inputs"] != source_records(raw / f"{lane}-overlay.json", env,
                                                   baseline_revision, lane == "baseline",
                                                   instrumentation_main_sha256=harness_start["instrumentation_sha256"]):
            raise AssertionError("build inputs changed during attribution run")
    harness_end = {"runner_sha256": sha(Path(__file__)),
                   "instrumentation_sha256": sha(HERE / "profile_main.go.txt")}
    if harness_start != harness_end:
        raise AssertionError("attribution harness changed during collection")
    if tool_hashes != {name: sha(path) for name, path in tool_paths.items()}:
        raise AssertionError("toolchain executable changed during collection")
    keys = ("allocated_bytes", "allocation_count", "gc_cycles_during_operation", "after_heap_alloc_bytes",
            "post_gc_live_heap_bytes", "post_gc_heap_inuse_bytes")
    medians = {lane: {key: statistics.median(row[key] for row in rows) for key in keys}
               for lane, rows in samples.items()}
    receipt = {
        "schema": "dircue-bounded-reader-allocation-1",
        "created_at_utc": datetime.datetime.now(datetime.timezone.utc).isoformat(),
        "baseline_revision": baseline_revision, "candidate_parent_revision": head,
        "candidate_has_uncommitted_reader_changes": True,
        "source_difference_paths": differences, "instrumentation_main_sha256": sha(instrumentation),
        "build_flags": ["CGO_ENABLED=0", "GOWORK=off", "GOFLAGS=", "GOPROXY=off",
                        "GOTOOLCHAIN=local", "GOSUMDB=off", "-mod=readonly", "-trimpath", "-buildvcs=false",
                        "-X dircue/internal/cli.Version=0.6.0", "symbols retained"],
        "environment": {"platform": platform.platform(), "go": selected_env, "tool_sha256": tool_hashes,
                        "note": args.environment_note,
                        "runtime_environment": {key: env.get(key) for key in ("GOMAXPROCS", "GOGC", "GOMEMLIMIT", "GODEBUG")}},
        "command": common[:-1] + ["<verified-xml-fixture>"], "fixture": before_input,
        "samples": samples, "medians": medians,
        "median_allocated_bytes_change_percent": 100 * (medians["candidate"]["allocated_bytes"] /
                                                        medians["baseline"]["allocated_bytes"] - 1),
        "exact_stdout_stderr_exit_match": True, "production_latency_measurement": False,
        "raw_artifact_sha256": raw_artifacts,
        "build_inputs_file": "build-inputs.json",
        "reproduction": harness_start,
        "harness_and_toolchain_unchanged_during_collection": True,
        "limits": [
            "MemProfileRate=1 starts after package initialization; startup allocations are not uniformly sampled.",
            "Operation counters surround cli.Execute; forced GC and profile writing happen afterward.",
            "pprof tables attribute allocation stacks, not latency or RSS, and may include profile-capture work.",
            "The instrumentation main uses context.Background and omits production signal-handler setup equally for both variants.",
            "Three fresh processes per variant show variation; they do not establish universal allocation or retained-memory savings.",
            "The XML corpus is large but CLI analysis reads bounded prefixes, not every byte of the fixture.",
            "Local raw files retain full outputs and paths; only portable counts, hashes, identities and pprof tables are exported.",
        ],
    }
    validate_portable(receipt)
    validate_portable(builds)
    write_json(public / "build-inputs.json", builds)
    write_json(public / "allocation-results.json", receipt)
    print(json.dumps({"medians": medians, "allocation_change_percent": receipt["median_allocated_bytes_change_percent"],
                      "effective_source_differences": differences}, indent=2))


if __name__ == "__main__":
    main()
