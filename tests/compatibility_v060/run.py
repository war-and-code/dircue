#!/usr/bin/env python3
"""Compare released 0.5.0 with a candidate using raw outputs and exit codes."""
import argparse
from datetime import datetime, timezone
import gzip
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import platform
import tempfile

ROOT = Path(__file__).resolve().parents[2]
REFERENCE_SHA256 = 'ab4de3aec442d70f0849a52a5aa1a5540fecc4540145e3226aafaba11930b3f4'
PREVIOUS = ROOT / 'tests/compatibility_next/run.py'
spec = importlib.util.spec_from_file_location('previous_compatibility', PREVIOUS)
previous = importlib.util.module_from_spec(spec)
spec.loader.exec_module(previous)


def extra_cases(base, worker):
    root = base / 'v040-modules'
    previous.write_files(root, {
        'App.csproj': '<Project><ItemGroup><ProjectReference Include="lib/Lib.csproj"/></ItemGroup></Project>',
        'lib/Lib.csproj': '<Project/>',
        'App.cs': 'class App { int Twice(int x) { return x * 2; } }\n',
        'logs/events.xml': '<events/>\n',
        'data.txt': 'plain text\n',
        '.npmrc': 'registry=https://registry.example.invalid/path\n//registry.example.invalid/:_authToken=DO_NOT_DISCLOSE\n',
        'NuGet.Config': '<configuration><packageSources><add key="example" value="https://packages.example.invalid/v3/index.json"/></packageSources></configuration>',
        'dotnet/app/App.csproj': '<Project/>',
    })
    policy = base / 'policy.json'
    policy.write_text(json.dumps({'schema_version': '1.0.0', 'rules': [
        {'id': 'source', 'match': {'extensions': ['.cs']}},
        {'id': 'content', 'match': {'filenames': ['data.txt']}, 'content': {'contains_utf8': 'plain'}}]}))
    syft = ROOT / 'tests/packageevidence/fixtures/syft-1.52.0.json'
    commands = []
    for mode in ('discovery', 'graph', 'registries'):
        commands.extend([
            ['analyze', mode], ['analyze', mode, '--json'],
            ['analyze', mode, '--json', '--tree-size', '1'],
            ['analyze', mode, '--json', '--max-file-bytes', '1'],
        ])
    for prefix in (['analyze', 'rules'], ['analyze', 'all']):
        commands.extend([[*prefix, '--rules-file', str(policy), '--json'],
                         [*prefix, '--rules-file', str(policy), '--discovery'],
                         [*prefix, '--rules-file', str(policy), '--json', '--tree-size', '1']])
    for prefix in (['analyze', 'packages'], ['analyze', 'all']):
        commands.extend([[*prefix, '--syft-report', str(syft), '--syft-root', '/', '--json'],
                         [*prefix, '--syft-report', str(syft), '--json'],
                         [*prefix, '--syft-report', str(syft), '--syft-root', '/']])
    commands.append(['analyze', 'all', '--json', '--discovery', '--graph', '--registries', '--rules-file', str(policy)])
    commands.extend([['analyze', 'rules'], ['analyze', 'rules', '--rules-file', str(base / 'absent')],
                     ['analyze', 'packages'], ['analyze', 'packages', '--syft-report', str(policy)]])
    if worker:
        commands.extend([
            ['analyze', 'structure', '--functions', '--json', '--structural-worker', str(worker)],
            ['analyze', 'all', '--structure', '--functions', '--files', '--json', '--structural-worker', str(worker)],
            ['analyze', 'structure', '--functions', '--structural-worker', str(worker)],
        ])
    return [(f'v040-modules-{i:03}', 'v040-modules', root, args) for i, args in enumerate(commands, 1)]


def declaration_cases(base, baseline, env):
    import sys
    sys.path.insert(0, str(ROOT / 'scripts'))
    import declarations_release_smoke as fixture
    root = base / 'v050-declarations'
    previous.write_files(root, fixture.FIXTURES)
    cases = []
    for prefix in (['analyze', 'declarations'], ['analyze', 'all', '--declarations']):
        for suffix in ([], ['--json'], ['--json', '--workers', '1'], ['--json', '--workers', '8'],
                       ['--json', '--max-file-bytes', '1'], ['--json', '--tree-size', '1'],
                       ['--json', '--projects', '--graph', '--discovery']):
            cases.append((root, [*prefix, '--source', 'directory', *suffix]))
    saved = base / 'v050-saved'
    saved.mkdir()
    command = ['analyze', 'declarations', '--source', 'directory', '--json']
    original = previous.capture(baseline, command, root, env)
    assert original[0] == 0 and not original[2] and json.loads(original[1])['declarations']['status'] == 'complete'
    (saved / 'base.json').write_bytes(original[1])
    manifest = root / 'go/app/go.mod'
    original_manifest = manifest.read_bytes()
    manifest.write_bytes(original_manifest.replace(b'1.24.0', b'1.25.0'))
    changed = previous.capture(baseline, command, root, env)
    assert changed[0] == 0 and not changed[2]
    (saved / 'head.json').write_bytes(changed[1])
    manifest.write_bytes(original_manifest)
    broken = root / 'broken' / 'pyproject.toml'
    broken.parent.mkdir()
    broken.write_bytes(b'[project\ninvalid-manifest')
    partial = previous.capture(baseline, command, root, env)
    assert partial[0] == 0 and not partial[2]
    broken.unlink()
    broken.parent.rmdir()
    (saved / 'partial.json').write_bytes(partial[1])
    (saved / 'bad.json').write_bytes(b'{"duplicate":1,"duplicate":2}')
    (saved / 'malformed.json').write_bytes(b'{bad')
    for before, after in [('base.json', 'base.json'), ('base.json', 'head.json'), ('head.json', 'base.json'),
                          ('base.json', 'partial.json'), ('partial.json', 'base.json'), ('base.json', 'bad.json'),
                          ('malformed.json', 'head.json'), ('base.json', 'missing.json')]:
        for flag in ([], ['--json']):
            cases.append((saved, ['compare', before, after, *flag]))
    cases.extend([(saved, ['compare']), (saved, ['compare', 'base.json']),
                  (saved, ['compare', 'base.json', 'head.json', 'base.json']),
                  (root, ['analyze', 'declarations', '--source', 'invalid'])])
    # Read-only Git snapshots use locally authored fixture commits only.
    previous.git(root, env, 'init', '-q', '-b', 'main')
    previous.git(root, env, 'config', 'user.name', 'Fixture')
    previous.git(root, env, 'config', 'user.email', 'fixture@example.invalid')
    previous.git(root, env, 'config', 'core.autocrlf', 'false')
    previous.git(root, env, 'add', '.')
    previous.git(root, env, '-c', 'commit.gpgsign=false', '-c', 'core.hooksPath=/dev/null', 'commit', '-qm', 'fixture')
    for flags in (['--source', 'git'], ['--source', 'git', '--json'], ['--commit', 'HEAD', '--json']):
        cases.append((root, ['analyze', 'declarations', *flags]))
    return [(f'v050-declarations-{i:03}', 'v050-declarations', cwd, args) for i, (cwd, args) in enumerate(cases, 1)]


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--baseline', required=True, type=Path)
    parser.add_argument('--baseline-sha256', default=REFERENCE_SHA256)
    parser.add_argument('--candidate', required=True, type=Path)
    parser.add_argument('--build-receipt', required=True, type=Path,
                        help='Historical candidate receipt from the v050 performance harness build command')
    parser.add_argument('--worker', type=Path)
    parser.add_argument('--worker-sha256')
    parser.add_argument('--output', required=True, type=Path)
    args = parser.parse_args()
    baseline, candidate = args.baseline.resolve(), args.candidate.resolve()
    worker = args.worker.resolve() if args.worker else None
    assert previous.sha(baseline) == args.baseline_sha256, 'unverified reference binary'
    build_record = json.loads(args.build_receipt.read_text())
    assert build_record.get('schema') in {'dircue-v050-benchmark-build-1', 'dircue-v060-benchmark-build-1'}, 'unsupported build receipt'
    assert build_record.get('files') and build_record.get('source_at_build'), 'missing source binding'
    assert build_record.get('candidate_sha256') == previous.sha(candidate), 'candidate differs from build receipt'
    if worker:
        assert args.worker_sha256 and previous.sha(worker) == args.worker_sha256, 'unverified worker'
    assert not args.output.exists(), 'choose a fresh output receipt'
    legacy = previous.load_legacy()
    report = {'schema_version': '1.0.0', 'started_at_utc': datetime.now(timezone.utc).isoformat(),
        'baseline_release': 'v0.5.0', 'baseline_sha256': previous.sha(baseline),
        'candidate_sha256': previous.sha(candidate), 'platform': platform.platform(),
        'harness_sha256': previous.sha(__file__),
        'declaration_fixture_sha256': previous.sha(ROOT / 'scripts/declarations_release_smoke.py'), 'fixture_helpers': {str(p.relative_to(ROOT)): previous.sha(p) for p in (PREVIOUS, previous.LEGACY_HARNESS)},
        'build_receipt': build_record, 'build_receipt_sha256': previous.sha(args.build_receipt),
        'cases': [], 'interface_changes': [],
        'method': 'Released reference and candidate run on identical source paths/environment. Raw stdout, stderr and exit code must match; no normalization. Version/help changes are recorded separately. Candidate comparison build sets only its reported version to 0.5.0.',
        'untested': [] if worker else ['native structural parsing: worker not supplied']}
    if worker:
        report['worker_sha256'] = previous.sha(worker)
    with tempfile.TemporaryDirectory(prefix='dircue-v060-compat-') as temporary:
        base = Path(temporary)
        env, flat, inherited = legacy.fixture(base)
        env.pop('DIRCUE_STRUCTURAL_WORKER', None)
        env.update(NO_COLOR='1', LC_ALL='C')
        matrix, languages = previous.cases(base, env, inherited, worker)
        matrix += extra_cases(base, worker)
        matrix += declaration_cases(base, baseline, env)
        report['fixtures_sha256'] = previous.fixture_manifest(base)
        for case_id, group, cwd, options in matrix:
            old, new = [previous.capture(binary, options, cwd, env) for binary in (baseline, candidate)]
            previous.check_reference(case_id, old, languages)
            row = {'id': case_id, 'group': group, 'args': options, 'cwd': str(cwd.relative_to(base)), 'equal': old == new,
                   'baseline': previous.recorded(old), 'candidate': {'identical_to_baseline': True} if old == new else previous.recorded(new)}
            report['cases'].append(row)
        for options in (['--version'], ['--help'], ['analyze'], ['analyze', '--help'], ['analyze', 'all', '--help']):
            report['interface_changes'].append({'args': options, 'baseline': previous.recorded(previous.capture(baseline, options, flat, env)), 'candidate': previous.recorded(previous.capture(candidate, options, flat, env))})
        assert previous.fixture_manifest(base) == report['fixtures_sha256'], 'fixtures changed during execution'
    report['total'] = len(report['cases'])
    report['exact_matches'] = sum(row['equal'] for row in report['cases'])
    report['passed'] = report['total'] == report['exact_matches']
    report['finished_at_utc'] = datetime.now(timezone.utc).isoformat()
    assert previous.sha(baseline) == report['baseline_sha256'] and previous.sha(candidate) == report['candidate_sha256'], 'binary changed during execution'
    args.output.parent.mkdir(parents=True, exist_ok=True)
    payload = (json.dumps(report, indent=2) + '\n').encode()
    args.output.write_bytes(gzip.compress(payload, mtime=0) if args.output.suffix == '.gz' else payload)
    print(json.dumps({k: report[k] for k in ('passed', 'total', 'exact_matches', 'untested')}))
    raise SystemExit(0 if report['passed'] else 1)


if __name__ == '__main__':
    main()
