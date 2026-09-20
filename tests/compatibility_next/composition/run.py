#!/usr/bin/env python3
"""Check optional profiling-module composition using supplied executables."""
import argparse
import copy
import hashlib
import json
from pathlib import Path
import subprocess


def digest(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--candidate', type=Path, required=True)
    parser.add_argument('--worker', type=Path, required=True)
    parser.add_argument('--output', type=Path, required=True)
    args = parser.parse_args()
    source = Path(__file__).resolve().parent
    root = source.parents[2]
    output = args.output.resolve()
    output.mkdir(parents=True, exist_ok=False)
    candidate, worker = args.candidate.resolve(), args.worker.resolve()
    fixture, policy = source / 'fixture', source / 'policy.json'
    syft_report = root / 'tests/packageevidence/fixtures/syft-1.52.0.json'
    identity = {
        'candidate_sha256': digest(candidate),
        'worker_sha256': digest(worker),
        'fixture_sha256': {str(path.relative_to(fixture)): digest(path)
                           for path in sorted(fixture.rglob('*')) if path.is_file()},
        'policy_sha256': digest(policy),
        'syft_report_sha256': digest(syft_report),
    }
    common = ['analyze', 'all', '--source', 'directory', '--json', '--projects',
              '--metrics', '--structure', '--files', '--structural-worker', str(worker)]
    extras = ['--functions', '--graph', '--discovery', '--registries', '--rules-file',
              str(policy), '--syft-report', str(syft_report), '--syft-root', '/']
    commands = []

    def run(name, arguments):
        command = [str(candidate), *arguments, str(fixture)]
        commands.append(command)
        result = subprocess.run(command, capture_output=True, timeout=45)
        (output / (name + '.json')).write_bytes(result.stdout)
        (output / (name + '.stderr')).write_bytes(result.stderr)
        assert result.returncode == 0, (name, result.returncode, result.stderr)
        assert b'PRIVATE_SENTINEL' not in result.stdout + result.stderr, name
        return json.loads(result.stdout)

    before = run('composition-baseline', common + ['--workers', '1'])
    after = run('composition-enriched', common + extras + ['--workers', '1'])
    parallel = run('composition-enriched-eight', common + extras + ['--workers', '8'])
    assert after == parallel, 'worker determinism'
    assert before['schema_version'] == '1.2.0' and after['schema_version'] == '1.3.0'
    fields = ['registries', 'rules', 'package_evidence', 'discovery', 'graph',
              'projects', 'structure', 'metrics']
    assert all(after.get(name) is not None for name in fields)
    assert after['registries']['coverage']['retained_declarations'] == 2
    assert after['rules']['total_matches'] == 3
    assert len(after['graph']['nodes']) == 2
    assert after['structure']['functions']['total_spaces'] >= 3
    stripped = copy.deepcopy(after)
    for name in ['registries', 'rules', 'package_evidence', 'discovery', 'graph']:
        del stripped[name]
    del stripped['structure']['functions']
    stripped['schema_version'] = before['schema_version']
    assert before == stripped, 'new modules changed baseline fields'

    limited = run('composition-limited', common + extras + ['--max-file-bytes', '1'])
    assert limited['registries']['status'] == 'partial'
    assert limited['registries']['coverage']['read_files'] == 0
    skipped = run('composition-tree-skipped', common + extras + ['--tree-size', '1'])
    assert skipped['registries']['status'] == skipped['rules']['status'] == 'skipped'
    assert identity['candidate_sha256'] == digest(candidate)
    assert identity['worker_sha256'] == digest(worker)
    assert identity['policy_sha256'] == digest(policy)
    assert identity['syft_report_sha256'] == digest(syft_report)
    assert identity['fixture_sha256'] == {
        str(path.relative_to(fixture)): digest(path)
        for path in sorted(fixture.rglob('*')) if path.is_file()}

    receipt = {
        **identity,
        'passed': True,
        'checks': [
            'All eight requested profile fields coexist',
            'Schema versions 1.2 and 1.3 selected correctly',
            'One and eight workers produce identical reports',
            'All baseline fields equal after removing requested additions',
            'Two sanitized registry declarations',
            'Three source-return rule matches',
            'Two .NET graph nodes',
            'Native function evidence present',
            'File limits prevent registry reads',
            'Tree refusal marks new modules skipped',
            'No credential marker on stdout or stderr',
        ],
        'commands': commands,
        'fixture_path': str(fixture),
        'candidate_path': str(candidate),
        'worker_path': str(worker),
        'harness_sha256': digest(Path(__file__)),
    }
    (output / 'composition-receipt.json').write_text(json.dumps(receipt, indent=2) + '\n')
    print('All five composition reports passed; run verify_schema.py to validate their schema.')


if __name__ == '__main__':
    main()
