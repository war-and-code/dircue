import importlib.util
import json
import unittest
from pathlib import Path


SCRIPT = Path(__file__).with_name("verify_public_quality.py")
SPEC = importlib.util.spec_from_file_location("verify_public_quality", SCRIPT)
quality = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(quality)


def node(node_id, path, kind="component"):
    return {
        "id": node_id,
        "kind": kind,
        "name": node_id,
        "paths": [path],
        "properties": {"root": path},
        "evidence": [{"path": path}],
    }


def document(nodes=None, edges=None, coverage=None):
    return {
        "nodes": nodes if nodes is not None else [node("in-slice", "src/app.toml")],
        "edges": edges if edges is not None else [],
        "coverage": coverage if coverage is not None else [
            {"question": question, "scope": ".", "status": "unknown"}
            for question in sorted(quality.COVERAGE_QUESTIONS)
        ],
    }


class PublicQualityHarnessTests(unittest.TestCase):
    def test_score_uses_multiset_counts(self):
        tp, fp, fn, precision, recall = quality.score(["expected"], ["expected", "duplicate"])
        self.assertEqual((tp, fp, fn), (1, 1, 0))
        self.assertEqual(precision, 0.5)
        self.assertEqual(recall, 1.0)

    def test_mismatch_report_preserves_counts_and_bounds_samples(self):
        report = quality.mismatch_report([{"x": 1}], [{"x": 2}, {"x": 2}], sample_limit=1)
        self.assertEqual(report["missing_count"], 1)
        self.assertEqual(report["unexpected_count"], 2)
        self.assertEqual(report["missing_records"], [{"x": 1}])
        self.assertEqual(report["unexpected_records"], [{"x": 2}])
        self.assertEqual(report["unexpected_records_omitted"], 1)

    def test_mismatch_report_bounds_large_record_values(self):
        report = quality.mismatch_report([{"x": "0123456789"}], [], value_bytes=8)
        sample = report["missing_records"][0]
        self.assertTrue(sample["truncated_record"])
        self.assertLessEqual(len(sample["preview"].encode("utf-8")), 8)

    def test_paths_reject_parent_escape_and_non_normalized_forms(self):
        for path in ("../outside", "a/../b", "./file", "/absolute", "a\\b", ""):
            with self.subTest(path=path):
                self.assertFalse(quality.valid_repo_path(path))
        self.assertTrue(quality.valid_repo_path("src/module/manifest.toml"))

    def test_manifest_requires_non_vacuous_exact_coverage_universe(self):
        manifest = json.loads((SCRIPT.with_name("public_quality_expectations.json")).read_text())
        quality.validate_manifest(manifest)
        broken = json.loads(json.dumps(manifest))
        broken["repositories"][0]["expected_coverage"].pop()
        with self.assertRaisesRegex(ValueError, "every known question"):
            quality.validate_manifest(broken)

    def test_manifest_missing_expected_list_fails_with_field_name(self):
        manifest = json.loads((SCRIPT.with_name("public_quality_expectations.json")).read_text())
        del manifest["repositories"][0]["expected_nodes"]
        with self.assertRaisesRegex(ValueError, "expected_nodes"):
            quality.validate_manifest(manifest)

    def test_document_rejects_dangling_edge_endpoint(self):
        d = document(edges=[{
            "id": "e1", "from": "missing", "to": "in-slice",
            "type": "depends_on_local", "evidence": [{"path": "src/app.toml"}],
        }])
        with self.assertRaisesRegex(ValueError, "missing endpoint"):
            quality.validate_document(d, "fixture")

    def test_document_rejects_malformed_paths_and_evidence_entries(self):
        d = document()
        d["nodes"][0]["paths"] = "src/app.toml"
        with self.assertRaisesRegex(ValueError, "invalid paths"):
            quality.validate_document(d, "fixture")
        d = document()
        d["nodes"][0]["evidence"] = [None]
        with self.assertRaisesRegex(ValueError, "missing or invalid evidence path"):
            quality.validate_document(d, "fixture")

    def test_scoring_keeps_edge_with_scoped_evidence_and_full_endpoint_identity(self):
        source = node("source", "src/app.toml")
        target = node("target", "src/other.toml")
        edge = {
            "id": "edge-1", "type": "depends_on_local", "from": "source", "to": "target",
            "properties": {"state": "resolved"},
            "evidence": [{"path": "src/app.toml"}],
        }
        d = document(nodes=[source, target], edges=[edge])
        ids = quality.validate_document(d, "fixture")
        expected = {
            "evaluated_kinds": ["component"],
            "expected_nodes": [],
            "expected_edges": [],
            "expected_coverage": [],
        }
        expected_items, actual_items = quality.collect_scored_items(
            expected, d, ids, {"src/app.toml"},
        )
        actual_edges = [item for item in actual_items if item["record"] == "edge"]
        self.assertEqual(len(actual_edges), 1)
        self.assertEqual(actual_edges[0]["to"]["paths"], ["src/other.toml"])
        self.assertEqual(quality.question_bucket(actual_edges[0]), "components")

    def test_contains_edge_question_follows_endpoint_kinds(self):
        content_edge = {
            "record": "edge", "type": "contains",
            "from": {"kind": "content"}, "to": {"kind": "content"},
        }
        component_edge = {
            "record": "edge", "type": "contains",
            "from": {"kind": "component"}, "to": {"kind": "component"},
        }
        self.assertEqual(quality.question_bucket(content_edge), "content")
        self.assertEqual(quality.question_bucket(component_edge), "components")
        content_component_edge = {
            "record": "edge", "type": "contains",
            "from": {"kind": "component"}, "to": {"kind": "content"},
        }
        with self.assertRaisesRegex(ValueError, "unclassified endpoint kinds"):
            quality.question_bucket(content_component_edge)

    def test_coverage_scope_is_part_of_the_compared_record(self):
        d = document()
        d["coverage"].append({"question": "components", "scope": "src/", "status": "unknown"})
        ids = quality.validate_document(d, "fixture")
        expected = {
            "evaluated_kinds": ["component"],
            "expected_nodes": [], "expected_edges": [],
            "expected_coverage": [
                {"question": question, "status": "unknown"}
                for question in sorted(quality.COVERAGE_QUESTIONS)
            ],
        }
        expected_items, actual_items = quality.collect_scored_items(expected, d, ids, set())
        _, fp, fn, _, _ = quality.score(
            [x for x in expected_items if x["record"] == "coverage"],
            [x for x in actual_items if x["record"] == "coverage"],
        )
        self.assertEqual((fp, fn), (1, 0))

    def test_empty_facts_cannot_be_misreported_as_perfect(self):
        with self.assertRaisesRegex(ValueError, "no scored facts"):
            quality.require_scored_universe([{}], [0, 0, 0])


if __name__ == "__main__":
    unittest.main()
