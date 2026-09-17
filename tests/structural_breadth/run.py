#!/usr/bin/env python3
"""Validate every enabled structural language through the CLI and native worker."""
import argparse
import hashlib
import json
from pathlib import Path
import platform
import shutil
import subprocess
import tempfile

ROOT = Path(__file__).resolve().parent
BASIC_OBSERVATIONS = {'syntax_nodes', 'error_nodes', 'missing_nodes'}
COMPARE_FIELDS = ('status', 'language', 'source_bytes', 'parse_count', 'syntax_errors',
                  'observations', 'metrics', 'provenance')


def sha(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def execute(command, **kwargs):
    result = subprocess.run(command, capture_output=True, timeout=300, **kwargs)
    if result.returncode:
        raise AssertionError(f'{command}: exit {result.returncode}: {result.stderr[-2000:]!r}')
    return result.stdout


def request(worker, entry, source, mode='combined'):
    payload = {'path': entry['path'], 'language': entry['language'], 'source': source, 'mode': mode}
    return json.loads(execute([str(worker)], input=json.dumps(payload).encode()))


def command(candidate, worker, root, *options):
    return [str(candidate), 'analyze', 'structure', '--source', 'directory', '--json', '--files',
            '--structural-worker', str(worker), str(root), *options]


def require(condition, message):
    if not condition:
        raise AssertionError(message)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--candidate', type=Path, required=True)
    parser.add_argument('--worker', type=Path, required=True)
    parser.add_argument('--output', type=Path, required=True)
    args = parser.parse_args()
    candidate, worker = args.candidate.resolve(), args.worker.resolve()
    entries = json.loads((ROOT / 'fixtures.json').read_text())
    receipt = {
        'candidate_sha256': sha(candidate), 'worker_sha256': sha(worker),
        'harness_sha256': sha(Path(__file__)), 'manifest_sha256': sha(ROOT / 'fixtures.json'),
        'platform': platform.platform(), 'fixtures': [],
        'method': 'Handwritten syntax fixtures are naturally classified, then compared with direct '
                  'combined, structure-only, and metrics-only worker invocations. CLI reports must '
                  'match across one/eight workers and combined-module execution. This verifies '
                  'integration and parser acceptance, not independent correctness of BCA metric formulas '
                  'or comprehensive language-version support.'}
    with tempfile.TemporaryDirectory(prefix='dircue-structural-breadth-') as temporary:
        root = Path(temporary)
        for entry in entries:
            shutil.copyfile(ROOT / 'testdata' / entry['path'], root / entry['path'])
        first = execute(command(candidate, worker, root, '--workers', '1'))
        second = execute(command(candidate, worker, root, '--workers', '8'))
        require(first == second, 'one/eight worker reports differ')
        structural = json.loads(first)['structure']
        require(structural['status'] == 'complete', f'incomplete supported fixture coverage: {structural}')
        require(structural['parse_count'] == len(entries), 'expected exactly one parse per fixture')
        require(structural['analyzed_files'] == len(entries), 'missing fixture analysis')
        actual = {entry['path']: entry for entry in structural['files']}
        require(set(actual) == {entry['path'] for entry in entries}, 'unexpected structural file set')
        for entry in entries:
            path = entry['path']
            source_path = root / path
            source = source_path.read_bytes().decode('utf-8')
            direct = request(worker, entry, source)
            observed = actual[path]
            for key in COMPARE_FIELDS:
                require(observed[key] == direct[key], f'{path}: production/direct {key} mismatch')
            require(observed['status'] == entry['status'], f'{path}: unexpected parser recovery')
            require(observed['parse_count'] == 1, f'{path}: duplicate parse')
            require(observed['observations']['syntax_nodes'] > 0, f'{path}: empty syntax traversal')
            require(observed['metrics'], f'{path}: missing upstream metrics')
            if entry['language'] not in {'Java', 'C#'}:
                require(set(observed['observations']) == BASIC_OBSERVATIONS,
                        f'{path}: unsupported custom declaration counts must be absent')
            structure_only = request(worker, entry, source, 'structure')
            metrics_only = request(worker, entry, source, 'metrics')
            require(structure_only['observations'] == direct['observations'], f'{path}: structure mode differs')
            require(metrics_only['metrics'] == direct['metrics'], f'{path}: metrics mode differs')
            require(structure_only['parse_count'] == metrics_only['parse_count'] == 1,
                    f'{path}: mode parse count')
            require('metrics' not in structure_only and 'observations' not in metrics_only,
                    f'{path}: mode isolation')
            receipt['fixtures'].append({**entry, 'sha256': sha(source_path), 'provenance': direct['provenance'],
                                        'observation_fields': sorted(observed['observations']),
                                        'metric_groups': sorted(observed['metrics'])})
        combined = json.loads(execute([str(candidate), 'analyze', 'all', '--source', 'directory',
                                      '--json', '--files', '--projects', '--metrics', '--structure',
                                      '--structural-worker', str(worker), str(root)]))
        require(combined['structure'] == structural, 'combined-module structural result differs')
        receipt['supported_report_sha256'] = hashlib.sha256(json.dumps(structural, sort_keys=True).encode()).hexdigest()

        (root / 'unsupported.swift').write_text('func twice(_ value: Int) -> Int { return value * 2 }\n')
        (root / 'log.xml').write_text('<logs><entry>ordinary data</entry></logs>\n')
        (root / 'generated.go').write_text('package generated\nfunc Twice(v int) int { return v * 2 }\n')
        (root / 'broken.py').write_text('def broken(:\n    return 1\n')
        (root / '.gitattributes').write_text('generated.go linguist-generated=true\n')
        qualified = json.loads(execute(command(candidate, worker, root)))['structure']
        require(qualified['status'] == 'partial', 'syntax recovery/unsupported input must qualify coverage')
        rows = {entry['path']: entry for entry in qualified['files']}
        require(rows['broken.py']['status'] == 'partial' and rows['broken.py']['syntax_errors'],
                'Python syntax recovery was lost')
        require(rows['unsupported.swift']['status'] == 'skipped' and
                rows['unsupported.swift']['reason'] == 'unsupported_language', 'unsupported language omission lost')
        for path in ['log.xml', 'generated.go']:
            require(rows[path]['status'] == 'skipped' and rows[path]['parse_count'] == 0,
                    f'{path}: excluded content was parsed')
        require(qualified['parse_count'] == len(entries) + 1, 'excluded inputs caused extra parses')
        receipt['qualified_inputs'] = [{key: rows[path].get(key) for key in
                                       ('path', 'status', 'reason', 'parse_count', 'syntax_errors')}
                                      for path in ['unsupported.swift', 'log.xml', 'generated.go', 'broken.py']]
    receipt['language_count'] = len({entry['language'] for entry in entries})
    receipt['fixture_count'] = len(entries)
    receipt['checks'] = ['natural_classification', 'one_parse_per_file', 'direct_worker_parity',
                         'mode_isolation', 'one_eight_worker_determinism', 'combined_module_equivalence',
                         'unsupported_language', 'data_and_generated_exclusions', 'syntax_recovery']
    receipt['passed'] = True
    args.output.parent.mkdir(parents=True, exist_ok=True)
    args.output.write_text(json.dumps(receipt, indent=2) + '\n')
    print(f"Passed: {receipt['fixture_count']} fixtures, {receipt['language_count']} languages, "
          'worker parity, determinism, combined modules, and qualified omissions')


if __name__ == '__main__':
    main()
