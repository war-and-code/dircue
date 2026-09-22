#!/usr/bin/env python3
"""Compare the inherited CLI corpus with the released 0.8.0 executable.

Keep every raw observation, including intentional error-message improvements.
An allowed diagnostic change must retain the failure status and empty stdout;
successful outputs are never normalized or exempted.
"""

import argparse
from collections import Counter
from datetime import datetime, timezone
import json
from pathlib import Path
import sys
import tempfile


ROOT = Path(__file__).resolve().parents[2]
sys.path.insert(0, str(ROOT / "tests/context_v080"))
import common
import broad

BASELINE_SHA256 = "429583365b66f767443a7fff9efaa48873a924643dd48bb48c94d3afee5f65bc"


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    for name in ("baseline", "candidate", "build-receipt", "worker", "output"):
        parser.add_argument("--" + name, type=Path, required=True)
    parser.add_argument("--worker-sha256", required=True)
    parser.add_argument("--expected-diagnostics", type=Path,
                        help="Reviewed case IDs with exact old/new stderr and reasons")
    parser.add_argument("--expected-behavior-changes", type=Path,
                        help="Reviewed exit-code-changing rejections with exact old/new receipts")
    args = parser.parse_args()
    baseline, candidate, worker = (getattr(args, k).resolve()
                                   for k in ("baseline", "candidate", "worker"))
    if common.sha256(baseline) != BASELINE_SHA256:
        raise AssertionError("expected the verified darwin-arm64 v0.8.0 release")
    if common.sha256(worker) != args.worker_sha256:
        raise AssertionError("worker checksum differs")
    receipt, receipt_hash = common.load_build_receipt(args.build_receipt, candidate)
    allowed = json.loads(args.expected_diagnostics.read_text()) if args.expected_diagnostics else {}
    if not isinstance(allowed, dict) or any(
        not isinstance(v, dict) or set(v) != {"reason", "baseline_stderr", "candidate_stderr"}
        or any(not isinstance(s, str) or not s.strip() for s in v.values())
        for v in allowed.values()
    ):
        raise AssertionError("diagnostic exceptions need exact old/new stderr and reasons")
    behavior_changes = {}
    if args.expected_behavior_changes:
        raw = json.loads(args.expected_behavior_changes.read_text())
        if not isinstance(raw, dict) or raw.get("schema") != "dircue-v100-reviewed-behavior-changes-1":
            raise AssertionError("behavior-change file lacks the reviewed-behavior-changes-1 schema tag")
        cases = raw.get("cases", {})
        if not isinstance(cases, dict) or not cases:
            raise AssertionError("behavior-change file has no cases")
        for cid, entry in cases.items():
            if not isinstance(entry, dict) or set(entry) != {"review_id", "argv", "baseline", "candidate", "reason"}:
                raise AssertionError(f"behavior-change entry {cid!r} needs review_id, argv, baseline, candidate, reason")
            for side in ("baseline", "candidate"):
                side_entry = entry[side]
                if not isinstance(side_entry, dict) or set(side_entry) != {"exit", "stdout", "stderr"}:
                    raise AssertionError(f"behavior-change entry {cid!r}[{side}] needs exit, stdout, stderr")
                if not isinstance(side_entry["exit"], int) or not isinstance(side_entry["stdout"], str) or not isinstance(side_entry["stderr"], str):
                    raise AssertionError(f"behavior-change entry {cid!r}[{side}] has non-scalar fields")
            if not isinstance(entry["reason"], str) or not entry["reason"].strip():
                raise AssertionError(f"behavior-change entry {cid!r} needs a nonempty reason")
        behavior_changes = cases
    if set(behavior_changes) & set(allowed):
        raise AssertionError("a case ID cannot be both a diagnostic exception and a behavior change")
    report = {
        "schema": "dircue-v100-cli-preservation-1",
        "started_at_utc": datetime.now(timezone.utc).isoformat(),
        "baseline_release": "v0.8.0", "baseline_sha256": BASELINE_SHA256,
        "candidate_sha256": common.sha256(candidate),
        "worker_sha256": common.sha256(worker),
        "build_receipt": receipt, "build_receipt_sha256": receipt_hash,
        "harness_sha256": common.sha256(Path(__file__)),
        "inherited_helpers": broad.source_hashes(), "cases": [],
        "allowed_diagnostic_changes": allowed,
        "allowed_behavior_changes": behavior_changes,
    }
    with tempfile.TemporaryDirectory(prefix="dircue-v100-compat-") as temporary:
        base = Path(temporary)
        legacy = broad.v060.previous.load_legacy()
        env, _flat, inherited = legacy.fixture(base)
        env.pop("DIRCUE_STRUCTURAL_WORKER", None)
        env.update(NO_COLOR="1", LC_ALL="C")
        matrix, languages = broad.v060.previous.cases(base, env, inherited, worker)
        matrix += broad.v060.extra_cases(base, worker)
        matrix += broad.v060.declaration_cases(base, baseline, env)
        if dict(Counter(row[1] for row in matrix)) != broad.EXPECTED_GROUPS:
            raise AssertionError("inherited corpus changed")
        manifest = broad.v060.previous.fixture_manifest(base)
        report["fixtures_sha256"] = manifest
        for case_id, group, cwd, options in matrix:
            old = broad.v060.previous.capture(baseline, options, cwd, env)
            new = broad.v060.previous.capture(candidate, options, cwd, env)
            broad.v060.previous.check_reference(case_id, old, languages)
            diagnostic = (case_id in allowed and old[0] == new[0] == 1
                          and old[1] == new[1] == b"" and old[2] != new[2]
                          and old[2] == allowed[case_id]["baseline_stderr"].encode()
                          and new[2] == allowed[case_id]["candidate_stderr"].encode())
            behavior_entry = behavior_changes.get(case_id)
            behavior_ok = False
            if behavior_entry is not None:
                if list(options) != list(behavior_entry["argv"]):
                    raise AssertionError(f"{case_id!r}: behavior-change argv does not match the corpus argv")
                b, c = behavior_entry["baseline"], behavior_entry["candidate"]
                behavior_ok = (old[0] == b["exit"] and old[1] == b["stdout"].encode() and old[2] == b["stderr"].encode()
                               and new[0] == c["exit"] and new[1] == c["stdout"].encode() and new[2] == c["stderr"].encode())
            report["cases"].append({
                "id": case_id, "group": group, "args": options,
                "cwd": str(cwd.relative_to(base)), "exact": old == new,
                "approved_diagnostic_change": diagnostic,
                "approved_behavior_change": behavior_ok,
                "passed": old == new or diagnostic or behavior_ok,
                "baseline": common.recorded(old), "candidate": common.recorded(new),
            })
        if manifest != broad.v060.previous.fixture_manifest(base):
            raise AssertionError("fixtures changed during execution")
    observed = {r["id"] for r in report["cases"] if r["approved_diagnostic_change"]}
    report["unused_diagnostic_exceptions"] = sorted(set(allowed) - observed)
    observed_behavior = {r["id"] for r in report["cases"] if r["approved_behavior_change"]}
    report["unused_behavior_change_exceptions"] = sorted(set(behavior_changes) - observed_behavior)
    report["total"] = len(report["cases"])
    report["exact_matches"] = sum(r["exact"] for r in report["cases"])
    report["approved_behavior_changes"] = sum(r["approved_behavior_change"] for r in report["cases"])
    report["passed"] = (report["total"] == 278 and all(r["passed"] for r in report["cases"])
                        and not report["unused_diagnostic_exceptions"]
                        and not report["unused_behavior_change_exceptions"])
    report["finished_at_utc"] = datetime.now(timezone.utc).isoformat()
    if common.sha256(baseline) != BASELINE_SHA256 or common.sha256(candidate) != report["candidate_sha256"]:
        raise AssertionError("executables changed during execution")
    common.write_json(args.output, report)
    print(json.dumps({k: report[k] for k in ("passed", "total", "exact_matches",
                                              "approved_behavior_changes",
                                              "unused_diagnostic_exceptions",
                                              "unused_behavior_change_exceptions")}))
    raise SystemExit(0 if report["passed"] else 1)


if __name__ == "__main__":
    main()
