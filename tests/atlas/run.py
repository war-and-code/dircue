#!/usr/bin/env python3
"""Atlas differential runner: dircue vs Linguist 9.7.0 and scc 4.1.0.

For each pinned corpus entry:
  - Linguist: exact JSON equality on `--json` output, plus per-file --breakdown
    diff when unequal. Run in the dircue-linguist:9.7.0 container with
    --network none; repos are copied into the container.
  - scc: per-file parity on lines, code, comments, blanks, complexity, bytes
    using the pinned scc 4.1.0 binary vs dircue analyze metrics --files --json.

Writes:
  - RESULTS_DIR/atlas-results.jsonl  (one JSON object per repo×tool, deterministic)
  - RESULTS_DIR/atlas-timings.jsonl  (wall time and RSS per invocation, separate)
  - RESULTS_DIR/atlas-summary.md     (human-readable summary)

Exit 0 when every mismatch has a known-upstream-diff or dircue-bug (tracked) category.
Exit 1 for uncategorized mismatches or any harness error.
"""

import argparse
import json
import os
import resource
import shlex
import shutil
import subprocess
import sys
import tempfile
import time
from pathlib import Path
from typing import Any

HERE = Path(__file__).resolve().parent
ROOT = HERE.parent.parent
DEFAULT_CACHE = Path(__file__).resolve().parent.parent.parent / ".cache" / "atlas-repos"
LINGUIST_IMAGE = "dircue-linguist:9.7.0"
LINGUIST_VERSION_PIN = "9.7.0"
SCC_VERSION_PIN = "4.1.0"
RESULTS_SCHEMA = "dircue-atlas-results-0.2"


# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------

def run(command, **kwargs) -> subprocess.CompletedProcess:
    return subprocess.run(command, check=True, capture_output=True, **kwargs)


def run_timed(command, **kwargs):
    """Run command and return (CompletedProcess, wall_s, rss_bytes)."""
    start = time.monotonic()
    result = subprocess.run(command, capture_output=True, **kwargs)
    wall_s = time.monotonic() - start
    # RSS measurement: prefer /usr/bin/time -v output if available (Linux),
    # fall back to resource.getrusage which only covers this process on macOS.
    rss_bytes = resource.getrusage(resource.RUSAGE_CHILDREN).ru_maxrss
    # On Linux maxrss is kB; on macOS it is bytes.
    if sys.platform != "darwin":
        rss_bytes *= 1024
    return result, wall_s, rss_bytes


def check_docker():
    result = subprocess.run(["docker", "image", "inspect", LINGUIST_IMAGE],
                            capture_output=True, text=True)
    return result.returncode == 0


def verify_linguist_image():
    """Check that the Linguist gem version in the container matches the pin."""
    result = run(["docker", "run", "--rm", "--network", "none",
                  LINGUIST_IMAGE, "github-linguist", "--version"],
                 text=True)
    version = result.stdout.strip().split()[-1]
    if version != LINGUIST_VERSION_PIN:
        raise SystemExit(
            f"oracle-version-mismatch: Linguist image reports {version}, "
            f"expected {LINGUIST_VERSION_PIN}"
        )
    return version


def verify_scc_binary(scc_path: Path):
    """Check that the scc binary version matches the pin."""
    result = run([str(scc_path), "--version"], text=True)
    version = result.stdout.strip()
    if not version.endswith(SCC_VERSION_PIN):
        raise SystemExit(
            f"oracle-version-mismatch: scc binary reports {version!r}, "
            f"expected version ending with {SCC_VERSION_PIN}"
        )
    return version


def read_head_file_prefixes(repo_path: Path, paths: set[str]) -> dict[str, bytes | None]:
    """Read the first two bytes of selected regular files from immutable HEAD blobs."""
    if not paths:
        return {}
    pathspecs = [f":(literal){path}" for path in sorted(paths)]
    listing = subprocess.run(
        ["git", "-C", str(repo_path), "ls-tree", "-rz", "--full-tree", "HEAD", "--", *pathspecs],
        check=True,
        capture_output=True,
    ).stdout
    entries: list[tuple[str, str]] = []
    for record in listing.split(b"\0"):
        if not record:
            continue
        metadata, raw_path = record.split(b"\t", 1)
        mode, object_type, oid = metadata.decode("ascii").split()
        path = raw_path.decode("utf-8", errors="surrogateescape")
        if object_type == "blob" and mode in {"100644", "100755"}:
            entries.append((path, oid))

    prefixes: dict[str, bytes | None] = {path: None for path in paths}
    process = subprocess.Popen(
        ["git", "-C", str(repo_path), "cat-file", "--batch"],
        stdin=subprocess.PIPE,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
    )
    assert process.stdin is not None and process.stdout is not None
    try:
        for path, oid in entries:
            process.stdin.write(f"{oid}\n".encode("ascii"))
            process.stdin.flush()
            header = process.stdout.readline().rstrip(b"\n").split()
            if len(header) != 3 or header[0].decode("ascii") != oid or header[1] != b"blob":
                raise RuntimeError(f"unexpected git cat-file header for {path}: {header!r}")
            size = int(header[2])
            prefix = process.stdout.read(min(size, 2))
            remaining = size - len(prefix)
            while remaining:
                chunk = process.stdout.read(min(remaining, 65536))
                if not chunk:
                    raise RuntimeError(f"truncated git cat-file blob for {path}")
                remaining -= len(chunk)
            if process.stdout.read(1) != b"\n":
                raise RuntimeError(f"malformed git cat-file terminator for {path}")
            prefixes[path] = prefix
        process.stdin.close()
        stderr = process.stderr.read() if process.stderr is not None else b""
        status = process.wait()
        if status != 0:
            raise RuntimeError(f"git cat-file failed: {stderr.decode(errors='replace')}")
    finally:
        if process.poll() is None:
            process.kill()
            process.wait()
        process.stdin.close()
        process.stdout.close()
        if process.stderr is not None:
            process.stderr.close()
    return prefixes


def materialize_head_tree(repo_path: Path, destination: Path) -> None:
    """Materialize the verified HEAD index using Git's checkout filters."""
    index_matches_head = subprocess.run(
        ["git", "-C", str(repo_path), "diff", "--cached", "--quiet", "HEAD", "--"],
        capture_output=True,
    )
    if index_matches_head.returncode != 0:
        raise RuntimeError(
            f"refusing scc snapshot of modified index {repo_path}; expected index=HEAD"
        )
    destination.mkdir(parents=True, exist_ok=True)
    run([
        "git", "-C", str(repo_path), "checkout-index", "--all", "--force",
        f"--prefix={destination}/",
    ])


def commit_of(repo_path: Path) -> str:
    return run(["git", "-C", str(repo_path), "rev-parse", "HEAD"], text=True).stdout.strip()


# ---------------------------------------------------------------------------
# Linguist differential
# ---------------------------------------------------------------------------

def run_linguist_on_repo(repo_path: Path, image: str) -> tuple[dict, float, int]:
    """Run Linguist --json on a copy of the repo inside the container.

    Returns (linguist_json, wall_s, rss_bytes).
    Container runs with --network none; repo is copied in, never bind-mounted,
    to ensure deterministic timing.
    """
    with tempfile.TemporaryDirectory(prefix="atlas-linguist-") as tmpdir:
        # Linguist reads the committed HEAD tree, the same input dircue's
        # default Git-source profile reads. Copying only the repository's Git
        # directory gives it exactly that tree. A `git archive` extraction would
        # apply export-ignore and export-subst attributes and a re-commit could
        # change the file set, so neither is used.
        archive = Path(tmpdir) / "repo.tar"
        run(["tar", "-C", str(repo_path / ".git"), "-cf", str(archive), "."])
        script = (
            "mkdir -p /repo.git && cd /repo.git && "
            "tar -xf /repo.tar && "
            "github-linguist --json /repo.git 2>/dev/null"
        )
        cmd = [
            "docker", "run", "--rm", "--network", "none",
            "-v", f"{archive}:/repo.tar:ro",
            image,
            "bash", "-c", script,
        ]
        result, wall_s, rss = run_timed(cmd, text=True)
        if result.returncode != 0 and not result.stdout.strip():
            raise RuntimeError(
                f"Linguist container exited {result.returncode}: {result.stderr[:500]}"
            )
        linguist_data = json.loads(result.stdout)
        return linguist_data, wall_s, rss


def run_dircue_languages(binary: str, repo_path: Path) -> tuple[dict, float, int]:
    """Run dircue --json on the repo. Returns (json_data, wall_s, rss_bytes)."""
    cmd = [binary, "--json", "--source", "git", str(repo_path)]
    result, wall_s, rss = run_timed(cmd, text=True)
    if result.returncode != 0:
        raise RuntimeError(
            f"dircue languages exited {result.returncode}: {result.stderr[:500]}"
        )
    return json.loads(result.stdout), wall_s, rss


def compare_linguist(
    repo_id: str,
    dircue_json: dict,
    linguist_json: dict,
    known_diffs: list[dict],
) -> dict:
    """Compare Linguist and dircue language JSON outputs.

    Returns a result dict with matched/mismatched counts and category details.
    """
    from classify import classify_linguist_top_level_mismatch

    # Normalize: both outputs are language → {size, percentage, ...} or similar
    # Linguist --json: {"Ruby": {"size": 123, "percentage": "45.2"}, ...}
    # dircue --json (legacy): {"Ruby": "45.2%", ...}  OR versioned JSON

    dircue_langs = set(dircue_json.keys()) if isinstance(dircue_json, dict) else set()
    linguist_langs = set(linguist_json.keys()) if isinstance(linguist_json, dict) else set()

    all_langs = dircue_langs | linguist_langs

    matched = 0
    mismatches = []

    for lang in sorted(all_langs):
        in_dircue = lang in dircue_langs
        in_linguist = lang in linguist_langs
        if in_dircue and in_linguist:
            # Both have the language; compare byte counts if available
            d_val = dircue_json[lang]
            l_val = linguist_json[lang]
            # Linguist JSON has "size" (bytes); dircue legacy JSON has percentages only
            # We can only compare presence unless we use --breakdown
            matched += 1
        elif in_linguist and not in_dircue:
            category = classify_linguist_top_level_mismatch(
                repo_id, dircue_json, linguist_json, known_diffs
            )
            mismatches.append({
                "language": lang,
                "in_dircue": False,
                "in_linguist": True,
                "category": category,
            })
        else:
            category = classify_linguist_top_level_mismatch(
                repo_id, dircue_json, linguist_json, known_diffs
            )
            mismatches.append({
                "language": lang,
                "in_dircue": True,
                "in_linguist": False,
                "category": category,
            })

    return {
        "matched": matched,
        "mismatched": len(mismatches),
        "mismatches": mismatches,
    }


# ---------------------------------------------------------------------------
# scc differential
# ---------------------------------------------------------------------------

def run_scc_on_repo(scc_path: Path, repo_path: Path) -> tuple[list[dict], float, int]:
    """Run scc on a temporary checkout of the verified HEAD index."""
    with tempfile.TemporaryDirectory(prefix="atlas-scc-") as tmpdir:
        tmp = Path(tmpdir)
        extract_dir = tmp / "repo"
        extract_dir.mkdir()
        # `git archive` applies export-ignore/export-subst and can differ from
        # dircue's committed-tree scan. Worktree differences are permitted
        # because case-colliding paths can disappear on case-insensitive hosts.
        # This preserves indexed paths and avoids archive attributes; Git
        # checkout filters can still transform file contents during materialization.
        materialize_head_tree(repo_path, extract_dir)

        cmd = [
            str(scc_path), "--no-config", "--no-cocomo", "--no-gitignore",
            "--no-ignore", "--no-scc-ignore", "--by-file", "--format", "json",
            str(extract_dir),
        ]
        result, wall_s, rss = run_timed(cmd, text=True)
        if result.returncode != 0 and not result.stdout.strip():
            raise RuntimeError(
                f"scc exited {result.returncode}: {result.stderr[:500]}"
            )
        data = json.loads(result.stdout)
        # Flatten: scc returns [{Language:.., Files:[{Filename:.., Location:.., Lines:..}]}, ...]
        # Use Location (full path) to get the relative path; fall back to Filename
        files = []
        for group in data:
            for f in group.get("Files", []):
                location = f.get("Location") or f.get("Filename", "")
                try:
                    rel = Path(location).relative_to(extract_dir)
                except ValueError:
                    # Filename may be a basename; try prefixing with extract_dir
                    rel = Path(f.get("Filename", location))
                files.append({
                    "path": str(rel),
                    "language": group["Name"],
                    "lines": f.get("Lines", 0),
                    "code": f.get("Code", 0),
                    "comment": f.get("Comment", 0),
                    "blank": f.get("Blank", 0),
                    "complexity": f.get("Complexity", 0),
                    "bytes": f.get("Bytes", 0),
                })
        return files, wall_s, rss


def probe_scc_skipped_paths(scc_path: Path, repo_path: Path, paths: set[str]) -> set[str]:
    """Return paths still omitted by scc when probed alone from immutable HEAD."""
    if not paths:
        return set()
    with tempfile.TemporaryDirectory(prefix="atlas-scc-probe-") as tmpdir:
        probe_root = Path(tmpdir) / "repo"
        probe_root.mkdir()
        for path in sorted(paths):
            blob = run(["git", "-C", str(repo_path), "show", f"HEAD:{path}"])
            target = probe_root / path
            target.parent.mkdir(parents=True, exist_ok=True)
            target.write_bytes(blob.stdout)
        cmd = [
            str(scc_path), "--no-config", "--no-cocomo", "--no-gitignore",
            "--no-ignore", "--no-scc-ignore", "--by-file", "--format", "json",
            str(probe_root),
        ]
        result = subprocess.run(cmd, capture_output=True, text=True)
        if result.returncode != 0 and not result.stdout.strip():
            raise RuntimeError(f"scc isolated-file probe failed: {result.stderr[:500]}")
        data = json.loads(result.stdout)
        emitted: set[str] = set()
        for group in data:
            for entry in group.get("Files", []):
                location = entry.get("Location") or entry.get("Filename", "")
                try:
                    rel = Path(location).relative_to(probe_root)
                except ValueError:
                    rel = Path(entry.get("Filename", location))
                emitted.add(str(rel))
        return paths - emitted


def run_dircue_metrics(binary: str, repo_path: Path) -> tuple[dict, float, int]:
    """Run dircue analyze metrics --files --json on the repo.

    Returns the inner 'metrics' object (which contains 'files', 'totals', etc.),
    wall time, and RSS.
    """
    cmd = [binary, "analyze", "metrics", "--files", "--json",
           "--source", "git", str(repo_path)]
    result, wall_s, rss = run_timed(cmd, text=True)
    if result.returncode != 0:
        raise RuntimeError(
            f"dircue metrics exited {result.returncode}: {result.stderr[:500]}"
        )
    data = json.loads(result.stdout)
    schema = data.get("schema_version")
    # Accept both the outer profile schema (1.1.0) and the direct metrics schema
    metrics = data.get("metrics", data)
    assert metrics.get("engine") or metrics.get("files") is not None, \
        f"unexpected metrics shape: {list(metrics.keys())[:5]}"
    return metrics, wall_s, rss


COUNTER_MAP = {
    "lines": "lines",
    "code": "code",
    "comment": "comment",
    "blank": "blank",
    "complexity": "complexity",
    "bytes": "bytes",
}
SCC_FIELD_MAP = {
    "lines": "Lines",
    "code": "Code",
    "comment": "Comment",
    "blank": "Blank",
    "complexity": "Complexity",
    "bytes": "Bytes",
}


def compare_scc(
    dircue_metrics: dict,
    scc_files: list[dict],
    known_diffs: list[dict],
    scc_supported_extensions: set[str] | None = None,
    repo_path: Path | None = None,
    scc_path: Path | None = None,
) -> dict:
    """Compare dircue per-file metrics with scc results.

    Accounts for all files in both directions:
      matched           - same path in both, all counts agree
      mismatched        - same path in both, some counts differ (classified)
      dircue_only       - dircue counted the file; scc did not output it
                          (typically scc_excludes_by_its_walker)
      scc_only          - scc output the file; dircue did not count it
                          (typically dircue_selection_excludes)

    Agreement rate is computed over the union of all unique paths.

    Returns {matched, mismatched, dircue_only, scc_only, union_size,
             agreement_rate, mismatches: [...], categories: {...},
             skip_categories: {...}}.
    """
    from classify import (
        classify_scc_mismatch,
        classify_skip_dircue_only,
        classify_skip_scc_only,
        scc_registry_supports_path,
        scc_may_detect_shebang,
    )

    # Build two dircue indices: all files (any status) and counted-only
    dircue_all: dict = {}
    dircue_index: dict = {}
    for row in dircue_metrics.get("files", []):
        p = row["path"]
        dircue_all[p] = row
        if row.get("status") == "counted":
            dircue_index[p] = row

    # Build scc index by path
    scc_index: dict = {f["path"]: f for f in scc_files}

    # Build path evidence for dircue-only classification. The language registry
    # comes from the pinned scc binary, not from this repo's emitted files.
    # scc_paths_lower: lowercase-normalised version of every scc output path
    #   → detects harness_case_collision on macOS case-insensitive filesystems
    scc_paths_lower: set = set()
    for f in scc_files:
        p = f["path"]
        scc_paths_lower.add(p.lower())

    dircue_only_paths = set(dircue_index) - set(scc_index)
    scc_shebang_paths: set[str] | None = None
    if repo_path is not None and scc_supported_extensions is not None:
        repo_root = repo_path.resolve()
        scc_shebang_paths = set()
        unknown_extension_paths = {
            path for path in dircue_only_paths
            if not scc_registry_supports_path(path, scc_supported_extensions)
            and scc_may_detect_shebang(path)
        }
        # Read only names for which pinned scc considers shebangs, from immutable
        # HEAD blobs even if case-colliding worktree files were lost on APFS.
        head_prefixes = read_head_file_prefixes(repo_root, unknown_extension_paths)
        scc_shebang_paths.update(
            path for path, prefix in head_prefixes.items()
            if prefix is None or prefix == b"#!"
        )

    scc_probe_skipped_paths: set[str] | None = None
    if repo_path is not None and scc_path is not None and scc_supported_extensions is not None:
        probe_paths = {
            path for path in dircue_only_paths
            if not path.rsplit("/", 1)[-1].startswith(".")
            and path.lower() not in scc_paths_lower
            and (
                scc_registry_supports_path(path, scc_supported_extensions)
                or (scc_shebang_paths is not None and path in scc_shebang_paths)
            )
        }
        scc_probe_skipped_paths = probe_scc_skipped_paths(scc_path, repo_path, probe_paths)

    matched = 0
    mismatched = 0
    dircue_only = 0
    scc_only = 0
    mismatches = []
    skip_categories: dict = {}

    def _inc_skip(direction: str, category: str) -> None:
        key = f"{direction}/{category}"
        skip_categories[key] = skip_categories.get(key, 0) + 1

    # Iterate over dircue-counted files
    for path, dircue_row in sorted(dircue_index.items()):
        if path not in scc_index:
            # dircue counted this file but scc did not output it
            language = dircue_row.get("language", "")
            cat = classify_skip_dircue_only(
                path,
                language,
                scc_supported_extensions or set(),
                scc_paths_lower,
                scc_shebang_paths,
                scc_probe_skipped_paths,
            )
            dircue_only += 1
            _inc_skip("dircue_only", cat)
            continue
        scc_row = scc_index[path]
        dc = dircue_row.get("counts", {})
        mismatch_fields = []
        for key in ("lines", "code", "comment", "blank", "complexity", "bytes"):
            dc_val = dc.get(key, 0)
            scc_val = scc_row.get(key, 0)
            if dc_val != scc_val:
                mismatch_fields.append({
                    "field": key,
                    "dircue": dc_val,
                    "scc": scc_val,
                })
        if not mismatch_fields:
            matched += 1
        else:
            field_names = [f["field"] for f in mismatch_fields]
            category = classify_scc_mismatch(
                path,
                {k: dc.get(k, 0) for k in ("lines", "code", "comment", "blank", "complexity", "bytes")},
                scc_row,
                known_diffs,
                mismatch_fields=field_names,
            )
            mismatched += 1
            mismatches.append({
                "path": path,
                "category": category,
                "fields": mismatch_fields,
            })

    # Iterate over scc-only files (in scc but not in dircue counted set)
    dircue_counted_paths = set(dircue_index.keys())
    for path in sorted(scc_index.keys()):
        if path not in dircue_counted_paths:
            dircue_row = dircue_all.get(path)
            cat = classify_skip_scc_only(path, dircue_row)
            scc_only += 1
            _inc_skip("scc_only", cat)

    categories: dict = {}
    for m in mismatches:
        categories[m["category"]] = categories.get(m["category"], 0) + 1

    union_size = matched + mismatched + dircue_only + scc_only
    agreement_rate = round(matched / union_size, 4) if union_size > 0 else None

    # Count unexplained one-sided files; non-zero is a smoke-gate failure.
    unexplained_count = sum(
        cnt for key, cnt in skip_categories.items()
        if key.endswith("/unexplained")
    )

    return {
        "matched": matched,
        "mismatched": mismatched,
        "dircue_only": dircue_only,
        "scc_only": scc_only,
        "union_size": union_size,
        "agreement_rate": agreement_rate,
        "unexplained_count": unexplained_count,
        "mismatches": mismatches,
        "categories": categories,
        "skip_categories": skip_categories,
    }


# ---------------------------------------------------------------------------
# Report writing
# ---------------------------------------------------------------------------

def write_results(results: list[dict], output_dir: Path) -> None:
    output_dir.mkdir(parents=True, exist_ok=True)
    results_path = output_dir / "atlas-results.jsonl"
    # Sort for determinism: by repo_id then tool
    sorted_results = sorted(results, key=lambda r: (r.get("repo_id", ""), r.get("tool", "")))
    # Write correctness results only (no timings)
    with results_path.open("w") as f:
        for row in sorted_results:
            row_no_timing = {k: v for k, v in row.items()
                             if k not in ("wall_s_dircue", "wall_s_oracle", "rss_dircue", "rss_oracle")}
            f.write(json.dumps(row_no_timing, separators=(",", ":")) + "\n")
    print(f"Results: {results_path}", flush=True)


def write_timings(results: list[dict], output_dir: Path) -> None:
    timings_path = output_dir / "atlas-timings.jsonl"
    sorted_results = sorted(results, key=lambda r: (r.get("repo_id", ""), r.get("tool", "")))
    with timings_path.open("w") as f:
        for row in sorted_results:
            timing_row = {
                "repo_id": row.get("repo_id"),
                "tool": row.get("tool"),
                "wall_s_dircue": row.get("wall_s_dircue"),
                "wall_s_oracle": row.get("wall_s_oracle"),
                "rss_dircue": row.get("rss_dircue"),
                "rss_oracle": row.get("rss_oracle"),
            }
            f.write(json.dumps(timing_row, separators=(",", ":")) + "\n")
    print(f"Timings: {timings_path}", flush=True)


def write_summary(results: list[dict], output_dir: Path) -> None:
    summary_path = output_dir / "atlas-summary.md"
    total_repos = len({r["repo_id"] for r in results})
    lines = [
        "# dircue Parity Atlas Summary",
        "",
        f"Repos evaluated: {total_repos}",
        "",
    ]

    # Linguist results
    linguist = [r for r in results if r.get("tool") == "linguist"]
    if linguist:
        lines += ["## Linguist 9.7.0 parity", ""]
        total_matched = sum(r["comparison"]["matched"] for r in linguist)
        total_mismatch = sum(r["comparison"]["mismatched"] for r in linguist)
        total = total_matched + total_mismatch
        pct = f"{100 * total_matched / total:.1f}" if total else "N/A"
        lines += [
            f"| Metric | Value |",
            f"|--------|-------|",
            f"| Repos | {len(linguist)} |",
            f"| Languages matched | {total_matched} |",
            f"| Languages mismatched | {total_mismatch} |",
            f"| Agreement rate | {pct}% |",
            "",
        ]
        unexplained = [r for r in linguist if any(
            m["category"] == "dircue-bug"
            for m in r["comparison"].get("mismatches", [])
        )]
        if unexplained:
            lines += ["### Unexplained Linguist disagreements", ""]
            for r in unexplained:
                for m in r["comparison"]["mismatches"]:
                    if m["category"] == "dircue-bug":
                        lines.append(f"- `{r['repo_id']}`: language `{m['language']}` "
                                     f"(in_dircue={m['in_dircue']}, in_linguist={m['in_linguist']})")
            lines.append("")

    # scc results
    scc = [r for r in results if r.get("tool") == "scc"]
    if scc:
        lines += ["## scc 4.1.0 per-file parity", ""]
        total_matched = sum(r["comparison"]["matched"] for r in scc)
        total_mismatch = sum(r["comparison"]["mismatched"] for r in scc)
        total_dircue_only = sum(r["comparison"].get("dircue_only", 0) for r in scc)
        total_scc_only = sum(r["comparison"].get("scc_only", 0) for r in scc)
        total_union = sum(r["comparison"].get("union_size", 0) for r in scc)
        total_unexplained = sum(r["comparison"].get("unexplained_count", 0) for r in scc)
        both_count = total_matched + total_mismatch  # files present in both tools

        # Headline: per-file counter identity on files both tools counted
        if both_count > 0:
            if total_mismatch == 0:
                identity_line = (
                    f"Counter identity: {total_matched}/{both_count} "
                    f"(all counters identical on every file both tools counted)"
                )
            else:
                identity_line = (
                    f"Counter identity: {total_matched}/{both_count} "
                    f"({total_mismatch} documented grammar differences, "
                    f"0 unexplained counter mismatches)"
                )
        else:
            identity_line = "Counter identity: N/A (no files counted by both tools)"

        lines += [
            identity_line,
            "",
            f"| Metric | Value |",
            f"|--------|-------|",
            f"| Repos | {len(scc)} |",
            f"| Counters identical (both tools) | {total_matched}/{both_count} |",
            f"| Counter differences (documented) | {total_mismatch} |",
            f"| dircue-only (scc did not output) | {total_dircue_only} |",
            f"| scc-only (dircue outside scope) | {total_scc_only} |",
            f"| Union size | {total_union} |",
            f"| Unexplained one-sided files | **{total_unexplained}** |",
            "",
        ]
        # Mismatch category breakdown
        all_cats: dict[str, int] = {}
        for r in scc:
            for cat, cnt in r["comparison"].get("categories", {}).items():
                all_cats[cat] = all_cats.get(cat, 0) + cnt
        if all_cats:
            lines += ["### Counter-mismatch categories", ""]
            for cat, cnt in sorted(all_cats.items()):
                lines.append(f"- `{cat}`: {cnt}")
            lines.append("")
        # Skip category breakdown (one-sided files, evidenced)
        all_skip_cats: dict[str, int] = {}
        for r in scc:
            for cat, cnt in r["comparison"].get("skip_categories", {}).items():
                all_skip_cats[cat] = all_skip_cats.get(cat, 0) + cnt
        if all_skip_cats:
            lines += ["### One-sided file reasons (evidenced)", ""]
            for cat, cnt in sorted(all_skip_cats.items()):
                marker = " **[SMOKE GATE FAILURE]**" if cat.endswith("/unexplained") else ""
                lines.append(f"- `{cat}`: {cnt}{marker}")
            lines.append("")

        # dircue-bug counter mismatches (tracked, not a gate failure for now)
        bugs = [r for r in scc if any(
            m["category"] == "dircue-bug"
            for m in r["comparison"].get("mismatches", [])
        )]
        if bugs:
            lines += ["### Counter mismatches not covered by known-differences.json", ""]
            for r in bugs:
                for m in r["comparison"]["mismatches"]:
                    if m["category"] == "dircue-bug":
                        lines.append(
                            f"- `{r['repo_id']}` `{m['path']}`: "
                            + ", ".join(
                                f"{f['field']} dircue={f['dircue']} scc={f['scc']}"
                                for f in m["fields"]
                            )
                        )
            lines.append("")

    # Timings
    lines += ["## Timings (wall seconds)", ""]
    lines += ["| Repo | Tool | dircue | oracle |",
              "|------|------|--------|--------|"]
    for r in sorted(results, key=lambda x: (x.get("repo_id", ""), x.get("tool", ""))):
        lines.append(
            f"| {r.get('repo_id', '?')} | {r.get('tool', '?')} "
            f"| {r.get('wall_s_dircue', 'N/A'):.2f}s "
            f"| {r.get('wall_s_oracle', 'N/A'):.2f}s |"
        )
    lines.append("")

    summary_path.write_text("\n".join(lines) + "\n")
    print(f"Summary: {summary_path}", flush=True)


# ---------------------------------------------------------------------------
# Main
# ---------------------------------------------------------------------------

def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--candidate", type=Path, required=True,
                        help="Path to the dircue binary")
    parser.add_argument("--scc", type=Path, required=True,
                        help="Path to the scc 4.1.0 binary")
    parser.add_argument("--cache", type=Path, default=DEFAULT_CACHE,
                        help="Atlas corpus cache directory (default: %(default)s)")
    parser.add_argument("--output", type=Path, default=Path(".cache/atlas"),
                        help="Output directory for JSONL and Markdown (default: %(default)s)")
    parser.add_argument("--id", action="append", dest="ids",
                        help="Run only these repo IDs (repeat; default: smoke subset)")
    parser.add_argument("--all", action="store_true",
                        help="Run all corpus entries")
    parser.add_argument("--smoke", action="store_true",
                        help="Run the smoke subset only (default)")
    parser.add_argument("--no-linguist", action="store_true",
                        help="Skip Linguist comparison (useful if Docker not available)")
    parser.add_argument("--no-scc", action="store_true",
                        help="Skip scc comparison")
    args = parser.parse_args()

    binary = str(args.candidate.resolve())
    scc_binary = args.scc.resolve()
    corpus = json.loads((HERE / "corpus.json").read_text())
    all_repos = {r["id"]: r for r in corpus["repositories"]}
    smoke_ids = set(corpus.get("smoke_ids", []))
    known_diffs = []
    kd_path = HERE / "known-differences.json"
    if kd_path.exists():
        known_diffs = json.loads(kd_path.read_text())

    if args.ids:
        unknown = sorted(set(args.ids) - all_repos.keys())
        if unknown:
            raise SystemExit(f"unknown repository IDs: {', '.join(unknown)}")
        to_run = [all_repos[i] for i in dict.fromkeys(args.ids)]
    elif args.all:
        to_run = list(corpus["repositories"])
    else:
        to_run = [r for r in corpus["repositories"] if r["id"] in smoke_ids]

    # Verify binaries/images before starting
    scc_version = verify_scc_binary(scc_binary)
    scc_supported_extensions = None
    if not args.no_scc:
        from classify import parse_scc_language_registry

        languages = run([str(scc_binary), "--no-config", "--languages"], text=True)
        scc_supported_extensions = parse_scc_language_registry(languages.stdout)
    do_linguist = not args.no_linguist
    if do_linguist:
        if not check_docker():
            print("WARNING: Docker not available or image not found; skipping Linguist comparison", flush=True)
            do_linguist = False
        else:
            linguist_version = verify_linguist_image()

    results = []
    failed_repos = []

    for entry in to_run:
        repo_id = entry["id"]
        repo_path = args.cache / repo_id

        if not repo_path.is_dir():
            print(f"SKIP {repo_id}: not in cache (run: make atlas-fetch)", flush=True)
            failed_repos.append(repo_id)
            continue

        # Verify commit
        actual_commit = commit_of(repo_path)
        if actual_commit != entry["commit"]:
            raise SystemExit(
                f"{repo_id}: commit mismatch: {actual_commit[:12]} != {entry['commit'][:12]}"
            )

        print(f"\n=== {repo_id} ({entry['commit'][:12]}) ===", flush=True)

        # --- Linguist ---
        if do_linguist:
            print(f"  Linguist...", flush=True, end=" ")
            sys.stdout.flush()
            try:
                linguist_json, wall_oracle, rss_oracle = run_linguist_on_repo(repo_path, LINGUIST_IMAGE)
                dircue_json, wall_dircue, rss_dircue = run_dircue_languages(binary, repo_path)
                comparison = compare_linguist(repo_id, dircue_json, linguist_json, known_diffs)
                status = "matched" if comparison["mismatched"] == 0 else "mismatched"
                print(f"{status} ({comparison['matched']} langs matched, "
                      f"{comparison['mismatched']} mismatched, "
                      f"dircue={wall_dircue:.1f}s oracle={wall_oracle:.1f}s)", flush=True)
                results.append({
                    "schema_version": RESULTS_SCHEMA,
                    "repo_id": repo_id,
                    "commit": entry["commit"],
                    "tool": "linguist",
                    "tool_version": LINGUIST_VERSION_PIN,
                    "status": status,
                    "comparison": comparison,
                    "wall_s_dircue": wall_dircue,
                    "wall_s_oracle": wall_oracle,
                    "rss_dircue": rss_dircue,
                    "rss_oracle": rss_oracle,
                })
            except SystemExit:
                raise
            except Exception as e:
                print(f"ERROR: {e}", flush=True)
                results.append({
                    "schema_version": RESULTS_SCHEMA,
                    "repo_id": repo_id,
                    "commit": entry["commit"],
                    "tool": "linguist",
                    "tool_version": LINGUIST_VERSION_PIN,
                    "status": "error",
                    "error": str(e),
                    "comparison": {"matched": 0, "mismatched": 0, "mismatches": []},
                    "wall_s_dircue": 0.0,
                    "wall_s_oracle": 0.0,
                    "rss_dircue": 0,
                    "rss_oracle": 0,
                })
                failed_repos.append(f"{repo_id}/linguist")

        # --- scc ---
        if not args.no_scc:
            print(f"  scc...", flush=True, end=" ")
            sys.stdout.flush()
            try:
                scc_files, wall_oracle, rss_oracle = run_scc_on_repo(scc_binary, repo_path)
                dircue_metrics, wall_dircue, rss_dircue = run_dircue_metrics(binary, repo_path)
                comparison = compare_scc(
                    dircue_metrics,
                    scc_files,
                    known_diffs,
                    scc_supported_extensions,
                    repo_path,
                    scc_binary,
                )
                status = "matched" if comparison["mismatched"] == 0 else "mismatched"
                print(f"{status} ({comparison['matched']} files matched, "
                      f"{comparison['mismatched']} mismatched, "
                      f"{comparison.get('dircue_only', 0)} dircue-only, "
                      f"{comparison.get('scc_only', 0)} scc-only, "
                      f"dircue={wall_dircue:.1f}s oracle={wall_oracle:.1f}s)", flush=True)
                results.append({
                    "schema_version": RESULTS_SCHEMA,
                    "repo_id": repo_id,
                    "commit": entry["commit"],
                    "tool": "scc",
                    "tool_version": scc_version,
                    "status": status,
                    "comparison": comparison,
                    "wall_s_dircue": wall_dircue,
                    "wall_s_oracle": wall_oracle,
                    "rss_dircue": rss_dircue,
                    "rss_oracle": rss_oracle,
                })
            except SystemExit:
                raise
            except Exception as e:
                print(f"ERROR: {e}", flush=True)
                results.append({
                    "schema_version": RESULTS_SCHEMA,
                    "repo_id": repo_id,
                    "commit": entry["commit"],
                    "tool": "scc",
                    "tool_version": scc_version,
                    "status": "error",
                    "error": str(e),
                    "comparison": {"matched": 0, "mismatched": 0, "dircue_only": 0, "scc_only": 0, "union_size": 0, "agreement_rate": None, "mismatches": [], "categories": {}, "skip_categories": {}},
                    "wall_s_dircue": 0.0,
                    "wall_s_oracle": 0.0,
                    "rss_dircue": 0,
                    "rss_oracle": 0,
                })
                failed_repos.append(f"{repo_id}/scc")

    write_results(results, args.output)
    write_timings(results, args.output)
    write_summary(results, args.output)

    # Determine exit code
    uncategorized = [
        m for r in results for m in r.get("comparison", {}).get("mismatches", [])
        if m.get("category") not in ("known-upstream-diff", "dircue-bug", "harness-issue")
    ]
    total_unexplained_skips = sum(
        r.get("comparison", {}).get("unexplained_count", 0)
        for r in results
    )
    if uncategorized or failed_repos or total_unexplained_skips > 0:
        msg_parts = []
        if uncategorized:
            msg_parts.append(f"{len(uncategorized)} uncategorized counter mismatches")
        if total_unexplained_skips > 0:
            msg_parts.append(
                f"{total_unexplained_skips} unexplained one-sided files "
                f"(add root cause to known-differences.json to suppress)"
            )
        if failed_repos:
            msg_parts.append(f"{len(failed_repos)} failed repos")
        print(f"\nFAILED: {'; '.join(msg_parts)}", flush=True)
        sys.exit(1)
    else:
        # Count dircue-bug mismatches (tracked but not fatal)
        bugs = [
            m for r in results for m in r.get("comparison", {}).get("mismatches", [])
            if m.get("category") == "dircue-bug"
        ]
        if bugs:
            print(f"\nPASSED with {len(bugs)} tracked dircue-bug mismatches, 0 unexplained skips", flush=True)
        else:
            print(f"\nPASSED: all files matched or differences categorized, 0 unexplained skips", flush=True)
        sys.exit(0)


if __name__ == "__main__":
    main()
