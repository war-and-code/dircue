"""Reject incomplete or unbound proof; synthetic receipts are not execution evidence."""
import copy
from pathlib import Path
import sys
import unittest

ROOT = Path(__file__).resolve().parents[2]
sys.path.insert(0, str(ROOT / 'scripts'))
import formats_release_smoke as formats
import hotspots_release_smoke as hotspots


def formats_receipt(version='0.6.0'):
    return {'schema_version': '1.0.0', 'version': version, 'passed': True,
            'candidate_sha256': 'a' * 64, 'source_sha256': formats.source_inputs(),
            'fixture_sha256': formats.fixture_inputs(), 'checks': sorted(formats.CHECKS),
            'observed_facts': copy.deepcopy(formats.FACTS), 'worker_required': False,
            'external_tools_required': False, 'source_removed_before_compare': True, 'stdout_sha256': {key: 'c' * 64 for key in
            ('default_languages', 'default_all', 'formats', 'combined', 'self_compare')}}


def hotspots_receipt(version='0.6.0'):
    return {'schema_version': '1.0.0', 'version': version, 'passed': True,
            'candidate_sha256': 'a' * 64, 'worker_sha256': 'b' * 64,
            'source_sha256': hotspots.source_inputs(), 'fixture_sha256': hotspots.fixture_inputs(),
            'checks': sorted(hotspots.CHECKS), 'observed_facts': copy.deepcopy(hotspots.FACTS),
            'worker_required': True, 'source_removed_before_compare': True, 'stdout_sha256': {key: 'c' * 64 for key in
            ('default_structure', 'hotspots', 'combined', 'functions', 'recovered', 'empty', 'self_compare')}}


class CapabilityProofContracts(unittest.TestCase):
    def test_version_gate(self):
        for module in (formats, hotspots):
            for version in ('0.1.0', '0.4.0', '0.5.0', '0.5.99'):
                self.assertFalse(module.required(version))
            for version in ('0.6.0-rc.1', '0.6.0', '1.0.0'):
                self.assertTrue(module.required(version))
            for version in ('', '0.6', '00.6.0', '0.6.0-dev', 'v0.6.0', '0.6.0;command'):
                with self.assertRaises(ValueError):
                    module.required(version)

    def test_formats_identity_scope_and_inventory(self):
        receipt = formats_receipt()
        self.assertEqual(receipt, formats.validate_receipt(receipt, '0.6.0', 'a' * 64))
        for key, value in [('passed', False), ('version', '0.5.0'), ('candidate_sha256', 'b' * 64),
                           ('source_sha256', {}), ('fixture_sha256', {}), ('checks', []),
                           ('observed_facts', {}), ('worker_required', True), ('external_tools_required', True),
                           ('stdout_sha256', {}), ('source_removed_before_compare', False)]:
            changed = copy.deepcopy(receipt)
            changed[key] = value
            with self.subTest(key=key), self.assertRaises(ValueError):
                formats.validate_receipt(changed, '0.6.0', 'a' * 64)
        for mutate in (lambda r: r['checks'].append(r['checks'][0]),
                       lambda r: r['stdout_sha256'].update(formats=''),
                       lambda r: r['observed_facts'].update(archive_expansion=True)):
            changed = copy.deepcopy(receipt)
            mutate(changed)
            with self.assertRaises(ValueError):
                formats.validate_receipt(changed, '0.6.0', 'a' * 64)

    def test_hotspots_identity_scope_and_inventory(self):
        receipt = hotspots_receipt()
        self.assertEqual(receipt, hotspots.validate_receipt(receipt, '0.6.0', 'a' * 64, 'b' * 64))
        for key, value in [('passed', False), ('version', '0.5.0'), ('candidate_sha256', 'b' * 64),
                           ('worker_sha256', 'a' * 64), ('source_sha256', {}), ('fixture_sha256', {}),
                           ('checks', []), ('observed_facts', {}), ('worker_required', False), ('stdout_sha256', {}), ('source_removed_before_compare', False)]:
            changed = copy.deepcopy(receipt)
            changed[key] = value
            with self.subTest(key=key), self.assertRaises(ValueError):
                hotspots.validate_receipt(changed, '0.6.0', 'a' * 64, 'b' * 64)
        for mutate in (lambda r: r['checks'].append(r['checks'][0]),
                       lambda r: r['stdout_sha256'].update(hotspots=3),
                       lambda r: r['observed_facts'].update(python_clean_spaces=1024),
                       lambda r: r.update(older_worker_refusal={}),
                       lambda r: r.update(older_worker_refusal={'worker_sha256': 'd' * 64, 'exit_code': 0,
                                                              'stdout_bytes': 0, 'update_instruction': True})):
            changed = copy.deepcopy(receipt)
            mutate(changed)
            with self.assertRaises(ValueError):
                hotspots.validate_receipt(changed, '0.6.0', 'a' * 64, 'b' * 64)

    def test_malformed_receipts_fail_without_type_errors(self):
        for value in (None, [], {}, {'checks': None}):
            with self.assertRaises(ValueError):
                formats.validate_receipt(value, '0.6.0', 'a' * 64)
            with self.assertRaises(ValueError):
                hotspots.validate_receipt(value, '0.6.0', 'a' * 64, 'b' * 64)

    def test_fixture_inventory_is_authored_and_source_bound(self):
        self.assertEqual(23, len(formats.FIXTURES))
        self.assertEqual(14, len(hotspots.fixture_inputs()))
        self.assertEqual(140, hotspots.FIXTURES['zz_late.py'].count(b'def ordinary_'))
        self.assertGreater(sum(data.count(b'def ') for data in hotspots.FIXTURES.values()), 1024)
        self.assertEqual(set(formats.FIXTURES), set(formats.fixture_inputs()))
        self.assertTrue(all(formats.valid_digest(digest) for digest in formats.source_inputs().values()))
        self.assertIn('scripts/formats_release_smoke.py', hotspots.source_inputs())


if __name__ == '__main__':
    unittest.main()
