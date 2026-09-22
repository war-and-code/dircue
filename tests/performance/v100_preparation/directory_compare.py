#!/usr/bin/env python3
"""Compare directory scans on a synthetic Java/C#/Go corpus on macOS.

Run only while other builds and tests are idle. Results describe this host and
window, not all repositories. Every timed invocation must match the baseline.
"""
import argparse
from datetime import datetime, timezone
import hashlib
import json
from pathlib import Path
import platform
import re
import statistics
import subprocess
import tempfile
import time


def sha(data):
    return hashlib.sha256(data).hexdigest()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--baseline', type=Path, required=True)
    parser.add_argument('--candidate', type=Path, required=True)
    parser.add_argument('--output', type=Path, required=True)
    parser.add_argument('--pairs', type=int, default=10)
    args = parser.parse_args()
    if platform.system() != 'Darwin' or args.pairs < 3 or args.output.exists():
        parser.error('requires macOS, at least three pairs and a fresh output path')
    binaries = {name: getattr(args, name).resolve() for name in ('baseline', 'candidate')}
    hashes = {name: sha(path.read_bytes()) for name, path in binaries.items()}
    receipt = {'started_at': datetime.now(timezone.utc).isoformat(),
               'platform': platform.platform(), 'machine': platform.machine(),
               'scope': 'Warm-cache synthetic directory scans; shared host; no worker or Git.',
               'binary_sha256': hashes, 'harness_sha256': sha(Path(__file__).read_bytes()),
               'warmup_pairs': 2, 'measured_pairs': args.pairs, 'lanes': []}
    content = [('cs', b'class Example { static int Value() { return 42; } }\n'),
               ('java', b'class Example { static int value() { return 42; } }\n'),
               ('go', b'package example\nfunc Value() int { return 42 }\n')]
    for count in (1000, 10000, 50000):
        with tempfile.TemporaryDirectory(prefix='dircue-directory-ab-') as temporary:
            root = Path(temporary) / 'tree'
            root.mkdir()
            fixture = hashlib.sha256()
            for index in range(count):
                extension, data = content[index % len(content)]
                relative = f'group{index // 500:04}/file{index:06}.{extension}'
                file = root / relative
                file.parent.mkdir(exist_ok=True)
                file.write_bytes(data)
                fixture.update(relative.encode() + b'\0' + data + b'\0')
            flags = ['--json', '--breakdown', '--workers', '16', '--source', 'directory',
                     '--tree-size', '1000000', str(root)]
            rows, expected = [], None
            for pair in range(-2, args.pairs):
                order = ('baseline', 'candidate') if pair % 2 == 0 else ('candidate', 'baseline')
                for name in order:
                    timing = Path(temporary) / 'time.txt'
                    started = time.perf_counter()
                    result = subprocess.run(['/usr/bin/time', '-l', '-o', str(timing),
                                             str(binaries[name]), *flags], capture_output=True, timeout=120)
                    elapsed = time.perf_counter() - started
                    actual = (result.returncode, result.stdout, result.stderr)
                    if expected is None:
                        if name != 'baseline' or result.returncode != 0 or result.stderr:
                            raise AssertionError('invalid baseline invocation')
                        expected = actual
                    if actual != expected:
                        raise AssertionError(f'output mismatch: {count}/{pair}/{name}')
                    raw_time = timing.read_text()
                    rss = re.search(r'^\s*(\d+)\s+maximum resident set size\s*$', raw_time, re.M)
                    if not rss:
                        raise AssertionError('missing peak RSS')
                    rows.append({'pair': pair, 'binary': name, 'elapsed_seconds': elapsed,
                                 'peak_rss_bytes': int(rss[1]), 'time_output': raw_time,
                                 'stdout_sha256': sha(result.stdout), 'exit_code': 0, 'stderr': ''})
            summary = {}
            for name in binaries:
                samples = [row for row in rows if row['pair'] >= 0 and row['binary'] == name]
                summary[name] = {'median_seconds': statistics.median(r['elapsed_seconds'] for r in samples),
                                 'median_peak_rss_bytes': statistics.median(r['peak_rss_bytes'] for r in samples)}
            lane = {'files': count, 'fixture_sha256': fixture.hexdigest(), 'flags': flags[:-1] + ['<fixture>'],
                    'summary': summary, 'samples': rows}
            receipt['lanes'].append(lane)
            print(json.dumps({'files': count, 'summary': summary}), flush=True)
    if hashes != {name: sha(path.read_bytes()) for name, path in binaries.items()}:
        raise AssertionError('binary changed during measurement')
    receipt['finished_at'] = datetime.now(timezone.utc).isoformat()
    args.output.parent.mkdir(parents=True, exist_ok=True)
    args.output.write_text(json.dumps(receipt, indent=2) + '\n')


if __name__ == '__main__':
    main()
