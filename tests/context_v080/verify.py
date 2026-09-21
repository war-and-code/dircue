#!/usr/bin/env python3
"""Verify 0.8 receipt integrity and current source bindings; does not replay scans."""

from __future__ import annotations

import argparse
from collections import Counter
import hashlib
import json
from pathlib import Path
import subprocess
import tempfile
from typing import Any

import build
import common


def require(condition: bool, message: str) -> None:
    if not condition:
        raise AssertionError(message)


def hex_text(value: str, length: int = 64) -> bool:
    return isinstance(value, str) and len(value) == length and all(c in "0123456789abcdef" for c in value)


def verify_build(receipt_path: Path, candidate: Path, require_current: bool = True, strict: bool = False) -> dict[str, Any]:
    receipt = common.read_json(receipt_path)
    require(receipt.get("schema") == "dircue-context-v080-build-1", "wrong build schema")
    require(candidate.is_file() and common.sha256(candidate) == receipt.get("candidate_sha256"), "candidate hash mismatch")
    files = receipt.get("files")
    require(isinstance(files, dict) and files, "missing build input manifest")
    for name, expected in files.items():
        require(isinstance(name, str) and hex_text(expected), "invalid build input row")
        source = common.ROOT / name
        require(source.is_file() and common.sha256(source) == expected, f"build input changed: {name}")
    source = receipt.get("source_at_build")
    require(isinstance(source, dict) and set(source) == {"commit", "head_tree", "status_sha256", "diff_sha256", "dirty"}, "invalid source identity")
    require(hex_text(source["commit"], 40) and hex_text(source["head_tree"], 40), "invalid Git source identity")
    require(hex_text(source["status_sha256"]) and hex_text(source["diff_sha256"]), "invalid source-state digest")
    require(type(source["dirty"]) is bool, "invalid dirty marker")
    if strict:
        require(source["dirty"] is False, "strict verification requires a clean build")
        resolved = subprocess.check_output(["git", "rev-parse", "--verify", source["commit"] + "^{commit}"], cwd=common.ROOT).decode().strip()
        tree = subprocess.check_output(["git", "rev-parse", "--verify", source["commit"] + "^{tree}"], cwd=common.ROOT).decode().strip()
        require(resolved == source["commit"] and tree == source["head_tree"], "recorded commit/tree mismatch")
        empty = hashlib.sha256(b"").hexdigest()
        require(source["status_sha256"] == empty and source["diff_sha256"] == empty, "clean build has nonempty source-state digests")
    if require_current:
        env = {**__import__("os").environ, "CGO_ENABLED": "0", "GOWORK": "off", "GOFLAGS": ""}
        require(files == build.build_inputs(env), "current Go compilation inputs differ from receipt")
    return receipt


def verify_baseline(baseline: Path) -> None:
    require(baseline.is_file() and common.sha256(baseline) == common.BASELINE_SHA256, "baseline is not pinned 0.7.0")
    version = subprocess.check_output([str(baseline), "--version"], env={"PATH":"/usr/bin:/bin", "LC_ALL":"C"}).decode().strip()
    require(version == "dircue 0.7.0", "baseline version output mismatch")


def verify_raw(record: dict[str, Any]) -> tuple[int, bytes, bytes]:
    require(set(record).issuperset({"exit", "stdout_sha256", "stderr_sha256"}), "raw record incomplete")
    return common.decode(record)


def verify_compatibility(receipt_path: Path, schema: str, total: int, baseline: Path,
                         candidate: Path, build_receipt: dict[str, Any], build_path: Path) -> dict[str, Any]:
    receipt = common.read_json(receipt_path)
    require(receipt.get("schema") == schema and receipt.get("passed") is True, "compatibility receipt did not pass")
    require(receipt.get("total") == total == receipt.get("exact_matches"), "compatibility totals are inconsistent")
    require(receipt.get("baseline_release") == "v0.7.0" and receipt.get("baseline_sha256") == common.sha256(baseline), "baseline identity mismatch")
    require(receipt.get("candidate_sha256") == common.sha256(candidate), "candidate identity mismatch")
    require(receipt.get("build_receipt") == build_receipt and receipt.get("build_receipt_sha256") == common.sha256(build_path), "embedded build receipt mismatch")
    cases = receipt.get("cases")
    require(isinstance(cases, list) and len(cases) == total, "case list is incomplete")
    ids = [row.get("id") for row in cases]
    require(len(set(ids)) == total and all(isinstance(v, str) and v for v in ids), "case IDs are invalid")
    for row in cases:
        old, new = verify_raw(row["baseline"]), verify_raw(row["candidate"])
        require(row.get("equal") is True and old == new, f"raw mismatch in {row['id']}")
    if schema == "dircue-context-v080-targeted-1":
        invalid = [row for row in cases if "absent.csproj" in row.get("args", [])]
        require(len(invalid) == 2 and all(row["baseline"]["exit"] != 0 and row["candidate"]["exit"] != 0 for row in invalid), "targeted negative oracle missing")
    return receipt


def verify_targeted_sources(receipt: dict[str, Any]) -> None:
    source = common.ROOT / "tests/focus_v070/fixture.py"
    require(receipt.get("fixture_source_sha256") == common.sha256(source), "targeted fixture source changed")
    driver = Path(__file__).with_name("targeted.py")
    require(receipt.get("harness_sha256") == common.sha256(driver), "targeted driver changed")
    import importlib.util
    spec = importlib.util.spec_from_file_location("context_v080_verify_fixture", source)
    module = importlib.util.module_from_spec(spec); assert spec.loader is not None; spec.loader.exec_module(module)
    with tempfile.TemporaryDirectory(prefix="dircue-v080-verify-fixture-") as temporary:
        base = Path(temporary); module.prepare(base)
        require(module.manifest(base) == receipt.get("fixture_manifest"), "targeted fixture manifest mismatch")


def verify_performance(path: Path, baseline: Path, candidate: Path, build_receipt: dict[str, Any], build_path: Path) -> dict[str, Any]:
    receipt = common.read_json(path)
    require(receipt.get("schema") == "dircue-context-v080-performance-1" and receipt.get("passed") is True, "performance receipt did not pass")
    require(receipt.get("baseline_sha256") == common.sha256(baseline) and receipt.get("candidate_sha256") == common.sha256(candidate), "performance executable mismatch")
    require(receipt.get("build_receipt") == build_receipt and receipt.get("build_receipt_sha256") == common.sha256(build_path), "performance build binding mismatch")
    require(receipt.get("driver_sha256") == common.sha256(Path(__file__).with_name("performance.py")), "performance driver changed")
    require(receipt.get("shared_helper_sha256") == common.sha256(Path(__file__).with_name("common.py")), "performance helper changed")
    method, summaries = receipt.get("method"), receipt.get("summaries")
    require(isinstance(method, dict) and method.get("repetitions", 0) >= 5 and method.get("warmups", 0) >= 1, "performance sampling is insufficient")
    require(isinstance(summaries, dict) and summaries, "performance summaries missing")
    for name, summary in summaries.items():
        samples = summary.get("samples")
        require(isinstance(samples, list) and len(samples) == method["repetitions"], f"sample count mismatch: {name}")
        expected = common.sample_summary(samples)
        require(summary == expected, f"summary is not reproducible: {name}")
        output_digest = receipt.get("expected_output_sha256", {}).get(name)
        if output_digest is not None:
            require(hex_text(output_digest) and all(row.get("stdout_sha256") == output_digest for row in samples), f"measured output changed: {name}")
    claim = method.get("claim_limit", "")
    require("different questions" in claim and "not equivalent-output savings" in claim, "performance claim limit missing")
    require(receipt.get("identical_output", {}).get("corpus_languages") is True and receipt.get("identical_output", {}).get("xml_languages") is True, "inherited performance outputs differ")
    comparisons = receipt.get("comparisons", {})
    formulas = {"small_staged_vs_full_wall_ratio":("small_staged","small_full"), "xml_staged_vs_full_wall_ratio":("xml_staged","xml_full"), "small_environments_vs_declarations_wall_ratio":("candidate_small_environments","candidate_small_declarations")}
    for key,(left,right) in formulas.items():
        if key in comparisons:
            require(comparisons[key] == summaries[left]["median_seconds"] / summaries[right]["median_seconds"], f"comparison ratio mismatch: {key}")
    return receipt


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--candidate", required=True, type=Path); parser.add_argument("--baseline", required=True, type=Path)
    parser.add_argument("--build-receipt", required=True, type=Path); parser.add_argument("--broad", required=True, type=Path)
    parser.add_argument("--targeted", required=True, type=Path); parser.add_argument("--performance", required=True, type=Path)
    parser.add_argument("--output", type=Path)
    parser.add_argument("--strict", action="store_true", help="Require a clean build and locally available matching commit/tree")
    args = parser.parse_args(); candidate=args.candidate.resolve(); baseline=args.baseline.resolve(); build_path=args.build_receipt.resolve()
    verify_baseline(baseline); built=verify_build(build_path,candidate,strict=args.strict)
    require(subprocess.check_output([str(candidate),"--version"],env={"PATH":"/usr/bin:/bin","LC_ALL":"C"}).decode().strip()=="dircue 0.8.0-rc.1","candidate is not the 0.8.0 release candidate")
    broad=verify_compatibility(args.broad,"dircue-context-v080-broad-compatibility-1",278,baseline,candidate,built,build_path)
    targeted=verify_compatibility(args.targeted,"dircue-context-v080-targeted-1",18,baseline,candidate,built,build_path); verify_targeted_sources(targeted)
    performance=verify_performance(args.performance,baseline,candidate,built,build_path)
    result={"schema":"dircue-context-v080-verification-1","passed":True,"candidate_sha256":common.sha256(candidate),"baseline_sha256":common.sha256(baseline),"receipts":{name:common.sha256(path) for name,path in {"build":build_path,"broad":args.broad,"targeted":args.targeted,"performance":args.performance}.items()},"case_counts":{"broad":broad["total"],"targeted":targeted["total"]},"performance_lanes":sorted(performance["summaries"])}
    if args.output:
        common.write_json(args.output,result)
        coverage=args.output.with_name("COVERAGE-results.md")
        coverage.write_text("# 0.8 context evidence results\n\nVerified against source-bound receipt files.\n\n| Evidence | Result |\n|---|---:|\n| Broad raw compatibility | 278 / 278 exact |\n| Targeted raw compatibility | 18 / 18 exact |\n| Performance lanes | %d verified |\n| Candidate | `%s` |\n| Baseline 0.7.0 | `%s` |\n" % (len(performance["summaries"]),result["candidate_sha256"],result["baseline_sha256"]))
    print(json.dumps(result,sort_keys=True))


if __name__ == "__main__": main()
