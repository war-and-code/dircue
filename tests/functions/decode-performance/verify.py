#!/usr/bin/env python3
"""Verify decoder profile inputs, samples, source snapshots and checksums."""
import gzip
import hashlib
import json
from pathlib import Path
import re
import statistics

from proof import main as verify_proof

HERE = Path(__file__).resolve().parent


def digest(data):
    return hashlib.sha256(data).hexdigest()


def main():
    for line in (HERE / 'SHA256SUMS').read_text().splitlines():
        expected, name = line.split('  ', 1)
        path = Path(name)
        assert not path.is_absolute() and '..' not in path.parts, 'unsafe artifact path'
        assert digest((HERE / path).read_bytes()) == expected, f'artifact changed: {name}'
    inputs = json.loads((HERE / 'inputs.json').read_text())
    for name, expected in inputs['files'].items():
        assert digest((HERE / 'inputs' / name).read_bytes()) == expected, name
    for count in [1, 10, 50, 100, 128, 500, 1000]:
        source = (HERE / 'inputs' / f'{count}.py').read_bytes()
        for variant in ['default', 'functions']:
            result = json.loads(gzip.decompress((HERE / 'inputs' / f'{count}-{variant}.json.gz').read_bytes()))
            assert result['path'] == 'many.py' and result['source_bytes'] == len(source) and result['parse_count'] == 1
            if variant == 'functions':
                functions = result['functions']
                assert functions['total_spaces'] == count and len(functions['entries']) == min(count, 128)
                assert functions['omitted_spaces'] == max(0, count - 128) and functions['invalid_span_spaces'] == 0
            else:
                assert 'functions' not in result
    pattern = re.compile(r'^(Benchmark\S+)\s+(\d+)\s+([\d.]+) ns/op\s+(?:[\d.]+ MB/s\s+)?([\d.]+) B/op\s+(\d+) allocs/op$')
    for phase in ['baseline', 'candidate']:
        receipt = json.loads((HERE / phase / 'receipt.json').read_text())
        assert receipt['passed'] and len(receipt['samples']) == 16
        assert receipt['harness_sha256'] == digest((HERE / 'profile.py').read_bytes())
        snapshot = json.loads(gzip.decompress((HERE / phase / 'sources.json.gz').read_bytes()))
        assert all(digest(snapshot[name].encode()) == value for name, value in receipt['source_sha256'].items())
        parsed = {}
        for stem in ['bench', 'stages']:
            for line in (HERE / phase / (stem + '.txt')).read_text().splitlines():
                match = pattern.match(line)
                if match:
                    name, iterations, ns, allocated, allocs = match.groups()
                    parsed.setdefault(name, []).append({'iterations': int(iterations), 'ns_per_op': float(ns),
                                                        'bytes_per_op': float(allocated), 'allocs_per_op': int(allocs)})
        assert parsed == receipt['samples'], f'{phase}: raw samples disagree'
        for name, values in parsed.items():
            assert len(values) == 20 and all(v['ns_per_op'] > 0 for v in values)
            assert statistics.median(v['ns_per_op'] for v in values) == receipt['summary'][name]['batch_mean_ns_p50']
        fingerprint = json.loads((HERE / phase / 'fingerprint.json').read_text())
        assert fingerprint['test_binary_sha256'] == receipt['binary_sha256']
    differential = json.loads((HERE / 'cli-differential.json').read_text())
    assert differential['passed'] and len(differential['cases']) == 27
    assert differential['harness_sha256'] == digest((HERE / 'compare_cli.py').read_bytes())
    assert differential['worker_sha256'] == inputs['worker_sha256']
    verify_proof()
    print('640 batch measurements, native inputs, source proof and CLI receipt verified.')


if __name__ == '__main__':
    main()
