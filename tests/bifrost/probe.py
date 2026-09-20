#!/usr/bin/env python3
"""Probe an explicitly supplied Bifrost binary using disposable source fixtures."""

import argparse
import hashlib
import json
import os
from pathlib import Path
import subprocess
import tempfile
import time


def sha256(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--binary", type=Path, required=True)
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument("--docker-image", help="Existing Linux image; never pulled")
    args = parser.parse_args()
    binary = args.binary.resolve(strict=True)
    output = args.output.resolve()
    output.mkdir(parents=True, exist_ok=True)
    records = []
    responses = {}
    with tempfile.TemporaryDirectory(prefix="dircue-bifrost-") as temporary:
        base = Path(temporary)
        base.chmod(0o755)
        root = base / "workspace"
        root.mkdir()
        cache = base / "cache"
        cache.mkdir(mode=0o777)
        cache.chmod(0o777)
        files = {
            "Service.java": "class Service { static int clean(int x) { return x + 1; } static int handle(int x) { return clean(x); } }\n",
            "Service.cs": "class Service { static int Clean(int x) { return x + 1; } static int Handle(int x) { return Clean(x); } }\n",
            "service.js": "function clean(x) { return x + 1; } function handle(x) { return clean(x); } function dynamicCall(target, x) { return target(x); }\n",
            "Ignored.java": "class IgnoredCanary { static int value() { return 9; } }\n",
            ".bifrostignore": "Ignored.java\n",
        }
        for path, content in files.items():
            (root / path).write_text(content)
        outside = base / "Outside.java"
        outside.write_text("class ExternalCanary { int outside() { return 42; } }\n")
        (root / "Link.java").symlink_to("../Outside.java")
        environment = {key: value for key, value in os.environ.items() if not key.startswith("BIFROST_")}
        environment.update(BIFROST_CACHE_DIR=str(cache), BIFROST_SEMANTIC_PACK_DOWNLOAD="off",
                           BIFROST_WORKSPACE_SEMANTIC_MODELS="off", BIFROST_CACHE_GC="off",
                           RAYON_NUM_THREADS="1")
        prefix = [str(binary)]
        target_root = str(root)
        image_id = None
        if args.docker_image:
            image_id = subprocess.check_output(["docker", "image", "inspect", args.docker_image,
                                               "--format", "{{.Id}}"], text=True).strip()
            target_root = "/probe/workspace"
            prefix = ["docker", "run", "--rm", "--pull=never", "--network=none", "--read-only",
                      "--user=65534:65534", "--cpus=1", "--memory=1g", "--memory-swap=1g",
                      "--pids-limit=128", "--cap-drop=ALL", "--security-opt=no-new-privileges",
                      "--tmpfs", "/tmp:rw,nosuid,nodev,size=32m",
                      "--mount", f"type=bind,source={binary},target=/bifrost,readonly",
                      "--mount", f"type=bind,source={base},target=/probe,readonly",
                      "--mount", f"type=bind,source={cache},target=/cache",
                      "--env", "BIFROST_CACHE_DIR=/cache", "--env", "BIFROST_SEMANTIC_PACK_DOWNLOAD=off",
                      "--env", "BIFROST_WORKSPACE_SEMANTIC_MODELS=off", "--env", "BIFROST_CACHE_GC=off",
                      "--env", "RAYON_NUM_THREADS=1", image_id, "/bifrost"]

        def run(name, extra):
            command = prefix + ["--root", target_root] + extra
            started = time.monotonic()
            completed = subprocess.run(command, env=environment, capture_output=True, timeout=60)
            stdout = completed.stdout.decode("utf-8", errors="replace")
            stderr = completed.stderr.decode("utf-8", errors="replace")
            (output / f"{name}.stdout").write_text(stdout)
            (output / f"{name}.stderr").write_text(stderr)
            try:
                data = json.loads(stdout)
            except json.JSONDecodeError:
                data = None
            records.append({"name": name, "arguments": extra, "exit_code": completed.returncode,
                            "seconds": round(time.monotonic() - started, 6),
                            "stdout_sha256": hashlib.sha256(completed.stdout).hexdigest(),
                            "stderr_sha256": hashlib.sha256(completed.stderr).hexdigest()})
            responses[name] = data
            return completed.returncode, data

        def query(name, expression, sources=()):
            extra = ["--tool", "query_code", "--args", json.dumps(expression)]
            for source in sources:
                extra += ["--sources", source]
            return run(name, extra)

        run("version", ["--version"])
        run("identity", ["--build-identity"])
        for language, method, expected in [("java", "handle", "clean"), ("csharp", "Handle", "Clean"),
                                            ("javascript", "handle", "clean")]:
            code, data = query(language + "-callees", {"schema_version": "1", "languages": [language],
                               "match": {"kind": "function" if language == "javascript" else "method", "name": method},
                               "steps": [{"op": "enclosing_decl"}, {"op": "callees"}]})
            names = [row.get("fq_name", "") for row in (data or {}).get("structuredContent", {}).get("results", [])]
            records[-1]["expected_callee_found"] = code == 0 and any(name.split(".")[-1] == expected for name in names)
        query("dynamic-dispatch", {"languages": ["javascript"], "match": {"kind": "call", "callee": {"name": "target"}},
                                   "steps": [{"op": "dispatch_outcome"}]}, ["service.js"])
        query("unsupported-kind", {"languages": ["java"], "match": {"kind": "function"}}, ["Service.java"])
        ignored = {"languages": ["java"], "match": {"kind": "class", "name": "IgnoredCanary"}}
        query("ignored-default", ignored)
        query("ignored-explicit", ignored, ["Ignored.java"])
        external = {"languages": ["java"], "match": {"kind": "class", "name": "ExternalCanary"}}
        query("symlink-relative", external, ["Link.java"])
        query("symlink-absolute", external, [target_root + "/Link.java"])
        (root / "Service.java").write_text(files["Service.java"].replace("clean", "fresh"))
        query("changed-bytes", {"languages": ["java"], "match": {"kind": "method", "name": "handle"},
                                "steps": [{"op": "enclosing_decl"}, {"op": "callees"}]}, ["Service.java"])
        run("query-file-with-sources", ["--query-file", "query.rql", "--sources", "Service.java"])
        cache_files = sorted(str(path.relative_to(cache)) for path in cache.rglob("*") if path.is_file())
        source_unchanged = all((root / path).read_text() == content for path, content in files.items() if path != "Service.java")
        def result(name):
            return (responses.get(name) or {}).get("structuredContent", {})

        expected_failures = {"symlink-absolute", "query-file-with-sources"}
        dynamic = result("dynamic-dispatch").get("results", [])
        checks = {
            "expected_exit_codes": all(record["exit_code"] == (1 if record["name"] in expected_failures else 0) for record in records),
            "three_declared_callees": all(record.get("expected_callee_found", True) for record in records),
            "dynamic_dispatch_stays_open": bool(dynamic) and all(row.get("outcome") == "unproven" and row.get("coverage") == "open" for row in dynamic),
            "unsupported_kind_reports_incomplete": any(item.get("impact") == "incomplete" for item in result("unsupported-kind").get("diagnostics", [])),
            "ignore_applies_by_default": result("ignored-default").get("results") == [],
            "explicit_source_overrides_ignore": bool(result("ignored-explicit").get("results")),
            "relative_source_follows_external_symlink": bool(result("symlink-relative").get("results")),
            "cache_refreshes_changed_source": [row.get("fq_name") for row in result("changed-bytes").get("results", [])] == ["Service.fresh"],
            "source_preserved": source_unchanged,
        }
        receipt = {"binary_sha256": sha256(binary), "binary_name": binary.name,
                   "docker_image": image_id, "network_disabled": bool(args.docker_image),
                   "read_only_source_mount": bool(args.docker_image), "source_other_than_intended_edit_unchanged": source_unchanged,
                   "fixture_sha256": {path: hashlib.sha256(content.encode()).hexdigest() for path, content in files.items()},
                   "environment": {key: value for key, value in environment.items() if key.startswith("BIFROST_") or key == "RAYON_NUM_THREADS"},
                   "cache_files": cache_files, "checks": checks, "runs": records}
        receipt["environment"]["BIFROST_CACHE_DIR"] = "/cache" if args.docker_image else "<temporary-private-cache>"
        (output / "receipt.json").write_text(json.dumps(receipt, indent=2) + "\n")
        print(json.dumps({"receipt": str(output / "receipt.json"), "runs": len(records), "checks": checks}))
        if not all(checks.values()):
            raise SystemExit(1)


if __name__ == "__main__":
    main()
