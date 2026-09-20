#!/usr/bin/env python3
"""Check released worker defaults and bounded, opt-in function-space evidence."""
import argparse
from datetime import datetime, timezone
import hashlib
import json
from pathlib import Path
import platform
import re
import subprocess
import tempfile

HERE = Path(__file__).resolve().parent
ROOT = HERE.parents[1]
GROUPS = {'abc', 'cognitive', 'cyclomatic', 'halstead', 'loc', 'mi',
          'nargs', 'nexits', 'nom', 'tokens'}


def require(condition, message):
    if not condition:
        raise AssertionError(message)


def sha(value):
    return hashlib.sha256(value).hexdigest()


def file_sha(path):
    return sha(path.read_bytes())


def physical_lines(source):
    # Match Rust str::lines, including terminal CRLF and lone CR behavior.
    return source.count('\n') + (1 if source and not source.endswith('\n') else 0)


def execute(binary, request):
    completed = subprocess.run([str(binary)], input=(json.dumps(request) + '\n').encode(),
                               capture_output=True, timeout=30)
    return completed.returncode, completed.stdout, completed.stderr


def without_timings(output):
    # Only this existing worker field is nondeterministic. No JSON reserialization
    # or path/newline normalization is allowed in the default comparison.
    return re.sub(rb'"timings_ns":\{[^}]*\}', b'"timings_ns":{}', output)


def compare_default(old, new):
    require(old[0] == new[0], 'default exit code changed')
    require(old[2] == new[2], 'default stderr changed')
    require(without_timings(old[1]) == without_timings(new[1]), 'default stdout changed')


def report(worker, request):
    code, output, error = execute(worker, request)
    require(code == 0 and not error, f'worker failed: {code} {error!r}')
    result = json.loads(output)
    require(result.get('parse_count') == 1, 'source must be parsed exactly once')
    return result


def validate_functions(functions, source):
    for key, value in {'provider': 'big-code-analysis@2.2.0', 'rule': 'space-kind-function',
                       'rule_version': '1.0.0', 'scope': 'file', 'limit': 128,
                       'name_max_bytes': 256, 'metric_scope': 'includes_nested_spaces',
                       'order': 'provider_preorder'}.items():
        require(functions[key] == value, f'wrong {key}')
    entries = functions['entries']
    require(len(entries) <= 128, 'per-file retention exceeded')
    require(functions['total_spaces'] == len(entries) + functions['omitted_spaces'] +
            functions['invalid_span_spaces'], 'function population invariant failed')
    previous = 0
    for entry in entries:
        require(previous < entry['index'] <= functions['total_spaces'], 'invalid function ordinal')
        previous = entry['index']
        require(1 <= entry['start_line'] <= entry['end_line'] <= physical_lines(source), 'invalid span')
        require(set(entry['metrics']) == GROUPS, 'missing or fabricated function metric group')
        if entry['name_status'] == 'present':
            name = entry['name']
            require(name and len(name.encode()) <= 256 and
                    not any(ord(c) < 32 or 127 <= ord(c) <= 159 for c in name), 'invalid name')
        else:
            require(entry['name_status'] in {'unavailable', 'omitted'} and 'name' not in entry,
                    'unavailable name was invented')
    partial = (functions['syntax_errors'] or functions['omitted_spaces'] or
               functions['invalid_span_spaces'] or any(e['name_status'] == 'omitted' for e in entries))
    require(functions['status'] == ('partial' if partial else 'complete'), 'coverage not qualified')


def fixture_cases():
    for name, language in [('Examples.java', 'Java'), ('Examples.cs', 'C#'), ('examples.py', 'Python')]:
        yield name, language, (HERE / 'fixtures' / name).read_bytes().decode('utf-8')


def counterexamples(worker):
    rows = []
    for name, language, source in fixture_cases():
        result = report(worker, {'path': name, 'language': language, 'source': source, 'functions': True})
        functions = result['functions']
        validate_functions(functions, source)
        require(functions['status'] == 'complete', f'{language}: unexpected parser recovery')
        entries = {e.get('name'): e for e in functions['entries']}
        names = ('Defective', 'Guarded') if language == 'C#' else ('defective', 'guarded')
        defective, guarded = (entries[n] for n in names)
        # Independently counted from these handwritten fixtures and the pinned
        # provider rule: base 1, plus 1 for an if. A division-by-zero risk adds no
        # decision; a protective check does. These are not quality thresholds.
        require(defective['metrics']['cyclomatic']['sum'] == 1, 'straight-line cyclomatic changed')
        require(guarded['metrics']['cyclomatic']['sum'] == 2, 'guard cyclomatic changed')
        expected = ((1, 2), (4, 7)) if language == 'Python' else ((2, 4), (5, 8))
        require([(e['start_line'], e['end_line']) for e in (defective, guarded)] == list(expected),
                'independently marked source spans changed')
        if language == 'Python':
            require((entries['outer']['start_line'], entries['outer']['end_line']) == (24, 29), 'outer span')
            require((entries['inner']['start_line'], entries['inner']['end_line']) == (25, 28), 'inner span')
            require(entries['outer']['metrics']['cyclomatic']['sum'] >
                    entries['inner']['metrics']['cyclomatic']['sum'], 'nested aggregate lost')
        rows.append({'path': name, 'language': language, 'source_sha256': sha(source.encode()),
                     'functions': functions})
    cases = [('empty', '', 'complete'),
             ('recovery', 'def broken(:\n    return 1\n', 'partial'),
             ('many', ''.join(f'def f{i}():\n    return {i}\n' for i in range(140)), 'partial'),
             ('name_bound', 'def ' + 'x' * 257 + '():\n    return 0\n', 'partial'),
             ('suppression', 'def guard(value):\n    # bca: suppress(cyclomatic)\n    if value:\n        return 1\n    return 0\n', 'complete')]
    for ending in ('', '\n', '\r\n'):
        cases.append(('unicode_' + repr(ending), 'def λ():\r\n    return 1' + ending, 'complete'))
    for name, source, status in cases:
        result = report(worker, {'path': name + '.py', 'language': 'Python', 'source': source, 'functions': True})
        functions = result['functions']
        validate_functions(functions, source)
        require(functions['status'] == status, f'{name}: status mismatch')
        if name == 'many':
            require(functions['total_spaces'] == 140 and functions['omitted_spaces'] == 12 and
                    len(functions['entries']) == 128, 'cap erased population')
        if name.startswith('unicode_'):
            entry = functions['entries'][0]
            require((entry['start_line'], entry['end_line'], entry['name']) == (1, 2, 'λ'), 'newline/UTF-8 span')
        if name == 'suppression':
            require(functions['entries'][0]['metrics']['cyclomatic']['sum'] == 2, 'suppression changed evidence')
            require('suppressions' not in functions['entries'][0], 'suppression leaked into evidence')
        rows.append({'case': name, 'source': source, 'source_sha256': sha(source.encode()), 'functions': functions})
    return rows


def cli_counterexamples(candidate, worker):
    with tempfile.TemporaryDirectory(prefix='dircue-functions-') as temporary:
        path = Path(temporary)
        expected = {}
        for name, language, source in fixture_cases():
            (path / name).write_bytes(source.encode())
            direct = report(worker, {'path': name, 'language': language, 'source': source, 'functions': True})
            for entry in direct['functions']['entries']:
                expected[(name, entry['index'])] = {**entry, 'path': name, 'language': language,
                                                   'source_sha256': sha(source.encode())}
        # Generated source remains excluded by the existing language admission
        # policy; the function view must expose that omission, not override it.
        (path / 'generated.py').write_text('def generated():\n    return 0\n')
        (path / '.gitattributes').write_text('generated.py linguist-generated=true\n')
        command = [str(candidate), 'analyze', 'structure', '--source', 'directory', '--json',
                   '--functions', '--structural-worker', str(worker), str(path)]
        completed = subprocess.run(command, capture_output=True, timeout=60)
        require(completed.returncode == 0 and not completed.stderr, f'CLI failed: {completed.stderr!r}')
        result = json.loads(completed.stdout)
        functions = result['structure']['functions']
        actual = {(e['path'], e['index']): e for e in functions['entries']}
        require(actual == expected, 'CLI source identity, spans or provider measurements changed')
        require(functions['total_spaces'] == len(expected) and functions['omitted_spaces'] == 0,
                'CLI population changed')
        require(all(e['path'] != 'generated.py' for e in functions['entries']), 'generated source admitted')
        require(functions['omissions'], 'excluded source omitted without coverage disclosure')
        return {'command': command, 'stdout_sha256': sha(completed.stdout), 'functions': functions}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--baseline-worker', type=Path, required=True)
    parser.add_argument('--worker', type=Path, required=True)
    parser.add_argument('--candidate', type=Path, help='also check the real CLI on the counterexamples')
    parser.add_argument('--output', type=Path, required=True)
    args = parser.parse_args()
    require(not args.output.exists(), 'choose a new receipt path')
    baseline, worker = args.baseline_worker.resolve(), args.worker.resolve()
    fixtures = json.loads((ROOT / 'tests/structural_breadth/fixtures.json').read_text())
    receipt = {'started_at_utc': datetime.now(timezone.utc).isoformat(), 'platform': platform.platform(),
               'baseline_worker_sha256': file_sha(baseline), 'worker_sha256': file_sha(worker),
               'harness_sha256': file_sha(Path(__file__)), 'default_cases': [], 'breadth': [],
               'method': 'Default stdout is compared byte-for-byte except timings_ns; stderr and exit '
                         'code exact. Counterexample spans and two cyclomatic values per language are '
                         'hand counted. Other metrics are provider evidence, not independent formula validation.'}
    native_root = ROOT / 'prototypes/structural/worker'
    receipt['observed_native_source_sha256'] = {
        str(path.relative_to(ROOT)): file_sha(path)
        for path in [*sorted((native_root / 'src').glob('*.rs')), native_root / 'Cargo.toml', native_root / 'Cargo.lock']}
    receipt['source_provenance_limit'] = 'Observed local source hashes do not prove these exact bytes built the supplied binary; the binary SHA256 identifies what was exercised.'
    for fixture in fixtures:
        source = (ROOT / 'tests/structural_breadth/testdata' / fixture['path']).read_bytes().decode('utf-8')
        for mode in ('combined', 'structure', 'metrics'):
            request = {'path': fixture['path'], 'language': fixture['language'], 'source': source, 'mode': mode}
            old, new = execute(baseline, request), execute(worker, request)
            compare_default(old, new)
            receipt['default_cases'].append({'path': fixture['path'], 'mode': mode,
                                            'source_sha256': sha(source.encode()), 'stdout_sha256_except_timings': sha(without_timings(new[1])),
                                            'exit_code': new[0], 'stderr_sha256': sha(new[2])})
        request.update(mode='combined', functions=True)
        result = report(worker, request)
        validate_functions(result['functions'], source)
        require(result['functions']['status'] == 'complete', 'breadth fixture became partial')
        receipt['breadth'].append({**fixture, 'source_sha256': sha(source.encode()), 'functions': result['functions']})
    for language, source in [('Java', 'class Broken { void x( {'), ('Python', 'def broken(:\n return'),
                             ('C#', 'class Broken { void X( {'), ('XML', '<log/>')]:
        request = {'path': 'broken', 'language': language, 'source': source}
        old, new = execute(baseline, request), execute(worker, request)
        compare_default(old, new)
        receipt['default_cases'].append({'language': language, 'source': source,
                                        'source_sha256': sha(source.encode()), 'stdout_sha256_except_timings': sha(without_timings(new[1])),
                                        'exit_code': new[0], 'stderr_sha256': sha(new[2])})
    require(len(receipt['default_cases']) == 67 and len(receipt['breadth']) == 21, 'missing protocol cases')
    receipt['counterexamples'] = counterexamples(worker)
    receipt['cli'] = cli_counterexamples(args.candidate.resolve(), worker) if args.candidate else None
    if args.candidate:
        receipt['candidate_sha256'] = file_sha(args.candidate)
    receipt['passed'] = True
    args.output.parent.mkdir(parents=True, exist_ok=True)
    args.output.write_text(json.dumps(receipt, indent=2, allow_nan=False) + '\n')
    print(json.dumps({'passed': True, 'default_cases': 67, 'breadth': 21,
                      'counterexamples': len(receipt['counterexamples']), 'cli_checked': bool(args.candidate)}))


if __name__ == '__main__':
    main()
