"""Check offline installation controls and binding to the selected native core."""
import copy
import json
import os
from pathlib import Path, PureWindowsPath
import subprocess
import sys
import tempfile
import unittest
from unittest import mock

ROOT = Path(__file__).resolve().parents[2]
sys.path.insert(0, str(ROOT / 'scripts'))
import wheel_release_smoke as smoke


class NativeWheelSmokeTests(unittest.TestCase):
    def test_windows_harness_keys_use_portable_slashes(self):
        class WindowsFile(PureWindowsPath):
            def resolve(self):
                return self

            def read_bytes(self):
                return b'fixture source'
        root = WindowsFile('C:/checkout')
        with mock.patch.object(smoke, 'ROOT', root), mock.patch.object(smoke, 'Path', return_value=root / 'scripts/wheel_release_smoke.py'):
            self.assertEqual({'scripts/wheel_release_smoke.py', 'scripts/declarations_release_smoke.py', 'scripts/wheels.py'}, set(smoke.source_inputs()))

    def receipt(self, version='0.5.0', target='linux-amd64'):
        required = smoke.declarations.declarations_required(version)
        assess = smoke.declarations.assessment_required(version)
        tag = smoke.wheels.PLATFORMS[tuple(target.split('-'))][0]
        keys = ({'default_languages', 'default_all'} |
                ({'declarations', 'combined', 'changed_compare', 'identical_compare', 'partial_compare'} if required else set()) |
                ({'assessment', 'assessment_all'} if assess else set()))
        receipt = {'schema_version': '1.0.0', 'passed': True, 'version': version, 'platform': target,
                'wheel': f'dircue-{smoke.wheels.python_version(version)}-py3-none-{tag}.whl',
                'wheel_sha256': 'a' * 64, 'wheel_tag_executed': tag, 'installed_core_sha256': 'b' * 64,
                'launcher_sha256': 'c' * 64, 'installation': smoke.INSTALLATION, 'scope': smoke.SCOPE,
                'checks': sorted(smoke.declarations.DEFAULT_CHECKS |
                                 (smoke.declarations.DECLARATION_CHECKS if required else set()) |
                                 (smoke.declarations.ASSESSMENT_CHECKS if assess else set())),
                'stdout_sha256': {key: 'd' * 64 for key in keys}, 'source_removed_before_compare': required,
                'fixture_sha256': smoke.declarations.fixture_inputs(),
                'observed_facts': ({'declarations': smoke.declarations.FACTS,
                                    'assessment': smoke.declarations.ASSESSMENT_FACTS} if assess else
                                   smoke.declarations.FACTS if required else {}),
                'negative_cases': ['duplicate-json-key', 'malformed-json'] if required else [],
                'harness_sha256': smoke.source_inputs()}
        if assess:
            receipt['assessment_required'] = True
        if smoke.formats.required(version):
            from test_v060_smoke import formats_receipt
            receipt['formats'] = formats_receipt(version)
            receipt['formats']['candidate_sha256'] = receipt['launcher_sha256']
        return receipt

    def test_receipt_scope_and_identity_are_required(self):
        baseline = self.receipt()
        def validate(receipt):
            return smoke.validate_receipt(receipt, '0.5.0', 'linux-amd64', 'b' * 64, baseline['wheel'], 'a' * 64)
        self.assertEqual(baseline, validate(baseline))
        for field, value in [('passed', False), ('version', '0.4.0'), ('platform', 'windows-amd64'),
                             ('wheel', 'other.whl'), ('wheel_sha256', 'f' * 64), ('installed_core_sha256', 'e' * 64),
                             ('launcher_sha256', ''), ('wheel_tag_executed', 'musllinux_1_2_x86_64'),
                             ('harness_sha256', {}), ('fixture_sha256', {}), ('checks', []),
                             ('stdout_sha256', {}), ('source_removed_before_compare', False),
                             ('installation', 'online'), ('scope', 'all wheels'), ('observed_facts', {}), ('negative_cases', [])]:
            changed = copy.deepcopy(baseline)
            changed[field] = value
            with self.subTest(field=field), self.assertRaises(ValueError):
                validate(changed)
        duplicate = copy.deepcopy(baseline)
        duplicate['checks'].append(duplicate['checks'][0])
        with self.assertRaises(ValueError):
            validate(duplicate)

    def test_060_requires_format_proof_from_actual_launcher(self):
        receipt = self.receipt('0.6.0')
        def validate(value):
            return smoke.validate_receipt(value, '0.6.0', 'linux-amd64', 'b' * 64, receipt['wheel'], 'a' * 64)
        self.assertEqual(receipt, validate(receipt))
        for value in (None, {}, {**receipt['formats'], 'candidate_sha256': 'b' * 64}):
            changed = copy.deepcopy(receipt)
            changed['formats'] = value
            with self.assertRaises(ValueError):
                validate(changed)

    def test_140_requires_assessment_smoke_inventory_and_receipt_gate(self):
        version = '1.4.0-rc.1'
        receipt = self.receipt(version)

        def validate(value):
            return smoke.validate_receipt(value, version, 'linux-amd64', 'b' * 64, receipt['wheel'], 'a' * 64)

        self.assertEqual(receipt, validate(receipt))
        for mutate in (
            lambda value: value['checks'].remove('assessment_lock_partition'),
            lambda value: value['stdout_sha256'].pop('assessment_all'),
            lambda value: value['observed_facts']['assessment'].update(parsed_projects=10),
            lambda value: value.pop('assessment_required'),
            lambda value: value.update(assessment_required=False),
        ):
            changed = copy.deepcopy(receipt)
            mutate(changed)
            with self.assertRaises(ValueError):
                validate(changed)

    def test_pre_140_wheel_receipts_keep_the_previous_shape(self):
        version = '1.3.99'
        receipt = self.receipt(version)
        self.assertNotIn('assessment_required', receipt)
        self.assertEqual(smoke.declarations.DEFAULT_CHECKS | smoke.declarations.DECLARATION_CHECKS,
                         set(receipt['checks']))
        self.assertEqual({'default_languages', 'default_all', 'declarations', 'combined',
                          'changed_compare', 'identical_compare', 'partial_compare'},
                         set(receipt['stdout_sha256']))
        self.assertEqual(smoke.declarations.FACTS, receipt['observed_facts'])
        self.assertEqual(receipt, smoke.validate_receipt(receipt, version, 'linux-amd64', 'b' * 64,
                                                         receipt['wheel'], 'a' * 64))

    def test_assessment_gate_is_taken_from_validated_launcher_receipt(self):
        inner = {'assessment_required': True}
        with mock.patch.object(smoke.declarations, 'validate_receipt', return_value=inner) as validate:
            self.assertIs(smoke.validated_assessment_gate(inner, '1.4.0-rc.1', 'c' * 64), True)
            validate.assert_called_once_with(inner, '1.4.0-rc.1', 'c' * 64)
        older = {}
        with mock.patch.object(smoke.declarations, 'validate_receipt', return_value=older) as validate:
            self.assertIsNone(smoke.validated_assessment_gate(older, '1.3.99', 'c' * 64))
            validate.assert_called_once_with(older, '1.3.99', 'c' * 64)

    def test_inherited_python_and_pip_controls_removed(self):
        env = smoke.environment({'PATH': 'keep', 'SystemRoot': 'keep-windows', 'PYTHONPATH': 'bad',
                                 'PYTHONHOME': 'bad', 'pythonstartup': 'bad', 'PIP_INDEX_URL': 'bad',
                                 'PIP_TARGET': 'bad', 'PIP_CONFIG_FILE': 'bad'})
        self.assertEqual({'PATH': 'keep', 'SystemRoot': 'keep-windows', 'PIP_CONFIG_FILE': os.devnull}, env)

    def test_installer_uses_bundled_pip_and_no_package_index(self):
        for target in ('linux-amd64', 'windows-amd64', 'darwin-arm64'):
            with self.subTest(target=target), mock.patch.object(smoke, 'execute') as execute:
                env, area = {'PIP_CONFIG_FILE': os.devnull}, Path('/temporary')
                wheel, venv = Path('/local/native.whl'), area / 'venv'
                python, commands = smoke.install_wheel(wheel, target, venv, env, area)
                self.assertEqual(3, execute.call_count)
                calls = [entry.args for entry in execute.call_args_list]
                self.assertEqual([sys.executable, '-I', '-m', 'venv', '--without-pip', venv], calls[0][0])
                self.assertEqual([python, '-I', '-m', 'ensurepip', '--default-pip'], calls[1][0])
                self.assertEqual([python, '-I', '-m', 'pip', '--isolated', '--disable-pip-version-check',
                                  '--no-cache-dir', 'install', '--no-index', '--no-deps', '--only-binary=:all:',
                                  '--no-compile', wheel], calls[2][0])
                self.assertTrue(all(entry[1:] == (env, area) for entry in calls))
                self.assertEqual('Scripts' if target.startswith('windows') else 'bin', commands.name)

    def test_host_target_is_explicit(self):
        with mock.patch.object(smoke.platform, 'system', return_value='Windows'), mock.patch.object(smoke.platform, 'machine', return_value='AMD64'):
            self.assertEqual('windows-amd64', smoke.native_platform())
            with self.assertRaisesRegex(ValueError, 'executing host'):
                smoke.run(Path('.'), Path('.'), 'linux-amd64', '0.5.0')

    def test_glibc_selected_and_provenance_mismatches_rejected(self):
        with tempfile.TemporaryDirectory() as temporary:
            directory = Path(temporary)
            core_row = {'os': 'linux', 'arch': 'amd64', 'name': 'core.tar.gz', 'sha256': 'c' * 64, 'binary_sha256': 'b' * 64}
            provenance = {'version': '0.5.0', 'git_revision': 'a' * 40}
            core = (provenance, b'core provenance', '0.5.0', [(core_row, {}, {})])
            filename = 'dircue-0.5.0-py3-none-manylinux_2_17_x86_64.whl'
            (directory / filename).write_bytes(b'wheel bytes')
            baseline = {'version': '0.5.0', 'binary_version': '0.5.0', 'source_revision': 'a' * 40,
                        'release_provenance_sha256': smoke.sha(core[1]), 'wheels': [
                            {'name': filename, 'sha256': smoke.sha(b'wheel bytes'), 'platform': 'manylinux_2_17_x86_64',
                             'binary_sha256': 'b' * 64, 'source_archive': 'core.tar.gz', 'source_archive_sha256': 'c' * 64}]}
            def select(receipt):
                (directory / 'wheel-provenance.json').write_text(json.dumps(receipt))
                with mock.patch.object(smoke.wheels, 'load_release', return_value=core):
                    return smoke.select_wheel(directory, directory, 'linux-amd64', '0.5.0')
            selected, _, tag = select(baseline)
            self.assertEqual(filename, selected.name)
            self.assertEqual('manylinux_2_17_x86_64', tag)
            for field in ('sha256', 'binary_sha256', 'source_archive', 'source_archive_sha256', 'platform'):
                invalid = copy.deepcopy(baseline)
                invalid['wheels'][0][field] = 'incorrect'
                with self.subTest(field=field), self.assertRaises(ValueError):
                    select(invalid)
            duplicate = copy.deepcopy(baseline)
            duplicate['wheels'].append(duplicate['wheels'][0])
            with self.assertRaises(ValueError):
                select(duplicate)

    def test_subprocess_timeout_and_environment_forwarded(self):
        with mock.patch.object(smoke.subprocess, 'run', return_value=subprocess.CompletedProcess([], 0, b'fine', b'')) as run:
            smoke.execute(['python', '-V'], {'PIP_CONFIG_FILE': os.devnull}, Path('/work'))
            self.assertEqual(120, run.call_args.kwargs['timeout'])
            self.assertEqual({'PIP_CONFIG_FILE': os.devnull}, run.call_args.kwargs['env'])
        with mock.patch.object(smoke.subprocess, 'run', return_value=subprocess.CompletedProcess([], 1, b'', b'private text')):
            with self.assertRaises(ValueError) as failure:
                smoke.execute(['python'], {}, Path('/work'))
            self.assertNotIn('private text', str(failure.exception))

    @unittest.skipUnless(os.environ.get('DIRCUE_NATIVE_WHEEL_CORE'), 'set DIRCUE_NATIVE_WHEEL_CORE and DIRCUE_NATIVE_WHEEL_DIR for actual installation')
    def test_actual_native_wheel(self):
        receipt = smoke.run(Path(os.environ['DIRCUE_NATIVE_WHEEL_CORE']), Path(os.environ['DIRCUE_NATIVE_WHEEL_DIR']),
                            smoke.native_platform(), os.environ.get('DIRCUE_NATIVE_WHEEL_VERSION', '0.5.0'))
        self.assertTrue(receipt['passed'])
        self.assertNotEqual(receipt['installed_core_sha256'], receipt['launcher_sha256'])


if __name__ == '__main__':
    unittest.main()
