#!/usr/bin/env python3
"""Compare a deterministic sample of committed Java/C# files with standalone scc."""
import argparse
import hashlib
import json
from pathlib import Path
import subprocess
import tempfile

from run import COUNTERS, counted, execute, metrics


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--candidate', type=Path, required=True)
    parser.add_argument('--scc', type=Path, required=True)
    parser.add_argument('--project', action='append', required=True, help='NAME=PATH')
    parser.add_argument('--sample', type=int, default=200)
    parser.add_argument('--output', type=Path, required=True)
    args = parser.parse_args()
    if args.sample < 1:
        parser.error('sample must be positive')
    binary, scc = str(args.candidate.resolve()), str(args.scc.resolve())
    receipt = {'schema_version': '1.0.0', 'candidate_sha256': hashlib.sha256(Path(binary).read_bytes()).hexdigest(),
               'scc_sha256': hashlib.sha256(Path(scc).read_bytes()).hexdigest(),
               'scc_version': execute([scc, '--version']).strip(), 'sample_order': 'SHA-256 of relative path', 'projects': []}
    for argument in args.project:
        name, folder = argument.split('=', 1)
        root = Path(folder).resolve()
        commit = execute(['git', '-C', str(root), 'rev-parse', 'HEAD']).strip()
        report = metrics(binary, root, '--source', 'git')
        available = [row for row in counted(report).values() if row['grammar'] in ('Java', 'C#')]
        available.sort(key=lambda row: hashlib.sha256(row['path'].encode()).digest())
        selected = available[:args.sample]
        assert selected, name
        project = {'name': name, 'commit': commit, 'tree': report['tree'], 'totals': report['totals'],
                   'status': report['status'], 'skipped': report['skipped'], 'available_java_csharp_files': len(available), 'files': []}
        with tempfile.TemporaryDirectory(prefix='dircue-scc-corpus-') as directory:
            snapshots = {}
            for index, row in enumerate(selected):
                content = subprocess.check_output(['git', '-C', str(root), 'show', f'{commit}:{row["path"]}'])
                suffix = '.java' if row['grammar'] == 'Java' else '.cs'
                target = Path(directory)/f'sample-{index}{suffix}'
                target.write_bytes(content)
                snapshots[target.name] = (row, hashlib.sha256(content).hexdigest())
            reference = json.loads(execute([scc, '--no-config', '--no-cocomo', '--no-gitignore', '--no-ignore',
                                            '--no-scc-ignore', '--by-file', '--format', 'json', directory]))
            reference_files = [file for group in reference for file in group['Files']]
            assert len(reference_files) == len(selected), (name, len(reference_files), len(selected))
            for expected in reference_files:
                row, digest = snapshots[Path(expected['Location']).name]
                assert row['grammar'] == expected['Language'], (row['path'], row['grammar'], expected['Language'])
                for key, field in COUNTERS.items():
                    assert row['counts'][key] == expected[field], (name, row['path'], key, row['counts'][key], expected[field])
                project['files'].append({'path': row['path'], 'sha256': digest, 'counts': row['counts'], 'grammar': row['grammar']})
        project['files'].sort(key=lambda row: row['path'])
        project['result'] = 'pass'
        receipt['projects'].append(project)
        args.output.parent.mkdir(parents=True, exist_ok=True)
        args.output.write_text(json.dumps(receipt, indent=2)+'\n')
        print(json.dumps({'name': name, 'result': 'pass', 'matched_files': len(selected), 'totals': report['totals']}), flush=True)


if __name__ == '__main__':
    main()
