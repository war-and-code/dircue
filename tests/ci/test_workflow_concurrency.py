#!/usr/bin/env python3
"""Regression tests for pull-request workflow concurrency lanes."""

from __future__ import annotations

from dataclasses import dataclass
from pathlib import Path
import re
import unittest


ROOT = Path(__file__).resolve().parents[2]
WORKFLOWS = {
    "CI": ROOT / ".github/workflows/ci.yml",
    "Structural worker": ROOT / ".github/workflows/structural-worker.yml",
    "Structural prototype": ROOT / ".github/workflows/structural-prototype.yml",
}
HEAD_SHA = "0a619c4b8563a84b85bc1e5068e55fe6d23c4351"


def concurrency_group(path: Path) -> str:
    lines = path.read_text().splitlines()
    start = lines.index("concurrency:")
    if lines[start + 1].strip() != "group: >-":
        raise AssertionError(f"{path}: concurrency group must use a folded scalar")
    expression = []
    for line in lines[start + 2 :]:
        if line and not line.startswith("    "):
            break
        if line.strip():
            expression.append(line.strip())
    return " ".join(expression)


def cancel_in_progress(path: Path) -> bool:
    match = re.search(r"^  cancel-in-progress: (true|false)$", path.read_text(), re.MULTILINE)
    if not match:
        raise AssertionError(f"{path}: missing literal cancel-in-progress value")
    return match.group(1) == "true"


def job_conditions(path: Path) -> dict[str, str | None]:
    conditions: dict[str, str | None] = {}
    current: str | None = None
    in_jobs = False
    for line in path.read_text().splitlines():
        if line == "jobs:":
            in_jobs = True
            continue
        if not in_jobs:
            continue
        job = re.fullmatch(r"  ([A-Za-z0-9_-]+):", line)
        if job:
            current = job.group(1)
            conditions[current] = None
            continue
        condition = re.fullmatch(r"    if: (.+)", line)
        if condition and current:
            conditions[current] = condition.group(1)
    return conditions


TOKEN = re.compile(
    r"\s*(?:(?P<string>'(?:[^'\\]|\\.)*')|(?P<op>\&\&|\|\||==|!=|[!()])|"
    r"(?P<identifier>[A-Za-z_][A-Za-z0-9_.]*))"
)


class Expression:
    """Small evaluator for the operators used by these workflow conditions."""

    def __init__(self, source: str, context: dict[str, object]) -> None:
        self.context = context
        self.tokens: list[tuple[str, str]] = []
        position = 0
        while position < len(source):
            if source[position:].isspace():
                break
            match = TOKEN.match(source, position)
            if not match:
                raise AssertionError(f"unsupported expression near {source[position:]!r}")
            kind = "string" if match.group("string") else "op" if match.group("op") else "identifier"
            self.tokens.append((kind, match.group(kind)))
            position = match.end()
        self.tokens.append(("end", ""))
        self.position = 0

    def evaluate(self) -> object:
        value = self.parse_or()
        if self.peek() != "":
            raise AssertionError(f"unexpected token {self.peek()!r}")
        return value

    def peek(self) -> str:
        return self.tokens[self.position][1]

    def take(self, value: str | None = None) -> tuple[str, str]:
        token = self.tokens[self.position]
        if value is not None and token[1] != value:
            raise AssertionError(f"expected {value!r}, got {token[1]!r}")
        self.position += 1
        return token

    def parse_or(self) -> object:
        left = self.parse_and()
        while self.peek() == "||":
            self.take("||")
            right = self.parse_and()
            left = left if truthy(left) else right
        return left

    def parse_and(self) -> object:
        left = self.parse_comparison()
        while self.peek() == "&&":
            self.take("&&")
            right = self.parse_comparison()
            left = right if truthy(left) else left
        return left

    def parse_comparison(self) -> object:
        left = self.parse_unary()
        while self.peek() in ("==", "!="):
            operator = self.take()[1]
            right = self.parse_unary()
            left = left == right if operator == "==" else left != right
        return left

    def parse_unary(self) -> object:
        if self.peek() == "!":
            self.take("!")
            return not truthy(self.parse_unary())
        if self.peek() == "(":
            self.take("(")
            value = self.parse_or()
            self.take(")")
            return value
        kind, value = self.take()
        if kind == "string":
            return value[1:-1].replace("\\'", "'").replace("\\\\", "\\")
        if kind == "identifier":
            return resolve(self.context, value)
        raise AssertionError(f"expected a value, got {value!r}")


def resolve(context: dict[str, object], path: str) -> object:
    value: object = context
    for part in path.split("."):
        if not isinstance(value, dict):
            return None
        value = value.get(part)
    return value


def truthy(value: object) -> bool:
    return value not in (None, False, 0, "")


def render(template: str, context: dict[str, object]) -> str:
    def replace(match: re.Match[str]) -> str:
        value = Expression(match.group(1), context).evaluate()
        if isinstance(value, bool):
            return str(value).lower()
        return "" if value is None else str(value)

    return re.sub(r"\$\{\{(.*?)}}", replace, template)


def pull_request_context(workflow: str, action: str, draft: bool) -> dict[str, object]:
    return {
        "github": {
            "workflow": workflow,
            "event_name": "pull_request",
            "ref": "refs/pull/51/merge",
            "event": {
                "action": action,
                "pull_request": {
                    "number": 51,
                    "draft": draft,
                    "head": {"sha": HEAD_SHA},
                },
            },
        }
    }


def non_pr_context(workflow: str, event_name: str, ref: str) -> dict[str, object]:
    return {
        "github": {
            "workflow": workflow,
            "event_name": event_name,
            "ref": ref,
            "event": {},
        }
    }


def selected_jobs(path: Path, context: dict[str, object]) -> set[str]:
    return {
        job
        for job, condition in job_conditions(path).items()
        if condition is None or truthy(Expression(condition, context).evaluate())
    }


@dataclass(frozen=True)
class Run:
    name: str
    group: str
    head_sha: str = HEAD_SHA


class ConcurrencyState:
    """Deterministic model of one running and one pending run per group."""

    def __init__(self) -> None:
        self.running: dict[str, Run] = {}
        self.pending: dict[str, Run] = {}
        self.cancelled: list[Run] = []

    def seed_running(self, run: Run) -> None:
        self.running[run.group] = run

    def seed_pending(self, run: Run) -> None:
        self.pending[run.group] = run

    def admit(self, run: Run, *, cancel_in_progress: bool = True) -> None:
        old_pending = self.pending.pop(run.group, None)
        if old_pending:
            self.cancelled.append(old_pending)
        old_running = self.running.get(run.group)
        if old_running:
            if cancel_in_progress:
                self.cancelled.append(old_running)
            self.pending[run.group] = run
        else:
            self.running[run.group] = run


class WorkflowExpressionTests(unittest.TestCase):
    def test_event_contexts_select_the_expected_lane_in_all_workflows(self) -> None:
        cases = (
            ("opened", True, "draft"),
            ("reopened", True, "draft"),
            ("synchronize", True, "draft"),
            ("synchronize", False, "full"),
            ("ready_for_review", False, "full"),
            ("converted_to_draft", True, "full"),
        )
        for workflow, path in WORKFLOWS.items():
            expression = concurrency_group(path)
            for action, draft, lane in cases:
                with self.subTest(workflow=workflow, action=action, draft=draft):
                    context = pull_request_context(workflow, action, draft)
                    self.assertEqual(f"{workflow}-51-{lane}", render(expression, context))

    def test_non_pr_events_keep_ref_keys_and_use_the_full_lane(self) -> None:
        contexts = (
            ("CI", "push", "refs/heads/main"),
            ("CI", "workflow_dispatch", "refs/heads/topic"),
            ("Structural worker", "workflow_dispatch", "refs/heads/topic"),
            ("Structural prototype", "workflow_dispatch", "refs/heads/topic"),
        )
        for workflow, event_name, ref in contexts:
            with self.subTest(workflow=workflow, event_name=event_name):
                context = non_pr_context(workflow, event_name, ref)
                self.assertEqual(
                    f"{workflow}-{ref}-full",
                    render(concurrency_group(WORKFLOWS[workflow]), context),
                )

    def test_draft_events_retain_inexpensive_job_selection(self) -> None:
        for workflow, path in WORKFLOWS.items():
            for action in ("synchronize", "converted_to_draft"):
                with self.subTest(workflow=workflow, action=action):
                    context = pull_request_context(workflow, action, True)
                    expected = {"preflight"} if workflow == "CI" else set()
                    self.assertEqual(expected, selected_jobs(path, context))

    def test_full_pr_events_retain_expensive_job_selection(self) -> None:
        expected = {
            "CI": {"preflight", "test", "linguist-conformance", "metrics-conformance", "map-diff-dogfood", "counter-regression"},
            "Structural worker": {"package"},
            "Structural prototype": {"prototype"},
        }
        for workflow, path in WORKFLOWS.items():
            with self.subTest(workflow=workflow):
                context = pull_request_context(workflow, "ready_for_review", False)
                self.assertEqual(expected[workflow], selected_jobs(path, context))

    def test_main_push_runs_linux_validation_without_five_platform_worker_matrix(self) -> None:
        ci = non_pr_context("CI", "push", "refs/heads/main")
        worker = non_pr_context("Structural worker", "push", "refs/heads/main")
        self.assertEqual(
            {"preflight", "test", "linguist-conformance", "metrics-conformance"},
            selected_jobs(WORKFLOWS["CI"], ci),
        )
        self.assertEqual({"package"}, selected_jobs(WORKFLOWS["Structural worker"], worker))
        workflow = WORKFLOWS["Structural worker"].read_text()
        self.assertIn("github.event_name == 'push'", workflow)
        self.assertEqual(2, workflow.count('"platform":"linux-amd64"'))
        self.assertEqual(1, workflow.count('"platform":"linux-arm64"'))

    def test_native_structural_matrix_smokes_an_installed_release_wheel(self) -> None:
        workflow = WORKFLOWS["Structural worker"].read_text()
        self.assertIn("DIRCUE_CI_VERSION: '0.8.0-rc.1'", workflow)
        self.assertIn("scripts/release.py --version", workflow)
        self.assertIn("scripts/wheels.py --release-dir .cache/native-release/core", workflow)
        self.assertIn("scripts/wheel_release_smoke.py --release-dir .cache/native-release/core", workflow)
        self.assertLess(workflow.index("scripts/release.py --version"), workflow.index("Install pinned Rust toolchain"))
        self.assertIn("github.event_name == 'push'", workflow)
        self.assertIn('"release_target":"linux/amd64"', workflow)
        self.assertIn('"release_target":"linux/arm64"', workflow)
        self.assertIn('"release_target":"darwin/arm64"', workflow)
        self.assertIn('"release_target":"darwin/amd64"', workflow)
        self.assertIn('"release_target":"windows/amd64"', workflow)
        self.assertNotIn("gh release create", workflow)

    def test_cancel_in_progress_remains_enabled_in_all_workflows(self) -> None:
        for workflow, path in WORKFLOWS.items():
            with self.subTest(workflow=workflow):
                self.assertTrue(cancel_in_progress(path))


class ConcurrencyAdmissionTests(unittest.TestCase):
    def groups(self, workflow: str = "CI") -> tuple[str, str]:
        expression = concurrency_group(WORKFLOWS[workflow])
        full = render(expression, pull_request_context(workflow, "ready_for_review", False))
        draft = render(expression, pull_request_context(workflow, "synchronize", True))
        return full, draft

    def test_running_full_run_survives_later_ordinary_draft_update(self) -> None:
        for workflow in WORKFLOWS:
            with self.subTest(workflow=workflow):
                full_group, draft_group = self.groups(workflow)
                state = ConcurrencyState()
                full = Run("ready", full_group)
                draft = Run("delayed-draft-update", draft_group)
                state.seed_running(full)
                state.admit(draft)
                self.assertEqual(full.head_sha, draft.head_sha)
                self.assertIs(state.running[full_group], full)
                self.assertNotIn(full, state.cancelled)
                self.assertIs(state.running[draft_group], draft)

    def test_pending_full_run_survives_later_ordinary_draft_update(self) -> None:
        for workflow in WORKFLOWS:
            with self.subTest(workflow=workflow):
                full_group, draft_group = self.groups(workflow)
                state = ConcurrencyState()
                full = Run("ready-pending", full_group)
                draft = Run("delayed-draft-update", draft_group)
                state.seed_pending(full)
                state.admit(draft)
                self.assertEqual(full.head_sha, draft.head_sha)
                self.assertIs(state.pending[full_group], full)
                self.assertNotIn(full, state.cancelled)

    def test_successive_draft_updates_coalesce_in_the_draft_lane(self) -> None:
        _, draft_group = self.groups()
        state = ConcurrencyState()
        first = Run("draft-1", draft_group)
        second = Run("draft-2", draft_group)
        state.seed_running(first)
        state.admit(second)
        self.assertIn(first, state.cancelled)
        self.assertIs(state.pending[draft_group], second)

    def test_non_draft_update_replaces_older_full_validation(self) -> None:
        full_group, _ = self.groups()
        state = ConcurrencyState()
        old = Run("ready", full_group)
        replacement = Run("non-draft-synchronize", full_group)
        state.seed_running(old)
        state.admit(replacement)
        self.assertIn(old, state.cancelled)
        self.assertIs(state.pending[full_group], replacement)

    def test_converted_to_draft_cancels_full_without_expensive_jobs(self) -> None:
        for workflow, path in WORKFLOWS.items():
            with self.subTest(workflow=workflow):
                expression = concurrency_group(path)
                context = pull_request_context(workflow, "converted_to_draft", True)
                converted_group = render(expression, context)
                full_group, _ = self.groups(workflow)
                self.assertEqual(full_group, converted_group)
                expected = {"preflight"} if workflow == "CI" else set()
                self.assertEqual(expected, selected_jobs(path, context))

                state = ConcurrencyState()
                old = Run("ready", full_group)
                converted = Run("converted-to-draft", converted_group)
                state.seed_running(old)
                state.admit(converted, cancel_in_progress=cancel_in_progress(path))
                self.assertIn(old, state.cancelled)

    def test_promotion_starts_full_validation_in_a_separate_lane(self) -> None:
        full_group, draft_group = self.groups()
        state = ConcurrencyState()
        draft = Run("draft-update", draft_group)
        promotion = Run("ready-for-review", full_group)
        state.seed_running(draft)
        state.admit(promotion)
        self.assertIs(state.running[full_group], promotion)
        self.assertNotIn(draft, state.cancelled)

    def test_workflow_names_keep_concurrency_isolated(self) -> None:
        state = ConcurrencyState()
        groups = [self.groups(workflow)[0] for workflow in WORKFLOWS]
        for workflow, group in zip(WORKFLOWS, groups, strict=True):
            state.admit(Run(workflow, group))
        self.assertEqual(len(WORKFLOWS), len(state.running))
        self.assertEqual([], state.cancelled)


if __name__ == "__main__":
    unittest.main()
