#!/usr/bin/env python3
"""Package verified dircue release binaries as local wheels. Never builds or publishes."""
import argparse
import base64
import csv
import hashlib
import io
import json
import os
from pathlib import Path
import re
import shutil
import stat
import struct
import tarfile
import tempfile
import zipfile

PLATFORMS = {
    ('linux', 'amd64'): ('manylinux_2_17_x86_64', 'musllinux_1_2_x86_64'),
    ('linux', 'arm64'): ('manylinux_2_17_aarch64', 'musllinux_1_2_aarch64'),
    ('darwin', 'amd64'): ('macosx_12_0_x86_64',),
    ('darwin', 'arm64'): ('macosx_12_0_arm64',),
    ('windows', 'amd64'): ('win_amd64',),
}
LAUNCHER = '''"""Locate and run the bundled dircue executable without downloading anything."""
import errno
import os
import subprocess
import sys


def get_binary_path():
    return os.path.join(os.path.dirname(os.path.abspath(__file__)), "bin", {binary!r})


def main():
    binary = get_binary_path()
    arguments = [binary, *sys.argv[1:]]
    try:
        if sys.platform == "win32":
            child = subprocess.Popen(arguments)
            while True:
                try:
                    raise SystemExit(child.wait())
                except KeyboardInterrupt:
                    # The child shares the console and receives Ctrl-C itself.
                    # Keep its exit status instead of killing it or tracing here.
                    continue
        os.execv(binary, arguments)
    except OSError as error:
        print("dircue: cannot execute bundled binary: " + str(error), file=sys.stderr)
        raise SystemExit(127 if error.errno == errno.ENOENT else 126)
'''


def sha(data):
    return hashlib.sha256(data).hexdigest()


def encoded(value):
    return (json.dumps(value, indent=2, sort_keys=True) + '\n').encode()


def python_version(version):
    match = re.fullmatch(r'(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(?:-(alpha|beta|rc)\.(0|[1-9][0-9]*))?', version)
    if not match:
        raise ValueError('release version must be X.Y.Z or X.Y.Z-(alpha|beta|rc).N')
    base = '.'.join(match.group(1, 2, 3))
    return base + ({'alpha': 'a', 'beta': 'b', 'rc': 'rc'}[match[4]] + match[5] if match[4] else '')


def validate_binary(content, target):
    """Check architecture and linkage before assigning compatibility platform tags."""
    operating_system, architecture = target
    try:
        if operating_system == 'linux':
            if content[:6] != b'\x7fELF\x02\x01' or len(content) < 64:
                raise ValueError('Linux binary must be little-endian ELF64')
            machine = struct.unpack_from('<H', content, 18)[0]
            if machine != {'amd64': 62, 'arm64': 183}[architecture]:
                raise ValueError('ELF architecture does not match release target')
            offset = struct.unpack_from('<Q', content, 32)[0]
            size, count = struct.unpack_from('<HH', content, 54)
            if size != 56 or not count or offset < 64 or offset + size * count > len(content):
                raise ValueError('invalid ELF program headers')
            kinds = [struct.unpack_from('<I', content, offset + size * i)[0] for i in range(count)]
            if 1 not in kinds or any(kind in (2, 3) for kind in kinds):
                raise ValueError('Linux wheel requires static ELF without PT_DYNAMIC or PT_INTERP')
            return {'format': 'ELF64', 'static': True, 'machine': machine}
        if operating_system == 'darwin':
            if content[:4] != b'\xcf\xfa\xed\xfe' or len(content) < 32:
                raise ValueError('Darwin binary must be little-endian Mach-O64')
            machine, _, kind, count, size = struct.unpack_from('<IIIII', content, 4)
            if machine != {'amd64': 0x01000007, 'arm64': 0x0100000c}[architecture] or kind != 2:
                raise ValueError('Mach-O executable architecture does not match release target')
            end = 32 + size
            if end > len(content) or not count:
                raise ValueError('invalid Mach-O load commands')
            offset, minimums = 32, []
            for _ in range(count):
                command, length = struct.unpack_from('<II', content, offset)
                if length < 8 or offset + length > end:
                    raise ValueError('invalid Mach-O load command bounds')
                if command == 0x32:
                    if length < 24 or struct.unpack_from('<I', content, offset + 8)[0] != 1:
                        raise ValueError('Mach-O build platform is not macOS')
                    minimums.append(struct.unpack_from('<I', content, offset + 12)[0])
                elif command == 0x24:
                    if length < 16:
                        raise ValueError('invalid macOS minimum-version command')
                    minimums.append(struct.unpack_from('<I', content, offset + 8)[0])
                offset += length
            if offset != end or minimums != [12 << 16]:
                raise ValueError('Mach-O must declare the reviewed macOS 12.0 minimum')
            return {'format': 'Mach-O64', 'machine': machine, 'minimum_macos': '12.0'}
        if operating_system == 'windows':
            if content[:2] != b'MZ':
                raise ValueError('Windows binary must be PE')
            offset = struct.unpack_from('<I', content, 60)[0]
            if content[offset:offset + 4] != b'PE\0\0' or struct.unpack_from('<H', content, offset + 4)[0] != 0x8664:
                raise ValueError('PE architecture does not match Windows amd64')
            return {'format': 'PE', 'machine': 0x8664}
    except (struct.error, IndexError) as error:
        raise ValueError('truncated executable headers') from error
    raise ValueError('unsupported release target')


def archive_payload(raw, windows):
    """Read only a flat regular payload into memory; never extract archive paths."""
    payload = {}
    total = 0
    def add(name, size, reader):
        nonlocal total
        total += size
        if name not in ('dircue', 'dircue.exe', 'LICENSE', 'README.md', 'THIRD_PARTY_NOTICES.md') or name in payload:
            raise ValueError('unexpected or duplicate release archive member')
        if size < 0 or size > 128 * 1024**2 or total > 256 * 1024**2:
            raise ValueError('release archive payload exceeds size limit')
        content = reader()
        if len(content) != size:
            raise ValueError('truncated release archive member')
        payload[name] = content
    if windows:
        with zipfile.ZipFile(io.BytesIO(raw)) as archive:
            for member in archive.infolist():
                if not stat.S_ISREG(member.external_attr >> 16) or member.flag_bits & 1:
                    raise ValueError('release ZIP must contain only unencrypted regular files')
                add(member.filename, member.file_size, lambda: archive.read(member))
    else:
        with tarfile.open(fileobj=io.BytesIO(raw), mode='r:gz') as archive:
            for member in archive:
                if not member.isfile():
                    raise ValueError('release tar must contain only regular files')
                add(member.name, member.size, lambda: archive.extractfile(member).read())
    binary = 'dircue.exe' if windows else 'dircue'
    if set(payload) != {binary, 'LICENSE', 'README.md', 'THIRD_PARTY_NOTICES.md'}:
        raise ValueError('release archive is missing a required payload')
    return payload


def load_release(directory):
    raw_provenance = (directory / 'provenance.json').read_bytes()
    provenance = json.loads(raw_provenance)
    version = python_version(provenance['version'])
    controls = provenance.get('controls', {})
    if (provenance.get('cgo_enabled') is not False or controls.get('CGO_ENABLED') != '0'
            or controls.get('GOAMD64') != 'v1' or controls.get('GOARM64') != 'v8.0'
            or controls.get('selected_toolchain') != 'go1.26.6'
            or not provenance.get('toolchain', '').startswith('go version go1.26.6 ')
            or provenance.get('clean_checkout') is not True
            or provenance.get('head_and_clean_state_verified_after_build') is not True):
        raise ValueError('release provenance does not identify the reviewed clean static Go build')
    flags = ['-mod=readonly', '-buildvcs=false', '-trimpath', '-ldflags',
             '-s -w -X github.com/war-and-code/dircue/internal/cli.Version=' + provenance['version']]
    if provenance.get('build_flags') != flags:
        raise ValueError('release version or build flags do not match provenance')
    sums = {}
    for line in (directory / 'SHA256SUMS').read_text().splitlines():
        match = re.fullmatch(r'([0-9a-f]{64})  ([^/\\]+)', line)
        if not match or match[2] in sums:
            raise ValueError('invalid or duplicate release checksum entry')
        sums[match[2]] = match[1]
    records = provenance['archives']
    if not records or len({row['name'] for row in records}) != len(records) or set(sums) != {row['name'] for row in records}:
        raise ValueError('release checksum inventory differs from provenance')
    loaded = []
    targets = set()
    for row in records:
        target = row['os'], row['arch']
        if target not in PLATFORMS or target in targets:
            raise ValueError('unsupported or duplicate release target')
        targets.add(target)
        windows = target[0] == 'windows'
        expected = 'dircue_{}_{}_{}{}'.format(provenance['version'], *target, '.zip' if windows else '.tar.gz')
        if row['name'] != expected:
            raise ValueError('release archive name does not match its version and target')
        raw = (directory / expected).read_bytes()
        if sha(raw) != row['sha256'] or sha(raw) != sums[expected]:
            raise ValueError('release archive checksum mismatch')
        payload = archive_payload(raw, windows)
        binary = 'dircue.exe' if windows else 'dircue'
        if sha(payload[binary]) != row['binary_sha256']:
            raise ValueError('release executable checksum mismatch')
        verification = validate_binary(payload[binary], target)
        loaded.append((row, payload, verification))
    return provenance, raw_provenance, version, loaded


def wheel_files(version, platform, row, payload, provenance):
    info = f'dircue-{version}.dist-info'
    binary = 'dircue.exe' if row['os'] == 'windows' else 'dircue'
    metadata = ('Metadata-Version: 2.4\nName: dircue\nVersion: ' + version + '\n'
                'Summary: Profile source code repos and other directories of computer content.\n'
                'Requires-Python: >=3.10\nLicense-Expression: MIT\n'
                'License-File: LICENSE\nLicense-File: THIRD_PARTY_NOTICES.md\n'
                'Project-URL: Source, https://github.com/war-and-code/dircue\n'
                'Project-URL: Issues, https://github.com/war-and-code/dircue/issues\n'
                'Description-Content-Type: text/markdown\n\n' + payload['README.md'].decode('utf-8'))
    files = {
        'dircue/__init__.py': LAUNCHER.format(binary=binary).encode(),
        'dircue/__main__.py': b'from . import main\n\nmain()\n',
        'dircue/bin/' + binary: payload[binary],
        info + '/METADATA': metadata.encode(),
        info + '/WHEEL': ('Wheel-Version: 1.0\nGenerator: dircue archive adapter 1\nRoot-Is-Purelib: false\nTag: py3-none-' + platform + '\n').encode(),
        info + '/entry_points.txt': b'[console_scripts]\ndircue = dircue:main\n',
        info + '/licenses/LICENSE': payload['LICENSE'],
        info + '/licenses/THIRD_PARTY_NOTICES.md': payload['THIRD_PARTY_NOTICES.md'],
        info + '/release-provenance.json': provenance,
        info + '/bundled-binary.json': encoded(row),
    }
    record = info + '/RECORD'
    buffer = io.StringIO(newline='')
    writer = csv.writer(buffer, lineterminator='\n')
    for name, content in sorted(files.items()):
        digest = base64.urlsafe_b64encode(hashlib.sha256(content).digest()).rstrip(b'=').decode()
        writer.writerow([name, 'sha256=' + digest, len(content)])
    writer.writerow([record, '', ''])
    files[record] = buffer.getvalue().encode()
    return files


def write_wheel(path, files):
    with zipfile.ZipFile(path, 'w', compression=zipfile.ZIP_DEFLATED, compresslevel=9) as archive:
        for name, content in sorted(files.items()):
            info = zipfile.ZipInfo(name, date_time=(1980, 1, 1, 0, 0, 0))
            info.create_system = 3
            info.external_attr = (0o100755 if '/bin/' in name else 0o100644) << 16
            info.compress_type = zipfile.ZIP_DEFLATED
            archive.writestr(info, content, compresslevel=9)


def package(release_dir, output):
    if os.path.lexists(output):
        raise ValueError('wheel output must not already exist')
    provenance, raw_provenance, version, loaded = load_release(release_dir)
    wheels = []
    with tempfile.TemporaryDirectory(prefix='dircue-wheels-') as temporary:
        staging = Path(temporary)
        for row, payload, verification in loaded:
            for platform in PLATFORMS[(row['os'], row['arch'])]:
                filename = f'dircue-{version}-py3-none-{platform}.whl'
                path = staging / filename
                write_wheel(path, wheel_files(version, platform, row, payload, raw_provenance))
                wheels.append({'name': filename, 'sha256': sha(path.read_bytes()),
                               'source_archive': row['name'], 'source_archive_sha256': row['sha256'],
                               'binary_sha256': row['binary_sha256'], 'platform': platform,
                               'binary_verification': verification})
        receipt = {'version': version, 'binary_version': provenance['version'],
                   'release_provenance_sha256': sha(raw_provenance),
                   'source_revision': provenance['git_revision'], 'go_rebuilt': False,
                   'packager_sha256': sha(Path(__file__).read_bytes()), 'wheels': wheels}
        (staging / 'wheel-provenance.json').write_bytes(encoded(receipt))
        (staging / 'release-provenance.json').write_bytes(raw_provenance)
        (staging / 'SHA256SUMS').write_text(''.join(f"{row['sha256']}  {row['name']}\n" for row in wheels))
        output.mkdir(parents=True, exist_ok=False)
        for path in sorted(staging.iterdir()):
            shutil.copyfile(path, output / path.name)
    return receipt


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--release-dir', type=Path, required=True, help='Verified archives from scripts/release.py')
    parser.add_argument('--output', type=Path, required=True, help='Fresh local wheel directory')
    args = parser.parse_args()
    try:
        receipt = package(args.release_dir.resolve(), args.output.absolute())
    except (ValueError, KeyError, OSError, tarfile.TarError, zipfile.BadZipFile) as error:
        parser.error(str(error))
    for row in receipt['wheels']:
        print(args.output / row['name'])


if __name__ == '__main__':
    main()
