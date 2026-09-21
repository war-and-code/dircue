#!/usr/bin/env python3
"""Preserve released 0.7 targeted CLI bytes on the authored focus fixtures."""
import argparse
import importlib.util
import json
from pathlib import Path
import tempfile
import common

SPEC = common.ROOT / 'tests/focus_v070/fixture.py'
spec = importlib.util.spec_from_file_location('focus_fixture', SPEC)
fixture = importlib.util.module_from_spec(spec)
spec.loader.exec_module(fixture)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    for key in ('baseline', 'candidate', 'build-receipt', 'output'):
        parser.add_argument('--' + key, required=True, type=Path)
    args = parser.parse_args()
    baseline, candidate = args.baseline.resolve(), args.candidate.resolve()
    if common.sha256(baseline) != common.BASELINE_SHA256:
        raise AssertionError('unverified released 0.7 binary')
    build, digest = common.load_build_receipt(args.build_receipt, candidate)
    rows = []
    with tempfile.TemporaryDirectory(prefix='dircue-v080-targeted-') as temporary:
        base = Path(temporary)
        cases = fixture.prepare(base)
        manifest = fixture.manifest(base)
        for name, case in cases.items():
            root = case['root']
            common_args = ['--source', 'directory', '--json', str(root)]
            commands = [
                ['analyze', 'focus', '--project', case['project'], *common_args],
                ['analyze', 'focus', '--project', case['project'], '--metrics', '--files', *common_args],
                ['analyze', 'focus', '--project', case['project'], '--related-project', case['related'], '--metrics', '--files', *common_args],
                ['analyze', 'focus', '--affected-by', case['affected_by'], *common_args],
                ['analyze', 'availability', *common_args],
                ['analyze', 'all', '--declarations', '--availability', *common_args],
                ['analyze', 'explain', '--file', case['primary'][1], *common_args],
                ['analyze', 'focus', '--project', 'absent.csproj', *common_args],
            ]
            saved = base / (name + '-saved.json')
            value = common.capture(baseline, commands[1], root)
            if value[0] != 0:
                raise AssertionError('baseline fixture failed')
            saved.write_bytes(value[1])
            commands.append(['analyze', 'explain', '--report', str(saved), '--project', case['project'], '--json'])
            for index, command in enumerate(commands):
                old = common.capture(baseline, command, root)
                new = common.capture(candidate, command, root)
                if index == 7 and (old[0] == 0 or new[0] == 0):
                    raise AssertionError('absent focus project did not fail closed')
                rows.append({'id': f'{name}-{index+1}', 'args': command,
                             'equal': old == new, 'baseline': common.recorded(old),
                             'candidate': common.recorded(new)})
            saved.unlink()
        if fixture.manifest(base) != manifest:
            raise AssertionError('fixtures changed')
    result = {'schema': 'dircue-context-v080-targeted-1', 'baseline_release': 'v0.7.0',
              'baseline_sha256': common.sha256(baseline), 'candidate_sha256': common.sha256(candidate),
              'build_receipt': build, 'build_receipt_sha256': digest,
              'fixture_source_sha256': common.sha256(SPEC), 'harness_sha256': common.sha256(Path(__file__)),
              'fixture_manifest': manifest, 'cases': rows, 'total': len(rows),
              'exact_matches': sum(r['equal'] for r in rows)}
    result['passed'] = result['exact_matches'] == result['total'] == 18
    common.write_json(args.output, result)
    print(json.dumps({k: result[k] for k in ('passed', 'total', 'exact_matches')}))
    if not result['passed']:
        raise SystemExit(1)

if __name__ == '__main__':
    main()
