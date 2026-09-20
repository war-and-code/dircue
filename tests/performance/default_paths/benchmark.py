#!/usr/bin/env python3
"""Compare ordinary language/project commands; macOS diagnostic, no new modules."""
import argparse
from datetime import datetime, timezone
import gzip
import hashlib
import importlib.util
import json
from pathlib import Path
import platform
import re
import statistics
import subprocess
import tempfile
import time

ROOT = Path(__file__).resolve().parents[3]
spec = importlib.util.spec_from_file_location('discovery_benchmark', ROOT / 'tests/discovery/benchmark.py')
discovery = importlib.util.module_from_spec(spec)
spec.loader.exec_module(discovery)
digest, inventory, run = discovery.digest, discovery.inventory, discovery.run
BASELINE = 'ff0d723411657a61dc4385c84fc011c1392ce7696066fc13f1c663b1c3967ade'
CANDIDATE = '20ea257d856d2805fadc8b0ce7488ff15768b3ac23f164f0d101cff332f24d8e'


def utc():
    return datetime.now(timezone.utc).isoformat()


def save(path, value):
    assert not path.exists(), f'refusing to overwrite {path}'
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_bytes(gzip.compress((json.dumps(value, indent=2) + '\n').encode(), mtime=0))


def prepare(args):
    baseline, candidate = args.baseline.resolve(), args.candidate.resolve()
    assert digest(baseline) == BASELINE and digest(candidate) == CANDIDATE
    provenance = json.loads(gzip.decompress(args.build_inputs.read_bytes()))
    assert provenance['candidate_sha256'] == CANDIDATE
    pins = {p['name']: p['commit'] for p in json.loads((ROOT / 'tests/performance/corpus.json').read_text())['projects']}
    cases = []
    for name, source in [('cobra', 'git'), ('spring-framework', 'git'), ('roslyn', 'directory'), ('roslyn', 'git')]:
        path = (args.corpus_root / name).resolve()
        assert run(['git', '-C', str(path), 'rev-parse', 'HEAD']) == pins[name]
        assert not run(['git', '-C', str(path), 'status', '--porcelain'])
        cases.append({'name': name + '-' + source, 'source': source, 'path': str(path),
                      'commit': pins[name], 'tree': run(['git', '-C', str(path), 'rev-parse', 'HEAD^{tree}']),
                      'git_object_counts': run(['git', '-C', str(path), 'count-objects', '-v'])})
    mixed = args.fixtures.resolve() / 'xml-and-dotnet'
    xml_files = sorted(mixed.glob('*.xml'))
    size = 16 * 1024 * 1024
    line = b'<entry timestamp="2026-01-01T00:00:00Z">example deterministic log record</entry>\n'
    inner = size - len(b'<logs>\n</logs>\n')
    xml_hash = hashlib.sha256(b'<logs>\n' + line * (inner // len(line)) + b' ' * (inner % len(line)) + b'</logs>\n').hexdigest()
    assert len(xml_files) == 128 and sum(p.stat().st_size for p in xml_files) == 2147483648
    assert all(digest(p) == xml_hash for p in xml_files)
    assert (mixed / 'App.csproj').is_file()
    cases.append({'name': 'xml-and-dotnet', 'path': str(mixed), 'source': 'directory',
                  'xml_files': 128, 'xml_bytes': 2147483648, 'xml_sha256': xml_hash})
    for case in cases:
        case['input'] = inventory(Path(case['path']), case['source'])
    data = {'prepared_at_utc': utc(), 'baseline': str(baseline), 'candidate': str(candidate),
            'baseline_sha256': BASELINE, 'candidate_sha256': CANDIDATE,
            'baseline_version': run([str(baseline), '--version']),
            'candidate_version': run([str(candidate), '--version']),
            'candidate_build_provenance': provenance,
            'provenance_artifact_sha256': digest(args.build_inputs),
            'platform': platform.platform(),
            'hardware': {key: run(['sysctl', '-n', key]) for key in ('machdep.cpu.brand_string', 'hw.logicalcpu', 'hw.memsize')},
            'go_version': run(['go', 'version']),
            'harness_sha256': digest(Path(__file__)),
            'inventory_helper_sha256': digest(ROOT / 'tests/discovery/benchmark.py'), 'cases': cases}
    assert data['baseline_version'] == data['candidate_version']
    save(args.output, data)
    print(args.output, flush=True)


def measure(command):
    # Separate time's diagnostics from the program's stderr, which must stay empty.
    with tempfile.TemporaryDirectory(prefix='dircue-time-') as tmp:
        usage = Path(tmp) / 'usage'
        started = time.perf_counter()
        result = subprocess.run(['/usr/bin/time', '-l', '-o', str(usage), *command], capture_output=True, timeout=180)
        seconds = time.perf_counter() - started
        raw = usage.read_bytes()
    assert result.returncode == 0 and result.stderr == b'', (command, result.returncode, result.stderr)
    match = re.search(rb'(\d+)\s+maximum resident set size', raw)
    assert match
    return result.stdout, {'seconds': seconds, 'peak_rss_bytes': int(match[1]), 'exit_code': result.returncode,
                           'stdout_sha256': hashlib.sha256(result.stdout).hexdigest(),
                           'stderr_sha256': hashlib.sha256(result.stderr).hexdigest(),
                           'time_diagnostics': raw.decode()}


def collect(args):
    assert args.repetitions >= 5
    assert not args.output.exists()
    data = json.loads(gzip.decompress(args.prepared.read_bytes()))
    assert digest(Path(__file__)) == data['harness_sha256']
    assert digest(ROOT / 'tests/discovery/benchmark.py') == data['inventory_helper_sha256']
    for key in ('baseline', 'candidate'):
        assert digest(Path(data[key])) == data[key + '_sha256']
    data['started_at_utc'] = utc()
    data['method'] = {'repetitions': args.repetitions, 'warmups': 1, 'workers': 8, 'tree_size': 1000000,
                      'order': 'Within each case, alternate old/new order every round and reverse language/project mode order every round.',
                      'wall_time': 'Python subprocess elapsed including time wrapper; excludes parsing, verification and artifact compression.',
                      'peak_rss': 'macOS time -l standalone CLI maximum resident bytes; excludes harness.',
                      'scope': 'Legacy JSON and analyze all --projects only. Rules, functions, discovery, graph, metrics and structure absent.',
                      'environment': args.environment_note,
                      'limitations': 'Warm-cache diagnostic on one host, five pairs per mode, no enforced resource limits; not a universal speed guarantee. Candidate version deliberately overridden to 0.3.0 for exact output comparison. Build manifest is the historical verified binary input identity, not a claim that the current worktree remains unchanged.'}
    for case in data['cases']:
        common = ['--json', '--source', case['source'], '--workers', '8', '--tree-size', '1000000', case['path']]
        commands = {f'{generation}_{mode}': [data[generation], *(['analyze', 'all', '--projects'] if mode == 'projects' else []), *common]
                    for mode in ('languages', 'projects') for generation in ('baseline', 'candidate')}
        expected, warmups, samples, execution = {}, {}, {key: [] for key in commands}, []
        for key, command in commands.items():
            expected[key], warmups[key] = measure(command)
        for mode in ('languages', 'projects'):
            assert expected['baseline_' + mode] == expected['candidate_' + mode], case['name'] + '/' + mode
        for index in range(args.repetitions):
            modes = ('languages', 'projects') if index % 2 == 0 else ('projects', 'languages')
            generations = ('baseline', 'candidate') if index % 2 == 0 else ('candidate', 'baseline')
            for mode in modes:
                for generation in generations:
                    key = generation + '_' + mode
                    payload, sample = measure(commands[key])
                    assert payload == expected[key], (case['name'], key, index)
                    sample['round'] = index + 1
                    samples[key].append(sample)
                    execution.append(key)
        case.update(commands=commands, warmups=warmups, samples=samples, execution_order=execution, artifacts={})
        case['summary'] = {key: {'median_seconds': statistics.median(s['seconds'] for s in values),
                                 'median_peak_rss_bytes': statistics.median(s['peak_rss_bytes'] for s in values),
                                 'max_peak_rss_bytes': max(s['peak_rss_bytes'] for s in values)} for key, values in samples.items()}
        case['comparisons'] = {}
        for mode in ('languages', 'projects'):
            old, new = case['summary']['baseline_' + mode], case['summary']['candidate_' + mode]
            case['comparisons'][mode] = {'median_time_change_percent': 100 * (new['median_seconds'] / old['median_seconds'] - 1),
                                         'median_rss_change_percent': 100 * (new['median_peak_rss_bytes'] / old['median_peak_rss_bytes'] - 1),
                                         'paired_time_change_percent': [100 * (n['seconds'] / o['seconds'] - 1) for o, n in zip(samples['baseline_' + mode], samples['candidate_' + mode])]}
            artifact = args.output.parent / f'{case["name"]}-{mode}.json.gz'
            assert not artifact.exists()
            artifact.parent.mkdir(parents=True, exist_ok=True)
            artifact.write_bytes(gzip.compress(expected['baseline_' + mode], mtime=0))
            case['artifacts'][artifact.name] = digest(artifact)
        print(case['name'], case['comparisons'], flush=True)
    data['timings_finished_at_utc'] = utc()
    print('TIMINGS FINISHED; verifying corpus identities', flush=True)
    for key in ('baseline', 'candidate'):
        assert digest(Path(data[key])) == data[key + '_sha256']
    for case in data['cases']:
        assert inventory(Path(case['path']), case['source']) == case['input'], 'corpus changed: ' + case['name']
        if 'commit' in case:
            assert run(['git', '-C', case['path'], 'rev-parse', 'HEAD']) == case['commit']
            assert not run(['git', '-C', case['path'], 'status', '--porcelain'])
    data.update(finished_at_utc=utc(), passed=True)
    save(args.output, data)
    print(args.output, flush=True)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    sub = parser.add_subparsers(dest='command', required=True)
    prep = sub.add_parser('prepare')
    for name in ('baseline', 'candidate', 'build-inputs', 'fixtures', 'corpus-root', 'output'):
        prep.add_argument('--' + name, type=Path, required=True)
    bench = sub.add_parser('measure')
    for name in ('prepared', 'output'):
        bench.add_argument('--' + name, type=Path, required=True)
    bench.add_argument('--repetitions', type=int, default=5)
    bench.add_argument('--environment-note', required=True)
    args = parser.parse_args()
    assert platform.system() == 'Darwin'
    (prepare if args.command == 'prepare' else collect)(args)


if __name__ == '__main__':
    main()
