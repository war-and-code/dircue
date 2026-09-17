#!/usr/bin/env python3
"""Verify staged-workflow receipts, retained outputs, and deterministic fixture bytes."""
import argparse
import gzip
import hashlib
import json
from pathlib import Path
import statistics


def digest(path):
    result = hashlib.sha256()
    with path.open('rb') as source:
        while chunk := source.read(1024 * 1024):
            result.update(chunk)
    return result.hexdigest()


def require(condition, message):
    if not condition:
        raise AssertionError(message)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--report', type=Path, required=True)
    args = parser.parse_args()
    raw = args.report.read_bytes()
    receipt = json.loads(gzip.decompress(raw) if args.report.suffix == '.gz' else raw)
    require(receipt['passed'], 'measurement did not finish')
    require(receipt['candidate_sha256'] == receipt['candidate_sha256_after'], 'candidate changed')
    require(receipt['harness_sha256'] == digest(Path(__file__).with_name('benchmark.py')), 'measurement harness changed')
    cases = {case['name']: case for case in receipt['cases']}
    require(set(cases) == {'xml-only', 'xml-and-dotnet', 'small-source', 'spring-framework', 'roslyn'}, 'case set changed')
    for case in cases.values():
        for name, checksum in case['artifacts'].items():
            require(digest(args.report.parent / name) == checksum, f'{name}: retained output changed')
            json.loads(gzip.decompress((args.report.parent / name).read_bytes()))
        for kind, samples in case['samples'].items():
            require(len(samples) == 3, 'expected three measured samples')
            summary = case['summary'][kind]
            require(summary['median_seconds'] == statistics.median(s['seconds'] for s in samples), 'median differs')
            require(summary['peak_rss_bytes'] == max(s['peak_rss_bytes'] for s in samples), 'RSS summary differs')
            require(all(s['seconds'] > 0 and s['peak_rss_bytes'] > 0 for s in samples), 'invalid measurement')
    expected_size = 16 * 1024 * 1024
    line = b'<entry timestamp="2026-01-01T00:00:00Z">example deterministic log record</entry>\n'
    inner = expected_size - len(b'<logs>\n</logs>\n')
    expected_xml = b'<logs>\n' + line * (inner // len(line)) + b' ' * (inner % len(line)) + b'</logs>\n'
    expected_hash = hashlib.sha256(expected_xml).hexdigest()
    verified_inodes = {}
    for name in ['xml-only', 'xml-and-dotnet']:
        root = Path(cases[name]['path'])
        xml_files = sorted(root.glob('*.xml'))
        require(len(xml_files) == 128, f'{name}: wrong file count')
        for path in xml_files:
            stat = path.stat()
            identity = (stat.st_dev, stat.st_ino)
            if identity not in verified_inodes:
                verified_inodes[identity] = digest(path)
            require(stat.st_size == expected_size and verified_inodes[identity] == expected_hash, f'{path}: XML bytes changed')
        roles = cases[name]['observations']['composition']
        data_bytes = sum(role['bytes'] for role in roles if role['name'] == 'data')
        require(data_bytes == 2 * 1024 ** 3, f'{name}: XML byte accounting differs')
        require(cases[name]['observations']['omitted_files'] == 0, f'{name}: partial inventory')
    require(cases['xml-only']['observations']['project_count'] == 0, 'XML-only invented projects')
    require(cases['xml-only']['observations']['metrics']['totals']['files'] == 0, 'default metrics counted XML')
    require(cases['xml-and-dotnet']['observations']['project_count'] >= 1, 'mixed XML obscured project')
    require(cases['xml-and-dotnet']['observations']['metrics']['totals']['files'] >= 1, 'mixed source lost')
    require(not any(s['follow_up'] for s in cases['xml-only']['samples']['staged']), 'XML-only follow-up ran')
    for name in set(cases) - {'xml-only'}:
        require(all(s['follow_up'] for s in cases[name]['samples']['staged']), f'{name}: source follow-up skipped')
    source_hashes = {}
    for name in ['small-source', 'xml-and-dotnet']:
        root = Path(cases[name]['path'])
        for path in sorted(root.iterdir()):
            if path.suffix != '.xml':
                source_hashes[f'{name}/{path.name}'] = digest(path)
    small = Path(cases['small-source']['path'])
    require(len(list(small.glob('*.go'))) == 200, 'small source count differs')
    for index in range(200):
        expected = 'package sample\n' + ''.join(f'func Value{index}_{number}(x int) int {{ if x > 0 {{ return x }}; return 0 }}\n' for number in range(100))
        require((small / f'source{index:03}.go').read_bytes() == expected.encode(), 'small fixture changed')
    receipt['postverification'] = {'passed': True, 'verifier_sha256': digest(Path(__file__)),
                                   'xml_logical_bytes_per_case': 2 * 1024 ** 3,
                                   'xml_file_count_per_case': 128, 'xml_file_sha256': expected_hash,
                                   'unique_xml_inodes_hashed': len(verified_inodes),
                                   'source_fixture_sha256': source_hashes,
                                   'checks': ['retained_output_checksums', 'sample_summaries', 'fully_written_xml_content',
                                              'exact_xml_inventory_bytes', 'mixed_project_discovery',
                                              'default_metrics_xml_exclusion', 'conditional_followup', 'source_fixture_content']}
    encoded = (json.dumps(receipt, indent=2) + '\n').encode()
    args.report.write_bytes(gzip.compress(encoded, mtime=0) if args.report.suffix == '.gz' else encoded)
    print('Verified output artifacts, measurements, 2 GiB XML content/accounting, and mixed-project preservation')


if __name__ == '__main__':
    main()
