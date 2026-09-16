#!/usr/bin/env python3
"""Exercise the prebuilt Linux worker without networking or writable storage."""
import argparse
import hashlib
import json
from pathlib import Path
import subprocess
import time

ROOT = Path(__file__).resolve().parents[1]


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--image', default='dircue-structural-prototype:local')
    parser.add_argument('--output', type=Path, required=True)
    parser.add_argument('--driver', type=Path, help='optional Linux driver matching the image architecture')
    args = parser.parse_args()
    image = json.loads(subprocess.check_output(['docker', 'image', 'inspect', args.image]))[0]
    cases = []
    for language, folder, extension in [('Java', 'java', '*.java'), ('C#', 'csharp', '*.cs')]:
        for path in sorted((ROOT / 'tests/fixtures' / folder).glob(extension)):
            cases.append((language, str(path.relative_to(ROOT)), path.read_text()))
    cases.append(('Java', 'synthetic/repeated.java', 'class C { int f(int n) { if (n > 0) return n; return 0; } }\n' * 4600))
    rows = []
    for language, path, source in cases:
        request = json.dumps({'path': path, 'language': language, 'source': source}).encode()
        started = time.monotonic()
        completed = subprocess.run([
            'docker', 'run', '--rm', '-i', '--network', 'none', '--read-only',
            '--memory', '256m', '--memory-swap', '256m', '--pids-limit', '32',
            '--cpus', '1', '--cap-drop', 'ALL', '--security-opt', 'no-new-privileges',
            '--entrypoint', '/bin/sh', args.image, '-c',
            'dircue-structural-worker; result=$?; if [ -r /sys/fs/cgroup/memory.peak ]; then cat /sys/fs/cgroup/memory.peak >&2; else echo unavailable >&2; fi; cat /sys/fs/cgroup/memory.max /sys/fs/cgroup/memory.events >&2; exit "$result"',
        ], input=request, capture_output=True, timeout=60)
        elapsed = time.monotonic() - started
        result = json.loads(completed.stdout)
        lines = completed.stderr.decode().splitlines()
        peak = None if lines[0] == 'unavailable' else int(lines[0])
        maximum = int(lines[1])
        events = dict(line.split() for line in lines[2:])
        assert completed.returncode == 0, completed.stderr
        assert result['status'] == 'complete', result
        assert result['parse_count'] == 1, result
        assert result['source_bytes'] == len(source.encode()), result
        assert maximum == 256 * 1024 * 1024
        assert peak is None or peak <= maximum
        assert events['oom_kill'] == '0'
        rows.append({'path': path, 'language': language, 'source_bytes': len(source.encode()),
                     'source_sha256': hashlib.sha256(source.encode()).hexdigest(),
                     'elapsed_with_container_startup_seconds': elapsed,
                     'cgroup_peak_bytes': peak, 'cgroup_limit_bytes': maximum, 'cgroup_memory_events': events,
                     'status': result['status'], 'parse_count': result['parse_count'],
                     'observations': result['observations'], 'provenance': result['provenance']})
    identity = subprocess.check_output(['docker', 'run', '--rm', '--network', 'none', '--read-only',
        '--entrypoint', '/bin/sh', args.image, '-c',
        'sha256sum /usr/local/bin/dircue-structural-worker; test ! -e /usr/local/cargo/bin/cargo; test -f /usr/share/dircue-structural/notices/source/big-code-analysis-2.2.0.crate'], text=True).split()[0]
    report = {'image_id': image['Id'], 'os': image['Os'], 'architecture': image['Architecture'],
              'worker_sha256': identity, 'network': 'none', 'read_only': True,
              'user': image['Config']['User'], 'compiler_required_at_runtime': False,
              'bca_source_archive_in_image': True, 'cases': rows,
              'limitations': 'One worker per container; peak includes shell and cgroup-charged pages when the kernel exposes memory.peak; null means unavailable. '
                  'This verifies these inputs under a container limit, not a bound on every input or on the Go driver.'}
    if args.driver:
        driver = args.driver.resolve(strict=True)
        completed = subprocess.run([
            'docker', 'run', '--rm', '--network', 'none', '--read-only',
            '--memory', '256m', '--memory-swap', '256m', '--pids-limit', '32',
            '--cap-drop', 'ALL', '--security-opt', 'no-new-privileges',
            '--mount', f'type=bind,source={driver},target=/driver,readonly',
            '--mount', f'type=bind,source={ROOT / "tests/fixtures"},target=/input,readonly',
            '--entrypoint', '/driver', args.image,
            '--worker', '/usr/local/bin/dircue-structural-worker', '/input',
        ], capture_output=True, text=True, timeout=60)
        assert completed.returncode == 0, completed.stderr
        records = [json.loads(line) for line in completed.stdout.splitlines()]
        summary = records[-1]
        assert summary['type'] == 'summary' and summary['errors'] == 0
        assert summary['observed'] == 4 and summary['partial_files'] == 2, summary
        assert all(row['result']['parse_count'] == 1 for row in records if row['type'] == 'file')
        report['driver_and_worker'] = {
            'driver_sha256': hashlib.sha256(driver.read_bytes()).hexdigest(),
            'container_limit_bytes': 256 * 1024 * 1024, 'network': 'none',
            'read_only': True, 'summary': summary,
            'limitations': 'The limit covers both processes together; this checks fixtures, not arbitrary inputs.'}
    args.output.parent.mkdir(parents=True, exist_ok=True)
    args.output.write_text(json.dumps(report, indent=2) + '\n')
    print(f'{len(rows)} offline cases passed under the enforced 256 MiB container limit')


if __name__ == '__main__':
    main()
