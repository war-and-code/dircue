"""Focused local packaging checks; no Go builds, network, or release archives."""
import gzip
import importlib.util
import io
import os
from pathlib import Path
import subprocess
import tarfile
import tempfile
import unittest
import zipfile

ROOT = Path(__file__).resolve().parents[2]
SPEC = importlib.util.spec_from_file_location('release_packager', ROOT/'scripts/release.py')
release = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(release)


class PackagingTest(unittest.TestCase):
    def test_canonical_tar_and_zip_are_independent_of_filename_and_order(self):
        payload = {'README.md':(b'hello\n', False), 'dircue':(b'\x00executable\xff', True)}
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            for windows in [False, True]:
                first, second = root/'first', root/'different-name'
                release.write_archive(first, payload, windows)
                release.write_archive(second, dict(reversed(list(payload.items()))), windows)
                self.assertEqual(first.read_bytes(), second.read_bytes())
                if windows:
                    with zipfile.ZipFile(first) as archive:
                        self.assertEqual(archive.namelist(), sorted(payload))
                        for info in archive.infolist():
                            self.assertEqual(info.date_time, (1980, 1, 1, 0, 0, 0))
                            self.assertEqual(info.external_attr >> 16, 0o100755 if payload[info.filename][1] else 0o100644)
                            self.assertEqual(archive.read(info), payload[info.filename][0])
                else:
                    self.assertEqual(first.read_bytes()[4:8], b'\0'*4)
                    self.assertEqual(first.read_bytes()[3] & 8, 0)  # no gzip filename
                    with tarfile.open(fileobj=io.BytesIO(gzip.decompress(first.read_bytes()))) as archive:
                        self.assertEqual(archive.getnames(), sorted(payload))
                        for info in archive:
                            self.assertEqual((info.uid, info.gid, info.mtime, info.uname, info.gname), (0, 0, 0, '', ''))
                            content, executable = payload[info.name]
                            if content is None:
                                self.assertTrue(info.issym())
                                self.assertEqual(info.linkname, executable)
                            else:
                                self.assertEqual(info.mode, 0o755 if executable else 0o644)
                                self.assertEqual(archive.extractfile(info).read(), content)

    def test_symlink_entry_in_tar_archive(self):
        payload = {'README.md': (b'hello\n', False), 'dircue': (b'\x00bin\xff', True),
                   'dirq': (None, 'dircue')}
        with tempfile.TemporaryDirectory() as temporary:
            path = Path(temporary) / 'out.tar.gz'
            release.write_archive(path, payload)
            with tarfile.open(fileobj=io.BytesIO(gzip.decompress(path.read_bytes()))) as archive:
                members = {m.name: m for m in archive}
                self.assertEqual(sorted(members), sorted(payload))
                self.assertTrue(members['dirq'].issym())
                self.assertEqual(members['dirq'].linkname, 'dircue')
                self.assertFalse(members['dircue'].issym())
                self.assertEqual(archive.extractfile(members['dircue']).read(), b'\x00bin\xff')

    def test_environment_ignores_workspace_arch_flags_and_private_proxy(self):
        poisoned = {'PATH':'/example/bin', 'HOME':'/example/home', 'GOFLAGS':'-race',
                    'GOWORK':'/outside/go.work', 'GOENV':'/outside/config', 'GOROOT':'/wrong',
                    'GOPROXY':'https://credential@example.invalid', 'GOAUTH':'secret-command',
                    'GOEXPERIMENT':'someexperiment', 'GOAMD64':'v4', 'GOARM64':'v9.0',
                    'CGO_ENABLED':'1', 'CGO_CFLAGS':'-march=native', 'GOTOOLCHAIN':'auto'}
        env = release.build_environment('go1.26.6', Path('/temporary'), poisoned)
        for key, value in release.CONTROLS.items():
            self.assertEqual(env[key], value)
        self.assertEqual(env['GOTOOLCHAIN'], 'go1.26.6')
        self.assertEqual(env['PATH'], poisoned['PATH'])
        self.assertNotIn('GOROOT', env)
        self.assertNotIn('CGO_CFLAGS', env)
        self.assertNotIn('credential', str(env))

    def test_output_refuses_empty_existing_directory_and_dangling_link(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            release.require_fresh(root/'new')
            with self.assertRaises(ValueError):
                release.require_fresh(root)
            if hasattr(os, 'symlink'):
                (root/'link').symlink_to(root/'absent')
                with self.assertRaises(ValueError):
                    release.require_fresh(root/'link')

    def test_committed_snapshot_preserves_local_replace_and_ignores_checkout_extras(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)/'repo'
            root.mkdir()
            env = dict(os.environ, GIT_CONFIG_NOSYSTEM='1', GIT_CONFIG_GLOBAL=os.devnull,
                       GIT_AUTHOR_NAME='Packaging Test', GIT_AUTHOR_EMAIL='test@example.invalid',
                       GIT_COMMITTER_NAME='Packaging Test', GIT_COMMITTER_EMAIL='test@example.invalid')

            def git(*args):
                return subprocess.check_output(['git', '-c', 'core.hooksPath='+os.devnull, *args], cwd=root, env=env, stderr=subprocess.DEVNULL)

            git('init', '-q', '--initial-branch=main')
            (root/'nested').mkdir()
            (root/'nested/go.mod').write_text('module dependency\n\ngo 1.26.6\n')
            (root/'go.mod').write_text('module fixture\n\ngo 1.26.6\n\nreplace dependency => ./nested\n')
            (root/'.gitattributes').write_text('tracked.go export-ignore\n')
            (root/'.gitignore').write_text('poison.go\n')
            (root/'tracked.go').write_bytes(b'package fixture\n// exact committed bytes\xff\n')
            git('add', '.')
            git('commit', '-qm', 'fixture')
            revision = release.clean_revision(root)
            (root/'poison.go').write_text('ignored working-tree source must not enter build\n')
            destination = Path(temporary)/'snapshot'
            destination.mkdir()
            self.assertEqual(release.snapshot(root, revision, destination), 5)
            self.assertFalse((destination/'poison.go').exists())
            self.assertEqual((destination/'tracked.go').read_bytes(), (root/'tracked.go').read_bytes())
            self.assertEqual((destination/'nested/go.mod').read_bytes(), (root/'nested/go.mod').read_bytes())
            self.assertEqual(release.clean_revision(root, revision), revision)
            (root/'tracked.go').write_text('dirty')
            with self.assertRaises(ValueError):
                release.clean_revision(root, revision)
            git('add', 'tracked.go')
            git('commit', '-qm', 'changed revision')
            with self.assertRaises(ValueError):
                release.clean_revision(root, revision)

    def test_local_replacement_accepts_aliased_root_and_rejects_real_escapes(self):
        with tempfile.TemporaryDirectory() as temporary:
            parent = Path(temporary)
            source = parent/'source'
            dependency = source/'third_party/go-enry'
            dependency.mkdir(parents=True)
            outside = parent/'outside'
            outside.mkdir()
            (parent/'source-sibling').mkdir()
            self.assertEqual(release.local_replacement(source, './third_party/go-enry'), dependency.resolve())
            for value in ['../outside', '../source-sibling', str(dependency.resolve()), 'missing']:
                with self.subTest(value=value), self.assertRaises(ValueError):
                    release.local_replacement(source, value)
            alias = parent/'alias'
            try:
                alias.symlink_to(source, target_is_directory=True)
                (source/'escape').symlink_to(outside, target_is_directory=True)
            except (OSError, NotImplementedError):
                self.skipTest('directory symlinks unavailable on this host')
            self.assertEqual(release.local_replacement(alias, './third_party/go-enry'), dependency.resolve())
            with self.assertRaises(ValueError):
                release.local_replacement(alias, 'escape')


if __name__ == '__main__':
    unittest.main()
