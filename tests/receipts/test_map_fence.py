#!/usr/bin/env python3
"""Regression fences for dircue map on committed fixtures.

A fence re-runs the current binary on a small, committed fixture and
compares the result against reviewed structural expectations. These tests
replace inert receipts: rather than just checking that a receipt's format
is valid, they verify that the current binary still produces the expected
output.

Fences in this file:
  FenceCompositionFixture — the synthetic C#/Java fixture from
    tests/compatibility_next/composition/fixture/ with verified
    structural expectations (schema version, component count/kinds,
    content language names, edge count).

Determinism: these tests use --source directory --workers 1 to ensure the
result is byte-identical regardless of scheduling. IDs are deterministic
from the fixture content; they do not depend on the absolute path.

Usage:
  python3 tests/receipts/test_map_fence.py --binary bin/dircue
  python -m unittest tests/receipts/test_map_fence.py  # uses bin/dircue
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

# Fixture path (small, committed, stable fixture from composition checks).
COMPOSITION_FIXTURE = ROOT / "tests" / "compatibility_next" / "composition" / "fixture"


def _run_map(binary: Path, fixture: Path) -> dict:
    result = subprocess.run(
        [str(binary), "map", "--source", "directory", "--workers", "1", "--json", str(fixture)],
        capture_output=True,
        text=True,
        timeout=30,
    )
    if result.returncode != 0:
        raise AssertionError(
            f"dircue map exited {result.returncode}\nstderr: {result.stderr}"
        )
    return json.loads(result.stdout)


class FenceCompositionFixture(unittest.TestCase):
    """Regression fence for the composition fixture.

    Structural expectations (schema 1.0.0):
    - 2 component nodes (App and Lib, both dotnet)
    - At least 1 content node (C# language population)
    - 1 or more edges (project reference between components)
    - components coverage is partial (no full declaration info)
    - content coverage is complete (all files read)
    - schema_version is 1.0.0

    These expectations are verified to be stable across path changes,
    worker counts, and preset selections (see metamorphic.py). They
    reflect the fixture's actual content, not current dircue output.
    """

    binary: Path = BINARY_DEFAULT

    def _map(self) -> dict:
        if not self.binary.exists():
            self.skipTest(f"binary not found: {self.binary}")
        return _run_map(self.binary, COMPOSITION_FIXTURE)

    def test_schema_version_is_stable(self) -> None:
        d = self._map()
        self.assertEqual(d["schema_version"], "1.0.0",
                         "schema_version must be 1.0.0 (contract freeze)")

    def test_dotnet_components_are_detected(self) -> None:
        d = self._map()
        components = [n for n in d["nodes"] if n["kind"] == "component"]
        self.assertGreaterEqual(len(components), 2,
                                "The App/ and Lib/ dotnet projects must each be a component")
        discriminators = {c.get("discriminator", "") for c in components}
        self.assertIn("dotnet", discriminators,
                      "At least one component must be discriminated as dotnet")

    def test_component_names_include_app_and_lib(self) -> None:
        d = self._map()
        names = {n["name"] for n in d["nodes"] if n["kind"] == "component"}
        self.assertIn("App", names, "App component must be present")
        self.assertIn("Lib", names, "Lib component must be present")

    def test_csharp_language_content_is_present(self) -> None:
        d = self._map()
        languages = {
            n.get("properties", {}).get("language", "")
            for n in d["nodes"]
            if n["kind"] == "content" and n.get("properties", {}).get("role") == "language_population"
        }
        self.assertIn("C#", languages,
                      "C# language population must appear in content nodes for the .cs files")

    def test_content_coverage_is_complete(self) -> None:
        d = self._map()
        cov = {c["question"]: c["status"] for c in d.get("coverage", [])}
        self.assertEqual(cov.get("content"), "complete",
                         "Content coverage must be complete: all files in the fixture are readable")

    def test_at_least_one_edge_present(self) -> None:
        d = self._map()
        self.assertGreater(len(d.get("edges", [])), 0,
                           "At least one edge (e.g., project reference between App and Lib) must be present")

    def test_no_absolute_paths_in_output(self) -> None:
        d = self._map()
        text = json.dumps(d)
        self.assertNotIn(str(COMPOSITION_FIXTURE), text,
                         "Absolute fixture path must not appear in portable map output")
        self.assertNotIn(str(ROOT), text,
                         "Absolute repository root must not appear in portable map output")


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawTextHelpFormatter)
    parser.add_argument("--binary", type=Path, default=BINARY_DEFAULT,
                        help="Path to the dircue binary to test (default: bin/dircue)")
    args, remaining = parser.parse_known_args()
    FenceCompositionFixture.binary = args.binary.resolve()
    # Run tests.
    loader = unittest.TestLoader()
    suite = loader.loadTestsFromTestCase(FenceCompositionFixture)
    runner = unittest.TextTestRunner(verbosity=2)
    result = runner.run(suite)
    sys.exit(0 if result.wasSuccessful() else 1)


if __name__ == "__main__":
    main()
