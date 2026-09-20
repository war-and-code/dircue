#!/usr/bin/env python3
"""Verify population-wide hotspot evidence from real packaged core and worker binaries."""
import argparse
import copy
import hashlib
import json
import shutil
from pathlib import Path
import subprocess
import tempfile

import formats_release_smoke as formats

ROOT = Path(__file__).resolve().parents[1]
CHECKS = {'core_version', 'default_omission', 'default_repeat_exact', 'one_eight_workers',
          'standalone_combined_parity', 'default_fields_unchanged', 'single_parse',
          'late_function_beyond_file_cap', 'late_file_beyond_report_cap', 'exact_population',
          'histogram_partition', 'deterministic_top_order', 'source_bound_spans',
          'clean_recovered_separation', 'independent_function_opt_in', 'nested_scope_disclosed',
          'empty_population', 'missing_worker_refused', 'oracle_mutations_rejected', 'saved_report_compare_without_source'}


def fixture_bytes():
    result = {f'a{file:02}.py': ''.join(f'def ordinary_{index}(x):\n    return x\n\n' for index in range(130)).encode()
              for file in range(9)}
    late = ''.join(f'def ordinary_{index}(x):\n    return x\n\n' for index in range(140))
    late += 'def late_complex(x):\n' + ''.join(f'    if x == {index}:\n        return {index}\n' for index in range(40)) + '    return -1\n'
    result['zz_late.py'] = late.encode()
    result['nested.py'] = b'def outer(x):\n    def inner(y):\n        return y\n    return inner(x)\n'
    result['Example.java'] = b'class Example { int branch(int x) { if (x > 0) return x; return 0; } }\n'
    result['Example.cs'] = b'class Example { int Branch(int x) { if (x > 0) return x; return 0; } }\n'
    return result


FIXTURES = fixture_bytes()
RECOVERY = b'def recovered(x):\n    if x:\n        return 1\n    return 0\n\ndef broken(:\n    return 1\n'
FACTS = {'python_clean_spaces': 1313, 'clean_spaces': 1315, 'clean_files': 13,
         'late_function_index': 141, 'late_cyclomatic_sum': 41, 'late_span_lines': 82,
         'retained_top_limit': 10, 'histogram_buckets': 65,
         'languages': ['C#', 'Java', 'Python'], 'ranking_before_retention': True}
require, sha, valid_digest = formats.require, formats.sha, formats.valid_digest
required = formats.required


def source_inputs():
    return {path.relative_to(ROOT).as_posix(): sha(path.read_bytes()) for path in
            (Path(__file__).resolve(), ROOT / 'scripts/formats_release_smoke.py', ROOT / 'scripts/wheels.py')}


def fixture_inputs():
    return {name: sha(data) for name, data in sorted({**FIXTURES, 'recovery.py': RECOVERY}.items())}


def validate_receipt(receipt, version, candidate_sha256, worker_sha256):
    require(isinstance(receipt, dict) and required(version), 'hotspot proof requires release 0.6 or later')
    require(receipt.get('schema_version') == '1.0.0' and receipt.get('version') == version and
            receipt.get('passed') is True, 'hotspot proof version/status mismatch')
    require(valid_digest(candidate_sha256) and valid_digest(worker_sha256) and
            receipt.get('candidate_sha256') == candidate_sha256 and receipt.get('worker_sha256') == worker_sha256,
            'hotspot proof executable identity mismatch')
    require(receipt.get('source_sha256') == source_inputs() and receipt.get('fixture_sha256') == fixture_inputs(),
            'hotspot proof input identity mismatch')
    require(receipt.get('checks') == sorted(CHECKS) and receipt.get('observed_facts') == FACTS,
            'hotspot proof coverage mismatch')
    digests = receipt.get('stdout_sha256')
    require(isinstance(digests, dict) and set(digests) == {'default_structure', 'hotspots', 'combined', 'functions', 'recovered', 'empty', 'self_compare'} and
            all(valid_digest(value) for value in digests.values()), 'hotspot proof output identity missing')
    require(receipt.get('worker_required') is True and receipt.get('source_removed_before_compare') is True, 'hotspot proof worker scope missing')
    if 'older_worker_refusal' in receipt:
        refusal = receipt['older_worker_refusal']
        require(isinstance(refusal, dict) and valid_digest(refusal.get('worker_sha256')) and
                type(refusal.get('exit_code')) is int and refusal['exit_code'] != 0 and
                refusal.get('stdout_bytes') == 0 and refusal.get('update_instruction') is True,
                'hotspot older-worker evidence incomplete')
    return receipt


def output(command):
    result = subprocess.run([str(part) for part in command], capture_output=True, timeout=120)
    require(result.returncode == 0 and not result.stderr,
            f'hotspot smoke failed: exit={result.returncode}, stderr SHA256={sha(result.stderr)}')
    return result.stdout


def check_distribution(report, source):
    require(report['provider'] == 'big-code-analysis@2.2.0' and report['rule'] == 'function-population' and
            report['rule_version'] == '1.0.0' and report['population'] == 'all_valid_span_function_spaces_before_retention' and
            report['limit'] == 10, 'hotspot population definition differs')
    require(report['order'] == 'value_desc_then_path_then_provider_index' and
            report['histogram_rule'] == 'bucket_0_is_zero;bucket_i_is_[2^(i-1),2^i-1]_for_i_1_to_64',
            'hotspot ranking/bin definitions differ')
    require([(g['language'], g['syntax_cohort']) for g in report['groups']] ==
            sorted((g['language'], g['syntax_cohort']) for g in report['groups']), 'hotspot group order differs')
    for group in report['groups']:
        require([m['metric'] for m in group['metrics']] == ['cyclomatic_sum', 'span_lines'], 'metric definitions missing')
        for metric in group['metrics']:
            count = group['total_spaces'] - group['invalid_span_spaces']
            require(metric['count'] == count and len(metric['histogram']) == 65 and
                    sum(metric['histogram']) == count and len(metric['top']) == min(10, count),
                    'histogram or retained population differs')
            top = metric['top']
            require([(-r['value'], r['path'], r['index']) for r in top] ==
                    sorted((-r['value'], r['path'], r['index']) for r in top), 'top evidence order differs')
            if count:
                require(metric['max'] == top[0]['value'] and metric['min'] <= top[-1]['value'], 'extrema disagree with top evidence')
            else:
                require(metric['min'] is None and metric['max'] is None, 'empty population acquired fabricated extrema')
            require(metric['metric_scope'] == ('includes_nested_spaces' if metric['metric'] == 'cyclomatic_sum' else 'includes_nested_spans'),
                    'overlapping metric scope not disclosed')
            for row in top:
                data = source[row['path']]
                require(row['path_status'] == 'present' and row['path_sha256'] == sha(row['path'].encode()) and
                        row['source_sha256'] == sha(data) and 1 <= row['start_line'] <= row['end_line'] <= len(data.splitlines()),
                        'hotspot source/span identity differs')
                require(metric['min'] <= row['value'] <= metric['max'] and metric['histogram'][row['value'].bit_length()] > 0,
                        'top value outside histogram/extrema')
                if metric['metric'] == 'span_lines':
                    require(row['value'] == row['end_line'] - row['start_line'] + 1, 'line span measure differs')


def check_clean(report):
    check_distribution(report, FIXTURES)
    require(report['status'] == report['file_coverage_status'] == 'complete' and report['analyzed_files'] == 13 and
            report['total_spaces'] == 1315 and report['invalid_span_spaces'] == report['recovered_files'] == 0,
            'authored clean function population differs')
    require([g['language'] for g in report['groups']] == FACTS['languages'], 'clean language cohorts differ')
    group = next(g for g in report['groups'] if g['language'] == 'Python')
    require(group['total_spaces'] == 1313 and group['analyzed_files'] == 11, 'Python population was sampled')
    for metric in group['metrics']:
        row = metric['top'][0]
        require(row['path'] == 'zz_late.py' and row['index'] == 141 and row['name'] == 'late_complex',
                'late high-valued function lost to old file/report retention caps')
        require(row['value'] == (41 if metric['metric'] == 'cyclomatic_sum' else 82), 'authored late function measure differs')


def check_mutations(report):
    def sampled(value):
        value['total_spaces'] = 1024
    def bad_histogram(value):
        value['groups'][0]['metrics'][0]['histogram'][0] += 1
    def bad_source(value):
        value['groups'][0]['metrics'][0]['top'][0]['source_sha256'] = '0' * 64
    def omit_late(value):
        group = next(g for g in value['groups'] if g['language'] == 'Python')
        group['metrics'][0]['top'][0]['index'] = 128
    for mutate in (sampled, bad_histogram, bad_source, omit_late):
        changed = copy.deepcopy(report)
        mutate(changed)
        try:
            check_clean(changed)
        except ValueError:
            continue
        raise ValueError('hotspot oracle failed to reject a planted false observation')


def run(candidate, worker, version, baseline_worker=None):
    require(required(version), 'hotspot smoke requires release 0.6 or later')
    candidate, worker = candidate.resolve(), worker.resolve()
    require(output([candidate, '--version']) == f'dircue {version}\n'.encode(), 'hotspot core version differs')
    receipt = {'schema_version': '1.0.0', 'version': version, 'candidate_sha256': sha(candidate.read_bytes()),
               'worker_sha256': sha(worker.read_bytes()), 'source_sha256': source_inputs(), 'fixture_sha256': fixture_inputs()}
    with tempfile.TemporaryDirectory(prefix='dircue hotspot smoke ') as temporary:
        root = Path(temporary) / 'source'
        root.mkdir()
        for name, data in FIXTURES.items():
            (root / name).write_bytes(data)
        base = [candidate, 'analyze', 'structure', '--source', 'directory', '--json', '--files', '--structural-worker', worker, root]
        default = output([*base, '--workers', '1'])
        baseline = json.loads(default)
        require('hotspots' not in baseline['structure'], 'default emitted opt-in hotspots')
        first = output([*base, '--hotspots', '--workers', '1'])
        require(first == output([*base, '--hotspots', '--workers', '8']), 'hotspot evidence depends on scheduling')
        enriched = json.loads(first)
        hotspots = enriched['structure']['hotspots']
        check_clean(hotspots)
        check_mutations(hotspots)
        require('functions' not in enriched['structure'] and enriched['structure']['parse_count'] == 13 and
                all('hotspots' not in row and 'source_sha256' not in row for row in enriched['structure']['files']),
                'hotspot option duplicated parsing/evidence or implied functions')
        del enriched['structure']['hotspots']
        enriched['schema_version'] = baseline['schema_version']
        require(enriched == baseline, 'hotspot option changed existing report fields')
        combined = output([candidate, 'analyze', 'all', '--source', 'directory', '--json', '--structure', '--hotspots',
                           '--structural-worker', worker, root])
        require(json.loads(combined)['structure']['hotspots'] == hotspots, 'standalone/combined hotspots differ')
        functions = output([*base, '--hotspots', '--functions'])
        enriched = json.loads(functions)['structure']
        require(enriched['hotspots'] == hotspots and len(enriched['functions']['entries']) <= 1024 and
                enriched['functions']['omitted_spaces'] > 0, 'function evidence retention changed full hotspot population')
        require(output([*base, '--workers', '1']) == default, 'default output changed after hotspot checks')
        missing = subprocess.run([str(candidate), 'analyze', 'structure', '--hotspots', '--json', str(root)], capture_output=True, timeout=30)
        require(missing.returncode != 0 and not missing.stdout, 'hotspots ran without explicit worker')
        if baseline_worker:
            old = baseline_worker.resolve()
            refused = subprocess.run([str(candidate), 'analyze', 'structure', '--source', 'directory', '--hotspots', '--json',
                                      '--structural-worker', str(old), str(root)], capture_output=True, timeout=30)
            require(refused.returncode != 0 and not refused.stdout and b'updated worker' in refused.stderr,
                    'old worker did not clearly refuse hotspots')
            receipt['older_worker_refusal'] = {'worker_sha256': sha(old.read_bytes()), 'exit_code': refused.returncode,
                                             'stdout_bytes': 0, 'update_instruction': True}
        (root / 'recovery.py').write_bytes(RECOVERY)
        recovered = output([*base, '--hotspots'])
        recovery_report = json.loads(recovered)['structure']['hotspots']
        check_distribution(recovery_report, {**FIXTURES, 'recovery.py': RECOVERY})
        require(recovery_report['status'] == 'partial' and recovery_report['recovered_files'] == 1 and
                any(g['language'] == 'Python' and g['syntax_cohort'] == 'recovered' for g in recovery_report['groups']) and
                next(g for g in recovery_report['groups'] if g['language'] == 'Python' and g['syntax_cohort'] == 'clean') ==
                next(g for g in hotspots['groups'] if g['language'] == 'Python'), 'recovered syntax contaminated clean distributions')
        with tempfile.TemporaryDirectory(prefix='dircue empty hotspot ') as empty_root:
            empty = output([candidate, 'analyze', 'structure', '--source', 'directory', '--json', '--hotspots',
                            '--structural-worker', worker, empty_root])
            empty_report = json.loads(empty)['structure']['hotspots']
            require(empty_report['total_spaces'] == 0 and empty_report['groups'] == [], 'empty directory acquired hotspot population')
        saved = Path(temporary) / 'saved.json'
        saved.write_bytes(first)
        shutil.rmtree(root)
        comparison = output([candidate, 'compare', saved, saved, '--json'])
        compared = next(row for row in json.loads(comparison)['modules'] if row['name'] == 'hotspots')
        require(compared['status'] == 'unchanged' and compared['compatibility'] == 'observed_only' and
                compared['counts']['changed'] == 0, 'saved hotspot report did not self-compare with qualified population')
        receipt['stdout_sha256'] = {name: sha(data) for name, data in
            [('default_structure', default), ('hotspots', first), ('combined', combined),
             ('functions', functions), ('recovered', recovered), ('empty', empty), ('self_compare', comparison)]}
    receipt.update(passed=True, checks=sorted(CHECKS), observed_facts=copy.deepcopy(FACTS), worker_required=True, source_removed_before_compare=True)
    return validate_receipt(receipt, version, sha(candidate.read_bytes()), sha(worker.read_bytes()))


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--candidate', type=Path, required=True)
    parser.add_argument('--worker', type=Path, required=True)
    parser.add_argument('--version', required=True)
    parser.add_argument('--output', type=Path, required=True)
    parser.add_argument('--baseline-worker', type=Path)
    args = parser.parse_args()
    require(not args.output.exists(), 'choose a fresh hotspot smoke receipt path')
    receipt = run(args.candidate, args.worker, args.version, args.baseline_worker)
    args.output.parent.mkdir(parents=True, exist_ok=True)
    args.output.write_text(json.dumps(receipt, indent=2, allow_nan=False) + '\n')
    print('Packaged hotspot smoke passed')


if __name__ == '__main__':
    main()
