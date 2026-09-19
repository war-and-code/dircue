#!/usr/bin/env python3
"""Capture same-host decoder batch measurements and CPU/allocation profiles."""
import argparse
from datetime import datetime, timezone
import hashlib
import json
from pathlib import Path
import re
import statistics
import subprocess
import time

HERE = Path(__file__).resolve().parent
ROOT = HERE.parents[2]


def digest(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def quantile(values, fraction):
    return sorted(values)[min(len(values) - 1, max(0, __import__('math').ceil(len(values) * fraction) - 1))]


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--binary', type=Path, required=True)
    parser.add_argument('--phase', choices=['baseline', 'candidate'], required=True)
    args = parser.parse_args()
    directory = HERE / args.phase
    directory.mkdir(exist_ok=True)
    if (directory / 'bench.txt').exists():
        raise SystemExit('choose a phase without existing measurements')
    binary = args.binary.resolve()
    sources = {str(p.relative_to(ROOT)): digest(p) for p in sorted((ROOT / 'pkg/structure').glob('*.go'))}
    receipt = {'started_at_utc': datetime.now(timezone.utc).isoformat(), 'binary_sha256': digest(binary),
               'harness_sha256': digest(Path(__file__)), 'source_sha256': sources, 'commands': [],
               'method': '20 independent benchmark batch averages per case; optimized Go, GOMAXPROCS=1. Percentiles are of batch means, not request latencies. Extreme quantiles collapse to the maximum with 20 samples.'}

    def execute(name, options):
        command = ['/usr/bin/time', '-l', str(binary), '-test.run', '^$', '-test.cpu', '1', *options]
        start = time.perf_counter()
        with (directory / (name + '.txt')).open('wb') as out, (directory / (name + '.time')).open('wb') as err:
            subprocess.run(command, cwd=ROOT / 'pkg/structure', stdout=out, stderr=err, check=True, timeout=180)
        receipt['commands'].append({'command': command, 'elapsed_seconds': time.perf_counter() - start,
                                    'stdout_sha256': digest(directory / (name + '.txt')),
                                    'stderr_sha256': digest(directory / (name + '.time'))})

    execute('bench', ['-test.bench', '^BenchmarkFunctionResponse$', '-test.benchmem', '-test.benchtime', '100ms', '-test.count', '20'])
    execute('stages', ['-test.bench', '^BenchmarkFunctionStages$', '-test.benchmem', '-test.benchtime', '100ms', '-test.count', '20'])
    execute('profile', ['-test.bench', '^BenchmarkFunctionResponse/spaces=128/functions=true$', '-test.benchmem',
                        '-test.benchtime', '5s', '-test.count', '1', '-test.cpuprofile', str(directory / 'cpu.pprof'),
                        '-test.memprofile', str(directory / 'heap.pprof')])
    for name, options in [('cpu-top', ['-top', '-cum']), ('cpu-flat', ['-top']), ('alloc-top', ['-top', '-cum', '-alloc_space'])]:
        profile = directory / ('heap.pprof' if name.startswith('alloc') else 'cpu.pprof')
        with (directory / (name + '.txt')).open('wb') as out:
            subprocess.run(['go', 'tool', 'pprof', *options, str(binary), str(profile)], stdout=out, check=True, timeout=60)
    groups = {}
    pattern = re.compile(r'^(Benchmark\S+)\s+(\d+)\s+([\d.]+) ns/op\s+(?:[\d.]+ MB/s\s+)?([\d.]+) B/op\s+(\d+) allocs/op$')
    for name in ['bench', 'stages']:
        for line in (directory / (name + '.txt')).read_text().splitlines():
            match = pattern.match(line)
            if match:
                label, iterations, ns, allocated, allocs = match.groups()
                groups.setdefault(label, []).append({'iterations': int(iterations), 'ns_per_op': float(ns),
                                                    'bytes_per_op': float(allocated), 'allocs_per_op': int(allocs)})
    assert len(groups) == 16 and all(len(values) == 20 for values in groups.values()), 'benchmark sample population incomplete'
    receipt['samples'] = groups
    receipt['summary'] = {}
    for label, samples in groups.items():
        values = [sample['ns_per_op'] for sample in samples]
        receipt['summary'][label] = {'batch_mean_ns_p50': statistics.median(values),
                                    **{f'batch_mean_ns_p{q}': quantile(values, q / 100) for q in (95, 99, 99.9, 99.99)},
                                    'batch_mean_ns_max': max(values), 'median_ops_per_second': 1e9 / statistics.median(values),
                                    'median_bytes_per_op': statistics.median(s['bytes_per_op'] for s in samples),
                                    'median_allocs_per_op': statistics.median(s['allocs_per_op'] for s in samples)}
    assert {str(p.relative_to(ROOT)): digest(p) for p in sorted((ROOT / 'pkg/structure').glob('*.go'))} == sources, 'source changed during profiling'
    assert digest(binary) == receipt['binary_sha256'], 'binary changed during profiling'
    receipt.update(passed=True, completed_at_utc=datetime.now(timezone.utc).isoformat())
    (directory / 'receipt.json').write_text(json.dumps(receipt, indent=2) + '\n')
    print(f'{args.phase}: 320 batch measurements and CPU/allocation profiles retained', flush=True)


if __name__ == '__main__':
    main()
