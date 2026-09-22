"""Keep Actions event-driven and external dependencies pinned."""

from pathlib import Path
import re
import unittest


WORKFLOWS = Path(__file__).resolve().parents[2] / ".github" / "workflows"
ALLOWED_EVENTS = {"push", "pull_request", "workflow_dispatch"}
ACTION_SHA = re.compile(r"^[0-9a-f]{40}$")
USES = re.compile(r"^\s*(?:- )?uses:\s*([^\s#]+)")


class WorkflowPolicyTests(unittest.TestCase):
    def test_every_workflow_is_event_driven(self) -> None:
        files = sorted((*WORKFLOWS.glob("*.yml"), *WORKFLOWS.glob("*.yaml")))
        self.assertTrue(files, "no workflows found")
        for path in files:
            with self.subTest(path=path.name):
                lines = path.read_text().splitlines()
                self.assertIn("on:", lines, "expected an explicit event block")
                start = lines.index("on:") + 1
                events = set()
                for line in lines[start:]:
                    if line and not line.startswith((" ", "\t", "#")):
                        break
                    match = re.match(r"^  ([A-Za-z_]+):", line)
                    if match:
                        events.add(match.group(1))
                self.assertTrue(events, "empty workflow event block")
                self.assertLessEqual(events, ALLOWED_EVENTS)

    def test_external_actions_use_full_commit_hashes(self) -> None:
        files = sorted((*WORKFLOWS.glob("*.yml"), *WORKFLOWS.glob("*.yaml")))
        self.assertTrue(files, "no workflows found")
        for path in files:
            for number, line in enumerate(path.read_text().splitlines(), 1):
                match = USES.match(line)
                if not match or match.group(1).startswith("./"):
                    continue
                action, separator, revision = match.group(1).rpartition("@")
                with self.subTest(path=path.name, line=number):
                    self.assertTrue(separator and action, "external action lacks a revision")
                    self.assertRegex(revision, ACTION_SHA)


if __name__ == "__main__":
    unittest.main()
