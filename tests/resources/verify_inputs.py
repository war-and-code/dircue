#!/usr/bin/env python3
"""Verify selected stress payloads and packed objects against retained receipts."""
import hashlib
import json
from pathlib import Path
import sys


def digest(path):
    with path.open('rb') as stream:
        return hashlib.file_digest(stream, 'sha256').hexdigest()


def regular(path):
    if path.is_symlink() or not path.is_file():
        raise ValueError(f'not a regular fixture file: {path}')


def verify(root, manifest):
    fixtures = {f['name']: f for f in manifest['fixtures']}
    checked = []
    for name, variant_name in [('etl-pipeline', 'generated-excluded'), ('xml-log', 'default')]:
        variant = next(v for v in fixtures[name]['variants'] if v['name'] == variant_name)
        base = root / variant['flat']
        expected = {f['path'] for f in variant['files']}
        actual = {str(p.relative_to(base)) for p in base.rglob('*') if not p.is_dir()}
        if actual != expected:
            raise ValueError(f'fixture paths changed: {name}')
        for f in variant['files']:
            path = base / f['path']
            regular(path)
            if path.stat().st_size != f['bytes'] or digest(path) != f['sha256']:
                raise ValueError(f'fixture payload changed: {path}')
        checked.append({'fixture': name, 'variant': variant_name,
                        'files': len(expected), 'bytes': sum(f['bytes'] for f in variant['files'])})
    for name, key, files_key in [('dotnet-graph', 'packed_history', 'pack_files'),
                                  ('etl-pipeline-packed', 'packed_backend', 'packs')]:
        fixture = fixtures[name]
        base = root / fixture['git'] / '.git' / 'objects'
        entries = fixture[key][files_key]
        expected = {f['name'] for f in entries}
        actual = {p.name for p in (base / 'pack').iterdir() if p.is_file()}
        if actual != expected or (base / 'info' / 'alternates').exists():
            raise ValueError(f'packed object inventory changed: {name}')
        # Extra loose objects could shadow the verified packed objects.
        if any(p.is_dir() for p in base.iterdir() if p.name not in ('pack', 'info')):
            raise ValueError(f'unexpected loose object directories: {name}')
        for f in entries:
            path = base / 'pack' / f['name']
            regular(path)
            if path.stat().st_size != f['bytes'] or digest(path) != f['sha256']:
                raise ValueError(f'packed bytes changed: {path}')
        checked.append({'fixture': name, 'revision': fixture['variants'][-1]['revision'],
                        'pack_files': entries})
    return checked


def main():
    expected = Path('/expected/manifest.json')
    actual = Path('/stress') / sys.argv[1] / 'manifest.json'
    if expected.read_bytes() != actual.read_bytes():
        raise ValueError('volume manifest differs from retained host receipt')
    manifest = json.loads(actual.read_bytes())
    result = {'manifest_sha256': digest(actual), 'checked': verify(actual.parent, manifest)}
    print(json.dumps(result, indent=2))


if __name__ == '__main__':
    main()
