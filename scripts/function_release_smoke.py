#!/usr/bin/env python3
"""Check optional function evidence using real core and native worker executables."""
import argparse
import hashlib
import importlib.util
import json
from pathlib import Path
import re
import shutil
import subprocess
import tempfile

import wheels

ROOT = Path(__file__).resolve().parents[1]
BREADTH = ROOT / 'tests/structural_breadth'
FUNCTIONS = ROOT / 'tests/functions'
DEFAULT_CHECKS = {'default_omits_functions', 'default_repeat_exact'}
FUNCTION_CHECKS = {'one_eight_workers', 'source_hash_and_native_parity', 'single_parse',
                   'default_fields_unchanged', 'no_nested_file_duplication',
                   'known_java_csharp_python_spans', 'qualified_coverage',
                   'per_file_cap', 'combined_modules'}


def require(condition, message):
    if not condition:
        raise ValueError(message)


def sha(data):
    return hashlib.sha256(data).hexdigest()


def file_sha(path):
    return sha(path.read_bytes())


def functions_required(version):
    wheels.python_version(version)
    return tuple(int(value) for value in version.split('-')[0].split('.')) >= (0, 4, 0)


def source_inputs():
    paths = [Path(__file__), FUNCTIONS / 'run.py', BREADTH / 'fixtures.json',
             *sorted((FUNCTIONS / 'fixtures').glob('*'))]
    entries = json.loads((BREADTH / 'fixtures.json').read_text())
    paths += [BREADTH / 'testdata' / entry['path'] for entry in entries]
    return {path.relative_to(ROOT).as_posix(): file_sha(path) for path in sorted(paths)}


def probe_module():
    spec = importlib.util.spec_from_file_location('release_function_probes', FUNCTIONS / 'run.py')
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def command_output(command):
    completed = subprocess.run(command, capture_output=True, timeout=120)
    require(completed.returncode == 0 and not completed.stderr,
            f'function release smoke failed: exit {completed.returncode}, stderr {completed.stderr[-2000:]!r}')
    return completed.stdout


def valid_digest(value):
    return isinstance(value, str) and re.fullmatch('[0-9a-f]{64}', value) is not None


def validate_receipt(receipt, version, candidate_sha256, worker_sha256):
    require(isinstance(receipt, dict), 'function smoke receipt must be an object')
    required = functions_required(version)
    require(receipt.get('schema_version') == '1.0.0' and receipt.get('passed') is True and
            receipt.get('version') == version and receipt.get('functions_required') is required,
            'function smoke version/status mismatch')
    require(receipt.get('candidate_sha256') == candidate_sha256 and
            receipt.get('worker_sha256') == worker_sha256, 'function smoke executable identity mismatch')
    require(receipt.get('source_sha256') == source_inputs(), 'function smoke source inputs mismatch')
    require(receipt.get('fixture_count') == 21 and receipt.get('language_count') == 20,
            'function smoke fixture coverage mismatch')
    checks = receipt.get('checks', [])
    expected = DEFAULT_CHECKS | (FUNCTION_CHECKS if required else set())
    require(isinstance(checks, list) and all(isinstance(check, str) for check in checks) and
            len(checks) == len(expected) and set(checks) == expected,
            'function smoke check inventory mismatch')
    require(valid_digest(receipt.get('default_stdout_sha256')),
            'function smoke default digest missing')
    rows = receipt.get('counterexamples', [])
    require(isinstance(rows, list) and all(isinstance(row, dict) for row in rows) and
            len(rows) == (11 if required else 0),
            'function smoke counterexample coverage mismatch')
    if required:
        require(receipt.get('known_span_languages') == ['C#', 'Java', 'Python'],
                'function smoke known-span languages mismatch')
        expected_cases = ['Examples.java', 'Examples.cs', 'examples.py', 'empty', 'recovery',
                          'many', 'name_bound', 'suppression', "unicode_''", "unicode_'\\n'", "unicode_'\\r\\n'"]
        require([row.get('case') for row in rows] == expected_cases,
                'function smoke named cases mismatch')
        require(all(valid_digest(row.get('source_sha256')) for row in rows),
                'function smoke source digest missing')
        by_case = {row['case']: row for row in rows}
        require(by_case['many']['total_spaces'] == 140 and by_case['many']['omitted_spaces'] == 12 and
                by_case['many']['retained_spaces'] == 128 and by_case['many']['status'] == 'partial',
                'function smoke capped population mismatch')
        require(by_case['recovery']['status'] == by_case['name_bound']['status'] == 'partial',
                'function smoke incomplete evidence was unqualified')
        require(receipt.get('generated_source_omission') is True and receipt.get('parent_coverage_partial') is True,
                'function smoke parent coverage missing')
    return receipt


def run(candidate, worker, version, baseline_worker=None):
    required = functions_required(version)
    candidate, worker = candidate.resolve(), worker.resolve()
    require(command_output([str(candidate), '--version']) == f'dircue {version}\n'.encode(),
            'function smoke core version mismatch')
    entries = json.loads((BREADTH / 'fixtures.json').read_text())
    receipt = {'schema_version': '1.0.0', 'version': version, 'functions_required': required,
               'candidate_sha256': file_sha(candidate), 'worker_sha256': file_sha(worker),
               'source_sha256': source_inputs(), 'fixture_count': len(entries),
               'language_count': len({entry['language'] for entry in entries}), 'counterexamples': []}
    probes = probe_module()
    with tempfile.TemporaryDirectory(prefix='dircue-packaged-functions-') as temp:
        root = Path(temp)
        for entry in entries:
            shutil.copyfile(BREADTH / 'testdata' / entry['path'], root / entry['path'])
        base = [str(candidate), 'analyze', 'structure', '--source', 'directory', '--json', '--files',
                '--structural-worker', str(worker), str(root)]
        default = command_output([*base, '--workers', '1'])
        baseline = json.loads(default)
        require('functions' not in baseline['structure'] and
                all('functions' not in row for row in baseline['structure']['files']),
                'default path emitted opt-in function evidence')
        require(baseline['structure']['status'] == 'complete' and
                baseline['structure']['parse_count'] == len(entries) and
                {row['path'] for row in baseline['structure']['files']} == {entry['path'] for entry in entries},
                'default supported fixture coverage mismatch')
        if required:
            first = command_output([*base, '--functions', '--workers', '1'])
            second = command_output([*base, '--functions', '--workers', '8'])
            require(first == second, 'function output depends on worker scheduling')
            enriched = json.loads(first)
            functions = enriched['structure']['functions']
            require(enriched['schema_version'] == '1.3.0' and functions['status'] == 'complete',
                    'supported function fixture coverage/schema mismatch')
            require(enriched['structure']['parse_count'] == len(entries), 'duplicate function parse')
            require(all('functions' not in row and 'source_sha256' not in row
                        for row in enriched['structure']['files']), 'function evidence duplicated in file rows')
            actual = {(row['path'], row['index']): row for row in functions['entries']}
            expected, total = {}, 0
            for entry in entries:
                source = (root / entry['path']).read_bytes()
                request = {'path': entry['path'], 'language': entry['language'],
                           'source': source.decode('utf-8'), 'mode': 'combined', 'functions': True}
                direct = probes.report(worker, request)
                probes.validate_functions(direct['functions'], source.decode('utf-8'))
                total += direct['functions']['total_spaces']
                for row in direct['functions']['entries']:
                    expected[(entry['path'], row['index'])] = {**row, 'path': entry['path'],
                                                              'language': entry['language'], 'source_sha256': sha(source)}
            require(actual == expected and len(actual) == len(functions['entries']) and
                    functions['total_spaces'] == total and functions['omitted_spaces'] == 0,
                    'packaged function source, population, span or native metric mismatch')
            del enriched['structure']['functions']
            enriched['schema_version'] = baseline['schema_version']
            require(enriched == baseline, 'function opt-in changed default report fields')
            combined = json.loads(command_output([str(candidate), 'analyze', 'all', '--source', 'directory',
                '--json', '--files', '--projects', '--metrics', '--structure', '--functions',
                '--structural-worker', str(worker), str(root)]))
            require(combined['structure']['functions'] == functions, 'combined modules changed function evidence')
            for row in probes.counterexamples(worker):
                f = row['functions']
                receipt['counterexamples'].append({'case': row.get('path', row.get('case')),
                    'source_sha256': row['source_sha256'], 'status': f['status'],
                    'total_spaces': f['total_spaces'], 'omitted_spaces': f['omitted_spaces'],
                    'retained_spaces': len(f['entries'])})
            known = probes.cli_counterexamples(candidate, worker)['functions']
            require(known['scope'] == 'selected-source-files' and
                    known['status'] == known['parent_status'] == 'complete' and
                    known['omissions'].get('outside_scope') == 2,
                    'generated source exclusion changed selected-scope coverage')
            # Excluded files need not make the parent's selected structural scope
            # incomplete. Parser recovery does, and must propagate independently.
            with tempfile.TemporaryDirectory(prefix='dircue-function-recovery-') as recovery_temp:
                recovery_root = Path(recovery_temp)
                (recovery_root / 'broken.py').write_bytes(b'def broken(:\n    return 1\n')
                recovered = json.loads(command_output([str(candidate), 'analyze', 'structure',
                    '--source', 'directory', '--json', '--functions', '--structural-worker',
                    str(worker), str(recovery_root)]))['structure']
                require(recovered['status'] == recovered['functions']['parent_status'] ==
                        recovered['functions']['status'] == 'partial',
                        'parser recovery lost parent function coverage')
            receipt.update(known_span_languages=['C#', 'Java', 'Python'], generated_source_omission=True,
                           parent_coverage_partial=True)
            if baseline_worker:
                old = baseline_worker.resolve()
                refused = subprocess.run([*base[:base.index('--structural-worker')], '--functions',
                    '--structural-worker', str(old), str(root)], capture_output=True, timeout=120)
                require(refused.returncode != 0 and not refused.stdout and
                        b'updated worker' in refused.stderr, 'older worker did not clearly refuse function request')
                receipt['older_worker_refusal'] = {'worker_sha256': file_sha(old), 'exit_code': refused.returncode,
                                                   'stdout_bytes': 0, 'update_instruction': True}
        require(command_output([*base, '--workers', '1']) == default, 'default output changed after opt-in checks')
        receipt['default_stdout_sha256'] = sha(default)
    receipt['checks'] = sorted(DEFAULT_CHECKS | (FUNCTION_CHECKS if required else set()))
    receipt['passed'] = True
    return validate_receipt(receipt, version, file_sha(candidate), file_sha(worker))


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--candidate', type=Path, required=True)
    parser.add_argument('--worker', type=Path, required=True)
    parser.add_argument('--version', required=True)
    parser.add_argument('--output', type=Path, required=True)
    parser.add_argument('--baseline-worker', type=Path)
    args = parser.parse_args()
    require(not args.output.exists(), 'choose a fresh function smoke receipt path')
    receipt = run(args.candidate, args.worker, args.version, args.baseline_worker)
    args.output.parent.mkdir(parents=True, exist_ok=True)
    args.output.write_text(json.dumps(receipt, indent=2, allow_nan=False) + '\n')
    print(f'Packaged function smoke passed; version={args.version}; functions_required={receipt["functions_required"]}')


if __name__ == '__main__':
    main()
