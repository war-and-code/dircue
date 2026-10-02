import contextlib
import importlib.util
import io
from pathlib import Path
import subprocess
import unittest


spec = importlib.util.spec_from_file_location(
    "download_go_modules", Path(__file__).resolve().parents[2] / "scripts/download_go_modules.py"
)
downloader = importlib.util.module_from_spec(spec)
spec.loader.exec_module(downloader)


class GoDownloadRetryTests(unittest.TestCase):
    def invoke(self, outcomes):
        calls, sleeps = [], []
        def run(command, **kwargs):
            calls.append((command, kwargs))
            outcome = outcomes[len(calls) - 1]
            if isinstance(outcome, Exception):
                raise outcome
            return subprocess.CompletedProcess(command, outcome)
        log = io.StringIO()
        with contextlib.redirect_stderr(log):
            code = downloader.download(run=run, sleep=sleeps.append)
        return code, calls, sleeps, log.getvalue()

    def test_transient_failure_recovers_without_replacing_pinned_inputs(self):
        code, calls, sleeps, log = self.invoke([1, 0])
        self.assertEqual(code, 0)
        self.assertEqual(len(calls), 2)
        self.assertEqual(sleeps, [2])
        for command, kwargs in calls:
            self.assertEqual(command, ["go", "mod", "download"])
            self.assertEqual(kwargs["env"]["GOFLAGS"], "-mod=readonly")
            self.assertEqual(kwargs["timeout"], 180)
        self.assertIn("attempt 2/3", log)

    def test_permanent_failure_stops_and_fails(self):
        code, calls, sleeps, log = self.invoke([1, 1, 1])
        self.assertEqual(code, 1)
        self.assertEqual(len(calls), 3)
        self.assertEqual(sleeps, [2, 4])
        self.assertIn("failed after three attempts", log)

    def test_timeout_is_bounded_and_can_recover(self):
        code, calls, sleeps, log = self.invoke([subprocess.TimeoutExpired("go", 180), 0])
        self.assertEqual(code, 0)
        self.assertEqual(len(calls), 2)
        self.assertIn("exceeded 180 seconds", log)

    def test_warm_cache_does_not_retry(self):
        code, calls, sleeps, _ = self.invoke([0])
        self.assertEqual((code, len(calls), sleeps), (0, 1, []))


if __name__ == "__main__":
    unittest.main()
