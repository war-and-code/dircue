#!/usr/bin/env python3
"""Recompute retained comparison summaries and check hashes of supporting artifacts."""
import gzip
import hashlib
import json
import math
from pathlib import Path
import statistics

ROOT = Path(__file__).resolve().parents[2]


def sha(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def main():
    compatibility = json.loads((ROOT/'tests/compatibility_v030/results.json').read_text())
    performance = json.loads((ROOT/'tests/performance_v030/results.json').read_text())
    stress = json.loads((ROOT/'tests/performance_v030/stress-results.json').read_text())
    checks = []
    assert compatibility['passed'] and compatibility['total'] == 118
    assert compatibility['exact_matches'] == sum(row['baseline'] == row['candidate'] for row in compatibility['cases']) == 118
    checks.append('118 retained legacy stdout/stderr/exit comparisons are exact')
    final_path = ROOT/'tests/compatibility_v030/final-results.json'
    if final_path.exists():
        final = json.loads(final_path.read_text())
        assert final['passed'] and final['total'] == final['exact_matches'] == 118
        assert all(row['baseline'] == row['candidate'] for row in final['cases'])
        assert final['baseline']['sha256'] == compatibility['baseline']['sha256']
        assert final['harness_sha256'] == sha(ROOT/'tests/compatibility_v030/run.py')
        checks.append('final Maven 4.1 candidate independently passes all 118 legacy comparisons')
    assert compatibility['harness_sha256'] == sha(ROOT/'tests/compatibility_v030/run.py')
    assert compatibility['candidate']['sha256'] == performance['binaries']['candidate']['sha256']
    assert compatibility['baseline']['sha256'] == performance['binaries']['baseline']['sha256']
    assert compatibility['source']['build_inputs_sha256'] == performance['source_provenance']['build_inputs_sha256'] == stress['source_provenance']['build_inputs_sha256']
    checks.append('checkpoint compatibility, native timing, and Linux stress receipts describe the same Go source snapshot')
    for name, expected in performance['harnesses'].items():
        assert sha(ROOT/name) == expected, name
    assert performance['passed'] and len(performance['projects']) == 3
    for project in performance['projects']:
        assert project['language_match'] and project['inventory']['count_invariants_passed']
        for tool, samples in project['samples'].items():
            values = sorted(sample['seconds'] for sample in samples)
            summary = project['summary'][tool]
            assert len(samples) == summary['runs'] == 3
            assert summary['median_seconds'] == statistics.median(values)
            assert summary['p95_seconds'] == values[math.ceil(.95*len(values))-1]
            assert summary['max_peak_rss_bytes'] == max(sample['peak_rss_bytes'] for sample in samples)
        assert project['language_candidate_to_baseline_p95_ratio'] == project['summary']['candidate']['p95_seconds']/project['summary']['baseline']['p95_seconds']
    checks.append('all native timing medians, observed p95 values, RSS maxima, and ratios recomputed')
    assert stress['passed'] and len(stress['cases']) == 3
    assert stress['harness_sha256'] == sha(ROOT/'tests/performance_v030/stress.py')
    assert stress['environment']['cgroups']['/sys/fs/cgroup/memory.max'] == '536870912'
    assert stress['environment']['cgroups']['/sys/fs/cgroup/cpu.max'] == '200000 100000'
    for case in stress['cases']:
        artifact = ROOT/'tests/performance_v030'/case['artifact']
        assert sha(artifact) == case['artifact_sha256']
        content = gzip.decompress(artifact.read_bytes())
        assert hashlib.sha256(content).hexdigest() == case['stdout_sha256']
        profile = json.loads(content)
        inventory = profile['projects']
        assert 'structure' not in profile and 'metrics' not in profile
        files = sum(role['files'] for role in inventory['composition'])
        size = sum(role['bytes'] for role in inventory['composition'])
        assert files == case['fixture_files'] == case['inventory']['selected_files']
        assert size == case['logical_bytes'] == case['inventory']['selected_bytes']
        assert files == sum(project['files'] for project in inventory['projects'])+inventory['unassigned']['files']+inventory['ambiguous']['files']
        assert size == sum(project['bytes'] for project in inventory['projects'])+inventory['unassigned']['bytes']+inventory['ambiguous']['bytes']
    checks.append('all compressed stress outputs verify hashes and ownership/composition totals under recorded CPU/memory limits')
    print(json.dumps({'passed': True, 'checks': checks}, indent=2))


if __name__ == '__main__':
    main()
