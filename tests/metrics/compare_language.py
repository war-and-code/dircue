#!/usr/bin/env python3
"""Compare language-only process cost; optionally measure the separate metrics command."""
import argparse
import hashlib
import json
import math
import os
from pathlib import Path
import platform
import re
import statistics
import subprocess
import tempfile
import threading
import time


def sha(path):
    return hashlib.sha256(Path(path).read_bytes()).hexdigest()


def output(command):
    result = subprocess.run(command, capture_output=True, text=True, timeout=600)
    if result.returncode:
        raise RuntimeError(f'{command}: exit {result.returncode}: {result.stderr[:2000]}')
    return result.stdout, result.stderr


def measured(command):
    system = platform.system()
    with tempfile.TemporaryFile() as diagnostic, tempfile.NamedTemporaryFile() as stats:
        launcher = ['/usr/bin/time', '-l'] if system == 'Darwin' else [
            '/usr/bin/time', '-f', '%U %S %M', '-o', stats.name]
        started = time.perf_counter()
        result = subprocess.Popen(launcher + command, stdout=subprocess.DEVNULL, stderr=diagnostic)
        timer = threading.Timer(600, result.kill)
        timer.daemon = True
        timer.start()
        try:
            result.wait()
        finally:
            timer.cancel()
        elapsed = time.perf_counter() - started
        diagnostic.seek(0)
        stderr = diagnostic.read().decode(errors='replace')
        if result.returncode:
            raise RuntimeError(f'{command}: exit {result.returncode}: {stderr[:2000]}')
        if system == 'Darwin':
            cpu = re.search(r'([\d.]+) real\s+([\d.]+) user\s+([\d.]+) sys', stderr)
            rss = re.search(r'(\d+)\s+maximum resident set size', stderr)
            if not cpu or not rss:
                raise RuntimeError(f'unrecognized BSD time result: {stderr}')
            user, system_time, rss_bytes = float(cpu[2]), float(cpu[3]), int(rss[1])
        else:
            stats.seek(0)
            user, system_time, rss_kib = map(float, stats.read().decode().split())
            rss_bytes = int(rss_kib * 1024)
        return {'seconds': elapsed, 'user_seconds': user, 'system_seconds': system_time,
                'peak_rss_bytes': rss_bytes,
                'cpu_percent': 100 * (user + system_time) / elapsed}


def summary(samples):
    values = sorted(row['seconds'] for row in samples)
    def percentile(p):
        return values[min(len(values)-1, math.ceil(p * len(values))-1)]
    median = statistics.median(values)
    return {'runs': len(samples), 'median_seconds': median,
            'p95_seconds': percentile(.95), 'p99_seconds': percentile(.99),
            'p999_seconds': percentile(.999), 'p9999_seconds': percentile(.9999),
            'max_seconds': max(values), 'min_seconds': min(values),
            'coefficient_of_variation': statistics.pstdev(values)/statistics.mean(values),
            'operations_per_second': 1/median,
            'peak_rss_bytes': max(row['peak_rss_bytes'] for row in samples),
            'mean_cpu_percent': statistics.mean(row['cpu_percent'] for row in samples)}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--baseline', type=Path, required=True)
    parser.add_argument('--candidate', type=Path, required=True)
    parser.add_argument('--project', action='append', required=True, help='NAME=PATH')
    parser.add_argument('--source', choices=['auto', 'git', 'directory'], default='auto')
    parser.add_argument('--metrics', action='store_true')
    parser.add_argument('--runs', type=int, default=20)
    parser.add_argument('--warmup', type=int, default=3)
    parser.add_argument('--output', type=Path, required=True)
    args = parser.parse_args()
    if args.runs < 20 or args.warmup < 0:
        parser.error('use at least 20 samples and nonnegative warmups')
    if platform.system() not in ('Linux', 'Darwin'):
        parser.error('peak-RSS collection supports Linux and macOS')
    binaries = {'baseline': str(args.baseline.resolve()), 'candidate': str(args.candidate.resolve())}
    report = {'schema_version': '1.0.0', 'started_at_utc': time.strftime('%Y-%m-%dT%H:%M:%SZ', time.gmtime()),
              'binaries': {name: {'path': binary, 'sha256': sha(binary)} for name, binary in binaries.items()},
              'harness_sha256': sha(__file__),
              'environment': {'platform': platform.platform(), 'machine': platform.machine(),
                              'cpu_count': os.cpu_count(), 'load_average': os.getloadavg()},
              'methodology': {'cache': 'warm after explicit warmups; no OS cache flush',
                              'order': 'alternating tool order each iteration',
                              'wall': 'perf_counter includes identical system time launcher overhead',
                              'rss': 'system time target-child peak RSS, normalized to bytes',
                              'tail_note': 'p99+ are observed order statistics, not reliable rare-tail estimates',
                              'comparison': 'exact language JSON and exit status before timing; metrics is extra work',
                              'isolation': 'operator must stop concurrent builds and heavy tests before running'},
              'projects': []}
    environment_commands = [['uname', '-a'], ['df', '-T' if platform.system() == 'Linux' else '-h', '.']]
    if platform.system() == 'Darwin':
        environment_commands += [['sysctl', 'machdep.cpu.brand_string', 'hw.memsize', 'hw.logicalcpu', 'hw.physicalcpu'], ['pmset', '-g']]
    else:
        environment_commands += [['cat', '/proc/cpuinfo'], ['cat', '/proc/meminfo']]
    report['environment']['details'] = {}
    for command in environment_commands:
        result = subprocess.run(command, capture_output=True, text=True)
        report['environment']['details'][' '.join(command)] = result.stdout if result.returncode == 0 else result.stderr
    args.output.parent.mkdir(parents=True, exist_ok=True)
    for item in args.project:
        name, path = item.split('=', 1)
        path = str(Path(path).resolve())
        commands = {name: [binary, '--json', '--source', args.source, path]
                    for name, binary in binaries.items()}
        outputs = {name: output(command) for name, command in commands.items()}
        if json.loads(outputs['baseline'][0]) != json.loads(outputs['candidate'][0]):
            raise RuntimeError(f'{name}: language JSON differs; timing withheld')
        entry = {'name': name, 'path': path, 'source': args.source, 'language_match': True,
                 'output_sha256': {name: hashlib.sha256(value[0].encode()).hexdigest() for name, value in outputs.items()},
                 'stderr': {name: value[1] for name, value in outputs.items()}, 'commands': commands}
        git = subprocess.run(['git', '-C', path, 'rev-parse', 'HEAD'], capture_output=True, text=True)
        if git.returncode == 0:
            entry['git_commit'] = git.stdout.strip()
            entry['worktree_status'] = subprocess.check_output(['git', '-C', path, 'status', '--porcelain']).decode()
        entry['language_bytes'] = sum(row['size'] for row in json.loads(outputs['baseline'][0]).values())
        if args.metrics:
            commands['metrics'] = [binaries['candidate'], 'analyze', 'metrics', '--json', '--source', args.source, path]
            metrics, stderr = output(commands['metrics'])
            entry['metrics_output_sha256'] = hashlib.sha256(metrics.encode()).hexdigest()
            entry['metrics_stderr'] = stderr
        for _ in range(args.warmup):
            for command in commands.values():
                measured(command)
        samples = {name: [] for name in commands}
        for iteration in range(args.runs):
            order = list(commands) if iteration % 2 == 0 else list(reversed(commands))
            for tool in order:
                samples[tool].append(measured(commands[tool]))
        entry['samples'] = samples
        entry['summary'] = {tool: summary(rows) for tool, rows in samples.items()}
        entry['candidate_to_baseline_p95_ratio'] = entry['summary']['candidate']['p95_seconds']/entry['summary']['baseline']['p95_seconds']
        for stats in entry['summary'].values():
            stats['language_bytes_per_second'] = entry['language_bytes']/stats['median_seconds']
        report['projects'].append(entry)
        args.output.write_text(json.dumps(report, indent=2)+'\n')
        print(json.dumps({'project': name, 'summary': entry['summary'],
                          'candidate_to_baseline_p95_ratio': entry['candidate_to_baseline_p95_ratio']}), flush=True)


if __name__ == '__main__':
    main()
