"""Publication must preserve the verified GitHub wheel payloads."""

import hashlib
import json
from pathlib import Path
import shutil
import sys
import tempfile
import unittest
import zipfile

ROOT = Path(__file__).resolve().parents[2]
sys.path.insert(0, str(ROOT / 'scripts'))
import pypi_release  # noqa: E402
import wheels  # noqa: E402
from test_wheels import fixture_release  # noqa: E402


def sha(data):
    return hashlib.sha256(data).hexdigest()


def fixture(directory, readme=b'# dircue\n'):
    release = directory / 'release'
    provenance = fixture_release(release, readme=readme)
    publication = directory / 'publication'
    wheel_receipt = wheels.package(release, publication)
    platforms = []
    for (os_name, arch), tags in wheels.PLATFORMS.items():
        archive = next(row for row in provenance['archives']
                       if (row['os'], row['arch']) == (os_name, arch))
        platforms.append({'platform': os_name + '-' + arch,
                          'core_sha256': archive['binary_sha256'], 'wheels': len(tags)})
    candidate = {'version': '0.1.0', 'commit': '0' * 40, 'platforms': platforms}
    (publication / 'release-candidate.json').write_text(json.dumps(candidate))
    (publication / 'SHA256SUMS.sigstore.json').write_text('{}')
    manifest = {row['name']: row['sha256'] for row in provenance['archives']}
    manifest.update({row['name']: row['sha256'] for row in wheel_receipt['wheels']})
    manifest['release-candidate.json'] = sha((publication / 'release-candidate.json').read_bytes())
    (publication / 'SHA256SUMS').write_text(''.join(f'{digest}  {name}\n'
                                                     for name, digest in sorted(manifest.items())))
    (publication / 'wheel-provenance.json').unlink()
    (publication / 'release-provenance.json').unlink()
    return publication


class PyPIPublicationTests(unittest.TestCase):
    def test_verified_release_wheels_are_accepted(self):
        with tempfile.TemporaryDirectory() as temporary:
            directory = fixture(Path(temporary))
            result = pypi_release.verify(directory, '0.1.0', '0' * 40)
            self.assertEqual(len(result), 7)

    def test_wrong_commit_or_version_is_rejected(self):
        with tempfile.TemporaryDirectory() as temporary:
            directory = fixture(Path(temporary))
            with self.assertRaisesRegex(ValueError, 'version or commit'):
                pypi_release.verify(directory, '0.1.0', '1' * 40)
            with self.assertRaises(ValueError):
                pypi_release.verify(directory, '0.2.0', '0' * 40)

    def test_stale_pypi_description_is_rejected(self):
        with tempfile.TemporaryDirectory() as temporary:
            directory = fixture(Path(temporary),
                                readme=b'# dircue\n\nThere is no PyPI package.\n')
            with self.assertRaisesRegex(ValueError, 'stale PyPI availability claim'):
                pypi_release.verify(directory, '0.1.0', '0' * 40)

    def test_published_wheel_metadata_uses_release_links(self):
        with tempfile.TemporaryDirectory() as temporary:
            directory = fixture(Path(temporary),
                                readme=b'# dircue\n\n[guide](docs/DISTRIBUTION.md)\n')
            pypi_release.verify(directory, '0.1.0', '0' * 40)
            with zipfile.ZipFile(sorted(directory.glob('*.whl'))[0]) as archive:
                metadata = archive.read('dircue-0.1.0.dist-info/METADATA').decode()
            self.assertIn('](https://github.com/war-and-code/dircue/blob/v0.1.0/'
                          'docs/DISTRIBUTION.md)', metadata)

    def test_missing_extra_or_corrupt_wheel_is_rejected(self):
        for corruption in ('missing', 'extra', 'payload'):
            with self.subTest(corruption=corruption), tempfile.TemporaryDirectory() as temporary:
                directory = fixture(Path(temporary))
                wheel = sorted(directory.glob('*.whl'))[0]
                if corruption == 'missing':
                    wheel.unlink()
                elif corruption == 'extra':
                    shutil.copyfile(wheel, directory / 'unlisted.whl')
                else:
                    wheel.write_bytes(wheel.read_bytes() + b'changed')
                with self.assertRaises(ValueError):
                    pypi_release.verify(directory, '0.1.0', '0' * 40)

    def test_wheel_record_mutation_is_rejected_even_if_manifest_is_updated(self):
        with tempfile.TemporaryDirectory() as temporary:
            directory = fixture(Path(temporary))
            wheel = sorted(directory.glob('*.whl'))[0]
            with zipfile.ZipFile(wheel) as archive:
                entries = {row.filename: archive.read(row) for row in archive.infolist()}
            entries['dircue/__main__.py'] = b'changed after packaging\n'
            with zipfile.ZipFile(wheel, 'w') as archive:
                for name, content in entries.items():
                    archive.writestr(name, content)
            lines = (directory / 'SHA256SUMS').read_text().splitlines()
            (directory / 'SHA256SUMS').write_text(''.join(
                f'{sha(wheel.read_bytes()) if line.endswith(wheel.name) else line.split("  ", 1)[0]}  '
                f'{line.split("  ", 1)[1]}\n' for line in lines))
            with self.assertRaisesRegex(ValueError, 'RECORD mismatch'):
                pypi_release.verify(directory, '0.1.0', '0' * 40)

    def test_candidate_platform_count_is_rejected(self):
        with tempfile.TemporaryDirectory() as temporary:
            directory = fixture(Path(temporary))
            path = directory / 'release-candidate.json'
            candidate = json.loads(path.read_text())
            candidate['platforms'][0]['wheels'] = 7
            path.write_text(json.dumps(candidate))
            manifest = (directory / 'SHA256SUMS').read_text()
            old = next(line for line in manifest.splitlines() if line.endswith('  release-candidate.json'))
            (directory / 'SHA256SUMS').write_text(manifest.replace(old, sha(path.read_bytes()) + '  release-candidate.json'))
            with self.assertRaisesRegex(ValueError, 'wheel count'):
                pypi_release.verify(directory, '0.1.0', '0' * 40)


if __name__ == '__main__':
    unittest.main()
