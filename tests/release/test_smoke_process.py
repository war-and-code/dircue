"""Real subprocess checks for timeout cleanup and captured diagnostics."""

import os
from pathlib import Path
import signal
import subprocess
import sys
import tempfile
import time
import unittest
from unittest.mock import Mock, patch

sys.path.insert(0, str(Path(__file__).resolve().parents[2] / "scripts"))
import smoke_process


class SmokeProcessTests(unittest.TestCase):
    def test_interruption_cleans_up_before_reraising(self):
        process = Mock(pid=12345)
        process.communicate.side_effect = [KeyboardInterrupt, (b"partial", b"")]
        process.poll.return_value = None
        with patch("smoke_process.subprocess.Popen", return_value=process), \
             patch("smoke_process.os.killpg", create=True) as kill_group, \
             patch("smoke_process.subprocess.run") as taskkill:
            with self.assertRaises(KeyboardInterrupt):
                smoke_process.run(["command"], timeout=1, capture_output=True)
            process.kill.assert_called_once()
            process.wait.assert_called_once_with(timeout=5)
            if os.name == "nt":
                taskkill.assert_called_once()
            else:
                kill_group.assert_called_once_with(12345, signal.SIGKILL)

    def test_output_exit_and_utf8_match_run_contract(self):
        command = [sys.executable, "-c", "import sys;print('雪');print('detail',file=sys.stderr);sys.exit(3)"]
        environment = {**os.environ, "PYTHONIOENCODING": "utf-8"}
        result = smoke_process.run(command, timeout=10, capture_output=True,
                                   text=True, encoding="utf-8", check=False, env=environment)
        self.assertEqual((result.returncode, result.stdout, result.stderr), (3, "雪\n", "detail\n"))
        with self.assertRaises(subprocess.CalledProcessError) as raised:
            smoke_process.run(command, timeout=10, capture_output=True, check=True, env=environment)
        self.assertEqual(raised.exception.returncode, 3)
        self.assertEqual(raised.exception.stdout.replace(b"\r\n", b"\n"), "雪\n".encode())

    def test_devnull_and_partial_timeout_diagnostics(self):
        result = smoke_process.run([sys.executable, "-c", "print('discarded')"],
                                   timeout=10, stdout=subprocess.DEVNULL, stderr=subprocess.PIPE)
        self.assertIsNone(result.stdout)
        self.assertEqual(result.stderr, b"")
        command = [sys.executable, "-c", "import sys,time;print('before timeout',flush=True);time.sleep(30)"]
        with self.assertRaises(subprocess.TimeoutExpired) as raised:
            smoke_process.run(command, timeout=1, capture_output=True)
        self.assertEqual(raised.exception.stdout.replace(b"\r\n", b"\n"), b"before timeout\n")

    def test_early_parent_exit_does_not_hide_a_childs_output_or_timeout(self):
        child = "import time;time.sleep(.4);print('child output',flush=True)"
        parent = (
            "import subprocess,sys;subprocess.Popen([sys.executable,'-c'," + repr(child) + "]); "
            "print('parent output',flush=True)"
        )
        command = [sys.executable, "-c", parent]
        result = smoke_process.run(command, timeout=10, capture_output=True)
        self.assertEqual(result.returncode, 0)
        self.assertEqual(result.stdout.replace(b"\r\n", b"\n"), b"parent output\nchild output\n")
        with self.assertRaises(subprocess.TimeoutExpired):
            smoke_process.run(command, timeout=.1, capture_output=True)

    def test_incomplete_utf8_during_cleanup_preserves_timeout_and_raw_bytes(self):
        command = [sys.executable, "-c", "import sys,time;sys.stdout.buffer.write(b'partial\\xe9');sys.stdout.flush();time.sleep(30)"]
        with self.assertRaises(subprocess.TimeoutExpired) as raised:
            smoke_process.run(command, timeout=1, capture_output=True, text=True, encoding="utf-8")
        self.assertEqual(raised.exception.stdout, b"partial\xe9")

    def test_timeout_stops_a_descendant_that_inherits_output_handles(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            heartbeat, pid_file = root / "heartbeat", root / "child.pid"
            child = (
                "import pathlib,time; p=pathlib.Path(" + repr(str(heartbeat)) + "); "
                "\nwhile True:\n p.write_text(str(time.time_ns())); time.sleep(.02)\n"
            )
            parent = (
                "import pathlib,subprocess,sys,time; "
                "child=subprocess.Popen([sys.executable,'-c'," + repr(child) + "]); "
                "pathlib.Path(" + repr(str(pid_file)) + ").write_text(str(child.pid)); "
                "print('parent ready',flush=True); time.sleep(30)"
            )
            try:
                started = time.monotonic()
                with self.assertRaises(subprocess.TimeoutExpired) as raised:
                    smoke_process.run([sys.executable, "-c", parent], timeout=2, capture_output=True)
                self.assertLess(time.monotonic() - started, 10)
                self.assertIn(b"parent ready", raised.exception.stdout)
                self.assertTrue(pid_file.is_file())
                self.assertTrue(heartbeat.is_file())
                time.sleep(.1)
                stopped = heartbeat.read_bytes()
                time.sleep(.2)
                self.assertEqual(stopped, heartbeat.read_bytes(), "timed-out descendant is still active")
            finally:
                # Keep a failed mutation/reproduction from leaving a process.
                if pid_file.is_file():
                    pid = int(pid_file.read_text())
                    if os.name == "nt":
                        subprocess.run(["taskkill", "/PID", str(pid), "/T", "/F"],
                                       stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, timeout=5)
                    else:
                        try:
                            os.kill(pid, signal.SIGKILL)
                        except ProcessLookupError:
                            pass


if __name__ == "__main__":
    unittest.main()
