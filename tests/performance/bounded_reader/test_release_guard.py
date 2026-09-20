"""Baseline provenance regressions using tiny files, without invoking Go."""
import hashlib
import importlib.util
import json
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

SPEC = importlib.util.spec_from_file_location("allocation_harness", Path(__file__).with_name("profile_allocations.py"))
HARNESS = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(HARNESS)


class ReleaseTreeGuardTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name)
        self.contents = {
            "main.go": b"package main\nfunc main() {}\n",
            "pkg/init.go": b"package pkg\nfunc init() { register() }\n",
            "pkg/platform_windows.go": b"//go:build windows\npackage pkg\n",
            "native/unused.c": b"int released_constant = 1;\n",
            "pkg/scanner/scanner.go": b"package scanner\n// release scanner\n",
            "pkg/scanner/git.go": b"package scanner\n// release Git reader\n",
            "assets/model.bin": b"\x00embedded model\xff",
            "go.mod": b"module example\n",
            "README.md": b"Release documentation\n",
        }
        self.entries = []
        for name, contents in self.contents.items():
            path = self.root / name
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_bytes(contents)
            oid = hashlib.sha1(b"blob " + str(len(contents)).encode() + b"\0" + contents).hexdigest()
            self.entries.append({"path": name, "mode": "100644", "oid": oid})

    def check(self, replacements=None, **options):
        return HARNESS.verify_release_tree(self.root, self.entries, replacements or {}, **options)

    def test_complete_release_passes(self):
        result = self.check()
        self.assertEqual(result["tracked_blob_count"], len(self.entries))
        self.assertFalse(result["instrumentation_main_exception"])

    def test_missing_init_only_file_rejected(self):
        (self.root / "pkg/init.go").unlink()
        with self.assertRaisesRegex(AssertionError, "missing.*pkg/init.go"):
            self.check()

    def test_new_build_tag_cannot_hide_changed_source(self):
        (self.root / "pkg/init.go").write_bytes(b"//go:build never_selected\npackage pkg\n")
        with self.assertRaisesRegex(AssertionError, "compiled source differs: pkg/init.go"):
            self.check()

    def test_already_excluded_platform_source_is_checked(self):
        (self.root / "pkg/platform_windows.go").write_bytes(b"//go:build windows\npackage pkg\nvar changed = 1\n")
        with self.assertRaisesRegex(AssertionError, "compiled source differs"):
            self.check()

    def test_unselected_native_source_is_checked(self):
        (self.root / "native/unused.c").write_bytes(b"int released_constant = 2;\n")
        with self.assertRaisesRegex(AssertionError, "compiled source differs"):
            self.check()

    def test_missing_embedded_blob_rejected(self):
        (self.root / "assets/model.bin").unlink()
        with self.assertRaisesRegex(AssertionError, "missing.*assets/model.bin"):
            self.check()

    def test_document_changes_allowed_but_deletion_rejected(self):
        path = self.root / "README.md"
        path.write_text("Updated evidence documentation\n")
        self.check()
        path.unlink()
        with self.assertRaisesRegex(AssertionError, "missing.*README.md"):
            self.check()

    def test_intended_source_restoration_and_main_instrumentation(self):
        replacements = {}
        for name in HARNESS.READ_PATHS:
            (self.root / name).write_text("package scanner\n// candidate\n")
            restored = self.root / (Path(name).name + ".restored")
            restored.write_bytes(self.contents[name])
            replacements[str(self.root / name)] = str(restored)
        instrumented = self.root / "instrumentation.go"
        instrumented.write_bytes(b"package main\nfunc main() { collect() }\n")
        replacements[str(self.root / "main.go")] = str(instrumented)
        replacements[str(self.root / "pkg/scanner/read.go")] = ""
        result = self.check(replacements, instrumentation_main_sha256=HARNESS.sha(instrumented))
        self.assertTrue(result["instrumentation_main_exception"])
        with self.assertRaisesRegex(AssertionError, "expected hash"):
            self.check(replacements, instrumentation_main_sha256="0" * 64)

    def test_main_exception_requires_explicit_authorization(self):
        with self.assertRaisesRegex(AssertionError, "unexpected baseline overlay: main.go"):
            self.check({str(self.root / "main.go"): str(self.root / "main.go")})
        (self.root / "main.go").write_bytes(b"package main\nfunc main() { changed() }\n")
        with self.assertRaisesRegex(AssertionError, "compiled source differs: main.go"):
            self.check()

    def test_other_release_sources_cannot_be_removed_by_overlay(self):
        with self.assertRaisesRegex(AssertionError, "unexpected baseline overlay"):
            self.check({str(self.root / "pkg/init.go"): ""})

    def test_guard_runs_before_go_package_selection(self):
        (self.root / "pkg/init.go").unlink()
        overlay = self.root / "overlay.json"
        overlay.write_text(json.dumps({"Replace": {}}))
        with patch.object(HARNESS, "ROOT", self.root), \
             patch.object(HARNESS, "release_entries", return_value=self.entries), \
             patch.object(HARNESS, "command", side_effect=AssertionError("Go must not execute")) as command:
            with self.assertRaisesRegex(AssertionError, "missing.*pkg/init.go"):
                HARNESS.source_records(overlay, {}, "pinned-release", True)
            command.assert_not_called()


if __name__ == "__main__":
    unittest.main()
