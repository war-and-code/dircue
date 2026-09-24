import importlib.util
from pathlib import Path
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[2]
SPEC = importlib.util.spec_from_file_location("update_go_git", ROOT/"third_party/update_go_git.py")
UPDATE = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(UPDATE)

class EmbeddedGoGitTests(unittest.TestCase):
    def test_rewrites_self_imports_but_not_source_comments(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            source = root/"sample.go"
            source.write_text('package sample\nimport (\n\t"github.com/go-git/go-git/v5/plumbing"\n)\n// upstream path github.com/go-git/go-git/v5 is attribution.\n')
            changed = UPDATE.rewrite_import_paths(root)
            self.assertEqual(changed, ["sample.go"])
            result = source.read_text()
            self.assertIn(UPDATE.RUNTIME_MODULE + "/plumbing", result)
            self.assertIn("// upstream path " + UPDATE.MODULE + " is attribution.", result)

    def test_temporary_module_uses_retained_upstream_manifests(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            (root/"upstream.go.mod").write_text(f"module {UPDATE.MODULE}\n\ngo 1.25.0\n")
            (root/"upstream.go.sum").write_text("sum bytes\n")
            UPDATE.prepare_embedded_module(root)
            self.assertEqual((root/"go.mod").read_text(), f"module {UPDATE.RUNTIME_MODULE}\n\ngo 1.25.0\n")
            self.assertEqual((root/"go.sum").read_text(), "sum bytes\n")
            UPDATE.remove_embedded_module(root)
            self.assertFalse((root/"go.mod").exists())
            self.assertFalse((root/"go.sum").exists())

if __name__ == "__main__":
    unittest.main()
