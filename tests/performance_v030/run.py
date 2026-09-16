#!/usr/bin/env python3
"""Measure unchanged language mode and optional project mapping on the same corpus."""
import argparse
import datetime
import hashlib
import importlib.util
import json
import math
import os
from pathlib import Path
import platform
import statistics
import subprocess

ROOT = Path(__file__).resolve().parents[2]


def load(name, path):
    spec = importlib.util.spec_from_file_location(name, path)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


compat = load('v030_compat', ROOT/'tests/compatibility_v030/run.py')
measure = load('metrics_measure', ROOT/'tests/metrics/compare_language.py')


def capture(command):
    result = subprocess.run(command, capture_output=True, timeout=600)
    if result.returncode:
        raise RuntimeError(f'{command}: exit {result.returncode}: {result.stderr[:2000]!r}')
    return result.stdout, result.stderr


def summarize(rows):
    values = sorted(row['seconds'] for row in rows)
    return {'runs': len(rows), 'median_seconds': statistics.median(values),
        'p95_seconds': values[math.ceil(.95*len(values))-1],
        'min_seconds': min(values), 'max_seconds': max(values),
        'max_peak_rss_bytes': max(row['peak_rss_bytes'] for row in rows)}


def project_summary(report):
    projects = report['projects']
    configurations = report['configurations']
    refs = [ref for project in projects for ref in project['references']]
    refs += [ref for config in configurations for ref in config['references']]
    manifests = {evidence for project in projects for evidence in project['evidence']}
    manifests.update(config['path'] for config in configurations)
    composition = report['composition']
    total_files = sum(role['files'] for role in composition)
    total_bytes = sum(role['bytes'] for role in composition)
    assert total_files == sum(project['files'] for project in projects) + report['unassigned']['files'] + report['ambiguous']['files']
    assert total_bytes == sum(project['bytes'] for project in projects) + report['unassigned']['bytes'] + report['ambiguous']['bytes']
    kinds = {}
    for project in projects:
        kinds[project['kind']] = kinds.get(project['kind'], 0)+1
    return {'status': report['status'], 'project_roots': len(projects),
        'distinct_project_directories': len({p['root'] for p in projects}),
        'project_kinds': kinds, 'observed_manifests': len(manifests),
        'configurations': len(configurations), 'references': len(refs),
        'missing_reference_targets': sum(r.get('target_status') == 'missing' for r in refs),
        'unresolved_reference_targets': sum(r.get('target_status') == 'unresolved' for r in refs),
        'diagnostics': len(report['diagnostics']), 'omitted_files': report['omitted_files'],
        'selected_files': total_files, 'selected_bytes': total_bytes,
        'unassigned': report['unassigned'], 'ambiguous': report['ambiguous'],
        'composition': composition, 'count_invariants_passed': True}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--baseline', type=Path, required=True)
    parser.add_argument('--candidate', type=Path, required=True)
    parser.add_argument('--project', action='append', required=True, help='NAME=PATH')
    parser.add_argument('--source', choices=('auto', 'git', 'directory'), default='git')
    parser.add_argument('--runs', type=int, default=3)
    parser.add_argument('--warmup', type=int, default=1)
    parser.add_argument('--output', type=Path, required=True)
    args = parser.parse_args()
    if args.runs < 3 or args.warmup < 1:
        parser.error('use at least three measurements and one warmup')
    binaries = {'baseline': str(args.baseline.resolve()), 'candidate': str(args.candidate.resolve())}
    report = {'schema_version': '1.0.0', 'started_at_utc': datetime.datetime.now(datetime.timezone.utc).isoformat(),
        'binaries': {name: {'path': binary, 'sha256': compat.sha(binary)} for name, binary in binaries.items()},
        'source_provenance': compat.source_provenance(),
        'harnesses': {str(path.relative_to(ROOT)): compat.sha(path) for path in
            (Path(__file__), ROOT/'tests/compatibility_v030/run.py', ROOT/'tests/metrics/compare_language.py')},
        'environment': {'platform': platform.platform(), 'machine': platform.machine(), 'cpu_count': os.cpu_count(),
            'load_average_start': os.getloadavg()},
        'methodology': {'runs': args.runs, 'warmups': args.warmup, 'cache': 'warm; no OS cache flush',
            'order': 'alternating forward and reverse command order',
            'wall': 'perf_counter around system time launcher; target stdout discarded during timing',
            'memory': 'system time peak child RSS, normalized to bytes',
            'tail_limitation': 'nearest-rank p95 from three runs is the observed maximum, not a reliable tail estimate',
            'scope': 'language stdout/stderr exact comparison before timing; project mode performs additional work',
            'isolation': 'no heavy concurrent builds or tests requested; host not exclusively reserved'},
        'projects': [], 'passed': False}
    args.output.parent.mkdir(parents=True, exist_ok=True)
    for specification in args.project:
        name, folder = specification.split('=', 1)
        folder = str(Path(folder).resolve())
        commands = {name: [binary, '--json', '--source', args.source, folder] for name, binary in binaries.items()}
        outputs = {tool: capture(command) for tool, command in commands.items()}
        if outputs['baseline'] != outputs['candidate']:
            raise RuntimeError(f'{name}: language stdout/stderr mismatch; timing withheld')
        commands['projects'] = [binaries['candidate'], 'analyze', 'projects', '--json', '--source', args.source, folder]
        project_output, project_stderr = capture(commands['projects'])
        profile = json.loads(project_output)
        entry = {'name': name, 'path': folder, 'source': args.source, 'commands': commands,
            'language_match': True, 'language_stdout_sha256': hashlib.sha256(outputs['baseline'][0]).hexdigest(),
            'language_bytes': sum(value['size'] for value in json.loads(outputs['baseline'][0]).values()),
            'projects_stdout_sha256': hashlib.sha256(project_output).hexdigest(),
            'projects_stderr': project_stderr.decode(), 'inventory': project_summary(profile['projects'])}
        revision = subprocess.run(['git', '-C', folder, 'rev-parse', 'HEAD'], capture_output=True, text=True)
        if revision.returncode == 0:
            entry['git_commit'] = revision.stdout.strip()
            entry['worktree_status'] = subprocess.check_output(['git', '-C', folder, 'status', '--porcelain']).decode()
        for _ in range(args.warmup):
            for command in commands.values():
                measure.measured(command)
        samples = {name: [] for name in commands}
        for iteration in range(args.runs):
            order = list(commands) if iteration % 2 == 0 else list(reversed(commands))
            for tool in order:
                samples[tool].append(measure.measured(commands[tool]))
        entry['samples'] = samples
        entry['summary'] = {tool: summarize(rows) for tool, rows in samples.items()}
        entry['language_candidate_to_baseline_p95_ratio'] = entry['summary']['candidate']['p95_seconds']/entry['summary']['baseline']['p95_seconds']
        entry['projects_to_language_candidate_median_ratio'] = entry['summary']['projects']['median_seconds']/entry['summary']['candidate']['median_seconds']
        report['projects'].append(entry)
        args.output.write_text(json.dumps(report, indent=2)+'\n')
        print(json.dumps({'project': name, 'summary': entry['summary'], 'inventory': entry['inventory'],
            'language_candidate_to_baseline_p95_ratio': entry['language_candidate_to_baseline_p95_ratio']}), flush=True)
    report['passed'] = True
    report['finished_at_utc'] = datetime.datetime.now(datetime.timezone.utc).isoformat()
    report['environment']['load_average_finish'] = os.getloadavg()
    args.output.write_text(json.dumps(report, indent=2)+'\n')


if __name__ == '__main__':
    main()
