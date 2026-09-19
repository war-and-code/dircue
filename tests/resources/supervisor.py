#!/usr/bin/env python3
"""Run one bounded probe inside its disposable cgroup-v2 container."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import selectors
import signal
import subprocess
import time


CGROUP = Path('/sys/fs/cgroup')


def snapshot():
    result = {}
    for name in ('memory.current', 'memory.peak', 'memory.max', 'memory.swap.max',
                 'memory.events', 'cpu.max', 'cpu.stat', 'pids.max'):
        try:
            result[name] = (CGROUP / name).read_text().strip()
        except FileNotFoundError:
            if name != 'memory.peak':
                raise
            result[name] = None
    return result


def atomic_json(path, value):
    temporary = path.with_suffix('.tmp')
    temporary.write_text(json.dumps(value, indent=2) + '\n')
    temporary.replace(path)


def run(command, destination, timeout, output_limit):
    before = snapshot()
    sampled_max = int(before.get('memory.current', 0))
    started = time.time()
    clock_start = time.monotonic()
    child = subprocess.Popen(command, stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                             start_new_session=True, stdin=subprocess.DEVNULL)
    selector = selectors.DefaultSelector()
    selector.register(child.stdout, selectors.EVENT_READ, 'stdout')
    selector.register(child.stderr, selectors.EVENT_READ, 'stderr')
    sizes = {'stdout': 0, 'stderr': 0}
    digests = {name: hashlib.sha256() for name in sizes}
    files = {name: (destination / (name + '.txt')).open('wb') for name in sizes}
    failure = None
    killed_at = None
    try:
        while selector.get_map():
            if before:
                sampled_max = max(sampled_max, int((CGROUP / 'memory.current').read_text()))
            now = time.monotonic()
            if failure is None and now - clock_start > timeout:
                failure = 'timeout'
            if failure is not None and killed_at is None:
                try:
                    os.killpg(child.pid, signal.SIGKILL)
                except ProcessLookupError:
                    pass
                killed_at = now
            if killed_at is not None and now - killed_at > 2:
                break
            for key, _ in selector.select(timeout=0.02):
                data = os.read(key.fileobj.fileno(), 65536)
                if not data:
                    selector.unregister(key.fileobj)
                    continue
                name = key.data
                cap = output_limit if name == 'stdout' else min(output_limit, 1024 * 1024)
                remaining = max(0, cap - sizes[name])
                retained = data[:remaining]
                files[name].write(retained)
                digests[name].update(retained)
                sizes[name] += len(retained)
                if len(data) > remaining:
                    failure = 'output_limit'
        child.wait(timeout=3)
    finally:
        if child.poll() is None:
            os.killpg(child.pid, signal.SIGKILL)
            child.wait(timeout=3)
        selector.close()
        for stream in files.values():
            stream.close()
        child.stdout.close()
        child.stderr.close()
    ended = time.time()
    return {'argv': command, 'started_unix': started, 'ended_unix': ended,
            'wall_seconds': time.monotonic() - clock_start, 'exit_code': child.returncode,
            'failure': failure, 'before': before, 'after': snapshot(),
            'sampled_memory_current_max': sampled_max, 'sampled_peak_is_lower_bound': True,
            'output_bytes': sizes, 'output_sha256': {k: v.hexdigest() for k, v in digests.items()}}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--timeout', type=float, default=120)
    parser.add_argument('--output-limit', type=int, default=16 * 1024 * 1024)
    parser.add_argument('command', nargs=argparse.REMAINDER)
    args = parser.parse_args()
    command = args.command[1:] if args.command[:1] == ['--'] else args.command
    if not command or not 0 < args.timeout <= 600 or not 0 < args.output_limit <= 64 * 1024 * 1024:
        parser.error('command and bounded positive timeout/output limit required')
    destination = Path('/results')
    # All six supervisors reach this point before the host releases a batch.
    atomic_json(destination / 'ready.json', {'cgroup': snapshot()})
    deadline = time.monotonic() + 90
    start_file = Path('/control/start.json')
    while not start_file.exists():
        if time.monotonic() > deadline:
            raise TimeoutError('batch start barrier was not released')
        time.sleep(0.02)
    start_at = json.loads(start_file.read_text())['start_at_unix']
    while time.time() < start_at:
        time.sleep(max(0, min(0.01, start_at - time.time())))
    record = run(command, destination, args.timeout, args.output_limit)
    atomic_json(destination / 'receipt.json', record)
    raise SystemExit(0 if record['exit_code'] == 0 and record['failure'] is None else 1)


if __name__ == '__main__':
    main()
