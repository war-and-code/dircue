#!/usr/bin/env python3
"""Measure report import independently of Syft execution or directory profiling."""
import argparse
import copy
import hashlib
import json
import pathlib
import os
import platform
import statistics
import subprocess


def sha(data):
    return hashlib.sha256(data).hexdigest()


def create_reports(output):
    output.mkdir(parents=True, exist_ok=True)
    fixture = pathlib.Path('tests/packageevidence/fixtures/syft-1.52.0.json')
    native = json.loads(fixture.read_text())
    cases = [{'name': 'real-fixture', 'path': str(fixture.resolve()), 'synthetic': False, 'iterations': 100}]
    template = next(p for p in native['artifacts'] if p.get('metadataType') == 'dotnet-packages-lock-entry')
    for count in [1000, 20000]:
        report = {'artifacts': [], 'artifactRelationships': [], 'files': [], 'source': {'id': 'synthetic-directory', 'name': 'synthetic-monorepo', 'version': '', 'type': 'directory', 'metadata': {'path': '/synthetic'}}, 'distro': {}, 'descriptor': {'name': 'syft', 'version': '1.52.0', 'configuration': {}}, 'schema': native['schema']}
        for i in range(count):
            p = {key: copy.deepcopy(template[key]) for key in ('id', 'name', 'version', 'type', 'foundBy', 'language', 'metadataType') if key in template}
            p['metadata'] = {'name': 'Synthetic.Package%05d' % i, 'version': '1.2.3', 'type': 'Direct'}
            p['licenses'] = []
            p['cpes'] = []
            p['id'] = 'package-%05d' % i
            p['name'] = 'Synthetic.Package%05d' % i
            p['purl'] = 'pkg:nuget/' + p['name'] + '@1.2.3'
            location = '/projects/p%04d/packages.lock.json' % (i // 20)
            p['locations'] = [{'path': location, 'accessPath': location, 'annotations': {'evidence': 'primary'}}]
            report['artifacts'].append(p)
            report['artifactRelationships'].append({'parent': 'synthetic-directory', 'child': p['id'], 'type': 'contains'})
            if i:
                report['artifactRelationships'].append({'parent': p['id'], 'child': 'package-%05d' % (i-1), 'type': 'dependency-of'})
        path = output / ('synthetic-%d.json' % count)
        path.write_text(json.dumps(report, separators=(',', ':')) + '\n')
        assert path.stat().st_size < 16 << 20
        cases.append({'name': 'synthetic-%d' % count, 'path': str(path.resolve()), 'synthetic': True, 'iterations': 3 if count == 20000 else 10})
    (output / 'cases.json').write_text(json.dumps(cases, indent=2) + '\n')
    return cases


def time_command(command):
    if platform.system() == 'Darwin':
        timed = ['/usr/bin/time', '-l'] + command
    elif platform.system() == 'Linux':
        timed = ['/usr/bin/time', '-v'] + command
    else:
        raise RuntimeError('This receipt harness requires macOS or Linux process RSS accounting')
    result = subprocess.run(timed, capture_output=True, check=True, timeout=180)
    rss = None
    for line in result.stderr.decode().splitlines():
        if 'maximum resident set size' in line:
            rss = int(line.strip().split()[0])
        if 'Maximum resident set size (kbytes):' in line:
            rss = int(line.rsplit(':', 1)[1]) * 1024
    assert rss is not None, result.stderr.decode()
    return json.loads(result.stdout), rss


def main():
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument('--prepare', type=pathlib.Path, help='Generate bounded synthetic reports and case metadata; do not measure')
    p.add_argument('--cases', type=pathlib.Path)
    p.add_argument('--binary', type=pathlib.Path)
    p.add_argument('--output', type=pathlib.Path)
    p.add_argument('--runs', type=int, default=3)
    args = p.parse_args()
    if args.prepare:
        print(json.dumps(create_reports(args.prepare), indent=2))
        return
    if not all([args.cases, args.binary, args.output]) or args.runs < 3:
        p.error('measurement requires cases, binary, output and at least 3 runs')
    observations = []
    for case in json.loads(args.cases.read_text()):
        raw = pathlib.Path(case['path']).read_bytes()
        samples = []
        for _ in range(args.runs):
            result, rss = time_command([str(args.binary.resolve()), '--report', case['path'], '--iterations', str(case['iterations'])])
            result['process_peak_rss_bytes'] = rss
            samples.append(result)
        times = [row['ns_per_import'] for row in samples]
        observations.append({'name': case['name'], 'synthetic': case['synthetic'], 'report_sha256': sha(raw), 'report_bytes': len(raw), 'samples': samples, 'median_ms_per_import': statistics.median(times)/1e6, 'minimum_ms_per_import': min(times)/1e6, 'maximum_ms_per_import': max(times)/1e6, 'maximum_process_rss_bytes': max(row['process_peak_rss_bytes'] for row in samples)})
        print(case['name'], observations[-1]['median_ms_per_import'], flush=True)
    receipt = {'method': 'Separate child processes per sample; input read and first validation outside the timed loop, one GC before timing, subsequent importer allocations/GC included. Peak RSS covers the entire helper process including input, initial validation, and timed imports. No Syft scanner, source inventory, graph or output serialization is timed.', 'synthetic_scope': 'Generated native-schema package rows derived from a real .NET lockfile fixture. Synthetic locations and relationship chains are not observed repositories or resolved build graphs.', 'environment': {'system': platform.system(), 'machine': platform.machine(), 'platform': platform.platform(), 'GOMAXPROCS': os.environ.get('GOMAXPROCS', 'runtime-default')}, 'binary_sha256': sha(args.binary.read_bytes()), 'source_files': {str(path): sha(path.read_bytes()) for path in sorted([pathlib.Path('go.mod'), pathlib.Path('go.sum'), pathlib.Path('tests/packageevidence/bench/main.go')] + [path for path in pathlib.Path('pkg/packageevidence').glob('*.go') if not path.name.endswith('_test.go')])}, 'build_command': 'CGO_ENABLED=0 go build -mod=readonly -trimpath -buildvcs=false -o BENCH ./tests/packageevidence/bench', 'harness_sha256': sha(pathlib.Path(__file__).read_bytes()), 'cases': observations, 'statistical_limit': 'Three independent process samples report median/minimum/maximum; this is not a production p95 estimate.'}
    args.output.parent.mkdir(parents=True, exist_ok=True)
    args.output.write_text(json.dumps(receipt, indent=2, sort_keys=True)+'\n')


if __name__ == '__main__':
    main()
