#!/usr/bin/env python3
"""Check fuzz discovery, failure handling and exact package/cache arguments."""
import subprocess
import os
import shutil
import sys
import tempfile
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
        self.assertNotIn("/usr/bin/makepkg", output, "make must preserve shell loop variables")

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

    @unittest.skipUnless(shutil.which("make"), "make is required for the fuzz target regression")
    def test_package_discovery_failure_is_not_reported_as_no_fuzz_targets(self):
        self.assert_fake_go_fails("list", "go list failed deliberately")

    @unittest.skipUnless(shutil.which("make"), "make is required for the fuzz target regression")
    def test_test_list_failure_is_not_silenced(self):
        self.assert_fake_go_fails("test-list", "go test -list failed deliberately")

    @unittest.skipUnless(shutil.which("make"), "make is required for the fuzz target regression")
    def test_empty_target_set_fails_instead_of_returning_green(self):
        self.assert_fake_go_fails("empty", "No fuzz targets found")

    @unittest.skipUnless(shutil.which("make"), "make is required for the fuzz target regression")
    def test_successful_discovery_runs_the_named_package_and_target(self):
        for cache in (False, True):
            with self.subTest(explicit_cache=cache):
                self.assert_successful_discovery(cache)

    def assert_successful_discovery(self, cache):
        make = shutil.which("make")
        with tempfile.TemporaryDirectory() as temporary:
            fake_bin = Path(temporary)
            fake_go = fake_bin / "go"
            log = fake_bin / "go-args.log"
            fake_go.write_text("""#!/bin/sh
for arg do printf '%s\\n' "$arg" >> "$FAKE_GO_LOG"; done
if [ "$1" = "list" ]; then echo ./pkg/demo; exit 0; fi
if [ "$1" = "test" ] && [ "$2" = "-list" ]; then echo FuzzDemo; exit 0; fi
if [ "$1" = "test" ] && [ "$2" = "./pkg/demo" ] && [ "$3" = "-run=^$" ] && [ "$4" = "-fuzz=^FuzzDemo$" ] && [ "$5" = "-fuzztime=1s" ]; then
  if [ "$FAKE_GO_CACHE" = "yes" ]; then
    [ "$#" = "6" ] && [ "$6" = "-test.fuzzcachedir=$FAKE_GO_CACHE_PATH" ] && exit 0
  else
    [ "$#" = "5" ] && exit 0
  fi
fi
echo "unexpected go arguments: $*" >&2
exit 7
""")
            fake_go.chmod(0o755)
            environment = os.environ.copy()
            environment["PATH"] = str(fake_bin) + os.pathsep + environment.get("PATH", "")
            environment["FAKE_GO_MODE"] = "success"
            environment["FAKE_GO_LOG"] = str(log)
            environment["FAKE_GO_CACHE"] = "yes" if cache else "no"
            cache_path = fake_bin / "cache with spaces"
            environment["FAKE_GO_CACHE_PATH"] = str(cache_path)
            command = [make, "fuzz-campaign", "FUZZ_TIME=1", "FUZZ_PKG=./pkg/mapdiff"]
            if cache:
                command.append("FUZZ_CACHE=" + str(cache_path))
            result = subprocess.run(
                command,
                cwd=REPO_ROOT, env=environment, capture_output=True, text=True,
            )
            arguments = log.read_text().splitlines()
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertIn("./pkg/demo", arguments)
        self.assertIn("-list", arguments)
        self.assertIn("^Fuzz", arguments)
        self.assertIn("-fuzz=^FuzzDemo$", arguments)
        self.assertIn("-fuzztime=1s", arguments)
        self.assertIn("==> ./pkg/demo: FuzzDemo (1s)", result.stdout)

    def assert_fake_go_fails(self, mode, diagnostic):
        make = shutil.which("make")
        with tempfile.TemporaryDirectory() as temporary:
            fake_bin = Path(temporary)
            fake_go = fake_bin / "go"
            fake_go.write_text("""#!/bin/sh
if [ "$1" = "list" ]; then
  if [ "$FAKE_GO_MODE" = "list" ]; then echo "go list failed deliberately" >&2; exit 9; fi
  echo ./pkg/demo
  exit 0
fi
if [ "$1" = "test" ] && [ "$2" = "-list" ]; then
  if [ "$FAKE_GO_MODE" = "test-list" ]; then echo "go test -list failed deliberately" >&2; exit 8; fi
  if [ "$FAKE_GO_MODE" = "empty" ]; then exit 0; fi
  echo FuzzDemo
  exit 0
fi
exit 0
""")
            fake_go.chmod(0o755)
            environment = os.environ.copy()
            environment["PATH"] = str(fake_bin) + os.pathsep + environment.get("PATH", "")
            environment["FAKE_GO_MODE"] = mode
            result = subprocess.run(
                [make, "fuzz-campaign", "FUZZ_TIME=1", "FUZZ_PKG=./pkg/mapdiff"],
                cwd=REPO_ROOT, env=environment, capture_output=True, text=True,
            )
        self.assertNotEqual(result.returncode, 0, result.stdout)
        self.assertIn(diagnostic, result.stdout + result.stderr)


if __name__ == "__main__":
    unittest.main()
