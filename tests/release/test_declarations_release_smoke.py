"""Receipt rejection contracts; optional real-core execution is selected explicitly."""
import copy
import json
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
        assessment_required = smoke.assessment_required(version)
        keys = {'default_languages', 'default_all'}
        if required:
            keys |= {'declarations', 'combined', 'changed_compare', 'identical_compare', 'partial_compare'}
        if assessment_required:
            keys |= {'assessment', 'assessment_all'}
        receipt = {'schema_version': '1.0.0', 'passed': True, 'version': version,
                'declarations_required': required,
                'candidate_sha256': 'a' * 64,
                'source_sha256': smoke.source_inputs(), 'fixture_sha256': smoke.fixture_inputs(),
                'checks': sorted(smoke.DEFAULT_CHECKS | (smoke.DECLARATION_CHECKS if required else set()) |
                                 (smoke.ASSESSMENT_CHECKS if assessment_required else set())),
                'stdout_sha256': {key: 'b' * 64 for key in keys},
                'observed_facts': ({'declarations': copy.deepcopy(smoke.FACTS),
                                    'assessment': copy.deepcopy(smoke.ASSESSMENT_FACTS)} if assessment_required else
                                   copy.deepcopy(smoke.FACTS) if required else {}),
                'source_removed_before_compare': required, 'worker_required': False,
                'negative_cases': ['duplicate-json-key', 'malformed-json'] if required else []}
        if assessment_required:
            receipt['assessment_required'] = True
        return receipt

    def test_version_gate_preserves_earlier_releases(self):
        for version in ('0.1.0', '0.2.0', '0.3.0', '0.4.0', '0.4.99'):
            self.assertFalse(smoke.declarations_required(version))
            self.assertFalse(smoke.assessment_required(version))
            receipt = self.receipt(version)
            self.assertNotIn('assessment_required', receipt)
            self.assertEqual(receipt, smoke.validate_receipt(receipt, version, 'a' * 64))
        for version in ('0.5.0-alpha.1', '0.5.0-beta.0', '0.5.0-rc.1', '0.5.0', '1.0.0'):
            self.assertTrue(smoke.declarations_required(version))
            self.assertFalse(smoke.assessment_required(version))
            self.assertEqual(smoke.DEFAULT_CHECKS | smoke.DECLARATION_CHECKS,
                             set(self.receipt(version)['checks']))
        for version in ('1.3.99', '1.3.99-rc.9'):
            self.assertFalse(smoke.assessment_required(version))
            self.assertEqual(smoke.DEFAULT_CHECKS | smoke.DECLARATION_CHECKS,
                             set(self.receipt(version)['checks']))
        for version in ('1.4.0-alpha.1', '1.4.0-beta.0', '1.4.0-rc.1', '1.4.0', '1.4.1', '2.0.0'):
            self.assertTrue(smoke.assessment_required(version))
            self.assertEqual(smoke.DEFAULT_CHECKS | smoke.DECLARATION_CHECKS | smoke.ASSESSMENT_CHECKS,
                             set(self.receipt(version)['checks']))
        for version in ('', '0.5', '00.5.0', '0.5.0-dev', 'v0.5.0', '0.5.0;command'):
            with self.assertRaises(ValueError):
                smoke.declarations_required(version)
            with self.assertRaises(ValueError):
                smoke.assessment_required(version)

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
        changed = copy.deepcopy(baseline)
        changed['assessment_required'] = False
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

    def test_assessment_receipts_require_new_gate_checks_and_facts(self):
        version = '1.4.0-beta.1'
        baseline = self.receipt(version)
        self.assertEqual(baseline, smoke.validate_receipt(baseline, version, 'a' * 64))
        for mutate in (
            lambda r: r['checks'].remove('assessment_lock_partition'),
            lambda r: r['stdout_sha256'].pop('assessment_all'),
            lambda r: r['observed_facts']['assessment'].update(parsed_projects=smoke.ASSESSMENT_FACTS['parsed_projects'] + 1),
            lambda r: r.update(assessment_required=False),
            lambda r: r.pop('assessment_required'),
        ):
            changed = copy.deepcopy(baseline)
            mutate(changed)
            with self.assertRaises(ValueError):
                smoke.validate_receipt(changed, version, 'a' * 64)

    def test_assessment_parity_compares_the_native_payload(self):
        assessment = {'inventory': {'files': {'count': 1}}, 'projects': {'count': 1}}
        standalone = {'schema_version': '1.9.0', 'languages': [{'name': 'Go'}],
                      'assessment': assessment, 'modules': []}
        combined = {'schema_version': '1.9.0', 'languages': [{'name': 'Go'}],
                    'assessment': copy.deepcopy(assessment), 'modules': [{'name': 'metrics'}]}
        standalone_raw = json.dumps(standalone).encode()
        combined_raw = json.dumps(combined, sort_keys=True).encode()
        self.assertEqual(assessment, smoke.check_assessment_parity(standalone_raw, combined_raw)['assessment'])
        combined['assessment']['projects']['count'] = 2
        with self.assertRaises(ValueError):
            smoke.check_assessment_parity(standalone_raw, json.dumps(combined).encode())

    def test_assessment_lock_states_partition_all_projects(self):
        def metric(count):
            return {'count': count, 'completeness': 'complete'}

        report = {
            'schema_version': '1.9.0',
            'assessment': {
                'inventory': {'files': metric(14), 'bytes': metric(smoke.ASSESSMENT_FACTS['logical_bytes'])},
                'manifest_candidate_population': metric(12),
                'projects': metric(10),
                'project_roots': metric(10),
                'lockfiles_overall': {
                    'eligible': metric(1), 'covered': metric(0), 'missing': metric(1),
                    'not_applicable': metric(3), 'unsupported': metric(6), 'unknown': metric(0),
                },
            },
        }
        smoke.check_assessment_facts(report)
        for field, value in (('not_applicable', 2), ('eligible', 4)):
            changed = copy.deepcopy(report)
            changed['assessment']['lockfiles_overall'][field]['count'] = value
            with self.assertRaises(ValueError):
                smoke.check_assessment_facts(changed)

    def test_helper_and_fixture_coverage(self):
        self.assertEqual({'scripts/declarations_release_smoke.py', 'scripts/wheels.py'}, set(smoke.source_inputs()))
        self.assertEqual(set(smoke.FIXTURES), set(smoke.fixture_inputs()))
        self.assertEqual(12, len(smoke.EXPECTED_MANIFESTS))
        self.assertEqual(['cargo', 'dotnet', 'go', 'maven', 'npm', 'python-uv'], smoke.FACTS['ecosystems'])
        for value in smoke.fixture_inputs().values():
            self.assertTrue(smoke.valid_digest(value))
        self.assertEqual(12, smoke.ASSESSMENT_FACTS['manifest_candidates'])
        self.assertEqual(10, smoke.ASSESSMENT_FACTS['parsed_projects'])

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
