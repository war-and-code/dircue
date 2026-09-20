#!/usr/bin/env python3
"""Measure opt-in function evidence on bounded fixtures and pinned source files."""
import argparse
from datetime import datetime, timezone
import gzip
import json
from pathlib import Path
import platform
import re
import statistics
import subprocess
import tempfile
import time

from run import ROOT, file_sha, fixture_cases, require, sha, validate_functions, without_timings


def source_snapshot(root):
    names = subprocess.check_output(['git', '-C', str(root), 'ls-files', '-co', '--exclude-standard', '-z'])
    selected = {}
    for raw in sorted(set(names.split(b'\0')) - {b''}):
        name = raw.decode()
        path = root / name
        if path.is_file() and (path.suffix in {'.go', '.rs', '.mod', '.sum', '.db'} or
                               path.name in {'Cargo.lock', 'Cargo.toml'}):
            selected[name] = file_sha(path)
    return selected


def measure(command, payload=None):
    start = time.perf_counter()
    result = subprocess.run(['/usr/bin/time', '-l', *command], input=payload,
                            capture_output=True, timeout=120)
    elapsed = time.perf_counter() - start
    require(result.returncode == 0, f'command failed: {command}: {result.stderr!r}')
    match = re.search(rb'(\d+)\s+maximum resident set size', result.stderr)
    require(match is not None, 'macOS RSS measurement missing')
    # time emits the command's stderr before its own measurements.
    require(re.match(rb'\s*\d+(?:\.\d+)? real\s+\d+(?:\.\d+)? user', result.stderr) is not None,
            f'unexpected command stderr: {result.stderr!r}')
    return result.stdout, {'seconds': elapsed, 'max_rss_bytes': int(match[1]),
                           'stdout_bytes': len(result.stdout), 'stdout_sha256': sha(result.stdout),
                           'time_stderr': result.stderr.decode()}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    for name in ('baseline', 'candidate', 'baseline-worker', 'worker', 'candidate-source', 'corpus-root', 'output'):
        parser.add_argument('--' + name, type=Path, required=True)
    args = parser.parse_args()
    require(platform.system() == 'Darwin', 'this diagnostic uses macOS time -l; RSS is bytes')
    require(not args.output.exists(), 'choose a fresh output directory')
    args.output.mkdir(parents=True)
    binaries = {name: getattr(args, name).resolve() for name in ('baseline', 'candidate', 'baseline_worker', 'worker')}
    hashes = {name: file_sha(path) for name, path in binaries.items()}
    snapshot = source_snapshot(args.candidate_source.resolve())
    native_root = ROOT / 'prototypes/structural/worker'
    worker_snapshot = {str(p.relative_to(ROOT)): file_sha(p) for p in
                       [*sorted((native_root / 'src').glob('*.rs')), native_root / 'Cargo.toml', native_root / 'Cargo.lock']}
    versions = {name: subprocess.check_output([str(binaries[name]), '--version']).decode().strip()
                for name in ('baseline', 'candidate')}
    receipt = {'started_at_utc': datetime.now(timezone.utc).isoformat(), 'platform': platform.platform(),
               'binary_sha256': hashes, 'versions': versions, 'harness_sha256': file_sha(Path(__file__)),
               'helpers_sha256': file_sha(Path(__file__).with_name('run.py')),
               'candidate_source_snapshot': snapshot, 'native_source_snapshot': worker_snapshot,
               'candidate_go_build_info': subprocess.check_output(['go', 'version', '-m', str(binaries['candidate'])]).decode(),
               'method': {'warmups': 1, 'repetitions': 3, 'workers': 1,
                          'order': 'rotate all variants one position per repetition',
                          'wall_time': 'external command plus time wrapper; excludes report validation and fixture creation',
                          'memory': 'macOS time -l maximum RSS; CLI and standalone worker measured separately. This is not simultaneous process-tree peak; do not add peaks.',
                          'limits': 'No CPU/RAM limits; warm-cache diagnostic on one host, not an optimization claim or universal overhead bound. No repository code executed.',
                          'source_provenance': 'Binary hashes bind the executable tested. Source snapshots are checked before/after, not a reproducible-build attestation.'},
               'cases': []}
    pins = {p['name']: p for p in json.loads((ROOT / 'tests/performance/corpus.json').read_text())['projects']}
    sources = [(name, language, source.encode(), {'kind': 'handwritten'})
               for name, language, source in fixture_cases()]
    sources.append(('many.py', 'Python', ''.join(f'def f{i}():\n    return {i}\n' for i in range(140)).encode(),
                    {'kind': 'generated_by_harness', 'purpose': '128-entry per-file retention cap'}))
    for project, path, language in [
            ('spring-framework', 'spring-core/src/main/java/org/springframework/util/StringUtils.java', 'Java'),
            ('roslyn', 'src/Compilers/CSharp/Portable/Syntax/CSharpSyntaxTree.cs', 'C#'),
            ('flask', 'src/flask/app.py', 'Python')]:
        pin = pins[project]
        source = subprocess.check_output(['git', '-C', str(args.corpus_root / project), 'show', pin['commit'] + ':' + path])
        require(0 < len(source) < 1024 * 1024, 'selected source exceeds bounded benchmark size')
        sources.append((Path(path).name, language, source,
                        {'kind': 'pinned_repository_blob', 'repository': pin['url'], 'commit': pin['commit'], 'path': path}))
    cases = [(Path(name).stem, [(name, language, data, provenance)]) for name, language, data, provenance in sources]
    cases.append(('mixed', sources[:3]))
    # Avoid colliding Java and C# fixture stems in evidence filenames.
    cases = [(f'{index:02d}-{name}', files) for index, (name, files) in enumerate(cases)]
    with tempfile.TemporaryDirectory(prefix='dircue-function-cost-') as temporary:
        for name, files in cases:
            directory = Path(temporary) / name
            directory.mkdir()
            for filename, _, data, _ in files:
                (directory / filename).write_bytes(data)
            common = ['analyze', 'structure', '--source', 'directory', '--json', '--files', '--workers', '1']
            variants = {
                'old_cli': ([str(binaries['baseline']), *common, '--structural-worker', str(binaries['baseline_worker']), str(directory)], None),
                'new_cli': ([str(binaries['candidate']), *common, '--structural-worker', str(binaries['worker']), str(directory)], None),
                'functions_cli': ([str(binaries['candidate']), *common, '--functions', '--structural-worker', str(binaries['worker']), str(directory)], None)}
            if len(files) == 1:
                filename, language, data, _ = files[0]
                request = {'path': filename, 'language': language, 'source': data.decode('utf-8'), 'mode': 'combined'}
                payload = (json.dumps(request) + '\n').encode()
                variants.update(old_worker=([str(binaries['baseline_worker'])], payload),
                                new_worker=([str(binaries['worker'])], payload),
                                functions_worker=([str(binaries['worker'])], (json.dumps({**request, 'functions': True}) + '\n').encode()))
            expected, samples = {}, {v: [] for v in variants}
            for variant, (command, payload) in variants.items():
                expected[variant], _ = measure(command, payload)
            require(expected['old_cli'] == expected['new_cli'], f'{name}: default CLI changed')
            base = json.loads(expected['new_cli'])
            enriched = json.loads(expected['functions_cli'])
            functions = enriched['structure'].pop('functions')
            # Opt-in omissions qualify the parent report as partial. Only this
            # documented status change is allowed; default comparisons stay raw.
            if functions['status'] == 'partial':
                require(base['structure']['status'] == 'partial' or functions['omitted_spaces'] > 0 or
                        functions['invalid_span_spaces'] > 0 or functions['partial_files'] > 0,
                        f'{name}: parent qualification lacks partial evidence')
                base['structure']['status'] = 'partial'
            require(enriched['structure'] == base['structure'], f'{name}: existing structure evidence changed')
            require(enriched['structure']['parse_count'] == len(files), f'{name}: unexpected parsing count')
            source_map = {f[0]: f[2] for f in files}
            for entry in functions['entries']:
                require(entry['source_sha256'] == sha(source_map[entry['path']]), 'CLI source digest mismatch')
            require(functions['total_spaces'] == len(functions['entries']) + functions['omitted_spaces'] +
                    functions['invalid_span_spaces'], 'CLI population invariant')
            if name.endswith('-many'):
                require(functions['total_spaces'] == 140 and functions['omitted_spaces'] == 12 and
                        functions['status'] == 'partial', 'cap did not qualify report')
            if len(files) == 1:
                require(without_timings(expected['old_worker']) == without_timings(expected['new_worker']), 'default worker changed')
                direct = json.loads(expected['functions_worker'])
                validate_functions(direct['functions'], files[0][2].decode())
                require([{k: v for k, v in entry.items() if k not in {'path', 'language', 'source_sha256'}}
                         for entry in functions['entries']] == direct['functions']['entries'], 'CLI/provider entries differ')
            order = list(variants)
            for repeat in range(3):
                for variant in order[repeat:] + order[:repeat]:
                    output, sample = measure(*variants[variant])
                    require((without_timings(output) == without_timings(expected[variant])) if variant.endswith('worker')
                            else output == expected[variant], f'{name}: nondeterministic report {variant}')
                    samples[variant].append({'repetition': repeat + 1, **sample})
            outputs = {}
            for variant, raw in expected.items():
                filename = name + '-' + variant + '.json.gz'
                compressed = gzip.compress(raw, mtime=0)
                (args.output / filename).write_bytes(compressed)
                outputs[variant] = {'path': filename, 'sha256': sha(compressed), 'uncompressed_sha256': sha(raw)}
            receipt['cases'].append({'name': name, 'files': [{'path': filename, 'language': language, 'bytes': len(data),
                                                           'sha256': sha(data), **provenance}
                                                          for filename, language, data, provenance in files],
                                     'commands': {v: command for v, (command, _) in variants.items()},
                                     'samples': samples, 'outputs': outputs,
                                     'function_population': {k: functions[k] for k in ('status', 'total_spaces', 'omitted_spaces', 'invalid_span_spaces')},
                                     'summary': {variant: {'median_seconds': statistics.median(s['seconds'] for s in values),
                                                           'max_rss_bytes': max(s['max_rss_bytes'] for s in values),
                                                           'stdout_bytes': values[0]['stdout_bytes']}
                                                 for variant, values in samples.items()}})
            print(name, 'passed', flush=True)
    require(source_snapshot(args.candidate_source.resolve()) == snapshot, 'candidate source changed during measurement')
    require(all(file_sha(ROOT / path) == digest for path, digest in worker_snapshot.items()), 'worker source changed')
    require({name: file_sha(path) for name, path in binaries.items()} == hashes, 'tested binary changed')
    receipt.update(passed=True, completed_at_utc=datetime.now(timezone.utc).isoformat())
    (args.output / 'receipt.json').write_text(json.dumps(receipt, indent=2, allow_nan=False) + '\n')
    print('All measurements and output checks passed.')


if __name__ == '__main__':
    main()
