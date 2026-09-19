#!/usr/bin/env python3
"""Check synthetic supported/unsupported cases against pinned npm/ini, then Go."""
import argparse
import hashlib
import json
from pathlib import Path
import subprocess
import urllib.request

ROOT = Path(__file__).resolve().parents[2]
REVISION = '180a8d5c72d7f13ed70c53619132f3a8ee5ac6ed'
SOURCE_SHA256 = '007123836ffec243f41c8b35584fb3db0498aa12a440a9e0efc675d0000f4c32'
URL = f'https://raw.githubusercontent.com/npm/ini/{REVISION}/lib/ini.js'


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--output', type=Path, required=True)
    args = parser.parse_args()
    assert not args.output.exists()
    fixtures = json.loads((Path(__file__).parent / 'npm-cases.json').read_text())
    # The oracle is trusted upstream tooling; no inspected repository code runs.
    source = urllib.request.urlopen(URL, timeout=30).read(1024 * 1024)
    assert hashlib.sha256(source).hexdigest() == SOURCE_SHA256, 'upstream oracle source differs'
    oracle = ROOT / '.cache/registries-oracle' / REVISION / 'ini.cjs'
    oracle.parent.mkdir(parents=True, exist_ok=True)
    oracle.write_bytes(source)
    payload = json.dumps(fixtures).encode()
    node = "const ini=require(process.argv[1]);let x='';process.stdin.on('data',d=>x+=d);process.stdin.on('end',()=>process.stdout.write(JSON.stringify(JSON.parse(x).map(v=>{const value=ini.parse(v.text);return {value,types:Object.fromEntries(Object.entries(value).map(([k,v])=>[k,typeof v]))}}))));"
    actual = json.loads(subprocess.check_output(['node', '-e', node, str(oracle)], input=payload))
    observed = json.loads(subprocess.check_output(['go', 'run', './tests/registries/probe'], cwd=ROOT, input=payload))
    results = []
    for fixture, npm, report in zip(fixtures, actual, observed, strict=True):
        assert npm['value'] == fixture['npm'], fixture['id'] + ': pinned npm interpretation changed'
        if 'npm_types' in fixture:
            assert npm['types'] == fixture['npm_types'], fixture['id'] + ': pinned npm type changed'
        origins = [d['endpoint']['origin'] for d in report['declarations'] if d.get('endpoint', {}).get('origin')]
        assert origins == fixture['origins'] and report['status'] == fixture['status'], fixture['id'] + ': Go subset mismatch'
        encoded = json.dumps(report)
        for sentinel in ['synthetic-user', 'synthetic-password', 'synthetic-query', 'synthetic-fragment', 'synthetic-auth-token']:
            assert sentinel not in encoded, fixture['id'] + ': sentinel escaped'
        results.append({'id': fixture['id'], 'passed': True, 'status': report['status'], 'origins': origins})
    receipt = {'passed': True, 'upstream_url': URL, 'upstream_revision': REVISION,
               'upstream_source_sha256': hashlib.sha256(source).hexdigest(),
               'node_version': subprocess.check_output(['node', '--version'], text=True).strip(),
               'go_version': subprocess.check_output(['go', 'version'], text=True).strip(),
               'cases': results, 'scope': 'Synthetic fixture comparison of a documented subset, not general npm compatibility. No raw oracle values retained.'}
    args.output.parent.mkdir(parents=True, exist_ok=True)
    args.output.write_text(json.dumps(receipt, indent=2) + '\n')
    print(f'{len(results)} pinned npm/ini subset cases passed')


if __name__ == '__main__':
    main()
