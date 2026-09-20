#!/usr/bin/env python3
"""Recompute raw-capture equality and coverage from a 0.5 compatibility receipt."""
import argparse
import base64
from collections import Counter
import gzip
import hashlib
import json
from pathlib import Path


def decode(record):
    values = []
    for key in ('stdout', 'stderr'):
        assert (key in record) != (key + '_base64' in record), 'ambiguous capture encoding'
        value = record[key].encode() if key in record else base64.b64decode(record[key + '_base64'], validate=True)
        assert hashlib.sha256(value).hexdigest() == record[key + '_sha256'], 'capture digest mismatch'
        values.append(value)
    assert type(record['exit']) is int
    return record['exit'], *values


def verify(report):
    assert report['schema_version'] == '1.0.0' and report['baseline_release'] == 'v0.5.0'
    assert report['build_receipt']['candidate_sha256'] == report['candidate_sha256']
    expected = {'v020-retained': 118, 'v030-projects': 55, 'v030-structure-validation': 6,
                'v040-modules': 29, 'v050-declarations': 37}
    if report.get('worker_sha256'):
        expected.update({'v040-modules': 32, 'v030-structure-native': 30})
        assert not report['untested']
    else:
        assert report['untested']
    assert len({row['id'] for row in report['cases']}) == len(report['cases']), 'duplicate case identifiers'
    assert dict(Counter(row['group'] for row in report['cases'])) == expected, 'case coverage differs'
    passed = 0
    for row in report['cases']:
        before = decode(row['baseline'])
        after = before if row['candidate'] == {'identical_to_baseline': True} else decode(row['candidate'])
        equal = before == after
        assert row['equal'] is equal, 'reported equality differs'
        passed += equal
    for row in report['interface_changes']:
        decode(row['baseline'])
        decode(row['candidate'])
    assert report['total'] == sum(expected.values()) == len(report['cases'])
    assert report['exact_matches'] == passed and report['passed'] is (passed == report['total'])
    return {'passed': report['passed'], 'total': report['total'], 'exact_matches': passed, 'coverage': expected}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('receipt', type=Path)
    args = parser.parse_args()
    payload = args.receipt.read_bytes()
    report = json.loads(gzip.decompress(payload) if args.receipt.suffix == '.gz' else payload)
    result = verify(report)
    print(json.dumps(result))
    raise SystemExit(0 if result['passed'] else 1)


if __name__ == '__main__':
    main()
