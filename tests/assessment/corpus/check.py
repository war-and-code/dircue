#!/usr/bin/env python3
"""Compare `dircue analyze assessment` with independently derived corpus labels.

    python3 tests/assessment/corpus/check.py --binary .cache/assessment140/candidate --corpus-root .cache/corpus

Each repository in expectations.json carries count assertions and, for npm
repositories, per-manifest association labels from label.py. Every label is
derived without dircue. The script prints each failed assertion and exits
non-zero if any assertion fails or none ran. Missing checkouts are skipped and
reported.
"""
import argparse
import json
import subprocess
import sys
from pathlib import Path

EXPECTATIONS = Path(__file__).resolve().with_name("expectations.json")


def analyze(binary, repo):
    out = subprocess.run([str(binary), "analyze", "assessment", "--source", "directory", "--json", str(repo)],
                         capture_output=True, timeout=600)
    if out.returncode != 0:
        raise RuntimeError(out.stderr.decode(errors="replace")[:500])
    return json.loads(out.stdout)


def actual(report, count):
    a = report["assessment"]
    field = count["field"]
    if field == "manifest_files":
        return sum(v["files"] for v in a["manifest_candidates"]
                   if v["ecosystem"] == count["ecosystem"] and v["filename"] == count["filename"])
    if field == "lockfile_state":
        row = next((v for v in a["lockfiles"] if v["ecosystem"] == count["ecosystem"]), None)
        return row[count["state"]]["count"] if row else 0
    if field == "workspace_completeness":
        return a["workspace_membership"]["completeness"]
    raise ValueError("unknown field: " + field)


def main():
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("--binary", type=Path, required=True)
    parser.add_argument("--corpus-root", type=Path, required=True)
    args = parser.parse_args()
    checked = failed = 0
    skipped = []
    for entry in json.loads(EXPECTATIONS.read_text())["repositories"]:
        repo = args.corpus_root / entry["id"]
        head = subprocess.run(["git", "-C", str(repo), "rev-parse", "HEAD"], capture_output=True, text=True).stdout.strip()
        if head != entry["commit"]:
            skipped.append(entry["id"])
            continue
        report = analyze(args.binary, repo)
        for count in entry["counts"]:
            checked += 1
            got = actual(report, count)
            if got != count["equals"]:
                failed += 1
                print(f"FAIL {entry['id']}: {json.dumps({k: v for k, v in count.items() if k != 'method'})} got {got!r}")
        labels = entry.get("npm_contexts", {}).get("states", {})
        states = {c["manifest_path"]: c["association_state"] for c in report["lockfiles"]["contexts"]
                  if c["ecosystem"] == "npm"}
        for manifest, want in sorted(labels.items()):
            checked += 1
            if states.get(manifest) != want:
                failed += 1
                print(f"FAIL {entry['id']}: {manifest} is {states.get(manifest)}, npm label {want}")
    print(f"{checked} assertions; {failed} failed; skipped (not fetched at the pinned commit): {', '.join(skipped) or 'none'}")
    if not checked:
        print("no repository was checked; fetch the pinned commits first", file=sys.stderr)
    return 1 if failed or not checked else 0


if __name__ == "__main__":
    sys.exit(main())
