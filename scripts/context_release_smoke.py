#!/usr/bin/env python3
"""Exercise 0.8 environment, planning, capability, and comparison contracts."""
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
    'core_version', 'default_omission', 'environment_worker_determinism',
    'environment_declared_facts', 'capability_contract', 'saved_plan_without_source',
    'plan_exact_report_identity', 'plan_inert_argv', 'plan_source_binding',
    'focus_comparison_schema_1_6', 'availability_comparison_schema_1_6',
    'malformed_report_rejected', 'planning_request_cap_enforced',
}
FIXTURES = {
    'global.json': b'{"sdk":{"version":"8.0.300","rollForward":"latestPatch","allowPrerelease":false}}\n',
    'app/App.csproj': b'<Project Sdk="Microsoft.NET.Sdk"><PropertyGroup><TargetFramework>net8.0</TargetFramework></PropertyGroup></Project>\n',
    'app/Program.cs': b'class App { static void Main() {} }\n',
    'py/pyproject.toml': b'[project]\nname="demo"\nversion="1.0"\nrequires-python=">=3.12"\n',
    'py/main.py': b'print("fixture")\n',
    '.gitattributes': b'assets/*.bin filter=lfs\n',
    'assets/model.bin': b'version https://git-lfs.github.com/spec/v1\noid sha256:' + b'a' * 64 + b'\nsize 42\n',
}
FACTS = {
    'environment_provider_version': '1.0.0',
    'dotnet_project': 'app/App.csproj',
    'dotnet_target': 'net8.0',
    'python_project': 'py/pyproject.toml',
    'python_constraint': '>=3.12',
    'sdk_version': '8.0.300',
    'capability_schema_version': '1.0.0',
    'profile_schema_version': '1.7.0',
    'comparison_profile_schema_version': '1.6.0',
    'planned_commands_executable': False,
    'source_removed_before_plan': True,
    'external_tools_required': False,
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
    return tuple(int(part) for part in version.split('-')[0].split('.')) >= (0, 8, 0)


def source_inputs():
    return {path.relative_to(ROOT).as_posix(): sha(path.read_bytes()) for path in
            (Path(__file__).resolve(), ROOT / 'scripts/wheels.py')}


def fixture_inputs():
    return {name: sha(data) for name, data in sorted(FIXTURES.items())}


def validate_receipt(receipt, version, candidate_sha256, expected_platform):
    require(isinstance(receipt, dict) and required(version), 'context proof requires release 0.8 or later')
    require(set(receipt) == {'schema_version', 'version', 'platform', 'passed', 'candidate_sha256',
                             'source_sha256', 'fixture_sha256', 'checks', 'observed_facts',
                             'worker_required', 'external_tools_required',
                             'source_removed_before_plan', 'stdout_sha256'},
            'context proof receipt fields differ')
    require(receipt.get('schema_version') == '1.0.0' and receipt.get('version') == version and
            receipt.get('passed') is True, 'context proof version/status mismatch')
    require(receipt.get('platform') == expected_platform, 'context proof platform mismatch')
    require(valid_digest(candidate_sha256) and receipt.get('candidate_sha256') == candidate_sha256,
            'context proof executable identity mismatch')
    require(receipt.get('source_sha256') == source_inputs() and receipt.get('fixture_sha256') == fixture_inputs(),
            'context proof input identity mismatch')
    require(receipt.get('checks') == sorted(CHECKS) and receipt.get('observed_facts') == FACTS,
            'context proof coverage mismatch')
    outputs = receipt.get('stdout_sha256')
    require(isinstance(outputs, dict) and set(outputs) == {
        'capabilities', 'environment', 'saved_profile', 'plan', 'focus_comparison',
        'availability_comparison'} and all(valid_digest(value) for value in outputs.values()),
        'context proof output identity missing')
    require(receipt.get('worker_required') is False and receipt.get('external_tools_required') is False and
            receipt.get('source_removed_before_plan') is True, 'context proof execution scope mismatch')
    return receipt


def output(command, *, succeeds=True):
    completed = subprocess.run([str(part) for part in command], capture_output=True, timeout=120)
    if succeeds:
        require(completed.returncode == 0 and not completed.stderr,
                f'context smoke command failed: exit={completed.returncode}, stderr SHA256={sha(completed.stderr)}')
        return completed.stdout
    require(completed.returncode != 0 and not completed.stdout and completed.stderr,
            'invalid context command did not fail closed with empty stdout and a diagnostic')
    return completed.stderr


def module(report, name):
    rows = [row for row in report['modules'] if row['name'] == name]
    require(len(rows) == 1, f'comparison module {name} missing or duplicated')
    return rows[0]


def check_environment(report):
    env = report['environments']
    require(report['schema_version'] == FACTS['profile_schema_version'] and env['provider'] == 'dircue' and
            env['provider_version'] == FACTS['environment_provider_version'] and env['status'] == 'complete',
            'environment identity/status differs')
    requirements = {(row['project_id'], row['kind'], row['value']) for row in env['requirements']}
    require((FACTS['dotnet_project'], 'target-framework', FACTS['dotnet_target']) in requirements and
            (FACTS['python_project'], 'python-requires-python', FACTS['python_constraint']) in requirements,
            'declared environment facts differ')
    selections = {row['project_id']: row for row in env['selections']}
    require(selections[FACTS['dotnet_project']]['sdk_version'] == FACTS['sdk_version'] and
            selections[FACTS['dotnet_project']]['global_json'] == 'global.json',
            'nearest SDK selection differs')


def check_plan(plan, saved_bytes, capabilities):
    require(plan['kind'] == 'dircue-follow-up-plan' and plan['identity']['report_sha256'] == sha(saved_bytes) and
            plan['identity']['profile_schema_version'] == FACTS['profile_schema_version'],
            'plan does not bind exact saved input')
    require(plan['identity']['source'] == {'mode': 'directory', 'status': 'consistent'},
            'plan source identity does not match the saved report source contract')
    require(plan['identity']['capability_schema_version'] == capabilities['schema_version'] and
            plan['identity']['capability_version'] == capabilities['provider_version'],
            'plan does not bind capability contract')
    require(plan['steps'] and all(step['command']['executable'] is False for step in plan['steps']),
            'plan emitted executable commands')
    for step in plan['steps']:
        command = step['command']
        require(command['source_placeholder'] == '{source}' and command['argv'][-1] == '{source}' and
                len(command['revalidation_required']) >= 3, 'plan command lacks explicit source revalidation')
        require(all('/source' not in arg and '\\source' not in arg for arg in command['argv']),
                'plan argv retained deleted fixture source')


def run(candidate, version, platform):
    require(required(version), 'context smoke requires release 0.8 or later')
    candidate = candidate.resolve()
    candidate_hash = sha(candidate.read_bytes())
    require(output([candidate, '--version']) == f'dircue {version}\n'.encode(), 'context core version differs')
    with tempfile.TemporaryDirectory(prefix='dircue context smoke ') as temporary:
        root = Path(temporary) / 'source'
        root.mkdir()
        for name, data in FIXTURES.items():
            path = root / name
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_bytes(data)
        capabilities_raw = output([candidate, 'capabilities', '--json'])
        capabilities = json.loads(capabilities_raw)
        require(capabilities['schema_version'] == FACTS['capability_schema_version'] and
                capabilities['provider_version'] == version and
                {'environments', 'focus', 'availability'} <= {row['id'] for row in capabilities['modules']},
                'capability descriptor differs')
        default = json.loads(output([candidate, 'analyze', 'all', '--json', '--source', 'directory', root]))
        require('environments' not in default, 'default analysis emitted opt-in environments')
        env_args = [candidate, 'analyze', 'environments', '--json', '--source', 'directory', root]
        environment_raw = output([*env_args, '--workers', '1'])
        require(environment_raw == output([*env_args, '--workers', '8']),
                'environment evidence depends on worker scheduling')
        check_environment(json.loads(environment_raw))
        saved_raw = output([candidate, 'analyze', 'all', '--declarations', '--environments', '--availability',
                            '--json', '--source', 'directory', root])
        saved = Path(temporary) / 'saved.json'
        saved.write_bytes(saved_raw)

        focus_args = [candidate, 'analyze', 'focus', '--project', FACTS['dotnet_project'], '--metrics',
                      '--json', '--source', 'directory', root]
        focus_a = Path(temporary) / 'focus-a.json'; focus_a.write_bytes(output(focus_args))
        (root / 'app/Extra.cs').write_bytes(b'class Extra {}\n')
        focus_b = Path(temporary) / 'focus-b.json'; focus_b.write_bytes(output(focus_args))
        focus_comparison = output([candidate, 'compare', focus_a, focus_b, '--json'])
        focus_diff = json.loads(focus_comparison)
        require(focus_diff['base']['schema_version'] == focus_diff['head']['schema_version'] ==
                FACTS['comparison_profile_schema_version'] and
                module(focus_diff, 'focus_primary')['compatibility'] in ('compatible', 'observed_only'),
                'focused comparison contract differs')

        availability_args = [candidate, 'analyze', 'availability', '--json', '--source', 'directory', root]
        availability_a = Path(temporary) / 'availability-a.json'; availability_a.write_bytes(output(availability_args))
        (root / 'assets/model.bin').write_bytes(FIXTURES['assets/model.bin'].replace(b'a' * 64, b'b' * 64))
        availability_b = Path(temporary) / 'availability-b.json'; availability_b.write_bytes(output(availability_args))
        availability_comparison = output([candidate, 'compare', availability_a, availability_b, '--json'])
        availability_diff = json.loads(availability_comparison)
        require(availability_diff['base']['schema_version'] == availability_diff['head']['schema_version'] ==
                FACTS['comparison_profile_schema_version'] and
                module(availability_diff, 'availability_lfs')['counts']['changed'] == 1,
                'availability comparison contract differs')

        shutil.rmtree(root)
        plan_raw = output([candidate, 'plan', saved, '--module', 'environments', '--module', 'focus',
                           '--project', FACTS['dotnet_project'], '--json'])
        check_plan(json.loads(plan_raw), saved_raw, capabilities)
        malformed = Path(temporary) / 'malformed.json'; malformed.write_bytes(b'{')
        output([candidate, 'plan', malformed, '--module', 'metrics', '--json'], succeeds=False)
        capped = [candidate, 'plan', saved, '--json']
        for _ in range(65):
            capped += ['--module', 'metrics']
        output(capped, succeeds=False)
        outputs = {'capabilities': capabilities_raw, 'environment': environment_raw, 'saved_profile': saved_raw,
                   'plan': plan_raw, 'focus_comparison': focus_comparison,
                   'availability_comparison': availability_comparison}
    receipt = {'schema_version': '1.0.0', 'version': version, 'platform': platform, 'passed': True,
               'candidate_sha256': candidate_hash, 'source_sha256': source_inputs(),
               'fixture_sha256': fixture_inputs(), 'checks': sorted(CHECKS),
               'observed_facts': copy.deepcopy(FACTS), 'worker_required': False,
               'external_tools_required': False, 'source_removed_before_plan': True,
               'stdout_sha256': {name: sha(data) for name, data in outputs.items()}}
    return validate_receipt(receipt, version, candidate_hash, platform)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--candidate', type=Path, required=True)
    parser.add_argument('--version', required=True)
    parser.add_argument('--platform', required=True)
    parser.add_argument('--output', type=Path, required=True)
    args = parser.parse_args()
    require(not args.output.exists(), 'choose a fresh context smoke receipt path')
    receipt = run(args.candidate, args.version, args.platform)
    args.output.parent.mkdir(parents=True, exist_ok=True)
    args.output.write_text(json.dumps(receipt, indent=2, allow_nan=False) + '\n')
    print('Packaged context smoke passed')


if __name__ == '__main__':
    main()
