import contextlib
import importlib.util
import io
from pathlib import Path
import subprocess
import sys
import tempfile
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
            self.assertTrue(hasattr(kwargs["stdout"], "write"))
            self.assertEqual(kwargs["stderr"], subprocess.STDOUT)
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

    def test_public_failures_remain_visible_and_private_urls_are_redacted(self):
        payload = ("go: example.org/pkg@v1.2.3: reading https://proxy.golang.org/example.org/pkg/@v/v1.2.3.zip: 404 Not Found\n"
                   "https://username:password@example.invalid/private/token?api=secret#fragment\n"
                   "https://github.com/public/repo?token=query-secret\n"
                   "https://example.invalid/private-secret\n")
        log = io.StringIO()
        def failing(command, **kwargs):
            kwargs["stdout"].write(payload.encode())
            return subprocess.CompletedProcess(command, 1)
        with contextlib.redirect_stderr(log):
            self.assertEqual(downloader.download(run=failing, sleep=lambda _: None), 1)
        result = log.getvalue()
        self.assertIn("404 Not Found", result)
        self.assertIn("example.org/pkg/@v/v1.2.3.zip", result)
        for secret in ("username", "password", "private/token", "query-secret", "private-secret", "fragment"):
            self.assertNotIn(secret, result)

    def test_excerpt_bounds_and_partial_url_credentials(self):
        log = io.StringIO()
        with tempfile.TemporaryFile() as capture, contextlib.redirect_stderr(log):
            capture.write(b"Useful first line\n" + b"https://" + b"secret" * 20000 + b":password@example.invalid\n")
            downloader.show_diagnostics(capture)
        self.assertIn("Useful first line", log.getvalue())
        self.assertIn("truncated", log.getvalue())
        self.assertNotIn("secret", log.getvalue())
        self.assertLess(len(log.getvalue()), downloader.MAX_DIAGNOSTIC_BYTES + 100)

    def test_truncated_single_line_error_keeps_context_but_drops_partial_url(self):
        log = io.StringIO()
        with tempfile.TemporaryFile() as capture, contextlib.redirect_stderr(log):
            capture.write(b"proxy request failed: " + b"x" * 100 + b" https://user:secret@private.invalid/" + b"z" * 70000)
            downloader.show_diagnostics(capture)
        output = log.getvalue()
        self.assertIn("proxy request failed:", output)
        self.assertIn("truncated", output)
        self.assertNotIn("user", output)
        self.assertNotIn("secret", output)

    def test_truncated_single_line_without_url_keeps_bounded_error(self):
        log = io.StringIO()
        with tempfile.TemporaryFile() as capture, contextlib.redirect_stderr(log):
            capture.write(b"module proxy unavailable: " + b"x" * 70000)
            downloader.show_diagnostics(capture)
        output = log.getvalue()
        self.assertIn("module proxy unavailable:", output)
        self.assertIn("truncated", output)
        self.assertLess(len(output), downloader.MAX_DIAGNOSTIC_BYTES + 100)

    def test_url_redaction_handles_tricky_authorities_and_public_query_tokens(self):
        output = downloader.safe_diagnostics(
            "https://proxy.golang.org.evil.invalid/private "
            "https://name:pass@proxy.golang.org/pkg?token=hidden#part "
            "https://proxy.golang.org/pkg?token=hidden#part "
            "https://[::1/private "
        )
        self.assertNotIn("proxy.golang.org.evil.invalid/private", output)
        self.assertNotIn("name", output)
        self.assertNotIn("pass", output)
        self.assertNotIn("hidden", output)
        self.assertNotIn("part", output)
        self.assertIn("https://proxy.golang.org/pkg", output)

    def test_controls_cannot_escape_terminal(self):
        self.assertEqual(downloader.safe_diagnostics("bad\x1b[31m\x00\r\n"), "bad\\x1b[31m\\x00\\x0d\n")

    def test_real_child_output_cannot_disclose_proxy_credentials(self):
        canary = "https://proxy-user-canary:proxy-password-canary@example.invalid"
        def noisy_child(command, **kwargs):
            return subprocess.run([sys.executable, "-c",
                                   "import sys; print(sys.argv[1]); print(sys.argv[1], file=sys.stderr); sys.exit(1)",
                                   canary], **kwargs)
        log = io.StringIO()
        with contextlib.redirect_stderr(log):
            code = downloader.download(run=noisy_child, sleep=lambda _: None)
        self.assertEqual(code, 1)
        self.assertNotIn("proxy-user-canary", log.getvalue())
        self.assertNotIn("proxy-password-canary", log.getvalue())


if __name__ == "__main__":
    unittest.main()
