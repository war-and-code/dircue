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

    def test_wrong_ecosystem_component_rejected_in_capability_matching(self):
        """Capability matching must reject a component whose ecosystem does not match.

        Before the P4 fix, match_component was called without ecosystem checking in
        the capability owner resolution path.  A component node with the right name
        but a different ecosystem (e.g. ``npm`` instead of the expected ``python``)
        would silently match.  This test verifies that the check now lives inside
        match_component and rejects the wrong-ecosystem node when matching a
        capability owner.
        """
        label_data = json.loads((HERE / "changedetection.json").read_text())
        label = next(c for c in label_data["capabilities"] if c["capability"] == "net:http-client")
        doc = load("changedetection")
        # Baseline: capability matches with the correct component.
        capability = scorer.match_capability(doc, label, label_data)
        self.assertIsNotNone(capability)
        # Corrupt the owning component's ecosystem so it no longer matches the
        # label (label says pypi, normalised to python; flip the emitted node to
        # a different ecosystem to confirm the check fires).
        owner = next(n for n in doc["nodes"] if n["kind"] == "component" and n["name"] == "changedetection.io")
        original_eco = owner["properties"]["ecosystem"]
        owner["properties"]["ecosystem"] = "go"
        self.assertIsNone(scorer.match_capability(doc, label, label_data))
        owner["properties"]["ecosystem"] = original_eco

    def test_adjudication_referencing_nonexistent_assertion_fails_closed(self):
        """An adjudication that names an assertion not in the scored label set must
        raise SystemExit rather than silently succeed or produce a wrong score.
        """
        bad_adj = {
            "adjudications": [
                {
                    "id": "adj-bad",
                    "frozen_assertion": {
                        "label_file": "chi.json",
                        "category": "edges",
                        "identifier": {"type": "runs", "from": "no-such:file", "to": "no-such-component"},
                    },
                    "decision": "drop",
                    "citations": [{"repo": "chi", "commit": "3d1777a1ef" + "0" * 30, "file": "chi.go", "line": 57, "text": "package chi"}],
                }
            ]
        }
        # Build a minimal all_checks_by_key that does not contain the bad key.
        real_checks = {}
        with self.assertRaisesRegex(SystemExit, "does not exist in the scored label set"):
            with mock.patch.object(scorer, "ADJUDICATIONS_FILE",
                                   new=mock.Mock(exists=lambda: True,
                                                 read_text=lambda: __import__("json").dumps(bad_adj))):
                scorer.load_and_validate_adjudications(real_checks)

    def test_adjudication_without_citations_is_rejected(self):
        """An adjudication entry with no citations must be rejected."""
        uncited_adj = {
            "adjudications": [
                {
                    "id": "adj-nocite",
                    "frozen_assertion": {
                        "label_file": "chi.json",
                        "category": "components",
                        "identifier": {"name": "github.com/go-chi/chi/v5", "root": "."},
                    },
                    "decision": "cite_erratum",
                    "citations": [],  # empty — must be rejected
                }
            ]
        }
        stub_key = ("chi.json", "components", ("github.com/go-chi/chi/v5", "."))
        with self.assertRaisesRegex(SystemExit, "no citations"):
            with mock.patch.object(scorer, "ADJUDICATIONS_FILE",
                                   new=mock.Mock(exists=lambda: True,
                                                 read_text=lambda: __import__("json").dumps(uncited_adj))):
                scorer.load_and_validate_adjudications({stub_key: {}})

    def test_wrong_ecosystem_component_rejected_in_edge_matching(self):
        """Edge matching must reject a source/target component with wrong ecosystem.

        Before the P4 fix, the component lookups inside match_edge bypassed the
        ecosystem check, so a wrong-ecosystem component could be accepted as the
        edge source or target.
        """
        label_data = json.loads((HERE / "umami.json").read_text())
        uc_label = next(e for e in label_data["edges"] if e["type"] == "uses_capability"
                        and e["to"] == "auth:jwt")
        doc = load("umami")
        # Baseline: edge matches with the correct component.
        edge = scorer.match_edge(doc, uc_label, label_data)
        self.assertIsNotNone(edge)
        # Flip the owning component's ecosystem to something wrong.
        owner = next(n for n in doc["nodes"] if n["kind"] == "component" and n["name"] == "umami")
        owner["properties"]["ecosystem"] = "go"
        self.assertIsNone(scorer.match_edge(doc, uc_label, label_data))
        owner["properties"]["ecosystem"] = "npm"  # restore


    def test_citations_must_be_pinned_and_match_their_text(self):
        """A citation needs a full commit, and a dircue citation must quote its line."""
        pinned = {"repo": "dircue", "commit": "a" * 40, "file": "docs/MAP.md", "line": 2, "text": "runs"}
        with self.assertRaisesRegex(SystemExit, "needs a full commit"):
            scorer.check_citation("adj-x", dict(pinned, commit="4eaa58f"))
        shown = mock.Mock(returncode=0, stdout=b"first line\nsecond line\n")
        with mock.patch.object(scorer.subprocess, "run", return_value=shown):
            with self.assertRaisesRegex(SystemExit, "does not contain the cited text"):
                scorer.check_citation("adj-x", pinned)
            scorer.check_citation("adj-x", dict(pinned, text="second"))


if __name__ == "__main__":
    unittest.main()
