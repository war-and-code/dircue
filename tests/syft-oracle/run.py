#!/usr/bin/env python3
"""Syft oracle: differential validation of dircue map + Syft-JSON attachment.

Oracle role
-----------
This harness is an independent oracle for the *packages* coverage question.

Contract under test
~~~~~~~~~~~~~~~~~~~
Every Syft artifact whose location is inside the scanned directory must appear
in the dircue map as a ``package`` node, matched by PURL. Artifacts whose
location is inside a dircue-detected component must have a ``packaged_in`` edge
to that component. The ``packages`` coverage question must transition from
``unknown`` (no attachment) to a bound status (``partial`` or ``complete``)
when a valid Syft report is attached. The coverage ledger must record the Syft
run with ``ran: true``.

Oracle matrix
~~~~~~~~~~~~~
| Oracle        | What it validates               | Where it runs                        |
|---------------|---------------------------------|--------------------------------------|
| Linguist 9.7.0 | language-detection fidelity    | CI: linguist-conformance job         |
| scc 4.1.0     | line-count metrics differential | CI: metrics-conformance job          |
| Syft 1.52.0   | package-coverage binding        | make syft-oracle / workflow_dispatch |

Fixtures
~~~~~~~~
``fixtures/go-single/`` + ``fixtures/go-single.syft.json``
  Minimal Go module with one declared dep (github.com/google/uuid v1.6.0).
  Real Syft 1.52.0 scan; 1 artifact; the artifact must be attributed to the
  Go module component.

``fixtures/multi/`` + ``fixtures/multi.syft.json``
  Two sub-directories: frontend/ (npm, chalk@5.3.0) and backend/ (Python,
  requests@2.31.0 + certifi@2024.2.2).
  Real Syft 1.52.0 scan; 4 artifacts; chalk and the frontend package must have
  ``packaged_in`` edges to the frontend component; requests and certifi must
  appear as package nodes.

Usage
-----
  python3 tests/syft-oracle/run.py --binary bin/dircue
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
from typing import Any

ROOT = Path(__file__).resolve().parents[2]
BINARY_DEFAULT = ROOT / "bin" / "dircue"
FIXTURES = ROOT / "tests" / "syft-oracle" / "fixtures"

# Syft fields that must not appear in the dircue map output.
# These are non-package fields that would indicate the map is leaking Syft
# metadata rather than absorbing it selectively.
_LEAKAGE_SENTINEL_KEYS = {"distro", "artifactRelationships", "schema"}


def _run_map(binary: Path, fixture_dir: Path, report: Path | None = None) -> dict:
    """Run dircue map on a fixture directory, optionally with a Syft attachment."""
    cmd = [
        str(binary),
        "map",
        "--source", "directory",
        "--workers", "1",
        "--json",
    ]
    if report is not None:
        cmd += ["--attach", f"syft-json={report}"]
    cmd.append(str(fixture_dir))
    result = subprocess.run(cmd, capture_output=True, text=True, timeout=60)
    if result.returncode != 0:
        raise AssertionError(
            f"dircue map exited {result.returncode}\n"
            f"cmd: {' '.join(cmd)}\n"
            f"stderr: {result.stderr}"
        )
    return json.loads(result.stdout)


def _syft_artifacts(report_path: Path) -> list[dict[str, Any]]:
    """Load the Syft JSON report and return the artifacts list."""
    with open(report_path) as f:
        return json.load(f)["artifacts"]


def _check_no_leakage(test: unittest.TestCase, doc: dict) -> None:
    """Assert that no Syft-specific non-package field has leaked into the map."""
    text = json.dumps(doc)
    for key in _LEAKAGE_SENTINEL_KEYS:
        test.assertNotIn(
            f'"{key}"', text,
            f"Syft field {key!r} must not appear in dircue map output",
        )


class SyftOracleGoSingle(unittest.TestCase):
    """Oracle for the go-single fixture.

    Verifies that dircue correctly maps one Go module dependency found by
    Syft and attributes it to the detected Go module component.
    """

    binary: Path = BINARY_DEFAULT
    fixture_dir: Path = FIXTURES / "go-single"
    report: Path = FIXTURES / "go-single.syft.json"

    def _skip_if_missing(self) -> None:
        if not self.binary.exists():
            self.skipTest(f"binary not found: {self.binary}")
        if not self.fixture_dir.exists():
            self.skipTest(f"fixture dir not found: {self.fixture_dir}")
        if not self.report.exists():
            self.skipTest(f"Syft report not found: {self.report}")

    def _map_with(self) -> dict:
        return _run_map(self.binary, self.fixture_dir, self.report)

    def _map_without(self) -> dict:
        return _run_map(self.binary, self.fixture_dir, None)

    def test_all_syft_artifacts_appear_as_package_nodes(self) -> None:
        """Every Syft artifact (by PURL) must appear as a package node."""
        self._skip_if_missing()
        artifacts = _syft_artifacts(self.report)
        doc = self._map_with()
        map_purls = {
            n["properties"].get("purl", "")
            for n in doc["nodes"]
            if n["kind"] == "package"
        }
        for a in artifacts:
            purl = a.get("purl", "")
            if not purl:
                continue
            self.assertIn(
                purl, map_purls,
                f"Syft artifact {purl!r} must appear as a package node",
            )

    def test_uuid_package_is_attributed_to_go_module_component(self) -> None:
        """The uuid package must have a packaged_in edge to the Go module component."""
        self._skip_if_missing()
        doc = self._map_with()
        # Find the component and the package nodes
        comps = {n["id"]: n for n in doc["nodes"] if n["kind"] == "component"}
        pkgs = {n["id"]: n for n in doc["nodes"] if n["kind"] == "package"}
        self.assertGreater(len(comps), 0, "No component detected in go-single fixture")
        # Find packaged_in edges
        pi_edges = [e for e in doc["edges"] if e["type"] == "packaged_in"]
        pkg_to_comp = {e["from"]: e["to"] for e in pi_edges}
        # At least one package must be attributed to a component
        attributed = {pid for pid in pkgs if pid in pkg_to_comp}
        self.assertGreater(
            len(attributed), 0,
            "No package node has a packaged_in edge to a component in go-single",
        )
        # The owner must be a real detected component
        for pkg_id in attributed:
            comp_id = pkg_to_comp[pkg_id]
            self.assertIn(
                comp_id, comps,
                f"packaged_in target {comp_id!r} is not a component node",
            )

    def test_packages_coverage_transitions_from_unknown_to_bound(self) -> None:
        """packages coverage must be unknown without Syft, non-unknown with it."""
        self._skip_if_missing()
        without = {c["question"]: c["status"] for c in self._map_without().get("coverage", [])}
        with_ = {c["question"]: c["status"] for c in self._map_with().get("coverage", [])}
        self.assertEqual(
            without.get("packages"), "unknown",
            "packages coverage must be 'unknown' without a Syft attachment",
        )
        self.assertNotEqual(
            with_.get("packages"), "unknown",
            "packages coverage must not be 'unknown' when a Syft report is attached",
        )

    def test_coverage_ledger_records_syft_run(self) -> None:
        """The coverage ledger must record the Syft run with ran=true."""
        self._skip_if_missing()
        doc = self._map_with()
        ledger = doc.get("coverage_ledger", [])
        syft_entries = [e for e in ledger if e.get("tool") == "syft"]
        self.assertGreater(len(syft_entries), 0,
                           "coverage_ledger must include a 'syft' entry")
        self.assertTrue(syft_entries[0].get("ran"),
                        f"coverage_ledger syft entry must have ran=true; got {syft_entries[0]!r}")

    def test_no_syft_metadata_leaks_into_map(self) -> None:
        """Syft non-package fields must not leak into the dircue map output."""
        self._skip_if_missing()
        _check_no_leakage(self, self._map_with())


class SyftOracleMultiComponent(unittest.TestCase):
    """Oracle for the multi-component fixture.

    Verifies that:
    - All 4 Syft artifacts (chalk, frontend, requests, certifi) appear as
      package nodes.
    - Packages in frontend/ (chalk, frontend npm) have packaged_in edges to
      the frontend component that dircue detects from frontend/package.json.
    - Packages in backend/ (requests, certifi) appear as package nodes even
      without a detected backend component.
    - packages coverage transitions from unknown to bound.
    """

    binary: Path = BINARY_DEFAULT
    fixture_dir: Path = FIXTURES / "multi"
    report: Path = FIXTURES / "multi.syft.json"

    def _skip_if_missing(self) -> None:
        if not self.binary.exists():
            self.skipTest(f"binary not found: {self.binary}")
        if not self.fixture_dir.exists():
            self.skipTest(f"fixture dir not found: {self.fixture_dir}")
        if not self.report.exists():
            self.skipTest(f"Syft report not found: {self.report}")

    def _map_with(self) -> dict:
        return _run_map(self.binary, self.fixture_dir, self.report)

    def _map_without(self) -> dict:
        return _run_map(self.binary, self.fixture_dir, None)

    def test_all_syft_artifacts_appear_as_package_nodes(self) -> None:
        """All 4 Syft artifacts (by PURL) must appear as package nodes."""
        self._skip_if_missing()
        artifacts = _syft_artifacts(self.report)
        doc = self._map_with()
        map_purls = {
            n["properties"].get("purl", "")
            for n in doc["nodes"]
            if n["kind"] == "package"
        }
        for a in artifacts:
            purl = a.get("purl", "")
            if not purl:
                continue
            self.assertIn(
                purl, map_purls,
                f"Syft artifact {purl!r} must appear as a package node",
            )

    def test_frontend_npm_packages_are_attributed_to_frontend_component(self) -> None:
        """chalk and frontend (npm) must have packaged_in edges to the frontend component."""
        self._skip_if_missing()
        doc = self._map_with()
        # Find the frontend component (npm, detected from frontend/package.json)
        comps = {n["id"]: n for n in doc["nodes"] if n["kind"] == "component"}
        frontend_comps = [
            n for n in comps.values()
            if any("frontend" in p for p in n.get("paths", []))
        ]
        self.assertGreater(len(frontend_comps), 0,
                           "No frontend component detected in multi fixture")
        frontend_id = frontend_comps[0]["id"]
        # Find package nodes in frontend/ by their purl
        frontend_purls = {"pkg:npm/chalk@5.3.0", "pkg:npm/frontend@1.0.0"}
        pkg_by_purl = {
            n["properties"].get("purl", ""): n["id"]
            for n in doc["nodes"]
            if n["kind"] == "package"
        }
        # Find packaged_in edges
        pi_by_from = {e["from"]: e["to"] for e in doc["edges"]
                      if e["type"] == "packaged_in"}
        for purl in frontend_purls:
            pkg_id = pkg_by_purl.get(purl)
            if pkg_id is None:
                self.fail(f"npm package {purl!r} not found in map")
            owner = pi_by_from.get(pkg_id)
            self.assertEqual(
                owner, frontend_id,
                f"{purl!r} must be attributed to the frontend component "
                f"(want {frontend_id!r}, got {owner!r})",
            )

    def test_python_packages_are_attributed_to_backend_component(self) -> None:
        """requests and certifi must have packaged_in edges to the backend component.

        The backend/ directory contains only requirements.txt (no .py source files).
        dircue must still detect it as a Python component and attribute the Syft
        packages located in that directory to it.
        """
        self._skip_if_missing()
        doc = self._map_with()
        # Find the backend component (Python, detected from backend/requirements.txt)
        comps = {n["id"]: n for n in doc["nodes"] if n["kind"] == "component"}
        backend_comps = [
            n for n in comps.values()
            if any("backend" in p for p in n.get("paths", []))
        ]
        self.assertGreater(
            len(backend_comps), 0,
            "No backend component detected in multi fixture: "
            "requirements-only Python directories must be recognised as components",
        )
        backend_id = backend_comps[0]["id"]
        # Verify the Python packages appear as nodes
        pkg_by_purl = {
            n["properties"].get("purl", ""): n["id"]
            for n in doc["nodes"]
            if n["kind"] == "package"
        }
        for purl in ("pkg:pypi/requests@2.31.0", "pkg:pypi/certifi@2024.2.2"):
            self.assertIn(purl, pkg_by_purl,
                          f"Python package {purl!r} must appear as a package node")
        # Verify the packaged_in edges to the backend component
        pi_by_from = {}
        for e in doc["edges"]:
            if e["type"] == "packaged_in":
                pi_by_from[e["from"]] = e["to"]
        for purl in ("pkg:pypi/requests@2.31.0", "pkg:pypi/certifi@2024.2.2"):
            pkg_id = pkg_by_purl[purl]
            owner = pi_by_from.get(pkg_id)
            self.assertEqual(
                owner, backend_id,
                f"{purl!r} must be attributed to the backend component "
                f"(want {backend_id!r}, got {owner!r})",
            )

    def test_packages_coverage_transitions_from_unknown_to_bound(self) -> None:
        """packages coverage must be unknown without Syft, non-unknown with it."""
        self._skip_if_missing()
        without = {c["question"]: c["status"] for c in self._map_without().get("coverage", [])}
        with_ = {c["question"]: c["status"] for c in self._map_with().get("coverage", [])}
        self.assertEqual(
            without.get("packages"), "unknown",
            "packages coverage must be 'unknown' without a Syft attachment",
        )
        self.assertNotEqual(
            with_.get("packages"), "unknown",
            "packages coverage must not be 'unknown' when a Syft report is attached",
        )

    def test_coverage_ledger_records_syft_run(self) -> None:
        """The coverage ledger must record the Syft run with ran=true."""
        self._skip_if_missing()
        doc = self._map_with()
        ledger = doc.get("coverage_ledger", [])
        syft_entries = [e for e in ledger if e.get("tool") == "syft"]
        self.assertGreater(len(syft_entries), 0,
                           "coverage_ledger must include a 'syft' entry")
        self.assertTrue(syft_entries[0].get("ran"),
                        f"coverage_ledger syft entry must have ran=true; got {syft_entries[0]!r}")

    def test_no_syft_metadata_leaks_into_map(self) -> None:
        """Syft non-package fields must not leak into the dircue map output."""
        self._skip_if_missing()
        _check_no_leakage(self, self._map_with())


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
        "--go-report", type=Path, default=None,
        help="Override path to the go-single Syft JSON report (default: fixtures/go-single.syft.json)",
    )
    parser.add_argument(
        "--multi-report", type=Path, default=None,
        help="Override path to the multi Syft JSON report (default: fixtures/multi.syft.json)",
    )
    args, _ = parser.parse_known_args()
    SyftOracleGoSingle.binary = args.binary.resolve()
    SyftOracleMultiComponent.binary = args.binary.resolve()
    if args.go_report is not None:
        SyftOracleGoSingle.report = args.go_report.resolve()
    if args.multi_report is not None:
        SyftOracleMultiComponent.report = args.multi_report.resolve()

    loader = unittest.TestLoader()
    suite = unittest.TestSuite()
    suite.addTests(loader.loadTestsFromTestCase(SyftOracleGoSingle))
    suite.addTests(loader.loadTestsFromTestCase(SyftOracleMultiComponent))
    runner = unittest.TextTestRunner(verbosity=2)
    result = runner.run(suite)
    sys.exit(0 if result.wasSuccessful() else 1)


if __name__ == "__main__":
    main()
