#!/usr/bin/env python3
"""Verify counter corrections against native Git bytes and standalone scc."""
import argparse
import hashlib
import json
from pathlib import Path
import subprocess
import tempfile

from run import COUNTERS, execute, metrics, reference


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--before', type=Path, required=True)
    parser.add_argument('--candidate', type=Path, required=True)
    parser.add_argument('--scc', type=Path, required=True)
    parser.add_argument('--project', action='append', required=True, help='NAME=PATH')
    parser.add_argument('--output', type=Path, required=True)
    args = parser.parse_args()
    before, candidate, scc = (str(path.resolve()) for path in (args.before, args.candidate, args.scc))
    report = {'schema_version': '1.0.0', 'binary_sha256': {name: hashlib.sha256(Path(binary).read_bytes()).hexdigest()
              for name, binary in [('before', before), ('candidate', candidate), ('scc', scc)]}, 'projects': []}
    for argument in args.project:
        name, path = argument.split('=', 1)
        root = Path(path).resolve()
        commit = execute(['git', '-C', str(root), 'rev-parse', 'HEAD']).strip()
        old = metrics(before, root, '--source', 'git')
        fixed = metrics(candidate, root, '--source', 'git')
        old_files = {row['path']: row for row in old['files']}
        fixed_files = {row['path']: row for row in fixed['files']}
        assert old_files.keys() == fixed_files.keys()
        changes = [path for path in fixed_files if old_files[path] != fixed_files[path]]
        entry = {'name': name, 'commit': commit, 'before_totals': old['totals'], 'fixed_totals': fixed['totals'], 'changes': []}
        with tempfile.TemporaryDirectory(prefix='dircue-reader-check-') as folder:
            for path in changes:
                row = fixed_files[path]
                assert row['status'] == 'counted', (path, row)
                content = subprocess.check_output(['git', '-C', str(root), 'show', f'{commit}:{path}'])
                snapshot = Path(folder)/Path(path).name
                snapshot.write_bytes(content)
                native = reference(scc, snapshot)
                assert native['Language'] == row['grammar'], (path, native['Language'], row['grammar'])
                for field, counter in COUNTERS.items():
                    assert row['counts'][field] == native[counter], (path, field, row['counts'][field], native[counter])
                entry['changes'].append({'path': path, 'native_blob_sha256': hashlib.sha256(content).hexdigest(),
                                         'before': old_files[path], 'fixed': row, 'standalone_matches_fixed': True})
        entry['result'] = 'pass'
        report['projects'].append(entry)
        args.output.parent.mkdir(parents=True, exist_ok=True)
        args.output.write_text(json.dumps(report, indent=2)+'\n')
        print(json.dumps({'name': name, 'changed_files': changes, 'result': 'pass'}), flush=True)


if __name__ == '__main__':
    main()
