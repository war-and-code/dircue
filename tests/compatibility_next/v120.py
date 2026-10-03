#!/usr/bin/env python3
"""Adjudicate the four reviewed registry-metadata deltas in the v1.2 receipt.

This is deliberately separate from ``minor.py`` and its exact-match verifier.
The input remains the immutable raw comparison receipt: a successful result here
does not turn its 274/278 exact matches into 278/278 matches.
"""

import argparse
import base64
import hashlib
import json
from pathlib import Path
import sys


RAW_SCHEMA = "dircue-minor-compatibility-1"
BASELINE_RELEASE = "1.1.0"
TOTAL_CASES = 278
RAW_EXACT_MATCHES = 274
GROUP_COUNTS = {
    "v020-retained": 118,
    "v030-projects": 55,
    "v030-structure-validation": 6,
    "v030-structure-native": 30,
    "v040-modules": 32,
    "v050-declarations": 37,
}
INTENTIONAL_CASES = {
    "v040-modules-010",
    "v040-modules-011",
    "v040-modules-012",
    "v040-modules-025",
}
EXPECTED_CASE_IDS = (
    {f"legacy-{index:03d}" for index in range(1, 119)}
    | {f"{group}-{index:02d}" for group in ("projects-flat", "projects-repo", "projects-partial", "empty", "nohead") for index in range(1, 11)}
    | {f"projects-revision-{index:02d}" for index in range(1, 6)}
    | {f"structure-validation-{index:02d}" for index in range(1, 7)}
    | {f"structure-breadth-{index:02d}" for index in range(1, 7)}
    | {f"structure-language-{index:02d}" for index in range(1, 22)}
    | {"structure-qualified", "structure-snapshot-git", "structure-snapshot-directory"}
    | {f"v040-modules-{index:03d}" for index in range(1, 33)}
    | {f"v050-declarations-{index:03d}" for index in range(1, 38)}
)
OLD_CONFIGURATIONS = [
    "nuget_config_basename_case_insensitive",
    "npmrc_basename_exact",
]
NEW_CONFIGURATIONS = [
    "cargo_dot_cargo_config_basename_exact",
    "cargo_dot_cargo_config_toml_basename_exact",
    "maven_settings_xml_basename_exact",
    "nuget_config_basename_case_insensitive",
    "npmrc_basename_exact",
]


def _require(condition, message):
    if not condition:
        raise ValueError(message)


def _decode_capture(record, context):
    _require(isinstance(record, dict), f"{context}: capture must be an object")
    _require(type(record.get("exit")) is int, f"{context}: exit must be an integer")
    captured = []
    for stream in ("stdout", "stderr"):
        text_key, b64_key = stream, stream + "_base64"
        _require((text_key in record) != (b64_key in record),
                 f"{context}: expected exactly one {stream} encoding")
        if text_key in record:
            _require(isinstance(record[text_key], str), f"{context}: {stream} must be text")
            raw = record[text_key].encode("utf-8")
        else:
            _require(isinstance(record[b64_key], str), f"{context}: {b64_key} must be text")
            try:
                raw = base64.b64decode(record[b64_key], validate=True)
            except (ValueError, TypeError) as error:
                raise ValueError(f"{context}: invalid {b64_key}") from error
        digest = record.get(stream + "_sha256")
        _require(isinstance(digest, str) and hashlib.sha256(raw).hexdigest() == digest,
                 f"{context}: {stream} digest mismatch")
        captured.append(raw)
    allowed = {"exit", "stdout_sha256", "stderr_sha256", "stdout", "stderr",
               "stdout_base64", "stderr_base64"}
    _require(set(record) <= allowed, f"{context}: unexpected capture field")
    return record["exit"], captured[0], captured[1]


def _json_object(raw, context):
    def reject_duplicates(pairs):
        value = {}
        for key, item in pairs:
            if key in value:
                raise ValueError(f"{context}: duplicate JSON member {key!r}")
            value[key] = item
        return value

    try:
        value = json.loads(raw, object_pairs_hook=reject_duplicates)
    except (UnicodeDecodeError, json.JSONDecodeError) as error:
        raise ValueError(f"{context}: differing output is not valid JSON") from error
    _require(isinstance(value, dict), f"{context}: differing JSON output must be an object")
    return value


def _diff_paths(left, right, prefix=""):
    if type(left) is not type(right):
        return {prefix}
    if isinstance(left, dict):
        paths = set()
        for key in left.keys() | right.keys():
            child = f"{prefix}.{key}" if prefix else str(key)
            if key not in left or key not in right:
                paths.add(child)
            else:
                paths.update(_diff_paths(left[key], right[key], child))
        return paths
    if isinstance(left, list):
        if len(left) != len(right):
            return {prefix}
        paths = set()
        for index, (before, after) in enumerate(zip(left, right)):
            paths.update(_diff_paths(before, after, f"{prefix}[{index}]"))
        return paths
    return set() if left == right else {prefix}


def _check_intentional_case(row):
    case_id = row["id"]
    _require(row["group"] == "v040-modules", f"{case_id}: unexpected group")
    baseline = _decode_capture(row["baseline"], f"{case_id} baseline")
    candidate = _decode_capture(row["candidate"], f"{case_id} candidate")
    _require(baseline[0] == candidate[0], f"{case_id}: exit status changed")
    _require(baseline[2] == candidate[2], f"{case_id}: stderr changed")
    before = _json_object(baseline[1], f"{case_id} baseline stdout")
    after = _json_object(candidate[1], f"{case_id} candidate stdout")
    _require(_diff_paths(before, after) == {
        "registries.rule_version",
        "registries.scope.supported_configurations",
    }, f"{case_id}: JSON changed outside the two adjudicated registry metadata fields")
    _require(before.get("registries", {}).get("rule_version") == "1.0.0",
             f"{case_id}: unexpected baseline registry rule version")
    _require(after.get("registries", {}).get("rule_version") == "1.1.0",
             f"{case_id}: unexpected candidate registry rule version")
    _require(before.get("registries", {}).get("scope", {}).get("supported_configurations")
             == OLD_CONFIGURATIONS, f"{case_id}: unexpected baseline configuration scope")
    _require(after.get("registries", {}).get("scope", {}).get("supported_configurations")
             == NEW_CONFIGURATIONS, f"{case_id}: unexpected candidate configuration scope")


def adjudicate(report):
    """Return an approval record only for the exact, reviewed four-delta receipt."""
    _require(isinstance(report, dict), "receipt must be a JSON object")
    expected_fields = {
        "schema", "started_at_utc", "platform", "baseline_sha256", "candidate_sha256",
        "worker_sha256", "helper_and_external_input_sha256", "untested", "method",
        "cases", "fixtures_sha256", "total", "exact_matches", "groups", "passed",
        "baseline_release",
    }
    _require(set(report) == expected_fields, "unexpected receipt-level field or missing provenance")
    _require(report.get("schema") == RAW_SCHEMA, "unexpected raw receipt schema")
    _require(report.get("baseline_release") == BASELINE_RELEASE,
             "receipt baseline must be the published 1.1.0 release")
    _require(report.get("total") == TOTAL_CASES, "raw receipt must contain all 278 cases")
    _require(report.get("exact_matches") == RAW_EXACT_MATCHES,
             "raw receipt must preserve the observed 274 exact matches")
    _require(report.get("passed") is False, "raw receipt must preserve its failed exact-match result")
    _require(report.get("untested") == [], "receipt must include the complete native-worker matrix")
    _require(report.get("groups") == GROUP_COUNTS, "case group counts changed")
    rows = report.get("cases")
    _require(isinstance(rows, list) and len(rows) == TOTAL_CASES,
             "receipt cases must be a complete 278-row list")
    ids = [row.get("id") if isinstance(row, dict) else None for row in rows]
    _require(all(isinstance(case_id, str) for case_id in ids), "every case must have an ID")
    _require(len(set(ids)) == TOTAL_CASES, "case IDs must be unique")
    _require(set(ids) == EXPECTED_CASE_IDS, "raw case ID set changed")
    differing = {row["id"] for row in rows if row.get("equal") is False}
    _require(differing == INTENTIONAL_CASES, "the set of differing case IDs changed")

    from collections import Counter
    actual_groups = Counter(row.get("group") for row in rows)
    _require(dict(actual_groups) == GROUP_COUNTS, "raw case groups do not match the matrix")
    exact_matches = 0
    for row in rows:
        case_id = row["id"]
        _require(set(row) == {"id", "group", "args", "cwd", "equal", "baseline", "candidate"},
                 f"{case_id}: unexpected case field")
        _require(isinstance(row["args"], list) and all(isinstance(arg, str) for arg in row["args"]),
                 f"{case_id}: invalid argument list")
        _require(isinstance(row["cwd"], str), f"{case_id}: invalid working directory")
        baseline = _decode_capture(row["baseline"], f"{case_id} baseline")
        candidate = _decode_capture(row["candidate"], f"{case_id} candidate")
        same = baseline == candidate
        _require(type(row["equal"]) is bool and row["equal"] == same,
                 f"{case_id}: equality flag does not match raw captures")
        if same:
            exact_matches += 1
            _require(row["baseline"] == row["candidate"],
                     f"{case_id}: equal capture records are not byte-identical")
        elif case_id in INTENTIONAL_CASES:
            _check_intentional_case(row)
        else:
            raise ValueError(f"{case_id}: unapproved output difference")
    _require(exact_matches == RAW_EXACT_MATCHES, "raw exact-match count does not match captures")

    # Receipt-level fields are preserved provenance. This checker accepts only
    # the original receipt shape, so new warnings or metadata require review.
    for key in ("baseline_sha256", "candidate_sha256", "worker_sha256"):
        value = report[key]
        _require(isinstance(value, str) and len(value) == 64
                 and all(char in "0123456789abcdef" for char in value),
                 f"invalid {key}")
    _require(isinstance(report["started_at_utc"], str) and isinstance(report["platform"], str)
             and isinstance(report["method"], str), "invalid receipt provenance")
    _require(isinstance(report["helper_and_external_input_sha256"], dict)
             and isinstance(report["fixtures_sha256"], dict), "invalid input hash maps")

    return {
        "schema": "dircue-v120-compatibility-adjudication-1",
        "approved_intentional_differences": True,
        "baseline_release": BASELINE_RELEASE,
        "raw_receipt": {
            "schema": RAW_SCHEMA,
            "total": TOTAL_CASES,
            "exact_matches": RAW_EXACT_MATCHES,
            "passed": False,
        },
        "approved_case_ids": sorted(INTENTIONAL_CASES),
        "approved_json_paths": [
            "registries.rule_version",
            "registries.scope.supported_configurations",
        ],
        "note": "Four reviewed metadata-only differences are adjudicated; the raw receipt remains 274/278 and did not pass exact compatibility.",
    }


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("receipt", type=Path, help="raw minor.py JSON receipt; never modified")
    args = parser.parse_args(argv)
    try:
        report = json.loads(args.receipt.read_text(encoding="utf-8"))
        result = adjudicate(report)
    except (OSError, UnicodeDecodeError, json.JSONDecodeError, ValueError) as error:
        print(f"v1.2 compatibility adjudication failed: {error}", file=sys.stderr)
        return 1
    print(json.dumps(result, sort_keys=True))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
