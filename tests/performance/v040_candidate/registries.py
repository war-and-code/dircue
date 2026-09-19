#!/usr/bin/env python3
"""Measure the explicit cost of registry inventory, with semantic checks."""
import argparse
import gzip
import json
from pathlib import Path
import statistics

import benchmark as bench

FIXTURE = {
    'App.csproj': '<Project Sdk="Microsoft.NET.Sdk"><PropertyGroup><TargetFramework>net8.0</TargetFramework></PropertyGroup></Project>\n',
    'App.cs': 'public class App { public int Value() => 1; }\n',
    'NuGet.Config': '<configuration><packageSources><clear/><add key="Example" value="https://synthetic-user:synthetic-password@nuget.example/private?token=synthetic-query"/></packageSources><disabledPackageSources><add key="Example" value="false"/></disabledPackageSources></configuration>\n',
    '.npmrc': '@company:registry=https://npm.example/private\n//npm.example/:_authToken=synthetic-auth-token\n',
}


def prepare(args):
    root = args.fixture.resolve()
    root.mkdir(parents=True, exist_ok=False)
    for name, content in FIXTURE.items():
        (root / name).write_text(content)
    prior = json.loads(gzip.decompress(args.default_prepared.read_bytes()))
    cases = []
    for name in ('cobra-git', 'roslyn-directory', 'xml-and-dotnet'):
        original = next(c for c in prior['cases'] if c['name'] == name)
        cases.append({'name': name.replace('-git', '-directory'), 'path': original['path']})
    cases.append({'name': 'synthetic-registry-project', 'path': str(root)})
    for case in cases:
        path = Path(case['path'])
        case['input'] = bench.inventory(path, 'directory')
        files = [p for p in path.rglob('*') if p.is_file() and not p.is_symlink()
                 and '.git' not in p.relative_to(path).parts and (p.name.lower() == 'nuget.config' or p.name == '.npmrc')]
        assert len(files) <= 64 and all(p.stat().st_size <= 262144 for p in files)
        case['expected_configurations'] = sorted(p.relative_to(path).as_posix() for p in files)
        case['expected_content_bytes'] = sum(p.stat().st_size for p in files)
    data = {'candidate': prior['candidate'], 'candidate_sha256': prior['candidate_sha256'],
            'candidate_build_provenance': prior['candidate_build_provenance'],
            'default_prepared_sha256': bench.digest(args.default_prepared),
            'harness_sha256': bench.digest(Path(__file__)), 'measure_helper_sha256': bench.digest(Path(bench.__file__)),
            'platform': prior['platform'], 'hardware': prior['hardware'], 'cases': cases,
            'fixture': 'Four synthetic files containing deliberate credential sentinels; no actual credentials.',
            'prepared_at_utc': bench.utc()}
    bench.save(args.output, data)
    print(args.output, flush=True)


def validate(case, payloads):
    base, combined, standalone = (json.loads(payloads[k]) for k in ('projects', 'projects_registries', 'registries'))
    assert base['schema_version'] == '1.2.0'
    assert combined['schema_version'] == standalone['schema_version'] == '1.3.0'
    before = {k:v for k,v in base.items() if k != 'schema_version'}
    after = {k:v for k,v in combined.items() if k not in ('schema_version', 'registries')}
    assert before == after, case['name'] + ': existing module data changed'
    assert combined['registries'] == standalone['registries'], case['name'] + ': standalone registry data differs'
    registries = standalone['registries']
    assert [c['path'] for c in registries['configurations']] == case['expected_configurations']
    coverage = registries['coverage']
    assert coverage['candidate_files'] == coverage['admitted_files'] == coverage['read_files'] == len(case['expected_configurations'])
    assert coverage['bytes_read'] == case['expected_content_bytes']
    assert coverage['enumeration_complete'] and not registries['omissions']
    assert coverage['bytes_read'] <= 128 * 262145
    assert registries['scope']['supported_configurations'] == ['nuget_config_basename_case_insensitive', 'npmrc_basename_exact']
    if case['name'] == 'synthetic-registry-project':
        origins = sorted(d['endpoint']['origin'] for c in registries['configurations'] for d in c['declarations'] if d.get('endpoint', {}).get('origin'))
        assert origins == ['https://npm.example', 'https://nuget.example']
        for payload in (payloads['projects_registries'], payloads['registries']):
            assert not any(s.encode() in payload for s in ('synthetic-user', 'synthetic-password', 'synthetic-query', 'synthetic-auth-token'))
    return registries


def collect(args):
    data = json.loads(gzip.decompress(args.prepared.read_bytes()))
    assert not args.output.exists()
    assert data['harness_sha256'] == bench.digest(Path(__file__))
    assert data['measure_helper_sha256'] == bench.digest(Path(bench.__file__))
    assert bench.digest(Path(data['candidate'])) == data['candidate_sha256']
    data['started_at_utc'] = bench.utc()
    data['method'] = {'repetitions': 5, 'warmups': 1,
                      'order': 'Five rounds rotating projects, projects+registries and registries-only by one lane.',
                      'environment': args.environment_note,
                      'scope': 'Incremental opt-in cost on the same current candidate. Registry-only skips language/project profiling and is not a substitute for those reports.',
                      'bytes_read': 'Registry configuration content only; excludes inventory, attributes, Git storage and other modules.',
                      'limits': 'Warm cache, one host, no enforced CPU/RAM limits; no universal performance claim.'}
    for case in data['cases']:
        common = ['--json', '--source', 'directory', '--workers', '8', '--tree-size', '1000000', case['path']]
        commands = {'projects': [data['candidate'], 'analyze', 'all', '--projects', *common],
                    'projects_registries': [data['candidate'], 'analyze', 'all', '--projects', '--registries', *common],
                    'registries': [data['candidate'], 'analyze', 'registries', *common]}
        expected, warmups = {}, {}
        for name, command in commands.items():
            expected[name], warmups[name] = bench.measure(command)
        report = validate(case, expected)
        samples, order = {k:[] for k in commands}, []
        keys = list(commands)
        for index in range(5):
            for key in keys[index % 3:] + keys[:index % 3]:
                payload, sample = bench.measure(commands[key])
                assert payload == expected[key], (case['name'], key, index)
                sample['round'] = index + 1
                samples[key].append(sample)
                order.append(key)
        case.update(commands=commands, warmups=warmups, samples=samples, execution_order=order, registries=report, artifacts={})
        case['summary'] = {k:{'median_seconds':statistics.median(s['seconds'] for s in rows),
                              'median_peak_rss_bytes':statistics.median(s['peak_rss_bytes'] for s in rows)} for k,rows in samples.items()}
        old, new = (case['summary'][k] for k in ('projects','projects_registries'))
        case['incremental'] = {'median_seconds_added':new['median_seconds']-old['median_seconds'],
                               'median_time_change_percent':100*(new['median_seconds']/old['median_seconds']-1),
                               'median_rss_bytes_added':new['median_peak_rss_bytes']-old['median_peak_rss_bytes'],
                               'paired_time_change_percent':[100*(n['seconds']/o['seconds']-1) for o,n in zip(samples['projects'],samples['projects_registries'])]}
        for key,payload in expected.items():
            file=args.output.parent / f'registry-{case["name"]}-{key}.json.gz'
            assert not file.exists()
            file.parent.mkdir(parents=True,exist_ok=True)
            file.write_bytes(gzip.compress(payload,mtime=0))
            case['artifacts'][file.name]=bench.digest(file)
        print(case['name'],case['incremental'], 'configuration_bytes',case['expected_content_bytes'],flush=True)
    data['timings_finished_at_utc']=bench.utc()
    print('REGISTRY TIMINGS FINISHED; checking input identities',flush=True)
    assert bench.digest(Path(data['candidate']))==data['candidate_sha256']
    for case in data['cases']:
        assert bench.inventory(Path(case['path']),'directory')==case['input'],case['name']
    data.update(passed=True,finished_at_utc=bench.utc())
    bench.save(args.output,data)
    print(args.output,flush=True)


if __name__=='__main__':
    parser=argparse.ArgumentParser(description=__doc__)
    sub=parser.add_subparsers(dest='command',required=True)
    prep=sub.add_parser('prepare')
    for name in ('default-prepared','fixture','output'):prep.add_argument('--'+name,type=Path,required=True)
    timing=sub.add_parser('measure')
    for name in ('prepared','output'):timing.add_argument('--'+name,type=Path,required=True)
    timing.add_argument('--environment-note',required=True)
    args=parser.parse_args()
    (prepare if args.command=='prepare' else collect)(args)
