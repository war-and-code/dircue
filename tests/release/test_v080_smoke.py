"""Reject incomplete or unbound 0.8 context proof receipts."""
import copy
from pathlib import Path
import sys
import unittest

ROOT = Path(__file__).resolve().parents[2]
sys.path.insert(0, str(ROOT / 'scripts'))
import context_release_smoke as context


def context_receipt(version='0.8.0'):
    return {
        'schema_version': '1.0.0', 'version': version, 'platform': 'linux-amd64', 'passed': True,
        'candidate_sha256': 'a' * 64, 'source_sha256': context.source_inputs(),
        'fixture_sha256': context.fixture_inputs(), 'checks': sorted(context.CHECKS),
        'observed_facts': copy.deepcopy(context.FACTS), 'worker_required': False,
        'external_tools_required': False, 'source_removed_before_plan': True,
        'stdout_sha256': {key: 'c' * 64 for key in
                          ('capabilities', 'environment', 'saved_profile', 'plan',
                           'focus_comparison', 'availability_comparison')},
    }


class ContextCapabilityProofContracts(unittest.TestCase):
    def test_version_gate_preserves_prior_releases(self):
        for version in ('0.1.0', '0.6.0', '0.7.0', '0.7.99'):
            self.assertFalse(context.required(version))
        for version in ('0.8.0-alpha.1', '0.8.0-rc.1', '0.8.0', '1.0.0'):
            self.assertTrue(context.required(version))
        for version in ('', '0.8', '00.8.0', 'v0.8.0', '0.8.0;command'):
            with self.assertRaises(ValueError):
                context.required(version)

    def test_receipt_identity_scope_and_exact_inventory(self):
        receipt = context_receipt()
        self.assertEqual(receipt, context.validate_receipt(receipt, '0.8.0', 'a' * 64, 'linux-amd64'))
        mutations = {
            'passed': lambda r: r.update(passed=False),
            'version': lambda r: r.update(version='0.7.0'),
            'candidate': lambda r: r.update(candidate_sha256='b' * 64),
            'platform': lambda r: r.update(platform='darwin-arm64'),
            'source': lambda r: r.update(source_sha256={}),
            'fixture': lambda r: r.update(fixture_sha256={}),
            'checks missing': lambda r: r['checks'].pop(),
            'checks duplicate': lambda r: r['checks'].append(r['checks'][0]),
            'facts': lambda r: r['observed_facts'].update(sdk_version='9.0.100'),
            'stdout missing': lambda r: r['stdout_sha256'].pop('plan'),
            'stdout malformed': lambda r: r['stdout_sha256'].update(capabilities=''),
            'worker': lambda r: r.update(worker_required=True),
            'external': lambda r: r.update(external_tools_required=True),
            'source retained': lambda r: r.update(source_removed_before_plan=False),
            'extra field': lambda r: r.update(unbound=True),
        }
        for name, mutate in mutations.items():
            changed = copy.deepcopy(receipt)
            mutate(changed)
            with self.subTest(name=name), self.assertRaises(ValueError):
                context.validate_receipt(changed, '0.8.0', 'a' * 64, 'linux-amd64')

    def test_malformed_receipts_fail_closed(self):
        for value in (None, [], {}, {'checks': None}):
            with self.subTest(value=value), self.assertRaises(ValueError):
                context.validate_receipt(value, '0.8.0', 'a' * 64, 'linux-amd64')

    def test_requirements_are_independently_enumerated(self):
        self.assertEqual(13, len(context.CHECKS))
        self.assertEqual(7, len(context.FIXTURES))
        self.assertEqual(set(context.FIXTURES), set(context.fixture_inputs()))
        self.assertEqual('>=3.12', context.FACTS['python_constraint'])
        self.assertEqual('8.0.300', context.FACTS['sdk_version'])
        self.assertTrue(all(context.valid_digest(value) for value in context.source_inputs().values()))

    def test_negative_commands_require_empty_stdout_and_diagnostic(self):
        diagnostic = context.output([sys.executable, '-c',
                                     'import sys; sys.stderr.write("rejected\\n"); raise SystemExit(2)'],
                                    succeeds=False)
        self.assertEqual(b'rejected\n', diagnostic)
        with self.assertRaises(ValueError):
            context.output([sys.executable, '-c',
                            'import sys; sys.stdout.write("unexpected"); raise SystemExit(2)'],
                           succeeds=False)


if __name__ == '__main__':
    unittest.main()
