"""Mutation checks for semantic identity matching in the frozen holdout scorer."""
import json
import unittest
from pathlib import Path
from unittest import mock

import score_first_run as scorer


HERE = Path(__file__).resolve().parent


def load(repo):
    return json.loads((HERE / "receipts/raw" / f"{repo}.json").read_text())


class IdentityMatchingTests(unittest.TestCase):
    def test_component_does_not_fall_back_to_only_root_component(self):
        label = json.loads((HERE / "chi.json").read_text())["components"][0]
        doc = load("chi")
        component = scorer.match_component(doc, label)
        self.assertIsNotNone(component)  # Go module property is a documented alias.
        component["properties"].pop("go_module")
        component["name"] = "unrelated-package"
        self.assertIsNone(scorer.match_component(doc, label))

    def test_deployable_does_not_accept_arbitrary_name_at_path(self):
        label = json.loads((HERE / "changedetection.json").read_text())["deployables"][0]
        doc = load("changedetection")
        deployable = scorer.match_deployable(doc, label)
        self.assertEqual(deployable["name"], "(root)")  # Dockerfile root alias.
        deployable["name"] = "unrelated-build"
        self.assertIsNone(scorer.match_deployable(doc, label))

    def test_capability_owner_is_part_of_semantic_identity(self):
        label_data = json.loads((HERE / "umami.json").read_text())
        label = label_data["capabilities"][0]
        doc = load("umami")
        capability = scorer.match_capability(doc, label, label_data)
        self.assertIsNotNone(capability)
        other = next(n for n in doc["nodes"] if n["kind"] == "component" and n["name"] == "@umami/api-client")
        capability["properties"]["owning_component"] = other["id"]
        self.assertIsNone(scorer.match_capability(doc, label, label_data))

    def test_label_bytes_must_match_frozen_git_blob(self):
        path = HERE / "chi.json"
        current = path.read_bytes()
        with mock.patch.object(scorer.subprocess, "run", return_value=mock.Mock(stdout=current)):
            self.assertEqual(scorer.verify_frozen_label(path, HERE.parents[2]), current)
        with mock.patch.object(scorer.subprocess, "run", return_value=mock.Mock(stdout=current + b" ")):
            with self.assertRaises(SystemExit):
                scorer.verify_frozen_label(path, HERE.parents[2])


if __name__ == "__main__":
    unittest.main()
