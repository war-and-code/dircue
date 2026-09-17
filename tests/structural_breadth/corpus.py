#!/usr/bin/env python3
"""Compare sampled committed files from eight more languages with the pinned worker."""
import argparse
import hashlib
import json
from pathlib import Path
import platform
import tempfile

from run import COMPARE_FIELDS, command, execute, request, require, sha

ROOT = Path(__file__).resolve().parents[2]
CORPORA = [('flask', 'Python', '.py'), ('express', 'JavaScript', '.js'),
           ('typescript', 'TypeScript', '.ts'), ('jq', 'C', '.c'),
           ('ripgrep', 'Rust', '.rs'), ('cobra', 'Go', '.go'),
           ('laravel', 'PHP', '.php'), ('rails', 'Ruby', '.rb')]


def git(root, *args):
    return execute(['git', '-C', str(root), *args])


def sample(root, suffix, count):
    entries = []
    for row in git(root, 'ls-tree', '-r', '-z', '--long', 'HEAD').split(b'\0'):
        if not row:
            continue
        metadata, raw_path = row.split(b'\t', 1)
        mode, kind, object_id, size = metadata.split()
        path = raw_path.decode('utf-8')
        if mode not in {b'100644', b'100755'} or kind != b'blob' or not path.endswith(suffix):
            continue
        if 0 < int(size) <= 1024 * 1024:
            entries.append({'path': path, 'bytes': int(size), 'blob': object_id.decode()})
    entries.sort(key=lambda entry: entry['path'])
    require(len(entries) >= count, f'{root.name}: fewer than {count} eligible files')
    # Half the sample spans sorted paths; half includes the largest files.
    spread = count // 2
    chosen = {entries[index * (len(entries) - 1) // (spread - 1)]['path'] for index in range(spread)}
    for entry in sorted(entries, key=lambda entry: (-entry['bytes'], entry['path'])):
        if len(chosen) == count:
            break
        chosen.add(entry['path'])
    return [entry for entry in entries if entry['path'] in chosen]


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--candidate', type=Path, required=True)
    parser.add_argument('--worker', type=Path, required=True)
    parser.add_argument('--corpus-root', type=Path, required=True)
    parser.add_argument('--output', type=Path, required=True)
    parser.add_argument('--files', type=int, default=20)
    args = parser.parse_args()
    if args.files < 4:
        parser.error('--files must be at least 4')
    candidate, worker = args.candidate.resolve(), args.worker.resolve()
    pins = {entry['name']: entry for entry in json.loads((ROOT / 'tests/performance/corpus.json').read_text())['projects']}
    receipt = {'candidate_sha256': sha(candidate), 'worker_sha256': sha(worker),
               'harness_sha256': sha(Path(__file__)), 'platform': platform.platform(), 'corpora': [],
               'method': 'Committed Git blobs from pinned repositories; half the deterministic sample '
                         'spans sorted paths and the remainder takes largest files, excluding empty files '
                         'and inputs over 1 MiB. Attributes admit generated/vendor/documentation samples. '
                         'Each CLI result matches direct pinned-worker observations, status, metrics, and '
                         'provenance; one/eight-worker output must be identical. Integration evidence, '
                         'not independent metric ground truth or whole-repository performance.'}
    for name, language, suffix in CORPORA:
        root = args.corpus_root.resolve() / name
        revision = git(root, 'rev-parse', 'HEAD').decode().strip()
        require(revision == pins[name]['commit'], f'{name}: checkout revision differs from pin')
        files = sample(root, suffix, args.files)
        with tempfile.TemporaryDirectory(prefix='dircue-breadth-corpus-') as temporary:
            stage = Path(temporary)
            for entry in files:
                source = git(root, 'cat-file', 'blob', entry['blob'])
                target = stage / entry['path']
                target.parent.mkdir(parents=True, exist_ok=True)
                target.write_bytes(source)
                entry['sha256'] = hashlib.sha256(source).hexdigest()
            (stage / '.gitattributes').write_text(f'*{suffix} linguist-vendored=false linguist-generated=false linguist-documentation=false linguist-detectable=true\n')
            first = execute(command(candidate, worker, stage, '--workers', '1'))
            second = execute(command(candidate, worker, stage, '--workers', '8'))
            require(first == second, f'{name}: nondeterministic report')
            structure = json.loads(first)['structure']
            actual = {entry['path']: entry for entry in structure['files']}
            require(structure['analyzed_files'] == structure['parse_count'] == len(files), f'{name}: unexpected coverage')
            partials = []
            for entry in files:
                path = entry['path']
                source = (stage / path).read_bytes().decode('utf-8')
                direct = request(worker, {'path': path, 'language': language}, source)
                observed = actual[path]
                for key in COMPARE_FIELDS:
                    require(observed[key] == direct[key], f'{name}:{path}:{key}: production/direct mismatch')
                if observed['status'] == 'partial':
                    partials.append({'path': path, 'syntax_errors': observed['syntax_errors'],
                                     'error_nodes': observed['observations']['error_nodes'],
                                     'missing_nodes': observed['observations']['missing_nodes']})
            require(structure['status'] == ('partial' if partials else 'complete'), f'{name}: aggregate status')
            receipt['corpora'].append({'name': name, 'language': language, 'commit': revision, 'files': files,
                                       'analyzed_files': len(files), 'partial_files': partials,
                                       'status': structure['status'], 'parse_count': structure['parse_count'],
                                       'structural_report_sha256': hashlib.sha256(json.dumps(structure, sort_keys=True).encode()).hexdigest()})
            print(f'{name}: {len(files)} exact comparisons; {len(partials)} qualified partial results', flush=True)
    receipt['passed'] = True
    args.output.parent.mkdir(parents=True, exist_ok=True)
    args.output.write_text(json.dumps(receipt, indent=2) + '\n')


if __name__ == '__main__':
    main()
