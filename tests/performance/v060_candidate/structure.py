#!/usr/bin/env python3
"""Measure the incremental cost of optional hotspots over the same structural scan."""
import argparse
import copy
from datetime import datetime, timezone
import gzip
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import platform
import statistics
import subprocess
import sys

ROOT = Path(__file__).resolve().parents[3]
MEASUREMENT = ROOT / 'tests/performance/v050_candidate/benchmark.py'


def load_module(name, path):
    spec = importlib.util.spec_from_file_location(name, path)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


common = load_module('v050_structural_cost_measurement', MEASUREMENT)
require, digest, save = common.require, common.digest, common.save
sys.path.insert(0, str(ROOT / 'scripts'))
import hotspots_release_smoke as hotspot_oracle


def utc():
    return datetime.now(timezone.utc).isoformat()


def sha(data):
    return hashlib.sha256(data).hexdigest()


def git(root, *arguments):
    return subprocess.check_output(['git', '-C', str(root), *arguments])


def selected_blobs(root, suffix, count, expected_commit):
    commit = git(root, 'rev-parse', 'HEAD').decode().strip()
    require(commit == expected_commit, f'pinned commit mismatch: {root.name}')
    rows = []
    for entry in git(root, 'ls-tree', '-r', '-l', '-z', 'HEAD').split(b'\0'):
        if not entry:
            continue
        header, raw_path = entry.split(b'\t', 1)
        mode, kind, oid, size = header.split()
        path = raw_path.decode('utf-8')
        if (mode not in (b'100644', b'100755') or kind != b'blob' or
                not path.endswith(suffix) or not 0 < int(size) <= 1 << 20 or
                any(part.startswith('.') for part in Path(path).parts)):
            continue
        rows.append({'path': path, 'bytes': int(size), 'git_blob': oid.decode()})
    rows.sort(key=lambda row: row['path'])
    require(rows, f'no eligible sources: {root.name}')
    if len(rows) > count:
        ordinary = count - 2
        chosen = [rows[index * (len(rows) - 1) // (ordinary - 1)] for index in range(ordinary)]
        for row in sorted(rows, key=lambda item: (-item['bytes'], item['path'])):
            if row not in chosen:
                chosen.append(row)
            if len(chosen) == count:
                break
        rows = sorted(chosen, key=lambda row: row['path'])
    sources = {}
    for row in rows:
        data = git(root, 'cat-file', 'blob', row['git_blob'])
        data.decode('utf-8')
        require(b'\0' not in data and len(data) == row['bytes'], 'invalid selected source blob')
        row['sha256'] = sha(data)
        sources[row['path']] = data
    return sources, {'commit': commit, 'tree': git(root, 'rev-parse', 'HEAD^{tree}').decode().strip(),
                     'selection': 'Tracked regular blobs with requested suffix, non-dot paths and 1..1048576 bytes; '
                                  'spread over sorted paths plus two largest remaining inputs; selected blobs must be UTF-8 without NUL. '
                                  'Read committed Git blobs, not the working tree. Attributes explicitly include generated/vendor selections.',
                     'selected_blobs': rows}


def file_manifest(sources):
    return {name: {'bytes': len(data), 'sha256': sha(data)} for name, data in sorted(sources.items())}


def stage_case(output, name, sources, provenance, expected_files):
    directory = output / name
    directory.mkdir()
    for relative, data in sources.items():
        path = directory / relative
        require(not Path(relative).is_absolute() and '..' not in Path(relative).parts, 'unsafe corpus path')
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_bytes(data)
    return {'name': name, 'directory': name, 'files': file_manifest(sources),
            'expected_analyzed_files': expected_files, 'provenance': provenance}


def prepare(args):
    require(4 <= args.files <= 40, '--files must be 4..40')
    output = args.output.resolve()
    require(not output.exists(), 'prepared output must not exist')
    output.mkdir(parents=True)
    pins = {entry['name']: entry for entry in json.loads((ROOT / 'tests/performance/corpus.json').read_text())['projects']}
    cases = []
    for name, suffix in [('spring-framework', '.java'), ('roslyn', '.cs'), ('flask', '.py')]:
        sources, provenance = selected_blobs(args.corpus_root.resolve() / name, suffix, args.files, pins[name]['commit'])
        expected_files = len(sources)
        provenance['url'] = pins[name]['url']
        sources['.gitattributes'] = f'*{suffix} linguist-generated=false linguist-vendored=false linguist-documentation=false linguist-detectable=true\n'.encode()
        cases.append(stage_case(output, name, sources, provenance, expected_files))
    fixture_root = ROOT / 'tests/structural_breadth'
    fixtures = json.loads((fixture_root / 'fixtures.json').read_text())
    sources = {entry['path']: (fixture_root / 'testdata' / entry['path']).read_bytes() for entry in fixtures}
    cases.append(stage_case(output, 'grammar-fixtures', sources,
                            {'selection': 'All committed structural-breadth fixtures, naturally classified', 'fixtures': fixtures}, len(fixtures)))
    sources = dict(hotspot_oracle.FIXTURES)
    cases.append(stage_case(output, 'planted-population', sources,
                            {'selection': 'Independent release-smoke fixture with late highest-valued function beyond both old retention caps',
                             'expected_spaces': 1315, 'oracle_sha256': digest(ROOT / 'scripts/hotspots_release_smoke.py')}, len(sources)))
    save(output / 'manifest.json', {'schema': 'dircue-hotspot-cost-inputs-1', 'prepared_at_utc': utc(),
                                   'harness_sha256': digest(Path(__file__)), 'cases': cases})
    print(output / 'manifest.json')


def verify_input(root, case):
    directory = root / case['directory']
    sources = {}
    for path in directory.rglob('*'):
        require(not path.is_symlink(), 'prepared corpus contains symlink')
        if path.is_file():
            sources[path.relative_to(directory).as_posix()] = path.read_bytes()
        else:
            require(path.is_dir(), 'prepared corpus contains special file')
    require(file_manifest(sources) == case['files'], 'prepared corpus changed: ' + case['name'])
    return sources


def compare_outputs(default, selected, case, sources):
    require(default['schema_version'] != '1.5.0', 'default structural schema unexpectedly changed')
    require(selected['schema_version'] == '1.5.0', 'hotspot schema version missing')
    require('hotspots' not in default['structure'], 'default structural path includes hotspots')
    hotspot = selected['structure']['hotspots']
    hotspot_oracle.check_distribution(hotspot, sources)
    require(default['structure']['analyzed_files'] == selected['structure']['analyzed_files'] == case['expected_analyzed_files'],
            'unexpected file coverage: ' + case['name'])
    require(default['structure']['parse_count'] == selected['structure']['parse_count'] == case['expected_analyzed_files'],
            'hotspots caused extra source parsing')
    prior = copy.deepcopy(selected)
    del prior['structure']['hotspots']
    prior['schema_version'] = default['schema_version']
    qualifications = []
    if prior['structure']['status'] != default['structure']['status']:
        require(default['structure']['status'] == 'complete' and prior['structure']['status'] == hotspot['status'] == 'partial' and
                hotspot['file_coverage_status'] == 'complete' and hotspot['invalid_span_spaces'] > 0,
                'unexpected structural status change')
        qualifications.append('structure.status reflects invalid hotspot spans; file_coverage_status remains complete')
        prior['structure']['status'] = default['structure']['status']
    require(prior == default, 'hotspot option changed pre-existing report fields')
    if case['name'] == 'planted-population':
        hotspot_oracle.check_clean(hotspot)
    return {'analyzed_files': hotspot['analyzed_files'], 'total_spaces': hotspot['total_spaces'],
            'invalid_span_spaces': hotspot['invalid_span_spaces'], 'recovered_files': hotspot['recovered_files'],
            'status': hotspot['status'], 'existing_field_qualifications': qualifications}


def measure(args):
    require(args.repetitions >= 5 and args.warmups >= 1, 'at least 5 paired repetitions and 1 warmup required')
    prepared, output = args.prepared.resolve(), args.output.resolve()
    require(not output.exists(), 'measurement output must be fresh')
    manifest = json.loads((prepared / 'manifest.json').read_text())
    require(manifest['schema'] == 'dircue-hotspot-cost-inputs-1', 'unexpected input manifest')
    build = json.loads(args.build_receipt.read_text())
    candidate, worker = args.candidate.resolve(), args.worker.resolve()
    candidate_hash, worker_hash = digest(candidate), digest(worker)
    require(build['candidate_sha256'] == candidate_hash, 'candidate does not match supplied build receipt')
    inputs = {case['name']: verify_input(prepared, case) for case in manifest['cases']}
    output.mkdir(parents=True)
    receipt = {'schema': 'dircue-hotspot-cost-result-1', 'started_at_utc': utc(),
               'candidate_sha256': candidate_hash, 'worker_sha256': worker_hash,
               'build_receipt_sha256': digest(args.build_receipt), 'source_at_build': build['source_at_build'],
               'harness_sha256': digest(Path(__file__)), 'measurement_helper_sha256': digest(MEASUREMENT),
               'oracle_sha256': digest(ROOT / 'scripts/hotspots_release_smoke.py'),
               'input_manifest_sha256': digest(prepared / 'manifest.json'),
               'platform': platform.platform(), 'processor_count': os.cpu_count(), 'environment_note': args.environment_note,
               'method': {'unit': 'whole CLI invocation including capability probe and worker process startup',
                          'lanes': ['structure', 'hotspots'], 'pair_order': 'structure/hotspots then hotspots/structure on alternating rounds',
                          'warmups_per_lane': args.warmups, 'measured_pairs_per_case': args.repetitions,
                          'workers': args.workers, 'source': 'directory',
                          'cache': 'warm; committed blobs staged and hashed before warmups; no cache eviction',
                          'rss': 'Operating-system /usr/bin/time maximum resident-set statistic for the wrapped invocation. '
                                 'Not a simultaneous sum of core and worker RSS, not a cgroup/process-tree memory budget.',
                          'scope': 'Bounded source subsets and authored fixtures, not full-repository performance or a universal speedup.'}, 'cases': []}
    for case in manifest['cases']:
        directory = prepared / case['directory']
        command = [str(candidate), 'analyze', 'structure', '--source', 'directory', '--json', '--files',
                   '--workers', str(args.workers), '--structural-worker', str(worker), str(directory)]
        commands = {'structure': command, 'hotspots': [*command, '--hotspots']}
        expected, warmups = {}, {}
        for lane in commands:
            warmups[lane] = []
            for _ in range(args.warmups):
                payload, timing = common.measure(commands[lane], args.timeout)
                if lane in expected:
                    require(expected[lane] == payload, 'warmup output changed')
                expected[lane] = payload
                warmups[lane].append(timing)
        observed = compare_outputs(json.loads(expected['structure']), json.loads(expected['hotspots']), case, inputs[case['name']])
        samples, execution_order = {lane: [] for lane in commands}, []
        for repetition in range(args.repetitions):
            for lane in (['structure', 'hotspots'] if repetition % 2 == 0 else ['hotspots', 'structure']):
                payload, timing = common.measure(commands[lane], args.timeout)
                require(payload == expected[lane], 'measured output changed: ' + case['name'])
                timing['round'] = repetition + 1
                samples[lane].append(timing)
                execution_order.append(lane)
        summaries = {lane: {'median_seconds': statistics.median(row['seconds'] for row in rows),
                            'min_seconds': min(row['seconds'] for row in rows), 'max_seconds': max(row['seconds'] for row in rows),
                            'median_time_max_rss_bytes': statistics.median(row['peak_rss_bytes'] for row in rows),
                            'max_time_max_rss_bytes': max(row['peak_rss_bytes'] for row in rows)} for lane, rows in samples.items()}
        artifacts = {}
        for lane, payload in expected.items():
            path = output / f'{case["name"]}-{lane}.json.gz'
            path.write_bytes(gzip.compress(payload, mtime=0))
            artifacts[path.name] = digest(path)
        result = {**case, 'commands': commands, 'warmups': warmups, 'samples': samples, 'execution_order': execution_order,
                  'summary': summaries, 'observed': observed, 'artifacts': artifacts,
                  'median_time_change_percent': 100 * (summaries['hotspots']['median_seconds'] / summaries['structure']['median_seconds'] - 1),
                  'paired_time_change_percent': [100 * (new['seconds'] / old['seconds'] - 1) for old, new in zip(samples['structure'], samples['hotspots'])]}
        receipt['cases'].append(result)
        save(output / f'{case["name"]}-receipt.json', result)
        print(case['name'], 'median hotspot time change', round(result['median_time_change_percent'], 2), '%', flush=True)
    for case in manifest['cases']:
        verify_input(prepared, case)
    require(digest(candidate) == candidate_hash and digest(worker) == worker_hash, 'benchmark binary changed')
    receipt.update(passed=True, finished_at_utc=utc())
    save(output / 'receipt.json', receipt)
    print(output / 'receipt.json')


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    subs = parser.add_subparsers(dest='command', required=True)
    prep = subs.add_parser('prepare', help='Copy bounded committed source blobs and fixtures; never run them')
    prep.add_argument('--corpus-root', type=Path, required=True)
    prep.add_argument('--output', type=Path, required=True)
    prep.add_argument('--files', type=int, default=40)
    timed = subs.add_parser('measure', help='Measure already-built dircue and worker executables')
    for name in ('prepared', 'candidate', 'worker', 'build-receipt', 'output'):
        timed.add_argument('--' + name, type=Path, required=True)
    timed.add_argument('--repetitions', type=int, default=5)
    timed.add_argument('--warmups', type=int, default=1)
    timed.add_argument('--workers', type=int, default=8)
    timed.add_argument('--timeout', type=int, default=300)
    timed.add_argument('--environment-note', required=True)
    args = parser.parse_args()
    try:
        (prepare if args.command == 'prepare' else measure)(args)
    except (ValueError, OSError, subprocess.SubprocessError) as error:
        parser.exit(1, f'structural benchmark failed: {error}\n')


if __name__ == '__main__':
    main()
