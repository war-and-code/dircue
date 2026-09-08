#!/usr/bin/env python3
"""Run an existing scanner.test inside one Linux container; never build fixtures.

Run baseline first, then separate cpu/mem/mutex stages using its receipt. Source
volumes must already be independently verified and mounted read-only. This
runner validates receipt identity and repeatability, not the language oracle.
"""
import argparse
import hashlib
import json
import math
import os
from pathlib import Path
import platform
import random
import signal
import statistics
import subprocess
import threading
import time

HERE = Path(__file__).resolve().parent
SCENARIOS = {
    'ripgrep-directory-w1': ('ripgrep', 'directory', 1),
    'jq-directory-w1': ('jq', 'directory', 1),
    'roslyn-git-w5': ('roslyn', 'git', 5),
    'roslyn-git-w1': ('roslyn', 'git', 1),
    'spring-framework-git-w1': ('spring-framework', 'git', 1),
    'spring-framework-git-w5': ('spring-framework', 'git', 5),
}


def sha(path):
    digest = hashlib.sha256()
    with path.open('rb') as stream:
        for block in iter(lambda: stream.read(1024 * 1024), b''):
            digest.update(block)
    return digest.hexdigest()


def write(path, value):
    with path.open('x') as stream:
        json.dump(value, stream, indent=2)
        stream.write('\n')


def utc_now():
    return time.strftime('%Y-%m-%dT%H:%M:%SZ', time.gmtime())


def distribution(values):
    values = sorted(values)
    mean = statistics.mean(values)
    return {'count': len(values), 'median': statistics.median(values),
            'p95': values[math.ceil(.95 * len(values)) - 1],
            'min': values[0], 'max': values[-1], 'total': sum(values),
            'coefficient_of_variation': statistics.pstdev(values) / mean if mean else None}


def execute(command, env, output, timeout):
    with (output / 'stdout.txt').open('xb') as stdout, (output / 'stderr.txt').open('xb') as stderr:
        started = time.perf_counter()
        process = subprocess.Popen(command, env=env, stdout=stdout, stderr=stderr, start_new_session=True)
        expired = threading.Event()
        def kill():
            expired.set()
            try:
                os.killpg(process.pid, signal.SIGKILL)
            except ProcessLookupError:
                pass
        watchdog = threading.Timer(timeout, kill)
        watchdog.daemon = True
        watchdog.start()
        try:
            _, status, _usage = os.wait4(process.pid, 0)
            elapsed = time.perf_counter() - started
            process.returncode = os.waitstatus_to_exitcode(status)
        except BaseException:
            kill()
            process.wait()
            raise
        finally:
            watchdog.cancel()
            watchdog.join()
        if expired.is_set():
            raise TimeoutError(f'process exceeded {timeout} seconds; inspect {output}')
        if process.returncode:
            raise RuntimeError(f'process exited {process.returncode}; inspect {output}')
        return elapsed


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    for name in ('binary', 'output', 'git-root', 'flat-root', 'git-receipt',
                 'flat-receipt', 'build-receipt', 'host-receipt'):
        parser.add_argument('--' + name, type=Path, required=True)
    parser.add_argument('--pins', type=Path, default=HERE.parent / 'performance' / 'corpus.json')
    parser.add_argument('--stage', choices=('baseline', 'cpu', 'mem', 'mutex'), default='baseline')
    parser.add_argument('--baseline', type=Path, help='Complete baseline result.json required for profile stages')
    parser.add_argument('--scenario', choices=tuple(SCENARIOS), action='append')
    parser.add_argument('--runs', type=int, default=20)
    parser.add_argument('--warmups', type=int, default=3)
    parser.add_argument('--seed', type=int, default=20260907)
    parser.add_argument('--timeout', type=float, default=600)
    parser.add_argument('--gomaxprocs', type=int, default=5)
    parser.add_argument('--time-binary', type=Path, default=Path('/usr/bin/time'))
    parser.add_argument('--include-files', type=int, choices=(0, 1), default=0)
    args = parser.parse_args()
    if platform.system() != 'Linux':
        parser.error('run inside the prepared Linux container')
    if args.runs < 20 or args.warmups < 3 or not math.isfinite(args.timeout) or args.timeout <= 0 or args.gomaxprocs < 1:
        parser.error('require at least 20 measured runs, 3 warmups, and positive timeout/GOMAXPROCS')
    selected = args.scenario or list(SCENARIOS)[:3]
    if len(set(selected)) != len(selected):
        parser.error('duplicate scenario')
    for name, value in vars(args).items():
        if isinstance(value, Path):
            setattr(args, name, value.resolve())
    if args.output.exists():
        parser.error('output must be a fresh directory')
    for root in (args.git_root, args.flat_root):
        if not root.is_dir() or args.output.is_relative_to(root):
            parser.error('roots must exist and output must be outside both input roots')
    receipts = {kind: getattr(args, kind + '_receipt') for kind in ('git', 'flat', 'build', 'host')}
    for path in receipts.values():
        if not path.is_file() or path.stat().st_size > 8 << 20:
            parser.error('receipts must be regular files no larger than 8 MiB; use a digest summary for full inventories')
    pins = {p['name']: p['commit'] for p in json.loads(args.pins.read_text())['projects']}
    identity = {
        'binary': str(args.binary), 'binary_sha256': sha(args.binary),
        'runner_sha256': sha(Path(__file__)), 'pins_sha256': sha(args.pins),
        'git_root': str(args.git_root), 'flat_root': str(args.flat_root),
        'receipts': {k: {'path': str(v), 'sha256': sha(v)} for k, v in receipts.items()},
        'gomaxprocs': args.gomaxprocs, 'include_files': args.include_files,
        'runtime_environment': {'GOGC': '100', 'GOMEMLIMIT': 'off',
                                'GODEBUG': os.environ.get('GODEBUG', '')},
        'platform': {'kernel': platform.release(), 'machine': platform.machine(),
                     'python': platform.python_version(), 'cpu_count': os.cpu_count(),
                     'cpu_affinity': sorted(os.sched_getaffinity(0))},
        'time_binary_sha256': sha(args.time_binary),
        'initialization': 'first-scan; fresh process; package initialization excluded from scan timer',
        'os_cache': 'uncontrolled; input preparation and preceding invocations can warm filesystem pages',
    }
    expected = {}
    baseline = None
    if args.stage != 'baseline':
        if args.baseline is None:
            parser.error('profile stages require --baseline')
        baseline = json.loads(args.baseline.read_text())
        if not baseline.get('complete') or baseline['stage'] != 'baseline' or baseline['identity'] != identity:
            parser.error('baseline incomplete or its binary/input/receipt/configuration identity differs')
        if baseline['runs'] < 20 or baseline['warmups'] < 3:
            parser.error('baseline has insufficient fresh invocations')
        for name in selected:
            if name not in baseline['result_digests']:
                parser.error('scenario absent from accepted baseline: ' + name)
            expected[name] = baseline['result_digests'][name]
    elif args.baseline is not None:
        parser.error('--baseline is only for profile stages')
    args.output.mkdir(parents=True)
    record = {
        'schema_version': '1.0.0', 'complete': False, 'stage': args.stage,
        'identity': identity, 'scenarios': {s: {'project': SCENARIOS[s][0],
            'source': SCENARIOS[s][1], 'workers': SCENARIOS[s][2],
            'commit': pins[SCENARIOS[s][0]]} for s in selected},
        'runs': args.runs if args.stage == 'baseline' else 1,
        'warmups': args.warmups if args.stage == 'baseline' else 0,
        'seed': args.seed, 'samples': [], 'result_digests': expected,
        'baseline_sha256': sha(args.baseline) if args.baseline else None,
        'started_at_utc': utc_now(), 'load_average_before': os.getloadavg(),
        'policy': {'minimum_runs': 20, 'minimum_excluded_warmups': 3,
                   'order': 'seeded shuffle within each round; all selected scenarios get equal counts',
                   'timing': 'perf_counter around GNU time spawn/blocking wait4; watchdog Timer; GNU time reports child CPU/RSS',
                   'percentile': 'empirical nearest rank ceil(0.95*n); no interpolation',
                   'variation': 'population standard deviation divided by mean; null when mean is zero',
                   'measured_duration_advisory_seconds': 10,
                   'duration_policy': 'fixed counts; flag below 10 seconds without automatic extension or excluding samples',
                   'timeout_seconds': args.timeout, 'cache_flush': False,
                   'claim_scope': 'recorded scenarios and environment only; no universal speed claim'},
    }
    write(args.output / 'plan.json', record)
    rng = random.Random(args.seed)
    rounds = [('warmup', n) for n in range(args.warmups)] + [('measured', n) for n in range(args.runs)]
    if args.stage != 'baseline':
        rounds = [(args.stage, 0)]
    try:
        for phase, iteration in rounds:
            order = selected.copy()
            rng.shuffle(order)
            for name in order:
                project, source, workers = SCENARIOS[name]
                output = args.output / f'{phase}-{iteration:02d}-{name}'
                output.mkdir()
                env = {k: v for k, v in os.environ.items() if not k.startswith('DIRCUE_PROFILE_')}
                env.update({'GOMAXPROCS': str(args.gomaxprocs), 'GOGC': '100', 'GOMEMLIMIT': 'off'})
                controls = {
                    'ROOT': str((args.git_root if source == 'git' else args.flat_root) / project),
                    'SOURCE': source, 'REVISION': pins[project] if source == 'git' else '',
                    'INIT': 'first-scan', 'MODE': 'languages', 'WORKERS': str(workers),
                    'MAX_TREE_SIZE': '100000', 'INCLUDE_FILES': str(args.include_files),
                    'FIXTURE_ID': name + '-' + pins[project], 'RUN_ID': 'scan',
                    'OUTPUT_DIR': str(output), 'FIXTURE_RECEIPT': str(receipts['git' if source == 'git' else 'flat']),
                    'BUILD_RECEIPT': str(receipts['build']), 'HOST_RECEIPT': str(receipts['host']),
                    'EXPECTED_SHA256': expected.get(name, ''), 'OS_CACHE': identity['os_cache'],
                    'MUTEX_FRACTION': '1' if args.stage == 'mutex' else '0', 'BLOCK_RATE_NS': '0',
                }
                env.update({'DIRCUE_PROFILE_' + k: v for k, v in controls.items()})
                command = [str(args.binary), '-test.run=^$', '-test.bench=^BenchmarkProfileScanner$',
                           '-test.benchtime=1x', '-test.count=1', '-test.benchmem']
                if args.stage != 'baseline':
                    flag = {'cpu': 'cpuprofile', 'mem': 'memprofile', 'mutex': 'mutexprofile'}[args.stage]
                    command.append(f'-test.{flag}={output / (args.stage + ".pprof")}')
                metrics = output / 'time.json'
                command = [str(args.time_binary), '-f', '{"wall_seconds":%e,"user_seconds":%U,"system_seconds":%S,"max_rss_kib":%M}', '-o', str(metrics), '--'] + command
                started_at = utc_now()
                elapsed = execute(command, env, output, args.timeout)
                finished_at = utc_now()
                fingerprints = list(output.glob('scan-*.json'))
                if len(fingerprints) != 1:
                    raise RuntimeError('expected exactly one scanner fingerprint: ' + str(output))
                fingerprint = json.loads(fingerprints[0].read_text())
                digest = fingerprint['result_sha256']
                if name in expected and expected[name] != digest:
                    raise RuntimeError('result digest changed: ' + name)
                expected[name] = digest
                sample = {'scenario': name, 'phase': phase, 'iteration': iteration,
                          'started_at_utc': started_at, 'finished_at_utc': finished_at,
                          'process_wall_seconds': elapsed, 'resources': json.loads(metrics.read_text()),
                          'scan_elapsed_ns': fingerprint['measured_elapsed_ns'],
                          'result_sha256': digest, 'fingerprint': str(fingerprints[0]),
                          'fingerprint_sha256': sha(fingerprints[0]), 'command': command}
                record['samples'].append(sample)
                print(f'{phase} {iteration + 1}: {name} verified', flush=True)
        record['complete'] = True
        if args.stage == 'baseline':
            record['medians'] = {}
            record['summaries'] = {}
            for name in selected:
                samples = [s for s in record['samples'] if s['scenario'] == name and s['phase'] == 'measured']
                record['medians'][name] = {'scan_elapsed_ns': statistics.median(s['scan_elapsed_ns'] for s in samples),
                    'process_wall_seconds': statistics.median(s['process_wall_seconds'] for s in samples),
                    **{k: statistics.median(s['resources'][k] for s in samples) for k in samples[0]['resources']}}
                scan_seconds = sum(s['scan_elapsed_ns'] for s in samples) / 1e9
                process_seconds = sum(s['process_wall_seconds'] for s in samples)
                record['summaries'][name] = {
                    'scan_elapsed_ns': distribution([s['scan_elapsed_ns'] for s in samples]),
                    'process_wall_seconds': distribution([s['process_wall_seconds'] for s in samples]),
                    'resources': {k: distribution([s['resources'][k] for s in samples]) for k in samples[0]['resources']},
                    'measured_scan_seconds': scan_seconds, 'measured_process_seconds': process_seconds,
                    'below_10s_scan_advisory': scan_seconds < 10,
                    'below_10s_process_advisory': process_seconds < 10,
                }
        record['finished_at_utc'] = utc_now()
        record['load_average_after'] = os.getloadavg()
        write(args.output / 'result.json', record)
    except Exception as error:
        record['error'] = str(error)
        record['finished_at_utc'] = utc_now()
        write(args.output / 'partial.json', record)
        raise


if __name__ == '__main__':
    main()
