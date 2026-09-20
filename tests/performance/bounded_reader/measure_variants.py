#!/usr/bin/env python3
"""Measure exact-output reader variants using fresh CLI processes and alternating pairs."""
from pathlib import Path
import argparse
import importlib.util
import json
import statistics
import datetime
import gzip
import os
ROOT = Path(__file__).resolve().parents[3]

def load(name, path):
    s = importlib.util.spec_from_file_location(name, path)
    m = importlib.util.module_from_spec(s)
    s.loader.exec_module(m)
    return m
b = load('bench', ROOT / 'tests/performance/v060_candidate/benchmark.py')
a = load('attrib', ROOT / 'tests/performance/bounded_reader/profile_allocations.py')
p = argparse.ArgumentParser(description=__doc__)
p.add_argument('--build-dir', type=Path, required=True)
p.add_argument('--corpus', default='.cache/staged-analysis/fixtures/xml-only')
p.add_argument('--name', default='xml')
p.add_argument('--variants', default='growth,growth128,growth512')
p.add_argument('--count', type=int, default=20)
args = p.parse_args()
if args.count < 1:
    p.error('--count must be positive')
if any(lane not in ('growth', 'growth128', 'growth512') for lane in args.variants.split(',')):
    p.error('--variants must select growth, growth128, or growth512')
HERE = args.build_dir.resolve()
out = HERE / f'{args.name}-measurements'
out.mkdir(exist_ok=False)
corpus = (ROOT / args.corpus).resolve()
variants = args.variants.split(',')
lanes = ['baseline', *variants]
builds = json.loads((HERE / 'builds.json').read_text())
start = {lane: a.sha(HERE / f'dircue-{lane}') for lane in lanes}
assert all((start[lane] == builds['builds'][lane]['sha256'] for lane in lanes))
sourcehash = a.sha(Path(__file__))
dependencyhashes = {str(q.relative_to(ROOT)): a.sha(q) for q in [ROOT / 'tests/performance/v060_candidate/benchmark.py', ROOT / 'tests/performance/bounded_reader/profile_allocations.py']}
runtimeenv = {k: os.environ.get(k) for k in ['GOGC', 'GODEBUG', 'GOMAXPROCS', 'GOMEMLIMIT']}
fixture = b.inventory(corpus, 'directory')
rows = {variant: {mode: {'baseline': [], 'candidate': []} for mode in ['languages', 'all']} for variant in variants}
gold = {}
raw = []

def run(lane, mode):
    cmd = [str(HERE / f'dircue-{lane}'), 'analyze', mode, '--json', '--source', 'directory', '--workers', '8', '--tree-size', '1000000', str(corpus)]
    stdout, sample = b.measure(cmd, 120)
    sample.pop('time_diagnostics', None)
    if mode not in gold:
        gold[mode] = stdout
        gzip.open(out / f'{mode}-stdout.json.gz', 'wb').write(stdout)
    assert stdout == gold[mode], (lane, mode, 'output mismatch')
    return sample
for mode in ['languages', 'all']:
    for lane in lanes:
        run(lane, mode)
    for repetition in range(args.count):
        order = variants[repetition % len(variants):] + variants[:repetition % len(variants)]
        for variant in order:
            pair = ['baseline', variant] if repetition % 2 == 0 else [variant, 'baseline']
            for lane in pair:
                sample = run(lane, mode)
                key = 'baseline' if lane == 'baseline' else 'candidate'
                rows[variant][mode][key].append(sample)
                raw.append({'variant': variant, 'mode': mode, 'repetition': repetition, 'lane': lane, **sample})
assert fixture == b.inventory(corpus, 'directory')
assert start == {lane: a.sha(HERE / f'dircue-{lane}') for lane in lanes}
assert sourcehash == a.sha(Path(__file__))
assert dependencyhashes == {name: a.sha(ROOT / name) for name in dependencyhashes}
summary = {}
for variant, modes in rows.items():
    summary[variant] = {}
    for mode, samples in modes.items():
        base = {k: statistics.median((r[k] for r in samples['baseline'])) for k in ['seconds', 'peak_rss_bytes']}
        candidate = {k: statistics.median((r[k] for r in samples['candidate'])) for k in base}
        summary[variant][mode] = {'baseline': base, 'candidate': candidate, 'change_percent': {k: (candidate[k] / base[k] - 1) * 100 for k in base}}
result = {'created_at_utc': datetime.datetime.now(datetime.timezone.utc).isoformat(), 'summary': summary, 'samples': rows, 'execution_order': raw, 'binary_sha256': start, 'harness_sha256': sourcehash, 'dependency_harness_sha256': dependencyhashes, 'runtime_environment': runtimeenv, 'fixture': fixture, 'exact_output_equal': True, 'warmups_per_mode_per_binary': 1, 'measured_pairs_per_variant_per_mode': args.count, 'environment_note': 'Warm filesystem cache; run only after stopping competing build, test and benchmark work. Record actual hardware and background activity separately.'}
a.write_json(out / 'results.json', result)
print(json.dumps(summary, indent=2))
