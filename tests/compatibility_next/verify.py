#!/usr/bin/env python3
"""Verify a compatibility receipt's captures, declared counts and reference checks."""
import argparse
import base64
import gzip
import hashlib
import json
from pathlib import Path
import run


def decode(record):
    result = []
    for key in ('stdout', 'stderr'):
        assert (key in record) != (key + '_base64' in record), 'ambiguous capture encoding'
        value = record[key].encode('utf-8') if key in record else base64.b64decode(record[key + '_base64'], validate=True)
        assert hashlib.sha256(value).hexdigest() == record[key + '_sha256'], 'capture digest mismatch'
        result.append(value)
    assert type(record['exit']) is int
    return record['exit'], *result


def verify(report):
    assert report['schema_version'] == '1.0.0'
    assert len({row['id'] for row in report['cases']}) == len(report['cases'])
    expected = {'v020-retained': 118, 'v030-projects': 55, 'v030-structure-validation': 6}
    if report['worker']:
        expected['v030-structure-native'] = 30
        assert len(report['structural_languages']) == 20 and report['structural_fixtures'] == 21
        assert not report['untested']
    else:
        assert report['untested'] and not report['structural_languages']
    matches, coverage = 0, {}
    manifest = json.loads((run.BREADTH / 'fixtures.json').read_text())
    for row in report['cases']:
        baseline = decode(row['baseline'])
        run.check_reference(row['id'], baseline, manifest)
        candidate = baseline if row['candidate'] == {'identical_to_baseline': True} else decode(row['candidate'])
        equal = baseline == candidate
        assert equal == row['equal'], 'incorrect equality result'
        matches += equal
        group = coverage.setdefault(row['group'], {'total': 0, 'passed': 0})
        group['total'] += 1
        group['passed'] += equal
    for row in report['interface_changes']:
        decode(row['baseline'])
        decode(row['candidate'])
    assert {group: values['total'] for group, values in coverage.items()} == expected
    assert report['coverage'] == coverage
    assert report['total'] == sum(expected.values())
    assert report['exact_matches'] == matches
    assert report['passed'] == (matches == report['total'])
    return {'passed': report['passed'], 'exact_matches': matches, 'total': report['total'], 'coverage': coverage}


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
