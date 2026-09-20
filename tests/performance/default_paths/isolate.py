#!/usr/bin/env python3
"""Isolate the observed Roslyn directory/project slowdown without replacing it."""
import argparse
import gzip
import json
from pathlib import Path
import statistics

import benchmark as bench


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--receipt', required=True, type=Path)
    parser.add_argument('--intermediate', required=True, type=Path)
    parser.add_argument('--output', required=True, type=Path)
    parser.add_argument('--environment-note', required=True)
    args = parser.parse_args()
    assert not args.output.exists()
    initial = json.loads(gzip.decompress(args.receipt.read_bytes()))
    case = next(c for c in initial['cases'] if c['name'] == 'roslyn-directory')
    intermediate = args.intermediate.resolve()
    intermediate_hash = '7c950ff5baa3870fc6fe332f6e98f3443877867bb3852149e1b20609362f43a2'
    assert bench.digest(intermediate) == intermediate_hash
    lanes = {'baseline': initial['baseline'], 'intermediate': str(intermediate),
             'candidate_a': initial['candidate'], 'candidate_b': initial['candidate']}
    hashes = {key: bench.digest(Path(binary)) for key, binary in lanes.items()}
    assert hashes['baseline'] == initial['baseline_sha256']
    assert hashes['candidate_a'] == hashes['candidate_b'] == initial['candidate_sha256']
    versions = {key: bench.run([binary, '--version']) for key, binary in lanes.items()}
    assert len(set(versions.values())) == 1
    assert bench.inventory(Path(case['path']), 'directory') == case['input']
    output_path = args.receipt.parent / (case['name'] + '-projects.json.gz')
    expected = gzip.decompress(output_path.read_bytes())
    commands = {key: [binary, *case['commands']['baseline_projects'][1:]] for key, binary in lanes.items()}
    receipt = {'started_at_utc': bench.utc(), 'initial_receipt_sha256': bench.digest(args.receipt),
               'output_artifact': str(output_path), 'output_artifact_sha256': bench.digest(output_path),
               'harness_sha256': bench.digest(Path(__file__)), 'measure_helper_sha256': bench.digest(Path(bench.__file__)),
               'purpose': 'Investigate one consistently slower case from the initial sample; original samples remain retained.',
               'environment': args.environment_note, 'input': case['input'], 'commands': commands, 'binary_sha256': hashes,
               'versions': versions, 'method': 'Five rounds; rotate four lanes by one each round, after one warmup per lane. candidate_a and candidate_b are exactly the same executable and arguments.',
               'samples': {key: [] for key in lanes}, 'warmups': {}, 'execution_order': []}
    for key, command in commands.items():
        payload, sample = bench.measure(command)
        assert payload == expected
        receipt['warmups'][key] = sample
    keys = list(commands)
    for index in range(5):
        order = keys[index % 4:] + keys[:index % 4]
        for key in order:
            payload, sample = bench.measure(commands[key])
            assert payload == expected
            sample['round'] = index + 1
            receipt['samples'][key].append(sample)
            receipt['execution_order'].append(key)
        print('round', index + 1, {k: round(receipt['samples'][k][-1]['seconds'], 4) for k in keys}, flush=True)
    receipt['timings_finished_at_utc'] = bench.utc()
    print('TIMINGS FINISHED', flush=True)
    receipt['summary'] = {key: {'median_seconds': statistics.median(s['seconds'] for s in samples),
                                'median_peak_rss_bytes': statistics.median(s['peak_rss_bytes'] for s in samples)}
                          for key, samples in receipt['samples'].items()}
    assert bench.inventory(Path(case['path']), 'directory') == case['input']
    assert all(bench.digest(Path(binary)) == hashes[key] for key, binary in lanes.items())
    receipt.update(passed=True, finished_at_utc=bench.utc())
    bench.save(args.output, receipt)
    print(receipt['summary'], flush=True)


if __name__ == '__main__':
    main()
