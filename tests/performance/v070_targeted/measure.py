#!/usr/bin/env python3
"""Observe opt-in costs; these commands answer different questions."""
import argparse
from datetime import datetime, timezone
import json
from pathlib import Path
import platform
import subprocess
import sys
import tempfile

ROOT = Path(__file__).resolve().parents[3]
sys.path.insert(0, str(ROOT / 'tests/focus_v070'))
import common
import performance


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--root', type=Path, required=True)
    parser.add_argument('--candidate', type=Path, required=True)
    parser.add_argument('--build-receipt', type=Path, required=True)
    parser.add_argument('--file', required=True, help='Root-relative file for the fresh and retained query')
    parser.add_argument('--output', type=Path, required=True)
    parser.add_argument('--repetitions', type=int, default=5)
    parser.add_argument('--workers', type=int, default=8)
    args = parser.parse_args()
    if args.repetitions < 3 or args.workers < 1:
        parser.error('use at least three repetitions and one worker')
    if args.output.exists():
        parser.error('choose a fresh output path')
    root, binary = args.root.resolve(), args.candidate.resolve()
    bound, build_sha = common.load_build_receipt(args.build_receipt, binary)
    source = performance.inventory(root)
    suffix = ['--json', '--source', 'directory', '--workers', str(args.workers), str(root)]
    commands = {
        'language_default': [str(binary), *suffix],
        'availability': [str(binary), 'analyze', 'availability', *suffix],
        'explain_file': [str(binary), 'analyze', 'explain', '--file', args.file, *suffix],
    }
    started = datetime.now(timezone.utc).isoformat()
    with tempfile.TemporaryDirectory(prefix='dircue-targeted-cost-') as temporary:
        saved = Path(temporary) / 'report.json'
        fresh = subprocess.run(commands['explain_file'], capture_output=True, check=True, timeout=300)
        if fresh.stderr or json.loads(fresh.stdout)['explanation']['decision']['status'] != 'included':
            raise AssertionError('choose an included file for explanation measurement')
        saved.write_bytes(fresh.stdout)
        commands['explain_saved'] = [str(binary), 'analyze', 'explain', '--file', args.file, '--json', '--report', str(saved)]
        samples = {key: [] for key in commands}
        observations = {}
        for round_number in range(args.repetitions + 1):
            order = list(commands) if round_number % 2 == 0 else reversed(list(commands))
            for lane in order:
                raw, record = performance.measure(commands[lane], 300)
                if round_number:
                    samples[lane].append(record)
                result = json.loads(raw)
                if lane == 'availability':
                    observations[lane] = {key: result['availability'][key] for key in ('status', 'coverage', 'counts', 'omissions')}
                if lane.startswith('explain'):
                    observations[lane] = {key: result['explanation'][key] for key in ('status', 'scope', 'extent', 'decision', 'omissions')}
    if source != performance.inventory(root) or common.sha256(binary) != bound['candidate_sha256']:
        raise AssertionError('source or executable changed during measurement')
    for rows in samples.values():
        if len({row['stdout_sha256'] for row in rows}) != 1:
            raise AssertionError('output changed between repetitions')
    result = {
        'schema': 'dircue-v070-targeted-cost-observation-1',
        'started_at_utc': started, 'finished_at_utc': datetime.now(timezone.utc).isoformat(),
        'candidate_sha256': common.sha256(binary), 'build_receipt_sha256': build_sha,
        'harness_sha256': {str(path.relative_to(ROOT)): common.sha256(path) for path in
                          (Path(__file__).resolve(), ROOT / 'tests/focus_v070/common.py', ROOT / 'tests/focus_v070/performance.py')},
        'host': platform.platform(), 'source': source, 'commands': commands,
        'summaries': {key: performance.summary(rows) for key, rows in samples.items()},
        'observations': observations,
        'method': 'One warmup, then repeated fresh processes in alternating lane order; directory source. Includes startup, metadata traversal, requested reads and JSON serialization. OS peak RSS. Lanes answer different questions, so timing ratios are not equivalent-work speedups. No host isolation; arrange a quiet measurement window.',
    }
    common.write_json(args.output, result)
    print(json.dumps({key: {'ms': value['median_seconds'] * 1000,
                           'rss_mib': value['median_peak_rss_bytes'] / 1048576}
                      for key, value in result['summaries'].items()}))


if __name__ == '__main__':
    main()
