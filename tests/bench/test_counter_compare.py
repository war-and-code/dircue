import importlib.util
import unittest
from pathlib import Path

spec = importlib.util.spec_from_file_location("counter_compare", Path(__file__).with_name("counter_compare.py"))
counter_compare = importlib.util.module_from_spec(spec)
spec.loader.exec_module(counter_compare)


class CounterCompareTests(unittest.TestCase):
    def test_flatten_nests_limit_hits(self):
        out = {}
        counter_compare.flatten("", {"files_read": 3, "limit_hits": {"tree_size": 0}}, out)
        self.assertEqual({"files_read": 3, "limit_hits.tree_size": 0}, out)

    def test_render_marks_new_equal_and_delta(self):
        report = counter_compare.render([
            ("fx", "a", None, 1),
            ("fx", "b", 2, 2),
            ("fx", "c", 5, 3),
        ])
        self.assertIn("| `fx` | `a` | — | 1 | new |", report)
        self.assertIn("| `fx` | `b` | 2 | 2 | = |", report)
        self.assertIn("| `fx` | `c` | 5 | 3 | -2 |", report)


if __name__ == "__main__":
    unittest.main()
