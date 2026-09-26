"""Keep the post-hoc Flask miss review complete and separate from scoring."""
import hashlib
import json
import unittest
from pathlib import Path

HERE = Path(__file__).resolve().parent
RECEIPT = HERE / "independent_results.json"
LABEL = HERE / "independent_labels" / "flask-on-docker.json"
LEDGER = HERE / "independent_flask_triage.json"
EXPECTED_RECEIPT_SHA256 = "93e439de40375d558db44ca51272fb0878be792c585aae88461318a7abac3bcf"
EXPECTED_LABEL_SHA256 = "fc72b58deb7034fa0c025108d29cee5f931a51584500e9642f1035e8a642a91f"
CATEGORIES = {
    "equivalent_observation_under_different_name_or_vocabulary",
    "real_map_miss",
    "unresolved",
}


class IndependentFlaskTriageTest(unittest.TestCase):
    def setUp(self):
        self.receipt_bytes = RECEIPT.read_bytes()
        self.receipt = json.loads(self.receipt_bytes)
        self.label_bytes = LABEL.read_bytes()
        self.label = json.loads(self.label_bytes)
        self.ledger = json.loads(LEDGER.read_text())

    def test_frozen_inputs_and_map_identity(self):
        self.assertEqual(hashlib.sha256(self.receipt_bytes).hexdigest(), EXPECTED_RECEIPT_SHA256)
        self.assertEqual(hashlib.sha256(self.label_bytes).hexdigest(), EXPECTED_LABEL_SHA256)
        result = next(r for r in self.receipt["repos"] if r["repo"] == "flask-on-docker")
        self.assertEqual(self.ledger["upstream_commit"], result["upstream_commit"])
        self.assertEqual(self.ledger["map_sha256"], result["map_sha256"])

    def test_ledger_covers_each_missed_fact_exactly_once(self):
        result = next(r for r in self.receipt["repos"] if r["repo"] == self.ledger["repo"])
        expected = {
            (question, fact)
            for question, data in result["questions"].items()
            for fact in data["missed_items"]
        }
        entries = self.ledger["entries"]
        actual = [(entry["question"], entry["missed_fact"]) for entry in entries]
        self.assertEqual(len(actual), len(set(actual)), "duplicate triage key")
        self.assertEqual(set(actual), expected, "ledger does not exactly cover receipt misses")

    def test_categories_and_citations_are_valid(self):
        self.assertEqual(
            {category: sum(e["category"] == category for e in self.ledger["entries"])
             for category in CATEGORIES},
            {
                "equivalent_observation_under_different_name_or_vocabulary": 4,
                "real_map_miss": 6,
                "unresolved": 2,
            },
        )
        oracle_paths = {item["path"] for item in self.label["oracle_files"]}
        for entry in self.ledger["entries"]:
            self.assertIn(entry["category"], CATEGORIES)
            self.assertTrue(entry["source"], entry["missed_fact"])
            for citation in entry["source"]:
                self.assertIn(citation["path"], oracle_paths, entry["missed_fact"])
                self.assertRegex(citation["lines"], r"^[0-9,-]+$", entry["missed_fact"])
            self.assertTrue(entry["rationale"].strip(), entry["missed_fact"])
            if entry["category"] == "equivalent_observation_under_different_name_or_vocabulary":
                self.assertTrue(entry["map_observations"], entry["missed_fact"])
            if entry["category"] == "real_map_miss":
                self.assertEqual(entry["map_observations"], [], entry["missed_fact"])


if __name__ == "__main__":
    unittest.main()
