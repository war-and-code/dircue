"""Keep Actions event-driven, external dependencies pinned, and permissions least-privilege."""

from pathlib import Path
import re
import unittest


REPO_ROOT = Path(__file__).resolve().parents[2]
WORKFLOWS = REPO_ROOT / ".github" / "workflows"
README = REPO_ROOT / "README.md"
RELEASING = REPO_ROOT / "RELEASING.md"
ALLOWED_EVENTS = {"push", "pull_request", "workflow_dispatch"}
ACTION_SHA = re.compile(r"^[0-9a-f]{40}$")

EXACT_COSIGN_IDENTITY = (
    "https://github.com/war-and-code/dircue/.github/workflows/release-candidate.yml"
    "@refs/heads/main"
)
SIGNER_WORKFLOW_FLAG = "--signer-workflow"
USES = re.compile(r"^\s*(?:- )?uses:\s*([^\s#]+)")
USES_KEY = re.compile(r"(?:^|[\s,{])(?:uses|['\"]uses['\"])\s*:")

RELEASE_WORKFLOW = WORKFLOWS / "release-candidate.yml"


def workflow_events(source: str) -> set[str]:
    lines = source.splitlines()
    if [line for line in lines if re.match(r"^(?:on|['\"]on['\"])\s*:", line)] != ["on:"]:
        raise AssertionError("expected exactly one canonical event key")
    if lines.count("on:") != 1:
        raise AssertionError("expected one canonical event block")
    start = lines.index("on:") + 1
    events = set()
    for line in lines[start:]:
        if line and not line.startswith((" ", "\t", "#")):
            break
        if not line.startswith("  ") or line.startswith("   "):
            continue
        event = line[2:]
        if not event or event.startswith("#"):
            continue
        match = re.fullmatch(r"([a-z_]+):(?:\s*.*)?", event)
        if not match:
            raise AssertionError(f"noncanonical event declaration: {event}")
        events.add(match.group(1))
    if not events or not events <= ALLOWED_EVENTS:
        raise AssertionError(f"unsupported workflow events: {events}")
    return events


def top_level_permissions(source: str) -> dict[str, str]:
    """Return the top-level permissions mapping for a workflow, or raise."""
    lines = source.splitlines()
    try:
        start = lines.index("permissions:")
    except ValueError:
        raise AssertionError("workflow has no top-level 'permissions:' block")
    result: dict[str, str] = {}
    for line in lines[start + 1:]:
        if not line.startswith("  ") or line.startswith("    "):
            break
        match = re.fullmatch(r"([a-z_-]+):\s*([a-z_-]+)", line.strip())
        if match:
            result[match.group(1)] = match.group(2)
    return result


def action_revisions(source: str) -> list[str]:
    revisions = []
    for line in source.splitlines():
        if not USES_KEY.search(line):
            continue
        match = USES.match(line)
        if not match:
            raise AssertionError(f"noncanonical action declaration: {line}")
        value = match.group(1).strip("\"'")
        if value.startswith("./"):
            continue
        action, separator, revision = value.rpartition("@")
        if not separator or not action or not ACTION_SHA.fullmatch(revision):
            raise AssertionError(f"external action lacks a full commit hash: {value}")
        revisions.append(revision)
    return revisions


class WorkflowPolicyTests(unittest.TestCase):
    def test_every_workflow_is_event_driven(self) -> None:
        files = sorted((*WORKFLOWS.glob("*.yml"), *WORKFLOWS.glob("*.yaml")))
        self.assertTrue(files, "no workflows found")
        for path in files:
            with self.subTest(path=path.name):
                workflow_events(path.read_text())

    def test_no_workflow_contains_schedule_trigger(self) -> None:
        """Explicit absence check: scheduled CI violates the project event-driven policy."""
        files = sorted((*WORKFLOWS.glob("*.yml"), *WORKFLOWS.glob("*.yaml")))
        self.assertTrue(files, "no workflows found")
        for path in files:
            with self.subTest(path=path.name):
                text = path.read_text()
                # A bare 'schedule:' or quoted 'schedule': at any indentation level
                # would indicate a schedule trigger block.
                self.assertNotRegex(
                    text,
                    r"(?m)^[ \t]*['\"]?schedule['\"]?\s*:",
                    f"{path.name} must not contain a 'schedule:' trigger",
                )

    def test_every_workflow_has_explicit_least_privilege_permissions(self) -> None:
        """Every workflow must declare top-level permissions: contents: read."""
        files = sorted((*WORKFLOWS.glob("*.yml"), *WORKFLOWS.glob("*.yaml")))
        self.assertTrue(files, "no workflows found")
        for path in files:
            with self.subTest(path=path.name):
                perms = top_level_permissions(path.read_text())
                self.assertIn(
                    "contents", perms,
                    f"{path.name}: top-level permissions must declare 'contents'",
                )
                self.assertEqual(
                    perms["contents"],
                    "read",
                    f"{path.name}: top-level 'contents' permission must be 'read' (write is allowed only at job scope)",
                )

    def test_external_actions_use_full_commit_hashes(self) -> None:
        files = sorted((*WORKFLOWS.glob("*.yml"), *WORKFLOWS.glob("*.yaml")))
        self.assertTrue(files, "no workflows found")
        for path in files:
            with self.subTest(path=path.name):
                action_revisions(path.read_text())

    def test_quoted_schedule_and_action_forms_cannot_bypass_checks(self) -> None:
        with self.assertRaises(AssertionError):
            workflow_events("on:\n  push:\n  'schedule':\n    - cron: '0 0 * * *'\njobs:\n")
        with self.assertRaises(AssertionError):
            workflow_events("on:\n  push:\n'on':\n  schedule:\n    - cron: '0 0 * * *'\njobs:\n")
        with self.assertRaises(AssertionError):
            action_revisions('      - uses: "actions/checkout@v7"\n')
        with self.assertRaises(AssertionError):
            action_revisions('      - { uses: actions/checkout@v7 }\n')


class ReleaseWorkflowSigningTests(unittest.TestCase):
    """Release workflow must contain build attestation and Sigstore signing steps."""

    def setUp(self) -> None:
        self.assertTrue(RELEASE_WORKFLOW.exists(), "release-candidate.yml not found")
        self.text = RELEASE_WORKFLOW.read_text()

    def test_attest_build_provenance_action_present(self) -> None:
        """actions/attest-build-provenance must be used in the release workflow."""
        self.assertIn(
            "actions/attest-build-provenance@",
            self.text,
            "release workflow must use actions/attest-build-provenance",
        )

    def test_cosign_installer_action_present(self) -> None:
        """sigstore/cosign-installer must be used in the release workflow."""
        self.assertIn(
            "sigstore/cosign-installer@",
            self.text,
            "release workflow must use sigstore/cosign-installer",
        )

    def test_cosign_sign_blob_step_present(self) -> None:
        """The release workflow must sign SHA256SUMS with cosign sign-blob."""
        self.assertIn(
            "cosign sign-blob",
            self.text,
            "release workflow must include a cosign sign-blob step",
        )

    def test_attestation_verify_step_present(self) -> None:
        """The release workflow must verify the attestation after upload."""
        self.assertIn(
            "gh attestation verify",
            self.text,
            "release workflow must include a 'gh attestation verify' step",
        )

    def test_cosign_verify_blob_step_present(self) -> None:
        """The release workflow must verify the cosign signature after upload."""
        self.assertIn(
            "cosign verify-blob",
            self.text,
            "release workflow must include a 'cosign verify-blob' step",
        )

    def test_signing_bundle_uploaded_to_release(self) -> None:
        """The Sigstore bundle (SHA256SUMS.sigstore.json) must be uploaded to the release."""
        self.assertIn(
            "SHA256SUMS.sigstore.json",
            self.text,
            "release workflow must reference SHA256SUMS.sigstore.json",
        )

    def test_draft_job_has_id_token_write(self) -> None:
        """The draft job must declare id-token: write to obtain OIDC tokens for signing."""
        self.assertIn(
            "id-token: write",
            self.text,
            "draft job must have id-token: write permission",
        )

    def test_draft_job_has_attestations_write(self) -> None:
        """The draft job must declare attestations: write to store build provenance."""
        self.assertIn(
            "attestations: write",
            self.text,
            "draft job must have attestations: write permission",
        )

    def test_top_level_permissions_still_read_only(self) -> None:
        """The workflow's top-level permissions must remain contents: read."""
        perms = top_level_permissions(self.text)
        self.assertEqual(
            perms.get("contents"),
            "read",
            "top-level contents permission must remain 'read'; write only at job scope",
        )
        self.assertNotIn(
            "id-token",
            perms,
            "top-level permissions must not grant id-token; restrict to the draft job",
        )
        self.assertNotIn(
            "attestations",
            perms,
            "top-level permissions must not grant attestations; restrict to the draft job",
        )

    def test_all_new_actions_pinned_by_sha(self) -> None:
        """All actions in the release workflow, including new ones, must use full SHA pins."""
        revisions = action_revisions(self.text)
        self.assertTrue(revisions, "expected at least one pinned action")
        # action_revisions raises if any action lacks a full SHA, so reaching here is the pass.


class SigningIdentityPinTests(unittest.TestCase):
    """The cosign identity and gh attestation verify signer must use the pinned exact form."""

    def _texts(self) -> dict[str, str]:
        return {
            "release-candidate.yml": RELEASE_WORKFLOW.read_text(),
            "README.md": README.read_text(),
            "RELEASING.md": RELEASING.read_text(),
        }

    def test_exact_cosign_identity_present_in_all_files(self) -> None:
        """All three files must contain the exact cosign certificate-identity string."""
        for name, text in self._texts().items():
            with self.subTest(file=name):
                self.assertIn(
                    EXACT_COSIGN_IDENTITY,
                    text,
                    f"{name}: must use exact certificate-identity '{EXACT_COSIGN_IDENTITY}'",
                )

    def test_certificate_identity_regexp_absent_from_all_files(self) -> None:
        """No file may use --certificate-identity-regexp; only the exact flag is allowed."""
        for name, text in self._texts().items():
            with self.subTest(file=name):
                self.assertNotIn(
                    "--certificate-identity-regexp",
                    text,
                    f"{name}: must not use --certificate-identity-regexp; use exact --certificate-identity",
                )

    def test_attestation_verify_carries_signer_workflow_in_all_files(self) -> None:
        """Every gh attestation verify command invocation must include --signer-workflow.

        Prose mentions of 'gh attestation verify' inside backticks (inline code in
        markdown) are not command invocations and are not checked here.
        """
        for name, text in self._texts().items():
            with self.subTest(file=name):
                if "gh attestation verify" not in text:
                    continue
                lines = text.splitlines()
                for i, line in enumerate(lines):
                    if "gh attestation verify" not in line:
                        continue
                    stripped = line.strip()
                    # Skip prose inline-code mentions: the line starts with a backtick
                    # or the command appears inside `` `...` `` rather than as a bare
                    # shell token. A real invocation starts with 'gh' (after optional
                    # whitespace) or is part of a 'for … do … gh …' loop body.
                    is_inline_code = stripped.startswith("`gh") or stripped.startswith("'gh")
                    if is_inline_code:
                        continue
                    # Also include a few following lines for loop-style invocations
                    # where continuation flags appear on the next lines.
                    extended = "\n".join(lines[i:min(i + 8, len(lines))])
                    self.assertIn(
                        SIGNER_WORKFLOW_FLAG,
                        extended,
                        f"{name} line {i + 1}: 'gh attestation verify' must include {SIGNER_WORKFLOW_FLAG}",
                    )


if __name__ == "__main__":
    unittest.main()
