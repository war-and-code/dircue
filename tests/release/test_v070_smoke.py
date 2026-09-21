"""Reject incomplete or unbound 0.7 targeted proof receipts."""
import copy
from pathlib import Path
import sys
import unittest

ROOT = Path(__file__).resolve().parents[2]
sys.path.insert(0, str(ROOT / 'scripts'))
import targeted_release_smoke as targeted


def targeted_receipt(version='0.7.0'):
    return {
        'schema_version': '1.0.0', 'version': version, 'passed': True,
        'candidate_sha256': 'a' * 64, 'source_sha256': targeted.source_inputs(),
        'fixture_sha256': targeted.fixture_inputs(), 'checks': sorted(targeted.CHECKS),
        'observed_facts': copy.deepcopy(targeted.FACTS), 'worker_required': False,
        'external_tools_required': False, 'source_removed_before_saved_explain': True,
        'stdout_sha256': {key: 'c' * 64 for key in
                          ('default_all', 'focus', 'availability', 'fresh_explain', 'saved_explain')},
    }


class TargetedCapabilityProofContracts(unittest.TestCase):
    def test_version_gate_preserves_prior_releases(self):
        for version in ('0.1.0', '0.5.0', '0.6.0', '0.6.99'):
            self.assertFalse(targeted.required(version))
        for version in ('0.7.0-alpha.1', '0.7.0-rc.1', '0.7.0', '1.0.0'):
            self.assertTrue(targeted.required(version))
        for version in ('', '0.7', '00.7.0', 'v0.7.0', '0.7.0;command'):
            with self.assertRaises(ValueError):
                targeted.required(version)

    def test_receipt_identity_scope_and_exact_inventory(self):
        receipt = targeted_receipt()
        self.assertEqual(receipt, targeted.validate_receipt(receipt, '0.7.0', 'a' * 64))
        mutations = {
            'passed': lambda r: r.update(passed=False),
            'version': lambda r: r.update(version='0.6.0'),
            'candidate': lambda r: r.update(candidate_sha256='b' * 64),
            'source': lambda r: r.update(source_sha256={}),
            'fixture': lambda r: r.update(fixture_sha256={}),
            'checks missing': lambda r: r['checks'].pop(),
            'checks duplicated': lambda r: r['checks'].append(r['checks'][0]),
            'facts': lambda r: r['observed_facts'].update(valid_lfs_pointers=2),
            'stdout missing': lambda r: r['stdout_sha256'].pop('focus'),
            'stdout malformed': lambda r: r['stdout_sha256'].update(availability=''),
            'worker': lambda r: r.update(worker_required=True),
            'external': lambda r: r.update(external_tools_required=True),
            'source retained': lambda r: r.update(source_removed_before_saved_explain=False),
            'extra field': lambda r: r.update(unbound=True),
        }
        for name, mutate in mutations.items():
            changed = copy.deepcopy(receipt)
            mutate(changed)
            with self.subTest(name=name), self.assertRaises(ValueError):
                targeted.validate_receipt(changed, '0.7.0', 'a' * 64)

    def test_malformed_receipts_fail_closed(self):
        for value in (None, [], {}, {'checks': None}):
            with self.subTest(value=value), self.assertRaises(ValueError):
                targeted.validate_receipt(value, '0.7.0', 'a' * 64)

    def test_fixture_inventory_and_sources_are_authored(self):
        self.assertEqual(7, len(targeted.FIXTURES))
        self.assertEqual(set(targeted.FIXTURES), set(targeted.fixture_inputs()))
        self.assertEqual(64, len(targeted.POINTER.split(b'oid sha256:', 1)[1].splitlines()[0]))
        self.assertTrue(all(targeted.valid_digest(value) for value in targeted.source_inputs().values()))


if __name__ == '__main__':
    unittest.main()
