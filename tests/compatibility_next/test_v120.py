import hashlib
import importlib.util
import json
from pathlib import Path
import unittest


MODULE_PATH = Path(__file__).with_name("v120.py")
SPEC = importlib.util.spec_from_file_location("compatibility_v120_adjudicator", MODULE_PATH)
v120 = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(v120)


def capture(stdout, stderr=b"", exit_code=0):
    if isinstance(stdout, str):
        stdout = stdout.encode("utf-8")
    result = {"exit": exit_code}
    for name, value in (("stdout", stdout), ("stderr", stderr)):
        result[name] = value.decode("utf-8")
        result[name + "_sha256"] = hashlib.sha256(value).hexdigest()
    return result


def output(rule_version, configurations, extra=None):
    value = {
        "status": "complete",
        "registries": {
            "rule_version": rule_version,
            "scope": {"supported_configurations": configurations},
            "coverage": {"read_files": 2, "observed_declarations": 2},
            "configurations": [
                {"path": ".npmrc", "ecosystem": "npm", "declarations": [
                    {"endpoint": {"status": "origin", "origin": "https://registry.example.invalid"}}
                ]},
                {"path": "NuGet.Config", "ecosystem": "nuget", "declarations": [
                    {"endpoint": {"status": "origin", "origin": "https://packages.example.invalid"}}
                ]},
            ],
        },
    }
    if extra is not None:
        value["other_fact"] = extra
    return json.dumps(value, sort_keys=True, separators=(",", ":")) + "\n"


def make_receipt():
    receipt = {
        "schema": v120.RAW_SCHEMA,
        "started_at_utc": "2026-10-03T00:00:00+00:00",
        "platform": "test-platform",
        "baseline_sha256": "a" * 64,
        "candidate_sha256": "b" * 64,
        "worker_sha256": "c" * 64,
        "helper_and_external_input_sha256": {"helper.py": "d" * 64},
        "untested": [],
        "method": "Exact exit status, stdout and stderr; no normalization.",
        "cases": [],
        "fixtures_sha256": {"fixture": "e" * 64},
        "total": v120.TOTAL_CASES,
        "exact_matches": v120.RAW_EXACT_MATCHES,
        "groups": dict(v120.GROUP_COUNTS),
        "passed": False,
        "baseline_release": v120.BASELINE_RELEASE,
    }
    prefixes = {
        "v020-retained": ("legacy-",),
        "v030-projects": ("projects-flat-", "projects-repo-", "projects-partial-", "empty-", "nohead-", "projects-revision-"),
        "v030-structure-validation": ("structure-validation-",),
        "v030-structure-native": ("structure-breadth-", "structure-language-", "structure-qualified", "structure-snapshot-"),
        "v040-modules": ("v040-modules-",),
        "v050-declarations": ("v050-declarations-",),
    }
    for group, count in v120.GROUP_COUNTS.items():
        case_ids = sorted(case_id for case_id in v120.EXPECTED_CASE_IDS
                          if any(case_id.startswith(prefix) for prefix in prefixes[group]))
        if len(case_ids) != count:
            raise AssertionError(f"bad test fixture IDs for {group}: {len(case_ids)}")
        for index, case_id in enumerate(case_ids):
            if case_id in v120.INTENTIONAL_CASES:
                old_stdout = output("1.0.0", v120.OLD_CONFIGURATIONS)
                new_stdout = output("1.1.0", v120.NEW_CONFIGURATIONS)
                equal = False
            else:
                old_stdout = new_stdout = '{"same":true}\n'
                equal = True
            receipt["cases"].append({
                "id": case_id,
                "group": group,
                "args": ["analyze", "registries", "--json"],
                "cwd": "fixture",
                "equal": equal,
                "baseline": capture(old_stdout),
                "candidate": capture(new_stdout),
            })
    return receipt


def replace_stdout(case, text, side="candidate"):
    record = case[side]
    raw = text.encode("utf-8") if isinstance(text, str) else text
    record["stdout"] = raw.decode("utf-8")
    record["stdout_sha256"] = hashlib.sha256(raw).hexdigest()


class V120AdjudicationTests(unittest.TestCase):
    def test_approves_exactly_four_registry_metadata_differences_without_rewriting_raw_result(self):
        receipt = make_receipt()
        result = v120.adjudicate(receipt)
        self.assertTrue(result["approved_intentional_differences"])
        self.assertEqual(result["baseline_release"], "1.1.0")
        self.assertEqual(result["raw_receipt"], {
            "schema": "dircue-minor-compatibility-1",
            "total": 278,
            "exact_matches": 274,
            "passed": False,
        })
        self.assertEqual(result["approved_case_ids"], sorted(v120.INTENTIONAL_CASES))
        self.assertEqual(receipt["exact_matches"], 274)
        self.assertIs(receipt["passed"], False)

    def assert_rejected(self, receipt):
        with self.assertRaises(ValueError):
            v120.adjudicate(receipt)

    def case(self, receipt, case_id):
        return next(row for row in receipt["cases"] if row["id"] == case_id)

    def test_rejects_new_fact_in_allowed_case(self):
        receipt = make_receipt()
        row = self.case(receipt, "v040-modules-010")
        candidate = json.loads(row["candidate"]["stdout"])
        candidate["registries"]["coverage"]["read_files"] = 3
        replace_stdout(row, json.dumps(candidate, sort_keys=True, separators=(",", ":")) + "\n")
        self.assert_rejected(receipt)

    def test_rejects_wrong_registry_versions_or_configuration_labels(self):
        for mutate in (
            lambda obj: obj["registries"].update(rule_version="1.2.0"),
            lambda obj: obj["registries"]["scope"].update(supported_configurations=["npmrc_basename_exact"]),
        ):
            with self.subTest(mutate=mutate):
                receipt = make_receipt()
                row = self.case(receipt, "v040-modules-010")
                candidate = json.loads(row["candidate"]["stdout"])
                mutate(candidate)
                replace_stdout(row, json.dumps(candidate, sort_keys=True, separators=(",", ":")) + "\n")
                self.assert_rejected(receipt)

    def test_rejects_changed_exit_or_stderr_even_when_json_delta_is_approved(self):
        for field in ("exit", "stderr"):
            with self.subTest(field=field):
                receipt = make_receipt()
                row = self.case(receipt, "v040-modules-010")
                if field == "exit":
                    row["candidate"]["exit"] = 1
                else:
                    row["candidate"]["stderr"] = "warning\n"
                    row["candidate"]["stderr_sha256"] = hashlib.sha256(b"warning\n").hexdigest()
                self.assert_rejected(receipt)

    def test_rejects_non_json_difference_and_capture_digest_tampering(self):
        receipt = make_receipt()
        replace_stdout(self.case(receipt, "v040-modules-010"), "not json\n")
        self.assert_rejected(receipt)
        receipt = make_receipt()
        self.case(receipt, "v040-modules-010")["candidate"]["stdout_sha256"] = "0" * 64
        self.assert_rejected(receipt)

    def test_rejects_duplicate_json_members_in_difference(self):
        receipt = make_receipt()
        row = self.case(receipt, "v040-modules-010")
        text = row["candidate"]["stdout"]
        text = text.replace('"rule_version":"1.1.0"', '"rule_version":"1.2.0","rule_version":"1.1.0"')
        replace_stdout(row, text)
        self.assert_rejected(receipt)

    def test_rejects_unknown_difference_case_and_case_set_changes(self):
        receipt = make_receipt()
        row = self.case(receipt, "v040-modules-010")
        row["id"] = "v040-modules-999"
        self.assert_rejected(receipt)
        receipt = make_receipt()
        receipt["cases"][0]["id"] = "invented-equal-case"
        self.assert_rejected(receipt)
        receipt = make_receipt()
        receipt["cases"].pop()
        self.assert_rejected(receipt)

    def test_rejects_counter_schema_warning_and_provenance_mutations(self):
        mutations = (
            lambda r: r.update(exact_matches=278),
            lambda r: r.update(total=277),
            lambda r: r.update(passed=True),
            lambda r: r.update(schema="dircue-minor-compatibility-2"),
            lambda r: r.update(warnings=[]),
            lambda r: r.update(baseline_release="1.0.1"),
        )
        for mutate in mutations:
            with self.subTest(mutate=mutate):
                receipt = make_receipt()
                mutate(receipt)
                self.assert_rejected(receipt)

    def test_rejects_differences_in_previously_matching_cases(self):
        receipt = make_receipt()
        row = receipt["cases"][0]
        replace_stdout(row, "changed\n")
        row["equal"] = False
        receipt["exact_matches"] -= 1
        self.assert_rejected(receipt)

    def test_cli_reports_adjudication_separately_and_keeps_receipt_bytes(self):
        from contextlib import redirect_stdout
        import io
        import tempfile

        receipt = make_receipt()
        with tempfile.TemporaryDirectory() as temporary:
            path = Path(temporary) / "raw.json"
            raw_bytes = json.dumps(receipt, indent=2).encode("utf-8")
            path.write_bytes(raw_bytes)
            output_buffer = io.StringIO()
            with redirect_stdout(output_buffer):
                self.assertEqual(v120.main([str(path)]), 0)
            self.assertEqual(path.read_bytes(), raw_bytes)
            printed = json.loads(output_buffer.getvalue())
            self.assertTrue(printed["approved_intentional_differences"])
            self.assertEqual(printed["raw_receipt"]["exact_matches"], 274)
            self.assertFalse(printed["raw_receipt"]["passed"])


if __name__ == "__main__":
    unittest.main()
