#!/usr/bin/env python3
"""Validate retained composition reports with the project's existing Go schema validator."""
import argparse
import json
import os
from pathlib import Path
import subprocess
import tempfile


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--output', type=Path, required=True, help='Composition report directory')
    args = parser.parse_args()
    source = Path(__file__).resolve().parent
    root = source.parents[2]
    reports = args.output.resolve()
    if not reports.is_dir():
        parser.error('composition output directory does not exist')
    with tempfile.TemporaryDirectory(prefix='dircue-composition-schema-') as temp:
        overlay = Path(temp) / 'overlay.json'
        overlay.write_text(json.dumps({'Replace': {
            str(root / 'schema/final_v040_composition_audit_test.go'):
                str(source / 'schema_test.go.txt')}}))
        result = subprocess.run([
            'go', 'test', '-overlay', str(overlay), './schema',
            '-run', '^TestFinalV040CompositionAudit$', '-count=1'],
            cwd=root, env=dict(os.environ, GOMAXPROCS='2', DIRCUE_COMPOSITION_REPORTS=str(reports)),
            capture_output=True, timeout=180)
    (reports / 'composition-schema.log').write_bytes(result.stdout + result.stderr)
    print((result.stdout + result.stderr).decode(), end='')
    raise SystemExit(result.returncode)


if __name__ == '__main__':
    main()
