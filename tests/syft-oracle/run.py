#!/usr/bin/env python3
"""Syft oracle: verify that dircue map integrates a pre-generated Syft JSON report.

Oracle role
-----------
This harness is an independent oracle for the *packages* coverage question.
The `dircue map` command produces a `packages` coverage status in its output.
Without a Syft attachment, that status is `unknown` (no package data available).
With a valid Syft JSON attachment, the status must become either `complete` or
`partial`—never remain `unknown`.

The fixture (tests/syft-oracle/fixtures/composition-syft.json) is a pre-generated
Syft JSON report for the composition fixture. It documents two NuGet packages that
Syft would discover in a real .NET project. The oracle checks that dircue correctly
ingests the attachment and reflects it in the packages coverage question and the
coverage ledger.

Oracle matrix
-------------
| Oracle         | What it validates                  | Where it runs         |
|----------------|------------------------------------|-----------------------|
| Linguist 9.7.0 | language-detection fidelity        | CI: linguist-conformance job |
| scc 4.1.0      | line-count metrics differential    | CI: metrics-conformance job  |
| Syft 1.x       | package coverage binding           | make syft-oracle / workflow_dispatch |

This harness is designed to run offline after an initial image pull. The Syft
report in tests/syft-oracle/fixtures/ is pre-generated and committed, so no
network access is required at oracle run time.

Usage
-----
  python3 tests/syft-oracle/run.py --binary bin/dircue
  python3 tests/syft-oracle/run.py --binary bin/dircue --fixture tests/syft-oracle/fixtures/composition-syft.json
  python -m unittest tests/syft-oracle/run.py

Exit code 0 on success, non-zero on any assertion failure.
"""

from __future__ import annotations

import argparse
import json
import subprocess
import sys
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
BINARY_DEFAULT = ROOT / "bin" / "dircue"
FIXTURE_DEFAULT = ROOT / "tests" / "syft-oracle" / "fixtures" / "composition-syft.json"
COMPOSITION_DIR = ROOT / "tests" / "compatibility_next" / "composition" / "fixture"


def _run_map(binary: Path, fixture_dir: Path, syft_fixture: Path) -> dict:
    """Run dircue map with a Syft attachment and return parsed JSON output."""
    cmd = [
        str(binary),
        "map",
        "--source", "directory",
        "--workers", "1",
        "--json",
        "--attach", f"syft-json={syft_fixture}",
        str(fixture_dir),
    ]
    result = subprocess.run(
        cmd,
        capture_output=True,
        text=True,
        timeout=60,
    )
    if result.returncode != 0:
        raise AssertionError(
            f"dircue map exited {result.returncode}\n"
            f"cmd: {' '.join(cmd)}\n"
            f"stderr: {result.stderr}"
        )
    return json.loads(result.stdout)


def _run_map_no_attach(binary: Path, fixture_dir: Path) -> dict:
    """Run dircue map without any Syft attachment and return parsed JSON output."""
    cmd = [
        str(binary),
        "map",
        "--source", "directory",
        "--workers", "1",
        "--json",
        str(fixture_dir),
    ]
    result = subprocess.run(
        cmd,
        capture_output=True,
        text=True,
        timeout=60,
    )
    if result.returncode != 0:
        raise AssertionError(
            f"dircue map exited {result.returncode}\n"
            f"stderr: {result.stderr}"
        )
    return json.loads(result.stdout)


class SyftOracleCompositionFixture(unittest.TestCase):
    """Oracle checks for the Syft attachment integration.

    These tests verify that the pre-generated Syft JSON fixture (committed at
    tests/syft-oracle/fixtures/composition-syft.json) is correctly imported by
    dircue and that the packages coverage question reflects the attachment.

    The oracle validates the *binding*, not the scan accuracy: we check that
    dircue correctly reads and reflects a Syft report, not that Syft itself
    found all packages. Scanning accuracy is Syft's responsibility.
    """

    binary: Path = BINARY_DEFAULT
    syft_fixture: Path = FIXTURE_DEFAULT

    def _skip_if_missing(self) -> None:
        if not self.binary.exists():
            self.skipTest(f"binary not found: {self.binary}")
        if not self.syft_fixture.exists():
            self.skipTest(f"Syft fixture not found: {self.syft_fixture}")
        if not COMPOSITION_DIR.exists():
            self.skipTest(f"composition fixture dir not found: {COMPOSITION_DIR}")

    def test_packages_coverage_is_not_unknown_with_attachment(self) -> None:
        """With a Syft attachment, packages coverage must not be 'unknown'."""
        self._skip_if_missing()
        doc = _run_map(self.binary, COMPOSITION_DIR, self.syft_fixture)
        coverage = {c["question"]: c["status"] for c in doc.get("coverage", [])}
        status = coverage.get("packages", "missing")
        self.assertNotEqual(
            status, "unknown",
            f"packages coverage must not be 'unknown' when a Syft report is attached; got {status!r}",
        )

    def test_packages_coverage_is_unknown_without_attachment(self) -> None:
        """Without a Syft attachment, packages coverage must be 'unknown'."""
        self._skip_if_missing()
        doc = _run_map_no_attach(self.binary, COMPOSITION_DIR)
        coverage = {c["question"]: c["status"] for c in doc.get("coverage", [])}
        status = coverage.get("packages", "missing")
        self.assertEqual(
            status, "unknown",
            f"packages coverage must be 'unknown' when no Syft report is attached; got {status!r}",
        )

    def test_coverage_ledger_records_syft_run(self) -> None:
        """The coverage ledger must record the Syft run after attachment."""
        self._skip_if_missing()
        doc = _run_map(self.binary, COMPOSITION_DIR, self.syft_fixture)
        ledger = doc.get("coverage_ledger", [])
        syft_entries = [e for e in ledger if e.get("tool") == "syft"]
        self.assertGreater(
            len(syft_entries), 0,
            "coverage_ledger must include a 'syft' entry after attachment",
        )
        entry = syft_entries[0]
        self.assertTrue(
            entry.get("ran"),
            f"coverage_ledger syft entry must have ran=true; got {entry!r}",
        )

    def test_package_nodes_are_present_after_attachment(self) -> None:
        """At least one package node must be present after Syft attachment."""
        self._skip_if_missing()
        doc = _run_map(self.binary, COMPOSITION_DIR, self.syft_fixture)
        packages = [n for n in doc.get("nodes", []) if n.get("kind") == "package"]
        self.assertGreater(
            len(packages), 0,
            "At least one package node must be present after a Syft attachment",
        )

    def test_syft_tool_run_node_is_present(self) -> None:
        """A tool_run node for Syft must be present after attachment."""
        self._skip_if_missing()
        doc = _run_map(self.binary, COMPOSITION_DIR, self.syft_fixture)
        tool_runs = [n for n in doc.get("nodes", []) if n.get("kind") == "tool_run"]
        syft_runs = [n for n in tool_runs if "syft" in n.get("name", "").lower()]
        self.assertGreater(
            len(syft_runs), 0,
            "A tool_run node for syft must appear in the map after attachment",
        )

    def test_fixture_is_valid_syft_json(self) -> None:
        """The committed Syft fixture must be valid JSON with the required fields."""
        if not self.syft_fixture.exists():
            self.skipTest(f"Syft fixture not found: {self.syft_fixture}")
        with open(self.syft_fixture) as f:
            data = json.load(f)
        for field in ("schema", "descriptor", "artifacts", "artifactRelationships",
                      "source", "distro"):
            self.assertIn(field, data, f"Syft fixture must contain field: {field}")
        self.assertEqual(
            data["descriptor"].get("name"), "syft",
            "Syft fixture descriptor.name must be 'syft'",
        )
        self.assertGreater(
            len(data["artifacts"]), 0,
            "Syft fixture must contain at least one artifact",
        )


def main() -> None:
    parser = argparse.ArgumentParser(
        description=__doc__,
        formatter_class=argparse.RawDescriptionHelpFormatter,
    )
    parser.add_argument(
        "--binary", type=Path, default=BINARY_DEFAULT,
        help="Path to the dircue binary to test (default: bin/dircue)",
    )
    parser.add_argument(
        "--fixture", type=Path, default=FIXTURE_DEFAULT,
        help="Path to the Syft JSON fixture (default: tests/syft-oracle/fixtures/composition-syft.json)",
    )
    args, remaining = parser.parse_known_args()

    SyftOracleCompositionFixture.binary = args.binary.resolve()
    SyftOracleCompositionFixture.syft_fixture = args.fixture.resolve()

    loader = unittest.TestLoader()
    suite = loader.loadTestsFromTestCase(SyftOracleCompositionFixture)
    runner = unittest.TextTestRunner(verbosity=2)
    result = runner.run(suite)
    sys.exit(0 if result.wasSuccessful() else 1)


if __name__ == "__main__":
    main()
