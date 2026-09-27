"""Unit tests for verify_corpus_availability.py — no network, no real binary."""

import importlib.util
import json
import os
import stat
import sys
import tempfile
import textwrap
import unittest
from pathlib import Path

SCRIPT = Path(__file__).with_name("verify_corpus_availability.py")
SPEC = importlib.util.spec_from_file_location("verify_corpus_availability", SCRIPT)
avail = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(avail)


def _fake_binary(tmpdir: Path, exit_code: int, stdout: str, stderr: str = "") -> Path:
    """Write a tiny shell script that acts as a fake dircue binary."""
    script = tmpdir / "fake_dircue"
    escaped_stdout = stdout.replace("'", "'\\''")
    escaped_stderr = stderr.replace("'", "'\\''")
    script.write_text(
        textwrap.dedent(f"""\
        #!/bin/sh
        echo '{escaped_stdout}'
        echo '{escaped_stderr}' >&2
        exit {exit_code}
        """)
    )
    script.chmod(script.stat().st_mode | stat.S_IEXEC)
    return script


def _valid_map_json(schema_version: str = "1.0.0") -> str:
    doc = {
        "schema_version": schema_version,
        "kind": "map",
        "status": "partial",
        "coverage": [],
        "nodes": [],
        "edges": [],
    }
    return json.dumps(doc)


def _make_repos(root: Path, names):
    """Create empty subdirectories to stand in for repo clones."""
    for name in names:
        (root / name).mkdir(parents=True, exist_ok=True)


class RunMapTests(unittest.TestCase):
    def setUp(self):
        self._tmp = tempfile.TemporaryDirectory()
        self.tmp = Path(self._tmp.name)

    def tearDown(self):
        self._tmp.cleanup()

    def test_exit_0_valid_json_passes(self):
        binary = _fake_binary(self.tmp, 0, _valid_map_json())
        repo = self.tmp / "myrepo"
        repo.mkdir()
        result = avail.run_map(binary, repo)
        self.assertEqual(result["exit_code"], 0)
        self.assertTrue(result["schema_ok"])
        self.assertEqual(result["error"], "")

    def test_exit_1_fails(self):
        binary = _fake_binary(self.tmp, 1, "", "Error: something went wrong")
        repo = self.tmp / "badrepo"
        repo.mkdir()
        result = avail.run_map(binary, repo)
        self.assertEqual(result["exit_code"], 1)
        self.assertFalse(result["schema_ok"])
        self.assertIn("exit 1", result["error"])

    def test_exit_0_non_json_stdout_fails(self):
        binary = _fake_binary(self.tmp, 0, "not json at all")
        repo = self.tmp / "repo"
        repo.mkdir()
        result = avail.run_map(binary, repo)
        self.assertEqual(result["exit_code"], 0)
        self.assertFalse(result["schema_ok"])
        self.assertIn("not valid JSON", result["error"])

    def test_exit_0_wrong_schema_version_fails(self):
        binary = _fake_binary(self.tmp, 0, _valid_map_json("0.9.0"))
        repo = self.tmp / "repo"
        repo.mkdir()
        result = avail.run_map(binary, repo)
        self.assertFalse(result["schema_ok"])
        self.assertIn("schema_version", result["error"])

    def test_result_includes_required_fields(self):
        binary = _fake_binary(self.tmp, 0, _valid_map_json())
        repo = self.tmp / "myrepo"
        repo.mkdir()
        result = avail.run_map(binary, repo)
        for field in ("repo", "path", "exit_code", "duration_s", "first_stderr", "schema_ok", "error"):
            self.assertIn(field, result, f"missing field {field!r}")

    def test_first_stderr_captured(self):
        binary = _fake_binary(self.tmp, 1, "", "Error: something specific")
        repo = self.tmp / "repo"
        repo.mkdir()
        result = avail.run_map(binary, repo)
        self.assertIn("Error", result["first_stderr"])


class CheckCorpusRootTests(unittest.TestCase):
    def setUp(self):
        self._tmp = tempfile.TemporaryDirectory()
        self.tmp = Path(self._tmp.name)

    def tearDown(self):
        self._tmp.cleanup()

    def test_all_pass(self):
        root = self.tmp / "corpus"
        root.mkdir()
        binary = _fake_binary(self.tmp, 0, _valid_map_json())
        _make_repos(root, ["repo-a", "repo-b", "repo-c"])
        results = avail.check_corpus_root(binary, root)
        self.assertEqual(len(results), 3)
        self.assertTrue(all(r["schema_ok"] for r in results))

    def test_one_failure_detected(self):
        root = self.tmp / "corpus"
        root.mkdir()
        # Binary exits 1; all repos fail.
        binary = _fake_binary(self.tmp, 1, "", "Error: doc-path bug")
        _make_repos(root, ["good", "bad"])
        results = avail.check_corpus_root(binary, root)
        failed = [r for r in results if not r["schema_ok"]]
        self.assertEqual(len(failed), 2)

    def test_repos_are_sorted(self):
        root = self.tmp / "corpus"
        root.mkdir()
        binary = _fake_binary(self.tmp, 0, _valid_map_json())
        _make_repos(root, ["zebra", "alpha", "middle"])
        results = avail.check_corpus_root(binary, root)
        names = [r["repo"] for r in results]
        self.assertEqual(names, sorted(names))


class SummariseTests(unittest.TestCase):
    def _result(self, ok: bool, repo: str = "repo") -> dict:
        return {
            "repo": repo,
            "path": f"/tmp/{repo}",
            "exit_code": 0 if ok else 1,
            "duration_s": 0.1,
            "first_stderr": "" if ok else "Error: something",
            "schema_ok": ok,
            "error": "" if ok else "exit 1: Error: something",
        }

    def test_all_pass(self):
        results = [self._result(True, f"r{i}") for i in range(5)]
        passed, failed, summary = avail.summarise(results)
        self.assertEqual(len(passed), 5)
        self.assertEqual(len(failed), 0)
        self.assertEqual(summary["passed"], 5)
        self.assertEqual(summary["failed"], 0)

    def test_one_failure(self):
        results = [self._result(True), self._result(False, "bad")]
        _, failed, summary = avail.summarise(results)
        self.assertEqual(len(failed), 1)
        self.assertEqual(summary["failed"], 1)


if __name__ == "__main__":
    unittest.main()
