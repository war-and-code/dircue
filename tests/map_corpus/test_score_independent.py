"""Guard the distinction between exhaustive precision and targeted recall."""
import unittest
from types import SimpleNamespace

from score_independent import scoped_question


class ScopeTest(unittest.TestCase):
    def setUp(self):
        self.question = SimpleNamespace(
            question="deployables", tp=2, fp=3, fn=1,
            precision=0.4, recall=2 / 3,
            tp_items=["matched"], fp_items=["extra"], fn_items=["missing"],
        )

    def test_targeted_scope_does_not_claim_false_positives_or_precision(self):
        result = scoped_question(self.question, "targeted_recall_only")
        self.assertNotIn("precision", result)
        self.assertNotIn("false_positives", result)
        self.assertEqual(result["unadjudicated_extra_observations"], 3)
        self.assertEqual(result["targeted_recall"], 0.6667)

    def test_exhaustive_scope_reports_precision(self):
        result = scoped_question(self.question, "exhaustive")
        self.assertEqual(result["precision"], 0.4)
        self.assertEqual(result["false_positives"], 3)
        self.assertEqual(result["missed"], 1)

    def test_unknown_scope_is_rejected(self):
        with self.assertRaises(ValueError):
            scoped_question(self.question, "unknown")


if __name__ == "__main__":
    unittest.main()
