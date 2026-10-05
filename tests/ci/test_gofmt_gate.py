"""Run the local and CI formatting gates against isolated Git checkouts."""

import os
from pathlib import Path
import re
import shutil
import subprocess
import sys
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[2]


@unittest.skipUnless(os.name != "nt" and all(shutil.which(tool) for tool in ("make", "git", "gofmt", "bash")),
                     "the POSIX formatting gates need make, git, gofmt, and bash")
class GofmtGateTests(unittest.TestCase):
    def run_gate(self, root, gate, environment=None):
        if gate == "make":
            command = ["make", "fmt-check"]
        else:
            workflow = (ROOT / ".github/workflows/ci.yml").read_text()
            step = re.search(r"(?ms)^      - name: gofmt \(Linux only\)\n.*?        run: \|\n(.*?)(?=^      -|\Z)", workflow)
            self.assertIsNotNone(step, "Linux CI must retain a gofmt gate")
            script = "\n".join(line[10:] for line in step[1].splitlines())
            command = ["bash", "-c", script]
        return subprocess.run(command, cwd=root, env=environment, capture_output=True, text=True, timeout=10)

    def checkout(self, root):
        subprocess.run(["git", "init", "-q", str(root)], check=True, capture_output=True)
        shutil.copyfile(ROOT / "Makefile", root / "Makefile")

    def test_formatted_files_pass_and_excluded_trees_are_ignored(self):
        for gate in ("make", "ci"):
            with self.subTest(gate=gate), tempfile.TemporaryDirectory() as temporary:
                root = Path(temporary)
                self.checkout(root)
                (root / "formatted.go").write_text("package example\n")
                for name in ("third_party/dirty.go", "pkg/testdata/dirty.go", "tests/fixtures/dirty.go"):
                    path = root / name
                    path.parent.mkdir(parents=True, exist_ok=True)
                    path.write_text("not valid Go")
                result = self.run_gate(root, gate)
                self.assertEqual(result.returncode, 0, result.stderr)

    def test_unformatted_tracked_and_untracked_filenames_fail(self):
        for gate in ("make", "ci"):
            with self.subTest(gate=gate), tempfile.TemporaryDirectory() as temporary:
                root = Path(temporary)
                self.checkout(root)
                names = ("tracked with spaces.go", "untracked-服务\nfile.go")
                for name in names:
                    (root / name).write_text("package example\nfunc f( ){ }\n")
                subprocess.run(["git", "add", "--", names[0]], cwd=root, check=True, capture_output=True)
                result = self.run_gate(root, gate)
                self.assertNotEqual(result.returncode, 0)
                for name in names:
                    self.assertIn(name, result.stdout)

    def test_tool_errors_fail_even_without_unformatted_output(self):
        for gate in ("make", "ci"):
            for tool in ("git", "gofmt"):
                with self.subTest(gate=gate, tool=tool), tempfile.TemporaryDirectory() as temporary:
                    root = Path(temporary)
                    self.checkout(root)
                    (root / "formatted.go").write_text("package example\n")
                    fake_bin = root / "tools"
                    fake_bin.mkdir()
                    fake = fake_bin / tool
                    fake.write_text(f"#!{sys.executable}\nimport sys\nprint('deliberate {tool} failure', file=sys.stderr)\nsys.exit(7)\n")
                    fake.chmod(0o755)
                    environment = dict(os.environ, PATH=str(fake_bin) + os.pathsep + os.environ["PATH"])
                    result = self.run_gate(root, gate, environment)
                    self.assertNotEqual(result.returncode, 0, "tool errors must fail the formatting gate")
                    self.assertIn(f"deliberate {tool} failure", result.stderr)


if __name__ == "__main__":
    unittest.main()
