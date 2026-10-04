"""Fast generator properties; CLI behavior is checked by the tiered runner."""

import tempfile
import unittest
from pathlib import Path, PureWindowsPath
from types import SimpleNamespace
from unittest.mock import patch

from generated_trees import CI_SEEDS, ECOSYSTEM_PAIRS, create_tree
from metamorphic_generated import _assert_generated_evidence, _assert_limit_effect, assert_no_absolute_strings, git, invoke, validate_output_guard_mutants


class GeneratedTreeTests(unittest.TestCase):
    def test_generator_rejects_a_nonempty_root(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary) / "tree"
            root.mkdir()
            (root / "stale.txt").write_text("stale", encoding="utf-8")
            with self.assertRaisesRegex(ValueError, "root must be empty"):
                create_tree(root, *CI_SEEDS[0])

    def test_seed_repeats_same_paths_and_contents(self):
        with tempfile.TemporaryDirectory() as temporary:
            parent = Path(temporary)
            first, second = parent / "first", parent / "second"
            evidence_a = create_tree(first, *CI_SEEDS[0])
            evidence_b = create_tree(second, *CI_SEEDS[0])
            self.assertEqual(evidence_a, evidence_b)
            snapshot = lambda root: {
                item.relative_to(root).as_posix(): item.read_bytes()
                for item in root.rglob("*") if item.is_file() and not item.is_symlink()
            }
            self.assertEqual(snapshot(first), snapshot(second))

    def test_changed_seed_changes_generated_content(self):
        pair = ECOSYSTEM_PAIRS[0]
        with tempfile.TemporaryDirectory() as temporary:
            parent = Path(temporary)
            first, second = parent / "first", parent / "second"
            create_tree(first, 10101, pair)
            create_tree(second, 10102, pair)
            self.assertNotEqual((first / "catalog/item-000.yaml").read_bytes(),
                                (second / "catalog/item-000.yaml").read_bytes())

    def test_each_pair_has_two_manifest_and_language_markers(self):
        self.assertEqual([pair.name for pair in ECOSYSTEM_PAIRS],
                         ["npm-go", "python-cargo", "dotnet-maven"])
        self.assertEqual([pair.first for pair in ECOSYSTEM_PAIRS], ["npm", "python", "dotnet"])
        self.assertEqual([pair.second for pair in ECOSYSTEM_PAIRS], ["go", "cargo", "maven"])

    def test_generated_shape_includes_depth_unicode_and_symlink_applicability(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary) / "tree"
            evidence = create_tree(root, 10101, ECOSYSTEM_PAIRS[0])
            self.assertEqual(evidence["deep_directory_count"], 55)
            self.assertGreater(len(Path(evidence["deepest_file"]).parts), 50)
            self.assertTrue((root / evidence["unicode_path"]).is_file())
            self.assertTrue(evidence["unicode_path"].endswith(".go"))
            self.assertIn(evidence["control_path_applicability"], {"supported", "unsupported-on-windows"})
            if evidence["control_path"]:
                self.assertTrue((root / evidence["control_path"]).is_file())
                self.assertTrue(evidence["control_path"].endswith(".py"))
            self.assertEqual(len(evidence["symlinks"]), 3)
            self.assertTrue(all("created" in link and "reason" in link for link in evidence["symlinks"]))

    def test_output_guard_mutants_detect_missing_generated_evidence(self):
        with tempfile.TemporaryDirectory() as temporary:
            evidence = create_tree(Path(temporary) / "tree", *CI_SEEDS[0])
            document = {
                "nodes": [
                    {"kind": "component", "paths": ["services/npm-service/package.json"]},
                    {"kind": "component", "paths": ["services/go-service/go.mod"]},
                    *[
                        {"kind": "content", "name": language,
                         "properties": {"role": "language_population", "language": language,
                                        "files": str(count)}}
                        for language, count in evidence["expected_language_file_counts"].items()
                    ],
                ],
                "coverage": [{"question": "content", "status": "partial", "reasons": ["non_regular_file"]}],
            }
            _assert_generated_evidence(document, evidence)
            language_report = {
                language: {"files": list(paths)}
                for language, paths in evidence["expected_language_paths"].items()
            }
            caught = validate_output_guard_mutants(document, evidence, language_report)
            self.assertIn("deep-python-path-omitted", caught)
            self.assertIn("unicode-go-path-omitted", caught)
            self.assertIn("ecosystem-component-omitted", caught)
            self.assertIn("exact-ecosystem-manifest-path-omitted", caught)
            self.assertIn("manifest-on-wrong-kind-node-rejected", caught)
            self.assertIn("ignored-file-byte-limit", caught)
            self.assertIn("ignored-inventory-budget", caught)
            if evidence["control_path"]:
                self.assertIn("control-python-path-omitted", caught)
            if any(link["created"] for link in evidence["symlinks"]):
                self.assertIn("symlink-omission-status-hidden", caught)

    def test_limits_require_their_own_reason_even_when_baseline_is_partial(self):
        baseline = {
            "status": "partial",
            "nodes": [
                {"kind": "content", "properties": {"role": "language_population", "files": "3"}},
            ],
            "coverage": [{"question": "content", "status": "partial", "reasons": ["non_regular_file"]}],
        }
        with self.assertRaisesRegex(AssertionError, "file_too_large"):
            _assert_limit_effect(baseline, baseline, "file_too_large", "byte-limit")
        with self.assertRaisesRegex(AssertionError, "tree_size_limit"):
            _assert_limit_effect(baseline, baseline, "tree_size_limit", "inventory-budget",
                                 require_lower_language_population=True)

        byte_limited = {
            **baseline,
            "coverage": [{"question": "content", "status": "partial",
                          "reasons": ["non_regular_file", "file_too_large"]}],
        }
        _assert_limit_effect(baseline, byte_limited, "file_too_large", "byte-limit")

        inventory_limited = {
            **baseline,
            "nodes": [{"kind": "content", "properties": {"role": "language_population", "files": "0"}}],
            "coverage": [{"question": "content", "status": "partial",
                          "reasons": ["non_regular_file", "tree_size_limit"]}],
        }
        _assert_limit_effect(baseline, inventory_limited, "tree_size_limit", "inventory-budget",
                             require_lower_language_population=True)

    def test_cli_and_git_capture_are_decoded_as_utf8(self):
        with tempfile.TemporaryDirectory() as temporary, patch("metamorphic_generated.smoke_process.run") as run:
            run.return_value = SimpleNamespace(returncode=0, stdout='{"雪":"ok"}', stderr="")
            self.assertEqual(invoke(Path("dircue"), "map", "--json"), {"雪": "ok"})
            self.assertEqual(run.call_args.kwargs["encoding"], "utf-8")

            run.reset_mock()
            run.return_value = SimpleNamespace(returncode=0, stdout="ok", stderr="")
            self.assertEqual(git(Path(temporary), "status", "--short"), "ok")
            self.assertEqual(run.call_args.kwargs["encoding"], "utf-8")

    def test_git_environment_cannot_redirect_temporary_repository_operations(self):
        poisoned = {
            "PATH": "ordinary-path", "GIT_DIR": "/unrelated/.git",
            "GIT_WORK_TREE": "/unrelated", "GIT_INDEX_FILE": "/unrelated/index",
            "GIT_OBJECT_DIRECTORY": "/unrelated/objects", "GIT_CONFIG_COUNT": "1",
            "GIT_CONFIG_KEY_0": "core.hooksPath", "GIT_CONFIG_VALUE_0": "/unrelated/hooks",
            "GIT_CONFIG_PARAMETERS": "poison", "GIT_AUTHOR_DATE": "2030-01-01",
        }
        with patch("metamorphic_generated.os.environ", poisoned), patch("metamorphic_generated.smoke_process.run") as run:
            run.return_value = SimpleNamespace(returncode=0, stdout="ok", stderr="")
            git(Path("temporary"), "status")
            environment = run.call_args.kwargs["env"]
            self.assertEqual(environment["PATH"], poisoned["PATH"])
            self.assertEqual(set(key for key in environment if key.startswith("GIT_")), {
                "GIT_CONFIG_NOSYSTEM", "GIT_CONFIG_GLOBAL", "GIT_AUTHOR_DATE", "GIT_COMMITTER_DATE",
            })
            self.assertEqual(environment["GIT_AUTHOR_DATE"], "2000-01-01T00:00:00Z")

    def test_absolute_path_guard_accepts_both_windows_separator_forms(self):
        root = PureWindowsPath(r"C:\Users\someone\temporary")
        for value in (str(root) + r"\file.go", root.as_posix() + "/file.go"):
            with self.subTest(value=value), self.assertRaisesRegex(AssertionError, "path leaked"):
                assert_no_absolute_strings({"nested": [value]}, (root,))


if __name__ == "__main__":
    unittest.main()
