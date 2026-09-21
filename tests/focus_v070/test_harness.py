#!/usr/bin/env python3
"""Self-tests for the 0.7 focus fixture and receipt primitives."""

from __future__ import annotations

import copy
import hashlib
from pathlib import Path
import sys
import tempfile
import unittest

sys.path.insert(0, str(Path(__file__).resolve().parent))

import common
import fixture
import run as focus_run
import verify


class HarnessTests(unittest.TestCase):
    def test_fixture_is_repeatable_and_expectations_are_external(self) -> None:
        with tempfile.TemporaryDirectory() as first, tempfile.TemporaryDirectory() as second:
            left = fixture.prepare(Path(first))
            right = fixture.prepare(Path(second))
            self.assertEqual(fixture.manifest(Path(first)), fixture.manifest(Path(second)))
            self.assertEqual(left["dotnet"]["primary"], fixture.DOTNET_PRIMARY)
            self.assertEqual(right["python"]["related_files"], fixture.PYTHON_RELATED)
            self.assertNotIn("app/vendor/NotOwned.cs", fixture.DOTNET_PRIMARY)
            self.assertNotIn("shared/Linked.cs", fixture.DOTNET_PRIMARY)

    def test_raw_capture_detects_tampering(self) -> None:
        value = common.recorded((7, b"out\n", b"err\n"))
        self.assertEqual((7, b"out\n", b"err\n"), common.decode(value))
        changed = copy.deepcopy(value)
        changed["stdout"] = "changed\n"
        with self.assertRaisesRegex(AssertionError, "stdout digest mismatch"):
            common.decode(changed)

    def test_metric_aggregation_ignores_explicit_skips(self) -> None:
        files = [
            {"status": "counted", "counts": {"files": 1, "bytes": 4, "lines": 2, "code": 1, "comment": 0, "blank": 1, "complexity": 0}},
            {"status": "skipped", "reason": "outside_scope"},
        ]
        self.assertEqual(
            {"files": 1, "bytes": 4, "lines": 2, "code": 1, "comment": 0, "blank": 1, "complexity": 0},
            common.aggregate_file_metrics(files),
        )

    def test_pinned_baseline_digest_is_sha256(self) -> None:
        self.assertEqual(64, len(common.BASELINE_SHA256))
        int(common.BASELINE_SHA256, 16)
        self.assertNotEqual(hashlib.sha256(b"").hexdigest(), common.BASELINE_SHA256)

    def test_build_binding_rejects_digest_hidden_in_random_field(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            executable = root / "dircue"
            executable.write_bytes(b"candidate")
            actual = common.sha256(executable)
            receipt = root / "build.json"
            receipt.write_text(__import__("json").dumps({
                "schema": "dircue-focus-v070-build-1",
                "candidate_sha256": "0" * 64,
                "random_note": actual,
                "files": {"main.go": "1" * 64},
                "source_at_build": {"commit": "a", "head_tree": "b", "status_sha256": "c", "diff_sha256": "d", "dirty": True},
            }))
            with self.assertRaisesRegex(AssertionError, "candidate differs"):
                common.load_build_receipt(receipt, executable)

    def test_missing_case_id_is_rejected(self) -> None:
        with self.assertRaisesRegex(AssertionError, "case matrix differs"):
            verify.require_exact_ids([{"id": "one"}], {"one", "two"}, "test")

    def test_metric_counter_tamper_is_recomputed(self) -> None:
        spec = fixture.case_specs()["dotnet"]
        def metric(path: str, code: int = 1) -> dict:
            return {"path": path, "language": "C#", "grammar": "C#", "status": "counted", "counts": {"files": 1, "bytes": 1, "lines": 1, "code": code, "comment": 0, "blank": 0, "complexity": 0}}
        primary_files = [metric(path) for path in spec["primary"]]
        related_files = [metric(path) for path in spec["related_files"]]
        totals = common.aggregate_file_metrics(primary_files)
        parsed = {
            "full": {"metrics": {"engine_version": "test", "files": primary_files + related_files}},
            "primary": {"focus": {"status": "partial", "scope": {"role": "project", "id": "primary"}, "primary": [{"path": path} for path in spec["primary"]], "context": [{"path": path} for path in spec["required_context"]]}, "focused_metrics": {"primary": {"files": copy.deepcopy(primary_files), "totals": totals, "skipped": [{"reason": "focus_scope_partial"}]}}},
            "related": {"focus": {"scope": {"id": "related", "related_projects": [spec["related"]]}, "related": [{"files": [{"path": path} for path in spec["related_files"]]}]}, "focused_metrics": {"related": [{"project": spec["related"], "metrics": {"files": copy.deepcopy(related_files)}}]}},
            "affected": {"focus": {"scope": {"id": "affected", "role": "affected-by"}, "affected_projects": {"projects": [{"project_id": spec["project"]}]}}},
        }
        focus_run.evaluate_focus(spec, parsed)
        parsed["primary"]["focused_metrics"]["primary"]["files"][0]["counts"]["code"] = 99
        with self.assertRaisesRegex(AssertionError, "per-file metrics differ"):
            focus_run.evaluate_focus(spec, parsed)


if __name__ == "__main__":
    unittest.main()
