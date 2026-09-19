#!/usr/bin/env python3
"""Compare released language/project paths with metadata discovery on macOS."""
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


def digest(path):
    value = hashlib.sha256()
    with path.open('rb') as stream:
        while chunk := stream.read(1024 * 1024):
            value.update(chunk)
    return value.hexdigest()


def run(command):
    return subprocess.check_output(command, text=True).strip()


def inventory(path, source):
    rows = []
    if source == 'git':
        raw = subprocess.check_output(['git', '-C', str(path), 'ls-tree', '-r', '-l', '-z', 'HEAD'])
        for entry in raw.split(b'\0'):
            if not entry:
                continue
            header, name = entry.split(b'\t', 1)
            mode, kind, oid, size = header.split()
            if mode in (b'100644', b'100755'):
                rows.append([name.decode(), int(size), oid.decode()])
    else:
        for base, dirs, files in os.walk(path, followlinks=False):
            dirs[:] = sorted(d for d in dirs if d != '.git')
            for name in sorted(files):
                file = Path(base) / name
                if file.is_symlink() or not file.is_file():
                    continue
                rows.append([file.relative_to(path).as_posix(), file.stat().st_size, digest(file)])
    rows.sort()
    return {'files': len(rows), 'bytes': sum(r[1] for r in rows),
            'manifest_sha256': hashlib.sha256(json.dumps(rows, separators=(',', ':')).encode()).hexdigest(),
            'manifest_definition': 'sorted [relative_path,bytes,SHA256] for directory; [relative_path,bytes,Git_blob_OID] for Git regular files'}


def measure(command):
    started = time.perf_counter()
    result = subprocess.run(['/usr/bin/time', '-l', *command], capture_output=True, timeout=180)
    elapsed = time.perf_counter() - started
    assert result.returncode == 0, (command, result.returncode, result.stderr.decode(errors='replace'))
    match = re.search(rb'(\d+)\s+maximum resident set size', result.stderr)
    assert match, 'missing macOS RSS measurement'
    return result.stdout, {'seconds': elapsed, 'peak_rss_bytes': int(match[1]),
                           'stdout_sha256': hashlib.sha256(result.stdout).hexdigest()}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--baseline', type=Path, required=True)
    parser.add_argument('--candidate', type=Path, required=True)
    parser.add_argument('--build-inputs', type=Path, required=True)
    parser.add_argument('--fixtures', type=Path, required=True)
    parser.add_argument('--corpus-root', type=Path, required=True)
    parser.add_argument('--output', type=Path, required=True)
    args = parser.parse_args()
    assert platform.system() == 'Darwin', 'requires macOS time -l (RSS in bytes)'
    assert not args.output.exists(), 'choose a fresh receipt path'
    args.output.parent.mkdir(parents=True, exist_ok=True)
    candidate, baseline = args.candidate.resolve(), args.baseline.resolve()
    build = json.loads(args.build_inputs.read_text())
    def check_inputs():
        for name, expected in build['files'].items():
            assert digest(ROOT / name) == expected, f'build input changed: {name}'
    check_inputs()
    cases = [(n, args.fixtures.resolve() / n, 'directory', None) for n in ('xml-only', 'xml-and-dotnet', 'small-source')]
    pins = {p['name']: p['commit'] for p in json.loads((ROOT / 'tests/performance/corpus.json').read_text())['projects']}
    for name in ('spring-framework', 'roslyn'):
        path = args.corpus_root.resolve() / name
        commit = run(['git', '-C', str(path), 'rev-parse', 'HEAD'])
        assert commit == pins[name] and not run(['git', '-C', str(path), 'status', '--porcelain']), name
        cases.extend((name + '-' + source, path, source, commit) for source in ('directory', 'git'))
    inputs = {name: inventory(path, source) for name, path, source, _ in cases}
    # Check the large fixtures are fully written XML, not sparse/NUL substitutes.
    size = 16 * 1024 * 1024
    line = b'<entry timestamp="2026-01-01T00:00:00Z">example deterministic log record</entry>\n'
    inner = size - len(b'<logs>\n</logs>\n')
    xml_hash = hashlib.sha256(b'<logs>\n' + line * (inner // len(line)) + b' ' * (inner % len(line)) + b'</logs>\n').hexdigest()
    for name in ('xml-only', 'xml-and-dotnet'):
        files = list((args.fixtures / name).glob('*.xml'))
        assert len(files) == 128 and sum(f.stat().st_size for f in files) == 2147483648
        assert all(digest(f) == xml_hash for f in files)
    receipt = {'started_at_utc': datetime.now(timezone.utc).isoformat(), 'platform': platform.platform(),
               'hardware': {k: run(['sysctl', '-n', k]) for k in ('machdep.cpu.brand_string', 'hw.logicalcpu', 'hw.memsize')},
               'baseline_sha256': digest(baseline), 'candidate_sha256': digest(candidate),
               'baseline_version': run([str(baseline), '--version']), 'candidate_version': run([str(candidate), '--version']),
               'candidate_build': build, 'candidate_go_build_info': run(['go', 'version', '-m', str(candidate)]),
               'harness_sha256': digest(Path(__file__)), 'xml_fixture_sha256': xml_hash,
               'method': {'warmups': 1, 'repetitions': 3, 'workers': 8, 'tree_size': 1000000,
                          'order': 'rotate all five workflows each repetition',
                          'wall_time': 'subprocess plus time wrapper; excludes JSON parsing and fixture verification',
                          'peak_rss': 'single CLI process via macOS time -l; excludes Python harness',
                          'reads': 'discovery classification_bytes_read excludes inventory attribute reads and Git storage I/O; other commands have no comparable read counter',
                          'limits': 'No enforced CPU/RSS limits. Warm cache diagnostic on this host; not universal or statistical proof. No follow-up routing or total-workflow saving is claimed.'}, 'cases': []}
    for name, path, source, commit in cases:
        common = ['--json', '--source', source, '--workers', '8', '--tree-size', '1000000', str(path)]
        commands = {'old_languages': [str(baseline), *common], 'new_languages': [str(candidate), *common],
                    'old_projects': [str(baseline), 'analyze', 'all', '--projects', *common],
                    'new_projects': [str(candidate), 'analyze', 'all', '--projects', *common],
                    'discovery': [str(candidate), 'analyze', 'discovery', *common]}
        expected, samples = {}, {k: [] for k in commands}
        for kind, command in commands.items():
            expected[kind], _ = measure(command)
        assert expected['old_languages'] == expected['new_languages'], f'{name}: legacy language JSON differs'
        assert expected['old_projects'] == expected['new_projects'], f'{name}: legacy projects JSON differs'
        kinds = list(commands)
        for index in range(3):
            for kind in kinds[index:] + kinds[:index]:
                output, sample = measure(commands[kind])
                assert output == expected[kind], f'{name}/{kind}: output changed'
                samples[kind].append(sample)
        output = json.loads(expected['discovery'])
        discovery = output['discovery']
        assert discovery['inventory'] == {k: inputs[name][k] for k in ('files', 'bytes')}, name
        assert sum(g['files'] for g in discovery['categories']) == inputs[name]['files']
        assert sum(g['bytes'] for g in discovery['categories']) == inputs[name]['bytes']
        assert discovery['classification_bytes_read'] == 0 and not output['languages']
        assert discovery['status'] != 'skipped' and discovery['omissions'].get('tree_size_limit', 0) == 0
        assert discovery['source']['mode'] == source
        if source == 'git':
            assert discovery['source']['tree'] == run(['git', '-C', str(path), 'rev-parse', 'HEAD^{tree}'])
        candidates = {c['path']: c for c in discovery['candidates']}
        if name == 'xml-only':
            assert discovery['inventory']['bytes'] == 2147483648 and not candidates
        if name == 'xml-and-dotnet':
            assert candidates['App.csproj']['kind'] == 'manifest'
        if name == 'small-source':
            assert candidates['go.mod']['kind'] == 'manifest'
        sample = {'name': name, 'path': str(path), 'source': source, 'commit': commit, 'input': inputs[name],
                  'commands': commands, 'samples': samples, 'discovery': discovery,
                  'summary': {k: {'median_seconds': statistics.median(s['seconds'] for s in v),
                                  'max_peak_rss_bytes': max(s['peak_rss_bytes'] for s in v)} for k, v in samples.items()},
                  'artifacts': {}}
        if source == 'git':
            sample['git_object_counts'] = run(['git', '-C', str(path), 'count-objects', '-v'])
        for kind, payload in expected.items():
            file = args.output.parent / f'{name}-{kind}.json.gz'
            file.write_bytes(gzip.compress(payload, mtime=0))
            sample['artifacts'][file.name] = digest(file)
        receipt['cases'].append(sample)
        print(name, {k: round(v['median_seconds'], 4) for k, v in sample['summary'].items()}, flush=True)
    check_inputs()
    assert digest(candidate) == receipt['candidate_sha256'] and digest(baseline) == receipt['baseline_sha256']
    for name, path, source, _ in cases:
        assert inventory(path, source) == inputs[name], f'{name}: corpus changed'
    receipt['finished_at_utc'] = datetime.now(timezone.utc).isoformat()
    receipt['passed'] = True
    args.output.write_bytes(gzip.compress((json.dumps(receipt, indent=2) + '\n').encode(), mtime=0))
    print(args.output)


if __name__ == '__main__':
    main()
