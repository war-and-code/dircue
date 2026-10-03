#!/usr/bin/env python3
"""Compare published and candidate maps on caller-supplied cached repositories.

macOS timing via /usr/bin/time -l. No fetches, inspected-code execution, or
normalization of captured output. Existing receipts are never overwritten.
"""
import argparse
import hashlib
import json
import platform
import statistics
import subprocess
import sys
import time
from pathlib import Path


def sha(path):
    h = hashlib.sha256()
    with path.open('rb') as stream:
        for chunk in iter(lambda: stream.read(1 << 20), b''):
            h.update(chunk)
    return h.hexdigest()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--baseline', type=Path, required=True)
    parser.add_argument('--baseline-sha256', required=True)
    parser.add_argument('--candidate', type=Path, required=True)
    parser.add_argument('--output', type=Path, required=True)
    parser.add_argument('--pairs', type=int, default=20)
    parser.add_argument('--source', choices=('git', 'directory'), default='git')
    parser.add_argument('repositories', type=Path, nargs='+')
    args = parser.parse_args()
    if platform.system() != 'Darwin':
        parser.error('This harness requires macOS /usr/bin/time -l')
    if args.pairs < 1:
        parser.error('--pairs must be positive')
    if args.output.exists():
        parser.error('receipt already exists; choose a fresh output path')
    if sha(args.baseline) != args.baseline_sha256:
        parser.error('published baseline hash mismatch')
    capture = args.output.parent / (args.output.stem + '-captures')
    capture.mkdir(parents=True, exist_ok=False)
    binaries = {'released': args.baseline.resolve(), 'candidate': args.candidate.resolve()}
    report = {'method': 'Warm each binary, then interleave AB/BA pairs on each repository. Map contents may intentionally differ; per-binary capture hashes must be stable.',
              'source_mode': args.source, 'pairs': args.pairs,
              'host': {'platform': platform.platform(), 'machine': platform.machine(),
                       'python': platform.python_version(), 'cpu': subprocess.check_output(['sysctl', '-n', 'machdep.cpu.brand_string'], text=True).strip(),
                       'ram_bytes': int(subprocess.check_output(['sysctl', '-n', 'hw.memsize'], text=True)),
                       'isolation': 'shared developer host; warm filesystem caches; no OS tuning'},
              'source_commit': subprocess.check_output(['git', 'rev-parse', 'HEAD'], text=True).strip(),
              'source_dirty': bool(subprocess.check_output(['git', 'status', '--porcelain'], text=True).strip()),
              'binaries': {k: {'sha256': sha(v), 'go_metadata': subprocess.check_output(['go', 'version', '-m', str(v)], text=True).splitlines()[1:]} for k,v in binaries.items()},
              'workloads': [], 'runs': [], 'summary': {}}
    for repo in args.repositories:
        name = repo.name
        revision = subprocess.check_output(['git', '-C', str(repo), 'rev-parse', 'HEAD'], text=True).strip()
        status = subprocess.check_output(['git', '-C', str(repo), 'status', '--porcelain'], text=True)
        tracked = subprocess.check_output(['git', '-C', str(repo), 'ls-files', '-z'])
        report['workloads'].append({'name': name, 'revision': revision, 'dirty': bool(status), 'tracked_paths_sha256': hashlib.sha256(tracked).hexdigest(), 'tracked_files': tracked.count(b'\0')})
        for pair in range(-1, args.pairs):
            order = ('released', 'candidate') if pair % 2 == 0 or pair < 0 else ('candidate', 'released')
            for which in order:
                stem = f'{name}-{pair+1:02d}-{which}'
                stdout, stderr, timed = (capture / (stem + suffix) for suffix in ('.stdout', '.stderr', '.time'))
                command = ['/usr/bin/time', '-l', '-o', str(timed), str(binaries[which]), 'map', '--json', '--source', args.source, str(repo.resolve())]
                start = time.perf_counter()
                with stdout.open('wb') as out, stderr.open('wb') as err:
                    proc = subprocess.run(command, stdout=out, stderr=err, timeout=120)
                elapsed = time.perf_counter() - start
                timing = timed.read_text()
                rss = next(int(line.split()[0]) for line in timing.splitlines() if 'maximum resident set size' in line)
                if proc.returncode:
                    raise RuntimeError(f'{stem} failed, see captured stderr')
                report['runs'].append({'workload': name, 'pair': pair, 'binary': which, 'wall_seconds': elapsed, 'peak_rss_bytes': rss,
                                       'exit_code': proc.returncode, 'stdout_sha256': sha(stdout), 'stderr_sha256': sha(stderr)})
        summary = {}
        for which in binaries:
            runs = [r for r in report['runs'] if r['workload']==name and r['binary']==which and r['pair']>=0]
            walls = sorted(r['wall_seconds'] for r in runs)
            if len({r['stdout_sha256'] for r in runs}) != 1 or len({r['stderr_sha256'] for r in runs}) != 1:
                raise RuntimeError(f'{name}/{which} output is nondeterministic')
            summary[which] = {'n': len(walls), 'median_seconds': statistics.median(walls), 'p95_seconds': walls[max(0, (len(walls)*95+99)//100-1)],
                              'min_seconds': min(walls), 'max_seconds': max(walls), 'median_peak_rss_bytes': statistics.median(r['peak_rss_bytes'] for r in runs)}
        summary['median_change_pct'] = (summary['candidate']['median_seconds']/summary['released']['median_seconds']-1)*100
        report['summary'][name] = summary
        print(name, json.dumps(summary), flush=True)
    with args.output.open('x') as out:
        json.dump(report, out, indent=2);out.write('\n')


if __name__ == '__main__':
    main()
