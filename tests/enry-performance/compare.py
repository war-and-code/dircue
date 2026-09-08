#!/usr/bin/env python3
"""CLI timing with explicit semantic differences; no claim of equal work by default."""
# Result keys retain the original evidence format across the project rename.
import argparse
import hashlib
import importlib.util
import json
import math
import os
from pathlib import Path
import platform
import random
import stat
import statistics
import subprocess
import time

HERE = Path(__file__).resolve().parent
HELPER = HERE.parent/'stress'/'compare.py'
spec = importlib.util.spec_from_file_location('stress_measurement', HELPER)
measurement = importlib.util.module_from_spec(spec)
spec.loader.exec_module(measurement)


def sha(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def git(root, *args):
    return subprocess.check_output(['git', '-C', str(root), *args])


def inventory(root):
    entries = git(root, 'ls-tree', '-r', '-l', '-z', 'HEAD')
    files = {}
    for entry in entries.split(b'\0'):
        if not entry:
            continue
        meta, path = entry.split(b'\t', 1)
        mode, kind, oid, size = meta.split()
        if kind == b'blob' and mode in (b'100644', b'100755'):
            files[path.decode()] = {'bytes': int(size), 'git_blob_oid': oid.decode(), 'mode': mode.decode()}
    return files


def verify_materialized(root, records, expected):
    """Check the exact directory bytes independently against pinned Git objects."""
    if root.is_symlink() or not root.is_dir():
        raise RuntimeError('materialized project must be a regular directory')
    files = {}
    for record in records:
        name = record['path']
        if (not isinstance(name, str) or not name or name.startswith('/') or
                any(part in ('', '.', '..', '.git') for part in name.split('/')) or name in files):
            raise RuntimeError('unsafe or duplicate materialized path')
        files[name] = record
    if set(files) != set(expected):
        raise RuntimeError('materialized file population differs from the pinned Git tree')
    observed = set()
    def walk_error(error):
        raise error
    for directory, directories, names in os.walk(root, followlinks=False, onerror=walk_error):
        for name in directories:
            if (Path(directory)/name).is_symlink():
                raise RuntimeError('materialized view contains a directory symlink')
        for name in names:
            path = Path(directory)/name
            if not stat.S_ISREG(path.lstat().st_mode):
                raise RuntimeError('materialized view contains a nonregular file')
            observed.add(path.relative_to(root).as_posix())
    if observed != set(files):
        raise RuntimeError('materialized view contains missing or extra files')
    for name, record in files.items():
        pinned = expected[name]
        if any(record[field] != pinned[field] for field in ('bytes', 'git_blob_oid', 'mode')):
            raise RuntimeError('materialization provenance differs from Git: '+name)
        path = root/name
        info = path.stat()
        executable = bool(info.st_mode & 0o111)
        if info.st_size != pinned['bytes'] or executable != (pinned['mode'] == '100755'):
            raise RuntimeError('materialized size or executable mode differs: '+name)
        digest = hashlib.sha256()
        oid = hashlib.sha1(f"blob {pinned['bytes']}\0".encode())
        count = 0
        with path.open('rb') as stream:
            for block in iter(lambda: stream.read(1024*1024), b''):
                count += len(block)
                digest.update(block)
                oid.update(block)
        if count != pinned['bytes'] or digest.hexdigest() != record['sha256'] or oid.hexdigest() != pinned['git_blob_oid']:
            raise RuntimeError('materialized contents differ from pinned bytes: '+name)


def cases(args):
    if args.corpus:
        config = json.loads(args.corpus.read_text())
        if len(config['projects']) != 11:
            raise RuntimeError('primary corpus must contain all eleven pinned projects')
        materialization = json.loads((args.corpus_flat_root/'provenance.json').read_text())
        views = {view['name']: view for view in materialization['projects']}
        if (materialization.get('schema_version') != '1.0.0' or not materialization.get('complete') or
                len(materialization['projects']) != 11 or len(views) != 11 or
                set(views) != {project['name'] for project in config['projects']} or
                materialization['materializer_sha256'] != sha(HERE/'materialize.py')):
            raise RuntimeError('complete current materialization of all pinned projects is required')
        for project in config['projects']:
            if args.case and 'public-'+project['name'] not in args.case:
                continue
            root = args.corpus_root/project['name']
            revision = git(root, 'rev-parse', 'HEAD').decode().strip()
            if revision != project['commit']:
                raise RuntimeError('public corpus revision drift')
            if git(root, 'status', '--porcelain', '--untracked-files=all', '--ignored=matching').strip():
                raise RuntimeError('public checkout must be clean, including ignored files')
            view = views[project['name']]
            if view['commit'] != revision or view['tree'] != git(root, 'rev-parse', 'HEAD^{tree}').decode().strip():
                raise RuntimeError('materialized revision or tree differs from corpus pin')
            flat = args.corpus_flat_root/project['name']
            files = inventory(root)
            verify_materialized(flat, view['files'], files)
            yield {'name': 'public-'+project['name'], 'kind': 'public', 'root': str(flat),
                   'git': str(root), 'revision': revision, 'files': files, 'limit': None,
                   'population': 'All regular Git blobs, materialized without checkout filters; nonregular entries excluded.',
                   'excluded_nonregular_entries': view['excluded']}
    if args.stress:
        manifest = json.loads((args.stress/'manifest.json').read_text())
        if manifest.get('profile') != 'acceptance' or not manifest.get('finished_at_utc'):
            raise RuntimeError('stress acceptance fixture is incomplete')
        count = 0
        verified = {}
        for fixture in manifest['fixtures']:
            for variant in fixture['variants']:
                files = None
                for limit in [None]+fixture.get('tree_size_limits', []):
                    count += 1
                    name = fixture['name']+'-'+variant['name']+(f'-limit{limit}' if limit is not None else '')
                    if args.case and 'stress-'+name not in args.case:
                        continue
                    if files is None:
                        files = measurement.verify_inputs(args.stress, fixture, variant, verified)
                    yield {'name': 'stress-'+name, 'kind': 'stress', 'root': str(args.stress/variant['flat']),
                           'git': str(args.stress/fixture['git']), 'revision': variant['revision'],
                           'files': files, 'limit': limit}
        if count != 14:
            raise RuntimeError(f'expected fourteen stress cells; found {count}')


def pathmap(value, official):
    result = {}
    if not isinstance(value, dict):
        raise ValueError('breakdown JSON must be an object')
    for language, details in value.items():
        paths = details if official else details['files']
        if not isinstance(paths, list):
            raise ValueError('file list missing')
        for path in paths:
            if not isinstance(path, str) or path in result:
                raise ValueError('invalid or duplicate file path')
            result[path] = language
    return result


def difference(expected, actual, files):
    missing = sorted(set(expected)-set(actual))
    extra = sorted(set(actual)-set(expected))
    changed = [{'path': p, 'reference': expected[p], 'actual': actual[p]}
               for p in sorted(set(actual)&set(expected)) if actual[p] != expected[p]]
    unknown = sorted(set(actual)-set(files))
    sizes = {}
    for path, language in actual.items():
        if path in files:
            sizes[language] = sizes.get(language, 0)+files[path]['bytes']
    return {'exact_path_labels': not missing and not extra and not changed,
            'missing': missing, 'extra': extra, 'reclassified': changed, 'unaccounted_paths': unknown,
            'files': len(actual), 'language_bytes': sizes, 'total_bytes': sum(sizes.values())}


def ratio_interval(left, right, seed):
    if not left or len(left) != len(right) or any(not math.isfinite(s['seconds']) or s['seconds'] <= 0 for s in left+right):
        raise ValueError('paired bootstrap requires finite positive equal-length samples')
    rng = random.Random(seed)
    ratios = []
    for _ in range(2000):
        indices = [rng.randrange(len(left)) for _ in left]
        ratios.append(statistics.median(left[i]['seconds'] for i in indices)/
                      statistics.median(right[i]['seconds'] for i in indices))
    ratios.sort()
    return [ratios[49], ratios[1949]]


def main():
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument('--builds', type=Path, required=True)
    p.add_argument('--candidate', type=Path, required=True)
    p.add_argument('--candidate-receipt', type=Path, required=True)
    p.add_argument('--corpus', type=Path)
    p.add_argument('--corpus-root', type=Path)
    p.add_argument('--corpus-flat-root', type=Path, help='Verified raw-Git directory views from materialize.py')
    p.add_argument('--stress', type=Path)
    p.add_argument('--output', type=Path, required=True)
    p.add_argument('--case', action='append', default=[])
    p.add_argument('--runs', type=int, default=20, help='0 for correctness only')
    p.add_argument('--warmup', type=int, default=3)
    p.add_argument('--minimum-seconds', type=float, default=10)
    p.add_argument('--max-runs', type=int, default=2000)
    p.add_argument('--timeout', type=int, default=1800)
    p.add_argument('--seed', type=int, default=73191)
    a = p.parse_args()
    os.environ.update(GOGC='100', GOMEMLIMIT='off')
    os.environ.pop('GODEBUG', None)
    if platform.system() != 'Linux' or not (a.corpus or a.stress):
        p.error('Linux and at least one declared workload matrix are required')
    if bool(a.corpus) != bool(a.corpus_root) or bool(a.corpus) != bool(a.corpus_flat_root) or a.runs < 0 or a.warmup < 0 or a.timeout <= 0 or a.max_runs < max(a.runs, 1) or not math.isfinite(a.minimum_seconds) or a.minimum_seconds < 0:
        p.error('invalid workload or measurement arguments')
    if a.output.exists():
        p.error('refuse to overwrite existing evidence')
    receipt = json.loads((a.builds/'build-receipt.json').read_text())
    candidate_receipt = json.loads(a.candidate_receipt.read_text())
    candidate_sha = sha(a.candidate)
    if not receipt.get('complete') or receipt['profile'] or candidate_receipt['candidate_sha256'] != candidate_sha:
        p.error('release build receipts required and must match')
    def build_settings(info):
        return dict(line.strip().split('\t', 1)[1].split('=', 1)
                    for line in info.splitlines() if line.strip().startswith('build\t') and '=' in line)
    candidate_info = candidate_receipt['candidate_go_build_info']
    candidate_settings = build_settings(candidate_info)
    if receipt['pins']['go_version'] not in candidate_info.splitlines()[0].split():
        p.error('candidate toolchain differs from upstream benchmark toolchain')
    for name in ('enry-official', 'enry-refreshed'):
        if sha(a.builds/name) != receipt['builds'][name]['sha256']:
            p.error('upstream binary receipt mismatch')
        upstream_settings = build_settings(receipt['builds'][name]['binary_modules'])
        for setting in ('CGO_ENABLED', 'GOOS', 'GOARCH', 'GOARM64', 'GOAMD64', 'GOEXPERIMENT', '-gcflags', '-trimpath', '-compiler', '-buildmode'):
            if candidate_settings.get(setting, '') != upstream_settings.get(setting, ''):
                p.error('candidate/upstream compiler or architecture setting differs: '+setting)
    a.output.parent.mkdir(parents=True, exist_ok=True)
    artifacts = a.output.parent/(a.output.stem+'-artifacts')
    artifacts.mkdir()
    def optional(path):
        return Path(path).read_text() if Path(path).exists() else None
    report = {'schema_version': '1.0.0', 'complete': False, 'build_receipt': receipt,
              'candidate_receipt': candidate_receipt, 'candidate_sha256': candidate_sha,
              'harness_sha256': sha(Path(__file__)), 'measurement_helper_sha256': sha(HELPER),
              'corpus_sha256': sha(a.corpus) if a.corpus else None,
              'corpus_materialization_sha256': sha(a.corpus_flat_root/'provenance.json') if a.corpus else None,
              'stress_manifest_sha256': sha(a.stress/'manifest.json') if a.stress else None,
              'started_at_utc': time.strftime('%Y-%m-%dT%H:%M:%SZ', time.gmtime()),
              'methodology': {'runs': a.runs, 'warmup': a.warmup, 'minimum_seconds': a.minimum_seconds,
                              'max_runs': a.max_runs, 'seed': a.seed, 'cache': 'warm', 'mode': 'JSON breakdown',
                              'scope': 'Default CLIs differ in selection and output work. Matched window does not imply matched policy.'},
              'environment': {'platform': platform.platform(), 'cpu_count': os.cpu_count(),
                              'cpu_max': optional('/sys/fs/cgroup/cpu.max'), 'memory_max': optional('/sys/fs/cgroup/memory.max'),
                              'cpuinfo': optional('/proc/cpuinfo'), 'meminfo': optional('/proc/meminfo'),
                              'mounts': optional('/proc/mounts'), 'gomaxprocs': os.getenv('GOMAXPROCS')}, 'cases': []}
    report['environment']['runtime_controls'] = {key: os.getenv(key) for key in ['GOGC', 'GOMEMLIMIT', 'GODEBUG', 'GOMAXPROCS']}
    rng = random.Random(a.seed)
    def save():
        a.output.write_text(json.dumps(report, indent=2)+'\n')
    failed = False
    seen = set()
    try:
        for case in cases(a):
            if a.case and case['name'] not in a.case:
                continue
            seen.add(case['name'])
            flags = ['--json', '--breakdown']+([f"--tree-size={case['limit']}"] if case['limit'] is not None else [])
            commands = {
                'enry-official': [str(a.builds/'enry-official'), '-json', '-breakdown', case['root']],
                'enry-refreshed': [str(a.builds/'enry-refreshed'), '-json', '-breakdown', case['root']],
                'enry-refreshed-128k': [str(a.builds/'enry-refreshed'), '-limit', '128', '-json', '-breakdown', case['root']],
                'auragaze-directory': [str(a.candidate), '--source', 'directory', *flags, case['root']]}
            oracle_command = ['github-linguist', *flags, '--rev', case['revision'], case['git']]
            item = {key: value for key, value in case.items() if key != 'files'}
            item.update(commands=commands, source_files=len(case['files']), source_bytes=sum(f['bytes'] for f in case['files'].values()), correctness={}, timings={})
            report['cases'].append(item)
            def capture(name, command):
                result = measurement.execute(command, a.timeout, True)
                path = artifacts/(case['name']+'-'+name+'.json')
                path.write_text(json.dumps(result, indent=2)+'\n')
                item['correctness'][name] = {'artifact': path.name, 'sha256': sha(path),
                    'exit_code': result['exit_code'], 'timed_out': result['timed_out'],
                    'stdout_sha256': hashlib.sha256(result['stdout'].encode()).hexdigest()}
                if result['exit_code'] or result['timed_out']:
                    raise RuntimeError(f'{name} failed; retained output contains diagnosis')
                return json.loads(result['stdout'])
            def timed(name):
                result = measurement.execute(commands[name], a.timeout, True)
                digest = hashlib.sha256(result['stdout'].encode()).hexdigest()
                if result['exit_code'] or result['timed_out'] or digest != item['correctness'][name]['stdout_sha256']:
                    path = artifacts/(case['name']+'-'+name+'-timing-failure.json')
                    path.write_text(json.dumps(result, indent=2)+'\n')
                    raise RuntimeError(f'{name}: timed status/output differs from correctness baseline; see {path.name}')
                value = {key: result[key] for key in ['seconds', 'user_seconds', 'system_seconds', 'max_rss_kib', 'stderr_bytes']}
                if not math.isfinite(value['seconds']) or value['seconds'] <= 0 or not isinstance(value['max_rss_kib'], int) or value['max_rss_kib'] <= 0:
                    raise RuntimeError('invalid child metrics')
                value['stdout_sha256'] = digest
                return value
            try:
                oracle_json = capture('linguist', oracle_command)
                measurement.assert_accounting(oracle_json, case['files'], [])
                oracle = pathmap(oracle_json, False)
                candidate_contract = False
                for name, command in commands.items():
                    parsed = capture(name, command)
                    if name == 'auragaze-directory':
                        measurement.assert_accounting(parsed, case['files'], [])
                        candidate_contract = measurement.normalized(parsed) == measurement.normalized(oracle_json)
                    item['correctness'][name]['difference'] = difference(oracle, pathmap(parsed, name.startswith('enry')), case['files'])
                item['candidate_matches_ruby_contract'] = candidate_contract
                if not candidate_contract:
                    failed = True
                effective_limit = case['limit'] if case['limit'] is not None else 100000
                item['advisory'] = len(case['files']) >= effective_limit or not oracle
                if item['advisory']:
                    item['timing_status'] = 'unmeasured-advisory'
                    item['timing_reason'] = 'No matched tree cutoff in Enry or empty reference result; retain diagnostic resources only.'
                elif not a.runs:
                    item['timing_status'] = 'correctness-only'
                if a.runs and not item['advisory']:
                    item['timing_status'] = 'measured'
                    samples = {name: [] for name in commands}
                    base_order = list(commands)
                    rng.shuffle(base_order)
                    for i in range(a.warmup):
                        for name in base_order[i % len(base_order):]+base_order[:i % len(base_order)]:
                            timed(name)
                    i = 0
                    while i < a.runs or (i < a.max_runs and any(sum(s['seconds'] for s in values) < a.minimum_seconds for values in samples.values())):
                        for name in base_order[i % len(base_order):]+base_order[:i % len(base_order)]:
                            samples[name].append(timed(name))
                        i += 1
                    for name, values in samples.items():
                        item['timings'][name] = {'samples': values, 'summary': measurement.summary(values)}
                    candidate = samples['auragaze-directory']
                    item['comparisons'] = {}
                    for name in commands:
                        if name == 'auragaze-directory':
                            continue
                        ratio = statistics.median(s['seconds'] for s in samples[name])/statistics.median(s['seconds'] for s in candidate)
                        ci = ratio_interval(samples[name], candidate, a.seed)
                        p95ratio = measurement.summary(samples[name])['p95_seconds']/measurement.summary(candidate)['p95_seconds']
                        adequate = i >= 20 and a.warmup >= 3 and all(sum(s['seconds'] for s in v) >= max(10, a.minimum_seconds) for v in samples.values())
                        verdict = 'faster' if ratio >= 1.10 and ci[0] > 1 and p95ratio >= 1 else 'slower' if ratio <= 1/1.10 and ci[1] < 1 else 'inconclusive'
                        item['comparisons'][name] = {'median_speedup': ratio, 'paired_bootstrap_95_ci': ci,
                            'p95_speedup': p95ratio, 'sample_floor_met': adequate,
                            'verdict': 'invalid-candidate-correctness' if not candidate_contract else verdict if adequate and not item['advisory'] else 'advisory',
                            'equal_path_labels': item['correctness'][name]['difference']['exact_path_labels'] and item['correctness']['auragaze-directory']['difference']['exact_path_labels']}
            except Exception as exc:
                item['error'] = str(exc)
                failed = True
            print(case['name'], 'ERROR' if item.get('error') else 'recorded', flush=True)
            save()
        if set(a.case)-seen:
            raise RuntimeError('unknown selected case')
        report['partial_selection'] = bool(a.case)
        if candidate_sha != sha(a.candidate) or any(sha(a.builds/n) != receipt['builds'][n]['sha256'] for n in ('enry-official', 'enry-refreshed')):
            raise RuntimeError('measured binary changed during run')
        report['complete'] = not failed
    finally:
        save()
    return int(failed)


if __name__ == '__main__':
    raise SystemExit(main())
