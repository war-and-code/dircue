"""Manual fuzz jobs validate input and retain actual failure reproducers."""

import importlib.util
from pathlib import Path
import unittest
from unittest.mock import patch


ROOT = Path(__file__).resolve().parents[2]
SPEC = importlib.util.spec_from_file_location("fuzz_workflow", ROOT / "scripts/run_fuzz_workflow.py")
workflow = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(workflow)


class FuzzWorkflowTests(unittest.TestCase):
    def test_valid_input_preserves_literal_arguments(self):
        result = workflow.command({"FUZZ_SECONDS_INPUT": "30", "FUZZ_PACKAGES_INPUT": "./pkg/mapdiff ./pkg/deployables/...", "RUNNER_TEMP": "/tmp/runner with spaces"})
        cache = str(Path("/tmp/runner with spaces").resolve() / "fuzz-cache")
        self.assertEqual(result, ["make", "fuzz-campaign", "FUZZ_TIME=30", "FUZZ_PKG=./pkg/mapdiff ./pkg/deployables/...", "FUZZ_CACHE=" + cache])

    def test_unsafe_or_invalid_inputs_never_start_make(self):
        for seconds, packages in (("1; touch UNEXPECTED", "./..."), ("0", "./..."), ("1801", "./..."), ("10", "$(touch UNEXPECTED)"), ("10", "./pkg/../../outside"), ("10", "-C outside"), ("10", "")):
            with self.subTest(seconds=seconds, packages=packages):
                environment = {"FUZZ_SECONDS_INPUT": seconds, "FUZZ_PACKAGES_INPUT": packages, "RUNNER_TEMP": "/tmp"}
                with patch.dict(workflow.os.environ, environment, clear=True), patch.object(workflow.subprocess, "run") as run:
                    with self.assertRaises(ValueError):
                        workflow.main()
                    run.assert_not_called()

    def test_workflow_retains_crashers_and_does_not_embed_inputs_in_shell(self):
        text = (ROOT / ".github/workflows/fuzz-campaign.yml").read_text()
        self.assertIn("run: python3 scripts/run_fuzz_workflow.py", text)
        self.assertIn("**/testdata/fuzz/**", text)
        self.assertNotIn('FUZZ_TIME="${{', text)
        self.assertNotIn("|| true", text)


if __name__ == "__main__":
    unittest.main()
