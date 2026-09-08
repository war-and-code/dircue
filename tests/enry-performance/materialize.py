#!/usr/bin/env python3
"""Materialize all pinned public repositories as verified raw-Git-blob views.

This prepares inputs only: no checkout filters, archives, project code, language
analysis, timing runs, or profilers. Original repositories are read-only inputs.
"""

import argparse
import datetime
import hashlib
import json
import os
from pathlib import Path, PurePosixPath
import platform
import shutil
import stat
import subprocess
import tempfile
import threading


HERE = Path(__file__).resolve().parent
CHUNK = 1024 * 1024
MAX_TREE_OUTPUT = 64 * 1024 * 1024
REGULAR_MODES = {'100644': 0o644, '100755': 0o755}


def digest(path):
    result = hashlib.sha256()
    with path.open('rb') as stream:
        while block := stream.read(CHUNK):
            result.update(block)
    return result.hexdigest()


def safe_relative(value):
    path = PurePosixPath(value)
    if not value or path.is_absolute() or path.as_posix() != value or any(p in ('', '.', '..', '.git') for p in path.parts):
        raise RuntimeError(f'unsafe Git path: {value!r}')
    return path


def checked_root(path):
    """Reject symlinks in existing output ancestors, including the root itself."""
    path = Path(os.path.abspath(path))
    current = Path(path.anchor)
    for component in path.parts[1:]:
        current /= component
        try:
            info = current.lstat()
        except FileNotFoundError:
            current.mkdir()
            info = current.lstat()
        if not stat.S_ISDIR(info.st_mode):
            raise RuntimeError(f'output ancestor is not a real directory: {current}')
    return path


def git_command(root, *arguments):
    return ['git', '--no-replace-objects', '-c', 'core.hooksPath=/dev/null',
            '-c', 'safe.directory=*', '-C', str(root), *arguments]


def git_environment():
    # Do not inherit alternate object directories, config injection, namespaces,
    # tracing destinations, or user-supplied replacement/configuration settings.
    environment = {key: value for key, value in os.environ.items() if not key.startswith('GIT_')}
    environment.update(GIT_CONFIG_NOSYSTEM='1', GIT_CONFIG_GLOBAL='/dev/null',
                       GIT_NO_REPLACE_OBJECTS='1', GIT_OPTIONAL_LOCKS='0',
                       GIT_TERMINAL_PROMPT='0', LC_ALL='C')
    return environment


def git_text(root, environment, timeout, *arguments):
    result = subprocess.run(git_command(root, *arguments), env=environment,
                            stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                            timeout=timeout, check=True)
    return result.stdout.decode('utf-8').strip()


def tree_entries(root, commit, environment, timeout):
    """Read NUL-delimited paths with bounded buffering and a process deadline."""
    with tempfile.TemporaryFile() as errors:
        process = subprocess.Popen(git_command(root, 'ls-tree', '-r', '-l', '-z', commit),
                                   env=environment, stdout=subprocess.PIPE, stderr=errors)
        deadline = threading.Timer(timeout, process.kill)
        deadline.daemon = True
        deadline.start()
        pending = b''
        total = 0
        try:
            while block := process.stdout.read(65536):
                total += len(block)
                if total > MAX_TREE_OUTPUT:
                    raise RuntimeError('Git tree listing exceeds 64 MiB limit')
                pending += block
                parts = pending.split(b'\0')
                pending = parts.pop()
                if len(pending) > CHUNK:
                    raise RuntimeError('Git tree record exceeds 1 MiB limit')
                for record in parts:
                    metadata, raw_path = record.split(b'\t', 1)
                    mode, kind, oid, size = metadata.split()
                    filename = raw_path.decode('utf-8')
                    safe_relative(filename)
                    yield {'path': filename, 'mode': mode.decode(), 'kind': kind.decode(),
                           'oid': oid.decode(), 'bytes': None if size == b'-' else int(size)}
            if pending:
                raise RuntimeError('unterminated Git tree record')
            if process.wait(timeout=30):
                errors.seek(0)
                raise RuntimeError('Git tree enumeration failed: '+errors.read(65536).decode(errors='replace'))
        finally:
            deadline.cancel()
            if process.poll() is None:
                process.kill()
            process.wait()
            process.stdout.close()


class BlobReader:
    def __init__(self, root, environment, timeout):
        self.errors = tempfile.TemporaryFile()
        self.process = subprocess.Popen(git_command(root, 'cat-file', '--batch'), env=environment,
                                        stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=self.errors)
        self.deadline = threading.Timer(timeout, self.process.kill)
        self.deadline.daemon = True
        self.deadline.start()

    def copy(self, entry, target):
        self.process.stdin.write(entry['git_blob_oid'].encode('ascii')+b'\n')
        self.process.stdin.flush()
        header = self.process.stdout.readline(4096)
        expected = f"{entry['git_blob_oid']} blob {entry['bytes']}\n".encode('ascii')
        if header != expected:
            raise RuntimeError(f"unexpected raw blob header for {entry['path']}: {header!r}")
        content_hash = hashlib.sha256()
        object_hash = hashlib.sha1(f"blob {entry['bytes']}\0".encode('ascii'))
        remaining = entry['bytes']
        while remaining:
            block = self.process.stdout.read(min(CHUNK, remaining))
            if not block:
                raise RuntimeError(f"truncated raw blob: {entry['path']}")
            target.write(block)
            content_hash.update(block)
            object_hash.update(block)
            remaining -= len(block)
        if self.process.stdout.read(1) != b'\n':
            raise RuntimeError('invalid raw blob trailer')
        if object_hash.hexdigest() != entry['git_blob_oid']:
            raise RuntimeError(f"raw Git object hash mismatch: {entry['path']}")
        return content_hash.hexdigest()

    def close(self, success):
        try:
            if success:
                self.process.stdin.close()
                if self.process.wait(timeout=30):
                    self.errors.seek(0)
                    raise RuntimeError('Git cat-file failed: '+self.errors.read(65536).decode(errors='replace'))
        finally:
            self.deadline.cancel()
            if self.process.poll() is None:
                self.process.kill()
            self.process.wait()
            if not self.process.stdin.closed:
                self.process.stdin.close()
            self.process.stdout.close()
            self.errors.close()


def parent_directory(root_fd, filename):
    """Return an anchored parent fd; never follow a repository path symlink."""
    relative = safe_relative(filename)
    current = os.dup(root_fd)
    try:
        for component in relative.parts[:-1]:
            try:
                os.mkdir(component, 0o755, dir_fd=current)
            except FileExistsError:
                pass
            child = os.open(component, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW, dir_fd=current)
            os.close(current)
            current = child
        return current, relative.name
    except BaseException:
        os.close(current)
        raise


def materialize_file(root_fd, entry, reader):
    parent, name = parent_directory(root_fd, entry['path'])
    try:
        descriptor = os.open(name, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW,
                             0o600, dir_fd=parent)
        with os.fdopen(descriptor, 'wb') as target:
            expected_sha = reader.copy(entry, target)
            target.flush()
            os.fchmod(target.fileno(), REGULAR_MODES[entry['mode']])
        # Verify the stored view independently of the source pipe hash. Full
        # readback is input preparation, not a measured scanner invocation.
        descriptor = os.open(name, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK, dir_fd=parent)
        with os.fdopen(descriptor, 'rb') as stored:
            info = os.fstat(stored.fileno())
            if not stat.S_ISREG(info.st_mode) or info.st_size != entry['bytes'] or stat.S_IMODE(info.st_mode) != REGULAR_MODES[entry['mode']]:
                raise RuntimeError(f"stored file metadata mismatch: {entry['path']}")
            full_hash = hashlib.sha256()
            object_hash = hashlib.sha1(f"blob {entry['bytes']}\0".encode('ascii'))
            count = 0
            while block := stored.read(CHUNK):
                count += len(block)
                full_hash.update(block)
                object_hash.update(block)
            if count != entry['bytes'] or full_hash.hexdigest() != expected_sha or object_hash.hexdigest() != entry['git_blob_oid']:
                raise RuntimeError(f"stored bytes differ from raw blob: {entry['path']}")
        entry['sha256'] = expected_sha
    finally:
        os.close(parent)


def write_json_new(path, value):
    with path.open('x') as output:
        json.dump(value, output, indent=2)
        output.write('\n')


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--corpus-root', type=Path, required=True)
    parser.add_argument('--output', type=Path, required=True, help='Fresh or existing empty directory')
    parser.add_argument('--pins', type=Path, default=HERE.parent/'performance'/'corpus.json')
    parser.add_argument('--source-id', required=True, help='Original immutable corpus volume or receipt identity')
    parser.add_argument('--receipt-copy', type=Path, help='Optional additional fresh provenance.json path')
    parser.add_argument('--max-files', type=int, default=250000)
    parser.add_argument('--max-total-bytes', type=int, default=8*1024**3)
    parser.add_argument('--git-timeout', type=int, default=600, help='Deadline per tree enumeration or project blob stream')
    args = parser.parse_args()
    if platform.system() != 'Linux' or min(args.max_files, args.max_total_bytes, args.git_timeout) <= 0:
        parser.error('Linux and positive resource bounds are required')
    config = json.loads(args.pins.read_text())
    names = [project['name'] for project in config['projects']]
    if len(names) != 11 or len(set(names)) != 11:
        parser.error('exactly all eleven unique pinned public projects are required')
    for name in names:
        if len(safe_relative(name).parts) != 1:
            parser.error('project names must be single path components')
    output = checked_root(args.output)
    if any(output.iterdir()):
        parser.error('output must be empty; never overwrite or resume a partial materialization')
    if args.receipt_copy and args.receipt_copy.exists():
        parser.error('receipt copy already exists')
    source_root = args.corpus_root.resolve()
    if output == source_root or source_root in output.parents or output in source_root.parents:
        parser.error('input and output roots must be separate trees')
    environment = git_environment()
    receipt = {
        'schema_version': '1.0.0', 'complete': False,
        'created_at': datetime.datetime.now(datetime.timezone.utc).isoformat(),
        'materializer_sha256': digest(Path(__file__)), 'pins_sha256': digest(args.pins),
        'source_id': args.source_id, 'source_root': str(source_root),
        'population': 'all regular Git blobs (100644/100755); other entries inventoried and omitted',
        'transforms': 'none: raw cat-file blob bytes; no checkout, archive, filters, newline conversion or project execution',
        'verification': 'independent full readback SHA256, Git blob SHA1, size and executable mode for every written file',
        'bounds': {'max_files': args.max_files, 'max_total_bytes': args.max_total_bytes,
                   'git_timeout_seconds': args.git_timeout, 'tree_listing_max_bytes': MAX_TREE_OUTPUT},
        'projects': [],
    }
    total_files = total_bytes = 0
    try:
        for project in config['projects']:
            source = source_root/project['name']
            commit = git_text(source, environment, args.git_timeout, 'rev-parse', '--verify', 'HEAD')
            if commit != project['commit'] or len(commit) != 40:
                raise RuntimeError(f"pinned commit mismatch: {project['name']}")
            tree = git_text(source, environment, args.git_timeout, 'rev-parse', '--verify', commit+'^{tree}')
            entry = {'name': project['name'], 'commit': commit, 'tree': tree, 'files': [], 'excluded': []}
            seen = set()
            for item in tree_entries(source, commit, environment, args.git_timeout):
                if item['path'] in seen:
                    raise RuntimeError('duplicate Git path')
                seen.add(item['path'])
                if item['kind'] == 'blob' and item['mode'] in REGULAR_MODES:
                    if item['bytes'] is None or item['bytes'] < 0:
                        raise RuntimeError('invalid regular blob size')
                    entry['files'].append({'path': item['path'], 'mode': item['mode'],
                                           'bytes': item['bytes'], 'git_blob_oid': item['oid']})
                    total_files += 1
                    total_bytes += item['bytes']
                    if total_files > args.max_files or total_bytes > args.max_total_bytes:
                        raise RuntimeError('pinned corpus exceeds declared materialization bounds')
                else:
                    entry['excluded'].append({'path': item['path'], 'mode': item['mode'], 'kind': item['kind'],
                                              'bytes': item['bytes'], 'git_object_oid': item['oid'],
                                              'reason': 'outside regular-blob population'})
            entry['files'].sort(key=lambda value: value['path'])
            entry['excluded'].sort(key=lambda value: value['path'])
            receipt['projects'].append(entry)
        required_free = total_bytes + max(64*1024**2, total_bytes//10)
        if shutil.disk_usage(output).free < required_free:
            raise RuntimeError(f'insufficient free space: require {required_free} bytes')
        root_fd = os.open(output, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
        try:
            for project in receipt['projects']:
                os.mkdir(project['name'], 0o755, dir_fd=root_fd)
                project_fd = os.open(project['name'], os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW, dir_fd=root_fd)
                reader = BlobReader(source_root/project['name'], environment, args.git_timeout)
                success = False
                try:
                    for entry in project['files']:
                        materialize_file(project_fd, entry, reader)
                    success = True
                finally:
                    os.close(project_fd)
                    reader.close(success)
                print(json.dumps({'project': project['name'], 'verified_files': len(project['files']),
                                  'bytes': sum(file['bytes'] for file in project['files']),
                                  'excluded_entries': len(project['excluded'])}), flush=True)
        finally:
            os.close(root_fd)
        receipt['file_count'] = total_files
        receipt['bytes'] = total_bytes
        receipt['excluded_count'] = sum(len(project['excluded']) for project in receipt['projects'])
        receipt['complete'] = True
        receipt['finished_at'] = datetime.datetime.now(datetime.timezone.utc).isoformat()
        write_json_new(output/'provenance.json', receipt)
        if args.receipt_copy:
            write_json_new(args.receipt_copy, receipt)
        print(json.dumps({'complete': True, 'projects': len(receipt['projects']), 'files': total_files,
                          'bytes': total_bytes, 'excluded_entries': receipt['excluded_count'],
                          'provenance_sha256': digest(output/'provenance.json')}), flush=True)
    except BaseException as error:
        receipt['complete'] = False
        receipt['error'] = type(error).__name__+': '+str(error)
        write_json_new(output/'provenance.partial.json', receipt)
        raise


if __name__ == '__main__':
    main()
