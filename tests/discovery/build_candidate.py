#!/usr/bin/env python3
"""Build a comparison binary and record its exact local Go and embedded inputs."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import subprocess

ROOT = Path(__file__).resolve().parents[2]


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--output', type=Path, required=True)
    parser.add_argument('--inputs', type=Path, required=True)
    args = parser.parse_args()
    env = dict(os.environ, CGO_ENABLED='0')
    source = subprocess.check_output(['go', 'list', '-deps', '-json', '.'], cwd=ROOT, env=env, text=True)
    decoder = json.JSONDecoder()
    paths = {ROOT / 'go.mod', ROOT / 'go.sum'}
    while source.strip():
        package, end = decoder.raw_decode(source.lstrip())
        source = source.lstrip()[end:]
        directory = Path(package.get('Dir', '/'))
        if directory.is_relative_to(ROOT):
            for key in ('GoFiles', 'CgoFiles', 'CFiles', 'CXXFiles', 'HFiles', 'SFiles', 'SysoFiles', 'EmbedFiles'):
                paths.update(directory / name for name in package.get(key, []))
            if package.get('Module', {}).get('GoMod'):
                paths.add(Path(package['Module']['GoMod']))
    paths = sorted(p for p in paths if p.is_relative_to(ROOT))
    def current():
        return {str(p.relative_to(ROOT)): hashlib.sha256(p.read_bytes()).hexdigest() for p in paths}
    before = current()
    args.output.parent.mkdir(parents=True, exist_ok=True)
    args.inputs.parent.mkdir(parents=True, exist_ok=True)
    command = ['go', 'build', '-mod=readonly', '-buildvcs=false', '-trimpath', '-ldflags',
               '-s -w -X dircue/internal/cli.Version=0.3.0', '-o', str(args.output.resolve()), '.']
    subprocess.run(command, cwd=ROOT, env=env, check=True)
    assert before == current(), 'build inputs changed during compilation'
    record = {'files': before, 'command': command, 'CGO_ENABLED': '0',
              'source_commit': subprocess.check_output(['git', 'rev-parse', 'HEAD'], cwd=ROOT, text=True).strip(),
              'dirty': bool(subprocess.check_output(['git', 'status', '--porcelain'], cwd=ROOT, text=True))}
    args.inputs.write_text(json.dumps(record, indent=2) + '\n')
    print(args.output)


if __name__ == '__main__':
    main()
