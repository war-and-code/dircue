"""Integrity checks for source-first public holdouts."""

import hashlib
from pathlib import Path
import subprocess
import tempfile
import unittest

import run_holdout


class VerifySourceTest(unittest.TestCase):
    def test_rejects_modified_or_untracked_checkout(self):
        with tempfile.TemporaryDirectory() as temp:
            source = Path(temp)
            for args in (
                ("init", "-q"),
                ("config", "user.email", "holdout@example.invalid"),
                ("config", "user.name", "Holdout Test"),
            ):
                subprocess.run(("git", "-C", str(source), *args), check=True)
            oracle = source / "pom.xml"
            oracle.write_text("<project/>\n")
            subprocess.run(("git", "-C", str(source), "add", "pom.xml"), check=True)
            subprocess.run(("git", "-C", str(source), "commit", "-qm", "source"), check=True)
            commit = subprocess.check_output(("git", "-C", str(source), "rev-parse", "HEAD")).decode().strip()
            label = {
                "repo": "test",
                "commit": commit,
                "oracle_files": [{
                    "path": "pom.xml",
                    "sha256": hashlib.sha256(oracle.read_bytes()).hexdigest(),
                }],
            }
            run_holdout.verify_source(source, label)

            oracle.write_text("<changed/>\n")
            with self.assertRaisesRegex(ValueError, "modified or untracked"):
                run_holdout.verify_source(source, label)
            oracle.write_text("<project/>\n")
            (source / "extra.txt").write_text("surprise\n")
            with self.assertRaisesRegex(ValueError, "modified or untracked"):
                run_holdout.verify_source(source, label)


if __name__ == "__main__":
    unittest.main()
