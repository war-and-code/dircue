#!/usr/bin/env python3
"""
fetch_receipts.py — restore evidence receipt files from the GitHub release.

Usage:
  python3 scripts/fetch_receipts.py            # download and verify all files
  python3 scripts/fetch_receipts.py --check    # verify files present and correct, no download
  python3 scripts/fetch_receipts.py --path tests/enry-performance/results/final/evidence.tar.gz

Files are written to their original in-tree paths relative to the repository root.
The script is idempotent: files already present with the correct sha256 are skipped.
Network is only used when a file is missing or corrupt.

Exit codes: 0 = ok, 1 = error, 2 = offline / download failed (--check: missing files)
"""
import argparse
import hashlib
import json
import sys
import urllib.request
import urllib.error
from pathlib import Path

REPO_ROOT = Path(__file__).resolve().parents[1]
MANIFEST = REPO_ROOT / "tests" / "receipts" / "evidence-archive.json"


def sha256_file(path: Path) -> str:
    h = hashlib.sha256()
    with path.open("rb") as fh:
        for chunk in iter(lambda: fh.read(1 << 20), b""):
            h.update(chunk)
    return h.hexdigest()


def load_manifest() -> dict:
    if not MANIFEST.exists():
        print(f"error: manifest not found: {MANIFEST}", file=sys.stderr)
        sys.exit(1)
    return json.loads(MANIFEST.read_text())


def download(url: str, dest: Path, expected_sha256: str, expected_size: int) -> bool:
    """Download url to dest, verify sha256 and size. Returns True on success."""
    dest.parent.mkdir(parents=True, exist_ok=True)
    try:
        print(f"  downloading {dest.relative_to(REPO_ROOT)} ...", end=" ", flush=True)
        with urllib.request.urlopen(url, timeout=60) as resp:
            data = resp.read()
    except urllib.error.URLError as exc:
        print(f"FAILED ({exc})")
        return False
    actual_sha256 = hashlib.sha256(data).hexdigest()
    if actual_sha256 != expected_sha256:
        print(f"FAILED (sha256 mismatch: got {actual_sha256[:16]}...)")
        return False
    if len(data) != expected_size:
        print(f"FAILED (size mismatch: got {len(data)}, expected {expected_size})")
        return False
    dest.write_bytes(data)
    print("ok")
    return True


def check_file(entry: dict) -> tuple[bool, str]:
    """
    Returns (ok, status) where status is one of: 'ok', 'missing', 'corrupt', 'wrong-size'.
    """
    dest = REPO_ROOT / entry["path"]
    if not dest.exists():
        return False, "missing"
    if dest.stat().st_size != entry["size"]:
        return False, "wrong-size"
    if sha256_file(dest) != entry["sha256"]:
        return False, "corrupt"
    return True, "ok"


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--check", action="store_true",
                        help="Verify files are present and correct; do not download.")
    parser.add_argument("--path", metavar="PATH",
                        help="Restore only this one in-tree path.")
    args = parser.parse_args()

    manifest = load_manifest()
    entries = manifest["entries"]

    if args.path:
        entries = [e for e in entries if e["path"] == args.path]
        if not entries:
            print(f"error: path not in manifest: {args.path}", file=sys.stderr)
            return 1

    ok_count = skipped = failed = 0

    for entry in entries:
        dest = REPO_ROOT / entry["path"]
        good, status = check_file(entry)

        if good:
            skipped += 1
            continue

        if args.check:
            print(f"  MISSING/CORRUPT  {entry['path']}  ({status})", file=sys.stderr)
            failed += 1
            continue

        # Need to download
        success = download(entry["url"], dest, entry["sha256"], entry["size"])
        if success:
            ok_count += 1
        else:
            failed += 1

    total = len(entries)
    if args.check:
        present = total - failed
        print(f"check: {present}/{total} files present and correct", file=sys.stderr if failed else sys.stdout)
        return 2 if failed else 0

    print(f"fetch: {ok_count} downloaded, {skipped} already present, {failed} failed  (total {total})")
    return 1 if failed else 0


if __name__ == "__main__":
    sys.exit(main())
