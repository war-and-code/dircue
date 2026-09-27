"""The component-kind and ecosystem counts stated in public docs must match the code.

pkg/componentmap/build.go lists the component kinds in componentKinds;
TestComponentKindCounts pins its length and the number of distinct ecosystem()
values. This check keeps README.md, CHANGELOG.md and the 1.0.0
release notes in step with them.
"""

import re
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
BUILD_GO = ROOT / "pkg" / "componentmap" / "build.go"
DOCS = [ROOT / "README.md", ROOT / "CHANGELOG.md", ROOT / "docs" / "releases" / "1.0.0.md"]
# Pinned on the Go side by TestComponentKindCounts.
ECOSYSTEM_VALUES = 27
CLAIM = re.compile(r"(\d+) component kinds, reported under (\d+) `ecosystem` values")


def component_kinds() -> list[str]:
    source = BUILD_GO.read_text()
    match = re.search(r"var componentKinds = \[\]string\{(.*?)\n\}", source, re.S)
    if match is None:
        raise AssertionError("componentKinds not found in pkg/componentmap/build.go")
    return re.findall(r'"([^"]+)"', match.group(1))


class DocCountTests(unittest.TestCase):
    def test_docs_state_the_code_counts(self) -> None:
        kinds = len(component_kinds())
        for doc in DOCS:
            with self.subTest(doc=doc.name):
                claims = CLAIM.findall(doc.read_text())
                self.assertTrue(claims, f"{doc.name} must state the component-kind and ecosystem counts")
                for stated_kinds, stated_ecosystems in claims:
                    self.assertEqual(int(stated_kinds), kinds)
                    self.assertEqual(int(stated_ecosystems), ECOSYSTEM_VALUES)

    def test_no_stale_count_wording(self) -> None:
        for doc in DOCS:
            with self.subTest(doc=doc.name):
                self.assertNotRegex(doc.read_text(), r"\d+ manifest kinds|across \d+ ecosystems")


if __name__ == "__main__":
    unittest.main()
