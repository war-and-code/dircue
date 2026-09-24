"""Unit tests for atlas diff classification logic."""
import sys
import unittest
from pathlib import Path

# Make classify importable without installing
sys.path.insert(0, str(Path(__file__).parent))

from classify import (
    classify_linguist_mismatch,
    classify_linguist_top_level_mismatch,
    classify_oracle_version,
    classify_scc_mismatch,
    classify_skip_dircue_only,
    classify_skip_scc_only,
    parse_scc_language_registry,
    scc_may_detect_shebang,
)

KNOWN_DIFFS = [
    {
        "tool": "scc",
        "repo": "*",
        "file": "src/RawStringLimitation.java",
        "category": "known-upstream-diff",
        "root_cause": "scc raw-string overcounting",
        "resolved_in": None,
    }
]


class TestClassifyOracleVersion(unittest.TestCase):
    def test_match(self):
        self.assertEqual(classify_oracle_version("9.7.0", "9.7.0"), "matched")

    def test_mismatch(self):
        self.assertEqual(classify_oracle_version("9.6.0", "9.7.0"), "oracle-version-mismatch")

    def test_empty(self):
        self.assertEqual(classify_oracle_version("", "9.7.0"), "oracle-version-mismatch")


class TestClassifySccMismatch(unittest.TestCase):
    """Tests for scc per-file mismatch classification."""

    def test_known_diff_manifest_match(self):
        # Path is in the known-differences manifest
        cat = classify_scc_mismatch(
            "src/RawStringLimitation.java",
            {"comment": 1},
            {"Comment": 2},
            KNOWN_DIFFS,
        )
        self.assertEqual(cat, "known-upstream-diff")

    def test_unknown_path_becomes_bug(self):
        cat = classify_scc_mismatch(
            "src/Example.go",
            {"lines": 10, "code": 8, "comment": 1, "blank": 1},
            {"Lines": 11, "Code": 8, "Comment": 1, "Blank": 2},
            [],
        )
        self.assertEqual(cat, "dircue-bug")

    def test_java_off_by_one_comment(self):
        # The hardcoded raw-string rule: Java file, off-by-one comment
        cat = classify_scc_mismatch(
            "src/SomeOther.java",
            {"comment": 2},
            {"Comment": 3},
            [],
        )
        # Only 1 diff → known-upstream-diff
        self.assertEqual(cat, "known-upstream-diff")

    def test_java_two_comment_diff_is_bug(self):
        cat = classify_scc_mismatch(
            "src/SomeOther.java",
            {"comment": 2},
            {"Comment": 4},
            [],
        )
        # 2 comment diff → dircue-bug (outside known-upstream-diff rule)
        self.assertEqual(cat, "dircue-bug")

    def test_cs_off_by_one_comment(self):
        cat = classify_scc_mismatch(
            "src/SomeOther.cs",
            {"comment": 0},
            {"Comment": 1},
            [],
        )
        self.assertEqual(cat, "known-upstream-diff")

    def test_no_known_diffs_no_java_cs(self):
        cat = classify_scc_mismatch(
            "src/main.py",
            {"lines": 10},
            {"Lines": 11},
            [],
        )
        self.assertEqual(cat, "dircue-bug")


class TestClassifyLinguistMismatch(unittest.TestCase):
    def test_without_known_diff_is_bug(self):
        cat = classify_linguist_mismatch("src/foo.pl", "Perl", "Perl 6", [])
        self.assertEqual(cat, "dircue-bug")

    def test_with_matching_known_diff(self):
        diffs = [{"tool": "linguist", "file": "src/foo.pl", "category": "known-upstream-diff",
                  "root_cause": "Enry misclassifies Perl 5 as Perl 6"}]
        cat = classify_linguist_mismatch("src/foo.pl", "Perl 6", "Perl", diffs)
        self.assertEqual(cat, "known-upstream-diff")

    def test_with_non_matching_file_diff(self):
        diffs = [{"tool": "linguist", "file": "src/other.pl", "category": "known-upstream-diff",
                  "root_cause": "irrelevant"}]
        cat = classify_linguist_mismatch("src/foo.pl", "Perl 6", "Perl", diffs)
        self.assertEqual(cat, "dircue-bug")


class TestClassifyLinguistTopLevel(unittest.TestCase):
    def test_bug_by_default(self):
        cat = classify_linguist_top_level_mismatch(
            "somerepo", {"Go": "100%"}, {"Python": {"size": 100}}, []
        )
        self.assertEqual(cat, "dircue-bug")

    def test_known_diff_repo_match(self):
        diffs = [{"tool": "linguist", "repo": "somerepo", "category": "known-upstream-diff",
                  "root_cause": "strategy chain difference"}]
        cat = classify_linguist_top_level_mismatch(
            "somerepo", {"Go": "100%"}, {"Python": {"size": 100}}, diffs
        )
        self.assertEqual(cat, "known-upstream-diff")

    def test_known_diff_no_repo_filter(self):
        diffs = [{"tool": "linguist", "category": "known-upstream-diff",
                  "root_cause": "applies to all"}]
        cat = classify_linguist_top_level_mismatch(
            "any-repo", {}, {}, diffs
        )
        self.assertEqual(cat, "known-upstream-diff")


class TestSkipDircueOnly(unittest.TestCase):
    """Files that dircue counted but scc did not output."""

    def test_dotfile_returns_scc_skips_dotfiles(self):
        # basename starts with '.' → scc skips it regardless of extensions present
        cat = classify_skip_dircue_only("tests/modules/home1/.jq", "jq", {".jq"})
        self.assertEqual(cat, "scc_skips_dotfiles")

    def test_dotfile_at_root_returns_scc_skips_dotfiles(self):
        cat = classify_skip_dircue_only(".flaskenv", "INI", set())
        self.assertEqual(cat, "scc_skips_dotfiles")

    def test_registry_supported_extension_is_unexplained_even_when_unobserved(self):
        # Registry says scc supports Makefiles; no emitted Makefile in this repo
        # cannot explain why this particular file was omitted.
        cat = classify_skip_dircue_only("Makefile", "Makefile", {"makefile"})
        self.assertEqual(cat, "unexplained")

    def test_extension_absent_from_scc_registry_is_scc_no_language(self):
        cat = classify_skip_dircue_only(
            "Makefile.am", "Makefile", {"makefile", "py"}, set(), set()
        )
        self.assertEqual(cat, "scc_no_language")

    def test_isolated_scc_omission_has_its_own_observed_category(self):
        cat = classify_skip_dircue_only(
            "scripts/package/PKGBUILD", "Shell", {"pkgbuild"}, set(), set(),
            {"scripts/package/PKGBUILD"},
        )
        self.assertEqual(cat, "scc_skips_file")

    def test_shebang_on_unregistered_extension_stays_unexplained(self):
        # scc considers shebangs for extensionless basenames.
        cat = classify_skip_dircue_only(
            "scripts/tool", "Shell", {"sh"}, set(), {"scripts/tool"}
        )
        self.assertEqual(cat, "unexplained")

    def test_dotted_unknown_extension_does_not_use_shebang(self):
        # scc 4.1.0 only considers shebangs for extensionless names (or .dotfiles).
        self.assertFalse(scc_may_detect_shebang("scripts/tool.unknown"))
        cat = classify_skip_dircue_only("scripts/tool.unknown", "Shell", {"sh"}, set(), set())
        self.assertEqual(cat, "scc_no_language")

    def test_known_extension_in_scc_returns_unexplained(self):
        # .go IS in scc output but scc still did not output this file → unexplained
        cat = classify_skip_dircue_only("src/main.go", "Go", {"go"})
        self.assertEqual(cat, "unexplained")

    def test_extension_case_insensitive(self):
        # Extension matching is lowercase-normalised
        cat = classify_skip_dircue_only("Module.PY", "Python", {"py"})
        self.assertEqual(cat, "unexplained")

    def test_supported_bare_name_even_if_other_languages_are_present(self):
        cat = classify_skip_dircue_only("Makefile", "Makefile", {"py", "go", "makefile"})
        self.assertEqual(cat, "unexplained")


class TestSccLanguageRegistry(unittest.TestCase):
    def test_parse_languages_output(self):
        registry = parse_scc_language_registry(
            "Autoconf (in)\nMakefile (makefile,mak,gnumakefile)\n"
            "BASH (bash,.bash_profile)\n"
            "CloudFormation (JSON) (json)\n"
            "CloudFormation (YAML) (yaml,yml)\n"
            "Standard ML (SML) (sml)\n"
        )
        self.assertEqual(registry, {
            "in", "makefile", "mak", "gnumakefile", "bash", ".bash_profile",
            "json", "yaml", "yml", "sml",
        })

    def test_reject_empty_or_malformed_registry(self):
        with self.assertRaises(ValueError):
            parse_scc_language_registry("not a language registry")

    def test_case_collision_returns_harness_case_collision(self):
        # xt_CONNMARK.h (uppercase) collides with xt_connmark.h on macOS HFS+/APFS
        scc_paths_lower = {"include/uapi/linux/netfilter/xt_connmark.h"}
        cat = classify_skip_dircue_only(
            "include/uapi/linux/netfilter/xt_CONNMARK.h", "C",
            {"h"}, scc_paths_lower,
        )
        self.assertEqual(cat, "harness_case_collision")

    def test_no_case_collision_falls_through(self):
        # Path not in scc_paths_lower → normal extension check
        cat = classify_skip_dircue_only(
            "include/uapi/linux/netfilter/xt_CONNMARK.h", "C",
            {"h"}, {"something_else.h"},
        )
        # .h IS in extensions but no case collision → unexplained
        self.assertEqual(cat, "unexplained")

    def test_scc_paths_lower_none_skips_collision_check(self):
        # Backwards compat: scc_paths_lower=None means skip the case check
        cat = classify_skip_dircue_only("foo/Bar.go", "Go", {"go"}, None, set())
        self.assertEqual(cat, "unexplained")


class TestSkipSccOnly(unittest.TestCase):
    """Files that scc output but dircue did not count as source."""

    def test_no_dircue_row_is_unexplained(self):
        # No dircue row at all → cannot attribute the skip → unexplained
        cat = classify_skip_scc_only("vendor/foo/bar.go", None)
        self.assertEqual(cat, "unexplained")

    def test_outside_scope_maps_to_dircue_out_of_scope(self):
        row = {"path": "README.md", "status": "skipped", "reason": "outside_scope"}
        cat = classify_skip_scc_only("README.md", row)
        self.assertEqual(cat, "dircue_out_of_scope")

    def test_unsupported_language(self):
        row = {"path": "foo.x10", "status": "skipped", "reason": "unsupported_language"}
        cat = classify_skip_scc_only("foo.x10", row)
        self.assertEqual(cat, "dircue_unsupported_language")

    def test_binary(self):
        row = {"path": "assets/logo.png", "status": "skipped", "reason": "binary"}
        cat = classify_skip_scc_only("assets/logo.png", row)
        self.assertEqual(cat, "dircue_binary")

    def test_non_regular_file(self):
        row = {"path": "symlink_target", "status": "skipped", "reason": "non_regular_file"}
        cat = classify_skip_scc_only("symlink_target", row)
        self.assertEqual(cat, "dircue_non_regular_file")

    def test_file_too_large(self):
        row = {"path": "big.bin", "status": "skipped", "reason": "file_too_large"}
        cat = classify_skip_scc_only("big.bin", row)
        self.assertEqual(cat, "dircue_file_too_large")

    def test_unknown_reason_is_unexplained(self):
        # A reason that is not in the mapping → unexplained
        row = {"path": "foo.go", "status": "skipped", "reason": "some_future_reason"}
        cat = classify_skip_scc_only("foo.go", row)
        self.assertEqual(cat, "unexplained")

    def test_empty_reason_is_unexplained(self):
        row = {"path": "foo.go", "status": "skipped", "reason": ""}
        cat = classify_skip_scc_only("foo.go", row)
        self.assertEqual(cat, "unexplained")


class TestAdversarialCases(unittest.TestCase):
    """Edge cases: empty inputs, all-skip, all-match."""

    def test_empty_known_diffs_file(self):
        cat = classify_scc_mismatch("x.go", {}, {}, [])
        self.assertEqual(cat, "dircue-bug")

    def test_inject_known_diff_detected(self):
        """An injected synthetic known-difference must be detected and classified."""
        diffs = [{"tool": "scc", "file": "synthetic/injected.go",
                  "category": "known-upstream-diff", "root_cause": "synthetic injection test"}]
        cat = classify_scc_mismatch("synthetic/injected.go", {"lines": 1}, {"Lines": 2}, diffs)
        self.assertEqual(cat, "known-upstream-diff")
        # Same path without the manifest entry → dircue-bug
        cat2 = classify_scc_mismatch("synthetic/injected.go", {"lines": 1}, {"Lines": 2}, [])
        self.assertEqual(cat2, "dircue-bug")


if __name__ == "__main__":
    unittest.main()
