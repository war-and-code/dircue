"""The fixture oracle must reject wrong coverage and incomplete edge evidence."""
import importlib.util
from pathlib import Path
import unittest
from copy import deepcopy

spec = importlib.util.spec_from_file_location("fixture_verify", Path(__file__).with_name("verify.py"))
verify = importlib.util.module_from_spec(spec)
spec.loader.exec_module(verify)


class FixtureOracleTests(unittest.TestCase):
    def test_noncanonical_or_dangling_graph_identity_is_rejected(self):
        paths = ["src/hello.py"]
        identity = verify.stable_id("component", [*paths, "hello"])
        edge_id = verify.stable_id("edge-runs", [identity, identity, "fixture"])
        document = {"nodes": [{"kind": "component", "id": identity, "paths": paths, "discriminator": "hello"}],
                    "edges": [{"id": edge_id, "type": "runs", "from": identity, "to": identity, "discriminator": "fixture"}]}
        verify.check_identities(document)
        for mutation in ("node-id", "edge-id", "duplicate-node", "duplicate-edge", "dangling"):
            changed = deepcopy(document)
            if mutation == "node-id":
                changed["nodes"][0]["id"] = "component:" + "0" * 32
            elif mutation == "edge-id":
                changed["edges"][0]["id"] = "edge-runs:" + "0" * 32
            elif mutation == "duplicate-node":
                changed["nodes"].append(changed["nodes"][0])
            elif mutation == "duplicate-edge":
                changed["edges"].append(changed["edges"][0])
            else:
                changed["nodes"] = []
            with self.subTest(mutation=mutation), self.assertRaises(AssertionError):
                verify.check_identities(changed)

    def test_edge_coverage_and_evidence_are_part_of_the_oracle(self):
        document = {"nodes": [{"id": "a", "kind": "deployable", "paths": ["Procfile"]},
                              {"id": "b", "kind": "component", "paths": ["api/pyproject.toml"]}],
                    "edges": [{"type": "runs", "from": "a", "to": "b",
                               "coverage": {"status": "partial"},
                               "evidence": [{"path": "Procfile"}, {"path": "api/app.py"}]}]}
        want = {"kind": "edge", "type": "runs", "from_path": "Procfile",
                "to_path": "api/pyproject.toml", "coverage": {"status": "partial"},
                "evidence_paths": ["Procfile", "api/app.py"]}
        item = verify.entries(document)[-1]
        self.assertTrue(verify.matches(item, want))
        document["edges"][0]["coverage"]["status"] = "complete"
        self.assertFalse(verify.matches(verify.entries(document)[-1], want))
        document["edges"][0]["coverage"]["status"] = "partial"
        document["edges"][0]["evidence"].pop()
        self.assertFalse(verify.matches(verify.entries(document)[-1], want))

    def test_misspelled_constraints_do_not_silently_pass(self):
        with self.assertRaisesRegex(ValueError, "unknown fixture assertion"):
            verify.matches({"kind": "edge"}, {"kind": "edge", "covergae": {"status": "partial"}})


if __name__ == "__main__":
    unittest.main()
