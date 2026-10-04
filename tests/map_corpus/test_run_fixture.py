import importlib.util
from pathlib import Path
import subprocess
import unittest
from unittest.mock import patch


SPEC = importlib.util.spec_from_file_location("fixture_runner", Path(__file__).with_name("run.py"))
RUNNER = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(RUNNER)


class FixtureRunTests(unittest.TestCase):
    def test_directory_selection_and_process_timeout_are_explicit(self):
        result = subprocess.CompletedProcess([], 0, b'{"nodes": []}\n', b'')
        with patch.object(RUNNER.subprocess, "run", return_value=result) as execute:
            document, stdout, stderr = RUNNER.run_map(Path("dircue"), Path("fixture"), timeout=7)
        self.assertEqual(document, {"nodes": []})
        self.assertEqual(stdout, result.stdout)
        self.assertEqual(stderr, result.stderr)
        self.assertEqual(execute.call_args.args[0], ["dircue", "map", "--source", "directory", "--json", "fixture"])
        self.assertEqual(execute.call_args.kwargs["timeout"], 7)

    def test_timeout_failure_names_the_input(self):
        with patch.object(RUNNER.subprocess, "run", side_effect=subprocess.TimeoutExpired(["dircue"], 7)):
            with self.assertRaisesRegex(ValueError, "fixture: map exceeded 7s"):
                RUNNER.run_map(Path("dircue"), Path("fixture"), timeout=7)

    def test_process_and_json_failures_are_not_successful_documents(self):
        for result, message in [
            (subprocess.CompletedProcess([], 1, b'{}', b'fixture broken'), 'fixture broken'),
            (subprocess.CompletedProcess([], 0, b'not json', b''), 'did not emit JSON'),
            (subprocess.CompletedProcess([], 0, b'\xff', b''), 'did not emit JSON'),
        ]:
            with self.subTest(message=message), patch.object(RUNNER.subprocess, "run", return_value=result):
                with self.assertRaisesRegex(ValueError, message):
                    RUNNER.run_map(Path("dircue"), Path("fixture"))
