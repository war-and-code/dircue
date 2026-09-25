#!/usr/bin/env python3
"""Check optional project mapping on the existing large synthetic stress fixtures."""
import argparse
import datetime
import gzip
import hashlib
import json
import os
from pathlib import Path
import platform
import subprocess

import run as performance


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--candidate', type=Path, required=True)
    parser.add_argument('--fixtures', type=Path, required=True)
    parser.add_argument('--output', type=Path, required=True)
    args = parser.parse_args()
    binary = args.candidate.resolve()
    manifest_path = args.fixtures/'manifest.json'
    manifest = json.loads(manifest_path.read_text())
    assert manifest['profile'] == 'acceptance'
    report = {'schema_version': '1.0.0', 'started_at_utc': datetime.datetime.now(datetime.timezone.utc).isoformat(),
        'candidate_sha256': performance.compat.sha(binary), 'harness_sha256': performance.compat.sha(__file__),
        'fixture_manifest_sha256': performance.compat.sha(manifest_path),
        'source_provenance': performance.compat.source_provenance(),
        'environment': {'platform': platform.platform(), 'machine': platform.machine(),
            'cpu_count': os.cpu_count(), 'cgroups': {}},
        'methodology': {'scope': 'optional project mapping, no structural parsing or scc metrics requested',
            'timing': 'one diagnostic wall/CPU/RSS run per fixture; not a stable performance estimate',
            'fixtures': 'existing synthetic fixtures; current file sizes/allocated blocks checked, content hashes not reread',
            'storage': 'read-only fixture and source mounts; report output is the only writable mount',
            'network': 'Docker --network none required by invocation'},
        'cases': [], 'passed': False}
    for file in ('/sys/fs/cgroup/memory.max', '/sys/fs/cgroup/cpu.max'):
        if Path(file).exists():
            report['environment']['cgroups'][file] = Path(file).read_text().strip()
    args.output.parent.mkdir(parents=True, exist_ok=True)
    artifacts = args.output.parent/'stress-artifacts'
    artifacts.mkdir(exist_ok=True)
    selected = {'etl-pipeline': 'generated-excluded', 'xml-log': 'default', 'dotnet-graph': 'default'}
    for fixture in manifest['fixtures']:
        name = fixture['name']
        if name not in selected:
            continue
        variant = next(v for v in fixture['variants'] if v['name'] == selected[name])
        root = args.fixtures/variant['flat']
        sizes = []
        allocated = 0
        for file in variant['files']:
            stat = (root/file['path']).stat()
            assert stat.st_size == file['bytes'], file['path']
            sizes.append(stat.st_size)
            allocated += stat.st_blocks*512
        command = [str(binary), 'analyze', 'projects', '--json', '--source', 'directory', str(root)]
        stdout, stderr = performance.capture(command)
        profile = json.loads(stdout)
        assert 'structure' not in profile and 'metrics' not in profile
        inventory = performance.project_summary(profile['projects'])
        assert inventory['selected_files'] == len(sizes), (name, inventory['selected_files'], len(sizes))
        assert inventory['selected_bytes'] == sum(sizes), (name, inventory['selected_bytes'], sum(sizes))
        assert inventory['omitted_files'] == 0, inventory
        plain, _ = performance.capture([str(binary), '--json', '--source', 'directory', str(root)])
        languages = {row['name']: row['bytes'] for row in profile['languages']}
        assert languages == {language: row['size'] for language, row in json.loads(plain).items()}
        if name == 'etl-pipeline':
            assert sum(sizes) >= 2*1024**3
            assert inventory['project_kinds'].get('maven', 0) >= 1
        elif name == 'xml-log':
            assert max(sizes) > 1024**3
            assert 'XML' not in languages
            assert inventory['project_roots'] == 0
            assert any(role['name'] == 'data' and role['bytes'] > 1024**3 for role in inventory['composition'])
        else:
            assert inventory['project_kinds'].get('dotnet') == 2048, inventory['project_kinds']
        artifact = artifacts/(name+'.json.gz')
        with artifact.open('wb') as target:
            with gzip.GzipFile(filename='', mode='wb', fileobj=target, mtime=0) as zipped:
                zipped.write(stdout)
        report['cases'].append({'name': name, 'variant': variant['name'], 'path': str(root),
            'logical_bytes': sum(sizes), 'allocated_bytes': allocated, 'fixture_files': len(sizes),
            'inventory': inventory, 'languages': languages, 'language_mode_agreement': True,
            'structure_absent': True, 'metrics_absent': True, 'stderr': stderr.decode(),
            'sample': performance.measure.measured(command),
            'artifact': str(artifact.relative_to(args.output.parent)),
            'artifact_sha256': performance.compat.sha(artifact),
            'stdout_sha256': hashlib.sha256(stdout).hexdigest()})
        args.output.write_text(json.dumps(report, indent=2)+'\n')
        print(json.dumps({'case': name, 'inventory': inventory, 'sample': report['cases'][-1]['sample']}), flush=True)
    assert len(report['cases']) == len(selected)
    report['passed'] = True
    report['finished_at_utc'] = datetime.datetime.now(datetime.timezone.utc).isoformat()
    args.output.write_text(json.dumps(report, indent=2)+'\n')


if __name__ == '__main__':
    main()
