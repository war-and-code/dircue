#!/usr/bin/env python3
"""Preserve 0.7/0.8 focused, environment and saved-report JSON contracts."""

import argparse
import importlib.util
import json
from pathlib import Path
import sys
import tempfile

ROOT = Path(__file__).resolve().parents[2]
sys.path.insert(0, str(ROOT / "tests/context_v080"))
import common

BASELINE_SHA256 = "429583365b66f767443a7fff9efaa48873a924643dd48bb48c94d3afee5f65bc"
FIXTURE = ROOT / "tests/focus_v070/fixture.py"
spec = importlib.util.spec_from_file_location("focus_preservation_fixture", FIXTURE)
fixture = importlib.util.module_from_spec(spec)
spec.loader.exec_module(fixture)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    for name in ("baseline", "candidate", "build-receipt", "output"):
        parser.add_argument("--" + name, required=True, type=Path)
    args = parser.parse_args()
    baseline, candidate = args.baseline.resolve(), args.candidate.resolve()
    if common.sha256(baseline) != BASELINE_SHA256:
        raise AssertionError("expected verified v0.8.0 darwin-arm64 baseline")
    receipt, receipt_hash = common.load_build_receipt(args.build_receipt, candidate)
    rows = []

    def check(label, command, cwd, success=True, partial_focus_fix=None,
              complete_language_fix=False):
        old, new = common.capture(baseline, command, cwd), common.capture(candidate, command, cwd)
        if (old[0] == 0) != success:
            raise AssertionError((label, "invalid baseline scenario", old))
        fixed = False
        if partial_focus_fix is not None:
            if old != (1, b"", b"Error: saved report: invalid dircue profile JSON\n"):
                raise AssertionError((label, "baseline failure changed", old))
            if new[0] == 0 and new[2] == b"":
                explained = json.loads(new[1])
                fixed = explained.get("explanation", {}).get("query") == {
                    "kind": "project", "path": partial_focus_fix}
        language_fixed = False
        if complete_language_fix:
            if old[0] != 0 or old[2] or new[0] != 0 or new[2]:
                raise AssertionError((label, "comparison command failed", old, new))
            old_report, new_report = json.loads(old[1]), json.loads(new[1])
            old_languages = next(m for m in old_report["modules"] if m["name"] == "languages")
            new_languages = next(m for m in new_report["modules"] if m["name"] == "languages")
            incomplete = "incomplete_coverage_limits_absence_claims"
            language_fixed = (
                incomplete in old_languages["reasons"] and
                incomplete not in new_languages["reasons"] and
                new_languages["status"] == "unchanged" and
                new_languages["compatibility"] == "observed_only" and
                new_languages["counts"] == {
                    "added": 0, "removed": 0, "changed": 0,
                    "unchanged": 1, "unavailable": 0, "omitted_changes": 0,
                }
            )
            old_languages["reasons"] = new_languages["reasons"]
            language_fixed = language_fixed and old_report == new_report
        rows.append({"id": label, "args": command, "equal": old == new,
                     "expected_partial_focus_fix": partial_focus_fix is not None,
                     "partial_focus_fix_verified": fixed,
                     "expected_complete_language_fix": complete_language_fix,
                     "complete_language_fix_verified": language_fixed,
                     "baseline": common.recorded(old), "candidate": common.recorded(new)})
        return old

    with tempfile.TemporaryDirectory(prefix="dircue-v100-context-") as temporary:
        base = Path(temporary)
        cases = fixture.prepare(base)
        manifest = fixture.manifest(base)
        for name, case in cases.items():
            root = case["root"]
            tail = ["--source", "directory", "--json", str(root)]
            commands = [
                ["analyze", "focus", "--project", case["project"], *tail],
                ["analyze", "focus", "--project", case["project"], "--metrics", "--files", *tail],
                ["analyze", "focus", "--project", case["project"], "--related-project", case["related"], "--metrics", "--files", *tail],
                ["analyze", "focus", "--affected-by", case["affected_by"], *tail],
                ["analyze", "availability", *tail],
                ["analyze", "all", "--declarations", "--availability", *tail],
                ["analyze", "explain", "--file", case["primary"][1], *tail],
                ["analyze", "focus", "--project", "absent.csproj", *tail],
            ]
            saved = base / (name + "-saved.json")
            for index, command in enumerate(commands):
                old = check(f"focus-{name}-{index+1}", command, root, success=index != 7)
                if index == 1:
                    saved.write_bytes(old[1])
            check(f"focus-{name}-saved-explanation", ["analyze", "explain", "--report", str(saved), "--project", case["project"], "--json"], root,
                  success=False, partial_focus_fix=case["project"])
            saved.unlink()
        if fixture.manifest(base) != manifest:
            raise AssertionError("focused fixtures changed")

        root = base / "context"
        (root / "app").mkdir(parents=True)
        for name, body in {
            "global.json": '{"sdk":{"version":"8.0.100","rollForward":"latestFeature"}}\n',
            "app/App.csproj": '<Project Sdk="Microsoft.NET.Sdk"><PropertyGroup><TargetFramework>net8.0</TargetFramework></PropertyGroup></Project>\n',
            "app/Program.cs": 'class Program { static void Main() {} }\n',
        }.items():
            (root / name).write_text(body)
        tail = ["--source", "directory", "--json", str(root)]
        saved = base / "first-pass.json"
        full = base / "full.json"
        for mode in ("discovery", "declarations", "environments"):
            old = check(mode, ["analyze", mode, *tail], root)
            if mode == "discovery":
                saved.write_bytes(old[1])
        full.write_bytes(check("combined-environments", ["analyze", "all", "--declarations", "--environments", "--availability", "--discovery", *tail], root)[1])
        check("original-capabilities", ["capabilities", "--json"], root)
        for module in ("discovery", "declarations", "environments", "focus", "availability", "formats", "metrics", "structure"):
            for source, label in ((saved, "initial"), (full, "retained")):
                command = ["plan", str(source), "--module", module, "--json"]
                if module == "focus":
                    command += ["--project", "app/App.csproj"]
                if module == "structure":
                    command += ["--input", "structural-worker"]
                check(f"plan-{label}-{module}", command, root)
        check("combined-plan-questions", ["plan", str(full), "--question", "content-formats,code-metrics", "--json"], root)
        check("compare-environment-profile", ["compare", str(full), str(full), "--json"], root,
              complete_language_fix=True)
    result = {"schema": "dircue-v100-context-preservation-1", "baseline_release": "v0.8.0",
              "baseline_sha256": BASELINE_SHA256, "candidate_sha256": common.sha256(candidate),
              "build_receipt": receipt, "build_receipt_sha256": receipt_hash,
              "harness_sha256": common.sha256(Path(__file__)), "fixture_sha256": common.sha256(FIXTURE),
              "cases": rows, "total": len(rows), "exact_matches": sum(r["equal"] for r in rows),
              "intentional_fixes": sum(
                  r["partial_focus_fix_verified"] or r["complete_language_fix_verified"]
                  for r in rows)}

    def _row_ok(row):
        if row["expected_partial_focus_fix"]:
            return row["partial_focus_fix_verified"]
        if row["expected_complete_language_fix"]:
            return row["complete_language_fix_verified"]
        return row["equal"]

    result["passed"] = (result["total"] == 41 and result["exact_matches"] == 38
                        and result["intentional_fixes"] == 3
                        and all(_row_ok(row) for row in rows))
    common.write_json(args.output, result)
    print(json.dumps({k: result[k] for k in ("passed", "total", "exact_matches")}))
    raise SystemExit(0 if result["passed"] else 1)


if __name__ == "__main__":
    main()
