#!/usr/bin/env python3
"""Suggest follow-ups from a dircue 0.3 project report; never execute them."""

import argparse
import hashlib
import json
import sys
from pathlib import Path

STRUCTURAL_LANGUAGES = frozenset({
    "C", "C++", "C#", "Elixir", "Go", "Groovy", "Java", "JavaScript", "Kotlin",
    "Lua", "Objective-C", "Perl", "PHP", "Python", "Ruby", "Rust", "Shell", "Tcl",
    "TSX", "TypeScript",
})
ROLES = frozenset({
    "source", "test", "configuration", "generated", "vendored", "documentation",
    "data", "binary", "unknown",
})


def require(value, expected, field):
    if type(value) is not expected:
        raise ValueError(f"{field} must be {expected.__name__}")
    return value


def count(value, field):
    require(value, int, field)
    if value < 0:
        raise ValueError(f"{field} must be nonnegative")
    return value


def plan(report):
    """Return evidence-based candidates, not authorization to skip other work."""
    require(report, dict, "report")
    if report.get("schema_version") != "1.2.0":
        raise ValueError("expected aggregate schema 1.2.0 with --projects")
    projects = require(report.get("projects"), dict, "projects")
    if projects.get("source") != "directory":
        raise ValueError("this example requires --source directory")
    status = projects.get("status")
    if status not in {"complete", "partial", "skipped"}:
        raise ValueError("unrecognized projects.status")
    reasons = []
    if status != "complete":
        reasons.append(f"projects_{status}")
    if require(report.get("warnings"), list, "warnings"):
        reasons.append("scan_warnings")
    if require(projects.get("diagnostics"), list, "projects.diagnostics"):
        reasons.append("project_diagnostics")
    if count(projects.get("omitted_files"), "projects.omitted_files"):
        reasons.append("omitted_files")
    ambiguous = require(projects.get("ambiguous"), dict, "projects.ambiguous")
    if count(ambiguous.get("files"), "projects.ambiguous.files"):
        reasons.append("ambiguous_project_attribution")

    roles = {}
    for role in require(projects.get("composition"), list, "projects.composition"):
        require(role, dict, "composition entry")
        name = require(role.get("name"), str, "composition.name")
        files = count(role.get("files"), "composition.files")
        count(role.get("bytes"), "composition.bytes")
        if name in roles:
            raise ValueError(f"duplicate composition role: {name}")
        roles[name] = files
        if name not in ROLES:
            reasons.append("unrecognized_content_role")
    for name in ("unknown", "generated", "vendored", "binary"):
        if roles.get(name, 0):
            reasons.append(f"{name}_content")
    if not sum(roles.values()):
        reasons.append("empty_inventory")

    languages = set()
    language_files = 0
    for language in require(report.get("languages"), list, "languages"):
        require(language, dict, "language entry")
        name = require(language.get("name"), str, "language.name")
        files = count(language.get("file_count"), "language.file_count")
        language_files += files
        if files:
            languages.add(name)
    roots = set()
    for project in require(projects.get("projects"), list, "projects.projects"):
        require(project, dict, "project entry")
        roots.add(require(project.get("root"), str, "project.root"))
    ecosystems = require(report.get("ecosystems"), list, "ecosystems")
    for ecosystem in ecosystems:
        require(ecosystem, dict, "ecosystem entry")
        roots.add(require(ecosystem.get("root"), str, "ecosystem.root"))

    if roles.get("source", 0) + roles.get("test", 0) > language_files:
        reasons.append("source_outside_language_statistics")
    if roles.get("configuration", 0) and not roots:
        reasons.append("configuration_without_project_evidence")

    package_reasons = []
    if roots:
        package_reasons.append("project_or_ecosystem_evidence")
    for role in ("binary", "vendored"):
        if roles.get(role, 0):
            package_reasons.append(f"{role}_content")
    structure_languages = sorted(languages & STRUCTURAL_LANGUAGES)
    if (roles.get("source", 0) or roles.get("test", 0)) and not structure_languages:
        reasons.append("source_without_structural_language_evidence")
    candidates = {
        "metrics": {"suggested": bool(languages), "languages": sorted(languages)},
        "structure": {"suggested": bool(structure_languages),
                      "languages": structure_languages},
        "package_inventory": {"suggested": bool(package_reasons),
                              "project_root_hints": sorted(roots),
                              "reasons": package_reasons},
    }
    return {
        "example_policy_version": 1,
        "input_schema_version": report["schema_version"],
        "source": "directory",
        "project_status": status,
        "review_required": bool(reasons),
        "review_reasons": sorted(set(reasons)),
        "candidates": candidates,
        "no_followup_evidence": not reasons and not any(
            value["suggested"] for value in candidates.values()),
    }


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("report", type=Path, help="successful dircue project JSON report")
    args = parser.parse_args()
    try:
        payload = args.report.read_bytes()
        result = plan(json.loads(payload.decode("utf-8")))
        result["input_report_sha256"] = hashlib.sha256(payload).hexdigest()
    except (OSError, UnicodeError, ValueError, TypeError) as exc:
        print(f"Cannot route report: {exc}", file=sys.stderr)
        return 1
    print(json.dumps(result, indent=2, sort_keys=True))
    return 0


if __name__ == "__main__":
    sys.exit(main())
