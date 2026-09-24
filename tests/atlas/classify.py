"""Discrepancy classification for the dircue parity atlas.

Every per-file disagreement between dircue and an oracle gets one of these
categories:

    matched                   No discrepancy.
    grammar_selection_differs dircue and scc used different scc grammars for the same
                              file. dircue selects a grammar via its Linguist language
                              name (from enry); scc selects via filename/extension.
                              Counter differences are the expected consequence of the
                              grammar difference, not a counting bug. See issue #12 for
                              the --selection flag that would align grammar selection.
    dircue-bug                dircue produces a different result not covered by any
                              known upstream difference (same grammar, counts differ).
    known-upstream-diff       The disagreement is in the documented list of upstream
                              differences between dircue and the oracle (e.g. raw-string
                              comment counting in scc 4.1.0, or a Linguist strategy that
                              differs from Enry).
    harness-issue             The harness produced unreliable oracle output (e.g.
                              the container reported a gem-version mismatch, or scc
                              could not read a file).
    oracle-version-mismatch   The oracle version does not match the pinned version; abort.

A mismatch row that cannot be classified into any known-upstream-diff or
harness-issue becomes `dircue-bug`.
"""

from __future__ import annotations

import re


def parse_scc_language_registry(output: str) -> set[str]:
    """Parse `scc --languages` output into its case-insensitive file tokens."""
    extensions: set[str] = set()
    for line in output.splitlines():
        line = line.strip()
        if " (" not in line or not line.endswith(")"):
            continue
        _, tokens = line.rsplit(" (", 1)
        extensions.update(
            item.strip().lower()
            for item in tokens[:-1].split(",")
            if item.strip()
        )
    if not extensions:
        raise ValueError("scc --languages output contained no file extensions")
    return extensions


def scc_registry_supports_path(path: str, scc_supported_extensions: set[str]) -> bool:
    """Whether the registry names this basename or one of its suffixes."""
    basename = path.rsplit("/", 1)[-1].lower()
    candidates = {basename}
    for index, char in enumerate(basename):
        if char == "." and index > 0:
            suffix = basename[index + 1:]
            if suffix:
                candidates.add(suffix)
    return bool(candidates & scc_supported_extensions)


def scc_may_detect_shebang(path: str) -> bool:
    """Mirror scc 4.1.0 DetectLanguage's filename gate for shebang handling.

    In the pinned scc source, shebang detection is considered only for names
    without a dot, or a dotfile with exactly one dot. Other dotted names are
    resolved by the filename/extension registry and never use their shebang.
    """
    basename = path.rsplit("/", 1)[-1]
    dot_count = basename.count(".")
    return dot_count == 0 or (basename.startswith(".") and dot_count == 1)
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
    dircue_grammar: str = "",
    scc_language: str = "",
) -> str:
    """Classify a per-file scc counter mismatch.

    Returns one of the category strings defined in the module docstring.
    mismatch_fields is the list of field names that differ (e.g. ["complexity"]).
    dircue_grammar is the scc grammar name dircue selected for this file
        (from dircue's per-file 'grammar' field in metrics JSON output).
    scc_language is the language name scc used for this file
        (from scc's per-file 'Language' field in --by-file JSON output).
    When dircue_grammar and scc_language are both present and differ, the file
    is classified as grammar_selection_differs: the two tools chose different
    grammars, so counter differences are an expected consequence of that choice.
    dircue selects the scc grammar from its Linguist language name; scc selects
    by filename/extension registry. See issue #12 for alignment work.
    """
    # Per-file grammar attribution: if the two tools used different grammars,
    # the counter difference is caused by grammar selection, not a counting bug.
    if dircue_grammar and scc_language and dircue_grammar != scc_language:
        return "grammar_selection_differs"

    # Determine file extension
    ext = ""
    if "." in path:
        ext = "." + path.rsplit(".", 1)[-1].lower()

    # Check known-differences manifest (specific per-file same-grammar differences)
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
    scc_supported_extensions: set,
    scc_paths_lower: set | None = None,
    scc_shebang_paths: set | None = None,
    scc_probe_skipped_paths: set | None = None,
) -> str:
    """Classify a file that dircue counted as source but scc did not output.

    Evidence-backed via scc's known walker behaviour:
    - scc_skips_dotfiles: basename starts with '.' (scc skips dotfiles even with
      --no-ignore; confirmed empirically for scc 4.1.0)
    - harness_case_collision: the path differs only in case from a path that scc
      DID output; on macOS's case-insensitive filesystem, index materialization
      collapses case-variant pairs (e.g. Linux kernel xt_CONNMARK.h / xt_connmark.h)
      so scc only sees one member of each pair — not an scc skip, a harness limit
    - scc_no_language: no basename or suffix mapping exists in the pinned scc
      `--languages` registry, and the file has no shebang that scc 4.1.0 can
      consider for this filename (see `processor/detector.go:DetectLanguage`).
    - scc_skips_file: a focused run of the same pinned scc binary also omitted
      this exact HEAD file when isolated under its original relative path.
    - unexplained: registry support, a shebang, or another cause could explain
      the omission; the available evidence is insufficient to classify it.
    """
    basename = path.rsplit("/", 1)[-1]
    if basename.startswith("."):
        return "scc_skips_dotfiles"
    if scc_paths_lower is not None and path.lower() in scc_paths_lower:
        return "harness_case_collision"
    if scc_probe_skipped_paths is not None and path in scc_probe_skipped_paths:
        return "scc_skips_file"
    if scc_shebang_paths is None or path in scc_shebang_paths:
        return "unexplained"
    if not scc_registry_supports_path(path, scc_supported_extensions):
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
        "unsupported_encoding": "dircue_unsupported_encoding",
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
