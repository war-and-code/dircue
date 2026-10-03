#!/usr/bin/env python3
"""Build reproducible local release archives from committed source. Never publishes."""
import argparse
import download_go_modules
import gzip
import hashlib
import io
import json
import os
from pathlib import Path, PurePosixPath
import re
import shutil
import subprocess
import sys
import tarfile
import tempfile
import zipfile
import zlib

TARGETS = ('linux/amd64', 'linux/arm64', 'darwin/amd64', 'darwin/arm64', 'windows/amd64')
PAYLOAD = ('LICENSE', 'THIRD_PARTY_NOTICES.md', 'README.md')
CONTROLS = {'GOENV': 'off', 'GOWORK': 'off', 'GOFLAGS': '', 'GOEXPERIMENT': '',
            'CGO_ENABLED': '0', 'GOAMD64': 'v1', 'GOARM64': 'v8.0',
            'GOPROXY': 'https://proxy.golang.org,direct', 'GOSUMDB': 'sum.golang.org',
            'GOPRIVATE': '', 'GONOPROXY': '', 'GONOSUMDB': '', 'GOINSECURE': '',
            'GOAUTH': 'off', 'GOGC': '100', 'GOMEMLIMIT': 'off', 'GODEBUG': ''}


def git(root, *args):
    return subprocess.check_output(['git', *args], cwd=root)


def clean_revision(root, expected=None):
    if git(root, 'status', '--porcelain=v1', '-z', '--untracked-files=all'):
        raise ValueError('release archives require a clean committed checkout')
    revision = git(root, 'rev-parse', '--verify', 'HEAD').decode().strip()
    if expected is not None and revision != expected:
        raise ValueError('HEAD changed during release build')
    return revision


def require_fresh(output):
    if os.path.lexists(output):
        raise ValueError('release output must not already exist (including an empty directory)')


def snapshot(root, revision, destination):
    """Copy regular committed blobs, bypassing checkout filters and export attributes."""
    rows = git(root, 'ls-tree', '-r', '-z', revision).split(b'\0')
    files = []
    for row in filter(None, rows):
        metadata, raw_path = row.split(b'\t', 1)
        mode, kind, oid = metadata.split()
        path = PurePosixPath(os.fsdecode(raw_path))
        if mode not in (b'100644', b'100755') or kind != b'blob':
            raise ValueError('release source must contain only regular tracked files: '+str(path))
        if path.is_absolute() or any(part in ('..', '.git') for part in path.parts):
            raise ValueError('invalid committed source path')
        files.append((path, mode, oid))
    # A batch process preserves exact committed bytes without invoking filters.
    with subprocess.Popen(['git', 'cat-file', '--batch'], cwd=root,
                          stdin=subprocess.PIPE, stdout=subprocess.PIPE) as process:
        try:
            for relative, mode, oid in files:
                process.stdin.write(oid+b'\n')
                process.stdin.flush()
                actual, kind, raw_size = process.stdout.readline().split()
                size = int(raw_size)
                if actual != oid or kind != b'blob' or size < 0:
                    raise ValueError('unexpected Git object response')
                content = process.stdout.read(size)
                if len(content) != size or process.stdout.read(1) != b'\n':
                    raise ValueError('truncated Git object')
                path = destination.joinpath(*relative.parts)
                path.parent.mkdir(parents=True, exist_ok=True)
                path.write_bytes(content)
                path.chmod(0o755 if mode == b'100755' else 0o644)
        finally:
            process.stdin.close()
        if process.wait() != 0:
            raise ValueError('Git snapshot failed')
    return len(files)


def build_environment(toolchain, temporary, inherited=None):
    # Ignore caller Go configuration, credentials and architecture experiments.
    inherited = os.environ if inherited is None else inherited
    environment = {key:value for key,value in inherited.items()
                   if not key.startswith(('GO', 'CGO_'))}
    environment.update(CONTROLS, GOTOOLCHAIN=toolchain,
                       GOCACHE=str(temporary/'go-build-cache'),
                       GOMODCACHE=str(temporary/'go-module-cache'))
    return environment


def local_replacement(source, value):
    """Validate against one canonical boundary, including aliased temp roots."""
    source = source.resolve()
    path = Path(value)
    target = (source/path).resolve()
    if path.is_absolute() or not target.is_relative_to(source) or not target.is_dir():
        raise ValueError('local module replacement escapes committed source')
    return target


def write_archive(path, payload, windows=False):
    """Canonical flat payload: binary0755, text0644, symlinks by name; no filesystem metadata leaks.

    For tar archives a symlink entry uses (None, link_target) as its value.
    Windows zip archives do not support symlinks; use byte-identical copies instead.
    """
    entries = sorted(payload.items())
    if windows:
        with zipfile.ZipFile(path, 'w', compression=zipfile.ZIP_DEFLATED, compresslevel=9) as output:
            for name, (content, executable) in entries:
                info = zipfile.ZipInfo(name, date_time=(1980, 1, 1, 0, 0, 0))
                info.create_system = 3
                info.external_attr = (0o100755 if executable else 0o100644) << 16
                info.compress_type = zipfile.ZIP_DEFLATED
                output.writestr(info, content, compresslevel=9)
    else:
        with path.open('wb') as raw:
            with gzip.GzipFile(fileobj=raw, mode='wb', filename='', mtime=0, compresslevel=9) as compressed:
                with tarfile.open(fileobj=compressed, mode='w', format=tarfile.USTAR_FORMAT) as output:
                    for name, (content, executable) in entries:
                        info = tarfile.TarInfo(name)
                        if content is None:
                            # Relative symlink: executable holds the link target name.
                            info.type = tarfile.SYMTYPE
                            info.linkname = executable
                            info.mode, info.mtime = 0o777, 0
                            info.uid = info.gid = 0
                            info.uname = info.gname = ''
                            output.addfile(info)
                        else:
                            info.size, info.mode, info.mtime = len(content), 0o755 if executable else 0o644, 0
                            info.uid = info.gid = 0
                            info.uname = info.gname = ''
                            output.addfile(info, io.BytesIO(content))


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--version', required=True)
    parser.add_argument('--output', type=Path, default=Path('dist'))
    parser.add_argument('--target', action='append', choices=TARGETS, help='repeat to select a subset; default all five')
    parser.add_argument('--go', default='go', help='Go launcher; go.mod selects the exact compiler version')
    args = parser.parse_args()
    if not re.fullmatch(r'[0-9]+\.[0-9]+\.[0-9]+(?:-[A-Za-z0-9.-]+)?', args.version):
        parser.error('version must be a semantic version without a leading v')
    root = Path(__file__).resolve().parents[1]
    output = args.output.absolute()
    try:
        require_fresh(output)
        revision = clean_revision(root)
        archives = []
        targets = [target for target in TARGETS if target in (args.target or TARGETS)]
        with tempfile.TemporaryDirectory(prefix='dircue-release-') as directory:
            temporary = Path(directory).resolve()
            source = temporary/'source'
            source.mkdir()
            file_count = snapshot(root, revision, source)
            match = re.search(r'^go ([0-9]+\.[0-9]+\.[0-9]+)$', (source/'go.mod').read_text(), re.MULTILINE)
            if not match:
                raise ValueError('go.mod must pin an exact Go patch version')
            toolchain = 'go'+match[1]
            environment = build_environment(toolchain, temporary)
            # Resolve the checksum-verified compiler with the normal toolchain cache,
            # then use that exact compiler with fresh dependency/build caches.
            selection = dict(environment)
            selection.pop('GOMODCACHE')
            compiler_root = subprocess.check_output([args.go, 'env', 'GOROOT'], cwd=source, env=selection, text=True).strip()
            compiler = Path(compiler_root)/'bin'/('go.exe' if os.name == 'nt' else 'go')
            environment['GOTOOLCHAIN'] = 'local'
            version = subprocess.check_output([str(compiler), 'version'], cwd=source, env=environment, text=True).strip()
            if not version.startswith('go version '+toolchain+' '):
                raise ValueError('resolved compiler does not match go.mod')
            module = json.loads(subprocess.check_output([str(compiler), 'mod', 'edit', '-json'], cwd=source, env=environment))
            replacements = []
            for replacement in module.get('Replace') or []:
                new = replacement['New']
                if not new.get('Version'):
                    local_replacement(source, new['Path'])
                    replacements.append({'module':replacement['Old']['Path'], 'path':new['Path']})
            # The build intentionally uses a fresh module cache. Warm that exact
            # cache through the same bounded, credential-redacting retry path as
            # CI's preflight so transient proxy failures do not bypass retries.
            if download_go_modules.download(command=str(compiler), cwd=source, env=environment) != 0:
                raise ValueError('Go module download failed before release build')
            packaged = temporary/'archives'
            packaged.mkdir()
            flags = ['-mod=readonly', '-buildvcs=false', '-trimpath', '-ldflags', f'-s -w -X github.com/war-and-code/dircue/internal/cli.Version={args.version}']
            for target in targets:
                target_os, architecture = target.split('/')
                name = f'dircue_{args.version}_{target_os}_{architecture}'
                binary_name = 'dircue.exe' if target_os == 'windows' else 'dircue'
                binary = temporary/binary_name
                env = dict(environment, GOOS=target_os, GOARCH=architecture)
                subprocess.run([str(compiler), 'build', *flags, '-o', str(binary), '.'], cwd=source, env=env, check=True)
                binary_bytes = binary.read_bytes()
                payload = {filename:((source/filename).read_bytes(), False) for filename in PAYLOAD}
                payload[binary_name] = (binary_bytes, True)
                # Windows zip: byte-identical copy (zip symlinks are unreliable on Windows).
                # Unix tar: relative symlink dirq -> dircue at the archive root.
                if target_os == 'windows':
                    payload['dirq.exe'] = (binary_bytes, True)
                else:
                    payload['dirq'] = (None, 'dircue')
                archive = packaged/(name+('.zip' if target_os == 'windows' else '.tar.gz'))
                write_archive(archive, payload, target_os == 'windows')
                archives.append({'name':archive.name, 'sha256':hashlib.sha256(archive.read_bytes()).hexdigest(),
                                 'binary_sha256':hashlib.sha256(binary_bytes).hexdigest(), 'os':target_os, 'arch':architecture})
            clean_revision(root, revision)
            require_fresh(output)
            provenance = {'version':args.version, 'toolchain':version, 'git_revision':revision,
                'git_tree':git(root, 'rev-parse', revision+'^{tree}').decode().strip(),
                'clean_checkout':True, 'head_and_clean_state_verified_after_build':True,
                'source':'regular committed blobs; ignores working-tree filters and export attributes',
                'source_files':file_count, 'local_replacements':replacements, 'cgo_enabled':False, 'build_vcs':False, 'build_flags':flags,
                'controls':dict(CONTROLS, GOTOOLCHAIN='local', selected_toolchain=toolchain,
                                GOCACHE='fresh temporary cache', GOMODCACHE='fresh temporary cache'),
                'archive_metadata':{'tar_epoch':0, 'gzip_epoch':0, 'zip_date':'1980-01-01T00:00:00',
                                    'uid':0, 'gid':0, 'binary_mode':'0755', 'text_mode':'0644',
                                    'python':sys.version.split()[0], 'zlib':zlib.ZLIB_RUNTIME_VERSION},
                'archives':archives}
            (packaged/'SHA256SUMS').write_text(''.join(f"{entry['sha256']}  {entry['name']}\n" for entry in archives))
            (packaged/'provenance.json').write_text(json.dumps(provenance, indent=2)+'\n')
            # Exclusive creation fails if another process claimed this output path.
            output.mkdir(parents=True, exist_ok=False)
            for path in sorted(packaged.iterdir()):
                shutil.copyfile(path, output/path.name)
            for entry in archives:
                print(output/entry['name'], flush=True)
    except ValueError as error:
        parser.error(str(error))


if __name__ == '__main__':
    main()
