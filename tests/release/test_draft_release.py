import copy
import importlib.util
import io
import json
import os
from pathlib import Path
import stat
import subprocess
import sys
import tarfile
import tempfile
import unittest
from unittest import mock
import zipfile

ROOT = Path(__file__).resolve().parents[2]
sys.path.insert(0, str(ROOT / 'scripts'))
import draft_release as draft


class SourceContracts(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name)
        self.notes = b'# Release\r\n\r\nExact **notes**.\r\n'
        (self.root / 'NOTES.md').write_bytes(self.notes)
        self.git('init', '-q', '-b', 'main')
        self.git('config', 'user.name', 'Fixture')
        self.git('config', 'user.email', 'fixture@example.invalid')
        self.git('config', 'core.autocrlf', 'false')
        self.git('add', '.')
        self.git('-c', 'commit.gpgsign=false', '-c', 'core.hooksPath=/dev/null', 'commit', '-qm', 'fixture')
        self.commit = self.git('rev-parse', 'HEAD').strip()
        self.git('tag', 'v1.2.3')

    def git(self, *args):
        return subprocess.check_output(['git', *args], cwd=self.root, stderr=subprocess.PIPE).decode()

    def test_exact_notes_and_tag(self):
        self.assertEqual(self.notes, draft.validate_source(self.root, '1.2.3', self.commit, 'NOTES.md'))

    def test_version_and_commit_rejected_before_git_lookup(self):
        for version, commit in [('01.2.3', self.commit), ('1.2.3;echo bad', self.commit), ('1.2.3', '--help')]:
            with self.assertRaises(ValueError):
                draft.validate_source(self.root, version, commit, 'NOTES.md')

    def test_changed_checkout_and_wrong_tag_rejected(self):
        (self.root / 'NOTES.md').write_text('new notes')
        with self.assertRaisesRegex(ValueError, 'clean committed'):
            draft.validate_source(self.root, '1.2.3', self.commit, 'NOTES.md')
        self.git('add', '.')
        self.git('-c', 'commit.gpgsign=false', '-c', 'core.hooksPath=/dev/null', 'commit', '-qm', 'new notes')
        with self.assertRaisesRegex(ValueError, 'tag must point'):
            draft.validate_source(self.root, '1.2.3', self.git('rev-parse', 'HEAD').strip(), 'NOTES.md')

    def test_no_implicit_tag_creation(self):
        with self.assertRaises(subprocess.CalledProcessError):
            draft.validate_source(self.root, '1.2.4', self.commit, 'NOTES.md')
        self.assertEqual('v1.2.3\n', self.git('tag', '--list'))


class ArchiveContracts(unittest.TestCase):
    def test_unsafe_paths_and_duplicate_checksums(self):
        for name in ('../outside', '/absolute', 'a/../b', 'a//b', 'C:/outside', 'a\\b', './a', 'bad\nname'):
            with self.assertRaises(ValueError, msg=name):
                draft.safe_name(name)
        row = b'a' * 64 + b'  same\n'
        with self.assertRaisesRegex(ValueError, 'duplicate'):
            draft.checksums(row + row)

    def test_tar_links_and_duplicate_paths_rejected(self):
        with tempfile.TemporaryDirectory() as temp:
            for link in (True, False):
                path = Path(temp) / ('link.tar.gz' if link else 'duplicates.tar.gz')
                with tarfile.open(path, 'w:gz') as archive:
                    row = tarfile.TarInfo('entry')
                    if link:
                        row.type, row.linkname = tarfile.SYMTYPE, '/outside'
                        archive.addfile(row)
                    else:
                        archive.addfile(row, io.BytesIO())
                        archive.addfile(row, io.BytesIO())
                with self.assertRaises(ValueError):
                    draft.read_archive(path)

    def test_zip_links_and_oversized_entries_rejected(self):
        with tempfile.TemporaryDirectory() as temp:
            path = Path(temp) / 'link.zip'
            with zipfile.ZipFile(path, 'w') as archive:
                row = zipfile.ZipInfo('link')
                row.external_attr = (stat.S_IFLNK | 0o777) << 16
                archive.writestr(row, '/outside')
            with self.assertRaisesRegex(ValueError, 'nonregular'):
                draft.read_archive(path)
            path = Path(temp) / 'large.tar.gz'
            with tarfile.open(path, 'w:gz') as archive:
                row = tarfile.TarInfo('entry')
                row.size = 1024
                archive.addfile(row, io.BytesIO(b'x' * 1024))
            with mock.patch.object(draft, 'MAX_ARCHIVE_BYTES', 512):
                with self.assertRaises(ValueError):
                    draft.read_archive(path)

    def test_incomplete_platform_matrix_refused(self):
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp)
            (root / 'input').mkdir()
            with self.assertRaisesRegex(ValueError, 'exactly five'):
                draft.assemble(root / 'input', root / 'out', '1.2.3', 'a' * 40, b'notes')
            self.assertFalse((root / 'out').exists())


class WorkerContracts(unittest.TestCase):
    def fixture(self, path, *, dirty=False, tamper=False):
        # Synthetic protocol data exercises validation; these bytes are not executable.
        crate = b'synthetic crate bytes'
        checksum = draft.digest(crate)
        lock = ('[[package]]\nname = "dependency"\nversion = "1.0.0"\n'
                'source = "registry+https://example.invalid"\nchecksum = "' + checksum + '"\n').encode()
        sources = {'prototypes/structural/worker/Cargo.lock': draft.digest(lock)}
        prov = {'version': '1.2.3', 'commit': 'a' * 40, 'source_dirty': dirty,
                'platform': 'linux-amd64', 'target': draft.worker_release.TARGETS['linux-amd64'],
                'rust_toolchain': '1.94.0', 'source_sha256': sources,
                'binary_sha256': draft.digest(b'synthetic binary'),
                'dependencies': [{'name': 'dependency', 'version': '1.0.0', 'license': 'MIT',
                                  'source_archive': 'source/crates/dependency-1.0.0.crate', 'sha256': checksum}]}
        payload = {'provenance.json': json.dumps(prov).encode(), 'source/Cargo.lock': lock,
                   'source/crates/dependency-1.0.0.crate': b'tampered' if tamper else crate,
                   'dircue-structural-worker': b'synthetic binary', 'LICENSE': b'MIT',
                   'THIRD_PARTY_NOTICES.txt': b'Fixture notice'}
        payload['SHA256SUMS'] = ''.join(draft.digest(v) + '  ' + k + '\n' for k, v in sorted(payload.items())).encode()
        with tarfile.open(path, 'w:gz') as archive:
            for name, value in payload.items():
                row = tarfile.TarInfo(name)
                row.size = len(value)
                archive.addfile(row, io.BytesIO(value))
        return sources

    def test_dependency_source_and_identity_checks(self):
        with tempfile.TemporaryDirectory() as temp:
            for dirty, tamper in ((False, False), (True, False), (False, True)):
                path = Path(temp) / 'worker.tar.gz'
                sources = self.fixture(path, dirty=dirty, tamper=tamper)
                with mock.patch.object(draft.worker_release, 'source_hashes', return_value=sources):
                    if dirty or tamper:
                        with self.assertRaises(ValueError):
                            draft.verify_worker(path, 'linux-amd64', '1.2.3', 'a' * 40)
                    else:
                        prov, _ = draft.verify_worker(path, 'linux-amd64', '1.2.3', 'a' * 40)
                        self.assertEqual(prov['source_sha256'], sources)
                        with self.assertRaisesRegex(ValueError, 'identity mismatch'):
                            draft.verify_worker(path, 'linux-amd64', '1.2.3', 'b' * 40)

    def test_logical_zip_payload_ignores_compressor_variation(self):
        with tempfile.TemporaryDirectory() as temp:
            paths = [Path(temp) / 'stored.whl', Path(temp) / 'deflated.whl']
            for path, method in zip(paths, (zipfile.ZIP_STORED, zipfile.ZIP_DEFLATED)):
                with zipfile.ZipFile(path, 'w', compression=method) as archive:
                    archive.writestr('package/example', b'logical payload' * 100)
            self.assertNotEqual(paths[0].read_bytes(), paths[1].read_bytes())
            self.assertEqual(draft.read_archive(paths[0]), draft.read_archive(paths[1]))


class AssemblyContracts(unittest.TestCase):
    def test_complete_platform_inventory_and_matching_wheels(self):
        # Existing header fixtures validate archive/wheel assembly, not native execution.
        spec = importlib.util.spec_from_file_location('wheel_test_fixtures', ROOT / 'tests/release/test_wheels.py')
        fixtures = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(fixtures)
        with tempfile.TemporaryDirectory() as temp:
            base = Path(temp)
            incoming = base / 'input'
            incoming.mkdir()
            for platform in draft.PLATFORMS:
                folder = incoming / ('candidate-' + platform)
                folder.mkdir()
                prov = fixtures.fixture_release(folder / 'core')
                selected = next(row for row in prov['archives'] if row['os'] + '-' + row['arch'] == platform)
                for row in prov['archives']:
                    if row != selected:
                        (folder / 'core' / row['name']).unlink()
                prov['archives'], prov['git_tree'] = [selected], 'fixture-tree'
                (folder / 'core/provenance.json').write_text(json.dumps(prov))
                (folder / 'core/SHA256SUMS').write_text(selected['sha256'] + '  ' + selected['name'] + '\n')
                draft.wheels.package(folder / 'core', folder / 'wheels')
                (folder / 'worker').mkdir()
                suffix = '.zip' if platform.startswith('windows') else '.tar.gz'
                name = 'dircue-structural-worker_0.1.0_' + platform + suffix
                (folder / 'worker' / name).write_bytes(b'worker validation is tested separately')
                (folder / 'worker' / (name + '.sha256')).write_text(draft.digest((folder / 'worker' / name).read_bytes()) + '  ' + name + '\n')
                smoke = {'passed': True, 'fixture_count': 21, 'language_count': 20,
                         'candidate_sha256': selected['binary_sha256'], 'worker_sha256': 'fixture-worker',
                         'harness_sha256': draft.digest((ROOT / 'tests/structural_breadth/run.py').read_bytes()),
                         'manifest_sha256': draft.digest((ROOT / 'tests/structural_breadth/fixtures.json').read_bytes())}
                (folder / 'breadth.json').write_text(json.dumps(smoke))
            texts = {'LICENSE': b'MIT\n', 'THIRD_PARTY_NOTICES.md': b'Dependency licenses\n', 'README.md': b'# dircue\n'}
            def git(_root, operation, *args):
                return b'fixture-tree\n' if operation == 'rev-parse' else texts[args[0].split(':', 1)[1]]
            with mock.patch.object(draft.release, 'git', side_effect=git), mock.patch.object(draft, 'verify_worker', return_value=({'binary_sha256': 'fixture-worker'}, {})):
                receipt = draft.assemble(incoming, base / 'out', '0.1.0', '0' * 40, b'notes')
                self.assertEqual(5, len(receipt['platforms']))
                self.assertEqual(7, sum(row['wheels'] for row in receipt['platforms']))
                self.assertEqual(34, len(list((base / 'out').iterdir())))
                self.assertEqual(33, len(draft.checksums((base / 'out/SHA256SUMS').read_bytes())))
                # A matching core hash alone cannot hide an incomplete native smoke run.
                bad = incoming / 'candidate-linux-amd64/breadth.json'
                smoke = json.loads(bad.read_text()); smoke['fixture_count'] = 20
                bad.write_text(json.dumps(smoke))
                with self.assertRaisesRegex(ValueError, 'native smoke'):
                    draft.assemble(incoming, base / 'bad', '0.1.0', '0' * 40, b'notes')

    def declaration_assembly_fixture(self, base, version):
        # Header binaries and synthetic smoke receipts test assembly contracts.
        # They are not native-execution evidence and never leave this temp tree.
        def fixture_module(name, filename):
            spec = importlib.util.spec_from_file_location(name, ROOT / 'tests/release' / filename)
            module = importlib.util.module_from_spec(spec)
            spec.loader.exec_module(module)
            return module
        archives = fixture_module('declaration_archive_fixtures', 'test_wheels.py')
        functions = fixture_module('declaration_function_fixtures', 'test_function_smoke.py')
        declarations = fixture_module('declaration_receipt_fixtures', 'test_declarations_release_smoke.py')
        targeted = fixture_module('targeted_receipt_fixtures', 'test_v070_smoke.py')
        incoming = base / 'input'
        incoming.mkdir()
        worker_hash = 'b' * 64
        for platform in draft.PLATFORMS:
            folder = incoming / ('candidate-' + platform)
            folder.mkdir()
            prov = archives.fixture_release(folder / 'core')
            selected = next(row for row in prov['archives'] if row['os'] + '-' + row['arch'] == platform)
            for row in prov['archives']:
                if row != selected:
                    (folder / 'core' / row['name']).unlink()
            old_name = selected['name']
            selected['name'] = old_name.replace('0.1.0', version)
            (folder / 'core' / old_name).rename(folder / 'core' / selected['name'])
            prov.update(version=version, archives=[selected], git_tree='fixture-tree')
            prov['build_flags'] = [flag.replace('Version=0.1.0', 'Version=' + version) for flag in prov['build_flags']]
            (folder / 'core/provenance.json').write_text(json.dumps(prov))
            (folder / 'core/SHA256SUMS').write_text(selected['sha256'] + '  ' + selected['name'] + '\n')
            draft.wheels.package(folder / 'core', folder / 'wheels')
            (folder / 'worker').mkdir()
            suffix = '.zip' if platform.startswith('windows') else '.tar.gz'
            name = 'dircue-structural-worker_' + version + '_' + platform + suffix
            (folder / 'worker' / name).write_bytes(b'worker validation is tested separately')
            (folder / 'worker' / (name + '.sha256')).write_text(draft.digest((folder / 'worker' / name).read_bytes()) + '  ' + name + '\n')
            breadth = {'passed': True, 'fixture_count': 21, 'language_count': 20,
                       'candidate_sha256': selected['binary_sha256'], 'worker_sha256': worker_hash,
                       'harness_sha256': draft.digest((ROOT / 'tests/structural_breadth/run.py').read_bytes()),
                       'manifest_sha256': draft.digest((ROOT / 'tests/structural_breadth/fixtures.json').read_bytes())}
            (folder / 'breadth.json').write_text(json.dumps(breadth))
            if draft.function_release_smoke.functions_required(version):
                receipt = functions.FunctionReleaseContracts().receipt(version)
                receipt.update(candidate_sha256=selected['binary_sha256'], worker_sha256=worker_hash)
                (folder / 'functions.json').write_text(json.dumps(receipt))
            if draft.declarations_release_smoke.declarations_required(version):
                receipt = declarations.DeclarationReleaseContracts().receipt(version)
                receipt['candidate_sha256'] = selected['binary_sha256']
                (folder / 'declarations.json').write_text(json.dumps(receipt))
                launcher_contracts = fixture_module('native_wheel_contract_fixture', 'test_wheel_release_smoke.py')
                launcher = launcher_contracts.NativeWheelSmokeTests().receipt(version, platform)
                wheel_receipt = json.loads((folder / 'wheels/wheel-provenance.json').read_bytes())
                native_wheel = next(row for row in wheel_receipt['wheels'] if row['platform'] == launcher['wheel_tag_executed'])
                launcher.update(installed_core_sha256=selected['binary_sha256'], wheel=native_wheel['name'], wheel_sha256=native_wheel['sha256'])
                (folder / 'wheel-launcher.json').write_text(json.dumps(launcher))
            if draft.formats_release_smoke.required(version):
                current = fixture_module('v060_capability_proof_fixture', 'test_v060_smoke.py')
                format_proof = current.formats_receipt(version)
                format_proof['candidate_sha256'] = selected['binary_sha256']
                hotspot_proof = current.hotspots_receipt(version)
                hotspot_proof.update(candidate_sha256=selected['binary_sha256'], worker_sha256=worker_hash)
                (folder / 'formats.json').write_text(json.dumps(format_proof))
                (folder / 'hotspots.json').write_text(json.dumps(hotspot_proof))
            if draft.targeted_release_smoke.required(version):
                targeted_proof = targeted.targeted_receipt(version)
                targeted_proof['candidate_sha256'] = selected['binary_sha256']
                (folder / 'targeted.json').write_text(json.dumps(targeted_proof))
        return incoming, worker_hash

    def assemble_fixture(self, incoming, output, version, worker_hash):
        texts = {'LICENSE': b'MIT\n', 'THIRD_PARTY_NOTICES.md': b'Dependency licenses\n', 'README.md': b'# dircue\n'}
        def git(_root, operation, *args):
            return b'fixture-tree\n' if operation == 'rev-parse' else texts[args[0].split(':', 1)[1]]
        with mock.patch.object(draft.release, 'git', side_effect=git), mock.patch.object(draft, 'verify_worker', return_value=({'binary_sha256': worker_hash}, {})):
            return draft.assemble(incoming, output, version, '0' * 40, b'notes')

    def test_v060_capability_proofs_are_nested_and_mandatory(self):
        with tempfile.TemporaryDirectory() as temp:
            base = Path(temp)
            incoming, worker_hash = self.declaration_assembly_fixture(base, '0.6.0')
            result = self.assemble_fixture(incoming, base / 'complete-060', '0.6.0', worker_hash)
            self.assertEqual(44, len(list((base / 'complete-060').iterdir())))
            for platform in result['platforms']:
                self.assertTrue(platform['formats']['passed'])
                self.assertTrue(platform['hotspots']['passed'])
                self.assertTrue(platform['wheel_launcher']['formats']['passed'])
            folder = incoming / ('candidate-' + draft.PLATFORMS[0])
            for index, name in enumerate(('formats', 'hotspots')):
                path = folder / (name + '.json')
                original = path.read_bytes()
                path.unlink()
                with self.assertRaises(FileNotFoundError):
                    self.assemble_fixture(incoming, base / ('missing-060-' + name), '0.6.0', worker_hash)
                path.write_bytes(original)
                receipt = json.loads(original)
                receipt['candidate_sha256'] = 'f' * 64
                path.write_text(json.dumps(receipt))
                with self.assertRaisesRegex(ValueError, 'executable identity'):
                    self.assemble_fixture(incoming, base / ('changed-060-' + name), '0.6.0', worker_hash)
                path.write_bytes(original)

    def test_v070_targeted_proofs_are_nested_assets_and_mandatory(self):
        with tempfile.TemporaryDirectory() as temp:
            base = Path(temp)
            incoming, worker_hash = self.declaration_assembly_fixture(base, '0.7.0-rc.1')
            result = self.assemble_fixture(incoming, base / 'complete-070', '0.7.0-rc.1', worker_hash)
            self.assertEqual(49, len(list((base / 'complete-070').iterdir())))
            wanted = {'targeted-smoke-' + platform + '.json' for platform in draft.PLATFORMS}
            self.assertEqual(wanted, {name for name in result['assets'] if name.startswith('targeted-smoke-')})
            for platform in result['platforms']:
                self.assertTrue(platform['targeted']['passed'])

            folder = incoming / ('candidate-' + draft.PLATFORMS[0])
            path = folder / 'targeted.json'
            original = path.read_bytes()
            path.unlink()
            with self.assertRaises(FileNotFoundError):
                self.assemble_fixture(incoming, base / 'missing-070-targeted', '0.7.0-rc.1', worker_hash)
            path.write_bytes(original)
            changed = json.loads(original)
            changed['candidate_sha256'] = 'f' * 64
            path.write_text(json.dumps(changed))
            with self.assertRaisesRegex(ValueError, 'executable identity'):
                self.assemble_fixture(incoming, base / 'changed-070-targeted', '0.7.0-rc.1', worker_hash)

    def test_declaration_receipts_add_five_assets_only_from_v050(self):
        for version, expected_count in [('0.4.0', 39), ('0.5.0-rc.1', 44), ('0.5.0', 44)]:
            with self.subTest(version=version), tempfile.TemporaryDirectory() as temp:
                base = Path(temp)
                incoming, worker_hash = self.declaration_assembly_fixture(base, version)
                receipt = self.assemble_fixture(incoming, base / 'out', version, worker_hash)
                self.assertEqual(expected_count, len(list((base / 'out').iterdir())))
                self.assertEqual(expected_count - 1, len(draft.checksums((base / 'out/SHA256SUMS').read_bytes())))
                wanted = {'declarations-smoke-' + platform + '.json' for platform in draft.PLATFORMS} if version.startswith('0.5.') else set()
                self.assertEqual(wanted, {name for name in receipt['assets'] if name.startswith('declarations-smoke-')})
                for platform in receipt['platforms']:
                    self.assertEqual(version.startswith('0.5.'), 'wheel_launcher' in platform)

    def test_native_wheel_proof_is_required_and_source_bound(self):
        with tempfile.TemporaryDirectory() as temp:
            base = Path(temp)
            incoming, worker_hash = self.declaration_assembly_fixture(base, '0.5.0')
            filename = incoming / 'candidate-linux-amd64/wheel-launcher.json'
            raw = filename.read_bytes()
            filename.unlink()
            with self.assertRaises(FileNotFoundError):
                self.assemble_fixture(incoming, base / 'missing-launcher', '0.5.0', worker_hash)
            for index, (field, value) in enumerate([
                    ('wheel_sha256', 'e' * 64), ('installed_core_sha256', 'f' * 64), ('version', '0.4.0'),
                    ('harness_sha256', {}), ('checks', []), ('source_removed_before_compare', False)]):
                changed = json.loads(raw)
                changed[field] = value
                filename.write_text(json.dumps(changed))
                with self.subTest(field=field), self.assertRaises(ValueError):
                    self.assemble_fixture(incoming, base / ('tampered-launcher-' + str(index)), '0.5.0', worker_hash)
            filename.write_bytes(raw)

    def test_native_smoke_uses_extracted_core_and_version_gate(self):
        # This checks orchestration only; the helper has separate real-core tests.
        for version in ('0.4.0', '0.5.0-rc.1', '0.6.0-rc.1', '0.7.0-rc.1'):
            with self.subTest(version=version), tempfile.TemporaryDirectory() as temp:
                base = Path(temp)
                incoming, worker_hash = self.declaration_assembly_fixture(base, version)
                platform = 'linux-amd64'
                folder = incoming / ('candidate-' + platform)
                expected = json.loads((folder / 'core/provenance.json').read_bytes())['archives'][0]['binary_sha256']
                declarations_path = folder / 'declarations.json'
                declarations_path.unlink(missing_ok=True)
                seen = []
                def check_core(binary, selected_version):
                    self.assertEqual(version, selected_version)
                    self.assertEqual('dircue', binary.name)
                    self.assertEqual(expected, draft.digest(binary.read_bytes()))
                    seen.append(binary)
                    return {'test-only-orchestration': True}
                for name in ('formats', 'hotspots', 'targeted'):
                    (folder / (name + '.json')).unlink(missing_ok=True)
                def check_hotspots(binary, worker, selected_version):
                    self.assertEqual(b'worker-header-fixture', worker.read_bytes())
                    return check_core(binary, selected_version)
                worker_payload = {'dircue-structural-worker': b'worker-header-fixture'}
                with mock.patch.object(draft, 'verify_worker', return_value=({'binary_sha256': worker_hash}, worker_payload)), \
                     mock.patch.object(draft.subprocess, 'run', return_value=subprocess.CompletedProcess([], 0, f'dircue {version}\n'.encode(), b'')), \
                     mock.patch.object(draft.function_release_smoke, 'run', return_value={'function-orchestration-only': True}), \
                     mock.patch.object(draft.declarations_release_smoke, 'run', side_effect=check_core), \
                     mock.patch.object(draft.formats_release_smoke, 'run', side_effect=check_core), \
                     mock.patch.object(draft.hotspots_release_smoke, 'run', side_effect=check_hotspots), \
                     mock.patch.object(draft.targeted_release_smoke, 'run', side_effect=check_core):
                    draft.native_smoke(folder, platform, version, '0' * 40)
                required = draft.declarations_release_smoke.declarations_required(version)
                content_required = draft.formats_release_smoke.required(version)
                targeted_required = draft.targeted_release_smoke.required(version)
                self.assertEqual(required, declarations_path.exists())
                self.assertEqual(int(required) + 2 * int(content_required) + int(targeted_required), len(seen))
                for name in ('formats', 'hotspots'):
                    self.assertEqual(content_required, (folder / (name + '.json')).exists())
                self.assertEqual(targeted_required, (folder / 'targeted.json').exists())
                if required:
                    self.assertEqual({'test-only-orchestration': True}, json.loads(declarations_path.read_bytes()))

    def test_all_five_declaration_receipts_are_mandatory(self):
        with tempfile.TemporaryDirectory() as temp:
            base = Path(temp)
            incoming, worker_hash = self.declaration_assembly_fixture(base, '0.5.0')
            for platform in draft.PLATFORMS:
                receipt = incoming / ('candidate-' + platform) / 'declarations.json'
                original = receipt.read_bytes()
                receipt.unlink()
                with self.subTest(platform=platform), self.assertRaises(FileNotFoundError):
                    self.assemble_fixture(incoming, base / ('missing-' + platform), '0.5.0', worker_hash)
                self.assertFalse((base / ('missing-' + platform)).exists())
                receipt.write_bytes(original)

    def test_declaration_assembly_rejects_tampered_receipts(self):
        with tempfile.TemporaryDirectory() as temp:
            base = Path(temp)
            incoming, worker_hash = self.declaration_assembly_fixture(base, '0.5.0')
            target = incoming / ('candidate-' + draft.PLATFORMS[0]) / 'declarations.json'
            original = json.loads(target.read_bytes())
            mutations = {
                'version': lambda r: r.update(version='0.4.0'),
                'source': lambda r: r.update(source_sha256={}),
                'fixture': lambda r: r.update(fixture_sha256={}),
                'core_hash': lambda r: r.update(candidate_sha256='f' * 64),
                'checks_missing': lambda r: r['checks'].remove('offline_compare_after_source_removal'),
                'checks_duplicate': lambda r: r['checks'].append('one_eight_workers'),
                'facts': lambda r: r['observed_facts'].update(java_release='8'),
                'passed': lambda r: r.update(passed=False),
            }
            for name, mutate in mutations.items():
                changed = copy.deepcopy(original)
                mutate(changed)
                target.write_text(json.dumps(changed))
                with self.subTest(case=name), self.assertRaisesRegex(ValueError, 'declaration smoke'):
                    self.assemble_fixture(incoming, base / ('bad-' + name), '0.5.0', worker_hash)
                self.assertFalse((base / ('bad-' + name)).exists())
            target.write_bytes(b'{')
            with self.assertRaises(ValueError):
                self.assemble_fixture(incoming, base / 'malformed', '0.5.0', worker_hash)

    def test_downloaded_draft_assets_are_verified_and_mismatch_fails(self):
        with tempfile.TemporaryDirectory() as temp:
            output = Path(temp) / 'assets'
            output.mkdir()
            (output / 'example.tar.gz').write_bytes(b'asset bytes')
            for tampered in (False, True):
                def download(command, **_kwargs):
                    self.assertEqual(['gh', 'release', 'download', 'v1.2.3'], command[:4])
                    target = Path(command[command.index('--dir') + 1])
                    (target / 'example.tar.gz').write_bytes(b'wrong' if tampered else b'asset bytes')
                with mock.patch.object(subprocess, 'check_output', return_value=b'{"isDraft":true,"tagName":"v1.2.3"}'), mock.patch.object(subprocess, 'run', side_effect=download):
                    if tampered:
                        with self.assertRaisesRegex(ValueError, 'checksum mismatch'):
                            draft.verify_uploaded_draft(output, 'owner/project', '1.2.3')
                    else:
                        draft.verify_uploaded_draft(output, 'owner/project', '1.2.3')
                        self.assertTrue((output.parent / 'download-verification.json').exists())
            with mock.patch.object(subprocess, 'check_output', return_value=b'{"isDraft":false,"tagName":"v1.2.3"}'):
                with self.assertRaisesRegex(ValueError, 'expected draft'):
                    draft.verify_uploaded_draft(output, 'owner/project', '1.2.3')


class RemoteContracts(unittest.TestCase):
    def test_context_rejects_pr_branch_and_wrong_sha(self):
        context = {'GITHUB_EVENT_NAME': 'workflow_dispatch', 'GITHUB_REF': 'refs/heads/main',
                   'GITHUB_SHA': 'a' * 40, 'GITHUB_REPOSITORY': 'owner/project', 'GH_TOKEN': 'test-only',
                   'GITHUB_SERVER_URL': 'https://github.com', 'GITHUB_API_URL': 'https://api.github.com'}
        with mock.patch.dict(os.environ, context, clear=True):
            self.assertEqual('owner/project', draft.github_context('a' * 40))
            for key, value in [('GITHUB_EVENT_NAME', 'pull_request'), ('GITHUB_REF', 'refs/heads/feature'), ('GITHUB_SHA', 'b' * 40),
                               ('GITHUB_SERVER_URL', 'https://enterprise.invalid'), ('GITHUB_API_URL', 'https://enterprise.invalid/api/v3'),
                               ('GH_HOST', 'enterprise.invalid')]:
                with mock.patch.dict(os.environ, {key: value}):
                    with self.assertRaises(ValueError):
                        draft.github_context('a' * 40)

    def test_existing_draft_and_published_release_both_refused(self):
        for is_draft in (True, False):
            with mock.patch.object(draft, 'api', return_value=[{'tag_name': 'v1.2.3', 'draft': is_draft}]):
                with self.assertRaisesRegex(ValueError, 'refusing to overwrite'):
                    draft.ensure_remote_unused('owner/project', '1.2.3', 'a' * 40)

    def test_paginated_draft_detected_and_api_failure_not_absence(self):
        page = [{'tag_name': 'v0.0.' + str(i)} for i in range(100)]
        with mock.patch.object(draft, 'api', side_effect=[page, [{'tag_name': 'v1.2.3', 'draft': True}]]):
            with self.assertRaisesRegex(ValueError, 'refusing to overwrite'):
                draft.ensure_remote_unused('owner/project', '1.2.3', 'a' * 40)
        with mock.patch.object(draft, 'api', side_effect=OSError('network unavailable')):
            with self.assertRaises(OSError):
                draft.ensure_remote_unused('owner/project', '1.2.3', 'a' * 40)

    def test_annotated_tag_and_remote_movement(self):
        for actual in ('a' * 40, 'b' * 40):
            with mock.patch.object(draft, 'api', side_effect=[[], {'object': {'type': 'tag', 'sha': 'c' * 40}},
                                                            {'object': {'type': 'commit', 'sha': actual}}]):
                if actual == 'a' * 40:
                    draft.ensure_remote_unused('owner/project', '1.2.3', actual)
                else:
                    with self.assertRaisesRegex(ValueError, 'remote tag'):
                        draft.ensure_remote_unused('owner/project', '1.2.3', 'a' * 40)


if __name__ == '__main__':
    unittest.main()
