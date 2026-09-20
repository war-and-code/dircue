#!/usr/bin/env python3
"""Export bounded-reader measurements without local filesystem paths."""
import argparse
import hashlib
import json
from pathlib import Path
import statistics


def digest(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--receipt', type=Path, required=True)
    parser.add_argument('--baseline-build', type=Path, required=True)
    parser.add_argument('--candidate-build', type=Path, required=True)
    parser.add_argument('--output', type=Path, required=True)
    args = parser.parse_args()
    report = json.loads(args.receipt.read_text())
    baseline = json.loads(args.baseline_build.read_text())
    candidate = json.loads(args.candidate_build.read_text())
    if not report.get('passed') or not report.get('cases'):
        parser.error('a completed successful benchmark receipt is required')
    for label, build in [('baseline', baseline), ('candidate', candidate)]:
        if build['candidate_sha256'] != report[label + '_sha256']:
            parser.error(label + ' build does not identify the measured executable')
    if candidate != report['build_receipt']:
        parser.error('candidate build receipt differs from the measured one')
    root = Path(__file__).resolve().parents[3]
    replacements = {str(root): '<project>', str(Path.home()): '<home>'}
    for case in report['cases']:
        replacements[case['path']] = '<corpus:' + case['name'] + '>'
        for lane, command in case['commands'].items():
            replacements[command[0]] = '<baseline>' if lane.startswith('baseline_') else '<candidate>'
        expected_count = report['method']['repetitions']
        for lane, samples in case['samples'].items():
            if len(samples) != expected_count or any(s['exit_code'] != 0 for s in samples):
                parser.error('incomplete or unsuccessful measured lane')
            expected = case['summary'][lane]
            if statistics.median(s['seconds'] for s in samples) != expected['median_seconds']:
                parser.error('time summary does not reproduce')
            if statistics.median(s['peak_rss_bytes'] for s in samples) != expected['median_peak_rss_bytes']:
                parser.error('RSS summary does not reproduce')
        for mode in ('languages', 'all'):
            hashes = {s['stdout_sha256'] for lane in ('baseline_' + mode, 'candidate_' + mode)
                      for s in case['samples'][lane] + case['warmups'][lane]}
            if len(hashes) != 1:
                parser.error('existing command outputs differ')

    def clean(value):
        if isinstance(value, dict):
            return {key: clean(item) for key, item in value.items()}
        if isinstance(value, list):
            return [clean(item) for item in value]
        if isinstance(value, str):
            for before, after in sorted(replacements.items(), key=lambda pair: -len(pair[0])):
                value = value.replace(before, after)
        return value

    report.pop('build_receipt')
    report['schema'] = 'dircue-bounded-reader-benchmark-1'
    report['baseline_build'] = baseline
    report['candidate_build'] = candidate
    report['comparison_scope'] = {
        'baseline_release_runtime': 'v0.6.0',
        'reported_version_override': '0.5.0 in both executables, solely for exact output comparisons',
        'candidate_change': 'size-guided allocation for existing bounded file reads',
        'other_lanes': 'Optional format lanes measure candidate-only cost, not baseline-versus-candidate improvements.',
        'limits': 'Warm-cache single-host measurements; no universal speed or memory equivalence claim. XML scans read bounded prefixes, not the full corpus.',
    }
    report['export'] = {
        'source_receipt_sha256': digest(args.receipt),
        'baseline_build_receipt_sha256': digest(args.baseline_build),
        'candidate_build_receipt_sha256': digest(args.candidate_build),
        'transformations': ['Replaced local executable, corpus, workspace and home paths with placeholders. Retained every timing/RSS sample and measured output hash.'],
    }
    serialized = json.dumps(clean(report), indent=2) + '\n'
    if '/Users/' in serialized or str(Path.home()) in serialized:
        parser.error('unredacted local path remains')
    args.output.parent.mkdir(parents=True, exist_ok=True)
    with args.output.open('x') as stream:
        stream.write(serialized)
    print(args.output)


if __name__ == '__main__':
    main()
