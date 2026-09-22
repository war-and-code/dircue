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
            report["cases"].append({
                "id": case_id, "group": group, "args": options,
                "cwd": str(cwd.relative_to(base)), "exact": old == new,
                "approved_diagnostic_change": diagnostic,
                "passed": old == new or diagnostic,
                "baseline": common.recorded(old), "candidate": common.recorded(new),
            })
        if manifest != broad.v060.previous.fixture_manifest(base):
            raise AssertionError("fixtures changed during execution")
    observed = {r["id"] for r in report["cases"] if r["approved_diagnostic_change"]}
    report["unused_diagnostic_exceptions"] = sorted(set(allowed) - observed)
    report["total"] = len(report["cases"])
    report["exact_matches"] = sum(r["exact"] for r in report["cases"])
    report["passed"] = (report["total"] == 278 and all(r["passed"] for r in report["cases"])
                        and not report["unused_diagnostic_exceptions"])
    report["finished_at_utc"] = datetime.now(timezone.utc).isoformat()
    if common.sha256(baseline) != BASELINE_SHA256 or common.sha256(candidate) != report["candidate_sha256"]:
        raise AssertionError("executables changed during execution")
    common.write_json(args.output, report)
    print(json.dumps({k: report[k] for k in ("passed", "total", "exact_matches", "unused_diagnostic_exceptions")}))
    raise SystemExit(0 if report["passed"] else 1)


if __name__ == "__main__":
    main()
