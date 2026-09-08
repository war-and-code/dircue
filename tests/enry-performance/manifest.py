#!/usr/bin/env python3
"""Inventory identical classifier inputs; hash measured prefixes, never execute files."""
import argparse
import hashlib
import json
import os
from pathlib import Path


def main():
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument('--root', type=Path, required=True)
    p.add_argument('--output', type=Path, required=True)
    p.add_argument('--prefix', type=int, default=131072, help='0 means full content')
    p.add_argument('--expected-files', type=int, help='3388 for the complete pinned sample corpus')
    a = p.parse_args()
    if a.prefix < 0 or a.output.exists():
        p.error('prefix must be nonnegative; output must not already exist')
    root = a.root.resolve()
    files = []
    for directory, dirs, names in os.walk(root, followlinks=False):
        dirs[:] = sorted(d for d in dirs if d != '.git' and not (Path(directory)/d).is_symlink())
        for name in sorted(names):
            path = Path(directory)/name
            if path.is_symlink() or not path.is_file():
                continue
            digest = hashlib.sha256()
            measured = 0
            with path.open('rb') as stream:
                remaining = a.prefix or None
                while remaining is None or remaining:
                    block = stream.read(min(1024*1024, remaining) if remaining is not None else 1024*1024)
                    if not block:
                        break
                    digest.update(block)
                    measured += len(block)
                    if remaining is not None:
                        remaining -= len(block)
            files.append({'path': path.relative_to(root).as_posix(), 'sha256': digest.hexdigest(),
                          'source_bytes': path.stat().st_size, 'measured_bytes': measured})
    files.sort(key=lambda f: f['path'])
    if not files or (a.expected_files is not None and len(files) != a.expected_files):
        p.error('input population does not match the declared count')
    a.output.parent.mkdir(parents=True, exist_ok=True)
    a.output.write_text(json.dumps({'root': str(root), 'prefix': a.prefix, 'files': files,
        'generator_sha256': hashlib.sha256(Path(__file__).read_bytes()).hexdigest()}, indent=2)+'\n')


if __name__ == '__main__':
    main()
