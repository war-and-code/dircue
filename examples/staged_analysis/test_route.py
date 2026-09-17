"""Run with unittest discovery or --candidate /path/to/dircue for real scans."""

import argparse
import copy
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest

import route


class RoutingContract(unittest.TestCase):
    def setUp(self):
        self.report = {
            "schema_version": "1.2.0", "languages": [], "ecosystems": [],
            "warnings": [], "projects": {
                "source": "directory", "status": "complete", "diagnostics": [],
                "omitted_files": 0, "ambiguous": {"files": 0}, "projects": [],
                "composition": [{"name": "data", "files": 1, "bytes": 2**31}],
            },
        }

    def test_data_only_is_observation_not_skip_authorization(self):
        plan = route.plan(self.report)
        self.assertTrue(plan["no_followup_evidence"])
        self.assertFalse(plan["review_required"])
        self.assertNotIn("safe_to_skip", plan)

    def test_small_project_survives_large_data_share(self):
        self.report["projects"]["projects"] = [{"root": "tiny"}]
        self.report["languages"] = [{"name": "C#", "file_count": 1}]
        plan = route.plan(self.report)
        self.assertFalse(plan["no_followup_evidence"])
        self.assertTrue(all(item["suggested"] for item in plan["candidates"].values()))
        self.assertEqual(plan["candidates"]["package_inventory"]["project_root_hints"], ["tiny"])

    def test_uncertainty_prevents_negative_inference(self):
        mutations = [
            lambda r: r["warnings"].append({"code": "file_too_large"}),
            lambda r: r["projects"].update(status="partial"),
            lambda r: r["projects"].update(status="skipped"),
            lambda r: r["projects"].update(omitted_files=1),
            lambda r: r["projects"]["diagnostics"].append({"code": "malformed"}),
            lambda r: r["projects"]["ambiguous"].update(files=1),
            lambda r: r["projects"].update(composition=[]),
        ]
        for mutation in mutations:
            report = copy.deepcopy(self.report)
            mutation(report)
            plan = route.plan(report)
            self.assertTrue(plan["review_required"], report)
            self.assertFalse(plan["no_followup_evidence"], report)
        for role in ["unknown", "binary", "vendored", "generated", "new_role",
                     "source", "test", "configuration"]:
            with self.subTest(role=role):
                report = copy.deepcopy(self.report)
                report["projects"]["composition"][0]["name"] = role
                plan = route.plan(report)
                self.assertTrue(plan["review_required"])
                self.assertFalse(plan["no_followup_evidence"])
                self.assertEqual(plan["candidates"]["package_inventory"]["suggested"],
                                 role in {"binary", "vendored"})

    def test_test_role_and_partially_excluded_source(self):
        self.report["projects"]["composition"][0]["name"] = "test"
        self.report["languages"] = [{"name": "Go", "file_count": 1}]
        self.assertFalse(route.plan(self.report)["review_required"])
        self.report["projects"]["composition"][0]["files"] = 2
        plan = route.plan(self.report)
        self.assertIn("source_outside_language_statistics", plan["review_reasons"])
        self.assertFalse(plan["no_followup_evidence"])

    def test_rejects_wrong_contract_or_malformed_consumed_fields(self):
        mutations = [
            lambda r: r.update(schema_version="1.0.0"),
            lambda r: r.pop("projects"),
            lambda r: r["projects"].update(source="git"),
            lambda r: r.update(warnings=None),
            lambda r: r["projects"].update(omitted_files=True),
            lambda r: r["projects"]["composition"][0].update(files=-1),
            lambda r: r["projects"]["composition"].append(
                {"name": "data", "files": 1, "bytes": 1}),
        ]
        for mutation in mutations:
            report = copy.deepcopy(self.report)
            mutation(report)
            with self.assertRaises(ValueError):
                route.plan(report)

    def test_consumer_process_rejects_bad_json_without_output(self):
        with tempfile.TemporaryDirectory() as temporary:
            report = Path(temporary) / "broken.json"
            report.write_text("{", encoding="utf-8")
            result = subprocess.run([sys.executable, str(Path(route.__file__)), str(report)],
                                    capture_output=True, text=True)
            self.assertEqual(result.returncode, 1)
            self.assertEqual(result.stdout, "")
            self.assertIn("Cannot route report:", result.stderr)


@unittest.skipUnless(os.environ.get("DIRCUE_CANDIDATE"), "set DIRCUE_CANDIDATE for real CLI scans")
class CandidateReports(unittest.TestCase):
    def test_real_reports_and_consumer_process(self):
        candidate = str(Path(os.environ["DIRCUE_CANDIDATE"]).resolve())
        with tempfile.TemporaryDirectory() as temporary:
            base = Path(temporary)
            root = base / "input"
            root.mkdir()
            (root / "events.xml").write_text("<events><event>hello</event></events>\n", encoding="utf-8")

            def scan(*flags):
                result = subprocess.run([candidate, "analyze", "all", "--projects",
                                         "--source", "directory", "--json", *flags, str(root)],
                                        capture_output=True, text=True, check=True)
                report_path = base / "profile.json"
                report_path.write_text(result.stdout, encoding="utf-8")
                routed = subprocess.run([sys.executable, str(Path(route.__file__)), str(report_path)],
                                        capture_output=True, text=True, check=True)
                return json.loads(result.stdout), json.loads(routed.stdout)

            report, plan = scan()
            self.assertNotIn("metrics", report)
            self.assertNotIn("structure", report)
            self.assertTrue(plan["no_followup_evidence"])
            _, plan = scan("--max-file-bytes", "8")
            self.assertTrue(plan["review_required"])
            self.assertFalse(plan["no_followup_evidence"])
            _, plan = scan("--tree-size", "1")
            self.assertTrue(plan["review_required"])
            self.assertEqual(plan["project_status"], "skipped")
            (root / "app.csproj").write_text('<Project Sdk="Microsoft.NET.Sdk"/>', encoding="utf-8")
            (root / "App.cs").write_text("class App { static void Main() {} }\n", encoding="utf-8")
            _, plan = scan()
            self.assertTrue(all(item["suggested"] for item in plan["candidates"].values()))
            (root / ".gitattributes").write_text("*.cs linguist-detectable=false\n", encoding="utf-8")
            _, plan = scan()
            self.assertIn("source_outside_language_statistics", plan["review_reasons"])
            self.assertFalse(plan["no_followup_evidence"])
            (root / ".gitattributes").write_text(
                "*.cs linguist-detectable=false\n*.xml linguist-detectable=true\n"
                ".gitattributes linguist-vendored=false\n", encoding="utf-8")
            _, plan = scan()
            self.assertIn("source_without_structural_language_evidence", plan["review_reasons"])
            self.assertFalse(plan["no_followup_evidence"])
            for path in root.iterdir():
                path.unlink()
            (root / "library.dll").write_bytes(b"MZ\x00\x00library")
            _, plan = scan()
            self.assertTrue(plan["candidates"]["package_inventory"]["suggested"])
            self.assertTrue(plan["review_required"])


if __name__ == "__main__":
    parser = argparse.ArgumentParser(add_help=False)
    parser.add_argument("--candidate")
    args, remaining = parser.parse_known_args()
    if args.candidate:
        os.environ["DIRCUE_CANDIDATE"] = args.candidate
        CandidateReports.__unittest_skip__ = False
    unittest.main(argv=[sys.argv[0], *remaining])
