#!/usr/bin/env python3
"""Build identified candidates and measure existing paths plus optional declarations."""
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
import tempfile
import time

ROOT = Path(__file__).resolve().parents[3]
BASELINE_MAC_ARM64 = '0d4fd9167d11b590c32713a3ec2da2b3628dc72d12bc8fd58fd6873624af5d91'
PUBLIC_CASES = ('cobra', 'express', 'flask', 'ripgrep', 'roslyn', 'spring-framework')


def require(condition, message):
    if not condition:
        raise ValueError(message)


def utc():
    return datetime.now(timezone.utc).isoformat()


def sha(data):
    return hashlib.sha256(data).hexdigest()


def digest(path):
    h = hashlib.sha256()
    with Path(path).open('rb') as stream:
        while block := stream.read(1 << 20):
            h.update(block)
    return h.hexdigest()


def run(command, **kwargs):
    return subprocess.check_output(command, **kwargs).decode().strip()


def save(path, value):
    path.parent.mkdir(parents=True, exist_ok=True)
    data = (json.dumps(value, indent=2) + '\n').encode()
    with path.open('xb') as stream:
        stream.write(gzip.compress(data, mtime=0) if path.suffix == '.gz' else data)


def read_json(path):
    data = path.read_bytes()
    return json.loads(gzip.decompress(data) if path.suffix == '.gz' else data)


def build_inputs(env):
    raw = subprocess.check_output(['go', 'list', '-mod=readonly', '-deps', '-json', '.'], cwd=ROOT, env=env).decode()
    decoder = json.JSONDecoder()
    files = {ROOT / 'go.mod', ROOT / 'go.sum'}
    while raw.strip():
        package, end = decoder.raw_decode(raw.lstrip())
        raw = raw.lstrip()[end:]
        directory = Path(package.get('Dir', '/'))
        if directory.is_relative_to(ROOT):
            for field in ('GoFiles', 'CgoFiles', 'CFiles', 'CXXFiles', 'HFiles', 'SFiles', 'SysoFiles', 'EmbedFiles'):
                files.update(directory / name for name in package.get(field, []))
            if package.get('Module', {}).get('GoMod'):
                files.add(Path(package['Module']['GoMod']))
    return {str(p.relative_to(ROOT)): digest(p) for p in sorted(files) if p.is_relative_to(ROOT)}


def source_state():
    def git(*args):
        return subprocess.check_output(['git', *args], cwd=ROOT)
    return {'commit': git('rev-parse', 'HEAD').decode().strip(),
            'head_tree': git('rev-parse', 'HEAD^{tree}').decode().strip(),
            'status_sha256': sha(git('status', '--porcelain=v1', '-z', '--untracked-files=all')),
            'diff_sha256': sha(git('diff', 'HEAD', '--binary')),
            'dirty': bool(git('status', '--porcelain=v1', '--untracked-files=all'))}


def build(args):
    output, receipt = args.candidate.resolve(), args.build_receipt.resolve()
    require(not output.exists() and not receipt.exists(), 'candidate and build receipt must be fresh paths')
    env = dict(os.environ, CGO_ENABLED='0', GOWORK='off', GOFLAGS='')
    output.parent.mkdir(parents=True, exist_ok=True)
    inputs, source = build_inputs(env), source_state()
    command = ['go', 'build', '-mod=readonly', '-buildvcs=false', '-trimpath', '-ldflags',
               '-s -w -X dircue/internal/cli.Version=0.4.0', '-o', str(output), '.']
    started = utc()
    subprocess.run(command, cwd=ROOT, env=env, check=True)
    require(inputs == build_inputs(env), 'build input paths or contents changed during build; discard candidate')
    require(source == source_state(), 'worktree changed during build; discard candidate and retry quietly')
    save(receipt, {'schema': 'dircue-v050-benchmark-build-1', 'started_at_utc': started, 'finished_at_utc': utc(),
                   'source_at_build': source, 'files': inputs, 'candidate_sha256': digest(output),
                   'command': command, 'environment': {'CGO_ENABLED': '0', 'GOWORK': 'off', 'GOFLAGS': ''},
                   'go_version': run(['go', 'version']), 'go_build_info': run(['go', 'version', '-m', str(output)]),
                   'note': 'Version overridden to released 0.4.0 for output comparison; not a release executable.'})
    print(receipt)


def inventory(path, source):
    rows = []
    if source == 'git':
        raw = subprocess.check_output(['git', '-C', str(path), 'ls-tree', '-r', '-l', '-z', 'HEAD'])
        for entry in raw.split(b'\0'):
            if not entry:
                continue
            header, name = entry.split(b'\t', 1)
            mode, kind, oid, size = header.split()
            rows.append([name.decode(), mode.decode(), kind.decode(), oid.decode(), size.decode()])
        return {'selection': 'all recursive HEAD entries including symlinks/submodules',
                'entries': len(rows), 'manifest_sha256': sha(json.dumps(sorted(rows), separators=(',', ':')).encode()),
                'commit': run(['git', '-C', str(path), 'rev-parse', 'HEAD']),
                'tree': run(['git', '-C', str(path), 'rev-parse', 'HEAD^{tree}'])}
    total, xml_bytes = 0, 0
    for directory, dirs, files in os.walk(path, followlinks=False):
        dirs[:] = sorted(d for d in dirs if d != '.git')
        for name in sorted(dirs + files):
            file = Path(directory) / name
            relative = file.relative_to(path).as_posix()
            if file.is_symlink():
                rows.append([relative, 'symlink', os.readlink(file)])
            elif file.is_file():
                size = file.stat().st_size
                total += size
                if file.suffix.lower() == '.xml':
                    xml_bytes += size
                rows.append([relative, 'file', size, digest(file)])
            elif file.is_dir():
                rows.append([relative, 'directory'])
            else:
                raise ValueError('special file is outside this harness corpus contract: ' + relative)
    return {'selection': 'regular-file SHA256 and directory/symlink metadata, excluding .git directories',
            'entries': len(rows), 'bytes': total, 'xml_bytes': xml_bytes,
            'manifest_sha256': sha(json.dumps(sorted(rows), separators=(',', ':')).encode())}


def measure(command, timeout):
    system = platform.system()
    require(system in ('Darwin', 'Linux'), 'RSS measurement supports macOS time -l or Linux GNU time only')
    with tempfile.TemporaryDirectory(prefix='dircue-v050-time-') as temp:
        usage = Path(temp) / 'usage'
        wrapper = ['/usr/bin/time', '-l', '-o', str(usage)] if system == 'Darwin' else ['/usr/bin/time', '-f', '%M', '-o', str(usage)]
        started = time.perf_counter()
        child = subprocess.Popen([*wrapper, *command], stdout=subprocess.PIPE, stderr=subprocess.PIPE, start_new_session=True)
        try:
            stdout, stderr = child.communicate(timeout=timeout)
        except subprocess.TimeoutExpired:
            import signal
            os.killpg(child.pid, signal.SIGKILL)
            child.communicate()
            raise ValueError('benchmark command timed out; process group terminated') from None
        elapsed = time.perf_counter() - started
        raw = usage.read_text()
    require(child.returncode == 0 and not stderr, f'nonzero exit or stderr: {command!r}, exit={child.returncode}, stderr_sha256={sha(stderr)}')
    if system == 'Darwin':
        match = re.search(r'(\d+)\s+maximum resident set size', raw)
        require(match is not None, 'missing macOS peak RSS')
        rss = int(match[1])
    else:
        require(raw.strip().isdigit(), 'missing Linux GNU time peak RSS')
        rss = int(raw.strip()) * 1024
    require(elapsed > 0 and rss > 0, 'invalid timing/resource sample')
    return stdout, {'seconds': elapsed, 'peak_rss_bytes': rss, 'exit_code': child.returncode,
                    'stdout_sha256': sha(stdout), 'stderr_sha256': sha(stderr), 'time_diagnostics': raw}


def cases(args):
    output = []
    if args.corpus_root:
        pins = {p['name']: p['commit'] for p in read_json(ROOT / 'tests/performance/corpus.json')['projects']}
        for name in PUBLIC_CASES:
            path = (args.corpus_root / name).resolve()
            require(path.is_dir(), 'missing corpus: ' + str(path))
            require(run(['git', '-C', str(path), 'rev-parse', 'HEAD']) == pins[name], 'unexpected corpus commit: ' + name)
            require(not run(['git', '-C', str(path), 'status', '--porcelain']), 'dirty corpus: ' + name)
            output.append({'name': name + '-git', 'path': str(path), 'source': 'git'})
        output.append({'name': 'roslyn-directory', 'path': str((args.corpus_root / 'roslyn').resolve()), 'source': 'directory'})
    if args.xml_root:
        output.append({'name': 'large-xml-directory', 'path': str(args.xml_root.resolve()), 'source': 'directory', 'require_xml_gib': True})
    for arg in args.case:
        name, source, path = arg.split(':', 2)
        require(re.fullmatch(r'[a-zA-Z0-9_-]+', name) is not None and source in ('git', 'directory'), 'invalid --case name/source')
        output.append({'name': name, 'source': source, 'path': str(Path(path).resolve())})
    require(output and len({c['name'] for c in output}) == len(output), 'provide unique cases or --corpus-root/--xml-root')
    return output


def collect(args):
    require(args.repetitions >= 5 and args.warmups >= 1, 'use at least five repetitions and one warmup')
    require(args.workers >= 1 and args.timeout > 0, 'workers and timeout must be positive')
    require(not args.output.exists(), 'output must be a fresh directory')
    baseline, candidate = args.baseline.resolve(), args.candidate.resolve()
    expected_baseline = args.baseline_sha256 or (BASELINE_MAC_ARM64 if platform.system() == 'Darwin' and platform.machine() == 'arm64' else None)
    require(expected_baseline and digest(baseline) == expected_baseline, 'baseline SHA256 mismatch; supply verified release hash on other platforms')
    build_record = read_json(args.build_receipt)
    require(build_record.get('schema') == 'dircue-v050-benchmark-build-1' and build_record.get('files') and build_record.get('source_at_build'), 'use the build subcommand receipt')
    candidate_hash = digest(candidate)
    require(build_record['candidate_sha256'] == candidate_hash, 'candidate does not match historical build receipt')
    selected = cases(args)
    for case in selected:
        path = Path(case['path'])
        require(path.is_dir(), 'missing corpus directory')
        case['input'] = inventory(path, case['source'])
        if case.get('require_xml_gib'):
            require(case['input']['xml_bytes'] >= 1 << 30, '--xml-root must contain at least 1 GiB of real XML file bytes')
    args.output.mkdir(parents=True)
    report = {'started_at_utc': utc(), 'platform': platform.platform(), 'cpu_count': os.cpu_count(),
              'runtime_environment': {name: os.environ.get(name) for name in ('GOMAXPROCS', 'GOGC', 'GOMEMLIMIT', 'GODEBUG')},
              'baseline_sha256': expected_baseline, 'candidate_sha256': candidate_hash,
              'baseline_version': run([str(baseline), '--version']), 'candidate_version': run([str(candidate), '--version']),
              'build_receipt': build_record, 'build_receipt_sha256': digest(args.build_receipt),
              'harness_sha256': digest(Path(__file__)), 'environment_note': args.environment_note,
              'method': {'repetitions': args.repetitions, 'warmups': args.warmups, 'workers': args.workers,
                         'tree_size': 1000000, 'cache': 'warm; inventory and warmups before measurement; no eviction',
                         'order': 'reverse lane order each round; baseline/candidate stay adjacent for each old path',
                         'wall': 'subprocess elapsed including time wrapper; excludes JSON checks and inventories',
                         'rss': 'standalone CLI peak resident bytes; macOS bytes or Linux GNU time KiB converted to bytes',
                         'limits': 'No universal speed claim or enforced CPU/RAM limit; host activity described separately.'}, 'cases': []}
    for case in selected:
        common = ['--json', '--source', case['source'], '--workers', str(args.workers), '--tree-size', '1000000', case['path']]
        commands = {'baseline_languages': [str(baseline), *common], 'candidate_languages': [str(candidate), *common],
                    'baseline_all': [str(baseline), 'analyze', 'all', *common], 'candidate_all': [str(candidate), 'analyze', 'all', *common],
                    'candidate_combined': [str(candidate), 'analyze', 'all', '--declarations', *common],
                    'candidate_declarations': [str(candidate), 'analyze', 'declarations', *common]}
        expected, warmups = {}, {key: [] for key in commands}
        samples, order = {key: [] for key in commands}, []
        for _ in range(args.warmups):
            for lane, command in commands.items():
                payload, sample = measure(command, args.timeout)
                require(lane not in expected or expected[lane] == payload, 'nondeterministic warmup: ' + lane)
                expected[lane] = payload
                warmups[lane].append(sample)
        for mode in ('languages', 'all'):
            require(expected['baseline_' + mode] == expected['candidate_' + mode], case['name'] + ': old command output changed: ' + mode)
        combined, ordinary = json.loads(expected['candidate_combined']), json.loads(expected['candidate_all'])
        declaration = combined.pop('declarations')
        combined['schema_version'] = ordinary['schema_version']
        require(combined == ordinary, case['name'] + ': opt-in changed existing aggregate observations')
        require(declaration == json.loads(expected['candidate_declarations'])['declarations'], 'standalone/combined declaration mismatch')
        if case.get('require_xml_gib'):
            require(declaration['coverage']['manifest_candidates'] == 0 and declaration['coverage']['parsed_manifests'] == 0, 'XML-only declaration lane found manifests; use a fixture without supported manifests')
        lanes = list(commands)
        for index in range(args.repetitions):
            for lane in lanes if index % 2 == 0 else reversed(lanes):
                payload, sample = measure(commands[lane], args.timeout)
                require(payload == expected[lane], f'nondeterministic result: {case["name"]}/{lane}/{index}')
                sample['round'] = index + 1
                samples[lane].append(sample)
                order.append(lane)
        summary = {lane: {'median_seconds': statistics.median(s['seconds'] for s in values),
                           'min_seconds': min(s['seconds'] for s in values), 'max_seconds': max(s['seconds'] for s in values),
                           'median_peak_rss_bytes': statistics.median(s['peak_rss_bytes'] for s in values),
                           'max_peak_rss_bytes': max(s['peak_rss_bytes'] for s in values)} for lane, values in samples.items()}
        comparisons = {}
        for name, old, new in (('languages', 'baseline_languages', 'candidate_languages'), ('all', 'baseline_all', 'candidate_all'),
                               ('optional_declarations_cost', 'candidate_all', 'candidate_combined')):
            comparisons[name] = {'median_time_change_percent': 100 * (summary[new]['median_seconds'] / summary[old]['median_seconds'] - 1),
                                 'median_rss_change_percent': 100 * (summary[new]['median_peak_rss_bytes'] / summary[old]['median_peak_rss_bytes'] - 1),
                                 'paired_time_change_percent': [100 * (n['seconds'] / o['seconds'] - 1) for o, n in zip(samples[old], samples[new])]}
        artifacts = {}
        for lane, payload in expected.items():
            artifact = args.output / (case['name'] + '-' + lane + '.json.gz')
            with artifact.open('xb') as stream:
                stream.write(gzip.compress(payload, mtime=0))
            artifacts[artifact.name] = digest(artifact)
        case.update(commands=commands, warmups=warmups, samples=samples, execution_order=order, summary=summary,
                    comparisons=comparisons, declarations_coverage=declaration['coverage'], artifacts=artifacts)
        report['cases'].append(case)
        save(args.output / (case['name'] + '-receipt.json'), case)
        print(case['name'], json.dumps(comparisons), flush=True)
    report['timings_finished_at_utc'] = utc()
    for case in selected:
        require(inventory(Path(case['path']), case['source']) == case['input'], 'corpus changed: ' + case['name'])
    require(digest(candidate) == candidate_hash and digest(baseline) == expected_baseline, 'binary changed during benchmark')
    report.update(finished_at_utc=utc(), passed=True)
    save(args.output / 'receipt.json', report)
    print(args.output / 'receipt.json')


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    subs = parser.add_subparsers(dest='command', required=True)
    builder = subs.add_parser('build', help='Build only dircue and bind its input snapshot to its binary hash')
    for name in ('candidate', 'build-receipt'):
        builder.add_argument('--' + name, type=Path, required=True)
    bench = subs.add_parser('measure', help='Read existing corpora; never build or execute their code')
    for name in ('baseline', 'candidate', 'build-receipt', 'output'):
        bench.add_argument('--' + name, type=Path, required=True)
    for name in ('corpus-root', 'xml-root'):
        bench.add_argument('--' + name, type=Path)
    bench.add_argument('--case', action='append', default=[], metavar='NAME:git|directory:PATH')
    bench.add_argument('--baseline-sha256')
    bench.add_argument('--repetitions', type=int, default=5)
    bench.add_argument('--warmups', type=int, default=1)
    bench.add_argument('--workers', type=int, default=8)
    bench.add_argument('--timeout', type=int, default=300)
    bench.add_argument('--environment-note', required=True)
    args = parser.parse_args()
    try:
        (build if args.command == 'build' else collect)(args)
    except (ValueError, OSError, subprocess.SubprocessError) as error:
        parser.exit(1, f'benchmark failed: {error}\n')


if __name__ == '__main__':
    main()
