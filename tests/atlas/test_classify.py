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
