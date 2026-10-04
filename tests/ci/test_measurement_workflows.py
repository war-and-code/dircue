"""Measurement jobs must preserve failures and keep inputs out of shell source."""

import os
from pathlib import Path
import re
import subprocess
import sys
import tempfile
import unittest


ROOT = Path(__file__).resolve().parents[2]
WORKFLOWS = ROOT / ".github/workflows"


class MeasurementWorkflowTests(unittest.TestCase):
    def test_manual_measurement_inputs_are_transported_through_environment(self):
        for name in ("perf-stable.yml", "mutation.yml"):
            text = (WORKFLOWS / name).read_text()
            run_sections = re.findall(r"(?ms)^        run: \|\n(.*?)(?=^      -|\Z)", text)
            self.assertTrue(run_sections)
            for script in run_sections:
                self.assertNotRegex(script, r"\$\{\{\s*inputs\.", name)
        self.assertNotIn("|| true", (WORKFLOWS / "mutation.yml").read_text())

    def test_full_ci_exercises_mutations_and_real_cli_benchmark(self):
        text = (WORKFLOWS / "ci.yml").read_text()
        self.assertIn("tests/mutation/topology.py --output", text)
        self.assertIn("python3 tests/bench/run.py --baseline", text)
        self.assertIn("-benchtime=1x", text)
        self.assertIn("discover -s tests/mutation", text)

    @unittest.skipIf(os.name == "nt", "the manually dispatched Linux job uses POSIX executable scripts")
    def test_mutation_tool_failures_or_missing_receipts_fail_the_job(self):
        for mode in ("success", "error", "missing"):
            with self.subTest(mode=mode):
                result, invoked = self.execute_mutation_step(mode=mode)
                self.assertEqual(invoked, True)
                self.assertEqual(result.returncode == 0, mode == "success", result.stderr)
                if mode == "error":
                    self.assertIn("deliberate tool failure", result.stderr)
                if mode != "success":
                    self.assertIn("failed or produced no receipt", result.stderr)

    @unittest.skipIf(os.name == "nt", "the manually dispatched Linux job uses POSIX executable scripts")
    def test_invalid_package_and_timeout_do_not_start_the_tool(self):
        for packages, coefficient in (
            ("./pkg/example; touch UNEXPECTED", "20"),
            ("./pkg/example/../../outside", "20"),
            ("./pkg/example", "20; touch UNEXPECTED"),
            ("", "20"),
        ):
            with self.subTest(packages=packages, coefficient=coefficient):
                result, invoked = self.execute_mutation_step(packages=packages, coefficient=coefficient)
                self.assertNotEqual(result.returncode, 0)
                self.assertFalse(invoked)

    def execute_mutation_step(self, mode="success", packages="./pkg/example", coefficient="20"):
        text = (WORKFLOWS / "mutation.yml").read_text()
        body = text.split("          python3 - <<'PY'\n", 1)[1].split("          PY\n", 1)[0]
        script = "\n".join(line[10:] for line in body.splitlines())
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            (root / "pkg/example").mkdir(parents=True)
            (root / "tests/mutation/run").mkdir(parents=True)
            (root / "gremlins").mkdir()
            fake = root / "gremlins/gremlins"
            fake.write_text(f"#!{sys.executable}\n" + '''import os, pathlib, sys
pathlib.Path("tool-called").touch()
if os.environ["FAKE_MODE"] == "error":
    print("deliberate tool failure", file=sys.stderr)
    sys.exit(7)
if os.environ["FAKE_MODE"] != "missing":
    pathlib.Path(sys.argv[sys.argv.index("-o") + 1]).write_text("{}")
''')
            fake.chmod(0o755)
            environment = dict(os.environ, RUNNER_TEMP=str(root), PACKAGES_INPUT=packages,
                               TIMEOUT_INPUT=coefficient, FAKE_MODE=mode)
            result = subprocess.run([sys.executable, "-c", script], cwd=root, env=environment,
                                    capture_output=True, text=True, timeout=10)
            return result, (root / "tool-called").exists()
