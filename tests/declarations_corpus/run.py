#!/usr/bin/env python3
"""Check hand-selected manifest facts and integrated declaration invariants offline."""
import argparse
import collections
import copy
import gzip
import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess
import time

ROOT = Path(__file__).resolve().parents[2]
HERE = Path(__file__).resolve().parent


def sha(data):
    return hashlib.sha256(data).hexdigest()


def git(root, *args):
    env = {**os.environ, 'GIT_CONFIG_NOSYSTEM': '1', 'GIT_CONFIG_GLOBAL': os.devnull, 'GIT_TERMINAL_PROMPT': '0'}
    return subprocess.check_output(['git', '-C', str(root), '-c', 'core.hooksPath=' + os.devnull, '-c', 'core.fsmonitor=false', *args], env=env)


def save(path, data):
    path.parent.mkdir(parents=True, exist_ok=True)
    with path.open('xb') as out:
        out.write(data)


def inventory(root):
    rows = []
    for base, dirs, files in os.walk(root, followlinks=False):
        dirs[:] = sorted(d for d in dirs if d != '.git')
        for name in sorted(files + [d for d in dirs if (Path(base) / d).is_symlink()]):
            path = Path(base) / name
            rel = path.relative_to(root).as_posix()
            if path.is_symlink():
                rows.append([rel, 'symlink', os.readlink(path)])
            elif path.is_file():
                data = path.read_bytes()
                rows.append([rel, len(data), sha(data)])
            else:
                raise ValueError('special file outside corpus contract')
    return {'files': len(rows), 'sha256': sha(json.dumps(sorted(rows), separators=(',', ':')).encode())}


def facts(module, expected):
    errors = []
    projects = {p['id']: p for p in module['projects']}
    if len(projects) != len(module['projects']):
        errors.append('duplicate project IDs')
    for segment in expected.get('excluded_manifest_segments', []):
        if any(segment in identity.split('/') for identity in projects):
            errors.append('excluded manifest segment retained: ' + segment)
    for fact in expected.get('facts', []):
        project = projects.get(fact['id'])
        if project is None:
            errors.append('missing project: ' + fact['id'])
            continue
        for field, subset in fact.items():
            if field == 'id':
                continue
            rows = [project] if field == 'project' else project.get(field, [])
            if not any(all(k not in row if v is None else row.get(k) == v for k, v in subset.items()) for row in rows):
                errors.append('missing fact: ' + json.dumps(fact, sort_keys=True))
    for fact in expected.get('forbidden_facts', []):
        project = projects.get(fact['id'])
        if project is None:
            errors.append('missing project for forbidden fact: ' + fact['id'])
            continue
        for field, subset in fact.items():
            if field == 'id':
                continue
            rows = [project] if field == 'project' else project.get(field, [])
            if any(all(k not in row if v is None else row.get(k) == v for k, v in subset.items()) for row in rows):
                errors.append('unsupported inferred fact: ' + json.dumps(fact, sort_keys=True))
    for expected_diagnostic in expected.get('expected_diagnostics', []):
        if not any(all(d.get(k) == v for k, v in expected_diagnostic.items()) for d in module['diagnostics']):
            errors.append('expected diagnostic lost: ' + json.dumps(expected_diagnostic, sort_keys=True))
    rendered = json.dumps(module)
    for forbidden in expected.get('forbidden', []):
        if forbidden in rendered:
            errors.append('forbidden raw command in declarations')
    if module['diagnostics'] and module['status'] != 'partial':
        errors.append('diagnostics must remain visible as partial status')
    if module['coverage']['omitted_files'] and module['status'] != 'partial':
        errors.append('omissions must remain visible as partial status')
    return errors


def normalize(module):
    result = copy.deepcopy(module)
    # Git tree identity and acquisition method intentionally differ. Every
    # observation, limit, diagnostic and coverage count still participates.
    result.pop('source', None)
    result.pop('tree', None)
    return result


def synthetic(root):
    files = {
        'go.work': 'go 1.26.0\nuse (\n ./go/a\n ./go/b\n)\n',
        'go/a/go.mod': 'module example.test/a\ngo 1.26.0\nrequire example.test/b v0.0.0\nreplace example.test/b => ../b\n',
        'go/b/go.mod': 'module example.test/b\ngo 1.26.0\n',
        'package.json': '{"name":"workspace-root","private":true,"workspaces":["js/*"],"scripts":{"build":"DO_NOT_EXECUTE_SENTINEL"}}',
        'js/a/package.json': '{"name":"a","version":"1.0.0","dependencies":{"b":"file:../b"}}',
        'js/b/package.json': '{"name":"b","version":"1.0.0"}',
        'pyproject.toml': '[tool.uv.workspace]\nmembers=["py/*"]\n',
        'py/a/pyproject.toml': '[project]\nname="a"\ndynamic=["version"]\nrequires-python=">=3.11"\ndependencies=["b"]\n[tool.uv.sources]\nb={workspace=true}\n[build-system]\nrequires=["hatchling"]\nbuild-backend="hatchling.build"\n',
        'py/b/pyproject.toml': '[project]\nname="b"\nversion="1.0.0"\nrequires-python=">=3.11"\n',
        'Cargo.toml': '[workspace]\nmembers=["rs/a","rs/b"]\nresolver="2"\n[workspace.package]\nversion="1.2.3"\nedition="2021"\n',
        'rs/a/Cargo.toml': '[package]\nname="a"\nversion.workspace=true\nedition.workspace=true\n[dependencies]\nb={path="../b"}\n',
        'rs/b/Cargo.toml': '[package]\nname="b"\nversion="1.0.0"\nedition="2021"\n',
        'rs/a/src/lib.rs': 'pub fn a() {}\n',
        'rs/b/src/lib.rs': 'pub fn b() {}\n',
        'malformed/package.json': '{"name":"first","name":"duplicate"}',
    }
    for name, text in files.items():
        path = root / name
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(text)
    expected = {'facts': [
        {'id': 'go.work', 'references': {'kind': 'go-workspace-member', 'target': 'go/a/go.mod', 'target_status': 'present'}},
        {'id': 'go/a/go.mod', 'references': {'kind': 'go-local-replacement', 'target': 'go/b/go.mod', 'target_status': 'present'}},
        {'id': 'package.json', 'references': {'kind': 'npm-workspace-member', 'target': 'js/a/package.json', 'target_status': 'present'}},
        {'id': 'js/a/package.json', 'references': {'kind': 'npm-local-dependency', 'target': 'js/b/package.json', 'target_status': 'present'}},
        {'id': 'pyproject.toml', 'references': {'kind': 'uv-workspace-member', 'target': 'py/a/pyproject.toml', 'state': 'resolved'}},
        {'id': 'py/a/pyproject.toml', 'requirements': {'kind': 'python-dynamic-field', 'value': 'version', 'state': 'dynamic'}},
        {'id': 'py/a/pyproject.toml', 'project': {'name': 'a'}},
        {'id': 'Cargo.toml', 'references': {'kind': 'cargo-workspace-member', 'target': 'rs/a/Cargo.toml', 'target_status': 'present'}},
        {'id': 'rs/a/Cargo.toml', 'project': {'name': 'a', 'version': '1.2.3'}},
        {'id': 'rs/a/Cargo.toml', 'references': {'kind': 'cargo-path-dependency', 'target': 'rs/b/Cargo.toml', 'target_status': 'present'}},
    ], 'forbidden': ['DO_NOT_EXECUTE_SENTINEL']}
    return expected


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--candidate', type=Path, required=True)
    parser.add_argument('--build-receipt', type=Path, required=True)
    parser.add_argument('--output', type=Path, required=True)
    parser.add_argument('--corpus', type=Path, default=ROOT / '.cache/corpus')
    parser.add_argument('--case', action='append', dest='cases', help='Run only the named case; repeat to select multiple cases. Default: every case.')
    args = parser.parse_args()
    binary = args.candidate.resolve()
    receipt_raw = args.build_receipt.read_bytes()
    receipt = json.loads(receipt_raw)
    assert receipt['schema'] == 'dircue-v050-benchmark-build-1'
    assert receipt['candidate_sha256'] == sha(binary.read_bytes())
    assert receipt['files'] and receipt['source_at_build']
    output = args.output.resolve()
    output.mkdir(parents=True, exist_ok=False)
    save(output / 'build.json', receipt_raw)
    expected_raw = (HERE / 'expectations.json').read_bytes()
    expected = json.loads(expected_raw)['cases']
    selected_cases = set(args.cases or [*expected, 'dircue-selected-working-tree', 'multi-workspace'])
    unknown = selected_cases - {*expected, 'dircue-selected-working-tree', 'multi-workspace'}
    if unknown:
        parser.error('unknown case: ' + ', '.join(sorted(unknown)))
    cases = []
    # Verify pin, cleanliness and literal witnesses before any candidate run.
    for name, spec in expected.items():
        if name not in selected_cases:
            continue
        path = args.corpus.resolve() / name
        assert git(path, 'rev-parse', 'HEAD').decode().strip() == spec['commit'], name + ': incorrect commit'
        assert not git(path, 'status', '--porcelain=v1', '--untracked-files=all'), name + ': dirty input'
        witnesses = {}
        for filename, fragments in spec['witnesses'].items():
            data = (path / filename).read_bytes()
            text = data.decode('utf-8-sig')
            assert all(fragment in text for fragment in fragments), name + ': manifest witness differs'
            witnesses[filename] = sha(data)
        cases.append((name, path, spec, ['git', 'directory'], {'commit': spec['commit'], 'url': spec['url'], 'witness_sha256': witnesses, 'inventory': inventory(path)}))
    if 'dircue-selected-working-tree' in selected_cases:
        own = output / 'dircue-selected-working-tree'
        own.mkdir()
        selected = git(ROOT, 'ls-files', '-z', '--cached', '--others', '--exclude-standard').split(b'\0')
        for raw in sorted(set(selected)):
            if not raw:
                continue
            relative = os.fsdecode(raw)
            source = ROOT / relative
            target = own / relative
            if source.is_symlink():
                target.parent.mkdir(parents=True, exist_ok=True)
                target.symlink_to(os.readlink(source))
            elif source.is_file():
                target.parent.mkdir(parents=True, exist_ok=True)
                shutil.copyfile(source, target)
        own_spec = {'facts': [{'id': 'go.mod', 'project': {'name': 'dircue'}}, {'id': 'go.mod', 'references': {'kind': 'go-local-replacement', 'target': 'third_party/go-enry/go.mod', 'target_status': 'present'}}]}
        cases.append(('dircue-selected-working-tree', own, own_spec, ['directory'], {'selection': 'working-tree tracked and nonignored untracked files; no .git or ignored .cache', 'inventory': inventory(own)}))
    if 'multi-workspace' in selected_cases:
        synth = output / 'synthetic-input'
        synth.mkdir()
        synth_spec = synthetic(synth)
        cases.append(('multi-workspace', synth, synth_spec, ['directory'], {'selection': 'hand-authored mixed workspace and malformed npm manifest', 'inventory': inventory(synth)}))
    results, all_errors = [], []
    for name, path, spec, sources, provenance in cases:
        canonical = None
        for source in sources:
            for command in ('declarations', 'all'):
                for workers in (1, 8):
                    key = f'{name}-{source}-{command}-{workers}'
                    args_cli = [str(binary), 'analyze', command, '--json', '--source', source, '--workers', str(workers)]
                    if command == 'all':
                        args_cli.append('--declarations')
                    args_cli.append(str(path))
                    started = time.monotonic()
                    run = subprocess.run(args_cli, capture_output=True, timeout=180)
                    save(output / (key + '.stdout.json.gz'), gzip.compress(run.stdout, mtime=0))
                    save(output / (key + '.stderr'), run.stderr)
                    row = {'case': name, 'source': source, 'command': command, 'workers': workers, 'exit_code': run.returncode, 'stdout_sha256': sha(run.stdout), 'stderr_sha256': sha(run.stderr), 'seconds_observed_not_benchmark': time.monotonic() - started, 'errors': []}
                    expected_warnings = spec.get('directory_warnings', []) if source == 'directory' else []
                    warnings = run.stderr.decode('utf-8', errors='replace').splitlines()
                    row['warnings'] = warnings
                    if sorted(warnings) != sorted(expected_warnings):
                        row['errors'].append('unexpected or missing stderr warning; retained artifact requires inspection')
                    if run.returncode:
                        row['errors'].append('nonzero exit; retained artifact requires inspection')
                    else:
                        try:
                            module = json.loads(run.stdout)['declarations']
                            row['errors'].extend(facts(module, spec))
                            if canonical is None:
                                canonical = normalize(module)
                            elif normalize(module) != canonical:
                                row['errors'].append('module changed across command/worker/source variants')
                            row.update(status=module['status'], coverage=module['coverage'], diagnostics=dict(collections.Counter(d['code'] for d in module['diagnostics'])), project_count=len(module['projects']))
                            if name == 'multi-workspace':
                                if module['status'] != 'partial' or not any(d['path'] == 'malformed/package.json' for d in module['diagnostics']):
                                    row['errors'].append('malformed manifest diagnostic/status was lost')
                                dynamic = next(p for p in module['projects'] if p['id'] == 'py/a/pyproject.toml')
                                if dynamic.get('version'):
                                    row['errors'].append('dynamic Python version fabricated')
                        except (KeyError, ValueError, StopIteration) as error:
                            row['errors'].append('invalid result shape: ' + type(error).__name__)
                    results.append(row)
                    all_errors.extend(key + ': ' + error for error in row['errors'])
                    print(key, 'PASS' if not row['errors'] else 'FAIL', flush=True)
        if inventory(path) != provenance['inventory']:
            all_errors.append(name + ': input changed during profiling')
    report = {'schema': 'dircue-declarations-corpus-result-1', 'expectations_sha256': sha(expected_raw), 'harness_sha256': sha(Path(__file__).read_bytes()), 'candidate_sha256': receipt['candidate_sha256'], 'build_receipt_sha256': sha(receipt_raw), 'provenance': {name: provenance for name, _, _, _, provenance in cases}, 'runs': results, 'errors': all_errors, 'passed': not all_errors, 'scope': 'Selected literal facts and deterministic acquisition, not full build evaluation or exhaustive parser conformance.'}
    save(output / 'results.json', (json.dumps(report, indent=2) + '\n').encode())
    print(json.dumps({'runs': len(results), 'passed': report['passed'], 'errors': all_errors}, indent=2))
    raise SystemExit(0 if report['passed'] else 1)


if __name__ == '__main__':
    main()
