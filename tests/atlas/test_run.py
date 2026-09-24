"""Focused tests for runner evidence passed into discrepancy classification."""
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).parent))

from run import compare_scc, materialize_head_tree, probe_scc_skipped_paths


class TestCompareSccSkipEvidence(unittest.TestCase):
    def make_repo(self, root: Path, filename: str, contents: bytes) -> Path:
        root.mkdir()
        (root / filename).write_bytes(contents)
        subprocess.run(["git", "-C", str(root), "init", "--quiet"], check=True)
        subprocess.run(["git", "-C", str(root), "config", "user.email", "atlas-test@example.invalid"], check=True)
        subprocess.run(["git", "-C", str(root), "config", "user.name", "Atlas Test"], check=True)
        subprocess.run(["git", "-C", str(root), "add", filename], check=True)
        subprocess.run(["git", "-C", str(root), "commit", "--quiet", "-m", "fixture"], check=True)
        return root

    def compare_one_file(self, root: Path, path: str, registry: set[str]):
        return compare_scc(
            {"files": [{"path": path, "status": "counted", "language": "Autoconf"}]},
            [],
            [],
            registry,
            root,
        )

    def test_unregistered_extension_without_shebang_is_supported_as_no_language(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = self.make_repo(Path(tmp) / "repo", "configure.am", b"AC_INIT\n")
            result = self.compare_one_file(root, "configure.am", {"in"})
        self.assertEqual(result["skip_categories"]["dircue_only/scc_no_language"], 1)
        self.assertEqual(result["unexplained_count"], 0)

    def test_dotted_unregistered_extension_ignores_shebang_in_head(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = self.make_repo(Path(tmp) / "repo", "tool.unknown", b"#!/bin/bash\necho hi\n")
            result = self.compare_one_file(root, "tool.unknown", {"sh"})
        self.assertEqual(result["skip_categories"]["dircue_only/scc_no_language"], 1)
        self.assertEqual(result["unexplained_count"], 0)

    def test_extensionless_unregistered_shebang_stays_unexplained(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = self.make_repo(Path(tmp) / "repo", "tool", b"#!/bin/bash\necho hi\n")
            result = self.compare_one_file(root, "tool", {"sh"})
        self.assertEqual(result["unexplained_count"], 1)

    def test_supported_extension_without_scc_row_stays_unexplained(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = self.make_repo(Path(tmp) / "repo", "tool.go", b"package main\n")
            result = self.compare_one_file(root, "tool.go", {"go"})
        self.assertEqual(result["unexplained_count"], 1)

    def test_shebang_evidence_comes_from_head_not_mutable_worktree(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = self.make_repo(Path(tmp) / "repo", "configure.am", b"AC_INIT\n")
            # Worktree differs from HEAD; scc reads the immutable HEAD snapshot.
            (root / "configure.am").write_bytes(b"#!/bin/sh\nAC_INIT\n")
            result = self.compare_one_file(root, "configure.am", {"in"})
        self.assertEqual(result["skip_categories"]["dircue_only/scc_no_language"], 1)
        self.assertEqual(result["unexplained_count"], 0)

    def test_missing_registry_never_guesses_no_language(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = self.make_repo(Path(tmp) / "repo", "configure.am", b"AC_INIT\n")
            result = compare_scc(
                {"files": [{"path": "configure.am", "status": "counted", "language": "Autoconf"}]},
                [], [], None, root,
            )
        self.assertEqual(result["unexplained_count"], 1)

    def test_materialization_ignores_export_ignore_attribute(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp) / "repo"
            root.mkdir()
            (root / ".gitattributes").write_text("hidden.go export-ignore\n")
            (root / "hidden.go").write_text("package hidden\n")
            subprocess.run(["git", "-C", str(root), "init", "--quiet"], check=True)
            subprocess.run(["git", "-C", str(root), "config", "user.email", "atlas-test@example.invalid"], check=True)
            subprocess.run(["git", "-C", str(root), "config", "user.name", "Atlas Test"], check=True)
            subprocess.run(["git", "-C", str(root), "add", ".gitattributes", "hidden.go"], check=True)
            subprocess.run(["git", "-C", str(root), "commit", "--quiet", "-m", "fixture"], check=True)
            destination = Path(tmp) / "snapshot"
            materialize_head_tree(root, destination)
            self.assertEqual((destination / "hidden.go").read_text(), "package hidden\n")

    def test_probe_reports_paths_omitted_by_the_oracle(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = self.make_repo(Path(tmp) / "repo", "PKGBUILD", b"pkgname=fixture\n")
            fake_scc = Path(tmp) / "scc-empty"
            fake_scc.write_text("#!/usr/bin/env python3\nprint('[]')\n")
            fake_scc.chmod(0o755)
            skipped = probe_scc_skipped_paths(fake_scc, root, {"PKGBUILD"})
        self.assertEqual(skipped, {"PKGBUILD"})


if __name__ == "__main__":
    unittest.main()
