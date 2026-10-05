"""The reason-code table in docs/LOCKFILES.md must match the codes the analyzer emits.

Every kebab-case string literal in pkg/lockfiles/analyze.go is either a reason
code documented in the table or one of the identifiers listed in NOT_REASONS.
A new literal therefore needs a table row or a deliberate entry here.
"""

import re
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
ANALYZE_GO = ROOT / "pkg" / "lockfiles" / "analyze.go"
LOCKFILES_MD = ROOT / "docs" / "LOCKFILES.md"

NOT_REASONS = {
    # Module diagnostic codes, reported in lockfiles.diagnostics.
    "conflicting-inventory-entry", "duplicate-project-record", "invalid-inventory-path", "invalid-project-record",
    # Declaration diagnostic codes the analyzer reads as input.
    "duplicate-npm-workspace-name", "invalid-npm-dependency", "invalid-npm-field", "invalid-npm-manifest",
    "npm-resolution-limit", "npm-workspace-match-limit", "unsupported-npm-dependency",
    "unsupported-npm-workspace-dependency", "unsupported-npm-workspace-field", "unsupported-npm-workspace-identity",
    "unsupported-npm-workspace-pattern", "unsupported-npm-workspaces",
    # Declaration requirement and reference kinds.
    "npm-dependency", "npm-local-dependency", "npm-workspace-dependency", "npm-workspace-member", "npm-workspace-root",
    "package-manager", "package-reference",
    # Check names and semantics identifiers.
    "direct-dependency-presence", "npm-direct-declaration-table-match", "nuget-observed-direct-package-presence",
    "npm-package-lock-direct-tables-v1", "nuget-packages-lock-direct-presence-v1", "npm-workspace-member-lock-descriptors-v1",
    # An internal project-configuration result and an unreachable default branch.
    "custom-lock-path", "unsupported-ecosystem",
}


class LockfileReasonTests(unittest.TestCase):
    def test_reason_table_matches_the_analyzer(self) -> None:
        literals = set(re.findall(r'"([a-z0-9]+(?:-[a-z0-9]+)+)"', ANALYZE_GO.read_text()))
        documented = set(re.findall(r"^\| `([a-z0-9-]+)` \|", LOCKFILES_MD.read_text(), re.M))
        self.assertEqual(sorted(literals - NOT_REASONS - documented), [], "emitted reason codes missing from docs/LOCKFILES.md")
        self.assertEqual(sorted(documented - literals), [], "documented reason codes the analyzer never emits")
        self.assertEqual(sorted(NOT_REASONS - literals), [], "stale NOT_REASONS entries")
        self.assertEqual(sorted(NOT_REASONS & documented), [], "identifiers documented as reason codes")


if __name__ == "__main__":
    unittest.main()
