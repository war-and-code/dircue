#!/usr/bin/env python3
"""Check production test sensitivity using an isolated worker copy and Go overlays.

Requires Go, Python 3.10+, and Rust/Cargo >=1.94 with locked dependencies available.
The output directory must not exist. An optional Cargo target cache speeds local
runs; no repository source is modified. Receipts omit host paths and raw logs.
"""

import argparse
import hashlib
import json
import os
from pathlib import Path
import signal
import subprocess
import time

ROOT = Path(__file__).resolve().parents[2]
RUST_PATH = "prototypes/structural/worker/src/hotspots.rs"
GO_PATH = "pkg/structure/hotspots.go"
PIN = {
    RUST_PATH: "2d7fb19f09b44e6fab3cbfa12a9e59710040877c4b9532d80f37ea7c8935a84f",
    GO_PATH: "c322067df881a28afbe723f17fc4aecf1c8ab6aee9b64905fa567ad20deb8737",
}
# Rust runtime ends before the cfg(test) module; Go runtime is the whole file.
RUST_MUTATIONS = [
    ("worker-reverse-ties", "entry.value >= value", "entry.value > value"),
    ("worker-top-nine", "self.top.truncate(LIMIT);", "self.top.truncate(LIMIT - 1);"),
    ("worker-bucket64-wrap", "(u64::BITS - value.leading_zeros()) as usize", "((u64::BITS - value.leading_zeros()) as usize) % 64"),
    ("worker-max-as-min", "v.min(value)", "v.max(value)"),
    ("worker-retain-before-count", "self.count += 1;", "if self.count >= LIMIT { return; }\n        self.count += 1;"),
    ("worker-strict-end-boundary", "current.end_line > source_lines", "current.end_line >= source_lines"),
    ("worker-zero-based-index", "report.total_spaces,", "report.total_spaces - 1,"),
    ("worker-skip-invalid-subtree", "report.invalid_span_spaces += 1;", "report.invalid_span_spaces += 1;\n                continue;"),
]
GO_MUTATIONS = [
    ("merge-reverse-values", "if a.Value > b.Value {\n\t\treturn -1", "if a.Value > b.Value {\n\t\treturn 1"),
    ("merge-reverse-path-ties", "strings.Compare(a.sortPath, b.sortPath)", "strings.Compare(b.sortPath, a.sortPath)"),
    ("merge-display-path-ties", "strings.Compare(a.sortPath, b.sortPath)", "strings.Compare(a.Path, b.Path)"),
    ("merge-reverse-index-ties", "if a.Index < b.Index {\n\t\treturn -1", "if a.Index < b.Index {\n\t\treturn 1"),
    ("merge-last-file-histogram", "aggregate.Histogram[bucket] += count", "aggregate.Histogram[bucket] = count"),
    ("merge-retained-count-only", "aggregate.Count += m.Count", "aggregate.Count += uint64(len(m.Top))"),
    ("merge-mixed-syntax-cohorts", "g.Language == file.Language && g.SyntaxCohort == cohort", "g.Language == file.Language"),
    ("merge-top-nine", "aggregate.Top = aggregate.Top[:HotspotLimit]", "aggregate.Top = aggregate.Top[:HotspotLimit-1]"),
    ("merge-counter-overflow", "if f.TotalSpaces > math.MaxUint64-r.TotalSpaces || r.AnalyzedFiles == math.MaxUint64 {", "if false && (f.TotalSpaces > math.MaxUint64-r.TotalSpaces || r.AnalyzedFiles == math.MaxUint64) {"),
]


def sha(data):
    return hashlib.sha256(data).hexdigest()


def rust_inputs(worker):
    # Only the worker binary target is compiled. Snapshot every source file so
    # newly added modules or include files also participate in membership checks.
    paths = {worker / "Cargo.toml", worker / "Cargo.lock"}
    paths.update(p for p in (worker / "src").rglob("*") if p.is_file())
    for name in ("build.rs", ".cargo/config", ".cargo/config.toml"):
        path = worker / name
        if path.is_file():
            paths.add(path)
    return {p.relative_to(ROOT).as_posix(): p.read_bytes() for p in sorted(paths)}


def go_inputs(env):
    result = subprocess.run(
        ["go", "list", "-mod=readonly", "-deps", "-test", "-json", "./pkg/structure"],
        cwd=ROOT, env=env, text=True, stdout=subprocess.PIPE,
        stderr=subprocess.PIPE, timeout=60, check=True,
    )
    remaining = result.stdout
    decoder = json.JSONDecoder()
    paths = {ROOT / "go.mod", ROOT / "go.sum"}
    while remaining.strip():
        package, end = decoder.raw_decode(remaining.lstrip())
        remaining = remaining.lstrip()[end:]
        directory = Path(package.get("Dir", "/")).resolve()
        if not directory.is_relative_to(ROOT):
            continue
        for field in ("GoFiles", "CgoFiles", "CFiles", "CXXFiles", "MFiles",
                      "HFiles", "FFiles", "SFiles", "SwigFiles", "SwigCXXFiles",
                      "SysoFiles", "EmbedFiles", "TestGoFiles", "XTestGoFiles",
                      "TestEmbedFiles", "XTestEmbedFiles"):
            for name in package.get(field, []):
                path = (directory / name).resolve()
                if path.is_relative_to(ROOT):
                    paths.add(path)
        module = package.get("Module", {})
        for metadata in (module, module.get("Replace", {})):
            if metadata.get("GoMod"):
                path = Path(metadata["GoMod"]).resolve()
                if path.is_relative_to(ROOT):
                    paths.add(path)
    return {p.relative_to(ROOT).as_posix(): sha(p.read_bytes()) for p in sorted(paths)}


def main():
    harness = Path(__file__).resolve()
    harness_hash = sha(harness.read_bytes())
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--output", type=Path, required=True, help="fresh scratch directory")
    parser.add_argument("--cargo", default="cargo")
    parser.add_argument("--cargo-target-dir", type=Path, help="optional reusable build cache")
    parser.add_argument("--timeout", type=int, default=600, help="seconds per build/test invocation")
    args = parser.parse_args()
    if args.timeout < 1:
        parser.error("--timeout must be positive")
    output = args.output.resolve()
    if output.exists():
        parser.error("--output must not already exist")
    original = {path: (ROOT / path).read_bytes() for path in PIN}
    for path, data in original.items():
        runtime = data.split(b"#[cfg(test)]", 1)[0] if path == RUST_PATH else data
        if sha(runtime) != PIN[path]:
            raise SystemExit(f"runtime pin differs: {path}; review mutations before updating PIN")
    output.mkdir(parents=True)
    worker = ROOT / "prototypes/structural/worker"
    isolated = output / "worker"
    rust_snapshot = rust_inputs(worker)
    for name, content in rust_snapshot.items():
        destination = isolated / (ROOT / name).relative_to(worker)
        destination.parent.mkdir(parents=True, exist_ok=True)
        destination.write_bytes(content)
    if rust_snapshot[RUST_PATH] != original[RUST_PATH]:
        raise SystemExit("worker changed while capturing inputs")
    env = dict(os.environ, GOPROXY="off", GOTOOLCHAIN="local", GOWORK="off",
               GOFLAGS="", CGO_ENABLED="0", CARGO_NET_OFFLINE="true")
    env["CARGO_TARGET_DIR"] = str(args.cargo_target_dir.resolve() if args.cargo_target_dir else output / "target")
    initial_go = go_inputs(env)
    if initial_go[GO_PATH] != sha(original[GO_PATH]):
        raise SystemExit("Go aggregator changed while capturing inputs")
    initial_rust = {p: sha(data) for p, data in rust_snapshot.items()}
    inputs = {**initial_go, **initial_rust, harness.relative_to(ROOT).as_posix(): harness_hash}
    receipt = {
        "schema": "dircue-production-mutation-check-1",
        "status": "running",
        "runtime_sha256": PIN,
        "inputs_sha256": inputs,
        "go_inputs_sha256": initial_go,
        "rust_inputs_sha256": initial_rust,
        "harness_sha256": harness_hash,
        "versions": {},
        "scope": "deterministic finite generated tests; not a proof of production correctness",
        "results": [],
    }
    for name, command in {"go": ["go", "version"], "cargo": [args.cargo, "--version"], "rustc": ["rustc", "--version"]}.items():
        receipt["versions"][name] = subprocess.check_output(command, env=env, text=True, timeout=30).strip()

    def save():
        (output / "receipt.json").write_text(json.dumps(receipt, indent=2) + "\n")

    def execute(name, language, command, mutant=None):
        started = time.monotonic()
        with subprocess.Popen(command, cwd=ROOT, env=env, stdout=subprocess.PIPE,
                              stderr=subprocess.STDOUT, text=True,
                              start_new_session=os.name != "nt") as child:
            try:
                text, _ = child.communicate(timeout=args.timeout)
                code = child.returncode
                assertion_failed = ("test result: FAILED" in text) if language == "rust" else ("--- FAIL:" in text and "[build failed]" not in text)
                status = "passed" if code == 0 else "detected" if assertion_failed else "invalid-run"
            except subprocess.TimeoutExpired:
                # Cargo and go test can have compiler/test children. Stop the
                # invocation's process tree, not just its immediate launcher.
                if os.name == "nt":
                    subprocess.run(["taskkill", "/PID", str(child.pid), "/T", "/F"],
                                   stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL,
                                   timeout=30, check=False)
                    child.kill()
                else:
                    os.killpg(child.pid, signal.SIGKILL)
                text, _ = child.communicate(timeout=30)
                code, status = child.returncode, "timed-out"
        log = output / (name + ".log")
        log.write_text(text)
        row = {"name": name, "language": language, "status": status, "exit_code": code, "seconds": round(time.monotonic() - started, 3), "log": log.name}
        if mutant is not None:
            row["mutated_source_sha256"] = sha(mutant)
        receipt["results"].append(row)
        save()
        print(f"{name}: {status}", flush=True)
        return status

    rust_command = [args.cargo, "test", "--manifest-path", str(isolated / "Cargo.toml"), "--locked", "--offline", "--bin", "dircue-structural-worker", "hotspots::tests::generated", "--", "--nocapture"]
    go_args = ["-mod=readonly", "./pkg/structure", "-run", "TestHotspotGenerated|TestHotspotCounterOverflow", "-count=1"]
    for language, source_path, mutations in [("rust", RUST_PATH, RUST_MUTATIONS), ("go", GO_PATH, GO_MUTATIONS)]:
        source = original[source_path].decode()
        command = rust_command if language == "rust" else ["go", "test", *go_args]
        if execute(language + "-baseline", language, command) != "passed":
            receipt["status"] = "baseline-failed"
            save()
            raise SystemExit("baseline did not pass; no mutant result is meaningful")
        for name, old, new in mutations:
            if old not in source:
                raise SystemExit(f"mutation anchor missing: {name}")
            mutant = source.replace(old, new).encode()
            if language == "rust":
                (isolated / "src/hotspots.rs").write_bytes(mutant)
                trial = rust_command
            else:
                mutated = output / "hotspots-mutant.go"
                mutated.write_bytes(mutant)
                overlay = output / "overlay.json"
                overlay.write_text(json.dumps({"Replace": {str(ROOT / GO_PATH): str(mutated)}}))
                trial = ["go", "test", "-overlay", str(overlay), *go_args]
            execute(name, language, trial, mutant)
        if language == "rust":
            (isolated / "src/hotspots.rs").write_bytes(original[RUST_PATH])
        execute(language + "-restored", language, command)
    try:
        final_go = go_inputs(env)
        final_rust = {p: sha(data) for p, data in rust_inputs(worker).items()}
        binding = {
            "go_input_membership_and_hashes_unchanged": initial_go == final_go,
            "rust_input_membership_and_hashes_unchanged": initial_rust == final_rust,
            "harness_unchanged": sha(harness.read_bytes()) == harness_hash,
        }
        binding["isolated_worker_restored"] = all(
            sha((isolated / (ROOT / name).relative_to(worker)).read_bytes()) == expected
            for name, expected in initial_rust.items()
        )
    except (OSError, subprocess.SubprocessError, ValueError):
        binding = {"input_recheck_failed": True}
        receipt["source_binding"] = binding
        receipt["status"] = "source-binding-failed"
        save()
        raise SystemExit("could not recheck source binding; see receipt")
    unchanged = all(binding.values())
    receipt["source_binding"] = binding
    receipt["repository_runtime_unchanged"] = unchanged
    results = receipt["results"]
    mutants = [r for r in results if not r["name"].endswith(("-baseline", "-restored"))]
    controls = [r for r in results if r not in mutants]
    receipt["mutants_detected"] = sum(r["status"] == "detected" for r in mutants)
    receipt["mutants_total"] = len(mutants)
    passed = unchanged and all(r["status"] == "detected" for r in mutants) and all(r["status"] == "passed" for r in controls)
    receipt["status"] = "passed" if passed else "failed"
    save()
    raise SystemExit(0 if passed else 1)


if __name__ == "__main__":
    main()
