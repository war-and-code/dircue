from __future__ import annotations

import importlib.util
from pathlib import Path
import tempfile
import unittest


SCRIPT = Path(__file__).with_name("topology.py")
SPEC = importlib.util.spec_from_file_location("topology_mutation_gate", SCRIPT)
assert SPEC is not None and SPEC.loader is not None
topology = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(topology)


class TopologyMutationGateTests(unittest.TestCase):
    def test_catalog_has_unique_exact_operators_for_all_topology_areas(self):
        mutations = topology.validate_catalog()
        self.assertGreaterEqual(len(mutations), 8)
        self.assertEqual(len({mutation["name"] for mutation in mutations}), len(mutations))
        self.assertEqual(
            {Path(mutation["source"]).name for mutation in mutations},
            {"procfile.go", "procfile_edges.go", "build.go", "aspire.go"},
        )

    def test_only_the_named_test_assertion_counts_as_a_kill(self):
        compile_ok = {"returncode": 0, "timed_out": False}
        named_failure = {
            "returncode": 1,
            "timed_out": False,
            "stdout": "--- FAIL: TestExpected (0.00s)\n    file_test.go:12: invariant broken\nFAIL\n",
            "stderr": "",
        }
        self.assertEqual(topology.classify_mutant(compile_ok, named_failure, "TestExpected")[0], "killed")

    def test_recovered_panic_reported_by_test_assertion_is_a_kill(self):
        compile_ok = {"returncode": 0, "timed_out": False}
        named_failure = {
            "returncode": 1,
            "timed_out": False,
            "stdout": "--- FAIL: TestExpected (0.00s)\n    test.go:12: recovered parser panic: runtime error: index out of range\nFAIL\n",
            "stderr": "",
        }
        self.assertEqual(topology.classify_mutant(compile_ok, named_failure, "TestExpected")[0], "killed")

    def test_compile_failure_is_invalid_not_killed(self):
        compile_bad = {"returncode": 1, "timed_out": False, "stdout": "FAIL [build failed]", "stderr": ""}
        self.assertEqual(topology.classify_mutant(compile_bad, None, "TestExpected")[0], "invalid")

    def test_timeout_survived_and_infrastructure_are_distinct(self):
        compile_ok = {"returncode": 0, "timed_out": False}
        timed_out = {"returncode": None, "timed_out": True, "stdout": "", "stderr": ""}
        passed = {"returncode": 0, "timed_out": False, "stdout": "ok\n", "stderr": ""}
        infrastructure = {"returncode": 2, "timed_out": False, "stdout": "panic: unrelated\n", "stderr": ""}
        self.assertEqual(topology.classify_mutant(compile_ok, timed_out, "TestExpected")[0], "timeout")
        self.assertEqual(topology.classify_mutant(compile_ok, passed, "TestExpected")[0], "survived")
        self.assertEqual(topology.classify_mutant(compile_ok, infrastructure, "TestExpected")[0], "error")

    def test_unrecovered_panic_is_not_misreported_as_an_assertion_kill(self):
        compile_ok = {"returncode": 0, "timed_out": False}
        panic = {
            "returncode": 1,
            "timed_out": False,
            "stdout": "--- FAIL: TestExpected (0.00s)\n",
            "stderr": "panic: index out of range [recovered]\n",
        }
        self.assertEqual(topology.classify_mutant(compile_ok, panic, "TestExpected")[0], "error")

    def test_receipt_destination_never_overwrites_existing_file(self):
        with tempfile.TemporaryDirectory() as temp_dir:
            output = Path(temp_dir) / "receipt.json"
            output.write_text("existing", encoding="utf-8")
            with self.assertRaises(FileExistsError):
                topology.make_output(str(output), "0123456789abcdef")
            self.assertEqual(output.read_text(encoding="utf-8"), "existing")


if __name__ == "__main__":
    unittest.main()
