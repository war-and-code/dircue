#!/usr/bin/env python3
"""Characterize six simultaneous read-only scans under Docker cgroup budgets."""
import argparse
from concurrent.futures import ThreadPoolExecutor
import hashlib
import json
import os
from pathlib import Path
import re
import subprocess
import time
import uuid

HERE = Path(__file__).resolve().parent
ROOT = HERE.parents[1]


def digest(path):
    with path.open('rb') as stream:
        return hashlib.file_digest(stream, 'sha256').hexdigest()


def docker(*args, timeout=30):
    result = subprocess.run(['docker', *args], capture_output=True, text=True, timeout=timeout)
    if result.returncode:
        raise RuntimeError(f'docker {args[0]} failed: {result.stderr[:4096]}')
    return result.stdout


def counters(text):
    return {k: int(v) for k, v in (line.split() for line in text.splitlines())}


def assess(record, directory, reference=None):
    """A terminated or truncated process cannot contribute an accepted report."""
    reasons = []
    receipt = record.get('probe')
    state = record['docker']['State']
    if state.get('OOMKilled'):
        reasons.append('docker_oom_killed')
    if state.get('ExitCode') != 0:
        reasons.append('container_exit')
    if receipt is None:
        reasons.append('missing_supervisor_receipt')
    else:
        if receipt['exit_code'] != 0:
            reasons.append('child_exit')
        if receipt['failure']:
            reasons.append(receipt['failure'])
        for field in ('oom', 'oom_kill', 'oom_group_kill'):
            before = counters(receipt['before']['memory.events']).get(field, 0)
            after = counters(receipt['after']['memory.events']).get(field, 0)
            if after > before:
                reasons.append('cgroup_' + field)
        stdout = directory / 'stdout.txt'
        if not stdout.exists() or digest(stdout) != receipt['output_sha256']['stdout']:
            reasons.append('output_hash_mismatch')
        else:
            try:
                parsed = json.loads(stdout.read_bytes())
                if not isinstance(parsed, dict):
                    raise ValueError('expected object')
                record['module_statuses'] = {k: v.get('status') for k, v in parsed.items()
                                             if isinstance(v, dict) and 'status' in v}
            except (ValueError, UnicodeError):
                reasons.append('invalid_json')
            if reference is not None and stdout.read_bytes() != reference.read_bytes():
                reasons.append('reference_difference')
    record['accepted'] = not reasons
    record['rejection_reasons'] = reasons
    return record


def execute_batch(args, cases, name, memory=None, cpus=None, references=None):
    batch_dir = args.output / name
    batch_dir.mkdir()
    control = batch_dir / 'control'
    control.mkdir()
    containers = []
    records = []
    try:
        for index, case in enumerate(cases):
            destination = batch_dir / case['name']
            destination.mkdir(mode=0o777)
            destination.chmod(0o777)
            cname = f'dircue-resource-{args.run_id}-{name}-{index}'
            options = ['create', '--name', cname, '--pull=never', '--platform=linux/arm64',
                       '--network=none', '--read-only', '--user=65534:65534', '--cap-drop=ALL',
                       '--security-opt=no-new-privileges', '--pids-limit=128', '--log-driver=local',
                       '--log-opt=max-size=1m', '--log-opt=max-file=1', '--log-opt=compress=false',
                       '--env=PYTHONDONTWRITEBYTECODE=1', '--env=HOME=/nonexistent',
                       '--mount', f'type=bind,src={args.binary},dst=/candidate/dircue,readonly',
                       '--mount', f'type=bind,src={HERE},dst=/harness,readonly',
                       '--mount', f'type=bind,src={control},dst=/control,readonly',
                       '--mount', f'type=bind,src={destination},dst=/results',
                       '--mount', f'type=bind,src={args.cobra},dst=/corpus/cobra,readonly',
                       '--mount', f'type=volume,src={args.volume},dst=/stress,readonly']
            if memory is not None:
                options += [f'--memory={memory}', f'--memory-swap={memory}']
            if cpus is not None:
                options += [f'--cpus={cpus}']
            options += [args.image, 'python3', '/harness/supervisor.py', '--timeout',
                        str(args.timeout), '--', '/candidate/dircue', *case['argv']]
            cid = docker(*options).strip()
            containers.append((cid, destination, case))
        # Creation is excluded; each already-running supervisor waits on one barrier.
        with ThreadPoolExecutor(max_workers=len(containers)) as pool:
            list(pool.map(lambda item: docker('start', item[0]), containers))
        deadline = time.monotonic() + 60
        while not all((dest / 'ready.json').exists() for _, dest, _ in containers):
            if time.monotonic() > deadline:
                raise TimeoutError('containers did not reach start barrier')
            for cid, dest, _ in containers:
                if not (dest / 'ready.json').exists():
                    state = json.loads(docker('inspect', '--format', '{{json .State}}', cid))
                    if not state['Running']:
                        logs = subprocess.run(['docker', 'logs', cid], capture_output=True, timeout=10)
                        (dest / 'startup.txt').write_bytes((logs.stdout + logs.stderr)[:1024 * 1024])
                        raise RuntimeError('container exited before the start barrier: ' + str(dest))
            time.sleep(0.05)
        barrier = control / 'start.tmp'
        barrier.write_text(json.dumps({'start_at_unix': time.time() + 0.5}))
        barrier.replace(control / 'start.json')
        with ThreadPoolExecutor(max_workers=len(containers)) as pool:
            list(pool.map(lambda item: docker('wait', item[0], timeout=args.timeout + 30), containers))
        for cid, destination, case in containers:
            inspected = json.loads(docker('inspect', cid))[0]
            report_file = destination / 'receipt.json'
            record = {'case': case['name'], 'docker': {
                'Id': inspected['Id'], 'Image': inspected['Image'], 'State': inspected['State'],
                'HostConfig': {key: inspected['HostConfig'].get(key) for key in
                               ('Memory', 'MemorySwap', 'NanoCpus', 'PidsLimit', 'ReadonlyRootfs',
                                'NetworkMode', 'CapDrop', 'SecurityOpt')}},
                'probe': json.loads(report_file.read_text()) if report_file.exists() else None}
            reference = references.get(case['name']) if references else None
            record['ready'] = json.loads((destination / 'ready.json').read_text())
            assess(record, destination, reference)
            limits = record['ready']['cgroup']
            quota, period = limits['cpu.max'].split()
            valid_memory = (limits['memory.max'] == 'max' if memory is None else
                            limits['memory.max'] == str(memory) and limits['memory.swap.max'] == '0')
            valid_cpu = (quota == 'max' if cpus is None else
                         quota.isdecimal() and int(quota) == int(cpus * int(period)))
            if not valid_memory or not valid_cpu:
                record['rejection_reasons'].append('effective_limits_mismatch')
                record['accepted'] = False
            records.append(record)
    finally:
        for cid, _, _ in containers:
            # Only IDs returned by this invocation are removed, even on interruption.
            docker('rm', '--force', cid)
    intervals = [r['probe'] for r in records if r['probe']]
    overlap = (min(r['ended_unix'] for r in intervals) - max(r['started_unix'] for r in intervals)) if intervals else 0
    result = {'name': name, 'memory_bytes': memory, 'cpu_quota_cores': cpus,
              'simultaneous_child_overlap_seconds': max(0, overlap), 'cases': records}
    (batch_dir / 'batch.json').write_text(json.dumps(result, indent=2) + '\n')
    return result


def validate_fixture(args):
    output = docker('run', '--rm', '--pull=never', '--platform=linux/arm64',
                    '--network=none', '--read-only', '--user=65534:65534', '--cap-drop=ALL',
                    '--security-opt=no-new-privileges', '--pids-limit=32', '--memory=512m',
                    '--memory-swap=512m', '--cpus=2', '--env=PYTHONDONTWRITEBYTECODE=1',
                    '--mount', f'type=bind,src={HERE},dst=/harness,readonly',
                    '--mount', f'type=bind,src={args.manifest},dst=/expected/manifest.json,readonly',
                    '--mount', f'type=volume,src={args.volume},dst=/stress,readonly',
                    args.image, 'python3', '/harness/verify_inputs.py', args.fixture_subdir, timeout=600)
    return json.loads(output)


def cobra_identity(root):
    files = {}
    for p in sorted(root.rglob('*')):
        if '.git' in p.relative_to(root).parts or p.is_dir():
            continue
        if p.is_symlink() or not p.is_file():
            raise ValueError('unsupported corpus file: ' + str(p))
        files[str(p.relative_to(root))] = digest(p)
    return {'revision': subprocess.check_output(['git', '-C', str(root), 'rev-parse', 'HEAD'], text=True).strip(),
            'files': files}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--binary', type=Path, required=True)
    parser.add_argument('--build-inputs', type=Path, required=True)
    parser.add_argument('--manifest', type=Path, required=True)
    parser.add_argument('--cobra', type=Path, required=True)
    parser.add_argument('--volume', required=True)
    parser.add_argument('--fixture-subdir', default='acceptance')
    parser.add_argument('--image', required=True, help='existing immutable local sha256 image ID')
    parser.add_argument('--output', type=Path, required=True)
    parser.add_argument('--timeout', type=float, default=120)
    args = parser.parse_args()
    if not re.fullmatch(r'[A-Za-z0-9_-]+', args.fixture_subdir):
        parser.error('fixture subdirectory must be one plain directory name')
    if not re.fullmatch(r'sha256:[a-f0-9]{64}', args.image) or not 0 < args.timeout <= 600:
        parser.error('immutable image ID and timeout in (0, 600] required')
    for field in ('binary', 'build_inputs', 'manifest', 'cobra', 'output'):
        setattr(args, field, getattr(args, field).resolve())
    if args.output.exists():
        parser.error('output directory must not exist')
    args.output.mkdir(parents=True)
    args.run_id = uuid.uuid4().hex[:10]
    build = json.loads(args.build_inputs.read_text())
    with args.binary.open('rb') as stream:
        header = stream.read(20)
    if header[:6] != b'\x7fELF\x02\x01' or int.from_bytes(header[18:20], 'little') != 183:
        raise ValueError('candidate must be a Linux arm64 ELF binary')
    if build.get('binary_sha256') and digest(args.binary) != build['binary_sha256']:
        raise ValueError('candidate differs from build receipt')
    for name, expected in build['files'].items():
        if digest(ROOT / name) != expected:
            raise ValueError('build source no longer matches: ' + name)
    image = json.loads(docker('image', 'inspect', args.image))[0]
    if image['Architecture'] != 'arm64' or image['Os'] != 'linux':
        raise ValueError('image must be native Linux arm64')
    manifest = json.loads(args.manifest.read_text())
    fixtures = {f['name']: f for f in manifest['fixtures']}
    revision = fixtures['dotnet-graph']['variants'][-1]['revision']
    etl_pipeline_revision = fixtures['etl-pipeline-packed']['variants'][-1]['revision']
    cases = [
        {'name': 'go-directory', 'argv': ['--json', '--source=directory', '/corpus/cobra']},
        {'name': 'etl-pipeline-directory', 'argv': ['--json', '--source=directory', '/stress/acceptance/etl-pipeline/flat-generated-excluded']},
        {'name': 'xml-log-directory', 'argv': ['--json', '--source=directory', '/stress/acceptance/xml-log/flat-default']},
        {'name': 'dotnet-graph-packed', 'argv': ['analyze', 'graph', '--json', '--source=git', '--rev', revision, '/stress/acceptance/dotnet-graph/git']},
        {'name': 'dotnet-projects-packed', 'argv': ['analyze', 'projects', '--json', '--source=git', '--rev', revision, '/stress/acceptance/dotnet-graph/git']},
        {'name': 'etl-pipeline-packed', 'argv': ['--json', '--source=git', '--rev', etl_pipeline_revision, '/stress/acceptance/etl-pipeline-packed/git']},
    ]
    for case in cases:
        case['argv'] = [value.replace('/stress/acceptance/', '/stress/' + args.fixture_subdir + '/')
                        for value in case['argv']]
    result = {'schema_version': '1.0.0', 'started_unix': time.time(),
              'binary_sha256': digest(args.binary), 'build_inputs': build, 'target': 'linux/arm64',
              'image_id': image['Id'], 'fixture_manifest_sha256': digest(args.manifest),
              'fixture_subdir': args.fixture_subdir,
              'harness_sha256': {p.name: digest(p) for p in sorted(HERE.glob('*.py'))},
              'docker_version': json.loads(docker('version', '--format', '{{json .}}')),
              'cases': cases, 'baselines': [], 'batches': []}
    result['fixture_validation_before'] = validate_fixture(args)
    result['cobra_before'] = cobra_identity(args.cobra)
    references = {}
    for case in cases:
        baseline = execute_batch(args, [case], 'baseline-' + case['name'])
        result['baselines'].append(baseline)
        if not all(r['accepted'] for r in baseline['cases']):
            raise ValueError('unrestricted reference failed: ' + case['name'])
        references[case['name']] = args.output / baseline['name'] / case['name'] / 'stdout.txt'
    for cpu in (0.5, 1, 2):
        batch = execute_batch(args, cases, 'cpu-' + str(cpu), memory=512 * 1024 * 1024,
                              cpus=cpu, references=references)
        result['batches'].append(batch)
        (args.output / 'results.json').write_text(json.dumps(result, indent=2) + '\n')
    # This deliberate small limit applies only to a disposable container.
    result['too_small_memory'] = execute_batch(args, [cases[3]], 'memory-failure',
                                              memory=32 * 1024 * 1024, cpus=0.5,
                                              references=references)
    result['passed'] = all(r['accepted'] for b in result['batches'] for r in b['cases'])
    result['six_way_overlap_observed'] = all(b['simultaneous_child_overlap_seconds'] > 0 for b in result['batches'])
    result['finished_unix'] = time.time()
    result['binary_sha256_after'] = digest(args.binary)
    if result['binary_sha256_after'] != result['binary_sha256']:
        raise ValueError('candidate binary changed during campaign')
    result['fixture_validation_after'] = validate_fixture(args)
    result['cobra_after'] = cobra_identity(args.cobra)
    if result['fixture_validation_before'] != result['fixture_validation_after'] or result['cobra_before'] != result['cobra_after']:
        raise ValueError('fixture inputs changed during campaign')
    (args.output / 'results.json').write_text(json.dumps(result, indent=2) + '\n')
    print(json.dumps({'passed': result['passed'], 'six_way_overlap_observed': result['six_way_overlap_observed']}))
    raise SystemExit(0 if result['passed'] and result['six_way_overlap_observed'] else 1)


if __name__ == '__main__':
    main()
