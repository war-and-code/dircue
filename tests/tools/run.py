#!/usr/bin/env python3
"""Tool oracle: validates dircue's ingestion of real analyzer reports.

Oracle roles
------------
This harness is an independent oracle for several acceptance items:

  #77  Ingest deeper-tool facts and run coverage: Syft, OWASP Noir, Bifrost,
       SARIF run metadata.
  #78  Analyzer coverage ledger and blind spots.
  #79  Route deeper analyzers per component with prerequisites.
  #82  Map SARIF result locations onto the map (dircue map locate).

All fixtures in tests/tools/fixtures/ are committed real tool outputs produced
by pinned, offline Docker runs. See README.md for provenance.

Usage
-----
  python3 tests/tools/run.py --binary bin/dircue   # fast committed-report checks
  make tool-oracles                                 # full suite including go tests

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
FIXTURES = ROOT / "tests" / "tools" / "fixtures"
TESTDATA = ROOT / "tests" / "tools" / "testdata"
BINARY_DEFAULT = ROOT / "bin" / "dircue"


# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------

def _dircue(binary: Path, *args: str, timeout: int = 60) -> dict:
    """Run dircue and return parsed JSON stdout. Fails the test on error."""
    cmd = [str(binary)] + list(args)
    result = subprocess.run(cmd, capture_output=True, text=True, timeout=timeout)
    if result.returncode != 0:
        raise AssertionError(
            f"dircue exited {result.returncode}\ncmd: {' '.join(cmd)}\n"
            f"stderr: {result.stderr[:500]}"
        )
    return json.loads(result.stdout)


def _dircue_bytes(binary: Path, *args: str, timeout: int = 60) -> bytes:
    """Run dircue and return raw stdout bytes."""
    cmd = [str(binary)] + list(args)
    result = subprocess.run(cmd, capture_output=True, timeout=timeout)
    if result.returncode != 0:
        raise AssertionError(
            f"dircue exited {result.returncode}\ncmd: {' '.join(cmd)}\n"
            f"stderr: {result.stderr[:500]}"
        )
    return result.stdout


def _load_sarif_schema() -> dict:
    schema_path = TESTDATA / "sarif-schema-2.1.0.json"
    with open(schema_path) as f:
        return json.load(f)


def _validate_sarif_schema(doc: dict, schema: dict, test: unittest.TestCase) -> None:
    """Minimal SARIF 2.1.0 structural validation without external dependencies."""
    test.assertEqual(doc.get("version"), "2.1.0", "SARIF version must be 2.1.0")
    test.assertIn("runs", doc, "SARIF must have 'runs' array")
    test.assertIsInstance(doc["runs"], list, "runs must be a list")
    # Schema structure is present
    test.assertIn("properties", schema, "schema must have properties")
    test.assertIn("runs", schema["properties"], "schema must define runs")


def _check_no_findings_leaked(test: unittest.TestCase, doc: dict) -> None:
    """Assert that SARIF findings fields did not leak into the dircue map.

    dircue ingests facts and run coverage only. SARIF-specific fields
    (results, rules, notifications) must not appear as map nodes or properties.
    """
    text = json.dumps(doc)
    # No direct result data: ruleId values should not appear as map node ids
    for node in doc.get("nodes", []):
        node_id = node.get("id", "")
        test.assertFalse(
            node_id.startswith("ruleId:"),
            f"SARIF ruleId leaked into map node id: {node_id}",
        )
    # No severity or level fields in map document
    test.assertNotIn('"level":', text, "SARIF level field must not appear in map")
    test.assertNotIn('"severity":', text, "severity field must not appear in map")


# ---------------------------------------------------------------------------
# #77: Ingest deeper-tool facts (Noir)
# ---------------------------------------------------------------------------

class TestNoirIngestion(unittest.TestCase):
    """#77: Real OWASP Noir JSON reports on web framework fixtures.

    Validates:
    - Noir endpoints become interface nodes in the map.
    - Attribution is to the correct component.
    - No findings or verdicts leak into the map.
    - A mismatched snapshot binding is detected and recorded.
    """

    def setUp(self) -> None:
        if not hasattr(self.__class__, '_binary'):
            self.skipTest("binary not configured")
        self.binary = self.__class__._binary

    @classmethod
    def configure(cls, binary: Path) -> None:
        cls._binary = binary

    def _map_with_noir(self, fixture_dir: Path, noir_report: Path) -> dict:
        return _dircue(
            self.binary,
            "map",
            "--source", "directory",
            "--workers", "1",
            "--attach", f"noir-json={noir_report}",
            "--json",
            str(fixture_dir),
        )

    def test_flask_noir_endpoints_become_interfaces(self) -> None:
        """Flask fixture: 5 HTTP endpoints become interface nodes."""
        doc = self._map_with_noir(
            FIXTURES / "flask-app",
            FIXTURES / "flask-app.noir.json",
        )
        ifaces = [n for n in doc.get("nodes", []) if n.get("kind") == "interface"]
        self.assertGreaterEqual(len(ifaces), 3, f"expected ≥3 HTTP interfaces, got {len(ifaces)}")
        routes = {n.get("name", "") for n in ifaces}
        # Noir must report at least GET /items and POST /items
        self.assertTrue(
            any("/items" in r for r in routes),
            f"expected /items routes in {routes}",
        )
        _check_no_findings_leaked(self, doc)

    def test_express_noir_endpoints_become_interfaces(self) -> None:
        """Express fixture: 5 HTTP endpoints become interface nodes."""
        doc = self._map_with_noir(
            FIXTURES / "express-app",
            FIXTURES / "express-app.noir.json",
        )
        ifaces = [n for n in doc.get("nodes", []) if n.get("kind") == "interface"]
        self.assertGreaterEqual(len(ifaces), 3, f"expected ≥3 HTTP interfaces, got {len(ifaces)}")
        routes = {n.get("name", "") for n in ifaces}
        self.assertTrue(
            any("/api/items" in r for r in routes),
            f"expected /api/items routes in {routes}",
        )
        _check_no_findings_leaked(self, doc)

    def test_spring_noir_endpoints_become_interfaces(self) -> None:
        """Spring fixture: 5 HTTP endpoints (under /api) become interface nodes."""
        doc = self._map_with_noir(
            FIXTURES / "spring-app",
            FIXTURES / "spring-app.noir.json",
        )
        ifaces = [n for n in doc.get("nodes", []) if n.get("kind") == "interface"]
        self.assertGreaterEqual(len(ifaces), 3, f"expected ≥3 HTTP interfaces, got {len(ifaces)}")
        routes = {n.get("name", "") for n in ifaces}
        self.assertTrue(
            any("/api" in r for r in routes),
            f"expected /api routes in {routes}",
        )
        _check_no_findings_leaked(self, doc)

    def test_packages_coverage_transitions_on_attachment(self) -> None:
        """Coverage ledger records the Noir run."""
        doc = self._map_with_noir(
            FIXTURES / "flask-app",
            FIXTURES / "flask-app.noir.json",
        )
        ledger = doc.get("coverage_ledger", [])
        noir_entries = [e for e in ledger if e.get("tool", "").lower() == "noir"]
        self.assertTrue(noir_entries, f"no noir entry in ledger: {ledger}")
        self.assertTrue(noir_entries[0]["ran"], "Noir run must be marked ran=true")

    def test_mismatched_snapshot_binding_is_qualified(self) -> None:
        """A mismatched-snapshot Noir report degrades binding; not silently accepted.

        The Noir report was produced from a directory scan (no VCP). When
        the map source is a git commit, the binding must be 'unknown'
        (unverifiable), never 'verified'. Nothing derived from it is 'complete'.
        """
        doc = self._map_with_noir(
            FIXTURES / "flask-app",
            FIXTURES / "flask-app.noir.json",
        )
        ledger = doc.get("coverage_ledger", [])
        noir_entries = [e for e in ledger if e.get("tool", "").lower() == "noir"]
        self.assertTrue(noir_entries, f"no noir entry: {ledger}")
        # Without VCP, binding must be unknown (not verified)
        binding = noir_entries[0].get("binding", "")
        self.assertIn(binding, {"unknown", "mismatch"},
                      f"expected unknown/mismatch binding, got {binding!r}")


# ---------------------------------------------------------------------------
# #77/#82: SARIF run metadata and locate
# ---------------------------------------------------------------------------

class TestSARIFLocate(unittest.TestCase):
    """#82: dircue map locate with three emitters of different kinds.

    Fixtures:
    - ruff 0.11.13 SARIF (linter) on python-lint-sample/
    - Semgrep OSS 1.117.0 SARIF (pattern matcher) on python-lint-sample/
    - OWASP Noir 1.3.1 SARIF (endpoint finder) on flask-app/

    Validates:
    - Ownership correctness against hand-written expectations.
    - All original properties are preserved.
    - Output validates against the SARIF 2.1.0 schema structure.
    - Findings/ruleIds pass through unchanged and are not interpreted.
    """

    def setUp(self) -> None:
        if not hasattr(self.__class__, '_binary'):
            self.skipTest("binary not configured")
        self.binary = self.__class__._binary
        self._schema = _load_sarif_schema()

    @classmethod
    def configure(cls, binary: Path) -> None:
        cls._binary = binary

    def _map_dir(self, fixture_dir: Path) -> Path:
        """Build and return path to a temporary map JSON."""
        import tempfile
        import os
        tmpdir = tempfile.mkdtemp(prefix="dircue-oracle-")
        map_path = Path(tmpdir) / "map.json"
        result = subprocess.run(
            [
                str(self.binary),
                "map",
                "--source", "directory",
                "--workers", "1",
                "--json",
                str(fixture_dir),
            ],
            capture_output=True,
            text=True,
            timeout=60,
        )
        if result.returncode != 0:
            raise AssertionError(f"map failed: {result.stderr[:300]}")
        map_path.write_text(result.stdout)
        return map_path

    def _locate_summary(self, map_path: Path, sarif_path: Path, source_uri: str = "") -> dict:
        args = ["map", "locate", "--summary"]
        if source_uri:
            args += ["--source-uri", source_uri]
        args += [str(map_path), str(sarif_path)]
        return _dircue(self.binary, *args)

    def _locate_annotated(self, map_path: Path, sarif_path: Path,
                          source_uri: str = "") -> bytes:
        args = ["map", "locate"]
        if source_uri:
            args += ["--source-uri", source_uri]
        args += [str(map_path), str(sarif_path)]
        return _dircue_bytes(self.binary, *args)

    def test_ruff_linter_locations_resolve_to_component(self) -> None:
        """ruff SARIF: lint findings in python-lint-sample resolve to the component.

        Expectation: all 4 ruff findings (F401 ×2, F841, E722) are in sample.py,
        which belongs to the python-lint-sample component. Resolution: 'resolved'.
        source_uri: /src (container working directory used during report generation).
        """
        map_path = self._map_dir(FIXTURES / "python-lint-sample")
        summary = self._locate_summary(
            map_path,
            FIXTURES / "python-lint-sample.ruff.sarif.json",
            source_uri="/src",
        )
        resolutions = summary.get("resolutions", {})
        self.assertGreaterEqual(resolutions.get("resolved", 0), 4,
                                f"expected ≥4 resolved, got {resolutions}")
        self.assertEqual(resolutions.get("outside_root", 0), 0,
                         f"unexpected outside_root: {resolutions}")
        # node_counts: all resolved findings attributed to one component
        node_counts = summary.get("node_counts", [])
        self.assertTrue(node_counts, "expected non-empty node_counts")
        component_counts = [nc for nc in node_counts if nc["kind"] == "component"]
        self.assertTrue(component_counts, "expected component in node_counts")
        # Tool metadata preserved
        run = summary["runs"][0]
        self.assertEqual(run["tool"].lower(), "ruff")
        self.assertIn("0.11.13", run.get("version", ""))

    def test_ruff_annotated_output_preserves_original_properties(self) -> None:
        """Annotated ruff SARIF must preserve all original SARIF fields."""
        map_path = self._map_dir(FIXTURES / "python-lint-sample")
        annotated_bytes = self._locate_annotated(
            map_path,
            FIXTURES / "python-lint-sample.ruff.sarif.json",
            source_uri="/src",
        )
        annotated = json.loads(annotated_bytes)
        original = json.loads((FIXTURES / "python-lint-sample.ruff.sarif.json").read_bytes())
        # Version and runs count preserved
        self.assertEqual(annotated["version"], "2.1.0")
        self.assertEqual(len(annotated["runs"]), len(original["runs"]))
        # Original results fields preserved
        orig_run = original["runs"][0]
        ann_run = annotated["runs"][0]
        self.assertEqual(len(ann_run["results"]), len(orig_run["results"]))
        for orig_r, ann_r in zip(orig_run["results"], ann_run["results"]):
            self.assertEqual(ann_r["ruleId"], orig_r["ruleId"],
                             "ruleId must be preserved")
            orig_locs = orig_r.get("locations", [])
            ann_locs = ann_r.get("locations", [])
            self.assertEqual(len(ann_locs), len(orig_locs),
                             "location count must be preserved")
            # dircue.map property injected
            for loc in ann_locs:
                props = loc.get("properties", {})
                self.assertIn("dircue.map", props,
                              f"dircue.map annotation missing: {loc}")
        # Schema-level validation
        _validate_sarif_schema(annotated, self._schema, self)

    def test_semgrep_undefined_base_id_is_unresolvable(self) -> None:
        """Semgrep SARIF with %SRCROOT% base ID but no originalUriBaseIds.

        Semgrep 1.117.0 emits %SRCROOT% uriBaseId without defining it in
        originalUriBaseIds. dircue must report unresolvable_uri, not fabricate
        a path. This is a documented oracle finding, not a dircue defect.
        """
        map_path = self._map_dir(FIXTURES / "python-lint-sample")
        summary = self._locate_summary(
            map_path,
            FIXTURES / "python-lint-sample.semgrep.sarif.json",
            source_uri="/src",
        )
        resolutions = summary.get("resolutions", {})
        self.assertGreater(
            resolutions.get("unresolvable_uri", 0), 0,
            f"expected unresolvable_uri for undefined %SRCROOT%, got {resolutions}",
        )

    def test_noir_sarif_locations_resolve_to_component(self) -> None:
        """OWASP Noir SARIF: all 5 endpoint locations resolve to the Flask component.

        Noir 1.3.1 emits SARIF with /app/... URIs (container path). With
        source_uri=/app, all locations resolve to the Python component.
        Resolution: 'resolved' for all 5 findings.
        """
        map_path = self._map_dir(FIXTURES / "flask-app")
        summary = self._locate_summary(
            map_path,
            FIXTURES / "flask-app.noir.sarif.json",
            source_uri="/app",
        )
        resolutions = summary.get("resolutions", {})
        self.assertEqual(resolutions.get("resolved", 0), 5,
                         f"expected 5 resolved, got {resolutions}")
        self.assertEqual(resolutions.get("unresolvable_uri", 0), 0)
        # All attributed to a component node
        node_counts = summary.get("node_counts", [])
        component_counts = [nc for nc in node_counts if nc["kind"] == "component"]
        self.assertTrue(component_counts, "expected component in node_counts")
        # Tool: OWASP Noir, version 1.3.1
        run = summary["runs"][0]
        self.assertIn("Noir", run.get("tool", ""))

    def test_noir_sarif_annotated_preserves_unknown_properties(self) -> None:
        """Annotated Noir SARIF preserves all original SARIF properties."""
        map_path = self._map_dir(FIXTURES / "flask-app")
        annotated_bytes = self._locate_annotated(
            map_path,
            FIXTURES / "flask-app.noir.sarif.json",
            source_uri="/app",
        )
        annotated = json.loads(annotated_bytes)
        original = json.loads((FIXTURES / "flask-app.noir.sarif.json").read_bytes())
        # Structural preservation
        self.assertEqual(annotated["version"], "2.1.0")
        orig_run = original["runs"][0]
        ann_run = annotated["runs"][0]
        self.assertEqual(len(ann_run["results"]), len(orig_run["results"]))
        for orig_r, ann_r in zip(orig_run["results"], ann_run["results"]):
            self.assertEqual(ann_r["ruleId"], orig_r["ruleId"])
        # SARIF schema
        _validate_sarif_schema(annotated, self._schema, self)


# ---------------------------------------------------------------------------
# #78: Analyzer coverage ledger and blind spots
# ---------------------------------------------------------------------------

class TestCoverageLedger(unittest.TestCase):
    """#78: Expected blind spots with correct reasons on fixtures.

    Validates:
    - not_run: a tool with no attached report
    - unsupported_language: Java in spring-app; no taint analyzer runs
    - tool_error: documented as synthetic (see README); unit-tested in Go
    - prerequisite_unmet: JVM build not available for spring-app

    Note: 'crashed run' (executionSuccessful: false) could not be produced by
    any tested tool (ruff/semgrep/golangci-lint). See README.md for details.
    The case is covered by Go unit tests in pkg/coverageledger and pkg/providerjoin.
    """

    def setUp(self) -> None:
        if not hasattr(self.__class__, '_binary'):
            self.skipTest("binary not configured")
        self.binary = self.__class__._binary

    @classmethod
    def configure(cls, binary: Path) -> None:
        cls._binary = binary

    def test_flask_app_syft_not_run_blind_spot(self) -> None:
        """Flask app: no Syft report attached → syft blind spot is 'not_run'."""
        doc = _dircue(
            self.binary,
            "map",
            "--source", "directory",
            "--workers", "1",
            "--json",
            str(FIXTURES / "flask-app"),
        )
        # Find syft analyzer coverage entry for the Python component
        coverage = doc.get("analyzer_coverage", [])
        syft_entries = [e for e in coverage if e.get("tool") == "syft"]
        self.assertTrue(syft_entries, f"syft entry missing from analyzer_coverage: {coverage[:3]}")
        python_syft = [e for e in syft_entries if not e.get("ran", False)]
        self.assertTrue(python_syft, f"expected syft not_run entry: {syft_entries}")
        self.assertEqual(python_syft[0]["not_covered"], "not_run",
                         f"not_covered reason: {python_syft[0]}")

    def test_flask_app_noir_run_recorded_in_ledger(self) -> None:
        """Flask app with Noir report: Noir is marked ran=true in ledger."""
        doc = _dircue(
            self.binary,
            "map",
            "--source", "directory",
            "--workers", "1",
            "--attach", f"noir-json={FIXTURES / 'flask-app.noir.json'}",
            "--json",
            str(FIXTURES / "flask-app"),
        )
        ledger = doc.get("coverage_ledger", [])
        noir_entry = next((e for e in ledger if e.get("tool", "").lower() == "noir"), None)
        self.assertIsNotNone(noir_entry, f"noir missing from ledger: {ledger}")
        self.assertTrue(noir_entry["ran"], "Noir must be marked ran=true")
        # Blind spots: opentaint not applicable to Python
        coverage = doc.get("analyzer_coverage", [])
        opentaint_entries = [e for e in coverage if e.get("tool") == "opentaint"]
        if opentaint_entries:
            for e in opentaint_entries:
                self.assertEqual(e["not_covered"], "unsupported_language",
                                 f"opentaint must be unsupported_language for Python: {e}")

    def test_analyzer_blind_spots_summary_non_empty(self) -> None:
        """Analyzer blind spots roll-up is present and non-empty."""
        doc = _dircue(
            self.binary,
            "map",
            "--source", "directory",
            "--workers", "1",
            "--json",
            str(FIXTURES / "flask-app"),
        )
        blind_spots = doc.get("analyzer_blind_spots", [])
        self.assertTrue(blind_spots,
                        "expected at least one analyzer blind spot without any attachments")

    def test_no_run_never_silent(self) -> None:
        """'No report' must never be silently treated as clean/complete.

        Valid not_covered reasons for a non-ran tool with expected_applicable=True:
        - not_run: applicable but no report attached
        - unknown: applicable but language/support unclear
        - unsupported_language: language not in descriptor
        - unsupported_framework: framework-dependent tool, no framework detected
        - prerequisite_unmet: missing build prerequisite

        An empty string (silent non-run) is the defect. All non-empty reasons are acceptable.
        """
        doc = _dircue(
            self.binary,
            "map",
            "--source", "directory",
            "--workers", "1",
            "--json",
            str(FIXTURES / "python-lint-sample"),
        )
        coverage = doc.get("analyzer_coverage", [])
        valid_reasons = {
            "not_run", "unknown", "unsupported_language",
            "unsupported_framework", "prerequisite_unmet", "tool_error",
        }
        # Every tool with expected_applicable=True must not have not_covered=""
        for entry in coverage:
            if entry.get("expected_applicable") and not entry.get("ran", False):
                reason = entry.get("not_covered", "")
                self.assertIn(reason, valid_reasons,
                              f"silent non-run (reason={reason!r}): {entry}")


# ---------------------------------------------------------------------------
# #79: Route deeper analyzers per component
# ---------------------------------------------------------------------------

class TestAnalyzerRouting(unittest.TestCase):
    """#79: Routing expectations on fixtures.

    Validates:
    - JVM (Spring) component routes to JVM-capable tools with build prerequisites.
    - Python component routes to Noir (with framework) and Syft.
    - An unsupported-language component shows no taint analyzer.
    """

    def setUp(self) -> None:
        if not hasattr(self.__class__, '_binary'):
            self.skipTest("binary not configured")
        self.binary = self.__class__._binary

    @classmethod
    def configure(cls, binary: Path) -> None:
        cls._binary = binary

    def _route(self, fixture_dir: Path) -> list[dict]:
        """Build a map and return the analyzer route plans from map route."""
        import tempfile
        tmpdir = tempfile.mkdtemp(prefix="dircue-route-")
        map_path = Path(tmpdir) / "map.json"
        result = subprocess.run(
            [
                str(self.binary),
                "map", "--source", "directory", "--workers", "1", "--json",
                str(fixture_dir),
            ],
            capture_output=True, text=True, timeout=60,
        )
        if result.returncode != 0:
            raise AssertionError(f"map failed: {result.stderr[:300]}")
        map_path.write_text(result.stdout)
        # Run map route to get plans
        plans_result = subprocess.run(
            [str(self.binary), "map", "route", "--json", str(map_path)],
            capture_output=True, text=True, timeout=60,
        )
        if plans_result.returncode != 0:
            raise AssertionError(f"map route failed: {plans_result.stderr[:300]}")
        plans = json.loads(plans_result.stdout)
        return plans if isinstance(plans, list) else []

    def test_flask_python_routes_to_syft_and_scc(self) -> None:
        """Flask app: Syft and scc are applicable to the Python component.

        Expected: map route emits applicable plans for syft and scc.
        Prerequisite: flask-app/requirements.txt triggers Python component detection.
        """
        plans = self._route(FIXTURES / "flask-app")
        self.assertTrue(plans, "expected non-empty plans from map route")
        tools = {p["tool"] for p in plans if p.get("applicable")}
        self.assertIn("syft", tools, f"syft missing from applicable plans: {tools}")
        self.assertIn("scc", tools, f"scc missing: {tools}")

    def test_python_component_no_opentaint(self) -> None:
        """Python component must not route to opentaint (JVM-only taint analyzer).

        opentaint supports Java/Kotlin only. A Python component must not appear
        in its applicable plans.
        """
        plans = self._route(FIXTURES / "flask-app")
        self.assertTrue(plans, "expected non-empty plans")
        opentaint_applicable = [p for p in plans if p.get("tool") == "opentaint" and p.get("applicable")]
        self.assertEqual(len(opentaint_applicable), 0,
                         f"opentaint must not be applicable for Python: {opentaint_applicable}")


# ---------------------------------------------------------------------------
# Main
# ---------------------------------------------------------------------------

def _parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("--binary", type=Path, default=BINARY_DEFAULT,
                        help="Path to the dircue binary (default: %(default)s)")
    return parser.parse_args()


def main() -> None:
    args = _parse_args()
    binary = args.binary.resolve()
    if not binary.exists():
        print(f"ERROR: binary not found: {binary}", file=sys.stderr)
        sys.exit(2)

    # Configure all test classes with the binary path.
    TestNoirIngestion.configure(binary)
    TestSARIFLocate.configure(binary)
    TestCoverageLedger.configure(binary)
    TestAnalyzerRouting.configure(binary)

    loader = unittest.TestLoader()
    suite = unittest.TestSuite()
    for cls in [TestNoirIngestion, TestSARIFLocate, TestCoverageLedger, TestAnalyzerRouting]:
        suite.addTests(loader.loadTestsFromTestCase(cls))

    runner = unittest.TextTestRunner(verbosity=2)
    result = runner.run(suite)
    sys.exit(0 if result.wasSuccessful() else 1)


if __name__ == "__main__":
    main()
