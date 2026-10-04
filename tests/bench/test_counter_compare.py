import importlib.util
import io
import json
import os
import subprocess
import tempfile
import unittest
from contextlib import redirect_stderr, redirect_stdout
from pathlib import Path
from unittest import mock

spec = importlib.util.spec_from_file_location("counter_compare", Path(__file__).with_name("counter_compare.py"))
counter_compare = importlib.util.module_from_spec(spec)
spec.loader.exec_module(counter_compare)

FIXTURES = Path(__file__).parents[1] / "map_corpus" / "fixtures"
BUDGET = Path(__file__).with_name("counter_budget.json")


def stats_doc(**overrides):
    doc = {
        "schema_version": "1.0.0",
        "kind": "run-stats",
        "command": "map",
        "source_mode": "directory",
        "deterministic_costs": {
            "files_enumerated": 3,
            "files_content_read": 2,
            "bytes_requested": 128,
            "limit_hits": {"file_bytes": 0, "tree_size": 0},
        },
        "measurements": {"wall_time_ns": 1},
    }
    for key, value in overrides.items():
        doc[key] = value
    return doc


class CounterCompareTests(unittest.TestCase):
    def test_validate_stats_returns_only_known_deterministic_counters(self):
        counters = counter_compare.validate_stats(stats_doc(), "dircue", Path("fixture"))
        self.assertEqual({
            "files_enumerated": 3,
            "files_content_read": 2,
            "bytes_requested": 128,
            "limit_hits.file_bytes": 0,
            "limit_hits.tree_size": 0,
        }, counters)

    def test_rejects_missing_and_unknown_top_level_and_counter_fields(self):
        cases = [
            (lambda doc: doc.pop("measurements"), "missing stats fields: measurements"),
            (lambda doc: doc.update({"future": {}}), "unknown stats fields: future"),
            (lambda doc: doc["deterministic_costs"].pop("bytes_requested"), "missing counters: bytes_requested"),
            (lambda doc: doc["deterministic_costs"].update({"future_counter": 4}), "unknown counters: future_counter"),
            (lambda doc: doc["deterministic_costs"]["limit_hits"].update({"future": 0}), "unknown counters: limit_hits.future"),
            (lambda doc: doc["deterministic_costs"]["limit_hits"].pop("tree_size"), "missing counters: limit_hits.tree_size"),
        ]
        for mutate, expected in cases:
            with self.subTest(expected=expected):
                doc = stats_doc()
                mutate(doc)
                with self.assertRaisesRegex(counter_compare.GateError, expected):
                    counter_compare.validate_stats(doc, "dircue", Path("fx"))

    def test_rejects_wrong_metadata(self):
        for field, value in (("schema_version", "2.0.0"), ("kind", "map"),
                             ("command", "forest"), ("source_mode", "auto")):
            with self.subTest(field=field):
                doc = stats_doc(**{field: value})
                with self.assertRaisesRegex(counter_compare.GateError, field):
                    counter_compare.validate_stats(doc, "dircue", Path("fx"))

    def test_rejects_wrong_shapes_and_invalid_counter_values(self):
        bad_values = (True, -1, 1.5, "3")
        for value in bad_values:
            with self.subTest(value=value):
                doc = stats_doc()
                doc["deterministic_costs"]["files_enumerated"] = value
                with self.assertRaisesRegex(counter_compare.GateError, "non-negative integer"):
                    counter_compare.validate_stats(doc, "dircue", Path("fx"))
        for value in (None, [], "bad"):
            with self.subTest(limit_hits=value):
                doc = stats_doc()
                doc["deterministic_costs"]["limit_hits"] = value
                with self.assertRaisesRegex(counter_compare.GateError, "limit_hits must be a JSON object"):
                    counter_compare.validate_stats(doc, "dircue", Path("fx"))

    def test_rejects_counter_inconsistencies(self):
        doc = stats_doc()
        doc["deterministic_costs"]["files_content_read"] = 4
        with self.assertRaisesRegex(counter_compare.GateError, "exceeds files_enumerated"):
            counter_compare.validate_stats(doc, "dircue", Path("fx"))
        doc = stats_doc()
        doc["deterministic_costs"]["limit_hits"]["tree_size"] = 2
        with self.assertRaisesRegex(counter_compare.GateError, "must be 0 or 1"):
            counter_compare.validate_stats(doc, "dircue", Path("fx"))

    def test_inventory_is_exact_and_fixture_bytes_are_pinned(self):
        budget = counter_compare.load_budget(BUDGET)
        fixtures = sorted(path for path in FIXTURES.iterdir() if path.is_dir())
        inventory = counter_compare.validate_inventory(fixtures, budget)
        self.assertEqual(16, len(inventory))
        with self.assertRaisesRegex(counter_compare.GateError, "missing fixtures"):
            counter_compare.validate_inventory(fixtures[:-1], budget)
        with self.assertRaisesRegex(counter_compare.GateError, "unbudgeted fixtures"):
            counter_compare.validate_inventory(fixtures + [fixtures[0].parent / "extra"], budget)

    def test_fixture_digest_covers_regular_bytes_and_symlink_target(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp) / "fixture"
            root.mkdir()
            (root / "input").write_bytes(b"one")
            initial = counter_compare.fixture_digest(root)
            (root / "input").write_bytes(b"two")
            self.assertNotEqual(initial, counter_compare.fixture_digest(root))
            (root / "input").unlink()
            (root / "target-a").write_text("same")
            (root / "link").symlink_to("target-a")
            link_digest = counter_compare.fixture_digest(root)
            (root / "link").unlink()
            (root / "link").symlink_to("target-b")
            self.assertNotEqual(link_digest, counter_compare.fixture_digest(root))

    @unittest.skipUnless(hasattr(os, "mkfifo"), "requires named pipes")
    def test_fixture_digest_rejects_unsupported_filesystem_objects_without_reading(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp) / "fixture"
            root.mkdir()
            os.mkfifo(root / "pipe")
            with self.assertRaisesRegex(counter_compare.GateError, "unsupported filesystem object: pipe"):
                counter_compare.fixture_digest(root)

    def test_fixture_digest_wraps_filesystem_errors(self):
        with self.assertRaisesRegex(counter_compare.GateError, "cannot hash fixture"):
            counter_compare.fixture_digest(Path("/definitely/not/a/fixture"))

    def test_growth_policy_ceilings_use_25_percent_and_absolute_floors(self):
        budget = counter_compare.load_budget(BUDGET)
        items = {item["id"]: item for item in budget["fixtures"]}
        ceiling = counter_compare.ceilings(items["flask-entry-point"], budget["growth_policy"])
        self.assertEqual(4, ceiling["files_enumerated"])
        self.assertEqual(378, ceiling["bytes_requested"])
        self.assertEqual(0, ceiling["limit_hits.file_bytes"])
        self.assertEqual(0, ceiling["limit_hits.tree_size"])
        head = dict(items["flask-entry-point"]["baseline"])
        head["bytes_requested"] = 379
        self.assertGreater(head["bytes_requested"], ceiling["bytes_requested"])
        self.assertEqual([("bytes_requested", 379, 378)],
                         counter_compare.budget_violations(head, ceiling))
        head["bytes_requested"] = ceiling["bytes_requested"]
        head["limit_hits.tree_size"] = 1
        self.assertEqual([("limit_hits.tree_size", 1, 0)],
                         counter_compare.budget_violations(head, ceiling))

    def test_budget_rejects_non_integer_schema_and_duplicate_json_keys(self):
        with tempfile.TemporaryDirectory() as tmp:
            path = Path(tmp) / "budget.json"
            for version in (True, 1.0):
                path.write_text(json.dumps({"schema_version": version}))
                with self.subTest(version=version), self.assertRaisesRegex(counter_compare.GateError, "schema_version"):
                    counter_compare.load_budget(path)
            path.write_text('{"schema_version": 1, "schema_version": 1}')
            with self.assertRaisesRegex(counter_compare.GateError, "duplicate JSON object key"):
                counter_compare.load_budget(path)
            budget = counter_compare.load_budget(BUDGET)
            for field, value in (("percent", 25.0), ("files_absolute_floor", 2.0),
                                 ("bytes_absolute_floor", True)):
                malformed = dict(budget)
                malformed["growth_policy"] = dict(budget["growth_policy"])
                malformed["growth_policy"][field] = value
                path.write_text(json.dumps(malformed))
                with self.subTest(field=field), self.assertRaisesRegex(counter_compare.GateError, "growth"):
                    counter_compare.load_budget(path)

    def test_budget_rejects_invalid_digest_and_nonzero_limit_hit_baseline(self):
        budget = counter_compare.load_budget(BUDGET)
        item = dict(budget["fixtures"][0])
        item["id"] = "broken-hash"
        item["input_sha256"] = "g" * 64
        with self.assertRaisesRegex(counter_compare.GateError, "valid input_sha256"):
            counter_compare.validate_inventory([FIXTURES / item["id"]], {"fixtures": [item]})
        item = dict(budget["fixtures"][0])
        item["baseline"] = dict(item["baseline"])
        item["baseline"]["limit_hits.file_bytes"] = 1
        with self.assertRaisesRegex(counter_compare.GateError, "must be zero"):
            counter_compare.ceilings(item, budget["growth_policy"])
        for value in (True, 1.0):
            invalid = dict(budget["fixtures"][0])
            invalid["baseline"] = dict(invalid["baseline"])
            invalid["baseline"]["files_enumerated"] = value
            with self.subTest(value=value), self.assertRaisesRegex(counter_compare.GateError, "invalid baseline"):
                counter_compare.ceilings(invalid, budget["growth_policy"])

    def test_growth_ceiling_preserves_large_integer_precision(self):
        budget = counter_compare.load_budget(BUDGET)
        for value in (2**53 + 1, 2**60 + 1, 2**64 - 1):
            item = json.loads(json.dumps(budget["fixtures"][0]))
            item["baseline"]["bytes_requested"] = value
            expected = value + (value + 3) // 4
            with self.subTest(value=value):
                self.assertEqual(expected, counter_compare.ceilings(item, budget["growth_policy"])["bytes_requested"])

    def test_all_budget_baselines_are_validated_before_starting_binaries(self):
        mutations = (
            (lambda item: item["baseline"].update(bytes_requested="typo"), "invalid baseline"),
            (lambda item: item["baseline"].pop("bytes_requested"), "exactly the known counters"),
            (lambda item: item["baseline"].update(files_content_read=100), "exceeds files_enumerated"),
            (lambda item: item["baseline"].update(files_content_read=0), "no files were content-read"),
            (lambda item: item.update(input_sha256="bad"), "valid input_sha256"),
        )
        fixture_paths = sorted(path for path in FIXTURES.iterdir() if path.is_dir())
        with tempfile.TemporaryDirectory() as tmp:
            path = Path(tmp) / "budget.json"
            for mutate, diagnostic in mutations:
                budget = counter_compare.load_budget(BUDGET)
                # The last entry matters: validation must not wait for its turn
                # after earlier fixtures have already executed both binaries.
                mutate(budget["fixtures"][-1])
                path.write_text(json.dumps(budget))
                with self.subTest(diagnostic=diagnostic), \
                     mock.patch.object(counter_compare.smoke_process, "run") as run, \
                     redirect_stdout(io.StringIO()), redirect_stderr(io.StringIO()) as stderr:
                    status = counter_compare.main([
                        "--base", "base", "--head", "head", "--budget", str(path),
                        *map(str, fixture_paths),
                    ])
                    self.assertEqual(1, status)
                    self.assertIn(diagnostic, stderr.getvalue())
                    run.assert_not_called()

    def test_deep_json_fails_with_a_gate_diagnostic(self):
        deep_json = "[" * 20_000 + "0" + "]" * 20_000
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            path = root / "budget.json"
            path.write_text(deep_json)
            with self.assertRaisesRegex(counter_compare.GateError, "cannot read budget"):
                counter_compare.load_budget(path)

            def malformed_stats(command, **kwargs):
                Path(command[command.index("--stats-json") + 1]).write_text(deep_json)
                return subprocess.CompletedProcess(command, 0, "", "")

            with mock.patch.object(counter_compare.smoke_process, "run", side_effect=malformed_stats), \
                 self.assertRaisesRegex(counter_compare.GateError, "invalid stats JSON"):
                counter_compare.counters("dircue", FIXTURES / "non-source", root)

    def test_counter_command_fails_closed_on_process_or_stats_errors(self):
        fixture = FIXTURES / "non-source"
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            command = ["dircue", "map", "--stats-json", str(root / "stats.json"), str(fixture)]
            with mock.patch.object(counter_compare.smoke_process, "run",
                                   return_value=subprocess.CompletedProcess(command, 3, "", "bad input")):
                with self.assertRaisesRegex(counter_compare.GateError, "bad input"):
                    counter_compare.counters("dircue", fixture, root)
            with mock.patch.object(counter_compare.smoke_process, "run", side_effect=OSError("missing binary")):
                with self.assertRaisesRegex(counter_compare.GateError, "cannot run"):
                    counter_compare.counters("dircue", fixture, root)
            with mock.patch.object(counter_compare.smoke_process, "run",
                                   side_effect=subprocess.TimeoutExpired(command, 60)):
                with self.assertRaisesRegex(counter_compare.GateError, "timed out"):
                    counter_compare.counters("dircue", fixture, root)

            def malformed_stats(cmd, **kwargs):
                Path(cmd[cmd.index("--stats-json") + 1]).write_text("{")
                return subprocess.CompletedProcess(cmd, 0, "", "")

            with mock.patch.object(counter_compare.smoke_process, "run", side_effect=malformed_stats):
                with self.assertRaisesRegex(counter_compare.GateError, "invalid stats JSON"):
                    counter_compare.counters("dircue", fixture, root)

            with mock.patch.object(counter_compare.smoke_process, "run",
                                   return_value=subprocess.CompletedProcess(command, 0, "", "")):
                with self.assertRaisesRegex(counter_compare.GateError, "did not write stats JSON"):
                    counter_compare.counters("dircue", fixture, root)

    def test_main_fails_on_budget_overflow_and_renders_diagnostic(self):
        budget = counter_compare.load_budget(BUDGET)
        items = {item["id"]: item for item in budget["fixtures"]}
        fixture_paths = sorted(path for path in FIXTURES.iterdir() if path.is_dir())

        def run_with_stats(command, **kwargs):
            binary, fixture = command[0], Path(command[-1])
            counters = dict(items[fixture.name]["baseline"])
            if binary == "head" and fixture.name == "flask-entry-point":
                counters["files_enumerated"] = 5
                counters["files_content_read"] = 5
            doc = stats_doc(deterministic_costs={
                "files_enumerated": counters["files_enumerated"],
                "files_content_read": counters["files_content_read"],
                "bytes_requested": counters["bytes_requested"],
                "limit_hits": {
                    "file_bytes": counters["limit_hits.file_bytes"],
                    "tree_size": counters["limit_hits.tree_size"],
                },
            })
            Path(command[command.index("--stats-json") + 1]).write_text(json.dumps(doc))
            return subprocess.CompletedProcess(command, 0, "", "")

        stdout, stderr = io.StringIO(), io.StringIO()
        with mock.patch.object(counter_compare.smoke_process, "run", side_effect=run_with_stats), \
             redirect_stdout(stdout), redirect_stderr(stderr):
            status = counter_compare.main([
                "--base", "base", "--head", "head", "--budget", str(BUDGET),
                *map(str, fixture_paths),
            ])
        self.assertEqual(1, status)
        self.assertIn("| `flask-entry-point` | `files_enumerated` | 2 | 5 | +3 | 4 | FAIL |", stdout.getvalue())
        self.assertEqual("", stderr.getvalue())

    def test_main_fails_closed_for_missing_fixture_malformed_stats_and_missing_stats(self):
        fixture_paths = sorted(path for path in FIXTURES.iterdir() if path.is_dir())
        stdout, stderr = io.StringIO(), io.StringIO()
        with redirect_stdout(stdout), redirect_stderr(stderr):
            status = counter_compare.main([
                "--base", "base", "--head", "head", "--budget", str(BUDGET),
                *map(str, fixture_paths[:-1]),
            ])
        self.assertEqual(1, status)
        self.assertIn("missing fixtures", stderr.getvalue())

        def malformed_stats(command, **kwargs):
            Path(command[command.index("--stats-json") + 1]).write_text("{")
            return subprocess.CompletedProcess(command, 0, "", "")

        for runner, diagnostic in (
            (malformed_stats, "invalid stats JSON"),
            (lambda command, **kwargs: subprocess.CompletedProcess(command, 0, "", ""),
             "did not write stats JSON"),
        ):
            stdout, stderr = io.StringIO(), io.StringIO()
            with mock.patch.object(counter_compare.smoke_process, "run", side_effect=runner), \
                 redirect_stdout(stdout), redirect_stderr(stderr):
                status = counter_compare.main([
                    "--base", "base", "--head", "head", "--budget", str(BUDGET),
                    *map(str, fixture_paths),
                ])
            with self.subTest(diagnostic=diagnostic):
                self.assertEqual(1, status)
                self.assertIn(diagnostic, stderr.getvalue())

    def test_main_rechecks_fixture_digest_after_binaries_run(self):
        budget = counter_compare.load_budget(BUDGET)
        items = {item["id"]: item for item in budget["fixtures"]}
        fixture_paths = sorted(path for path in FIXTURES.iterdir() if path.is_dir())

        def run_with_stats(command, **kwargs):
            counters = items[Path(command[-1]).name]["baseline"]
            doc = stats_doc(deterministic_costs={
                "files_enumerated": counters["files_enumerated"],
                "files_content_read": counters["files_content_read"],
                "bytes_requested": counters["bytes_requested"],
                "limit_hits": {"file_bytes": 0, "tree_size": 0},
            })
            Path(command[command.index("--stats-json") + 1]).write_text(json.dumps(doc))
            return subprocess.CompletedProcess(command, 0, "", "")

        original_digest = counter_compare.fixture_digest
        calls = 0

        def changed_after_first_check(path):
            nonlocal calls
            calls += 1
            digest = original_digest(path)
            return "0" * 64 if calls > len(fixture_paths) else digest

        stdout, stderr = io.StringIO(), io.StringIO()
        with mock.patch.object(counter_compare, "fixture_digest", side_effect=changed_after_first_check), \
             mock.patch.object(counter_compare.smoke_process, "run", side_effect=run_with_stats), \
             redirect_stdout(stdout), redirect_stderr(stderr):
            status = counter_compare.main([
                "--base", "base", "--head", "head", "--budget", str(BUDGET),
                *map(str, fixture_paths),
            ])
        self.assertEqual(1, status)
        self.assertIn("fixture input digest changed", stderr.getvalue())


if __name__ == "__main__":
    unittest.main()
