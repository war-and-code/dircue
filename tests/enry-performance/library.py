#!/usr/bin/env python3
"""Paired identical-API library runs, with cold first pass and warm costs separate."""
import argparse
import hashlib
import importlib.util
import json
import math
import os
from pathlib import Path
import platform
import statistics

HERE = Path(__file__).resolve().parent
spec = importlib.util.spec_from_file_location('enry_cli_measurement', HERE/'compare.py')
shared = importlib.util.module_from_spec(spec)
spec.loader.exec_module(shared)


def main():
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument('--builds', type=Path, required=True)
    p.add_argument('--manifest', type=Path, required=True)
    p.add_argument('--output', type=Path, required=True)
    p.add_argument('--workers', type=int, default=1)
    p.add_argument('--api', choices=['language', 'classifier'], default='language')
    p.add_argument('--iterations', type=int, default=0, help='0 calibrates identical passes to at least 0.5 s warm cost on each side')
    p.add_argument('--runs', type=int, default=20, help='0 captures correctness pilots only, with no calibration or warmups')
    p.add_argument('--warmup', type=int, default=3)
    p.add_argument('--timeout', type=int, default=1800)
    p.add_argument('--max-runs', type=int, default=200)
    p.add_argument('--profile-directory', type=Path, help='Separate profiling-only run, never a timing result')
    a = p.parse_args()
    os.environ.update(GOGC='100', GOMEMLIMIT='off')
    os.environ.pop('GODEBUG', None)
    if platform.system() != 'Linux' or a.output.exists() or a.workers < 1 or a.runs < 0 or a.max_runs < max(a.runs, 1) or a.warmup < 0 or a.iterations < 0 or a.timeout <= 0 or (a.runs == 0 and a.profile_directory):
        p.error('Linux, valid counts, and fresh output required')
    if os.getenv('DIRCUE_PROFILE_CPU') or os.getenv('DIRCUE_PROFILE_HEAP'):
        p.error('unset inherited profiling variables; use --profile-directory explicitly')
    receipt = json.loads((a.builds/'build-receipt.json').read_text())
    if not receipt.get('complete') or bool(a.profile_directory) != receipt['profile']:
        p.error('receipt must match profile/nonprofile purpose')
    manifest = json.loads(a.manifest.read_text())
    file_paths = [f['path'] for f in manifest['files']]
    if not file_paths or len(set(file_paths)) != len(file_paths):
        p.error('manifest paths must be nonempty and unique')
    names = ['library-official', 'library-maintained']
    for name in names:
        if shared.sha(a.builds/name) != receipt['builds'][name]['sha256']:
            p.error('binary receipt mismatch')
    a.output.parent.mkdir(parents=True, exist_ok=True)
    artifacts = a.output.parent/(a.output.stem+'-artifacts')
    artifacts.mkdir()
    report = {'complete': False, 'profiled': bool(a.profile_directory), 'manifest_sha256': shared.sha(a.manifest),
              'build_receipt': receipt, 'harness_sha256': shared.sha(Path(__file__)),
              'measurement_helper_sha256': shared.sha(shared.HELPER), 'shared_runner_sha256': shared.sha(HERE/'compare.py'),
              'api': a.api, 'workers': a.workers, 'prefix': manifest['prefix'],
              'environment': {'platform': platform.platform(), 'cpu_count': os.cpu_count(), 'gomaxprocs': os.getenv('GOMAXPROCS')},
              'results': {name: [] for name in names}, 'correctness': {}, 'methodology': {
                  'preload': 'outside internal timer, within external process time',
                  'first_pass': 'includes lazy initialization; process/package init is external only',
                  'warm': 'same complete corpus passes and worker count; worker dispatch overhead included',
                  'labels': 'preserved before comparisons; differing classifier labels do not prevent measurement'}}
    for key, path in [('cpu_max', '/sys/fs/cgroup/cpu.max'), ('memory_max', '/sys/fs/cgroup/memory.max'),
                      ('cpuinfo', '/proc/cpuinfo'), ('meminfo', '/proc/meminfo'), ('mounts', '/proc/mounts')]:
        report['environment'][key] = Path(path).read_text() if Path(path).exists() else None
    report['environment']['runtime_controls'] = {key: os.getenv(key) for key in ['GOGC', 'GOMEMLIMIT', 'GODEBUG', 'GOMAXPROCS']}
    report['methodology'].update(runs=a.runs, max_runs=a.max_runs, warmup=a.warmup, minimum_warm_seconds=10)
    def execute(name, passes, label):
        command = [str(a.builds/name), '--manifest', str(a.manifest), '--prefix', str(manifest['prefix']),
                   '--iterations', str(passes), '--workers', str(a.workers), '--api', a.api]
        result = shared.measurement.execute(command, a.timeout, True)
        if result['exit_code'] or result['timed_out']:
            (artifacts/(name+'-failure.json')).write_text(json.dumps(result, indent=2)+'\n')
            raise RuntimeError(f'{name} failed: {result["stderr"][:1000]}')
        value = json.loads(result['stdout'])
        if (value['manifest_sha256'] != report['manifest_sha256'] or value['iterations'] != passes or
            value['workers'] != a.workers or value['api'] != a.api or value['prefix'] != manifest['prefix'] or
            value['calls'] != passes*len(file_paths) or value['files'] != len(file_paths) or
            set(value['labels']) != set(file_paths)):
            raise RuntimeError('driver input population or measured work differs')
        checksum = hashlib.sha256()
        for path in file_paths:
            label_value = value['labels'][path]
            if not isinstance(label_value, str):
                raise RuntimeError('invalid language label')
            path_bytes, label_bytes = path.encode(), label_value.encode()
            checksum.update(str(len(path_bytes)).encode()+b':'+path_bytes+str(len(label_bytes)).encode()+b':'+label_bytes)
        if checksum.hexdigest() != value['checksum']:
            raise RuntimeError('driver label checksum is inconsistent')
        if any(not math.isfinite(value[key]) or value[key] <= 0 for key in ['warm_seconds', 'ns_per_call']) or value['allocated_bytes'] < 0 or value['allocations'] < 0:
            raise RuntimeError('invalid driver metrics')
        if label:
            path = artifacts/(name+'-'+label+'.json')
            path.write_text(json.dumps(result, indent=2)+'\n')
            report['correctness'][name] = {'artifact': path.name, 'sha256': shared.sha(path),
                                           'labels': value['labels'], 'checksum': value['checksum']}
        elif value['checksum'] != report['correctness'][name]['checksum']:
            raise RuntimeError('timed output differs from correctness baseline')
        del value['labels']
        value['process'] = {key: result[key] for key in ['seconds', 'user_seconds', 'system_seconds', 'max_rss_kib']}
        return value
    try:
        pilots = {name: execute(name, 1, 'correctness') for name in names}
        expected = report['correctness'][names[0]]['labels']
        actual = report['correctness'][names[1]]['labels']
        report['label_differences'] = [{'path': path, 'official': expected.get(path), 'maintained': actual.get(path)}
                                     for path in sorted(set(expected)|set(actual)) if expected.get(path) != actual.get(path)]
        for name in names:
            del report['correctness'][name]['labels']
        passes = (a.iterations or max(1, max(math.ceil(.5/max(v['warm_seconds'], 1e-9)) for v in pilots.values()))) if a.runs else 1
        report['iterations'] = passes
        if a.profile_directory:
            a.profile_directory.mkdir(parents=True, exist_ok=True)
            for name in names:
                os.environ['DIRCUE_PROFILE_CPU'] = str((a.profile_directory/(name+'-cpu.pprof')).resolve())
                os.environ['DIRCUE_PROFILE_HEAP'] = str((a.profile_directory/(name+'-heap.pprof')).resolve())
                report['results'][name].append(execute(name, passes, None))
            os.environ.pop('DIRCUE_PROFILE_CPU', None)
            os.environ.pop('DIRCUE_PROFILE_HEAP', None)
        elif a.runs:
            for i in range(a.warmup):
                for name in names[i%2:]+names[:i%2]:
                    execute(name, passes, None)
            i = 0
            while i < a.runs or (i < a.max_runs and any(sum(v['warm_seconds'] for v in values)<10 for values in report['results'].values())):
                for name in names[i%2:]+names[:i%2]:
                    report['results'][name].append(execute(name, passes, None))
                i += 1
            report['summary'] = {}
            for name, values in report['results'].items():
                warm = [{'seconds': v['warm_seconds'], 'max_rss_kib': v['process']['max_rss_kib']} for v in values]
                report['summary'][name] = {'warm': shared.measurement.summary(warm),
                    'median_ns_per_call': statistics.median(v['ns_per_call'] for v in values),
                    'median_bytes_per_call': statistics.median(v['allocated_bytes']/v['calls'] for v in values),
                    'median_allocations_per_call': statistics.median(v['allocations']/v['calls'] for v in values),
                    'median_first_pass_seconds': statistics.median(v['first_pass_seconds'] for v in values),
                    'process': shared.measurement.summary([v['process'] for v in values]),
                    'sample_floor_met': len(values)>=20 and a.warmup>=3 and sum(v['warm_seconds'] for v in values)>=10}
            left = [{'seconds': v['warm_seconds']} for v in report['results'][names[0]]]
            right = [{'seconds': v['warm_seconds']} for v in report['results'][names[1]]]
            report['warm_speedup'] = statistics.median(v['seconds'] for v in left)/statistics.median(v['seconds'] for v in right)
            report['paired_bootstrap_95_ci'] = shared.ratio_interval(left, right, 73191)
        else:
            report['timing_status'] = 'correctness-only'
        if any(shared.sha(a.builds/n) != receipt['builds'][n]['sha256'] for n in names):
            raise RuntimeError('binary changed during run')
        report['complete'] = True
    finally:
        a.output.write_text(json.dumps(report, indent=2)+'\n')


if __name__ == '__main__':
    main()
