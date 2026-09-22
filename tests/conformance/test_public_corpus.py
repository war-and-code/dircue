"""Focused safety and provenance checks for the public corpus materializer."""
from io import BytesIO
import hashlib
import json
from pathlib import Path
import tarfile
import tempfile
import unittest

import public


class PublicCorpusTests(unittest.TestCase):
    def make_archive(self, archive, prefix, members):
        with tarfile.open(archive, 'w:gz') as bundle:
            root = tarfile.TarInfo(prefix.rstrip('/'))
            root.type = tarfile.DIRTYPE
            bundle.addfile(root)
            for name, data in members:
                info = tarfile.TarInfo(name)
                info.size = len(data)
                bundle.addfile(info, BytesIO(data))

    def test_archive_root_and_verified_license(self):
        with tempfile.TemporaryDirectory() as temporary:
            work = Path(temporary)
            archive = work / 'fixture.tar.gz'
            license_data = b'fixture license\n'
            self.make_archive(archive, 'project-deadbeef/', [
                ('project-deadbeef/COPYING', license_data),
                ('project-deadbeef/lib/example.pm', b'package Example;\n'),
            ])
            spec = {
                'id': 'fixture',
                'archive_url': 'unused',
                'archive_sha256': hashlib.sha256(archive.read_bytes()).hexdigest(),
                'archive_prefix': 'project-deadbeef/',
                'repository_license': {
                    'path': 'COPYING',
                    'sha256': hashlib.sha256(license_data).hexdigest(),
                },
            }
            target = work / 'target'
            target.mkdir()
            downloads = work / 'downloads'
            downloads.mkdir()
            receipt = public.materialize_archive(spec, target, archive, downloads)
            self.assertEqual(receipt['regular_files'], 2)
            self.assertEqual(receipt['skipped_nonregular'], 0)
            self.assertTrue(receipt['used_supplied_archive'])
            self.assertEqual((target / 'lib/example.pm').read_bytes(), b'package Example;\n')

    def test_manifest_records_each_kernel_file_license(self):
        manifest = json.loads((Path(__file__).parent / 'public_corpus.json').read_text())
        kernel = next(corpus for corpus in manifest['corpora'] if corpus['id'] == 'linux-kernel-slice')
        self.assertEqual(len(kernel['files']), 5)
        for file_spec in kernel['files']:
            self.assertTrue(file_spec['spdx'])
            self.assertEqual(len(file_spec['sha256']), 64)

    def test_fixture_commit_preserves_upstream_ignored_files(self):
        with tempfile.TemporaryDirectory() as temporary:
            fixture = Path(temporary)
            (fixture / '.gitignore').write_text('generated.pm\n')
            (fixture / 'generated.pm').write_text('package Generated;\n')
            public.initialize_fixture(fixture)
            tracked = public.synthetic.git(fixture, 'ls-files').splitlines()
            self.assertEqual(tracked, ['.gitignore', 'generated.pm'])


if __name__ == '__main__':
    unittest.main()
