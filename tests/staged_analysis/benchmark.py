#!/usr/bin/env python3
"""Measure first-pass and staged code-metric analysis without executing repository code."""
import argparse
from datetime import datetime, timezone
import gzip
import hashlib
import json
import os
from pathlib import Path
import platform
import re
import statistics
import subprocess
import time

ROOT = Path(__file__).resolve().parents[2]


def sha(data):
    return hashlib.sha256(data).hexdigest()


def run(command):
    return subprocess.check_output(command, text=True).strip()


def prepare(root):
    marker = root / 'fixture-spec.json'
    spec = {'xml_files': 128, 'xml_bytes_per_file': 16 * 1024 * 1024,
            'source_files': 200, 'source_lines_per_file': 100}
    if marker.exists():
        if json.loads(marker.read_text()) != spec:
            raise ValueError('fixture specification changed; choose a new fixture directory')
        return spec
    if root.exists():
        raise ValueError('refusing to overwrite an existing fixture directory')
    root.mkdir(parents=True)
    xml = root / 'xml-only'
    xml.mkdir()
    line = b'<entry timestamp="2026-01-01T00:00:00Z">example deterministic log record</entry>\n'
    size = spec['xml_bytes_per_file']
    inner = size - len(b'<logs>\n</logs>\n')
    body = b'<logs>\n' + line * (inner // len(line)) + b' ' * (inner % len(line)) + b'</logs>\n'
    for index in range(spec['xml_files']):
        (xml / f'events-{index:03}.xml').write_bytes(body)
    mixed = root / 'xml-and-dotnet'
    mixed.mkdir()
    for path in xml.iterdir():
        os.link(path, mixed / path.name)
    (mixed / 'App.csproj').write_text('<Project Sdk="Microsoft.NET.Sdk"><PropertyGroup><TargetFramework>net8.0</TargetFramework></PropertyGroup></Project>\n')
    (mixed / 'Program.cs').write_text('namespace Example; class Program { static int Twice(int x) => x * 2; }\n')
    small = root / 'small-source'
    small.mkdir()
    (small / 'go.mod').write_text('module example.invalid/staged\n\ngo 1.26\n')
    for index in range(spec['source_files']):
        source = 'package sample\n' + ''.join(f'func Value{index}_{number}(x int) int {{ if x > 0 {{ return x }}; return 0 }}\n' for number in range(spec['source_lines_per_file']))
        (small / f'source{index:03}.go').write_text(source)
    marker.write_text(json.dumps(spec, indent=2) + '\n')
    return spec


def measured(command):
    start = time.perf_counter()
    proc = subprocess.run(['/usr/bin/time', '-l', *command], capture_output=True, timeout=120)
    elapsed = time.perf_counter() - start
    if proc.returncode:
        raise AssertionError(f'{command}: {proc.returncode}: {proc.stderr.decode(errors="replace")}')
    stderr = proc.stderr.decode()
    match = re.search(r'(\d+)\s+maximum resident set size', stderr)
    if not match:
        raise AssertionError(f'RSS missing: {stderr}')
    return json.loads(proc.stdout), {'seconds': elapsed, 'peak_rss_bytes': int(match.group(1)),
                                    'output_sha256': sha(proc.stdout), 'command': command}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--candidate', type=Path, required=True)
    parser.add_argument('--fixtures', type=Path, required=True)
    parser.add_argument('--corpus-root', type=Path, required=True)
    parser.add_argument('--output', type=Path, required=True)
    parser.add_argument('--prepare-only', action='store_true')
    args = parser.parse_args()
    if platform.system() != 'Darwin':
        parser.error('this diagnostic harness requires macOS /usr/bin/time -l (RSS in bytes)')
    spec = prepare(args.fixtures.resolve())
    if args.prepare_only:
        print('Prepared fully written XML fixtures and source samples')
        return
    candidate = args.candidate.resolve()
    pins = {p['name']: p for p in json.loads((ROOT / 'tests/performance/corpus.json').read_text())['projects']}
    cases = [(name, args.fixtures.resolve() / name, None) for name in ['xml-only', 'xml-and-dotnet', 'small-source']]
    for name in ['spring-framework', 'roslyn']:
        path = args.corpus_root.resolve() / name
        commit = run(['git', '-C', str(path), 'rev-parse', 'HEAD'])
        if commit != pins[name]['commit'] or run(['git', '-C', str(path), 'status', '--porcelain']):
            raise AssertionError(f'{name}: corpus differs from pinned clean checkout')
        cases.append((name, path, commit))
    receipt = {'started_at_utc': datetime.now(timezone.utc).isoformat(),
               'candidate_sha256': sha(candidate.read_bytes()), 'candidate_version': run([str(candidate), '--version']),
               'go_build_info': run(['go', 'version', '-m', str(candidate)]),
               'source_commit': run(['git', '-C', str(ROOT), 'rev-parse', 'HEAD']),
               'harness_sha256': sha(Path(__file__).read_bytes()), 'platform': platform.platform(),
               'hardware': {k: run(['sysctl', '-n', k]) for k in ['machdep.cpu.brand_string', 'hw.physicalcpu', 'hw.logicalcpu', 'hw.memsize']},
               'fixture_spec': spec, 'cases': [],
               'method': {'warmups_per_workflow': 1, 'measured_runs_per_workflow': 3,
                          'order': 'rotate first-pass, combined, staged after each repetition',
                          'source': 'directory', 'workers': 8, 'tree_size': 1000000,
                          'metrics_scope': 'source', 'metrics_max_file_bytes': 16777216,
                          'rss': 'maximum single dircue process RSS from macOS time -l; staged uses max of sequential children, excludes Python controller',
                          'staging': 'Python harness controller skips source metrics only for complete data-only composition, no project entries, and no warnings; all other cases run metrics. Not route.py timing.',
                          'wall': 'whole workflow including Python JSON decode and decision, subprocess launch, and time wrapper',
                          'limits': 'No CPU/RSS hard limit. Host activity uncontrolled; other project heavy tests paused. Three warm-cache observations are diagnostic, not a release speed claim.'}}
    args.output.parent.mkdir(parents=True, exist_ok=True)
    for name, path, commit in cases:
        base = [str(candidate), '--source', 'directory', '--json', '--workers', '8', '--tree-size', '1000000', str(path)]
        commands = {'first-pass': [base[0], 'analyze', 'all', '--projects', *base[1:]],
                    'combined': [base[0], 'analyze', 'all', '--projects', '--metrics', *base[1:]],
                    'follow-up': [base[0], 'analyze', 'metrics', *base[1:]]}
        expected = {}
        sample = {'name': name, 'path': str(path), 'commit': commit, 'samples': {k: [] for k in ['first-pass', 'combined', 'staged']}}
        def workflow(kind):
            started = time.perf_counter()
            outputs = []
            processes = []
            command_kind = 'first-pass' if kind == 'staged' else kind
            data, process = measured(commands[command_kind])
            outputs.append(data)
            processes.append(process)
            project = data['projects']
            data_only = bool(project['composition']) and all(role['name'] == 'data' for role in project['composition']) and not project['projects']
            follow_up = kind == 'staged' and (not data_only or project['status'] != 'complete' or bool(data['warnings']))
            if follow_up:
                more, process = measured(commands['follow-up'])
                outputs.append(more)
                processes.append(process)
            elapsed = time.perf_counter() - started
            payload = {'seconds': elapsed, 'peak_rss_bytes': max(p['peak_rss_bytes'] for p in processes),
                       'follow_up': follow_up, 'processes': processes}
            if command_kind in expected:
                if expected[command_kind] != outputs[0]:
                    raise AssertionError(f'{name}: nondeterministic {command_kind}')
            else:
                expected[command_kind] = outputs[0]
            if kind == 'combined':
                if {k: v for k, v in data.items() if k != 'metrics'} != expected['first-pass']:
                    raise AssertionError(f'{name}: combined changed first-pass output')
            if follow_up and outputs[1]['metrics'] != expected['combined']['metrics']:
                raise AssertionError(f'{name}: staged metrics differ from combined')
            return payload
        for kind in ['first-pass', 'combined', 'staged']:
            workflow(kind)
        for iteration in range(3):
            kinds = ['first-pass', 'combined', 'staged']
            for kind in kinds[iteration:] + kinds[:iteration]:
                sample['samples'][kind].append(workflow(kind))
        first = expected['first-pass']
        sample['observations'] = {'languages': first['languages'], 'summary': first['summary'],
                                   'project_status': first['projects']['status'],
                                   'project_count': len(first['projects']['projects']),
                                   'composition': first['projects']['composition'],
                                   'omitted_files': first['projects']['omitted_files'],
                                   'warning_count': len(first['warnings']),
                                   'metrics': expected['combined']['metrics']}
        sample['summary'] = {kind: {'median_seconds': statistics.median(s['seconds'] for s in runs),
                                          'peak_rss_bytes': max(s['peak_rss_bytes'] for s in runs),
                                          'min_seconds': min(s['seconds'] for s in runs), 'max_seconds': max(s['seconds'] for s in runs)}
                             for kind, runs in sample['samples'].items()}
        for kind, output in expected.items():
            destination = args.output.parent / f'{name}-{kind}.json.gz'
            content = json.dumps(output, sort_keys=True).encode()
            destination.write_bytes(gzip.compress(content, mtime=0))
            sample.setdefault('artifacts', {})[destination.name] = sha(destination.read_bytes())
        receipt['cases'].append(sample)
        print(name, {k: round(v['median_seconds'], 4) for k, v in sample['summary'].items()}, flush=True)
    receipt['candidate_sha256_after'] = sha(candidate.read_bytes())
    if receipt['candidate_sha256_after'] != receipt['candidate_sha256']:
        raise AssertionError('candidate changed during measurement')
    receipt['finished_at_utc'] = datetime.now(timezone.utc).isoformat()
    receipt['passed'] = True
    args.output.write_text(json.dumps(receipt, indent=2) + '\n')
    print(args.output)


if __name__ == '__main__':
    main()
