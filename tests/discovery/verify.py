#!/usr/bin/env python3
"""Verify discovery benchmark receipts and retained process outputs."""
import argparse
import gzip
import hashlib
import json
from pathlib import Path
import statistics

from benchmark import digest, inventory


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--report', type=Path, required=True)
    parser.add_argument('--verify-corpus', action='store_true', help='also rehash the original corpus paths')
    args = parser.parse_args()
    receipt = json.loads(gzip.decompress(args.report.read_bytes()))
    assert receipt['passed']
    assert receipt['harness_sha256'] == digest(Path(__file__).with_name('benchmark.py'))
    expected_cases = {'xml-only', 'xml-and-dotnet', 'small-source', 'spring-framework-directory',
                      'spring-framework-git', 'roslyn-directory', 'roslyn-git'}
    assert {case['name'] for case in receipt['cases']} == expected_cases
    for case in receipt['cases']:
        outputs = {}
        for kind in ('old_languages', 'new_languages', 'old_projects', 'new_projects', 'discovery'):
            filename = f'{case["name"]}-{kind}.json.gz'
            path = args.report.parent / filename
            assert digest(path) == case['artifacts'][filename]
            output = gzip.decompress(path.read_bytes())
            outputs[kind] = output
            samples = case['samples'][kind]
            assert len(samples) == 3 and all(s['seconds'] > 0 and s['peak_rss_bytes'] > 0 for s in samples)
            assert all(s['stdout_sha256'] == hashlib.sha256(output).hexdigest() for s in samples)
            summary = case['summary'][kind]
            assert summary['median_seconds'] == statistics.median(s['seconds'] for s in samples)
            assert summary['max_peak_rss_bytes'] == max(s['peak_rss_bytes'] for s in samples)
        assert outputs['old_languages'] == outputs['new_languages']
        assert outputs['old_projects'] == outputs['new_projects']
        report = json.loads(outputs['discovery'])
        d = report['discovery']
        assert d == case['discovery'] and d['source']['mode'] == case['source']
        assert d['inventory'] == {k: case['input'][k] for k in ('files', 'bytes')}
        assert sum(g['files'] for g in d['categories']) == d['inventory']['files']
        assert sum(g['bytes'] for g in d['categories']) == d['inventory']['bytes']
        assert not report['languages'] and d['classification_bytes_read'] == 0
        assert d['status'] != 'skipped' and not d['omissions'].get('tree_size_limit')
        if case['name'] == 'xml-only':
            assert d['inventory'] == {'files': 128, 'bytes': 2147483648} and not d['candidates']
        if case['name'] == 'xml-and-dotnet':
            assert any(c['path'] == 'App.csproj' and c['kind'] == 'manifest' for c in d['candidates'])
        if case['name'] == 'small-source':
            assert any(c['path'] == 'go.mod' and c['kind'] == 'manifest' for c in d['candidates'])
        if args.verify_corpus:
            assert inventory(Path(case['path']), case['source']) == case['input'], case['name']
    print('Verified 7 cases, 105 measured outputs, legacy equality, inventory totals, and candidate preservation')


if __name__ == '__main__':
    main()
