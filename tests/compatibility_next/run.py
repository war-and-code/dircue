#!/usr/bin/env python3
"""Differential checks of a released dircue binary and a candidate, without output scrubbing."""
import argparse
import base64
from datetime import datetime, timezone
import gzip
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import platform
import shutil
import subprocess
import tempfile

ROOT = Path(__file__).resolve().parents[2]
V030_DARWIN_ARM64 = 'ff0d723411657a61dc4385c84fc011c1392ce7696066fc13f1c663b1c3967ade'
LEGACY_HARNESS = ROOT / 'tests/compatibility_v030/run.py'
BREADTH = ROOT / 'tests/structural_breadth'


def sha(path):
    return hashlib.sha256(Path(path).read_bytes()).hexdigest()


def load_legacy():
    spec = importlib.util.spec_from_file_location('v030_compatibility_fixture', LEGACY_HARNESS)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def capture(binary, options, cwd, env):
    result = subprocess.run([str(binary), *options], cwd=cwd, env=env, capture_output=True, timeout=60)
    return result.returncode, result.stdout, result.stderr


def recorded(result):
    code, stdout, stderr = result
    record = {'exit': code}
    for key, value in (('stdout', stdout), ('stderr', stderr)):
        record[key + '_sha256'] = hashlib.sha256(value).hexdigest()
        try:
            record[key] = value.decode('utf-8')
        except UnicodeDecodeError:
            record[key + '_base64'] = base64.b64encode(value).decode('ascii')
    return record


def write_files(root, files):
    root.mkdir(parents=True, exist_ok=True)
    for name, value in files.items():
        path = root / name
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_bytes(value.encode() if isinstance(value, str) else value)


def git(root, env, *options):
    return subprocess.check_output(['git', '-c', 'core.hooksPath=/dev/null', '-c', 'commit.gpgsign=false', *options],
                                   cwd=root, env=env, stderr=subprocess.PIPE)


def project_fixture(base, env):
    files = {
        'Directory.Build.props': '<Project><PropertyGroup><LangVersion>latest</LangVersion></PropertyGroup></Project>\n',
        'Directory.Packages.props': '<Project><ItemGroup><PackageVersion Include="Example" Version="1.2.3"/></ItemGroup></Project>\n',
        'global.json': '{"sdk":{"version":"8.0.100","rollForward":"latestFeature"}}\n',
        'NuGet.Config': '<configuration><packageSources><add key="example" value="https://packages.example.invalid/v3/index.json"/></packageSources></configuration>\n',
        'App/App.csproj': '<Project Sdk="Microsoft.NET.Sdk"><PropertyGroup><TargetFrameworks>net8.0;net9.0</TargetFrameworks></PropertyGroup><ItemGroup><ProjectReference Include="../Lib/Lib.csproj"/><ProjectReference Include="$(Missing)/Other.csproj" Condition="Exists(\'$(Missing)\')"/><PackageReference Include="Example" Version="1.2.3"/></ItemGroup><Import Project="../Shared.props"/></Project>\n',
        'Lib/Lib.csproj': '<Project><PropertyGroup><TargetFrameworkVersion>v4.8</TargetFrameworkVersion></PropertyGroup><ItemGroup><ProjectReference Include="../App/App.csproj"/></ItemGroup></Project>\n',
        'Shared.props': '<Project><PropertyGroup><Nullable>enable</Nullable></PropertyGroup></Project>\n',
        'App/Program.cs': 'namespace App; class Program { static int Twice(int x) => x * 2; }\n',
        'Example.slnx': '<Solution><Project Path="App/App.csproj"/><Project Path="Lib/Lib.csproj"/></Solution>\n',
        'java/pom.xml': '<project><modelVersion>4.0.0</modelVersion><groupId>example</groupId><artifactId>parent</artifactId><version>1</version><packaging>pom</packaging><modules><module>child</module><module>missing</module></modules><properties><maven.compiler.release>21</maven.compiler.release></properties></project>\n',
        'java/child/pom.xml': '<project><modelVersion>4.0.0</modelVersion><parent><groupId>example</groupId><artifactId>parent</artifactId><version>1</version></parent><artifactId>child</artifactId></project>\n',
        'java/child/Main.java': 'class Main { int twice(int x) { return x * 2; } }\n',
        'gradle/settings.gradle.kts': 'rootProject.name = "fixture"\ninclude(":app", ":library")\n',
        'gradle/build.gradle.kts': 'plugins { java }\njava { toolchain { languageVersion.set(JavaLanguageVersion.of(21)) } }\n',
        'gradle/gradle.properties': 'org.gradle.java.installations.auto-download=false\n',
        'gradle/gradle/wrapper/gradle-wrapper.properties': 'distributionUrl=https\\://services.gradle.org/distributions/gradle-8.13-bin.zip\n',
        'vendor/cache/pom.xml': '<project><modelVersion>4.0.0</modelVersion><artifactId>cached</artifactId></project>\n',
        'logs/events.xml': '<events><entry>data</entry></events>\n',
        '.gitattributes': 'vendor/** linguist-vendored\nlogs/** linguist-generated\n',
    }
    flat, repo = base / 'projects-flat', base / 'projects-repo'
    for folder in (flat, repo):
        write_files(folder, files)
    git(repo, env, 'init', '-q', '-b', 'main')
    git(repo, env, 'add', '.')
    git(repo, env, 'commit', '-qm', 'project declarations')
    first = git(repo, env, 'rev-parse', 'HEAD').decode().strip()
    (repo / 'App/App.csproj').write_bytes(files['App/App.csproj'].replace('net9.0', 'net10.0').encode())
    git(repo, env, 'add', '.')
    git(repo, env, 'commit', '-qm', 'second project snapshot')
    (repo / 'App/App.csproj').write_bytes(b'<Project><broken>')
    (repo / 'untracked.csproj').write_bytes(b'<Project/>')
    partial = base / 'projects-partial'
    write_files(partial, {
        'broken.csproj': '<Project><unclosed>', 'package.json': 'not JSON',
        'big.csproj': '<Project><!--' + 'x' * (1024 * 1024) + '--></Project>',
        'Directory.Build.props': '<Project><PropertyGroup><LangVersion>$(UNKNOWN)</LangVersion></PropertyGroup></Project>',
        'java/pom.xml': '<!DOCTYPE project [<!ENTITY x SYSTEM "file:///not-read">]><project>&x;</project>',
        '.gitattributes': '*.csproj linguist-detectable=false\n',
    })
    return flat, repo, partial, first


def cases(base, env, legacy_cases, worker):
    rows = [(f'legacy-{index:03}', 'v020-retained', cwd, options) for index, (cwd, options) in enumerate(legacy_cases, 1)]
    flat, repo, partial, first = project_fixture(base, env)
    commands = [
        ['analyze', 'projects'], ['analyze', 'projects', '--json'],
        ['analyze', 'all', '--projects'], ['analyze', 'all', '--projects', '--json'],
        ['analyze', 'all', '--projects', '--metrics', '--files', '--json'],
        ['analyze', 'projects', '--source', 'directory', '--json'],
        ['analyze', 'projects', '--max-file-bytes', '32', '--json'],
        ['analyze', 'projects', '--tree-size', '1', '--json'],
        ['analyze', 'projects', '--workers', '1', '--json'],
        ['analyze', 'projects', '--workers', '8', '--json'],
    ]
    for folder in (flat, repo, partial, base / 'empty', base / 'nohead'):
        rows.extend((f'{folder.name}-{index:02}', 'v030-projects', folder, options) for index, options in enumerate(commands, 1))
    for index, options in enumerate([
        ['analyze', 'projects', '--rev', first, '--json'],
        ['analyze', 'all', '--projects', '--metrics', '--rev', first, '--json'],
        ['analyze', 'projects', '--source', 'git', '--rev', 'missing-revision', '--json'],
        ['analyze', 'projects', '--source', 'directory', '--rev', first, '--json'],
        ['analyze', 'projects', '--json', 'App/App.csproj'],
    ], 1):
        rows.append((f'projects-revision-{index:02}', 'v030-projects', repo, options))
    for index, options in enumerate([
        ['analyze', 'structure', '--json'],
        ['analyze', 'all', '--structure', '--json'],
        ['analyze', 'structure', '--structural-worker', str(base / 'missing-worker'), '--json'],
        ['analyze', 'structure', '--structural-max-file-bytes', '0'],
        ['analyze', 'structure', '--structural-max-file-bytes', '8388609'],
        ['analyze', 'structure', '--structural-timeout', '0s'],
    ], 1):
        rows.append((f'structure-validation-{index:02}', 'v030-structure-validation', flat, options))
    if not worker:
        return rows, []
    breadth = json.loads((BREADTH / 'fixtures.json').read_text())
    native = base / 'structural-languages'
    native.mkdir()
    for entry in breadth:
        shutil.copyfile(BREADTH / 'testdata' / entry['path'], native / entry['path'])
    structural = ['--structural-worker', str(worker)]
    for index, options in enumerate([
        ['analyze', 'structure', '--json', '--files', '--workers', '1'],
        ['analyze', 'structure', '--json', '--files', '--workers', '8'],
        ['analyze', 'structure'],
        ['analyze', 'all', '--projects', '--metrics', '--structure', '--json', '--files'],
        ['analyze', 'structure', '--json', '--files', '--structural-max-file-bytes', '16'],
        ['analyze', 'structure', '--json', '--tree-size', '1'],
    ], 1):
        rows.append((f'structure-breadth-{index:02}', 'v030-structure-native', native, [*options, *structural]))
    for index, entry in enumerate(breadth, 1):
        single = base / 'single-language' / f'{index:02}'
        single.mkdir(parents=True)
        shutil.copyfile(native / entry['path'], single / entry['path'])
        rows.append((f'structure-language-{index:02}', 'v030-structure-native', single,
                     ['analyze', 'structure', '--json', '--files', *structural]))
    qualified = base / 'structural-partial'
    shutil.copytree(native, qualified)
    write_files(qualified, {'broken.py': 'def broken(:\n return 1\n',
                           'unsupported.swift': 'func twice(_ x: Int) -> Int { x * 2 }\n',
                           'generated.go': 'package generated\nfunc Twice(x int) int { return x * 2 }\n',
                           'log.xml': '<events/>\n', '.gitattributes': 'generated.go linguist-generated=true\n'})
    rows.append(('structure-qualified', 'v030-structure-native', qualified,
                 ['analyze', 'structure', '--json', '--files', *structural]))
    native_repo = base / 'structural-repo'
    shutil.copytree(native, native_repo)
    git(native_repo, env, 'init', '-q', '-b', 'main')
    git(native_repo, env, 'add', '.')
    git(native_repo, env, 'commit', '-qm', 'structural source snapshot')
    (native_repo / 'Example.java').write_bytes(b'class Dirty { invalid syntax!!! }')
    for source in ('git', 'directory'):
        rows.append((f'structure-snapshot-{source}', 'v030-structure-native', native_repo,
                     ['analyze', 'structure', '--json', '--files', '--source', source, *structural]))
    return rows, breadth


def check_reference(case_id, result, languages):
    """Check independent fixture expectations before treating output as an oracle."""
    code, stdout, stderr = result
    if case_id in ('structure-breadth-01', 'structure-breadth-02', 'structure-breadth-04'):
        assert code == 0 and not stderr, (case_id, code, stderr)
        structure = json.loads(stdout)['structure']
        assert structure['status'] == 'complete', (case_id, structure['status'])
        assert structure['parse_count'] == len(languages) == 21
        assert structure['analyzed_files'] == len(languages)
        assert {row['language'] for row in structure['files']} == {row['language'] for row in languages}
    elif case_id == 'structure-qualified':
        assert code == 0 and not stderr, (case_id, code, stderr)
        structure = json.loads(stdout)['structure']
        assert structure['status'] == 'partial'
        rows = {row['path']: row for row in structure['files']}
        assert rows['broken.py']['status'] == 'partial' and rows['broken.py']['syntax_errors']
        assert rows['unsupported.swift']['reason'] == 'unsupported_language'
        assert rows['generated.go']['parse_count'] == 0
    elif case_id == 'projects-flat-02':
        assert code == 0 and not stderr, (case_id, code, stderr)
        projects = json.loads(stdout)['projects']
        assert len(projects['projects']) >= 5, 'project fixture did not discover expected declarations'
    elif case_id == 'projects-partial-02':
        assert code == 0 and not stderr, (case_id, code, stderr)
        projects = json.loads(stdout)['projects']
        assert projects['status'] == 'partial' and projects['diagnostics']


def fixture_manifest(base):
    return {str(path.relative_to(base)): sha(path) for path in sorted(base.rglob('*'))
            if path.is_file() and not path.is_symlink() and '.git' not in path.relative_to(base).parts}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--baseline', type=Path, required=True)
    parser.add_argument('--baseline-sha256', default=V030_DARWIN_ARM64)
    parser.add_argument('--candidate', type=Path, required=True)
    parser.add_argument('--worker', type=Path)
    parser.add_argument('--worker-sha256', help='required with --worker; verify against the released worker artifact')
    parser.add_argument('--require-worker', action='store_true')
    parser.add_argument('--output', type=Path, required=True)
    args = parser.parse_args()
    baseline, candidate = args.baseline.resolve(), args.candidate.resolve()
    worker = args.worker.resolve() if args.worker else None
    if sha(baseline) != args.baseline_sha256:
        parser.error('baseline does not match the supplied verified v0.3.0 release hash')
    if args.require_worker and not worker:
        parser.error('--require-worker needs --worker')
    if worker and (not args.worker_sha256 or sha(worker) != args.worker_sha256):
        parser.error('--worker needs its matching verified release --worker-sha256')
    if args.output.exists():
        parser.error('choose a fresh output path; existing receipts are not replaced')
    legacy = load_legacy()
    report = {'schema_version': '1.0.0', 'started_at_utc': datetime.now(timezone.utc).isoformat(),
              'platform': platform.platform(), 'baseline': {'path': str(baseline), 'release': 'v0.3.0', 'sha256': sha(baseline)},
              'candidate': {'path': str(candidate), 'sha256': sha(candidate)},
              'worker': {'path': str(worker), 'sha256': sha(worker)} if worker else None,
              'source': legacy.source_provenance(), 'harness_sha256': sha(__file__),
              'reused_fixture_generator_sha256': sha(LEGACY_HARNESS), 'structural_manifest_sha256': sha(BREADTH / 'fixtures.json'),
              'method': 'Live released reference and candidate on identical paths/environment; raw stdout/stderr/status compared without normalization. Captures are diagnostic reference artifacts, not automatically accepted candidate goldens.',
              'cases': [], 'interface_changes': [], 'untested': [] if worker else ['native structural parsing: worker not supplied']}
    with tempfile.TemporaryDirectory(prefix='dircue-next-compat-') as temporary:
        base = Path(temporary)
        env, flat, inherited = legacy.fixture(base)
        # Avoid implicit structural-worker discovery or host locale coloring.
        env.pop('DIRCUE_STRUCTURAL_WORKER', None)
        env['NO_COLOR'] = '1'
        env['LC_ALL'] = 'C'
        assert len(inherited) == 118, 'retained legacy fixture matrix changed; review before updating'
        matrix, languages = cases(base, env, inherited, worker)
        report['fixtures_sha256'] = fixture_manifest(base)
        report['structural_languages'] = sorted({entry['language'] for entry in languages})
        report['structural_fixtures'] = len(languages)
        for case_id, group, cwd, options in matrix:
            old, new = [capture(binary, options, cwd, env) for binary in (baseline, candidate)]
            check_reference(case_id, old, languages)
            equal = old == new
            row = {'id': case_id, 'group': group, 'cwd': str(cwd.relative_to(base)), 'args': options,
                   'equal': equal, 'baseline': recorded(old)}
            row['candidate'] = {'identical_to_baseline': True} if equal else recorded(new)
            report['cases'].append(row)
        for options in [['--version'], ['--help'], ['analyze'], ['analyze', '--help'],
                        ['analyze', 'all', '--help'], ['analyze', 'projects', '--help'],
                        ['analyze', 'structure', '--help'], ['analyze', 'discovery', '--help'],
                        ['analyze', 'graph', '--help'], ['analyze', 'packages', '--help']]:
            report['interface_changes'].append({'args': options,
                'baseline': recorded(capture(baseline, options, flat, env)),
                'candidate': recorded(capture(candidate, options, flat, env))})
        assert fixture_manifest(base) == report['fixtures_sha256'], 'fixture inputs changed during execution'
    assert sha(baseline) == report['baseline']['sha256'] and sha(candidate) == report['candidate']['sha256'], 'executable changed'
    if worker:
        assert sha(worker) == report['worker']['sha256'], 'worker changed'
    report['exact_matches'] = sum(row['equal'] for row in report['cases'])
    report['total'] = len(report['cases'])
    report['coverage'] = {group: {'total': sum(row['group'] == group for row in report['cases']),
                                  'passed': sum(row['group'] == group and row['equal'] for row in report['cases'])}
                          for group in sorted({row['group'] for row in report['cases']})}
    report['passed'] = report['exact_matches'] == report['total']
    report['finished_at_utc'] = datetime.now(timezone.utc).isoformat()
    args.output.parent.mkdir(parents=True, exist_ok=True)
    payload = (json.dumps(report, indent=2) + '\n').encode()
    args.output.write_bytes(gzip.compress(payload, mtime=0) if args.output.suffix == '.gz' else payload)
    print(json.dumps({'passed': report['passed'], 'exact_matches': report['exact_matches'], 'total': report['total'],
                      'coverage': report['coverage'], 'untested': report['untested'],
                      'differences': [row['id'] for row in report['cases'] if not row['equal']]}))
    raise SystemExit(0 if report['passed'] else 1)


if __name__ == '__main__':
    main()
