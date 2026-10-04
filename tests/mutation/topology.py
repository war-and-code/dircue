#!/usr/bin/env python3
"""Run focused production mutations for Procfile, Gradle, and Aspire topology.

Each operator substitutes one exact source fragment through Go's -overlay
feature. The checkout is never edited. A mutant is killed only when its named
Go test reaches an assertion failure; compile failures and infrastructure
errors stay distinct and fail the campaign.
"""
from __future__ import annotations

import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import subprocess
import sys
import tempfile
import time

ROOT = Path(__file__).resolve().parents[2]
BASELINE_PACKAGES = [
    "./pkg/deployables",
    "./pkg/mapbuild",
    "./pkg/componentmap",
    "./pkg/projects",
    "./pkg/declarations",
]


def mutation(name: str, source: str, old: str, new: str, package: str, test: str) -> dict[str, str]:
    return {"name": name, "source": source, "old": old, "new": new,
            "package": package, "test": test}


MUTATIONS = [
    mutation(
        "Unicode whitespace invents shell tokens",
        "pkg/deployables/procfile.go",
        "strings.FieldsFunc(command, func(r rune) bool { return r == ' ' || r == '\\t' })",
        "strings.Fields(command)",
        "./pkg/mapbuild", "TestProcfileUnicodeWhitespaceCannotInventShellTokensOrRunEdges",
    ),
    mutation(
        "valid selected Procfile target is dropped",
        "pkg/deployables/procfile.go",
        "if inventory.paths[ref.Value] {\n\t\t\t\t\tmatches = append(matches, ref.Value)",
        "if false {\n\t\t\t\t\tmatches = append(matches, ref.Value)",
        "./pkg/deployables", "TestResolveProcfileTargetsRequiresUniqueSelectedFile",
    ),
    mutation(
        "ambiguous Python target chooses one file",
        "pkg/deployables/procfile.go",
        "if len(matches) != 1 {",
        "if len(matches) == 0 {",
        "./pkg/deployables", "TestResolveProcfileTargetsRequiresUniqueSelectedFile",
    ),
    mutation(
        "attached Gunicorn config and chdir options are ignored",
        "pkg/deployables/procfile.go",
        'if executable == "gunicorn" && (strings.HasPrefix(argument, "-C") || strings.HasPrefix(argument, "-c")) {',
        "if false {",
        "./pkg/mapbuild", "TestProcfileGunicornAttachedOptionsCannotCreateRunEdges",
    ),
    mutation(
        "ambiguous nearest Procfile owner picks first",
        "pkg/mapbuild/procfile_edges.go",
        'if len(components) > 1 {\n\t\t\treturn mapdoc.Node{}, "ambiguous_procfile_component_owner"\n\t\t}',
        'if len(components) > 1 {\n\t\t\tcomponents = components[:1]\n\t\t}',
        "./pkg/mapbuild", "TestProcfileDoesNotChooseAmbiguousOrNameOnlyComponentOwner",
    ),
    mutation(
        "nearest incompatible Procfile owner is accepted",
        "pkg/mapbuild/procfile_edges.go",
        'if !compatible {\n\t\t\t\treturn mapdoc.Node{}, "procfile_component_owner_mismatch"\n\t\t\t}',
        'if !compatible && false {\n\t\t\t\treturn mapdoc.Node{}, "procfile_component_owner_mismatch"\n\t\t\t}',
        "./pkg/mapbuild", "TestProcfileDoesNotChooseAmbiguousOrNameOnlyComponentOwner",
    ),
    mutation(
        "duplicate Gradle settings roots create membership claims",
        "pkg/componentmap/build.go",
        "if settingsByRoot[root] != 1 {",
        "if false {",
        "./pkg/componentmap", "TestGradleSettingsAmbiguityAndUnresolvedTargetsStayQualified",
    ),
    mutation(
        "missing Gradle target is treated as a retained component",
        "pkg/componentmap/build.go",
        'if ref.TargetStatus != "present" {',
        "if false {",
        "./pkg/componentmap", "TestGradleSettingsAmbiguityAndUnresolvedTargetsStayQualified",
    ),
    mutation(
        "Gradle membership ignores evaluation qualification",
        "pkg/componentmap/build.go",
        'if ref.State == "conditional" || ref.State == "unresolved" || ref.Condition != "" {\n\t\t\t\trelationship.Coverage = "partial"\n\t\t\t}',
        'if false {\n\t\t\t\trelationship.Coverage = "partial"\n\t\t\t}',
        "./pkg/componentmap", "TestActualGradleSettingsBecomeConditionalMapMembership",
    ),
    mutation(
        "Gradle settings joins by parent directory instead of exact target root",
        "pkg/componentmap/build.go",
        "targets := gradleByRoot[cleanRoot(ref.Target)]",
        "targets := gradleByRoot[path.Dir(cleanRoot(ref.Target))]",
        "./pkg/componentmap", "TestActualGradleSettingsBecomeConditionalMapMembership",
    ),
    mutation(
        "Aspire builder passed by ref or out remains trusted",
        "pkg/deployables/aspire.go",
        'if i > 0 && (s[i-1].text == "ref" || s[i-1].text == "out") {\n\t\t\treturn true\n\t\t}',
        'if false && i > 0 && (s[i-1].text == "ref" || s[i-1].text == "out") {\n\t\t\treturn true\n\t\t}',
        "./pkg/mapbuild", "TestAspireCLIObservationDoesNotLinkAfterBuilderPassedByReference",
    ),
    mutation(
        "malformed Aspire compound assignment indexes an empty token",
        "pkg/deployables/aspire.go",
        'if s[i+1].text == "" || strings.ContainsRune("+-?&|^/%*", rune(s[i+1].text[0])) {',
        'if strings.ContainsRune("+-?&|^/%*", rune(s[i+1].text[0])) {',
        "./pkg/deployables", "TestAspireMalformedStatementDoesNotPanic",
    ),
    mutation(
        "selected CSharp global alias is ignored",
        "pkg/deployables/aspire.go",
        "func hasGlobalAspireAlias(tokens []csToken) bool {\n",
        "func hasGlobalAspireAlias(tokens []csToken) bool {\n\tif len(tokens) > 0 { return false }\n",
        "./pkg/deployables", "TestCollectorAspireGlobalAliasGuardUsesSelectedProjectScope",
    ),
    mutation(
        "SDK generated global alias is ignored",
        "pkg/deployables/aspire.go",
        "func projectUsingAlias(data []byte) (bool, error) {\n",
        "func projectUsingAlias(data []byte) (bool, error) {\n\tif len(data) > 0 { return false, nil }\n",
        "./pkg/deployables", "TestAppHostProjectUsingAliasesAreScopedAndCommentSafe",
    ),
]

STATUSES = {"killed", "survived", "invalid", "timeout", "error"}
BUILD_FAILURE_MARKERS = (
    "[build failed]", "build failed", "syntax error:", "undefined:",
    "cannot use ", "no required module provides package", "cannot find module",
)
PROCESS_FAILURE = re.compile(r"^(?:panic:|fatal error:|runtime error:|signal: )", re.MULTILINE)


def sha256_bytes(value: bytes) -> str:
    return hashlib.sha256(value).hexdigest()


def execute(argv: list[str], cwd: Path, timeout_seconds: float) -> dict:
    start = time.monotonic()
    try:
        completed = subprocess.run(
            argv, cwd=cwd, text=True, stdout=subprocess.PIPE,
            stderr=subprocess.PIPE, timeout=timeout_seconds, check=False,
        )
        return {
            "argv": argv,
            "returncode": completed.returncode,
            "stdout": completed.stdout,
            "stderr": completed.stderr,
            "duration_seconds": round(time.monotonic() - start, 3),
            "timed_out": False,
        }
    except subprocess.TimeoutExpired as exc:
        return {
            "argv": argv,
            "returncode": None,
            "stdout": decode_output(exc.stdout),
            "stderr": decode_output(exc.stderr),
            "duration_seconds": round(time.monotonic() - start, 3),
            "timed_out": True,
        }


def decode_output(value: str | bytes | None) -> str:
    if value is None:
        return ""
    return value.decode(errors="replace") if isinstance(value, bytes) else value


def assertion_failure(test_name: str, result: dict) -> bool:
    """Return true only for the named Go test's ordinary assertion failure."""
    if result.get("returncode") in (None, 0) or result.get("timed_out"):
        return False
    output = (result.get("stdout", "") + result.get("stderr", "")).lower()
    if any(marker in output for marker in BUILD_FAILURE_MARKERS):
        return False
    if PROCESS_FAILURE.search(result.get("stdout", "") + result.get("stderr", "")):
        return False
    marker = re.compile(rf"^--- FAIL: {re.escape(test_name)}(?:/|\s|$)", re.MULTILINE)
    return marker.search(result.get("stdout", "") + result.get("stderr", "")) is not None


def classify_mutant(compile_result: dict, test_result: dict | None, test_name: str) -> tuple[str, str]:
    if compile_result.get("timed_out"):
        return "timeout", "overlay compile validation timed out"
    if compile_result.get("returncode") != 0:
        return "invalid", "overlay did not compile; compile failures never count as kills"
    if test_result is None:
        return "error", "selected test was not run"
    if test_result.get("timed_out"):
        return "timeout", "selected test timed out"
    if test_result.get("returncode") == 0:
        return "survived", "selected test passed with the mutant"
    if assertion_failure(test_name, test_result):
        return "killed", "named test failed through its assertion oracle"
    return "error", "non-assertion test failure or infrastructure error"


def validate_catalog(root: Path = ROOT) -> list[dict]:
    names: set[str] = set()
    for item in MUTATIONS:
        if item["name"] in names:
            raise ValueError(f"duplicate mutation name: {item['name']}")
        names.add(item["name"])
        source = root / item["source"]
        if not source.is_file():
            raise ValueError(f"mutation source missing: {item['source']}")
        content = source.read_text(encoding="utf-8")
        count = content.count(item["old"])
        if count != 1:
            raise ValueError(f"{item['name']}: expected one exact source anchor, found {count}")
        if item["old"] == item["new"]:
            raise ValueError(f"{item['name']}: mutation is a no-op")
        test_path = selected_test_source(item, root)
        test_file = root / test_path
        if not test_file.is_file():
            raise ValueError(f"{item['name']}: selected test source missing: {test_path}")
        test_content = test_file.read_text(encoding="utf-8")
        if not re.search(rf"^func {re.escape(item['test'])}\(t \*testing\.T\)", test_content, re.MULTILINE):
            raise ValueError(f"{item['name']}: selected Go test missing from {test_path}")
    return MUTATIONS


def selected_test_source(item: dict[str, str], root: Path = ROOT) -> str:
    pattern = re.compile(rf"^func {re.escape(item['test'])}\(t \*testing\.T\)", re.MULTILINE)
    directory = root / item["package"].removeprefix("./")
    matches = [path for path in sorted(directory.glob("*_test.go"))
               if pattern.search(path.read_text(encoding="utf-8"))]
    if len(matches) != 1:
        raise ValueError(f"{item['name']}: expected one named test source, found {len(matches)}")
    return matches[0].relative_to(root).as_posix()


def make_output(path: str | None, commit: str) -> Path:
    if path:
        target = Path(path).expanduser().resolve()
        target.parent.mkdir(parents=True, exist_ok=True)
        fd = os.open(target, os.O_CREAT | os.O_EXCL | os.O_WRONLY, 0o600)
        os.close(fd)
        return target
    output_dir = ROOT / ".cache" / "review130" / "mutation"
    output_dir.mkdir(parents=True, exist_ok=True)
    fd, raw_path = tempfile.mkstemp(prefix=f"topology-{commit[:12]}-", suffix=".json", dir=output_dir)
    os.close(fd)
    return Path(raw_path)


def run_campaign(timeout_seconds: float = 180, output: str | None = None) -> tuple[int, Path, dict]:
    mutations = validate_catalog()
    commit = subprocess.check_output(["git", "rev-parse", "HEAD"], cwd=ROOT, text=True).strip()
    output_path = make_output(output, commit)
    dirty = execute(["git", "status", "--short"], ROOT, 10)
    go_version = execute(["go", "version"], ROOT, 10)
    receipt = {
        "schema": "dircue-topology-mutation-campaign-1",
        "source_commit": commit,
        "working_tree_status": dirty.get("stdout", ""),
        "toolchain": {"go": go_version, "python": sys.version},
        "baseline": {},
        "mutations": [],
        "summary": {},
    }

    baseline_argv = ["go", "test", *BASELINE_PACKAGES, "-count=1"]
    baseline = execute(baseline_argv, ROOT, timeout_seconds)
    receipt["baseline"] = baseline
    if baseline["timed_out"]:
        receipt["summary"] = {"baseline_passed": False, "total": len(mutations), "killed": 0,
                               "survived": 0, "invalid": 0, "timeout": 1, "error": 0}
        output_path.write_text(json.dumps(receipt, indent=2) + "\n", encoding="utf-8")
        return 1, output_path, receipt
    if baseline["returncode"] != 0:
        receipt["summary"] = {"baseline_passed": False, "total": len(mutations), "killed": 0,
                               "survived": 0, "invalid": 0, "timeout": 0, "error": 1}
        output_path.write_text(json.dumps(receipt, indent=2) + "\n", encoding="utf-8")
        return 1, output_path, receipt

    with tempfile.TemporaryDirectory(prefix="dircue-topology-mutations-") as temp_dir:
        temp = Path(temp_dir)
        for index, item in enumerate(mutations):
            source_path = ROOT / item["source"]
            original = source_path.read_text(encoding="utf-8")
            mutated = original.replace(item["old"], item["new"], 1)
            mutant_path = temp / f"mutant-{index:02d}.go"
            mutant_path.write_text(mutated, encoding="utf-8")
            overlay_path = temp / f"overlay-{index:02d}.json"
            overlay_path.write_text(json.dumps({"Replace": {str(source_path): str(mutant_path)}}), encoding="utf-8")
            compile_argv = ["go", "test", f"-overlay={overlay_path}", item["package"], "-run", "^$", "-count=1"]
            test_argv = ["go", "test", f"-overlay={overlay_path}", item["package"], "-run", f"^{item['test']}$", "-count=1"]
            compile_result = execute(compile_argv, ROOT, timeout_seconds)
            test_result = None
            if compile_result["returncode"] == 0 and not compile_result["timed_out"]:
                test_result = execute(test_argv, ROOT, timeout_seconds)
            status, reason = classify_mutant(compile_result, test_result, item["test"])
            record = {
                "name": item["name"],
                "source": item["source"],
                "source_sha256": sha256_bytes(original.encode()),
                "mutated_source_sha256": sha256_bytes(mutated.encode()),
                "test_source": selected_test_source(item),
                "test_source_sha256": sha256_bytes((ROOT / selected_test_source(item)).read_bytes()),
                "selected_test": item["test"],
                "compile_validation": compile_result,
                "test_result": test_result,
                "status": status,
                "reason": reason,
            }
            if status not in STATUSES:
                raise AssertionError(f"unrecognized status {status}")
            receipt["mutations"].append(record)

    statuses = [record["status"] for record in receipt["mutations"]]
    summary = {status: statuses.count(status) for status in sorted(STATUSES)}
    receipt["summary"] = {"baseline_passed": True, "total": len(mutations), **summary}
    output_path.write_text(json.dumps(receipt, indent=2) + "\n", encoding="utf-8")
    return (0 if all(status == "killed" for status in statuses) else 1), output_path, receipt


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--output", help="new JSON receipt path; existing files are never overwritten")
    parser.add_argument("--timeout", type=float, default=180, help="seconds per Go command (default: 180)")
    args = parser.parse_args()
    if args.timeout <= 0:
        parser.error("--timeout must be positive")
    try:
        code, path, receipt = run_campaign(args.timeout, args.output)
    except (OSError, ValueError, subprocess.CalledProcessError) as exc:
        print(f"topology mutation campaign setup failed: {exc}", file=sys.stderr)
        return 1
    print(f"receipt: {path}")
    print(json.dumps(receipt.get("summary", {}), sort_keys=True))
    return code


if __name__ == "__main__":
    raise SystemExit(main())
