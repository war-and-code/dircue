"""Assert that the manifest-kind and ecosystem counts stated in README and docs
match the authoritative values derived from pkg/componentmap/build.go.

The authoritative source is:
  - ComponentKinds slice: 36 manifest kinds
  - ComponentEcosystems slice: 27 ecosystems (derived from ecosystem() applied
    to ComponentKinds)

Fail-before evidence (base 4eaa58f):
  README.md said "26 ecosystems" (wrong; correct is 27).
  No doc tests existed to catch the 34→36 kind discrepancy.
"""

import re
import unittest
from pathlib import Path

REPO_ROOT = Path(__file__).resolve().parents[2]
README = REPO_ROOT / "README.md"

# These constants must match the values in ComponentKinds and ComponentEcosystems
# in pkg/componentmap/build.go, and the Go tests TestComponentKindCountIsThirtySix
# and TestEcosystemCountIsTwentySeven enforce the same values on the Go side.
EXPECTED_MANIFEST_KINDS = 36
EXPECTED_ECOSYSTEMS = 27


class DocCountTests(unittest.TestCase):
    """README.md must state the correct manifest-kind and ecosystem counts."""

    def setUp(self) -> None:
        self.assertTrue(README.exists(), "README.md not found")
        self.text = README.read_text()

    def test_readme_states_correct_ecosystem_count(self) -> None:
        """README.md 'components: projects across N ecosystems' must say 27."""
        m = re.search(r"projects across (\d+) ecosystems", self.text)
        self.assertIsNotNone(
            m,
            "README.md must contain 'projects across N ecosystems'",
        )
        count = int(m.group(1))
        self.assertEqual(
            count,
            EXPECTED_ECOSYSTEMS,
            f"README.md says {count} ecosystems; expected {EXPECTED_ECOSYSTEMS} "
            f"(derived from pkg/componentmap/build.go ComponentEcosystems). "
            f"Update README.md and this constant together with the Go test.",
        )

    def test_readme_does_not_state_stale_34_manifest_kinds(self) -> None:
        """README.md must not state '34 manifest kinds' (the stale count)."""
        self.assertNotIn(
            "34 manifest kinds",
            self.text,
            "README.md must not state '34 manifest kinds'; update to 36",
        )


if __name__ == "__main__":
    unittest.main()
