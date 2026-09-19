#!/usr/bin/env python3
"""Verify retained default-path samples against captured complete JSON outputs."""
import argparse
import gzip
import hashlib
import json
from pathlib import Path
import statistics


def sha(data):
    return hashlib.sha256(data).hexdigest()


def verify(path):
    report = json.loads(gzip.decompress(path.read_bytes()))
    assert report['passed']
    if 'initial_receipt_sha256' in report:
        return verify_isolation(path, report)
    count = report['method']['repetitions']
    assert count >= 5 and report['method']['warmups'] == 1
    assert report['candidate_sha256'] == report['candidate_build_provenance']['candidate_sha256']
    assert report['candidate_version'] == report['baseline_version']
    assert sorted(case['name'] for case in report['cases']) == [
        'cobra-git', 'roslyn-directory', 'roslyn-git', 'spring-framework-git', 'xml-and-dotnet']
    total = 0
    for case in report['cases']:
        assert len(case['execution_order']) == count * 4
        for mode in ('languages', 'projects'):
            name = case['name'] + '-' + mode + '.json.gz'
            compressed = (path.parent / name).read_bytes()
            assert sha(compressed) == case['artifacts'][name]
            payload = gzip.decompress(compressed)
            parsed = json.loads(payload)
            assert isinstance(parsed, dict)
            for generation in ('baseline', 'candidate'):
                key = generation + '_' + mode
                command = case['commands'][key]
                common = ['--json', '--source', case['source'], '--workers', '8', '--tree-size', '1000000', case['path']]
                assert command == [report[generation], *(['analyze', 'all', '--projects'] if mode == 'projects' else []), *common]
                samples = case['samples'][key]
                assert len(samples) == count
                assert [s['round'] for s in samples] == list(range(1, count + 1))
                assert case['execution_order'].count(key) == count
                for sample in [case['warmups'][key], *samples]:
                    assert sample['stdout_sha256'] == sha(payload)
                    assert sample['stderr_sha256'] == sha(b'') and sample['exit_code'] == 0
                    assert sample['seconds'] > 0 and sample['peak_rss_bytes'] > 0
                summary = case['summary'][key]
                assert summary['median_seconds'] == statistics.median(s['seconds'] for s in samples)
                assert summary['median_peak_rss_bytes'] == statistics.median(s['peak_rss_bytes'] for s in samples)
                assert summary['max_peak_rss_bytes'] == max(s['peak_rss_bytes'] for s in samples)
                total += len(samples)
            old = case['samples']['baseline_' + mode]
            new = case['samples']['candidate_' + mode]
            comparison = case['comparisons'][mode]
            assert comparison['paired_time_change_percent'] == [100 * (n['seconds'] / o['seconds'] - 1) for o, n in zip(old, new)]
            for metric, field in [('median_seconds', 'median_time_change_percent'), ('median_peak_rss_bytes', 'median_rss_change_percent')]:
                assert comparison[field] == 100 * (case['summary']['candidate_' + mode][metric] / case['summary']['baseline_' + mode][metric] - 1)
        if case['name'] == 'xml-and-dotnet':
            assert case['xml_files'] == 128 and case['xml_bytes'] == 2147483648
            assert case['input']['bytes'] > case['xml_bytes']
    return total


def verify_isolation(path, report):
    compressed = (path.parent / Path(report['output_artifact']).name).read_bytes()
    assert sha(compressed) == report['output_artifact_sha256']
    payload = gzip.decompress(compressed)
    assert report['commands']['candidate_a'] == report['commands']['candidate_b']
    assert report['binary_sha256']['candidate_a'] == report['binary_sha256']['candidate_b']
    assert len(set(report['versions'].values())) == 1
    keys = ['baseline', 'intermediate', 'candidate_a', 'candidate_b']
    expected_order = [key for index in range(5) for key in keys[index % 4:] + keys[:index % 4]]
    assert report['execution_order'] == expected_order
    for key in keys:
        samples = report['samples'][key]
        assert [s['round'] for s in samples] == list(range(1, 6))
        assert report['commands'][key][1:] == report['commands']['baseline'][1:]
        for sample in [report['warmups'][key], *samples]:
            assert sample['stdout_sha256'] == sha(payload)
            assert sample['stderr_sha256'] == sha(b'') and sample['exit_code'] == 0
            assert sample['seconds'] > 0 and sample['peak_rss_bytes'] > 0
        assert report['summary'][key]['median_seconds'] == statistics.median(s['seconds'] for s in samples)
        assert report['summary'][key]['median_peak_rss_bytes'] == statistics.median(s['peak_rss_bytes'] for s in samples)
    return 20


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('receipt', type=Path)
    print(f'{verify(parser.parse_args().receipt)} measured outputs verified')
