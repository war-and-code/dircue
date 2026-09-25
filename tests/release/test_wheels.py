"""Verify archive identity, wheel integrity, and the executable launcher contract."""
import base64
import csv
import errno
import hashlib
import importlib.util
import io
import json
from pathlib import Path
import stat
import struct
import subprocess
import sys
import tarfile
import tempfile
import unittest
from unittest import mock
import zipfile

ROOT = Path(__file__).resolve().parents[2]

def module(name, path):
    spec = importlib.util.spec_from_file_location(name, path)
    value = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(value)
    return value


wheels = module('wheel_adapter', ROOT / 'scripts/wheels.py')
release = module('release_adapter', ROOT / 'scripts/release.py')


def executable(target, dynamic=False, macos=12):
    """Small format-header fixtures, never represented as executable builds."""
    operating_system, architecture = target
    if operating_system == 'linux':
        result = bytearray(120)
        result[:6] = b'\x7fELF\x02\x01'
        struct.pack_into('<H', result, 18, {'amd64': 62, 'arm64': 183}[architecture])
        struct.pack_into('<Q', result, 32, 64)
        struct.pack_into('<HH', result, 54, 56, 1)
        struct.pack_into('<I', result, 64, 3 if dynamic else 1)
    elif operating_system == 'darwin':
        result = bytearray(56)
        result[:4] = b'\xcf\xfa\xed\xfe'
        struct.pack_into('<IIIII', result, 4, {'amd64': 0x01000007, 'arm64': 0x0100000c}[architecture], 0, 2, 1, 24)
        struct.pack_into('<IIIIII', result, 32, 0x32, 24, 1, macos << 16, macos << 16, 0)
    else:
        result = bytearray(70)
        result[:2] = b'MZ'
        struct.pack_into('<I', result, 60, 64)
        result[64:68] = b'PE\0\0'
        struct.pack_into('<H', result, 68, 0x8664)
    return bytes(result)


def fixture_release(directory):
    directory.mkdir()
    records = []
    for target in wheels.PLATFORMS:
        windows = target[0] == 'windows'
        binary = 'dircue.exe' if windows else 'dircue'
        content = executable(target)
        name = 'dircue_0.1.0_{}_{}{}'.format(*target, '.zip' if windows else '.tar.gz')
        payload = {binary: (content, True), 'LICENSE': (b'MIT\n', False),
                   'THIRD_PARTY_NOTICES.md': (b'Dependency licenses\n', False),
                   'README.md': (b'# dircue\n', False)}
        release.write_archive(directory / name, payload, windows)
        records.append({'name': name, 'os': target[0], 'arch': target[1],
                        'sha256': wheels.sha((directory / name).read_bytes()),
                        'binary_sha256': wheels.sha(content)})
    provenance = {'version': '0.1.0', 'toolchain': 'go version go1.26.6 test/host',
                  'clean_checkout': True, 'head_and_clean_state_verified_after_build': True,
                  'git_revision': '0' * 40, 'cgo_enabled': False,
                  'controls': {'CGO_ENABLED': '0', 'GOAMD64': 'v1', 'GOARM64': 'v8.0', 'selected_toolchain': 'go1.26.6'},
                  'build_flags': ['-mod=readonly', '-buildvcs=false', '-trimpath', '-ldflags', '-s -w -X github.com/war-and-code/dircue/internal/cli.Version=0.1.0'],
                  'archives': records}
    (directory / 'provenance.json').write_bytes(wheels.encoded(provenance))
    (directory / 'SHA256SUMS').write_text(''.join(f"{row['sha256']}  {row['name']}\n" for row in records))
    return provenance


class WheelTests(unittest.TestCase):
    def test_wheel_payload_record_metadata_and_reproducibility(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            provenance = fixture_release(root / 'release')
            first = wheels.package(root / 'release', root / 'first')
            second = wheels.package(root / 'release', root / 'second')
            self.assertEqual(first, second)
            self.assertEqual(len(first['wheels']), 7)
            self.assertFalse(first['go_rebuilt'])
            for row in first['wheels']:
                path = root / 'first' / row['name']
                self.assertEqual(path.read_bytes(), (root / 'second' / row['name']).read_bytes())
                with zipfile.ZipFile(path) as archive:
                    entries = {item.filename: archive.read(item) for item in archive.infolist()}
                    self.assertEqual(len(entries), len(archive.infolist()))
                    info = 'dircue-0.1.0.dist-info'
                    record = info + '/RECORD'
                    rows = list(csv.reader(io.StringIO(entries[record].decode())))
                    self.assertEqual({r[0] for r in rows}, set(entries))
                    for name, digest, size in rows:
                        if name == record:
                            self.assertEqual((digest, size), ('', ''))
                        else:
                            encoded = base64.urlsafe_b64encode(hashlib.sha256(entries[name]).digest()).rstrip(b'=').decode()
                            self.assertEqual(digest, 'sha256=' + encoded)
                            self.assertEqual(int(size), len(entries[name]))
                    binary = next(name for name in entries if '/bin/' in name)
                    self.assertEqual(wheels.sha(entries[binary]), row['binary_sha256'])
                    self.assertEqual(stat.S_IMODE(archive.getinfo(binary).external_attr >> 16), 0o755)
                    self.assertEqual(entries[info + '/licenses/LICENSE'], b'MIT\n')
                    self.assertEqual(entries[info + '/licenses/THIRD_PARTY_NOTICES.md'], b'Dependency licenses\n')
                    metadata = entries[info + '/METADATA'].decode()
                    self.assertIn('License-File: THIRD_PARTY_NOTICES.md\n', metadata)
                    self.assertIn('Requires-Python: >=3.10\n', metadata)
                    self.assertIn('Root-Is-Purelib: false\n', entries[info + '/WHEEL'].decode())
                    self.assertIn('Tag: py3-none-' + row['platform'], entries[info + '/WHEEL'].decode())
                    self.assertEqual(json.loads(entries[info + '/release-provenance.json']), provenance)
                    self.assertTrue(all(member.date_time == (1980, 1, 1, 0, 0, 0) for member in archive.infolist()))

    def test_reject_corrupted_archive_and_provenance(self):
        for corruption in ('archive', 'binary_hash', 'version', 'target', 'cgo', 'checksums'):
            with self.subTest(corruption=corruption), tempfile.TemporaryDirectory() as temporary:
                root = Path(temporary)
                provenance = fixture_release(root / 'release')
                if corruption == 'archive':
                    path = root / 'release' / provenance['archives'][0]['name']
                    path.write_bytes(path.read_bytes() + b'x')
                elif corruption == 'binary_hash':
                    provenance['archives'][0]['binary_sha256'] = '0' * 64
                elif corruption == 'version':
                    provenance['version'] = '0.2.0'
                elif corruption == 'target':
                    provenance['archives'][0]['arch'] = '386'
                elif corruption == 'cgo':
                    provenance['controls']['CGO_ENABLED'] = '1'
                else:
                    with (root / 'release/SHA256SUMS').open('a') as output:
                        output.write('0' * 64 + '  unlisted.tar.gz\n')
                (root / 'release/provenance.json').write_bytes(wheels.encoded(provenance))
                with self.assertRaises(ValueError):
                    wheels.package(root / 'release', root / 'output')
                self.assertFalse((root / 'output').exists())

    def test_platform_rejections(self):
        with self.assertRaisesRegex(ValueError, 'static ELF'):
            wheels.validate_binary(executable(('linux', 'amd64'), dynamic=True), ('linux', 'amd64'))
        with self.assertRaisesRegex(ValueError, 'architecture'):
            wheels.validate_binary(executable(('linux', 'amd64')), ('linux', 'arm64'))
        with self.assertRaisesRegex(ValueError, '12.0'):
            wheels.validate_binary(executable(('darwin', 'amd64'), macos=13), ('darwin', 'amd64'))
        with self.assertRaises(ValueError):
            wheels.validate_binary(b'MZ', ('windows', 'amd64'))

    def test_archive_paths_and_symlinks_are_rejected(self):
        for name, kind in (('../dircue', tarfile.REGTYPE), ('dircue', tarfile.SYMTYPE)):
            with self.subTest(name=name, kind=kind):
                buffer = io.BytesIO()
                with tarfile.open(fileobj=buffer, mode='w:gz') as archive:
                    member = tarfile.TarInfo(name)
                    member.type = kind
                    archive.addfile(member, io.BytesIO())
                with self.assertRaises(ValueError):
                    wheels.archive_payload(buffer.getvalue(), False)

    def test_versions_and_output_safety(self):
        self.assertEqual(wheels.python_version('0.1.0-rc.3'), '0.1.0rc3')
        self.assertEqual(wheels.python_version('1.2.3-beta.1'), '1.2.3b1')
        for invalid in ('v0.1.0', '0.1', '0.1.0\nName: other', '01.2.3', '../1.2.3'):
            with self.subTest(version=invalid), self.assertRaises(ValueError):
                wheels.python_version(invalid)
        with tempfile.TemporaryDirectory() as temporary:
            with self.assertRaisesRegex(ValueError, 'already exist'):
                wheels.package(Path('unused'), Path(temporary))

    def test_launcher_import_and_windows_exit_and_error(self):
        namespace = {'__file__': '/installed with spaces/dircue/__init__.py'}
        with mock.patch('os.execv') as execute, mock.patch('subprocess.Popen') as child:
            exec(wheels.LAUNCHER.format(binary='dircue.exe'), namespace)
            execute.assert_not_called()
            child.assert_not_called()
            with mock.patch('sys.platform', 'win32'), mock.patch('sys.argv', ['dircue', 'path with spaces', '--json']):
                child.return_value.wait.return_value = 23
                with self.assertRaises(SystemExit) as status:
                    namespace['main']()
                self.assertEqual(status.exception.code, 23)
                child.assert_called_once_with(['/installed with spaces/dircue/bin/dircue.exe', 'path with spaces', '--json'])
            for number, expected in ((errno.ENOENT, 127), (errno.EACCES, 126)):
                with mock.patch('sys.platform', 'linux'), mock.patch('sys.stderr', io.StringIO()):
                    execute.side_effect = OSError(number, 'test execution failure')
                    with self.assertRaises(SystemExit) as status:
                        namespace['main']()
                    self.assertEqual(status.exception.code, expected)

    def test_windows_launcher_waits_through_console_interrupt(self):
        namespace = {'__file__': '/installed/dircue/__init__.py'}
        exec(wheels.LAUNCHER.format(binary='dircue.exe'), namespace)
        with mock.patch('sys.platform', 'win32'), mock.patch('subprocess.Popen') as spawn:
            spawn.return_value.wait.side_effect = [KeyboardInterrupt(), KeyboardInterrupt(), 130]
            with self.assertRaises(SystemExit) as status:
                namespace['main']()
            self.assertEqual(status.exception.code, 130)
            self.assertEqual(spawn.return_value.wait.call_count, 3)
            spawn.return_value.kill.assert_not_called()
            spawn.return_value.terminate.assert_not_called()

    @unittest.skipIf(sys.platform == 'win32', 'Unix exec contract')
    def test_unix_launcher_preserves_arguments_and_exit_status(self):
        with tempfile.TemporaryDirectory(prefix='dircue launcher ') as temporary:
            root = Path(temporary)
            (root / 'bin').mkdir()
            program = root / 'bin/dircue'
            program.write_text('#!' + sys.executable + '\nimport json,sys\nprint(json.dumps(sys.argv[1:]))\nsys.exit(19)\n')
            program.chmod(0o755)
            wrapper = root / 'launcher.py'
            wrapper.write_text(wheels.LAUNCHER.format(binary='dircue') + '\nmain()\n')
            arguments = ['path with spaces', '--json', 'semi;colon', '$(literal)', '']
            result = subprocess.run([sys.executable, str(wrapper), *arguments], capture_output=True, text=True)
            self.assertEqual(result.returncode, 19, result.stderr)
            self.assertEqual(json.loads(result.stdout), arguments)


if __name__ == '__main__':
    unittest.main()
