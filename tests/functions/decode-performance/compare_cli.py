#!/usr/bin/env python3
"""Compare pre/post optimization CLI bytes using the same pinned worker."""
import argparse
import hashlib
import json
from pathlib import Path
import subprocess
import tempfile

HERE = Path(__file__).resolve().parent


def digest(data):
    return hashlib.sha256(data).hexdigest()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    for name in ['baseline', 'candidate', 'worker', 'output']:
        parser.add_argument('--' + name, type=Path, required=True)
    args = parser.parse_args()
    if args.output.exists():
        raise SystemExit('choose a new receipt path')
    baseline, candidate, worker = (getattr(args, name).resolve() for name in ['baseline', 'candidate', 'worker'])
    receipt = {'baseline_sha256': digest(baseline.read_bytes()), 'candidate_sha256': digest(candidate.read_bytes()),
               'worker_sha256': digest(worker.read_bytes()), 'harness_sha256': digest(Path(__file__).read_bytes()), 'cases': []}
    with tempfile.TemporaryDirectory(prefix='dircue-function-differential-') as temporary:
        root = Path(temporary)
        cases = []
        for count in [1, 10, 50, 100, 128, 500, 1000]:
            directory = root / str(count)
            directory.mkdir()
            (directory / 'many.py').write_bytes((HERE / 'inputs' / f'{count}.py').read_bytes())
            cases.append(directory)
        mixed = root / 'mixed'
        mixed.mkdir()
        for path in (HERE.parent / 'fixtures').iterdir():
            (mixed / path.name).write_bytes(path.read_bytes())
        (mixed / 'broken.py').write_text('def broken(:\n    return 0\n')
        (mixed / 'generated.py').write_text('def generated():\n    return 1\n')
        (mixed / '.gitattributes').write_text('generated.py linguist-generated=true\n')
        cases.append(mixed)
        global_cap = root / 'global-cap'
        global_cap.mkdir()
        for index in range(10):
            (global_cap / f'file{index:02d}.py').write_bytes((HERE / 'inputs' / '500.py').read_bytes())
        cases.append(global_cap)
        for directory in cases:
            for variant, flags in [('default', []), ('functions', ['--functions']),
                                   ('functions-files', ['--functions', '--files'])]:
                common = ['analyze', 'structure', '--source', 'directory', '--json', '--workers', '2',
                          '--structural-worker', str(worker), *flags, str(directory)]
                old = subprocess.run([str(baseline), *common], capture_output=True, timeout=120)
                new = subprocess.run([str(candidate), *common], capture_output=True, timeout=120)
                assert (old.returncode, old.stdout, old.stderr) == (new.returncode, new.stdout, new.stderr), (directory.name, variant)
                assert old.returncode == 0 and not old.stderr
                if variant != 'default':
                    functions = json.loads(new.stdout)['structure']['functions']
                    assert functions['total_spaces'] == len(functions['entries']) + functions['omitted_spaces'] + functions['invalid_span_spaces']
                    if directory == global_cap:
                        assert len(functions['entries']) == 1024 and functions['total_spaces'] == 5000 and functions['status'] == 'partial'
                receipt['cases'].append({'name': directory.name, 'variant': variant, 'arguments': common,
                                         'stdout_sha256': digest(new.stdout), 'stdout_bytes': len(new.stdout),
                                         'stderr_sha256': digest(new.stderr), 'exit_code': new.returncode})
    assert len(receipt['cases']) == 27
    receipt['passed'] = True
    args.output.write_text(json.dumps(receipt, indent=2) + '\n')
    print('27 CLI cases match exactly, including partial and global-cap evidence.')


if __name__ == '__main__':
    main()
