#!/usr/bin/env python3
"""Check packaged declaration readers and offline comparison without external tools."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import tempfile

import wheels

ROOT = Path(__file__).resolve().parents[1]
DEFAULT_CHECKS = {'core_version', 'default_omits_declarations', 'default_repeat_exact'}
DECLARATION_CHECKS = {'six_ecosystem_facts', 'workspace_relationships', 'named_interfaces_without_script_bodies',
                      'one_eight_workers', 'standalone_combined_parity', 'default_fields_unchanged',
                      'offline_compare_after_source_removal', 'identical_report_compare',
                      'malformed_report_rejection', 'partial_coverage_qualified'}
ASSESSMENT_CHECKS = {'assessment_inventory_and_projects', 'assessment_lock_partition',
                     'assessment_offline_standalone_all_parity', 'assessment_no_external_tools',
                     'assessment_preserves_legacy_defaults'}
STRUCTURE_CHECKS = {'assessment_workspace_structure', 'assessment_dependency_graph',
                    'assessment_manifest_entrypoint_catalog'}
SCRIPT_SENTINEL = 'DIRCUE_RELEASE_SMOKE_SCRIPT_BODY_DO_NOT_EXECUTE'
FIXTURES = {
    'npm/package.json': '{"name":"suite","version":"1.0.0","workspaces":["packages/*"],"scripts":{"start":"node server.js","check":"echo ' + SCRIPT_SENTINEL + ' > EXECUTED"}}\n',
    'npm/packages/lib/package.json': '{"name":"library","version":"1.0.0"}\n',
    'go/go.work': 'go 1.24.0\nuse ./app\n',
    'go/app/go.mod': 'module example.org/release-smoke\ngo 1.24.0\n',
    'go/app/main.go': 'package main\nfunc main() {}\n',
    'python/pyproject.toml': '[project]\nname="suite"\nversion="1.0.0"\nrequires-python=">=3.11"\ndependencies=["common"]\n[project.scripts]\nsuite="suite.cli:main"\n[tool.uv.workspace]\nmembers=["packages/*"]\n[tool.uv.sources]\ncommon={workspace=true}\n',
    'python/packages/common/pyproject.toml': '[project]\nname="common"\nversion="1.0.0"\nrequires-python=">=3.11"\n',
    'cargo/Cargo.toml': '[workspace]\nmembers=["crates/app"]\nresolver="2"\n[workspace.package]\nversion="1.2.3"\nedition="2021"\n',
    'cargo/crates/app/Cargo.toml': '[package]\nname="smoke-app"\nversion.workspace=true\nedition.workspace=true\n[[bin]]\nname="smoke-app"\npath="src/main.rs"\n',
    'cargo/crates/app/src/main.rs': 'fn main() {}\n',
    'dotnet/App/App.csproj': '<Project Sdk="Microsoft.NET.Sdk"><PropertyGroup><TargetFramework>net8.0</TargetFramework></PropertyGroup><ItemGroup><ProjectReference Include="../Library/Library.csproj"/></ItemGroup></Project>\n',
    'dotnet/Library/Library.csproj': '<Project Sdk="Microsoft.NET.Sdk"><PropertyGroup><TargetFramework>net8.0</TargetFramework></PropertyGroup></Project>\n',
    'java/pom.xml': '<project><modelVersion>4.0.0</modelVersion><groupId>example</groupId><artifactId>suite</artifactId><version>1.0.0</version><packaging>pom</packaging><properties><maven.compiler.release>21</maven.compiler.release></properties><modules><module>app</module></modules></project>\n',
    'java/app/pom.xml': '<project><modelVersion>4.0.0</modelVersion><groupId>example</groupId><artifactId>app</artifactId><version>1.0.0</version></project>\n',
}
EXPECTED_MANIFESTS = sorted(name for name in FIXTURES if not name.endswith(('.go', '.rs')))
FACTS = {'ecosystems': ['cargo', 'dotnet', 'go', 'maven', 'npm', 'python-uv'],
         'manifest_ids': EXPECTED_MANIFESTS, 'go_minimum': '1.24.0', 'python_minimum': '>=3.11',
         'cargo_version': '1.2.3', 'cargo_edition': '2021', 'dotnet_framework': 'net8.0', 'java_release': '21'}
ASSESSMENT_FACTS = {
    'regular_files': len(FIXTURES),
    'logical_bytes': sum(len(content.encode('utf-8')) for content in FIXTURES.values()),
    'manifest_candidates': len(EXPECTED_MANIFESTS),
    # go.work and the Cargo [workspace]-only manifest are virtual roots, not projects.
    'parsed_projects': 10,
    'distinct_project_roots': 10,
    # Only the npm workspace root has something to lock (its members) and no
    # lockfile; the npm member and both .NET projects declare no packages.
    'eligible_lockfile_projects': 1,
    'lockfile_state_partition': ['covered', 'missing', 'not_applicable', 'unsupported', 'unknown'],
}
STRUCTURE_FACTS = {
    'workspace_groups': 5,
    'definite_edges': 2,
    'observed_connected_groups': 8,
    'parsed_projects': 10,
    'manifest_entrypoints': [
        {'ecosystem': 'npm', 'kind': 'manifest_interface:script', 'name': 'start'},
        {'ecosystem': 'cargo', 'kind': 'manifest_interface:cargo-bin', 'name': 'smoke-app', 'target': 'cargo/crates/app/src/main.rs'},
        {'ecosystem': 'python', 'kind': 'manifest_interface:python-console-script', 'name': 'suite', 'target': 'suite.cli:main'},
    ],
}


def require(condition, message):
    if not condition:
        raise ValueError(message)


def sha(data):
    return hashlib.sha256(data).hexdigest()


def file_sha(path):
    return sha(path.read_bytes())


def release_tuple(version):
    wheels.python_version(version)
    return tuple(int(value) for value in version.split('-')[0].split('.'))


def declarations_required(version):
    return release_tuple(version) >= (0, 5, 0)


def assessment_required(version):
    return release_tuple(version) >= (1, 4, 0)


def structure_required(version):
    return release_tuple(version) >= (1, 5, 0)


def source_inputs():
    return {p.relative_to(ROOT).as_posix(): file_sha(p) for p in (Path(__file__).resolve(), ROOT / 'scripts/wheels.py')}


def fixture_inputs():
    return {name: sha(content.encode()) for name, content in sorted(FIXTURES.items())}


def valid_digest(value):
    return isinstance(value, str) and re.fullmatch('[0-9a-f]{64}', value) is not None


def validate_receipt(receipt, version, candidate_sha256):
    require(isinstance(receipt, dict), 'declaration smoke receipt must be an object')
    required = declarations_required(version)
    assess = assessment_required(version)
    structure = structure_required(version)
    require(receipt.get('schema_version') == '1.0.0' and receipt.get('passed') is True and
            receipt.get('version') == version and receipt.get('declarations_required') is required,
            'declaration smoke version/status mismatch')
    require((receipt.get('assessment_required') is True) if assess else ('assessment_required' not in receipt),
            'declaration smoke assessment gate mismatch')
    require((receipt.get('structure_required') is True) if structure else ('structure_required' not in receipt),
            'declaration smoke structure gate mismatch')
    require(valid_digest(candidate_sha256) and receipt.get('candidate_sha256') == candidate_sha256,
            'declaration smoke executable identity mismatch')
    require(receipt.get('source_sha256') == source_inputs() and receipt.get('fixture_sha256') == fixture_inputs(),
            'declaration smoke input identity mismatch')
    expected = DEFAULT_CHECKS | (DECLARATION_CHECKS if required else set()) | (ASSESSMENT_CHECKS if assess else set()) | (STRUCTURE_CHECKS if structure else set())
    checks = receipt.get('checks')
    require(isinstance(checks, list) and all(isinstance(c, str) for c in checks) and len(checks) == len(expected) and set(checks) == expected,
            'declaration smoke check inventory mismatch')
    digests = receipt.get('stdout_sha256')
    keys = {'default_languages', 'default_all'} | ({'declarations', 'combined', 'changed_compare', 'identical_compare', 'partial_compare'} if required else set()) | ({'assessment', 'assessment_all'} if assess else set())
    require(isinstance(digests, dict) and set(digests) == keys and all(valid_digest(d) for d in digests.values()),
            'declaration smoke output identity missing')
    facts = {} if not required else FACTS
    if assess:
        facts = {'declarations': FACTS, 'assessment': ASSESSMENT_FACTS}
    if structure:
        facts = {'declarations': FACTS, 'assessment': ASSESSMENT_FACTS, 'structure': STRUCTURE_FACTS}
    require(receipt.get('observed_facts') == facts, 'declaration smoke fact coverage mismatch')
    require(receipt.get('source_removed_before_compare') is required and receipt.get('worker_required') is False,
            'declaration smoke execution scope mismatch')
    negatives = receipt.get('negative_cases')
    require(negatives == (['duplicate-json-key', 'malformed-json'] if required else []),
            'declaration smoke negative coverage mismatch')
    return receipt


def execute(candidate, args, success=True, env=None, cwd=None):
    result = subprocess.run([str(candidate), *args], capture_output=True, timeout=120, env=env, cwd=cwd)
    if success:
        require(result.returncode == 0 and not result.stderr, f'declaration release smoke failed: exit {result.returncode}, stderr SHA256={sha(result.stderr)}')
    else:
        require(result.returncode != 0 and not result.stdout and result.stderr, 'invalid report did not fail closed')
    return result


def output(candidate, args, env=None, cwd=None):
    return execute(candidate, args, env=env, cwd=cwd).stdout


def contains(rows, **facts):
    return any(all(row.get(key) == value for key, value in facts.items()) for row in rows)


def check_facts(report):
    require(report.get('schema_version') == '1.4.0', 'declaration aggregate schema mismatch')
    declarations = report['declarations']
    require(declarations['status'] == 'complete' and declarations['diagnostics'] == [] and
            declarations['coverage']['omitted_files'] == 0 and declarations['coverage']['parsed_manifests'] == len(EXPECTED_MANIFESTS),
            'synthetic declaration coverage incomplete')
    projects = {p['id']: p for p in declarations['projects']}
    require(sorted(projects) == EXPECTED_MANIFESTS and len(projects) == len(declarations['projects']), 'manifest identities differ')
    def req(manifest, kind, value):
        require(contains(projects[manifest]['requirements'], kind=kind, value=value, state='declared', evidence=manifest),
                'missing expected requirement: ' + manifest + '/' + kind)
    req('go/app/go.mod', 'go-language-minimum', '1.24.0')
    req('python/pyproject.toml', 'python-requires-python', '>=3.11')
    req('dotnet/App/App.csproj', 'target-framework', 'net8.0')
    req('java/pom.xml', 'java-release', '21')
    require(projects['go/app/go.mod']['name'] == 'example.org/release-smoke' and
            projects['npm/package.json']['name'] == 'suite' and projects['python/pyproject.toml']['name'] == 'suite', 'project names differ')
    cargo = projects['cargo/crates/app/Cargo.toml']
    require(cargo['name'] == 'smoke-app' and cargo['version'] == '1.2.3' and
            contains(cargo['requirements'], kind='cargo-edition', value='2021', evidence='cargo/Cargo.toml'), 'Cargo inherited identity differs')
    edges = [('npm/package.json', 'npm-workspace-member', 'npm/packages/lib/package.json'),
             ('go/go.work', 'go-workspace-member', 'go/app/go.mod'),
             ('python/pyproject.toml', 'uv-workspace-member', 'python/packages/common/pyproject.toml'),
             ('python/pyproject.toml', 'uv-local-dependency', 'python/packages/common/pyproject.toml'),
             ('cargo/Cargo.toml', 'cargo-workspace-member', 'cargo/crates/app/Cargo.toml'),
             ('dotnet/App/App.csproj', 'project-reference', 'dotnet/Library/Library.csproj'),
             ('java/pom.xml', 'module', 'java/app/pom.xml')]
    for manifest, kind, target in edges:
        state = 'declared' if kind == 'cargo-workspace-member' else 'resolved'
        require(contains(projects[manifest]['references'], kind=kind, target=target, state=state, target_status='present'), 'missing selected relationship: ' + manifest + '/' + kind)
    # Only conventional entry-point scripts (start, serve) are interfaces; a
    # developer task such as `check` is not.
    require(not contains(projects['npm/package.json']['interfaces'], name='check'), 'developer script reported as an interface')
    require(contains(projects['npm/package.json']['interfaces'], kind='script', name='start', state='declared') and
            contains(projects['python/pyproject.toml']['interfaces'], name='suite', target='suite.cli:main', state='declared') and
            contains(cargo['interfaces'], name='smoke-app', target='cargo/crates/app/src/main.rs'), 'named interfaces differ')
    require(SCRIPT_SENTINEL not in json.dumps(report), 'raw script body disclosed')


def check_assessment_facts(report, version='1.4.0'):
    expected_schema = '1.10.0' if release_tuple(version) >= (1, 5, 0) else '1.9.0'
    require(report.get('schema_version') == expected_schema, 'assessment schema mismatch')
    assessment = report.get('assessment')
    require(isinstance(assessment, dict), 'native assessment output is missing')
    inventory = assessment['inventory']
    require(inventory['files']['count'] == ASSESSMENT_FACTS['regular_files'] and
            inventory['files']['completeness'] == 'complete' and
            inventory['bytes']['count'] == ASSESSMENT_FACTS['logical_bytes'] and
            inventory['bytes']['completeness'] == 'complete', 'selected-file inventory or logical bytes differ')
    require(assessment['manifest_candidate_population']['count'] == ASSESSMENT_FACTS['manifest_candidates'] and
            assessment['manifest_candidate_population']['completeness'] == 'complete', 'selected manifest population differs')
    require(assessment['projects']['count'] == ASSESSMENT_FACTS['parsed_projects'] and
            assessment['projects']['completeness'] == 'complete' and
            assessment['project_roots']['count'] == ASSESSMENT_FACTS['distinct_project_roots'] and
            assessment['project_roots']['completeness'] == 'complete',
            'parsed projects or distinct roots differ (Go and Cargo virtual workspace roots are excluded)')
    lockfiles = assessment['lockfiles_overall']
    require(lockfiles['eligible']['count'] == ASSESSMENT_FACTS['eligible_lockfile_projects'], 'eligible npm/NuGet project count differs')
    states = ASSESSMENT_FACTS['lockfile_state_partition']
    require(sum(lockfiles[state]['count'] for state in states) == assessment['projects']['count'],
            'lock outcome states do not partition the project population')
    require(lockfiles['covered']['count'] == 0 and lockfiles['unknown']['count'] == 0,
            'fixture unexpectedly observed a lock or unresolved lock association')
    return assessment


def check_assessment_structure_facts(assessment):
    structure = assessment.get('structure')
    require(isinstance(structure, dict), '1.5 structural assessment is missing')
    require(assessment.get('version') == '1.1.0', 'structural assessment version mismatch')
    groups = structure.get('workspace_groups', [])
    require(structure.get('workspace_group_count') == STRUCTURE_FACTS['workspace_groups'] and
            len(groups) + structure.get('omitted_workspace_groups', -1) == STRUCTURE_FACTS['workspace_groups'],
            'workspace group exact total or bounded sample differs')
    expected_groups = {'cargo', 'go', 'maven', 'npm', 'python-uv'}
    require({group.get('ecosystem') for group in groups} == expected_groups and
            all(group.get('member_count') == 1 and group.get('membership_coverage', {}).get('status') == 'complete'
                for group in groups), 'workspace membership facts differ')
    dependencies = structure.get('dependencies', {})
    require(dependencies.get('projects', {}).get('count') == STRUCTURE_FACTS['parsed_projects'] and
            dependencies.get('definite_edges', {}).get('count') == STRUCTURE_FACTS['definite_edges'] and
            dependencies.get('connected_groups', {}).get('count') == STRUCTURE_FACTS['observed_connected_groups'],
            'structural graph project/edge/connected-group facts differ')
    entries = structure.get('entry_points', [])
    observed = []
    for expected in STRUCTURE_FACTS['manifest_entrypoints']:
        matches = [entry for entry in entries if entry.get('ecosystem') == expected['ecosystem'] and
                   entry.get('name') == expected['name'] and entry.get('state') == 'declared']
        require(len(matches) == 1, 'manifest entry-point catalog missing or duplicated: ' + repr(expected))
        entry = matches[0]
        if 'target' in expected:
            require(entry.get('target') == expected['target'], 'manifest entry-point target differs: ' + repr(expected))
        observed.append({key: entry[key] for key in expected})
    require(structure.get('entry_point_count') == len(STRUCTURE_FACTS['manifest_entrypoints']) and
            len(entries) == structure.get('entry_point_count'), 'entry-point catalog count differs')
    serialized = json.dumps(structure, sort_keys=True)
    require(SCRIPT_SENTINEL not in serialized and 'node server.js' not in serialized,
            'raw npm script body disclosed in structural catalog')
    require(observed == STRUCTURE_FACTS['manifest_entrypoints'], 'manifest entry-point facts differ')
    return structure


def check_assessment_parity(standalone_raw, combined_raw):
    standalone = json.loads(standalone_raw)
    combined = json.loads(combined_raw)
    require(standalone.get('schema_version') == combined.get('schema_version') and
            standalone.get('languages') == combined.get('languages') and
            standalone.get('assessment') == combined.get('assessment'),
            'standalone and all --assessment observations differ')
    # Compare the exact assessment payload after canonical JSON encoding. The
    # enclosing all report also carries opt-in modules, so its whole-document
    # bytes are not the relevant parity contract.
    canonical = lambda value: json.dumps(value, sort_keys=True, separators=(',', ':'), ensure_ascii=False)
    require(canonical(standalone['assessment']) == canonical(combined['assessment']),
            'standalone and all --assessment JSON differ')
    return standalone


def module(report, name):
    rows = [row for row in report['modules'] if row['name'] == name]
    require(len(rows) == 1, 'comparison module missing or duplicated')
    return rows[0]


def run(candidate, version):
    required = declarations_required(version)
    assess = assessment_required(version)
    structure_required_for_version = structure_required(version)
    candidate = candidate.resolve()
    binary_sha = file_sha(candidate)
    require(output(candidate, ['--version']) == f'dircue {version}\n'.encode(), 'packaged core version mismatch')
    receipt = {'schema_version': '1.0.0', 'version': version, 'declarations_required': required,
               'candidate_sha256': binary_sha, 'source_sha256': source_inputs(), 'fixture_sha256': fixture_inputs(),
               'stdout_sha256': {}, 'worker_required': False, 'source_removed_before_compare': False,
               'observed_facts': {}, 'negative_cases': []}
    if assess:
        receipt['assessment_required'] = True
    if structure_required_for_version:
        receipt['structure_required'] = True
    with tempfile.TemporaryDirectory(prefix='dircue-packaged-declarations-') as temp:
        area = Path(temp)
        root = area / 'source'
        root.mkdir()
        for name, content in FIXTURES.items():
            file = root / name
            file.parent.mkdir(parents=True, exist_ok=True)
            file.write_bytes(content.encode())
        common = ['--source', 'directory', '--json', str(root)]
        language = output(candidate, common)
        ordinary = output(candidate, ['analyze', 'all', *common])
        require('declarations' not in json.loads(ordinary), 'default aggregate emitted declarations')
        receipt['stdout_sha256'].update(default_languages=sha(language), default_all=sha(ordinary))
        if required:
            first = output(candidate, ['analyze', 'declarations', *common, '--workers', '1'])
            require(first == output(candidate, ['analyze', 'declarations', *common, '--workers', '8']), 'declarations depend on worker scheduling')
            base = json.loads(first)
            check_facts(base)
            combined_raw = output(candidate, ['analyze', 'all', '--declarations', *common])
            combined = json.loads(combined_raw)
            require(combined.pop('declarations') == base['declarations'], 'standalone and combined declarations differ')
            default = json.loads(ordinary)
            combined['schema_version'] = default['schema_version']
            require(combined == default, 'declarations changed existing aggregate observations')
            require(not list(root.rglob('EXECUTED')), 'repository script executed')
            receipt['stdout_sha256'].update(declarations=sha(first), combined=sha(combined_raw))
        if assess:
            tool_bin = area / 'empty-bin'
            tool_bin.mkdir()
            offline_env = os.environ.copy()
            offline_env['PATH'] = str(tool_bin)
            standalone_args = ['analyze', 'assessment', *common]
            combined_args = ['analyze', 'all', '--assessment', *common]
            standalone_raw = output(candidate, standalone_args, env=offline_env, cwd=area)
            combined_assessment_raw = output(candidate, combined_args, env=offline_env, cwd=area)
            assessment_report = check_assessment_parity(standalone_raw, combined_assessment_raw)
            check_assessment_facts(assessment_report, version)
            if structure_required_for_version:
                check_assessment_structure_facts(assessment_report['assessment'])
            require('package_evidence' not in assessment_report or assessment_report['package_evidence'] is None,
                    'native assessment unexpectedly imported package evidence')
            require(not list(root.rglob('EXECUTED')), 'repository script executed during assessment')
            receipt['stdout_sha256'].update(assessment=sha(standalone_raw), assessment_all=sha(combined_assessment_raw))
        require(language == output(candidate, common) and ordinary == output(candidate, ['analyze', 'all', *common]), 'default output changed after opt-in checks')
        if required:
            (area / 'base.json').write_bytes(first)
            (root / 'go/app/go.mod').write_bytes(FIXTURES['go/app/go.mod'].replace('1.24.0', '1.25.0').encode())
            changed = output(candidate, ['analyze', 'declarations', *common])
            (area / 'head.json').write_bytes(changed)
            (root / 'go/app/go.mod').unlink()
            (root / 'broken').mkdir()
            (root / 'broken/pyproject.toml').write_bytes(b'[project\nRAW_MALFORMED_MANIFEST_MARKER')
            partial = output(candidate, ['analyze', 'declarations', *common])
            require(b'RAW_MALFORMED_MANIFEST_MARKER' not in partial, 'malformed manifest payload disclosed')
            partial_report = json.loads(partial)['declarations']
            require(partial_report['status'] == 'partial' and partial_report['diagnostics'] and
                    partial_report['coverage']['parsed_manifests'] < partial_report['coverage']['manifest_candidates'], 'invalid manifest coverage was not qualified')
            (area / 'partial.json').write_bytes(partial)
            shutil.rmtree(root)
            require(not root.exists(), 'source was not removed before comparison')
            def compare(head):
                return output(candidate, ['compare', str(area / 'base.json'), str(area / head), '--json'])
            changed_compare = compare('head.json')
            changed_module = module(json.loads(changed_compare), 'declarations')
            require(changed_module['status'] == 'changed' and changed_module['compatibility'] == 'compatible' and
                    changed_module['counts']['changed'] == 1 and changed_module['counts']['added'] == changed_module['counts']['removed'] == 0 and
                    changed_module['changes'][0]['id'] == 'go/app/go.mod', 'offline comparison did not identify the Go requirement change')
            identical = compare('base.json')
            same = module(json.loads(identical), 'declarations')
            require(same['status'] == 'unchanged' and not same['changes'], 'identical reports changed')
            qualified = compare('partial.json')
            partial_module = module(json.loads(qualified), 'declarations')
            require(partial_module['compatibility'] == 'observed_only' and
                    contains(partial_module['changes'], id='go/app/go.mod', status='unavailable', reason='absence_from_head_is_not_proven'), 'incomplete coverage falsely claimed manifest removal')
            for name, content in [('malformed-json', b'{"RAW_PRIVATE_MARKER":'), ('duplicate-json-key', b'{"schema_version":"1.4.0","schema_version":"1.4.0"}')]:
                bad = area / (name + '.json')
                bad.write_bytes(content)
                result = execute(candidate, ['compare', str(bad), str(area / 'head.json'), '--json'], success=False)
                require(b'RAW_PRIVATE_MARKER' not in result.stderr, 'invalid report payload leaked into error')
            receipt['stdout_sha256'].update(changed_compare=sha(changed_compare), identical_compare=sha(identical), partial_compare=sha(qualified))
            observed_facts = {'declarations': FACTS, 'assessment': ASSESSMENT_FACTS} if assess else FACTS
            if structure_required_for_version:
                observed_facts = {'declarations': FACTS, 'assessment': ASSESSMENT_FACTS,
                                  'structure': STRUCTURE_FACTS}
            receipt.update(observed_facts=observed_facts, source_removed_before_compare=True,
                           negative_cases=['duplicate-json-key', 'malformed-json'])
    require(file_sha(candidate) == binary_sha, 'candidate changed during smoke checks')
    receipt.update(checks=sorted(DEFAULT_CHECKS | (DECLARATION_CHECKS if required else set()) |
                                  (ASSESSMENT_CHECKS if assess else set()) |
                                  (STRUCTURE_CHECKS if structure_required_for_version else set())), passed=True)
    return validate_receipt(receipt, version, binary_sha)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--candidate', type=Path, required=True)
    parser.add_argument('--version', required=True)
    parser.add_argument('--output', type=Path, required=True)
    args = parser.parse_args()
    require(not args.output.exists(), 'choose a fresh declaration smoke receipt path')
    receipt = run(args.candidate, args.version)
    args.output.parent.mkdir(parents=True, exist_ok=True)
    with args.output.open('x') as stream:
        stream.write(json.dumps(receipt, indent=2, allow_nan=False) + '\n')
    print(f'Packaged declaration smoke passed; version={args.version}; declarations_required={receipt["declarations_required"]}')


if __name__ == '__main__':
    main()
