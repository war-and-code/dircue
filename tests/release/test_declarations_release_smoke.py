"""Receipt rejection contracts; optional real-core execution is selected explicitly."""
import copy
import os
from pathlib import Path
import sys
import unittest

ROOT = Path(__file__).resolve().parents[2]
sys.path.insert(0, str(ROOT / 'scripts'))
import declarations_release_smoke as smoke


class DeclarationReleaseContracts(unittest.TestCase):
    def receipt(self, version='0.5.0-rc.1'):
        # These synthetic receipts exercise validator rejection paths. They are
        # not executable test results and are never retained as release evidence.
        required = smoke.declarations_required(version)
        keys = {'default_languages', 'default_all'}
        if required:
            keys |= {'declarations', 'combined', 'changed_compare', 'identical_compare', 'partial_compare'}
        return {'schema_version': '1.0.0', 'passed': True, 'version': version,
                'declarations_required': required, 'candidate_sha256': 'a' * 64,
                'source_sha256': smoke.source_inputs(), 'fixture_sha256': smoke.fixture_inputs(),
                'checks': sorted(smoke.DEFAULT_CHECKS | (smoke.DECLARATION_CHECKS if required else set())),
                'stdout_sha256': {key: 'b' * 64 for key in keys},
                'observed_facts': copy.deepcopy(smoke.FACTS) if required else {},
                'source_removed_before_compare': required, 'worker_required': False,
                'negative_cases': ['duplicate-json-key', 'malformed-json'] if required else []}

    def test_version_gate_preserves_earlier_releases(self):
        for version in ('0.1.0', '0.2.0', '0.3.0', '0.4.0', '0.4.99'):
            self.assertFalse(smoke.declarations_required(version))
            receipt = self.receipt(version)
            self.assertEqual(receipt, smoke.validate_receipt(receipt, version, 'a' * 64))
        for version in ('0.5.0-alpha.1', '0.5.0-beta.0', '0.5.0-rc.1', '0.5.0', '1.0.0'):
            self.assertTrue(smoke.declarations_required(version))
        for version in ('', '0.5', '00.5.0', '0.5.0-dev', 'v0.5.0', '0.5.0;command'):
            with self.assertRaises(ValueError):
                smoke.declarations_required(version)

    def test_identity_and_coverage_rejection(self):
        baseline = self.receipt()
        self.assertEqual(baseline, smoke.validate_receipt(baseline, '0.5.0-rc.1', 'a' * 64))
        for field, value in [('passed', False), ('version', '0.4.0'), ('declarations_required', False),
                             ('candidate_sha256', 'c' * 64), ('source_sha256', {}), ('fixture_sha256', {}),
                             ('observed_facts', {}), ('source_removed_before_compare', False),
                             ('worker_required', True), ('negative_cases', [])]:
            with self.subTest(field=field):
                changed = copy.deepcopy(baseline)
                changed[field] = value
                with self.assertRaises(ValueError):
                    smoke.validate_receipt(changed, '0.5.0-rc.1', 'a' * 64)

    def test_missing_duplicate_or_invalid_check_evidence_rejected(self):
        for mutate in (
            lambda r: r['checks'].remove('offline_compare_after_source_removal'),
            lambda r: r['checks'].append('one_eight_workers'),
            lambda r: r['checks'].append({}),
            lambda r: r['stdout_sha256'].pop('partial_compare'),
            lambda r: r['stdout_sha256'].update(changed_compare=''),
            lambda r: r['stdout_sha256'].update(changed_compare=3),
            lambda r: r['observed_facts']['manifest_ids'].pop(),
            lambda r: r['observed_facts'].update(cargo_version='0.0.0'),
            lambda r: r['fixture_sha256'].update({'go/app/go.mod': 'e' * 64}),
        ):
            changed = copy.deepcopy(self.receipt())
            mutate(changed)
            with self.assertRaises(ValueError):
                smoke.validate_receipt(changed, '0.5.0-rc.1', 'a' * 64)

    def test_helper_and_fixture_coverage(self):
        self.assertEqual({'scripts/declarations_release_smoke.py', 'scripts/wheels.py'}, set(smoke.source_inputs()))
        self.assertEqual(set(smoke.FIXTURES), set(smoke.fixture_inputs()))
        self.assertEqual(12, len(smoke.EXPECTED_MANIFESTS))
        self.assertEqual(['cargo', 'dotnet', 'go', 'maven', 'npm', 'python-uv'], smoke.FACTS['ecosystems'])
        for value in smoke.fixture_inputs().values():
            self.assertTrue(smoke.valid_digest(value))

    def test_malformed_receipt_shapes_fail_cleanly(self):
        for receipt in (None, [], {}, {'checks': None}):
            with self.assertRaises(ValueError):
                smoke.validate_receipt(receipt, '0.5.0-rc.1', 'a' * 64)

    @unittest.skipUnless(os.environ.get('DIRCUE_DECLARATIONS_SMOKE_BINARY'), 'set DIRCUE_DECLARATIONS_SMOKE_BINARY for real packaged-core execution')
    def test_real_core_smoke(self):
        binary = Path(os.environ['DIRCUE_DECLARATIONS_SMOKE_BINARY'])
        version = os.environ.get('DIRCUE_DECLARATIONS_SMOKE_VERSION', '0.5.0-rc.1')
        receipt = smoke.run(binary, version)
        self.assertTrue(receipt['passed'])
        self.assertEqual(smoke.file_sha(binary), receipt['candidate_sha256'])


if __name__ == '__main__':
    unittest.main()
