"""Unit tests for accuracy card math."""
import sys
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).parent))

from accuracy import (
    aggregate_kind_scores,
    evidence_paths,
    normalize_node,
    node_key,
    score_repo,
    wilson_ci,
)


class TestWilsonCI(unittest.TestCase):
    def test_zero_total(self):
        lo, hi = wilson_ci(0, 0)
        self.assertEqual(lo, 0.0)
        self.assertEqual(hi, 1.0)

    def test_all_success(self):
        lo, hi = wilson_ci(10, 10)
        self.assertGreater(lo, 0.5)
        self.assertEqual(hi, 1.0)

    def test_no_success(self):
        lo, hi = wilson_ci(0, 10)
        self.assertEqual(lo, 0.0)
        self.assertLess(hi, 0.5)

    def test_half(self):
        lo, hi = wilson_ci(5, 10)
        self.assertLess(lo, 0.5)
        self.assertGreater(hi, 0.5)

    def test_bounds_always_valid(self):
        for tp in range(0, 20):
            for total in range(tp, 20):
                lo, hi = wilson_ci(tp, total)
                self.assertGreaterEqual(lo, 0.0)
                self.assertLessEqual(hi, 1.0)
                self.assertLessEqual(lo, hi)

    def test_precision_recall_never_inverts(self):
        """Confidence interval never inverts (lo <= hi)."""
        for n in [1, 5, 10, 100]:
            for k in range(n + 1):
                lo, hi = wilson_ci(k, n)
                self.assertLessEqual(lo, hi, f"CI inverted for k={k} n={n}")


class TestNormalizeNode(unittest.TestCase):
    def test_basic(self):
        node = {"id": "1", "kind": "component", "name": "foo",
                "paths": ["a/b"], "properties": {"root": "a/b"}, "evidence": []}
        norm = normalize_node(node)
        self.assertEqual(norm["kind"], "component")
        self.assertEqual(norm["name"], "foo")
        self.assertNotIn("id", norm)
        self.assertNotIn("evidence", norm)

    def test_missing_fields(self):
        node = {"kind": "deployable"}
        norm = normalize_node(node)
        self.assertIsNone(norm["name"])
        self.assertEqual(norm["paths"], [])
        self.assertEqual(norm["properties"], {})


class TestScoreRepo(unittest.TestCase):
    def _make_document(self, nodes):
        return {
            "kind": "map",
            "schema_version": "1.0.0",
            "nodes": nodes,
            "edges": [],
        }

    def test_perfect_match(self):
        expected = {
            "evaluated_kinds": ["component"],
            "oracle_files": [{"path": "src/foo.py", "sha256": "abc"}],
            "expected_nodes": [
                {"kind": "component", "name": "foo", "paths": ["src/foo.py"], "properties": {"ecosystem": "python"}},
            ],
        }
        document = self._make_document([
            {"id": "1", "kind": "component", "name": "foo",
             "paths": ["src/foo.py"], "properties": {"ecosystem": "python"}, "evidence": [{"path": "src/foo.py"}]},
        ])
        scores = score_repo(expected, document)
        self.assertIn("component", scores)
        s = scores["component"]
        self.assertEqual(s["tp"], 1)
        self.assertEqual(s["fp"], 0)
        self.assertEqual(s["fn"], 0)
        self.assertEqual(s["precision"], 1.0)
        self.assertEqual(s["recall"], 1.0)

    def test_false_positive(self):
        expected = {
            "evaluated_kinds": ["component"],
            "oracle_files": [{"path": "src/foo.py", "sha256": "abc"}],
            "expected_nodes": [],
        }
        document = self._make_document([
            {"id": "1", "kind": "component", "name": "foo",
             "paths": ["src/foo.py"], "properties": {}, "evidence": [{"path": "src/foo.py"}]},
        ])
        scores = score_repo(expected, document)
        s = scores["component"]
        self.assertEqual(s["tp"], 0)
        self.assertEqual(s["fp"], 1)
        self.assertEqual(s["fn"], 0)
        self.assertEqual(s["precision"], 0.0)
        self.assertIsNone(s["recall"])  # no expected → recall undefined

    def test_false_negative(self):
        expected = {
            "evaluated_kinds": ["component"],
            "oracle_files": [{"path": "src/foo.py", "sha256": "abc"}],
            "expected_nodes": [
                {"kind": "component", "name": "foo", "paths": ["src/foo.py"], "properties": {}},
            ],
        }
        document = self._make_document([])
        scores = score_repo(expected, document)
        s = scores["component"]
        self.assertEqual(s["tp"], 0)
        self.assertEqual(s["fp"], 0)
        self.assertEqual(s["fn"], 1)
        self.assertIsNone(s["precision"])  # no actuals → precision undefined
        self.assertEqual(s["recall"], 0.0)

    def test_zero_labels(self):
        expected = {
            "evaluated_kinds": ["capability"],
            "oracle_files": [{"path": "req.txt", "sha256": "abc"}],
            "expected_nodes": [],
        }
        document = self._make_document([])
        scores = score_repo(expected, document)
        s = scores["capability"]
        self.assertEqual(s["label_count"], 0)
        self.assertIsNone(s["precision"])
        self.assertIsNone(s["recall"])
        self.assertIn("no labels", s["note"])

    def test_node_outside_oracle_paths_not_counted(self):
        """A node evidenced only by a non-oracle path is not in the evaluated universe."""
        expected = {
            "evaluated_kinds": ["component"],
            "oracle_files": [{"path": "src/a.py", "sha256": "abc"}],
            "expected_nodes": [],
        }
        document = self._make_document([
            {"id": "1", "kind": "component", "name": "other",
             "paths": ["src/b.py"], "properties": {}, "evidence": [{"path": "src/b.py"}]},
        ])
        scores = score_repo(expected, document)
        # "other" is not evidenced by oracle files, so not a FP
        s = scores["component"]
        self.assertEqual(s["fp"], 0)


class TestAggregateKindScores(unittest.TestCase):
    def test_two_repos_same_kind(self):
        kind_scores_list = [
            {"component": {"tp": 3, "fp": 1, "fn": 0, "precision": 0.75, "recall": 1.0,
                           "precision_ci_95": None, "recall_ci_95": None, "label_count": 3, "note": None}},
            {"component": {"tp": 2, "fp": 0, "fn": 1, "precision": 1.0, "recall": 0.67,
                           "precision_ci_95": None, "recall_ci_95": None, "label_count": 3, "note": None}},
        ]
        agg = aggregate_kind_scores(kind_scores_list)
        self.assertIn("component", agg)
        s = agg["component"]
        self.assertEqual(s["tp"], 5)
        self.assertEqual(s["fp"], 1)
        self.assertEqual(s["fn"], 1)
        self.assertEqual(s["repo_count"], 2)
        self.assertEqual(s["label_count"], 6)
        # precision = 5/6
        self.assertAlmostEqual(s["precision"], 5/6, places=3)
        # recall = 5/6
        self.assertAlmostEqual(s["recall"], 5/6, places=3)

    def test_empty_input(self):
        agg = aggregate_kind_scores([])
        self.assertEqual(agg, {})

    def test_zero_label_kind_excluded_from_repo_count(self):
        """A repo with zero labels for a kind should not increment repo_count."""
        kind_scores_list = [
            {"capability": {"tp": 0, "fp": 0, "fn": 0, "precision": None, "recall": None,
                            "precision_ci_95": None, "recall_ci_95": None, "label_count": 0, "note": "no labels"}},
            {"capability": {"tp": 1, "fp": 0, "fn": 0, "precision": 1.0, "recall": 1.0,
                            "precision_ci_95": None, "recall_ci_95": None, "label_count": 1, "note": None}},
        ]
        agg = aggregate_kind_scores(kind_scores_list)
        s = agg["capability"]
        self.assertEqual(s["repo_count"], 1)  # only one repo had labels


class TestAdversarialAccuracy(unittest.TestCase):
    def test_all_tp_false_positives_only(self):
        """All FP: precision = 0."""
        kind_scores_list = [
            {"component": {"tp": 0, "fp": 5, "fn": 0, "precision": 0.0, "recall": None,
                           "precision_ci_95": None, "recall_ci_95": None, "label_count": 0, "note": None}},
        ]
        agg = aggregate_kind_scores(kind_scores_list)
        s = agg["component"]
        self.assertEqual(s["precision"], 0.0)

    def test_all_fn_false_negatives_only(self):
        """All FN: recall = 0."""
        kind_scores_list = [
            {"component": {"tp": 0, "fp": 0, "fn": 3, "precision": None, "recall": 0.0,
                           "precision_ci_95": None, "recall_ci_95": None, "label_count": 3, "note": None}},
        ]
        agg = aggregate_kind_scores(kind_scores_list)
        s = agg["component"]
        self.assertIsNone(s["precision"])  # 0 actual nodes
        self.assertEqual(s["recall"], 0.0)

    def test_zero_division_treated_as_none_not_zero(self):
        """A question with no actuals should not silently report precision=0."""
        kind_scores_list = [
            {"component": {"tp": 0, "fp": 0, "fn": 0, "precision": None, "recall": None,
                           "precision_ci_95": None, "recall_ci_95": None, "label_count": 0, "note": "no labels"}},
        ]
        agg = aggregate_kind_scores(kind_scores_list)
        s = agg["component"]
        self.assertIsNone(s["precision"])
        self.assertIsNone(s["recall"])


if __name__ == "__main__":
    unittest.main()
