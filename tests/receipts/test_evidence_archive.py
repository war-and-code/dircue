#!/usr/bin/env python3
"""
Unit tests for the evidence-archive.json manifest.

Checks:
- All asset names are unique
- All sha256 values are valid lowercase hex (64 chars)
- All sizes are > 0
- No archived paths are tracked in Git (local receipt restoration is allowed)
- All URLs match the expected pattern for evidence-archive-1
- Manifest loads as valid JSON with required keys

Network is NOT used. Run with: python3 -m unittest tests/receipts/test_evidence_archive.py
"""
import hashlib
import json
import re
import subprocess
import unittest
from pathlib import Path

REPO_ROOT = Path(__file__).resolve().parents[2]
MANIFEST_PATH = REPO_ROOT / "tests" / "receipts" / "evidence-archive.json"
EXPECTED_URL_PREFIX = "https://github.com/war-and-code/dircue/releases/download/evidence-archive-1/"
HEX_RE = re.compile(r'^[0-9a-f]{64}$')


class TestEvidenceArchiveManifest(unittest.TestCase):

    @classmethod
    def setUpClass(cls):
        cls.manifest = json.loads(MANIFEST_PATH.read_text())
        cls.entries = cls.manifest["entries"]

    def test_manifest_has_required_top_level_keys(self):
        for key in ("description", "release_tag", "release_url", "base_download_url", "entries"):
            self.assertIn(key, self.manifest, f"missing key: {key}")

    def test_entries_is_nonempty_list(self):
        self.assertIsInstance(self.entries, list)
        self.assertGreater(len(self.entries), 0, "entries list is empty")

    def test_all_entries_have_required_keys(self):
        required = {"path", "sha256", "size", "asset", "url"}
        for e in self.entries:
            missing = required - set(e.keys())
            self.assertFalse(missing, f"entry missing keys {missing}: {e.get('path', '?')}")

    def test_asset_names_are_unique(self):
        assets = [e["asset"] for e in self.entries]
        self.assertEqual(len(assets), len(set(assets)),
                         "duplicate asset names found in manifest")

    def test_sha256_values_are_valid_hex(self):
        for e in self.entries:
            self.assertRegex(e["sha256"], HEX_RE,
                             f"invalid sha256 for {e['path']}: {e['sha256']!r}")

    def test_sizes_are_positive(self):
        for e in self.entries:
            self.assertGreater(e["size"], 0,
                               f"size is not > 0 for {e['path']}")

    def test_urls_match_expected_pattern(self):
        for e in self.entries:
            self.assertTrue(
                e["url"].startswith(EXPECTED_URL_PREFIX),
                f"unexpected URL for {e['path']}: {e['url']!r}"
            )
            expected_url = EXPECTED_URL_PREFIX + e["asset"]
            self.assertEqual(e["url"], expected_url,
                             f"URL does not match asset name for {e['path']}")

    def test_archived_paths_are_not_tracked(self):
        tracked = set(subprocess.check_output(
            ["git", "ls-files", "-z"], cwd=REPO_ROOT
        ).decode().split("\0"))
        self.assertFalse(tracked.intersection(e["path"] for e in self.entries),
                         "Archived receipts belong in release assets, not Git")

    def test_path_prefixes_are_expected(self):
        expected_prefixes = (
            "tests/enry-performance/results/",
            "tests/stress/results/",
            "tests/profiling/results/",
            "tests/release/results/",
            "tests/compatibility_next/results/v110-",
            "tests/performance/v110_candidate/results/",
        )
        for e in self.entries:
            self.assertTrue(
                any(e["path"].startswith(p) for p in expected_prefixes),
                f"unexpected path prefix: {e['path']!r}"
            )

    def test_release_tag_is_correct(self):
        self.assertEqual(self.manifest["release_tag"], "evidence-archive-1")


if __name__ == "__main__":
    unittest.main()
