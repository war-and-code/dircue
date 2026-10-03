#!/usr/bin/env python3
"""
fetch_receipts.py — restore evidence receipt files from the GitHub release.

Usage:
  python3 scripts/fetch_receipts.py            # download and verify all files
  python3 scripts/fetch_receipts.py --check    # verify files present and correct, no download
  python3 scripts/fetch_receipts.py --path tests/enry-performance/results/final/evidence.tar.gz

Files are written to their original in-tree paths relative to the repository root.
Downloads use the public release URL. While the repository is private, or when
that URL is refused, the script retries through the GitHub API with a token
from GH_TOKEN, GITHUB_TOKEN or `gh auth token`.
The script is idempotent: files already present with the correct sha256 are skipped.
Network is only used when a file is missing or corrupt.

Exit codes: 0 = ok, 1 = error, 2 = offline / download failed (--check: missing files)
"""
import argparse
import hashlib
import json
import os
import re
import subprocess
import sys
from urllib.parse import urlsplit
import urllib.request
import urllib.error
from pathlib import Path

REPO_ROOT = Path(__file__).resolve().parents[1]
MANIFEST = REPO_ROOT / "tests" / "receipts" / "evidence-archive.json"
ARCHIVE_REPOSITORY = "https://github.com/war-and-code/dircue"
SHA256_RE = re.compile(r"^[0-9a-f]{64}$")
ASSET_RE = re.compile(r"^[A-Za-z0-9][A-Za-z0-9._-]*$")


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
    manifest = json.loads(MANIFEST.read_text())
    validate_manifest(manifest)
    return manifest


def validate_manifest(manifest: dict) -> None:
    """Reject unsafe restore paths and URLs before any filesystem/network use."""
    if not isinstance(manifest, dict):
        raise ValueError("evidence archive manifest must be an object")
    tag = manifest.get("release_tag")
    if not isinstance(tag, str) or not re.fullmatch(r"[A-Za-z0-9._-]+", tag):
        raise ValueError("evidence archive release_tag is invalid")
    base = f"{ARCHIVE_REPOSITORY}/releases/download/{tag}"
    if manifest.get("base_download_url") != base or manifest.get("release_url") != f"{ARCHIVE_REPOSITORY}/releases/tag/{tag}":
        raise ValueError("evidence archive release URLs do not match the configured repository and tag")
    entries = manifest.get("entries")
    if not isinstance(entries, list) or not entries:
        raise ValueError("evidence archive entries must be a non-empty list")
    paths, assets = set(), set()
    for index, entry in enumerate(entries):
        label = f"evidence archive entry {index}"
        if not isinstance(entry, dict):
            raise ValueError(f"{label} must be an object")
        path, digest, size, asset, url = (entry.get(key) for key in ("path", "sha256", "size", "asset", "url"))
        if not isinstance(path, str) or not path or "\\" in path or path.startswith("/") or "\x00" in path:
            raise ValueError(f"{label} has an unsafe restore path")
        if any(part in ("", ".", "..") for part in path.split("/")):
            raise ValueError(f"{label} has an unsafe restore path")
        if path in paths:
            raise ValueError(f"{label} duplicates restore path {path!r}")
        paths.add(path)
        if not isinstance(digest, str) or not SHA256_RE.fullmatch(digest):
            raise ValueError(f"{label} has an invalid SHA-256 digest")
        if isinstance(size, bool) or not isinstance(size, int) or size <= 0:
            raise ValueError(f"{label} has an invalid size")
        if not isinstance(asset, str) or not ASSET_RE.fullmatch(asset) or asset in (".", ".."):
            raise ValueError(f"{label} has an unsafe asset name")
        if asset in assets:
            raise ValueError(f"{label} duplicates asset {asset!r}")
        assets.add(asset)
        expected_url = f"{base}/{asset}"
        try:
            parsed = urlsplit(url)
        except (TypeError, ValueError):
            parsed = None
        if parsed is None or parsed.scheme != "https" or parsed.netloc != "github.com" or parsed.query or parsed.fragment or url != expected_url:
            raise ValueError(f"{label} has an unexpected download URL")


def destination_for(entry: dict) -> Path:
    dest = REPO_ROOT / entry["path"]
    if not dest.resolve().is_relative_to(REPO_ROOT.resolve()):
        raise ValueError(f"restore path escapes repository through a symlink: {entry['path']!r}")
    return dest


RELEASE_URL = re.compile(r"^https://github\.com/([^/]+)/([^/]+)/releases/download/([^/]+)/([^/]+)$")
_token_cache: list = []
_asset_cache: dict = {}


def github_token() -> str:
    """Return a GitHub token from the environment or the gh CLI, or ""."""
    if not _token_cache:
        token = os.environ.get("GH_TOKEN") or os.environ.get("GITHUB_TOKEN") or ""
        if not token:
            try:
                token = subprocess.run(["gh", "auth", "token"], capture_output=True, text=True, timeout=10).stdout.strip()
            except (OSError, subprocess.SubprocessError):
                token = ""
        _token_cache.append(token)
    return _token_cache[0]


def api_asset_url(url: str, token: str) -> str:
    """Resolve a release download URL to its API asset URL, or ""."""
    m = RELEASE_URL.match(url)
    if not m:
        return ""
    owner, repo, tag, name = m.groups()
    key = (owner, repo, tag)
    if key not in _asset_cache:
        req = urllib.request.Request(f"https://api.github.com/repos/{owner}/{repo}/releases/tags/{tag}")
        req.add_header("Accept", "application/vnd.github+json")
        req.add_header("Authorization", f"Bearer {token}")
        with urllib.request.urlopen(req, timeout=60) as resp:
            release = json.load(resp)
        _asset_cache[key] = {a["name"]: a["url"] for a in release.get("assets", [])}
    return _asset_cache[key].get(name, "")


def fetch_bytes(url: str) -> bytes:
    """Fetch a release asset, retrying through the API when the public URL is refused."""
    try:
        with urllib.request.urlopen(url, timeout=60) as resp:
            return resp.read()
    except urllib.error.HTTPError as exc:
        token = github_token() if exc.code in (401, 403, 404) else ""
        if not token:
            raise
        asset = api_asset_url(url, token)
        if not asset:
            raise
        req = urllib.request.Request(asset)
        req.add_header("Accept", "application/octet-stream")
        # The API redirects to signed storage; the token must not follow.
        req.add_unredirected_header("Authorization", f"Bearer {token}")
        with urllib.request.urlopen(req, timeout=120) as resp:
            return resp.read()


def download(url: str, dest: Path, expected_sha256: str, expected_size: int) -> bool:
    """Download url to dest, verify sha256 and size. Returns True on success."""
    dest.parent.mkdir(parents=True, exist_ok=True)
    try:
        print(f"  downloading {dest.relative_to(REPO_ROOT)} ...", end=" ", flush=True)
        data = fetch_bytes(url)
    except (urllib.error.URLError, OSError, ValueError) as exc:
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
    dest = destination_for(entry)
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

    try:
        manifest = load_manifest()
    except (OSError, json.JSONDecodeError, ValueError) as exc:
        print(f"error: invalid evidence archive manifest: {exc}", file=sys.stderr)
        return 1
    entries = manifest["entries"]

    if args.path:
        entries = [e for e in entries if e["path"] == args.path]
        if not entries:
            print(f"error: path not in manifest: {args.path}", file=sys.stderr)
            return 1

    ok_count = skipped = failed = 0

    for entry in entries:
        try:
            dest = destination_for(entry)
        except ValueError as exc:
            print(f"error: {exc}", file=sys.stderr)
            return 1
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
