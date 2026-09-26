#!/usr/bin/env python3
"""Build the final reader and two reservation-cap experiments through Go overlays."""
from pathlib import Path
import argparse
import importlib.util
import json
import os
import shutil
ROOT = Path(__file__).resolve().parents[3]
parser = argparse.ArgumentParser(description='Build source-bound baseline and reader reservation variants without downloading dependencies.')
parser.add_argument('--output', type=Path, required=True)
parser.add_argument('--go-binary', default='go')
args = parser.parse_args()
OUT = args.output.resolve()
OUT.mkdir(parents=True, exist_ok=False)
source = (ROOT / 'pkg/scanner/read.go').read_bytes()
if source.count(b'maxAttributesBytes+1') != 1:
    raise ValueError('reader reservation expression changed; inspect before adapting this experiment')
for lane, cap in [('growth', None), ('growth128', 128 << 10), ('growth512', 512 << 10)]:
    data = source if cap is None else source.replace(b'maxAttributesBytes+1', str(cap).encode() + b'+1')
    target = OUT / f'read-{lane}.go'
    target.write_bytes(data)
    (OUT / f'{lane}.overlay.json').write_text(json.dumps({'Replace': {str(ROOT / 'pkg/scanner/read.go'): str(target)}}))
for name in ['git.go', 'scanner.go']:
    (OUT / name).write_bytes((ROOT / 'pkg/scanner' / name).read_bytes())
spec = importlib.util.spec_from_file_location('attrib', ROOT / 'tests/performance/bounded_reader/profile_allocations.py')
a = importlib.util.module_from_spec(spec)
spec.loader.exec_module(a)
a.GO_BINARY = args.go_binary
rev = 'aa0492135186b84d3d34c57f0f27908dc73cfc24'
if a.sha(ROOT / 'main.go') != a.digest(a.tracked_at(rev, 'main.go')):
    raise ValueError('main.go differs from the selected baseline')
env = dict(os.environ, CGO_ENABLED='0', GOWORK='off', GOFLAGS='', GOPROXY='off', GOTOOLCHAIN='local', GOSUMDB='off')
for p in OUT.glob('*.overlay.json'):
    x = json.loads(p.read_text())
    x['Replace'].update({str(ROOT / 'pkg/scanner' / name): str(OUT / name) for name in ['git.go', 'scanner.go']})
    p.write_text(json.dumps(x))
base = {str(ROOT / 'pkg/scanner/read.go'): ''}
for name in ['scanner.go', 'git.go']:
    p = OUT / f'baseline-{name}'
    p.write_bytes(a.tracked_at(rev, 'pkg/scanner/' + name))
    base[str(ROOT / 'pkg/scanner' / name)] = str(p)
a.write_json(OUT / 'baseline.overlay.json', {'Replace': base})
start_harness = {p.name: a.sha(p) for p in [Path(__file__), ROOT / 'tests/performance/bounded_reader/profile_allocations.py']}
goenv = json.loads(a.command(['go', 'env', '-json', 'GOVERSION', 'GOTOOLDIR', 'GOOS', 'GOARCH'], env=env))
tools = {name: Path(goenv['GOTOOLDIR']) / name for name in ['compile', 'link', 'asm']}
tools['go'] = Path(shutil.which(a.GO_BINARY) or a.GO_BINARY)
toolhash = {n: a.sha(p) for n, p in tools.items()}
builds = {}
for lane in ['baseline', 'growth', 'growth128', 'growth512']:
    overlay = OUT / f'{lane}.overlay.json'
    if not overlay.exists():
        continue
    before = a.source_records(overlay, env, rev, lane == 'baseline')
    binary = OUT / f'dircue-{lane}'
    flags = ['-mod=readonly', f'-overlay={overlay}', '-trimpath', '-buildvcs=false', '-ldflags=-s -w -X dircue/internal/cli.Version=0.5.0']
    a.command(['go', 'build', *flags, '-o', str(binary), '.'], env=env, timeout=300)
    after = a.source_records(overlay, env, rev, lane == 'baseline')
    assert before == after, lane
    builds[lane] = {'sha256': a.sha(binary), 'inputs': before, 'build_flags': flags, 'overlay_sha256': a.sha(overlay)}
    print(lane, builds[lane]['sha256'], flush=True)
for lane, b in builds.items():
    assert b['inputs'] == a.source_records(OUT / f'{lane}.overlay.json', env, rev, lane == 'baseline')
assert toolhash == {n: a.sha(p) for n, p in tools.items()}
assert start_harness == {p.name: a.sha(p) for p in [Path(__file__), ROOT / 'tests/performance/bounded_reader/profile_allocations.py']}
a.write_json(OUT / 'builds.json', {'baseline_revision': rev, 'builds': builds, 'tool_hashes': toolhash, 'harness_hashes': start_harness, 'goenv': goenv})
