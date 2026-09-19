#!/usr/bin/env python3
"""Verify retained function-check receipts and compressed benchmark reports."""
import argparse
import gzip
import json
from pathlib import Path
import statistics

from run import HERE, ROOT, file_sha, require, sha, validate_functions, without_timings


def read_json(path):
    raw = path.read_bytes()
    return json.loads(gzip.decompress(raw) if path.suffix == '.gz' else raw)


def verify_protocol(path):
    receipt = read_json(path)
    require(receipt['passed'] and len(receipt['default_cases']) == 67 and len(receipt['breadth']) == 21,
            'protocol population incomplete')
    require(receipt['harness_sha256'] == file_sha(HERE / 'run.py'), 'protocol harness changed')
    require(len(receipt['counterexamples']) == 11 and receipt['cli'] is not None, 'CLI/counterexamples missing')
    for entry in receipt['breadth']:
        source = (ROOT / 'tests/structural_breadth/testdata' / entry['path']).read_bytes()
        require(sha(source) == entry['source_sha256'], 'breadth source changed')
        validate_functions(entry['functions'], source.decode())
    for entry in receipt['counterexamples']:
        source = entry['source'].encode() if 'source' in entry else (HERE / 'fixtures' / entry['path']).read_bytes()
        require(sha(source) == entry['source_sha256'], 'counterexample source changed')
        validate_functions(entry['functions'], source.decode())


def verify_benchmark(directory):
    receipt = read_json(directory / 'receipt.json')
    require(receipt['passed'] and len(receipt['cases']) == 8, 'benchmark population incomplete')
    require(receipt['harness_sha256'] == file_sha(HERE / 'benchmark.py') and
            receipt['helpers_sha256'] == file_sha(HERE / 'run.py'), 'benchmark implementation changed')
    require(receipt['method']['repetitions'] == 3 and receipt['method']['warmups'] == 1, 'measurement method changed')
    for case in receipt['cases']:
        outputs = {}
        for variant, item in case['outputs'].items():
            require(Path(item['path']).name == item['path'], 'report path escaped artifact directory')
            compressed = (directory / item['path']).read_bytes()
            require(sha(compressed) == item['sha256'], 'compressed report corrupt')
            raw = gzip.decompress(compressed)
            require(sha(raw) == item['uncompressed_sha256'], 'report digest mismatch')
            outputs[variant] = raw
            samples = case['samples'][variant]
            require(len(samples) == 3 and sorted(s['repetition'] for s in samples) == [1, 2, 3], 'sample population')
            require(all(s['seconds'] > 0 and s['max_rss_bytes'] > 0 for s in samples), 'invalid sample')
            summary = case['summary'][variant]
            require(summary['median_seconds'] == statistics.median(s['seconds'] for s in samples) and
                    summary['max_rss_bytes'] == max(s['max_rss_bytes'] for s in samples), 'summary differs from samples')
            if not variant.endswith('worker'):
                require(all(s['stdout_sha256'] == sha(raw) and s['stdout_bytes'] == len(raw) for s in samples),
                        'CLI measurement output differs from checked report')
        require(outputs['old_cli'] == outputs['new_cli'], 'default CLI differs')
        base = json.loads(outputs['new_cli'])['structure']
        enriched = json.loads(outputs['functions_cli'])['structure']
        functions = enriched.pop('functions')
        require(functions['total_spaces'] == len(functions['entries']) + functions['omitted_spaces'] +
                functions['invalid_span_spaces'], 'CLI population invariant')
        require({k: functions[k] for k in case['function_population']} == case['function_population'], 'population summary')
        sources = {Path(f['path']).name: f['sha256'] for f in case['files']}
        require(all(e['source_sha256'] == sources[e['path']] for e in functions['entries']), 'source identity changed')
        if functions['status'] == 'partial':
            require(base['status'] == 'partial' or functions['omitted_spaces'] > 0 or
                    functions['invalid_span_spaces'] > 0 or functions['partial_files'] > 0, 'unexplained partial status')
            base['status'] = 'partial'
        require(enriched == base, 'existing structure fields changed')
        require(enriched['parse_count'] == len(case['files']), 'parse count changed')
        if 'old_worker' in outputs:
            require(without_timings(outputs['old_worker']) == without_timings(outputs['new_worker']), 'default worker differs')
            native = json.loads(outputs['functions_worker'])['functions']
            require([{k: v for k, v in e.items() if k not in {'path', 'language', 'source_sha256'}}
                     for e in functions['entries']] == native['entries'], 'CLI/provider disagreement')


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--protocol', type=Path, required=True)
    parser.add_argument('--benchmark-directory', type=Path, required=True)
    args = parser.parse_args()
    verify_protocol(args.protocol)
    verify_benchmark(args.benchmark_directory)
    print('Retained protocol and benchmark evidence verified.')


if __name__ == '__main__':
    main()
