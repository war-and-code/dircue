#!/usr/bin/env python3
"""Build isolated official CLI and identical library drivers; never run implicitly."""
import argparse
import hashlib
import json
import os
from pathlib import Path, PurePosixPath
import shutil
import stat
import subprocess
import tempfile
import urllib.request

HERE = Path(__file__).resolve().parent
ROOT = HERE.parent.parent


def digest(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def verified_fork_inventory(fork):
    """Hash every source/embedded asset, with no extension filter or symlinks."""
    if fork.is_symlink() or not fork.is_dir():
        raise RuntimeError('maintained fork must be a real directory')
    files = {}
    for path in sorted(fork.rglob('*')):
        mode = path.lstat().st_mode
        if stat.S_ISDIR(mode):
            continue
        if not stat.S_ISREG(mode):
            raise RuntimeError(f'nonregular maintained source: {path}')
        files[path.relative_to(fork).as_posix()] = digest(path)
    provenance = json.loads((fork/'PROVENANCE.json').read_text())
    managed = provenance.get('files')
    if not isinstance(managed, dict) or not managed:
        raise RuntimeError('missing managed source inventory')
    for name, expected in managed.items():
        if not isinstance(expected, str) or len(expected) != 64 or any(c not in '0123456789abcdef' for c in expected):
            raise RuntimeError(f'invalid managed source digest: {name!r}')
        path = PurePosixPath(name)
        if not name or path.is_absolute() or path.as_posix() != name or '..' in path.parts:
            raise RuntimeError(f'unsafe managed source path: {name!r}')
        if files.get(name) != expected:
            raise RuntimeError(f'managed source differs from provenance: {name}')
    extras = set(files)-set(managed)-{'PROVENANCE.json', 'GENERATOR_WARNINGS.txt', 'README.md'}
    if extras:
        raise RuntimeError(f'unmanaged maintained source files: {sorted(extras)}')
    for name, key in [('update_enry.py', 'generator_script_sha256'),
                      ('patches/enry-linguist-9.7.patch', 'patch_sha256')]:
        if digest(fork.parent/name) != provenance.get(key):
            raise RuntimeError(f'maintained generation input differs from provenance: {name}')
    return files


def unchanged_fork(fork, before):
    after = verified_fork_inventory(fork)
    if after != before:
        raise RuntimeError('maintained source changed during build')
    return after


def main():
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument('--output', type=Path, required=True)
    p.add_argument('--goos', default='linux')
    p.add_argument('--goarch', default='arm64')
    p.add_argument('--profile', action='store_true', help='Keep symbols; same optimizations')
    a = p.parse_args()
    a.output = a.output.resolve()
    if a.output.exists():
        p.error('output must not exist; refuse to overwrite a measured build')
    fork_root = ROOT/'third_party/go-enry'
    fork_before = verified_fork_inventory(fork_root)
    pins = json.loads((HERE/'pins.json').read_text())
    env = dict(os.environ, GOWORK='off', GOFLAGS='', GOENV='off', CGO_ENABLED='0', GOOS=a.goos, GOARCH=a.goarch,
               GOEXPERIMENT='', GOAMD64='v1', GOARM64='v8.0', GOTOOLCHAIN=pins['go_version'], GOGC='100', GOMEMLIMIT='off',
               GOPROXY='https://proxy.golang.org,direct', GOSUMDB='sum.golang.org', GOAUTH='off',
               GOPRIVATE='', GONOPROXY='', GONOSUMDB='', GOINSECURE='')
    # Do not inherit implementation/runtime settings or mutable extracted module
    # contents. These controls are recorded and applied to all four builds.
    for key in ('GOMAXPROCS', 'GOROOT', 'GODEBUG', 'GO386', 'GOARM', 'GOMIPS', 'GOMIPS64', 'GOPPC64', 'GORISCV64', 'GOWASM'):
        env.pop(key, None)
    env.pop('GOMOD', None)
    def run(args, cwd):
        return subprocess.check_output(args, cwd=cwd, env=env, text=True)
    version = run(['go', 'version'], HERE)
    if pins['go_version'] not in version.split():
        p.error('toolchain differs from pins.json')
    a.output.mkdir(parents=True)
    receipt = {'pins': pins, 'go_version': version.strip(), 'goos': a.goos, 'goarch': a.goarch,
               'profile': a.profile, 'builder_sha256': digest(Path(__file__)),
               'toolchain_selection': {'launcher': shutil.which('go'), 'GOTOOLCHAIN': env['GOTOOLCHAIN'],
                                       'observed_version': version.strip()}, 'builds': {},
               'maintained_source_before': fork_before,
               'maintained_source_policy': 'Every regular file, including embedded binary assets; managed provenance checked; no symlinks or unmanaged files.'}
    try:
        with tempfile.TemporaryDirectory(prefix='dircue-enry-build-') as directory:
            work = Path(directory)
            env['GOMODCACHE'] = str(work/'module-cache')
            env['GOCACHE'] = str(work/'build-cache')
            receipt['go_environment'] = json.loads(run(['go', 'env', '-json'], work))
            receipt['explicit_runtime_environment'] = {key: env.get(key) for key in ('GOGC', 'GOMEMLIMIT', 'GOMAXPROCS', 'GODEBUG')}
            source = work/'source'
            source.mkdir()
            for name, spec in pins['cli']['files'].items():
                target = source/name
                target.write_bytes(urllib.request.urlopen(spec['url'], timeout=60).read())
                if digest(target) != spec['sha256']:
                    raise RuntimeError(f'official source hash mismatch: {name}')
            cli_source = (source/'main.go').read_text()
            if 'version = "not-set"' not in cli_source or 'commit  = "not-set"' not in cli_source or 'package main' not in cli_source:
                raise RuntimeError('pinned CLI linker variables no longer match source')
            shutil.copy2(source/'LICENSE', a.output/'UPSTREAM_CLI_LICENSE')
            for name, library, fork, cli in [
                ('enry-official', 'v2.8.4', False, True),
                ('enry-refreshed', 'v2.9.6', False, True),
                ('library-official', 'v2.9.6', False, False),
                ('library-maintained', 'v2.9.6', True, False),
            ]:
                module = work/name
                if cli:
                    shutil.copytree(source, module)
                    if library != 'v2.8.4':
                        run(['go', 'mod', 'edit', '-require=github.com/go-enry/go-enry/v2@'+library], module)
                else:
                    module.mkdir()
                    (module/'go.mod').write_text('module enry-performance-driver\n\ngo 1.26.6\n\nrequire github.com/go-enry/go-enry/v2 '+library+'\n')
                    shutil.copyfile(HERE/'library.go.txt', module/'main.go')
                    if fork:
                        run(['go', 'mod', 'edit', '-replace=github.com/go-enry/go-enry/v2='+str(ROOT/'third_party/go-enry')], module)
                run(['go', 'mod', 'download'], module)
                flags = '' if a.profile else '-s -w'
                if cli:
                    flags += ' -X main.version=v1.3.0 -X main.commit='+pins['cli']['commit']
                command = ['go', 'build', '-mod=mod', '-buildvcs=false', '-trimpath', '-ldflags', flags.strip(), '-o', str(a.output/name), '.']
                run(command, module)
                binary_modules = run(['go', 'version', '-m', str(a.output/name)], module)
                if pins['go_version'] not in binary_modules.splitlines()[0].split():
                    raise RuntimeError('compiled binary toolchain differs from exact selection')
                metadata = json.loads(run(['go', 'list', '-m', '-json', pins['library']['module']], module))
                if metadata['Version'] != library or bool(metadata.get('Replace')) != fork:
                    raise RuntimeError('baseline dependency substitution detected')
                if not fork and library == 'v2.9.6' and metadata.get('Sum') != pins['library']['sum']:
                    raise RuntimeError('official module hash mismatch')
                if cli and digest(module/'main.go') != pins['cli']['files']['main.go']['sha256']:
                    raise RuntimeError('CLI source changed')
                receipt['builds'][name] = {'sha256': digest(a.output/name), 'command': command,
                    'module': metadata, 'modules': run(['go', 'list', '-m', '-json', 'all'], module),
                    'binary_modules': binary_modules,
                    'go_mod': (module/'go.mod').read_text(), 'go_sum': (module/'go.sum').read_text() if (module/'go.sum').exists() else '',
                    'driver_sha256': digest(HERE/'library.go.txt') if not cli else None,
                    'fork_provenance_sha256': digest(ROOT/'third_party/go-enry/PROVENANCE.json') if fork else None}
        receipt['maintained_source_after'] = unchanged_fork(fork_root, fork_before)
        receipt['maintained_source_stable_during_build'] = True
        receipt['complete'] = True
    finally:
        (a.output/'build-receipt.json').write_text(json.dumps(receipt, indent=2)+'\n')


if __name__ == '__main__':
    main()
