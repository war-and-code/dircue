#!/usr/bin/env python3
"""Measure exact-output reader variants using fresh CLI processes and alternating pairs."""
from pathlib import Path
import argparse
import importlib.util
import json
import statistics
import os
import datetime
import gzip
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
p.add_argument('--xml-root', type=Path, required=True)
args = p.parse_args()
HERE = args.build_dir.resolve()
out = HERE / 'memory-target-measurements'
out.mkdir(exist_ok=False)
corpus = args.xml_root.resolve()
binary = HERE / 'dircue-growth'
builds = json.loads((HERE / 'builds.json').read_text())
binaryhash = a.sha(binary)
assert binaryhash == builds['builds']['growth']['sha256']
harnesshash = a.sha(Path(__file__))
dependencyhashes = {name: a.sha(ROOT / name) for name in ['tests/performance/v060_candidate/benchmark.py', 'tests/performance/bounded_reader/profile_allocations.py']}
fixture = b.inventory(corpus, 'directory')
original = os.environ.get('GOMEMLIMIT')
runtimeenv = {k: os.environ.get(k) for k in ['GOGC', 'GODEBUG', 'GOMAXPROCS', 'GOMEMLIMIT']}
targets = ['32MiB', '64MiB', '128MiB']
rows = {t: {'unset': [], 'target': []} for t in targets}
order = []
gold = None
cmd = [str(binary), 'analyze', 'all', '--json', '--source', 'directory', '--workers', '8', '--tree-size', '1000000', str(corpus)]

def run(target):
    global gold
    if target is None:
        os.environ.pop('GOMEMLIMIT', None)
    else:
        os.environ['GOMEMLIMIT'] = target
    stdout, sample = b.measure(cmd, 120)
    sample.pop('time_diagnostics', None)
    if gold is None:
        gold = stdout
        gzip.open(out / 'stdout.json.gz', 'wb').write(stdout)
    assert stdout == gold, 'output mismatch'
    return sample
try:
    for target in [None, *targets]:
        run(target)
    for repetition in range(20):
        for target in targets[repetition % 3:] + targets[:repetition % 3]:
            for setting in [None, target] if repetition % 2 == 0 else [target, None]:
                sample = run(setting)
                lane = 'unset' if setting is None else 'target'
                rows[target][lane].append(sample)
                order.append({'repetition': repetition, 'comparison': target, 'setting': setting, **sample})
finally:
    if original is None:
        os.environ.pop('GOMEMLIMIT', None)
    else:
        os.environ['GOMEMLIMIT'] = original
assert fixture == b.inventory(corpus, 'directory')
assert binaryhash == a.sha(binary)
assert harnesshash == a.sha(Path(__file__))
assert dependencyhashes == {name: a.sha(ROOT / name) for name in dependencyhashes}
summary = {}
for target, pairs in rows.items():
    m = {lane: {key: statistics.median((s[key] for s in samples)) for key in ['seconds', 'peak_rss_bytes']} for lane, samples in pairs.items()}
    m['change_percent'] = {key: (m['target'][key] / m['unset'][key] - 1) * 100 for key in m['unset']}
    summary[target] = m
result = {'created_at_utc': datetime.datetime.now(datetime.timezone.utc).isoformat(), 'schema': 'dircue-memory-target-characterization-1', 'summary': summary, 'samples': rows, 'execution_order': order, 'binary_sha256': binaryhash, 'harness_sha256': harnesshash, 'dependency_harness_sha256': dependencyhashes, 'fixture': fixture, 'exact_output_equal': True, 'warmups_per_setting': 1, 'measured_pairs_per_target': 20, 'runtime_environment_before_experiment': runtimeenv, 'environment_note': 'Warm filesystem cache; record actual hardware and background activity separately. GOMEMLIMIT explicitly removed for unset lane and set per target lane; other inherited runtime settings recorded. Same final growth-fixed1MiB binary throughout.', 'limits': ['Characterization points on this workload, not recommended universal memory settings.', 'GOMEMLIMIT is a soft Go runtime memory target, not a process RSS or container memory hard limit.', 'Twenty samples do not characterize cold caches or other hosts.']}
a.write_json(out / 'results.json', result)
print(json.dumps(summary, indent=2))
