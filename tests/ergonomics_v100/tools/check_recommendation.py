#!/usr/bin/env python3
"""Run one applied recommendation's public CLI regression.

Usage: check_recommendation.py R-NNN BINARY
Exit 0 = contract met; 1 = assertion failed. Fixtures remain inside ignored
target .cache and are temporary. Commands are argv arrays, never shell input.
"""
import argparse
import json
from pathlib import Path
import re
import subprocess
import sys
import tempfile
import unicodedata

ROOT = Path(__file__).resolve().parents[3]


def check(identifier, binary):
    parent = ROOT / '.cache/v100'
    parent.mkdir(parents=True, exist_ok=True)
    with tempfile.TemporaryDirectory(prefix='ergo-regression-', dir=parent) as temp:
        cwd = Path(temp)
        (cwd/'main.go').write_text('package main\n')
        def run(*args, code=0):
            p = subprocess.run([binary, *args], cwd=cwd, capture_output=True, text=True, input='', timeout=10)
            assert p.returncode == code, (args, p.returncode, p.stderr)
            if code:
                assert not p.stdout, (args, 'unexpected data on failure')
            else:
                assert not p.stderr, (args, p.stderr)
            return p.stdout if not code else p.stderr
        if identifier == 'R-001':
            out = run('--help')
            assert 'handled errors exit 1' in out and 'capabilities --guide' in out
            assert 'Examples:' in run('analyze','explain','--help')
        elif identifier == 'R-002':
            assert 'did you mean --json?' in run('--jsno',code=1)
            out = run('--jsno=private-value',code=1)
            assert 'private-value' not in out
            assert 'did you mean' not in run('--unrelated-flag',code=1)
        elif identifier == 'R-003':
            assert 'dircue analyze declarations --json' in run('analyze','declaration',code=1)
            assert 'if you intended' in run('languages',code=1)
            (cwd/'languages').mkdir()
            (cwd/'languages/main.go').write_text('package main\n')
            assert 'Go' in json.loads(run('languages','--json'))
            assert 'Go' in json.loads(run('--json','--','languages'))
        elif identifier == 'R-004':
            assert 'dircue compare base.json head.json --json' in run('compare',code=1)
            out = run('plan','--help')
            assert '--workers' not in out and 'source-structure' in out and 'Examples:' in out
        elif identifier in ('R-005','R-009'):
            profile = json.loads(run('analyze','all','--json'))
            profile['root'] = '/untrusted/do-not-open'
            (cwd/'profile.json').write_text(json.dumps(profile))
            if identifier == 'R-005':
                assert '--module declarations' in run('plan','profile.json','--module','declaration',code=1)
                assert '--structural-worker' in run('analyze','structure',code=1)
            else:
                out = run('plan','profile.json','--module','metrics')
                assert 'Inert argv (not executable): [' in out and 'Revalidation required:' in out
                assert '/untrusted/do-not-open' not in out
                structured = json.loads(run('plan','profile.json','--module','metrics','--json'))
                assert all(not s['command']['executable'] for s in structured['steps'])
        elif identifier == 'R-006':
            out = json.loads(run('capabilities','--cli','--json'))
            assert out['kind'] == 'dircue-cli-capabilities'
            # The map comparison outcome contract added the two opt-in codes
            # for uncertainty and handled errors in 1.1.0.
            assert {x['code'] for x in out['exit_codes']} == {0,1,2,3,141}
            compare = next(x for x in out['commands'] if x['path'] == ['dircue','map','compare'])
            restrictions = ' '.join(compare['restrictions'])
            assert 'handled errors exit 1' in restrictions and '3 handled errors' in restrictions
            exit_three = next(x['meaning'] for x in out['exit_codes'] if x['code'] == 3)
            assert 'map compare --exit-code' in exit_three and 'diagnostic on stderr' in exit_three
            missing = str(cwd/'missing-map.json')
            run('map','compare','--json',missing,missing,code=1)
            run('map','compare','--exit-code','--json',missing,missing,code=3)
            # Contract: the set of registered command paths must equal this explicit list.
            # The list includes the map subcommands added in 1.0 (map compare/locate/route/settings).
            # Update this set when a new top-level or subcommand is deliberately added.
            EXPECTED_PATHS = {
                ('dircue',),
                ('dircue', 'analyze'),
                ('dircue', 'analyze', 'all'),
                ('dircue', 'analyze', 'availability'),
                ('dircue', 'analyze', 'declarations'),
                ('dircue', 'analyze', 'discovery'),
                ('dircue', 'analyze', 'ecosystems'),
                ('dircue', 'analyze', 'environments'),
                ('dircue', 'analyze', 'explain'),
                ('dircue', 'analyze', 'focus'),
                ('dircue', 'analyze', 'formats'),
                ('dircue', 'analyze', 'frameworks'),
                ('dircue', 'analyze', 'graph'),
                ('dircue', 'analyze', 'languages'),
                ('dircue', 'analyze', 'metrics'),
                ('dircue', 'analyze', 'packages'),
                ('dircue', 'analyze', 'projects'),
                ('dircue', 'analyze', 'registries'),
                ('dircue', 'analyze', 'rules'),
                ('dircue', 'analyze', 'structure'),
                ('dircue', 'capabilities'),
                ('dircue', 'compare'),
                ('dircue', 'help'),
                ('dircue', 'map'),
                ('dircue', 'map', 'compare'),
                ('dircue', 'map', 'locate'),
                ('dircue', 'map', 'route'),
                ('dircue', 'map', 'settings'),
                ('dircue', 'plan'),
            }
            actual_paths = {tuple(x['path']) for x in out['commands']}
            assert actual_paths == EXPECTED_PATHS, (
                'command set mismatch\n'
                '  unexpected: {}\n'
                '  missing:    {}'.format(
                    sorted(actual_paths - EXPECTED_PATHS),
                    sorted(EXPECTED_PATHS - actual_paths),
                )
            )
            plan = next(x for x in out['commands'] if x['path'] == ['dircue','plan'])
            assert '--source' in plan['rejected_inherited_flags']
            assert 'source' not in [x['name'] for x in plan['flags']]
            assert json.loads(run('capabilities','--json'))['kind'] == 'dircue-planner-capabilities'
        elif identifier == 'R-007':
            out = json.loads(run('capabilities','--schema','profile','--json'))
            assert out['$id'].endswith('/profile.schema.json')
            assert '_dircue_bundled_resources' in out['$defs']
            assert '--schema' in run('capabilities','--help')
            run('capabilities','--schema','../../private',code=1)
        elif identifier == 'R-008':
            guide = json.loads(run('capabilities','--guide','--json'))
            assert guide['kind'] == 'dircue-automation-guide'
            sections = guide['sections']
            assert sum(x['title'].startswith('dircue analyze ') for x in sections) == 18
            out = run('capabilities','--guide')
            assert all(text in out for text in ['--on-error', 'GOMEMLIMIT', 'not a sandbox', 'inert argv', '--functions', '--hotspots'])
        elif identifier == 'R-010':
            (cwd/'empty.cs').write_text('')
            (cwd/'.gitattributes').write_text('empty.cs linguist-language=C# linguist-detectable=true\n')
            schema = json.loads(run('capabilities','--schema','languages','--json'))
            percentage = schema['additionalProperties']['properties']['percentage']
            assert re.fullmatch(percentage['pattern'], 'NaN')
            assert not re.fullmatch(percentage['pattern'], 'Infinity')
            (cwd/'main.go').unlink()
            legacy = json.loads(run('--json'))
            assert legacy['C#']['percentage'] == 'NaN'
        elif identifier == 'R-011':
            unsafe = 'missing-\x1b[31m-\u0085-\u202e'
            for args in [
                (unsafe,),
                ('analyze', 'discovery', unsafe),
                ('--source', 'git', unsafe),
            ]:
                message = run(*args, code=1)
                body = message[:-1] if message.endswith('\n') else message
                assert 'missing-' in body, (args, body)
                assert all(unicodedata.category(ch) not in ('Cc', 'Cf') for ch in body), (args, body)
                assert all(escape in body for escape in ('\\x1b', '\\u0085', '\\u202e')), (args, body)
        else:
            raise AssertionError('Unknown recommendation: ' + identifier)


if __name__ == '__main__':
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument('recommendation')
    p.add_argument('binary')
    args = p.parse_args()
    try:
        check(args.recommendation, str(Path(args.binary).resolve()))
    except (AssertionError, OSError, subprocess.TimeoutExpired) as error:
        print(args.recommendation + ': contract not met: ' + (str(error) or 'expected behavior absent'), file=sys.stderr)
        raise SystemExit(1)
    print(args.recommendation + ': PASS')
