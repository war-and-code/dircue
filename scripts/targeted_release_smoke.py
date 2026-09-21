#!/usr/bin/env python3
"""Exercise 0.7 targeted analysis through a real packaged core binary."""
import argparse
import copy
import hashlib
import json
from pathlib import Path
import re
import shutil
import subprocess
import tempfile

import wheels

ROOT = Path(__file__).resolve().parents[1]
CHECKS = {
    'core_version', 'default_omission', 'focus_worker_determinism',
    'focus_primary_related_separation', 'focused_metrics_scope_binding',
    'availability_worker_determinism', 'availability_lfs_metadata',
    'availability_bounded_reads', 'fresh_explanation',
    'saved_explanation_without_source', 'saved_report_byte_identity',
}
POINTER = (b'version https://git-lfs.github.com/spec/v1\n'
           b'oid sha256:' + b'a' * 64 + b'\nsize 5000\n')
FIXTURES = {
    '.gitattributes': b'*.bin filter=lfs\napp/Program.cs linguist-language=Python linguist-detectable\n',
    'app/app.csproj': b'<Project><ItemGroup><ProjectReference Include="../lib/lib.csproj" /></ItemGroup></Project>\n',
    'app/Program.cs': b'class App { static void Main() {} }\n',
    'lib/lib.csproj': b'<Project />\n',
    'lib/Library.cs': b'class Lib {}\n',
    'assets/model.bin': POINTER,
    'assets/malformed.bin': b'version https://git-lfs.github.com/spec/v1\noid nope\nsize bad\n',
}
FACTS = {
    'primary_project': 'app/app.csproj',
    'related_project': 'lib/lib.csproj',
    'primary_population_files': 2,
    'related_population_files': 2,
    'primary_metric_files': 1,
    'related_metric_files': 1,
    'valid_lfs_pointers': 1,
    'pointer_like_files': 1,
    'explained_language': 'Python',
    'network_or_external_tools': False,
}


def require(condition, message):
    if not condition:
        raise ValueError(message)


def sha(data):
    return hashlib.sha256(data).hexdigest()


def valid_digest(value):
    return isinstance(value, str) and re.fullmatch('[0-9a-f]{64}', value) is not None


def required(version):
    wheels.python_version(version)
    return tuple(int(part) for part in version.split('-')[0].split('.')) >= (0, 7, 0)


def source_inputs():
    return {path.relative_to(ROOT).as_posix(): sha(path.read_bytes()) for path in
            (Path(__file__).resolve(), ROOT / 'scripts/wheels.py')}


def fixture_inputs():
    return {name: sha(data) for name, data in sorted(FIXTURES.items())}


def validate_receipt(receipt, version, candidate_sha256):
    require(isinstance(receipt, dict) and required(version), 'targeted proof requires release 0.7 or later')
    require(set(receipt) == {'schema_version', 'version', 'passed', 'candidate_sha256',
                             'source_sha256', 'fixture_sha256', 'checks', 'observed_facts',
                             'worker_required', 'external_tools_required',
                             'source_removed_before_saved_explain', 'stdout_sha256'},
            'targeted proof receipt fields differ')
    require(receipt.get('schema_version') == '1.0.0' and receipt.get('version') == version and
            receipt.get('passed') is True, 'targeted proof version/status mismatch')
    require(valid_digest(candidate_sha256) and receipt.get('candidate_sha256') == candidate_sha256,
            'targeted proof executable identity mismatch')
    require(receipt.get('source_sha256') == source_inputs() and
            receipt.get('fixture_sha256') == fixture_inputs(), 'targeted proof input identity mismatch')
    require(receipt.get('checks') == sorted(CHECKS) and receipt.get('observed_facts') == FACTS,
            'targeted proof coverage mismatch')
    digests = receipt.get('stdout_sha256')
    require(isinstance(digests, dict) and set(digests) ==
            {'default_all', 'focus', 'availability', 'fresh_explain', 'saved_explain'} and
            all(valid_digest(value) for value in digests.values()), 'targeted proof output identity missing')
    require(receipt.get('worker_required') is False and
            receipt.get('external_tools_required') is False and
            receipt.get('source_removed_before_saved_explain') is True,
            'targeted proof execution scope mismatch')
    return receipt


def output(command):
    completed = subprocess.run([str(part) for part in command], capture_output=True, timeout=120)
    require(completed.returncode == 0 and not completed.stderr,
            f'targeted smoke command failed: exit={completed.returncode}, stderr SHA256={sha(completed.stderr)}')
    return completed.stdout


def check_focus(report):
    focus, metrics = report['focus'], report['focused_metrics']
    require(focus['provider'] == 'dircue' and focus['provider_version'] == '1.0.0' and
            focus['status'] == 'complete', 'focus provider/status differs')
    require(focus['scope']['primary_project'] == FACTS['primary_project'] and
            focus['scope']['related_projects'] == [FACTS['related_project']], 'focus request differs')
    require([row['path'] for row in focus['primary']] == ['app/Program.cs', 'app/app.csproj'] and
            [row['path'] for row in focus['related'][0]['files']] == ['lib/Library.cs', 'lib/lib.csproj'],
            'focus populations differ')
    require(focus['coverage']['primary_files'] == FACTS['primary_population_files'] and
            focus['coverage']['related_files'] == FACTS['related_population_files'] and
            focus['coverage']['ambiguous_files'] == focus['coverage']['omitted_files'] == 0,
            'focus coverage differs')
    require(metrics['scope_id'] == focus['scope']['id'] and
            metrics['primary']['totals']['files'] == FACTS['primary_metric_files'] and
            len(metrics['related']) == 1 and metrics['related'][0]['project'] == FACTS['related_project'] and
            metrics['related'][0]['metrics']['totals']['files'] == FACTS['related_metric_files'],
            'focused metric populations or scope binding differ')
    require(metrics['primary']['languages'][0]['language'] == 'Python' and
            metrics['related'][0]['metrics']['languages'][0]['language'] == 'C#',
            'focused metric language differs')


def check_availability(report):
    value = report['availability']
    require(value['provider'] == 'dircue' and value['provider_version'] == '1.0.0' and
            value['status'] == 'complete' and value['source'] == {
                'mode': 'directory', 'consistency': 'live_directory',
                'checkout_metadata': 'confined_local_metadata'}, 'availability source/status differs')
    require(value['counts']['valid_pointers'] == FACTS['valid_lfs_pointers'] and
            value['counts']['pointer_like_files'] == FACTS['pointer_like_files'] and
            value['counts']['lfs_tracked_files'] == 2, 'availability LFS counts differ')
    pointer = next(row for row in value['lfs'] if row['kind'] == 'valid_pointer')
    require(pointer['path'] == 'assets/model.bin' and pointer['oid_algorithm'] == 'sha256' and
            pointer['oid_digest'] == 'a' * 64 and pointer['declared_object_bytes'] == 5000 and
            pointer['source_file_bytes'] == len(POINTER) and pointer['metadata_complete'] is True,
            'availability pointer metadata differs')
    coverage, bounds = value['coverage'], value['bounds']
    require(coverage['selected_inventory_complete'] is True and coverage['checkout_metadata_inspected'] is True and
            coverage['checkout_metadata_complete'] is True and coverage['pointer_inspections'] == len(FIXTURES) and
            coverage['content_bytes_read'] <= bounds['content_bytes'] and bounds['pointer_bytes'] == 1024,
            'availability coverage or read bounds differ')
    require(value['gitlinks'] == value['submodules'] == value['sparse'] == value['references'] == [] and
            value['omissions'] == {}, 'availability fabricated source boundaries')


def check_explanation(report, evidence):
    value = report['explanation']
    require(value['provider'] == 'dircue' and value['provider_version'] == '1.0.0' and
            value['status'] == 'complete' and value['query'] == {'kind': 'language-path', 'path': 'app/Program.cs'},
            'explanation identity differs')
    require(value['scope']['evidence'] == evidence and value['decision']['status'] == 'included' and
            value['decision']['reported_language'] == FACTS['explained_language'],
            'explanation decision differs')
    if evidence == 'fresh':
        require(value['source'] == {'mode': 'directory', 'consistency': 'live_directory'} and
                value['extent']['content_complete'] is True and value['overrides'],
                'fresh explanation evidence differs')
    else:
        require(value['source']['mode'] == 'directory' and valid_digest(value['source']['report_sha256']),
                'saved explanation source identity missing')


def run(candidate, version):
    require(required(version), 'targeted smoke requires release 0.7 or later')
    candidate = candidate.resolve()
    candidate_hash = sha(candidate.read_bytes())
    require(output([candidate, '--version']) == f'dircue {version}\n'.encode(), 'targeted core version differs')
    with tempfile.TemporaryDirectory(prefix='dircue targeted smoke ') as temporary:
        root = Path(temporary) / 'source'
        root.mkdir()
        for name, data in FIXTURES.items():
            path = root / name
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_bytes(data)

        default_all = output([candidate, 'analyze', 'all', '--source', 'directory', '--json', root])
        baseline = json.loads(default_all)
        require(all(key not in baseline for key in ('focus', 'focused_metrics', 'availability', 'explanation')),
                'default emitted opt-in targeted modules')

        focus_args = [candidate, 'analyze', 'focus', '--project', FACTS['primary_project'],
                      '--related-project', FACTS['related_project'], '--metrics', '--source', 'directory', '--json', root]
        focus = output([*focus_args, '--workers', '1'])
        require(focus == output([*focus_args, '--workers', '8']), 'focus evidence depends on worker scheduling')
        check_focus(json.loads(focus))

        availability_args = [candidate, 'analyze', 'availability', '--source', 'directory', '--json', root]
        availability = output([*availability_args, '--workers', '1'])
        require(availability == output([*availability_args, '--workers', '8']),
                'availability evidence depends on worker scheduling')
        check_availability(json.loads(availability))

        explain_args = [candidate, 'analyze', 'explain', '--file', 'app/Program.cs',
                        '--source', 'directory', '--json', root]
        fresh = output(explain_args)
        check_explanation(json.loads(fresh), 'fresh')
        saved = Path(temporary) / 'fresh-explanation.json'
        saved.write_bytes(fresh)
        saved_digest = sha(fresh)
        shutil.rmtree(root)
        retained = output([candidate, 'analyze', 'explain', '--file', 'app/Program.cs',
                           '--report', saved, '--json'])
        check_explanation(json.loads(retained), 'retained')
        require(json.loads(retained)['explanation']['source']['report_sha256'] == saved_digest,
                'saved explanation does not bind exact report bytes')

        outputs = {'default_all': default_all, 'focus': focus, 'availability': availability,
                   'fresh_explain': fresh, 'saved_explain': retained}
    receipt = {
        'schema_version': '1.0.0', 'version': version, 'passed': True,
        'candidate_sha256': candidate_hash, 'source_sha256': source_inputs(),
        'fixture_sha256': fixture_inputs(), 'checks': sorted(CHECKS),
        'observed_facts': copy.deepcopy(FACTS), 'worker_required': False,
        'external_tools_required': False, 'source_removed_before_saved_explain': True,
        'stdout_sha256': {name: sha(data) for name, data in outputs.items()},
    }
    return validate_receipt(receipt, version, candidate_hash)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--candidate', type=Path, required=True)
    parser.add_argument('--version', required=True)
    parser.add_argument('--output', type=Path, required=True)
    args = parser.parse_args()
    require(not args.output.exists(), 'choose a fresh targeted smoke receipt path')
    receipt = run(args.candidate, args.version)
    args.output.parent.mkdir(parents=True, exist_ok=True)
    args.output.write_text(json.dumps(receipt, indent=2, allow_nan=False) + '\n')
    print('Packaged targeted smoke passed')


if __name__ == '__main__':
    main()
