#!/usr/bin/env python3
"""Measure optional graph construction and JSON output against project profiling."""
import argparse
import hashlib
import json
import os
import pathlib
import platform
import statistics
import subprocess
import time


def sha(data):
    return hashlib.sha256(data).hexdigest()


def measured(command):
    timer = ['/usr/bin/time', '-l' if platform.system() == 'Darwin' else '-v']
    started = time.perf_counter()
    result = subprocess.run(timer + command, check=True, capture_output=True, timeout=180)
    seconds = time.perf_counter() - started
    rss = None
    for line in result.stderr.decode().splitlines():
        if 'maximum resident set size' in line:
            rss = int(line.split()[0])
        if 'Maximum resident set size (kbytes):' in line:
            rss = int(line.rsplit(':', 1)[1]) * 1024
    assert rss is not None, result.stderr.decode()
    report = json.loads(result.stdout)
    if 'graph' in report:
        observations = {'projects': len(report['projects']['projects']), 'graph_nodes': len(report['graph']['nodes']), 'graph_edges': report['graph']['coverage']['unique_edges'], 'status': report['graph']['status']}
    else:
        observations = {'projects': len(report['projects']['projects']), 'status': report['projects']['status']}
    return {'wall_seconds': seconds, 'process_peak_rss_bytes': rss, 'stdout_bytes': len(result.stdout), 'stdout_sha256': sha(result.stdout), 'observations': observations}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--binary', type=pathlib.Path, required=True)
    parser.add_argument('--build-inputs', type=pathlib.Path, required=True)
    parser.add_argument('--output', type=pathlib.Path, required=True)
    parser.add_argument('--runs', type=int, default=3)
    parser.add_argument('cases', nargs='+', help='NAME=git|directory=PATH')
    args = parser.parse_args()
    assert platform.system() in {'Darwin', 'Linux'} and args.runs >= 3
    inputs = json.loads(args.build_inputs.read_text())
    for name, value in inputs['files'].items():
        assert sha(pathlib.Path(name).read_bytes()) == value, 'candidate source changed: ' + name
    cases = []
    for item in args.cases:
        name, mode, root = item.split('=', 2)
        assert mode in {'git', 'directory'}
        commands = {kind: [str(args.binary.resolve()), 'analyze', kind, '--json', '--source', mode, str(pathlib.Path(root).resolve())] for kind in ['projects', 'graph']}
        # Warm both process paths and filesystem caches. Warmups are not samples.
        for cmd in commands.values():
            subprocess.run(cmd, check=True, stdout=subprocess.DEVNULL, stderr=subprocess.PIPE, timeout=180)
        samples = {'projects': [], 'graph': []}
        for iteration in range(args.runs):
            for kind in (['projects', 'graph'] if iteration % 2 == 0 else ['graph', 'projects']):
                samples[kind].append(measured(commands[kind]))
        summaries = {}
        for kind, rows in samples.items():
            assert len({row['stdout_sha256'] for row in rows}) == 1, 'nondeterministic output during benchmark'
            durations = [row['wall_seconds'] for row in rows]
            summaries[kind] = {'median_seconds': statistics.median(durations), 'minimum_seconds': min(durations), 'maximum_seconds': max(durations), 'maximum_process_rss_bytes': max(row['process_peak_rss_bytes'] for row in rows)}
        delta = summaries['graph']['median_seconds'] - summaries['projects']['median_seconds']
        cases.append({'name': name, 'source_mode': mode, 'samples': samples, 'summaries': summaries, 'graph_median_delta_seconds': delta, 'graph_median_delta_percent': delta/summaries['projects']['median_seconds']*100})
        print(name, summaries, flush=True)
    receipt = {'method': 'One untimed warmup per command, then three samples each in alternating order. Each child process performs inventory, project parsing and JSON serialization. Graph adds static .NET graph construction and graph JSON. Python wall clock includes process launch and captured output; process RSS comes from /usr/bin/time. No repository builds, restores or scripts run.', 'statistical_limit': 'Median/minimum/maximum of three warm local samples; small differences can be noise. This is not a production p95 estimate, a cold-cache result, or a comparison with evaluated MSBuild.', 'environment': {'system': platform.system(), 'machine': platform.machine(), 'platform': platform.platform(), 'logical_cpus': os.cpu_count(), 'GOMAXPROCS': os.environ.get('GOMAXPROCS', 'runtime-default')}, 'binary_sha256': sha(args.binary.read_bytes()), 'source_commit': inputs['source_commit'], 'source_dirty': inputs['dirty'], 'source_files': inputs['files'], 'build_inputs_sha256': sha(args.build_inputs.read_bytes()), 'harness_sha256': sha(pathlib.Path(__file__).read_bytes()), 'cases': cases}
    args.output.parent.mkdir(parents=True, exist_ok=True)
    args.output.write_text(json.dumps(receipt, indent=2, sort_keys=True)+'\n')


if __name__ == '__main__':
    main()
