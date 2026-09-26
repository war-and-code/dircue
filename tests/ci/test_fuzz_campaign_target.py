#!/usr/bin/env python3
"""Test that the fuzz-campaign Makefile target exists and produces a dry-run plan.

This test fails on 427c2f8 (target absent) and passes after the fix.
"""
import subprocess
import sys
import unittest
from pathlib import Path

REPO_ROOT = Path(__file__).resolve().parent.parent.parent


class TestFuzzCampaignTarget(unittest.TestCase):
    def test_make_n_fuzz_campaign_succeeds(self):
        """make -n fuzz-campaign must exit 0 and emit at least one go test command."""
        result = subprocess.run(
            ["make", "-n", "fuzz-campaign", "FUZZ_TIME=5", "FUZZ_PKG=./pkg/mapdiff"],
            cwd=REPO_ROOT,
            capture_output=True,
            text=True,
        )
        self.assertEqual(
            result.returncode,
            0,
            f"make -n fuzz-campaign failed (exit {result.returncode}):\n{result.stderr}",
        )
        output = result.stdout + result.stderr
        self.assertIn(
            "go test",
            output,
            "make -n fuzz-campaign should emit a go test command",
        )
        self.assertIn(
            "-fuzz",
            output,
            "make -n fuzz-campaign should include the -fuzz flag",
        )

    def test_fuzz_campaign_target_in_makefile(self):
        """The Makefile must declare a fuzz-campaign target."""
        makefile = (REPO_ROOT / "Makefile").read_text()
        self.assertIn(
            "fuzz-campaign",
            makefile,
            "Makefile must contain a fuzz-campaign target",
        )

    def test_workflow_references_fuzz_campaign(self):
        """The fuzz-campaign workflow must call make fuzz-campaign."""
        wf = REPO_ROOT / ".github" / "workflows" / "fuzz-campaign.yml"
        self.assertTrue(wf.exists(), f"workflow file not found: {wf}")
        content = wf.read_text()
        self.assertIn(
            "make fuzz-campaign",
            content,
            "fuzz-campaign.yml must call make fuzz-campaign",
        )


if __name__ == "__main__":
    unittest.main()
