#!/usr/bin/env python3
"""Replay the frozen pre-rename v0.1 comparison with its original audit helpers.

The final archive identifies the binary under its former project name. Its
measurements, labels, source hashes, and original recorder remain immutable.
Current measurement harnesses can evolve independently of this historical audit.
"""
import argparse
import hashlib
import io
from pathlib import Path, PurePosixPath
import subprocess
import sys
import tarfile
import tempfile

HERE = Path(__file__).resolve().parent
ARCHIVE_SHA256 = "5431ba1fd20e888e1546cf8357442c2f48bac2c94ba44e0ec876da34e3b73243"
HELPERS = {
    "harness/record_final.py": "tests/enry-performance/record_final.py",
    "harness/record_library.py": "tests/enry-performance/record_library.py",
    "harness/record_subset.py": "tests/profiling/record_subset.py",
    "harness/compare.py": "tests/enry-performance/compare.py",
    "harness/measurement.py": "tests/stress/compare.py",
}


def archived_helpers(raw):
    """Accept only the known archive before loading any executable helper bytes."""
    if hashlib.sha256(raw).hexdigest() != ARCHIVE_SHA256:
        raise ValueError("final evidence archive does not match its pinned SHA-256")
    helpers = {}
    seen = set()
    total = 0
    with tarfile.open(fileobj=io.BytesIO(raw), mode="r:gz") as archive:
        for member in archive:
            path = PurePosixPath(member.name)
            if (not member.isfile() or member.name in seen or path.is_absolute()
                    or ".." in path.parts or str(path) != member.name):
                raise ValueError("unsafe evidence archive member")
            seen.add(member.name)
            total += member.size
            if member.size > 128 * 1024**2 or total > 768 * 1024**2:
                raise ValueError("evidence archive exceeds size limit")
            if member.name in HELPERS:
                helpers[HELPERS[member.name]] = archive.extractfile(member).read()
    if set(helpers) != set(HELPERS.values()):
        raise ValueError("historical audit helpers are incomplete")
    return helpers


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--audit-only", action="store_true", required=True,
                        help="Replay retained evidence without running benchmarks")
    parser.add_argument("--output", type=Path, default=HERE / "results/final",
                        help="Directory containing the immutable final evidence")
    args = parser.parse_args()
    output = args.output.resolve()
    helpers = archived_helpers((output / "evidence.tar.gz").read_bytes())
    with tempfile.TemporaryDirectory(prefix="dircue-final-audit-") as temporary:
        root = Path(temporary)
        # Only fixed destinations are written; archive paths are never extracted.
        for name, content in helpers.items():
            destination = root / name
            destination.parent.mkdir(parents=True, exist_ok=True)
            destination.write_bytes(content)
        subprocess.run([
            sys.executable, "-I", str(root / HELPERS["harness/record_final.py"]),
            "--audit-only", "--output", str(output),
        ], check=True, cwd=root)


if __name__ == "__main__":
    main()
