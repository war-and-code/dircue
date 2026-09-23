"""Discrepancy classification for the dircue parity atlas.

Every per-file disagreement between dircue and an oracle gets one of these
categories:

    matched                  No discrepancy.
    dircue-bug               dircue produces a different result not covered by any
                             known upstream difference.
    known-upstream-diff      The disagreement is in the documented list of upstream
                             differences between dircue and the oracle (e.g. raw-string
                             comment counting in scc 4.1.0, or a Linguist strategy that
                             differs from Enry).
    harness-issue            The harness produced unreliable oracle output (e.g.
                             the container reported a gem-version mismatch, or scc
                             could not read a file).
    oracle-version-mismatch  The oracle version does not match the pinned version; abort.

A mismatch row that cannot be classified into any known-upstream-diff or
harness-issue becomes `dircue-bug`.
"""

from __future__ import annotations

import re
from typing import Any

# ---------------------------------------------------------------------------
# Known upstream differences
# ---------------------------------------------------------------------------

# scc 4.1.0: counts the first // line inside a Java or C# raw-string literal as
# a comment even though it is raw-string content.
_SCC_RAW_STRING_COMMENT_PATTERN = re.compile(
    r"^.+\.(java|cs)$", re.IGNORECASE
)

# scc 4.1.0 raw-string paths that have been seen in the fixed corpus fixture
_SCC_RAW_STRING_FIXED_PATHS = {
    "src/RawStringLimitation.java",
    "src/RawStringLimitation.cs",
}

# Linguist 9.7.0 known differences from Enry (dircue's classifier):
# - Strategy chain ordering differences in some edge cases
# - Some vendored/generated detection differences
# These are documented per-repo in known-differences.json
_LINGUIST_KNOWN_DIFF_PATHS: set[str] = set()  # loaded from known-differences.json at runtime


def load_known_differences(path) -> list[dict]:
    """Load the known-differences manifest from tests/atlas/known-differences.json."""
    import json
    from pathlib import Path
    p = Path(path)
    if not p.exists():
        return []
    return json.loads(p.read_text())


def classify_scc_mismatch(
    path: str,
    dircue_counts: dict[str, Any],
    scc_counts: dict[str, Any],
    known_diffs: list[dict],
    mismatch_fields: list[str] | None = None,
) -> str:
    """Classify a per-file scc counter mismatch.

    Returns one of the category strings defined in the module docstring.
    mismatch_fields is the list of field names that differ (e.g. ["complexity"]).
    """
    # Determine file extension
    ext = ""
    if "." in path:
        ext = "." + path.rsplit(".", 1)[-1].lower()

    # Check known-differences manifest first
    for diff in known_diffs:
        if diff.get("tool") not in ("scc", None):
            continue
        # Check file-extension rule (takes precedence over specific file rules)
        if diff.get("file_extension"):
            if ext != diff["file_extension"]:
                continue
            # If the diff specifies which fields must mismatch, check that
            if diff.get("fields") and mismatch_fields:
                if not all(f in mismatch_fields for f in diff["fields"]):
                    continue
            return "known-upstream-diff"
        # Check specific file rule
        if diff.get("file") and diff["file"] != path:
            continue
        # A diff with neither file nor file_extension applies to all files
        return "known-upstream-diff"

    # Check hardcoded known upstream differences
    # scc 4.1.0 raw-string comment overcounting in Java/C#
    if _SCC_RAW_STRING_COMMENT_PATTERN.match(path):
        scc_comment = scc_counts.get("Comment", scc_counts.get("comment", 0))
        dc_comment = dircue_counts.get("comment", 0)
        if abs(dc_comment - scc_comment) == 1:
            return "known-upstream-diff"

    return "dircue-bug"


def classify_linguist_mismatch(
    path: str,
    dircue_language: str | None,
    linguist_language: str | None,
    known_diffs: list[dict],
) -> str:
    """Classify a per-file Linguist language disagreement."""
    for diff in known_diffs:
        if diff.get("tool") not in ("linguist", None):
            continue
        if diff.get("file") and diff["file"] != path:
            continue
        return "known-upstream-diff"

    return "dircue-bug"


def classify_linguist_top_level_mismatch(
    repo_id: str,
    dircue_json: dict,
    linguist_json: dict,
    known_diffs: list[dict],
) -> str:
    """Classify a top-level Linguist language breakdown disagreement for a whole repo."""
    for diff in known_diffs:
        if diff.get("tool") not in ("linguist", None):
            continue
        if diff.get("repo") and diff["repo"] != repo_id:
            continue
        return "known-upstream-diff"
    return "dircue-bug"


def classify_skip_dircue_only(
    path: str,
    language: str,
    scc_extensions_in_repo: set,
    scc_paths_lower: set | None = None,
) -> str:
    """Classify a file that dircue counted as source but scc did not output.

    Evidence-backed via scc's known walker behaviour:
    - scc_skips_dotfiles: basename starts with '.' (scc skips dotfiles even with
      --no-ignore; confirmed empirically for scc 4.1.0)
    - harness_case_collision: the path differs only in case from a path that scc
      DID output; on macOS's case-insensitive filesystem, tar extraction collapses
      case-variant pairs (e.g. the Linux kernel's xt_CONNMARK.h / xt_connmark.h)
      so scc only sees one member of each pair — not an scc skip, a harness limit
    - scc_no_language: the file's extension does not appear anywhere in scc's
      output for this repo, meaning scc's language registry does not map that
      extension to any language
    - unexplained: none of the above evidence applies; the skip requires
      investigation
    """
    basename = path.rsplit("/", 1)[-1]
    if basename.startswith("."):
        return "scc_skips_dotfiles"
    if scc_paths_lower is not None and path.lower() in scc_paths_lower:
        return "harness_case_collision"
    # Extension: e.g. "foo.bar" -> ".bar"; "Makefile" -> "makefile"
    if "." in basename:
        ext = "." + path.rsplit(".", 1)[-1].lower()
    else:
        ext = basename.lower()
    if ext not in scc_extensions_in_repo:
        return "scc_no_language"
    return "unexplained"


def classify_skip_scc_only(path: str, dircue_row: dict | None) -> str:
    """Classify a file that scc output but dircue did not count as source.

    Evidence-backed via dircue's per-file 'reason' field in the metrics JSON:
    - dircue_out_of_scope: reason == 'outside_scope' (CI/config/docs/legal,
      Linguist-compatible selection rules)
    - dircue_unsupported_language: reason == 'unsupported_language'
    - dircue_binary: reason == 'binary'
    - dircue_non_regular_file: reason == 'non_regular_file' (symlinks, etc.)
    - dircue_file_too_large: reason == 'file_too_large'
    - unexplained: dircue_row is absent or reason is not in the known set;
      the skip is not accounted for and requires investigation
    """
    if dircue_row is None:
        return "unexplained"
    reason = dircue_row.get("reason", "")
    _REASON_MAP = {
        "outside_scope": "dircue_out_of_scope",
        "unsupported_language": "dircue_unsupported_language",
        "binary": "dircue_binary",
        "non_regular_file": "dircue_non_regular_file",
        "file_too_large": "dircue_file_too_large",
    }
    if reason in _REASON_MAP:
        return _REASON_MAP[reason]
    return "unexplained"


def classify_oracle_version(actual: str, expected: str) -> str:
    """Return 'matched' or 'oracle-version-mismatch'."""
    return "matched" if actual == expected else "oracle-version-mismatch"
